import { describe, expect, it } from "vite-plus/test";
import type { QueueItem } from "../../types";
import {
  buildSections,
  chapterLabel,
  dragDestination,
  flattenSections,
  isLiveRow,
  mergeLiveOrder,
  moveRow,
  moveRowToEdge,
  moveSection,
  moveSeries,
  queueRowIds,
  sectionIdOf,
  sectionIndex,
  sectionSortableId,
  seriesRowIds,
  sortSections,
  statusLabel,
  type QueueSection,
} from "./downloadsState";

function row(overrides: Partial<QueueItem> & { id: number }): QueueItem {
  return {
    chapterId: `chapter-${overrides.id}`,
    status: "PENDING",
    progress: 0,
    totalPages: 10,
    downloadedPages: 0,
    mangaId: "m1",
    mangaTitle: "Yosuga no Sora",
    sourceId: "s1",
    sourceName: "MangaDex",
    chapterNumber: 1,
    position: overrides.id,
    ...overrides,
  };
}

const queue: QueueItem[] = [
  row({ id: 1, chapterNumber: 1 }),
  row({ id: 2, chapterNumber: 2 }),
  row({
    id: 3,
    chapterNumber: 1,
    mangaId: "m2",
    mangaTitle: "Bokutachi wa Hanshoku wo Yameta",
    sourceId: "s2",
    sourceName: "Asura Scans",
  }),
];

function ids(sections: QueueSection[]): number[] {
  return flattenSections(sections).map((item) => item.id);
}

describe("buildSections", () => {
  it("groups rows by source and keeps the queue order", () => {
    const sections = buildSections(queue);
    expect(sections.map((section) => section.sourceName)).toEqual(["MangaDex", "Asura Scans"]);
    expect(sections.map((section) => ids([section]))).toEqual([[1, 2], [3]]);
  });

  it("leads with the source of the first queued row", () => {
    const sections = buildSections([queue[2], queue[0], queue[1]]);
    expect(sections.map((section) => section.sourceId)).toEqual(["s2", "s1"]);
  });

  it("drops terminal rows", () => {
    const sections = buildSections([
      row({ id: 1 }),
      row({ id: 2, status: "COMPLETED" }),
      row({ id: 3, status: "CANCELED" }),
      row({ id: 4, status: "FAILED" }),
    ]);
    expect(ids(sections)).toEqual([1, 4]);
    expect(isLiveRow(row({ id: 9, status: "CANCELED" }))).toBe(false);
  });
});

describe("sortSections", () => {
  const uploaded = [
    row({ id: 1, uploadedAt: 300 }),
    row({ id: 2, uploadedAt: 100 }),
    row({ id: 3, uploadedAt: 200 }),
  ];

  it("sorts by upload date in both directions", () => {
    const sections = buildSections(uploaded);
    expect(ids(sortSections(sections, { field: "uploadedAt", direction: "desc" }))).toEqual([
      1, 3, 2,
    ]);
    expect(ids(sortSections(sections, { field: "uploadedAt", direction: "asc" }))).toEqual([
      2, 3, 1,
    ]);
  });

  it("sorts by chapter number in both directions", () => {
    const sections = buildSections(queue);
    expect(ids(sortSections(sections, { field: "chapterNumber", direction: "asc" }))).toEqual([
      1, 2, 3,
    ]);
    expect(ids(sortSections(sections, { field: "chapterNumber", direction: "desc" }))).toEqual([
      2, 1, 3,
    ]);
  });

  it("keeps rows without the sorted value at the end of their group", () => {
    const sections = buildSections([
      row({ id: 1, chapterNumber: 2 }),
      row({ id: 2, chapterNumber: undefined, chapterTitle: "Special" }),
      row({ id: 3, chapterNumber: 1 }),
    ]);
    expect(ids(sortSections(sections, { field: "chapterNumber", direction: "asc" }))).toEqual([
      3, 1, 2,
    ]);
    expect(ids(sortSections(sections, { field: "chapterNumber", direction: "desc" }))).toEqual([
      1, 3, 2,
    ]);
  });

  it("leaves the groups where they are", () => {
    const sections = buildSections([...queue, row({ id: 4, uploadedAt: 500 })]);
    const sorted = sortSections(sections, { field: "uploadedAt", direction: "desc" });
    expect(sorted.map((section) => section.sourceId)).toEqual(["s1", "s2"]);
    expect(ids(sorted)).toEqual([4, 1, 2, 3]);
  });
});

