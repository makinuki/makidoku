import { describe, expect, it } from "vite-plus/test";
import {
  alignToSpread,
  defaultReaderGlobals,
  readerGlobalsFromSettings,
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
});
