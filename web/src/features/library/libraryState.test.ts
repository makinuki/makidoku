import { describe, expect, it } from "vite-plus/test";
import type { RuntimeSetting } from "../../api";
import type { Category, LibraryManga } from "../../types";
import {
  categoryCounts,
  defaultLibraryView,
  gridClasses,
  groupLibrary,
  libraryViewFromSettings,
  libraryViewSettings,
  naturalDirection,
  invertSelection,
  selectAllIds,
  sourceDisplayName,
  toggleRangeSelection,
  toggleSelection,
  visibleLibrary,
  withoutFilter,
  type LibraryView,
} from "./libraryState";

function category(id: number, name: string): Category {
  return { id, name, sortOrder: id };
}

function manga(overrides: Partial<LibraryManga>): LibraryManga {
  return {
    id: "manga",
    sourceId: "source",
    title: "Title",
    status: "ongoing",
    coverUrl: "",
    inLibrary: true,
    downloadFormat: "cbz",
    createdAt: 0,
    updatedAt: 0,
    categories: [],
    unreadChapters: 0,
    downloadedChapters: 0,
    totalChapters: 0,
    bookmarkedChapters: 0,
    languages: [],
    ...overrides,
  };
}

function setting(key: string, value: string | number | boolean): RuntimeSetting {
  return { key, value, default: value, type: "string", description: "" };
}

