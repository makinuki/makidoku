import { act, cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vite-plus/test";
import {
  createMekuriEngine,
  DISABLED_ZONE_MAP,
  type MekuriEngine,
  type MekuriEngineOptions,
  type MekuriPage,
} from "@makinuki/mekuri/engine";
import { PagedView, WebtoonView } from "@makinuki/mekuri/views";
import {
  mockResizeObserver,
  mockScrollGeometry,
  mockViewportDimensions,
  simulateTouchGesture,
  type MockResizeObserverHandle,
} from "@makinuki/mekuri/test-utils";
import { toMekuriPages } from "./mekuriAdapter";
import type { Page } from "../../types";

const SURFACE_WIDTH = 800;
const SURFACE_HEIGHT = 1200;
const VIEWPORT_HEIGHT = 600;
const PAGE_HEIGHT = 1000;

function hostPages(count: number): Page[] {
  return Array.from({ length: count }, (_, index) => ({
    id: `page-${index}`,
    chapterId: "chapter",
    index,
    isScrambled: false,
  }));
}

function pages(count: number): MekuriPage[] {
  return toMekuriPages(hostPages(count));
}

const PAGED_KEYBOARD_MAP = {
  nextPage: ["ArrowRight", "KeyD"],
  toggleHUD: ["KeyM"],
};

function renderPaged(
  engineOptions: Partial<MekuriEngineOptions> = {},
  props: { boundarySlot?: React.ReactNode } = {},
) {
  const viewPages = engineOptions.pages ?? pages(6);
  const engine = createMekuriEngine({ pages: viewPages, ...engineOptions });
  const renderResult = render(
    <PagedView
      engine={engine}
      pages={viewPages}
      renderPage={(page, index) => <span data-host-page={index}>{String(page.id)}</span>}
      hud={false}
      boundarySlot={props.boundarySlot}
      keyboardOptions={{ map: PAGED_KEYBOARD_MAP }}
    />,
  );
  return { engine, ...renderResult };
}

function surfaceOf(container: HTMLElement): HTMLElement {
  const element = container.querySelector("[data-mekuri-viewport]");
  expect(element).not.toBeNull();
  return element as HTMLElement;
}

function pressKey(code: string): void {
  act(() => {
    document.dispatchEvent(new KeyboardEvent("keydown", { code, bubbles: true, cancelable: true }));
  });
}

async function tapAt(container: HTMLElement, x: number, y: number): Promise<() => void> {
  const surface = surfaceOf(container);
  const restore = mockViewportDimensions(surface, {
    width: SURFACE_WIDTH,
    height: SURFACE_HEIGHT,
  });
  await act(async () => {
    await simulateTouchGesture(surface, { type: "tap", at: { x, y } });
  });
  return restore;
}

let geometryRestore: (() => void) | null = null;

function mountWebtoonGeometry(): MockResizeObserverHandle {
  const restoreGeometry = mockScrollGeometry({
    viewportHeight: VIEWPORT_HEIGHT,
    pageHeight: PAGE_HEIGHT,
  });
  const resize = mockResizeObserver();
  geometryRestore = () => {
    resize.restore();
    restoreGeometry();
  };
  return resize;
}

afterEach(() => {
  cleanup();
  geometryRestore?.();
  geometryRestore = null;
});

describe("paged view integration", () => {
  it("renders the spread at the reading position with host page bodies", () => {
    const { container } = renderPaged({ initialState: { mode: "single" } });

    const boxes = [...container.querySelectorAll("[data-mekuri-paged-page]")];
    expect(boxes).toHaveLength(1);
    expect(boxes[0].getAttribute("data-index")).toBe("0");
    expect(container.querySelector("[data-host-page]")?.textContent).toBe("page-0");
    expect(container.querySelector("[data-mekuri-paged]")?.getAttribute("data-mode")).toBe(
      "single",
    );
  });

  it("renders both pages of a double spread", () => {
    const { engine, container } = renderPaged({ initialState: { mode: "double" } });

    act(() => engine.next());

    const boxes = [...container.querySelectorAll("[data-mekuri-paged-page]")];
    expect(boxes.map((box) => box.getAttribute("data-index"))).toEqual(["1", "2"]);
  });

  it("turns pages from taps on the side zones", async () => {
    const { engine, container } = renderPaged({ initialState: { mode: "single" } });

    const restore = await tapAt(container, 700, 600);
    expect(engine.getState().pageIndex).toBe(1);
    const restoreBack = await tapAt(container, 100, 600);
    expect(engine.getState().pageIndex).toBe(0);
    restore();
    restoreBack();
  });

  it("toggles the chrome from a center tap", async () => {
    const { engine, container } = renderPaged({ initialState: { mode: "single" } });

    expect(engine.getState().isHUDVisible).toBe(true);
    const restore = await tapAt(container, 400, 600);
    expect(engine.getState().isHUDVisible).toBe(false);
    restore();
  });

  it("zooms on a double tap and resets on the next pair", async () => {
    const { engine, container } = renderPaged({ initialState: { mode: "single" } });
    const surface = surfaceOf(container);
    const restore = mockViewportDimensions(surface, {
      width: SURFACE_WIDTH,
      height: SURFACE_HEIGHT,
    });
    try {
      await act(async () => {
        await simulateTouchGesture(surface, { type: "doubleTap", at: { x: 400, y: 600 } });
      });
      expect(engine.getState().zoomScale).toBe(2);
      expect(container.querySelector("[data-mekuri-paged]")?.getAttribute("data-zoomed")).toBe(
        "true",
      );

      await act(async () => {
        await simulateTouchGesture(surface, { type: "doubleTap", at: { x: 400, y: 600 } });
      });
      expect(engine.getState().zoomScale).toBe(1);
    } finally {
      restore();
    }
  });

  it("turns pages from a swipe", async () => {
    const { engine, container } = renderPaged({ initialState: { mode: "single" } });
    const surface = surfaceOf(container);

    await act(async () => {
      await simulateTouchGesture(surface, {
        type: "swipe",
        from: { x: 600, y: 600 },
        to: { x: 400, y: 600 },
      });
    });

    expect(engine.getState().pageIndex).toBe(1);
  });

  it("mirrors zones and placement in RTL while the DOM keeps reading order", async () => {
    const { engine, container } = renderPaged({ initialState: { mode: "single" } });

    act(() => {
      engine.goToIndex(2);
      engine.setDirection("rtl");
    });
    expect(container.querySelector("[data-mekuri-paged]")?.getAttribute("dir")).toBe("rtl");

    // The right zone carries the previous action in RTL.
    const restore = await tapAt(container, 700, 600);
    expect(engine.getState().pageIndex).toBe(1);
    restore();
  });

  it("ignores taps while the zone map is disabled", async () => {
    const { engine, container } = renderPaged({
      initialState: { mode: "single" },
      zoneMap: DISABLED_ZONE_MAP,
    });

    const restore = await tapAt(container, 700, 600);
    expect(engine.getState().pageIndex).toBe(0);
    restore();
  });

  it("turns pages from the keyboard map without the dropped keys", () => {
    const { engine } = renderPaged({ initialState: { mode: "single" } });

    pressKey("ArrowRight");
    expect(engine.getState().pageIndex).toBe(1);
    pressKey("KeyM");
    expect(engine.getState().isHUDVisible).toBe(false);

    // Space and Escape stay host-side, so the dispatcher ignores them.
    pressKey("Space");
    expect(engine.getState().pageIndex).toBe(1);
    pressKey("Escape");
    expect(engine.getState().isHUDVisible).toBe(false);
  });

  it("locks page turns while zoomed but keeps programmatic jumps", () => {
    const { engine, container } = renderPaged({ initialState: { mode: "single" } });

    act(() => engine.setZoomScale(2));
    expect(container.querySelector("[data-mekuri-paged]")?.getAttribute("data-zoomed")).toBe(
      "true",
    );
    pressKey("ArrowRight");
    expect(engine.getState().pageIndex).toBe(0);

    act(() => engine.goToIndex(2));
    expect(engine.getState().pageIndex).toBe(2);
  });

  it("mounts host content at the boundary slot only while provided", () => {
    const { container, rerender, engine } = renderPaged(
      { initialState: { mode: "single" } },
      { boundarySlot: <p>Chapter complete</p> },
    );
    expect(container.querySelector("[data-mekuri-boundary]")?.textContent).toBe("Chapter complete");

    rerender(
      <PagedView
        engine={engine}
        pages={pages(6)}
        hud={false}
        keyboardOptions={{ map: PAGED_KEYBOARD_MAP }}
      />,
    );
    expect(container.querySelector("[data-mekuri-boundary]")).toBeNull();
  });
});

describe("webtoon view integration", () => {
  function renderWebtoon(
    engineOptions: Partial<MekuriEngineOptions> = {},
    gap = 8,
    now?: () => number,
  ): { engine: MekuriEngine; container: HTMLElement } {
    const viewPages = engineOptions.pages ?? pages(4);
    const engine = createMekuriEngine({
      pages: viewPages,
      initialState: { mode: "continuous-vertical" },
      ...engineOptions,
    });
    const { container } = render(
      <WebtoonView
        engine={engine}
        pages={viewPages}
        gap={gap}
        now={now}
        renderPage={(page, index) => <span data-host-page={index}>{String(page.id)}</span>}
        hud={false}
        keyboardOptions={{ map: PAGED_KEYBOARD_MAP }}
      />,
    );
    return { engine, container };
  }

  it("renders the measured column with the host gap", () => {
    const resize = mountWebtoonGeometry();
    const { container } = renderWebtoon();
    act(() => {
      resize.fireAll(() => PAGE_HEIGHT);
    });

    expect(container.querySelector('[data-mekuri-view="webtoon"]')).not.toBeNull();
    expect(container.querySelectorAll("[data-host-page]").length).toBeGreaterThan(0);
    // The gap flows through the virtualizer layout: the second page starts
    // one page height plus the gap down the column.
    const items = [...container.querySelectorAll("[data-mekuri-page]")] as HTMLElement[];
    expect(items.length).toBeGreaterThan(1);
    expect(items[1].style.top).toBe(`${PAGE_HEIGHT + 8}px`);
  });

  it("reports user scrolls back to the engine", async () => {
    const resize = mountWebtoonGeometry();
    // The alignment lock reads this clock, so settling the mount window is a
    // variable assignment instead of a real wait.
    let now = 1000;
    const { engine, container } = renderWebtoon({}, 8, () => now);
    act(() => {
      resize.fireAll(() => PAGE_HEIGHT);
    });
    const scroller = surfaceOf(container);

    now += 2000;
    await act(async () => {
      scroller.scrollTop = PAGE_HEIGHT + 500;
      scroller.dispatchEvent(new Event("scroll", { bubbles: true }));
      await new Promise((resolve) => {
        setTimeout(resolve, 150);
      });
    });

    expect(engine.getState().pageIndex).toBe(1);
  });

  it("leaves Space unbound so the column scrolls natively", () => {
    mountWebtoonGeometry();
    const { engine } = renderWebtoon();

    pressKey("Space");

    expect(engine.getState().pageIndex).toBe(0);
  });

  it("turns the page from the keyboard", () => {
    mountWebtoonGeometry();
    const { engine } = renderWebtoon();

    pressKey("ArrowRight");

    expect(engine.getState().pageIndex).toBe(1);
  });
});
