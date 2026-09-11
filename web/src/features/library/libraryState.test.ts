import { describe, expect, it } from "vite-plus/test";
import type { RuntimeSetting } from "../../api";
import type { Category, LibraryManga } from "../../types";
import {
  categoryCounts,
  defaultLibraryView,
  libraryViewFromSettings,
  libraryViewSettings,
  naturalDirection,
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
      unreadBadge: false,
      progressBar: false,
      continueButton: false,
      category: 7,
      filters: {
        readState: ["in_progress"],
        status: ["completed", "hiatus"],
        sources: ["source-a"],
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
    };
    const remaining = withoutFilter(
      {
        readState: [...filters.readState],
        status: [...filters.status],
        sources: [...filters.sources],
      },
      { id: "readState:unread", group: "readState", value: "unread", label: "Unread" },
    );
    expect(remaining.readState).toEqual(["completed"]);
    expect(remaining.status).toEqual(["ongoing"]);
    expect(remaining.sources).toEqual(["source-a", "source-b"]);
  });

  it("starts text sorts ascending and everything else descending", () => {
    expect(naturalDirection("title")).toBe("asc");
    expect(naturalDirection("recent")).toBe("desc");
  });
});