describe("moves", () => {
  it("moves a row inside its own group", () => {
    const sections = buildSections(queue);
    expect(ids(moveRow(sections, 1, 1))).toEqual([2, 1, 3]);
    expect(ids(moveRow(sections, 1, 5))).toEqual([2, 1, 3]);
    expect(ids(moveRow(sections, 3, 0))).toEqual([1, 2, 3]);
  });

  it("moves a row to either edge of its group", () => {
    const sections = buildSections(queue);
    expect(ids(moveRowToEdge(sections, 2, "top"))).toEqual([2, 1, 3]);
    expect(ids(moveRowToEdge(sections, 1, "bottom"))).toEqual([2, 1, 3]);
  });

  it("moves a whole source block", () => {
    const sections = buildSections(queue);
    const moved = moveSection(sections, "s2", 0);
    expect(ids(moved)).toEqual([3, 1, 2]);
    expect(moved.map((section) => section.sourceId)).toEqual(["s2", "s1"]);
  });

  it("moves every queued chapter of a series, raising its source", () => {
    const sections = buildSections([
      row({ id: 1, mangaId: "m1" }),
      row({ id: 2, mangaId: "m2", sourceId: "s2", sourceName: "Asura Scans" }),
      row({ id: 3, mangaId: "m2", sourceId: "s2", sourceName: "Asura Scans" }),
    ]);
    const moved = moveSeries(sections, "m2", "top");
    expect(ids(moved)).toEqual([2, 3, 1]);
    expect(moved.map((section) => section.sourceId)).toEqual(["s2", "s1"]);
    expect(ids(moveSeries(moved, "m2", "bottom"))).toEqual([1, 2, 3]);
  });

  it("ignores rows and series that are not queued", () => {
    const sections = buildSections(queue);
    expect(ids(moveRow(sections, 99, 0))).toEqual([1, 2, 3]);
    expect(ids(moveRowToEdge(sections, 99, "top"))).toEqual([1, 2, 3]);
    expect(ids(moveSection(sections, "missing", 0))).toEqual([1, 2, 3]);
    expect(ids(moveSeries(sections, "missing", "top"))).toEqual([1, 2, 3]);
  });

  it("reads the destination index of a drop", () => {
    const sections = buildSections(queue);
    expect(sectionIndex(sections, "s2")).toBe(1);
    expect(sectionIndex(sections, "missing")).toBe(-1);
    // A release reports the dragged entry as its own target, so the index the
    // entry reached wins over the hovered entry.
    expect(dragDestination(2, 0)).toBe(2);
    expect(dragDestination(undefined, 0)).toBe(0);
  });
});

describe("queue payload helpers", () => {
  it("lists the ids the reorder request carries", () => {
    const sections = buildSections(queue);
    expect(queueRowIds(sections)).toEqual([1, 2, 3]);
    expect(seriesRowIds(buildSections([...queue, row({ id: 4 })]), "m1")).toEqual([1, 2, 4]);
  });

  it("keeps rows the queue does not show out of the optimistic order", () => {
    const items = [row({ id: 1 }), row({ id: 2, status: "COMPLETED" }), row({ id: 3 })];
    const ordered = mergeLiveOrder(items, [items[2], items[0]]);
    expect(ordered.map((item) => item.id)).toEqual([3, 1, 2]);
  });

  it("addresses source headers in the shared sortable id space", () => {
    expect(sectionSortableId("s1")).toBe("source:s1");
    expect(sectionIdOf(sectionSortableId("s1"))).toBe("s1");
    expect(sectionIdOf(3)).toBe(null);
    expect(sectionIdOf("row:s1")).toBe(null);
  });
});

describe("labels", () => {
  it("labels a chapter the way the queue shows it", () => {
    expect(chapterLabel(row({ id: 1, chapterTitle: "Chapter 4.5" }))).toBe("Chapter 4.5");
    expect(chapterLabel(row({ id: 2, chapterNumber: 7, chapterTitle: undefined }))).toBe(
      "Chapter 7",
    );
    expect(chapterLabel(row({ id: 3, chapterNumber: undefined, chapterTitle: undefined }))).toBe(
      "Special chapter",
    );
  });

  it("names every status the queue shows", () => {
    expect(statusLabel(row({ id: 1, status: "PENDING" }))).toBe("Queued");
    expect(statusLabel(row({ id: 2, status: "DOWNLOADING" }))).toBe("Downloading");
    expect(statusLabel(row({ id: 3, status: "PAUSED" }))).toBe("Paused");
    expect(statusLabel(row({ id: 4, status: "FAILED" }))).toBe("Failed");
  });
});
