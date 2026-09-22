import { describe, expect, it } from "vite-plus/test";
import { renderHook } from "@testing-library/react";
import { DEFAULT_KEYBOARD_MAP, type MekuriReadingPosition } from "@makinuki/mekuri/engine";
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

describe("useMekuriReader", () => {
  it("stays null until enabled, then creates the engine at the start page", () => {
    const mekuriPages = toMekuriPages(pages(4));
    const { result, rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) =>
        useMekuriReader({
          pages: mekuriPages,
          mode: "double",
          direction: "rtl",
          navigation: "l-shaped",
          initialPageIndex: 3,
          enabled,
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
          navigation: "default-manga",
          initialPageIndex: 1,
          enabled: true,
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

  it("emits position samples on discrete navigation for persistence", () => {
    const seen: MekuriReadingPosition[] = [];
    const { result } = renderHook(() =>
      useMekuriReader({
        pages: toMekuriPages(pages(4)),
        mode: "single",
        direction: "ltr",
        navigation: "default-manga",
        initialPageIndex: 0,
        enabled: true,
        onPositionSample: (position) => {
          seen.push(position);
        },
      }),
    );
    result.current?.next();
    result.current?.goToIndex(3);
    expect(seen.map((position) => position.pageIndex)).toEqual([1, 3]);
  });

  it("reflects keyboard overrides and host suppression through the engine", () => {
    const { result, rerender } = renderHook(
      ({ suppressed }: { suppressed: boolean }) =>
        useMekuriReader({
          pages: toMekuriPages(pages(3)),
          mode: "single",
          direction: "ltr",
          navigation: "default-manga",
          initialPageIndex: 0,
          enabled: true,
          keyboardMap: { toggleHUD: ["KeyM"] },
          suppressKeyboard: suppressed,
        }),
      { initialProps: { suppressed: false } },
    );
    expect(result.current?.getKeyboardMap().toggleHUD).toEqual(["KeyM"]);
    expect(result.current?.getKeyboardMap().nextPage).toEqual(DEFAULT_KEYBOARD_MAP.nextPage);
    expect(result.current?.isKeyboardSuppressed()).toBe(false);
    rerender({ suppressed: true });
    expect(result.current?.isKeyboardSuppressed()).toBe(true);
  });
});
