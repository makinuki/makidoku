import { describe, expect, it } from "vite-plus/test";
import {
  MEKURI_SPREAD_CONFIG,
  buildMekuriEngineOptions,
  toMekuriMode,
  toMekuriPages,
  toZoneMapName,
} from "./mekuriAdapter";
import type { Page } from "../../types";
import type { MekuriReaderInput } from "./mekuriAdapter";
import { defaultReaderGlobals } from "./readerSettings";

function page(id: string): Page {
  return { id, chapterId: "chapter", index: 0, isScrambled: false };
}

describe("mekuri adapter", () => {
  it("maps each reader mode onto its engine mode", () => {
    expect(toMekuriMode("single")).toBe("single");
    expect(toMekuriMode("double")).toBe("double");
    expect(toMekuriMode("webtoon")).toBe("continuous-vertical");
  });

  it("maps each navigation preset onto its zone map name", () => {
    expect(toZoneMapName("default")).toBe("default-manga");
    expect(toZoneMapName("l")).toBe("l-shaped");
    expect(toZoneMapName("edge")).toBe("edge-only");
    expect(toZoneMapName("disabled")).toBe("disabled");
  });

  it("carries page ids into engine pages without dimensions", () => {
    const pages = [page("a"), page("b")];
    expect(toMekuriPages(pages)).toEqual([{ id: "a" }, { id: "b" }]);
  });

  it("builds engine options with the resolved mode, direction, and start page", () => {
    const options = buildMekuriEngineOptions({
      pages: [page("a"), page("b"), page("c")],
      globals: { mode: "webtoon", direction: "ltr", fit: "width" },
      direction: "rtl",
      index: 2,
      zoneMap: { name: "edge-only", zones: [] },
      resolveImage: (target) => `/api/pages/${target.id}/image`,
    });
    expect(options.initialState).toEqual({
      pageIndex: 2,
      mode: "continuous-vertical",
      direction: "rtl",
      spreadConfig: { ...MEKURI_SPREAD_CONFIG },
    });
    expect(options.zoneMap?.name).toBe("edge-only");
  });

  it("clamps the start page into the chapter bounds", () => {
    const pages = [page("a"), page("b")];
    const base: Omit<MekuriReaderInput, "index"> = {
      pages,
      globals: defaultReaderGlobals,
      direction: "ltr",
      resolveImage: (target: Page) => `/api/pages/${target.id}/image`,
    };
    expect(buildMekuriEngineOptions({ ...base, index: 9 }).initialState?.pageIndex).toBe(1);
    expect(buildMekuriEngineOptions({ ...base, index: -4 }).initialState?.pageIndex).toBe(0);
  });

  it("appends the attempt cache-buster on retries only", async () => {
    const seen: string[] = [];
    const options = buildMekuriEngineOptions({
      pages: [page("a")],
      globals: { mode: "single", direction: "ltr", fit: "width" },
      direction: "ltr",
      index: 0,
      zoneMap: { name: "default-manga", zones: [] },
      resolveImage: (target, attempt) => {
        const url = `/api/pages/${target.id}/image${attempt > 0 ? `?attempt=${attempt}` : ""}`;
        seen.push(url);
        return url;
      },
    });
    const first = await options.resolveSrc?.({ id: "a" }, 0);
    const retry = await options.resolveSrc?.({ id: "a" }, 2);
    expect(first).toBe("/api/pages/a/image");
    expect(retry).toBe("/api/pages/a/image?attempt=2");
    expect(seen).toEqual(["/api/pages/a/image", "/api/pages/a/image?attempt=2"]);
  });

  it("resolves an empty source for pages outside the chapter", async () => {
    const options = buildMekuriEngineOptions({
      pages: [page("a")],
      globals: { mode: "single", direction: "ltr", fit: "width" },
      direction: "ltr",
      index: 0,
      zoneMap: { name: "default-manga", zones: [] },
      resolveImage: (target) => `/api/pages/${target.id}/image`,
    });
    expect(await options.resolveSrc?.({ id: "missing" }, 0)).toBe("");
  });
});
