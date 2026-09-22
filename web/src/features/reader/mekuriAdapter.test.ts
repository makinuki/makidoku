import { describe, expect, it } from "vite-plus/test";
import { toMekuriMode, toMekuriPages } from "./mekuriAdapter";
import type { Page } from "../../types";

function page(id: string): Page {
  return { id, chapterId: "chapter", index: 0, isScrambled: false };
}

describe("mekuri adapter", () => {
  it("maps each reader mode onto its engine mode", () => {
    expect(toMekuriMode("single")).toBe("single");
    expect(toMekuriMode("double")).toBe("double");
    expect(toMekuriMode("webtoon")).toBe("continuous-vertical");
  });

  it("carries page ids into engine pages without dimensions", () => {
    const pages = [page("a"), page("b")];
    expect(toMekuriPages(pages)).toEqual([{ id: "a" }, { id: "b" }]);
  });
});
