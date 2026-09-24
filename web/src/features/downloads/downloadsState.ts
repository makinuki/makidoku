import type { QueueItem } from "../../types";

// The queue shows the rows that can still make progress. A finished or
// canceled row leaves the queue as it happens, so the filter only guards
// against a terminal row arriving in an event.
const liveStatuses = ["PENDING", "DOWNLOADING", "PAUSED", "FAILED"];

export function isLiveRow(item: QueueItem): boolean {
  return liveStatuses.includes(item.status);
}

export type QueueSection = {
  sourceId: string;
  sourceName: string;
  rows: QueueItem[];
};

export type QueueEdge = "top" | "bottom";

// Sections follow the queue order itself: the source whose first row comes
// first in the downloader order leads the page, so a series moved to the top
// raises its source with it.
export function buildSections(items: QueueItem[]): QueueSection[] {
  const sections = new Map<string, QueueSection>();
  for (const item of items) {
    if (!isLiveRow(item)) continue;
    const section = sections.get(item.sourceId);
    if (section) section.rows.push(item);
    else
      sections.set(item.sourceId, {
        sourceId: item.sourceId,
        sourceName: item.sourceName,
        rows: [item],
      });
  }
  return [...sections.values()];
}

export function flattenSections(sections: QueueSection[]): QueueItem[] {
  return sections.flatMap((section) => section.rows);
}

export function queueRowIds(sections: QueueSection[]): number[] {
  return flattenSections(sections).map((row) => row.id);
}

// The queued chapters of one title, in queue order, for the per-series actions.
export function seriesRowIds(sections: QueueSection[], mangaId: string): number[] {
  return flattenSections(sections)
    .filter((row) => row.mangaId === mangaId)
    .map((row) => row.id);
}

export function rowIndex(sections: QueueSection[], itemId: number): number {
  const section = sections.find((entry) => entry.rows.some((row) => row.id === itemId));
  return section ? section.rows.findIndex((row) => row.id === itemId) : -1;
}

export function sectionIndex(sections: QueueSection[], sourceId: string): number {
  return sections.findIndex((section) => section.sourceId === sourceId);
}

// The index a drag operation landed on. A sortable follows the pointer while
// it is dragged, so the released entry is usually reported as its own drop
// target; the projected index it reached is the destination, and the hovered
// entry only decides when the operation carries no projected index.
export function dragDestination(sourceIndex: number | undefined, targetIndex: number): number {
  return sourceIndex ?? targetIndex;
}

// Reordering is optimistic, so rows the queue does not show are kept aside and
// re-appended: the reorder payload only covers the live rows anyway.
export function mergeLiveOrder(items: QueueItem[], ordered: QueueItem[]): QueueItem[] {
  const live = new Set(ordered.map((row) => row.id));
  return [...ordered, ...items.filter((item) => !live.has(item.id))];
}

export type SortField = "uploadedAt" | "chapterNumber";
export type SortDirection = "asc" | "desc";
export type QueueSort = { field: SortField; direction: SortDirection };

export const sortOptions: Array<QueueSort & { label: string; group: string }> = [
  { field: "uploadedAt", direction: "desc", label: "Newest", group: "By upload date" },
  { field: "uploadedAt", direction: "asc", label: "Oldest", group: "By upload date" },
  { field: "chapterNumber", direction: "asc", label: "Ascending", group: "By chapter number" },
  { field: "chapterNumber", direction: "desc", label: "Descending", group: "By chapter number" },
];

export function isSortSelected(sort: QueueSort | null, option: QueueSort): boolean {
  return sort?.field === option.field && sort.direction === option.direction;
}

// Sorting runs inside each source, like the queue screen: every group keeps its
// place on the page and only its rows are ordered by the chosen field.
export function sortSections(sections: QueueSection[], sort: QueueSort): QueueSection[] {
  const factor = sort.direction === "asc" ? 1 : -1;
  return sections.map((section) => ({
    ...section,
    rows: [...section.rows].sort((a, b) => {
      const left = sort.field === "chapterNumber" ? a.chapterNumber : a.uploadedAt;
      const right = sort.field === "chapterNumber" ? b.chapterNumber : b.uploadedAt;
      // A chapter without a number or an upload date is not a useful first
      // row, so it stays at the end of its group in both directions.
      if (left == null || right == null) {
        if (left == null && right == null) return 0;
        return left == null ? 1 : -1;
      }
      return factor * (left - right);
    }),
  }));
}

// The usual drag move: take the entry out and insert it at the target index of
// the list it came from.
function moveWithin<T>(list: T[], from: number, to: number): T[] {
  const next = [...list];
  const [moved] = next.splice(from, 1);
  next.splice(Math.max(0, Math.min(to, next.length)), 0, moved);
  return next;
}

// A row never changes group, and the groups themselves keep their order.
export function moveRow(
  sections: QueueSection[],
  itemId: number,
  targetIndex: number,
): QueueSection[] {
  return sections.map((section) => {
    const from = section.rows.findIndex((row) => row.id === itemId);
    if (from < 0) return section;
    return { ...section, rows: moveWithin(section.rows, from, targetIndex) };
  });
}

export function moveRowToEdge(
  sections: QueueSection[],
  itemId: number,
  edge: QueueEdge,
): QueueSection[] {
  return sections.map((section) => {
    const from = section.rows.findIndex((row) => row.id === itemId);
    if (from < 0) return section;
    const to = edge === "top" ? 0 : section.rows.length - 1;
    return { ...section, rows: moveWithin(section.rows, from, to) };
  });
}

// A header drag moves its whole source block; the other groups keep their rows.
export function moveSection(
  sections: QueueSection[],
  sourceId: string,
  targetIndex: number,
): QueueSection[] {
  const from = sections.findIndex((section) => section.sourceId === sourceId);
  if (from < 0) return sections;
  return moveWithin(sections, from, targetIndex);
}

// Series moves work on the whole queue rather than on one group: moving a
// series to the top lifts every queued chapter of that title, which also
// raises the source it belongs to.
export function moveSeries(
  sections: QueueSection[],
  mangaId: string,
  edge: QueueEdge,
): QueueSection[] {
  const rows = flattenSections(sections);
  const series = rows.filter((row) => row.mangaId === mangaId);
  if (series.length === 0) return sections;
  const rest = rows.filter((row) => row.mangaId !== mangaId);
  return buildSections(edge === "top" ? [...series, ...rest] : [...rest, ...series]);
}

// Sortable ids share a single id space: rows are addressed by their queue item
// id while a source header carries a prefixed string.
export function sectionSortableId(sourceId: string): string {
  return `source:${sourceId}`;
}

export function sectionIdOf(id: unknown): string | null {
  const prefix = "source:";
  return typeof id === "string" && id.startsWith(prefix) ? id.slice(prefix.length) : null;
}

const statusLabels: Record<string, string> = {
  PENDING: "Queued",
  DOWNLOADING: "Downloading",
  PAUSED: "Paused",
  FAILED: "Failed",
};

export function statusLabel(item: QueueItem): string {
  return statusLabels[item.status] ?? item.status.toLowerCase();
}

// Rows label a chapter the way the rest of the app does: the chapter's own
// title when the source has one, else its number, else the special case.
export function chapterLabel(item: QueueItem): string {
  if (item.chapterTitle) return item.chapterTitle;
  if (item.chapterNumber != null) return `Chapter ${item.chapterNumber}`;
  return "Special chapter";
}