describe("library view state", () => {
  it("reads stored settings and drops values it does not recognize", () => {
    const view = libraryViewFromSettings([
      setting("library.view.sort", "title"),
      setting("library.view.sort_direction", "sideways"),
      setting("library.view.card_size", "large"),
      setting("library.view.unread_badge", false),
      setting("library.view.category", 4),
      setting("library.view.filter_read_state", "unread, completed"),
      setting("library.view.filter_status", "bogus"),
      setting("library.view.filter_sources", "source-a,source-b"),
    ]);
    expect(view.sort).toBe("title");
    expect(view.direction).toBe("desc");
    expect(view.cardSize).toBe("large");
    expect(view.unreadBadge).toBe(false);
    expect(view.category).toBe(4);
    expect(view.filters.readState).toEqual(["unread", "completed"]);
    expect(view.filters.status).toEqual([]);
    expect(view.filters.sources).toEqual(["source-a", "source-b"]);
  });

  it("round trips the whole view through the settings service", () => {
    const view: LibraryView = {
      ...defaultLibraryView,
      sort: "unread",
      direction: "asc",
      cardSize: "small",
      columns: 4,
      groupBy: "source",
      unreadBadge: false,
      progressBar: false,
      continueButton: false,
      category: 7,
      filters: {
        readState: ["in_progress"],
        status: ["completed", "hiatus"],
        sources: ["source-a"],
        downloaded: true,
        started: false,
        bookmarked: true,
      },
    };
    const stored = libraryViewSettings(view).map(([key, value]) =>
      setting(key, value as RuntimeSetting["value"]),
    );
    expect(libraryViewFromSettings(stored)).toEqual(view);
  });

  it("filters and sorts the visible titles", () => {
    const items = [
      manga({
        id: "alpha",
        title: "Alpha",
        updatedAt: 3,
        unreadChapters: 2,
        sourceId: "one",
        categories: [category(5, "Favourites")],
      }),
      manga({
        id: "beta",
        title: "Beta",
        updatedAt: 5,
        sourceId: "two",
        status: "completed",
        progress: {
          mangaId: "beta",
          lastReadChapterId: "chapter",
          lastReadPage: 1,
          totalPages: 20,
          isCompleted: false,
          lastReadAt: 9,
        },
      }),
      manga({
        id: "gamma",
        title: "Gamma",
        altTitles: JSON.stringify(["Alternate Name"]),
        updatedAt: 1,
        unreadChapters: 1,
        sourceId: "one",
        status: "hiatus",
      }),
    ];
    const base = defaultLibraryView;
    expect(visibleLibrary(items, base, "").map((item) => item.id)).toEqual([
      "beta",
      "alpha",
      "gamma",
    ]);
    expect(visibleLibrary(items, base, "alternate").map((item) => item.id)).toEqual(["gamma"]);
    expect(
      visibleLibrary(
        items,
        { ...base, filters: { ...base.filters, readState: ["unread"] } },
        "",
      ).map((item) => item.id),
    ).toEqual(["alpha", "gamma"]);
    expect(
      visibleLibrary(items, { ...base, filters: { ...base.filters, sources: ["two"] } }, "").map(
        (item) => item.id,
      ),
    ).toEqual(["beta"]);
    expect(visibleLibrary(items, { ...base, category: 5 }, "").map((item) => item.id)).toEqual([
      "alpha",
    ]);
    expect(visibleLibrary(items, { ...base, sort: "title", direction: "asc" }, "")).toHaveLength(3);
    expect(
      visibleLibrary(items, { ...base, sort: "title", direction: "asc" }, "").map(
        (item) => item.title,
      ),
    ).toEqual(["Alpha", "Beta", "Gamma"]);
  });

  it("counts each category with the other filters applied", () => {
    const items = [
      manga({ id: "alpha", title: "Alpha", categories: [category(5, "A"), category(6, "B")] }),
      manga({ id: "beta", title: "Beta", categories: [category(5, "A")] }),
      manga({ id: "gamma", title: "Gamma" }),
    ];
    const counts = categoryCounts(items, defaultLibraryView, "");
    expect(counts.get(0)).toBe(3);
    expect(counts.get(5)).toBe(2);
    expect(counts.get(6)).toBe(1);
    const narrowed = categoryCounts(items, defaultLibraryView, "beta");
    expect(narrowed.get(0)).toBe(1);
    expect(narrowed.get(5)).toBe(1);
    expect(narrowed.get(6)).toBeUndefined();
  });

  it("removes a single selected filter value", () => {
    const filters = {
      readState: ["unread", "completed"] as const,
      status: ["ongoing"],
      sources: ["source-a", "source-b"],
      downloaded: true,
      started: true,
      bookmarked: false,
    };
    const remaining = withoutFilter(
      {
        readState: [...filters.readState],
        status: [...filters.status],
        sources: [...filters.sources],
        downloaded: true,
        started: true,
        bookmarked: false,
      },
      { id: "readState:unread", group: "readState", value: "unread", label: "Unread" },
    );
    expect(remaining.readState).toEqual(["completed"]);
    expect(remaining.status).toEqual(["ongoing"]);
    expect(remaining.sources).toEqual(["source-a", "source-b"]);
    expect(
      withoutFilter(
        { ...remaining, downloaded: true },
        { id: "flag", group: "downloaded", value: "x", label: "Downloaded" },
      ),
    ).toMatchObject({ downloaded: false, started: true });
  });

  it("starts text sorts ascending and everything else descending", () => {
    expect(naturalDirection("title")).toBe("asc");
    expect(naturalDirection("recent")).toBe("desc");
  });

  it("applies the downloaded, started, and bookmarked flags", () => {
    const items = [
      manga({ id: "a", downloadedChapters: 2 }),
      manga({
        id: "b",
        progress: {
          mangaId: "b",
          lastReadChapterId: "c",
          lastReadPage: 1,
          totalPages: 5,
          isCompleted: false,
          lastReadAt: 1,
        },
      }),
      manga({ id: "c", bookmarkedChapters: 1 }),
      manga({ id: "d" }),
    ];
    const base = defaultLibraryView;
    const flagged = { ...base.filters };
    expect(
      visibleLibrary(items, { ...base, filters: { ...flagged, downloaded: true } }, "").map(
        (item) => item.id,
      ),
    ).toEqual(["a"]);
    expect(
      visibleLibrary(items, { ...base, filters: { ...flagged, started: true } }, "").map(
        (item) => item.id,
      ),
    ).toEqual(["b"]);
    expect(
      visibleLibrary(items, { ...base, filters: { ...flagged, bookmarked: true } }, "").map(
        (item) => item.id,
      ),
    ).toEqual(["c"]);
  });

  it("sorts by chapter count, source, and status", () => {
    const items = [
      manga({ id: "a", title: "B", totalChapters: 10, sourceId: "zeta", status: "ongoing" }),
      manga({ id: "b", title: "A", totalChapters: 30, sourceId: "alpha", status: "completed" }),
      manga({ id: "c", title: "C", totalChapters: 20, sourceId: "mid", status: "hiatus" }),
    ];
    const base = defaultLibraryView;
    expect(
      visibleLibrary(items, { ...base, sort: "chapters", direction: "desc" }, "").map(
        (item) => item.id,
      ),
    ).toEqual(["b", "c", "a"]);
    expect(
      visibleLibrary(items, { ...base, sort: "source", direction: "asc" }, "").map(
        (item) => item.id,
      ),
    ).toEqual(["b", "c", "a"]);
    expect(
      visibleLibrary(items, { ...base, sort: "status", direction: "asc" }, "").map(
        (item) => item.id,
      ),
    ).toEqual(["b", "c", "a"]);
  });

  it("shuffles deterministically per seed for the random sort", () => {
    const items = [
      manga({ id: "m1" }),
      manga({ id: "m2" }),
      manga({ id: "m3" }),
      manga({ id: "m4" }),
      manga({ id: "m5" }),
    ];
    const base = { ...defaultLibraryView, sort: "random" as const, direction: "desc" as const };
    const first = visibleLibrary(items, base, "", 7).map((item) => item.id);
    expect(visibleLibrary(items, base, "", 7).map((item) => item.id)).toEqual(first);
    expect([...first].sort()).toEqual(["m1", "m2", "m3", "m4", "m5"]);
    const other = visibleLibrary(items, base, "", 42).map((item) => item.id);
    expect([...other].sort()).toEqual(["m1", "m2", "m3", "m4", "m5"]);
    expect(other).not.toEqual(first);
  });

  it("sections titles by source, status, and category", () => {
    const items = [
      manga({
        id: "a",
        sourceId: "one",
        sourceName: "One",
        status: "ongoing",
        categories: [category(5, "A")],
      }),
      manga({ id: "b", sourceId: "two", status: "completed", categories: [category(5, "A")] }),
      manga({ id: "c", sourceId: "one", sourceName: "One", status: "ongoing" }),
    ];
    const sources = groupLibrary(items, "source", [category(5, "A")]);
    expect(sources.map((group) => group.label)).toEqual(["One", "Unknown plugin two"]);
    expect(sources[0].items.map((item) => item.id)).toEqual(["a", "c"]);
    const statuses = groupLibrary(items, "status", []);
    expect(statuses.map((group) => group.label)).toEqual(["Completed", "Ongoing"]);
    const categories = groupLibrary(items, "category", [category(5, "A")]);
    expect(categories.map((group) => group.label)).toEqual(["A", "Uncategorized"]);
    expect(categories[0].items.map((item) => item.id)).toEqual(["a", "b"]);
    expect(groupLibrary(items, "none", [])).toHaveLength(1);
  });

  it("resolves grid classes from mode and column count", () => {
    expect(gridClasses({ ...defaultLibraryView, cardSize: "list" })).toContain("grid-cols-1");
    expect(gridClasses({ ...defaultLibraryView, columns: 4 })).toContain("grid-cols-4");
    expect(gridClasses(defaultLibraryView)).toContain("sm:grid-cols-4");
    expect(sourceDisplayName({ sourceId: "0198c0de", sourceName: "Dex" })).toBe("Dex");
    expect(sourceDisplayName({ sourceId: "0198c0de-aaaa" })).toBe("Unknown plugin 0198c0de");
  });
});

describe("library selection", () => {
  it("toggles a single id on and off", () => {
    const selected = toggleSelection(new Set<string>(), "a");
    expect([...selected]).toEqual(["a"]);
    expect([...toggleSelection(selected, "a")]).toEqual([]);
  });

  it("extends a range from the anchor within the visible order", () => {
    const selected = toggleRangeSelection(new Set(["a"]), ["a", "b", "c", "d"], "a", "c");
    expect([...selected].sort()).toEqual(["a", "b", "c"]);
  });

  it("falls back to a toggle when the anchor is not visible", () => {
    const selected = toggleRangeSelection(new Set<string>(), ["a", "b"], null, "b");
    expect([...selected]).toEqual(["b"]);
  });

  it("inverts and selects within the visible list only", () => {
    expect([...selectAllIds(["a", "b"])].sort()).toEqual(["a", "b"]);
    expect([...invertSelection(new Set(["a", "hidden"]), ["a", "b"])].sort()).toEqual(["b"]);
  });
});
