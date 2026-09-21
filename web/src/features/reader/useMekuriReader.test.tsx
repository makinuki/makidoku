import { describe, expect, it } from "vite-plus/test";
import { renderHook } from "@testing-library/react";
import { createMekuriEngine, type MekuriEngineOptions } from "@makinuki/mekuri/engine";
import { useMekuriReader } from "./useMekuriReader";
import { toMekuriPages } from "./mekuriAdapter";
import type { Page } from "../../types";
import type { ReaderMode } from "./readerSettings";

function pages(count: number): Page[] {
  return Array.from({ length: count }, (_, at) => ({
    id: `page-${at}`,
    chapterId: "chapter",
    index: at,
    isScrambled: false,
  }));
}

const resolveImage = (pageId: string | number, attempt: number) =>
  `/api/pages/${pageId}/image${attempt > 1 ? `?attempt=${attempt}` : ""}`;

describe("useMekuriReader", () => {
  it("stays null until enabled, then creates the engine at the start page", () => {
    const mekuriPages = toMekuriPages(pages(4));
    const { result, rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) =>
        useMekuriReader({
          pages: mekuriPages,
          mode: "double",
          direction: "rtl",
          navigation: "l",
          initialPageIndex: 3,
          enabled,
          resolveImage,
        }),
      { initialProps: { enabled: false } },
    );
    expect(result.current).toBeNull();
    rerender({ enabled: true });
    // Double mode keeps the cover alone, so index 3 stays on its own spread.
    expect(result.current?.getState().pageIndex).toBe(3);
    expect(result.current?.getState().activeSpreads).toEqual([[0], [1, 2], [3]]);
    expect(result.current?.getState().mode).toBe("double");
    expect(result.current?.getState().direction).toBe("rtl");
    expect(result.current?.getState().activeZoneMap.name).toBe("l-shaped");
  });

  it("syncs mode and direction changes through engine transitions", () => {
    const mekuriPages = toMekuriPages(pages(4));
    const { result, rerender } = renderHook(
      ({ mode }: { mode: ReaderMode }) =>
        useMekuriReader({
          pages: mekuriPages,
          mode,
          direction: "ltr",
          navigation: "default",
          initialPageIndex: 1,
          enabled: true,
          resolveImage,
        }),
      { initialProps: { mode: "single" as ReaderMode } },
    );
    expect(result.current?.getState().pageIndex).toBe(1);
    rerender({ mode: "double" as ReaderMode });
    expect(result.current?.getState().mode).toBe("double");
    // Index 1 already opens the [1, 2] spread under cover-first pairing.
    expect(result.current?.getState().pageIndex).toBe(1);
    rerender({ mode: "webtoon" as ReaderMode });
    expect(result.current?.getState().mode).toBe("continuous-vertical");
  });

  it("resolves image sources through the host resolver", async () => {
    const options: MekuriEngineOptions = {
      pages: toMekuriPages(pages(2)),
      resolveSrc: (page, attempt) => resolveImage(page.id, attempt),
    };
    const engine = createMekuriEngine(options);
    await expect(engine.resolvePageSrc("page-1")).resolves.toBe("/api/pages/page-1/image");
    engine.retryPage("page-1");
    await expect(engine.resolvePageSrc("page-1")).resolves.toBe(
      "/api/pages/page-1/image?attempt=2",
    );
  });
});
