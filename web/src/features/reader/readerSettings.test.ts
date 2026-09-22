import { describe, expect, it } from "vite-plus/test";
import {
  alignToSpread,
  defaultReaderDisplay,
  defaultReaderGlobals,
  readerDisplayFromSettings,
  readerGlobalsFromSettings,
  readerImageFilter,
  readerOverridesFromManga,
  resolveReaderSettings,
  spreadStep,
} from "./readerSettings";

describe("reader settings resolution", () => {
  it("reads globals from the settings service and ignores unknown values", () => {
    const globals = readerGlobalsFromSettings([
      { key: "reader.default_mode", value: "webtoon" },
      { key: "reader.direction", value: "rtl" },
      { key: "reader.fit", value: "bogus" },
    ]);
    expect(globals).toEqual({ mode: "webtoon", direction: "rtl", fit: "width" });
  });

  it("prefers a per-title override and falls back to the global value", () => {
    const resolved = resolveReaderSettings(
      { mode: "single", direction: "ltr", fit: "width" },
      { mode: "double", direction: null, fit: null },
    );
    expect(resolved).toEqual({ mode: "double", direction: "ltr", fit: "width" });
  });

  it("keeps the built-in defaults when nothing is stored", () => {
    const overrides = readerOverridesFromManga({});
    expect(resolveReaderSettings(defaultReaderGlobals, overrides)).toEqual(defaultReaderGlobals);
  });

  it("aligns every paged entry point to the same double spread", () => {
    expect(spreadStep("double")).toBe(2);
    expect(spreadStep("single")).toBe(1);
    expect(alignToSpread(3, 10, "double")).toBe(2);
    expect(alignToSpread(3, 10, "single")).toBe(3);
    expect(alignToSpread(-4, 10, "double")).toBe(0);
    expect(alignToSpread(99, 10, "double")).toBe(8);
    expect(alignToSpread(99, 10, "single")).toBe(9);
  });

  it("reads the extended fit value and display preferences", () => {
    const globals = readerGlobalsFromSettings([{ key: "reader.fit", value: "screen" }]);
    expect(globals.fit).toBe("screen");
    const display = readerDisplayFromSettings([
      { key: "reader.navigation", value: "edge-only" },
      { key: "reader.webtoon_gap", value: 24 },
      { key: "reader.theme", value: "paper" },
      { key: "reader.brightness", value: 120 },
      { key: "reader.grayscale", value: true },
      { key: "reader.invert", value: false },
    ]);
    expect(display).toEqual({
      navigation: "edge-only",
      gap: 24,
      theme: "paper",
      brightness: 120,
      grayscale: true,
      invert: false,
    });
    expect(readerImageFilter(display)).toBe("brightness(120%) grayscale(1)");
    expect(readerImageFilter(defaultReaderDisplay)).toBe("none");
  });

  it("maps retired navigation names onto their presets", () => {
    expect(readerDisplayFromSettings([{ key: "reader.navigation", value: "l" }]).navigation).toBe(
      "l-shaped",
    );
    expect(
      readerDisplayFromSettings([{ key: "reader.navigation", value: "edge" }]).navigation,
    ).toBe("edge-only");
    expect(
      readerDisplayFromSettings([{ key: "reader.navigation", value: "default" }]).navigation,
    ).toBe("default-manga");
    expect(
      readerDisplayFromSettings([{ key: "reader.navigation", value: "corners" }]).navigation,
    ).toBe("default-manga");
    expect(readerDisplayFromSettings([]).navigation).toBe("default-manga");
  });

  it("clamps display preferences to their valid ranges", () => {
    const display = readerDisplayFromSettings([
      { key: "reader.navigation", value: "corners" },
      { key: "reader.webtoon_gap", value: 99 },
      { key: "reader.theme", value: "sepia" },
      { key: "reader.brightness", value: "dim" },
    ]);
    expect(display).toEqual({ ...defaultReaderDisplay, gap: 48 });
  });
});
