import type { RuntimeSetting } from "../../api";
import type { Category, LibraryManga } from "../../types";

export type LibrarySort =
  | "recent"
  | "title"
  | "added"
  | "last_read"
  | "unread"
  | "chapters"
  | "source"
  | "status"
  | "random";
export type SortDirection = "asc" | "desc";
export type CardSize = "small" | "medium" | "large" | "list" | "cover-only";
export type GroupBy = "none" | "category" | "source" | "status";
export type ReadState = "unread" | "in_progress" | "completed";

export type LibraryFilters = {
  readState: ReadState[];
  status: string[];
  sources: string[];
  downloaded: boolean;
  started: boolean;
  bookmarked: boolean;
};

export type LibraryView = {
  sort: LibrarySort;
  direction: SortDirection;
  cardSize: CardSize;
  columns: number;
  groupBy: GroupBy;
  unreadBadge: boolean;
  progressBar: boolean;
  continueButton: boolean;
  category: number;
  filters: LibraryFilters;
};

export type LibrarySourceOption = { id: string; name: string };

export type FilterChip = {
  id: string;
  group: keyof LibraryFilters;
  value: string;
  label: string;
};

// The view state travels through the settings service so it is validated by
// the daemon and survives a browser change.
export const viewSettingKeys = {
  sort: "library.view.sort",
  direction: "library.view.sort_direction",
  cardSize: "library.view.card_size",
  columns: "library.view.columns",
  groupBy: "library.view.group_by",
  unreadBadge: "library.view.unread_badge",
  progressBar: "library.view.progress_bar",
  continueButton: "library.view.continue_button",
  category: "library.view.category",
  readState: "library.view.filter_read_state",
  status: "library.view.filter_status",
  sources: "library.view.filter_sources",
  downloaded: "library.view.filter_downloaded",
  started: "library.view.filter_started",
  bookmarked: "library.view.filter_bookmarked",
} as const;

export const librarySorts: LibrarySort[] = [
  "recent",
  "title",
  "added",
  "last_read",
  "unread",
  "chapters",
  "source",
  "status",
  "random",
];
export const cardSizes: CardSize[] = ["small", "medium", "large", "list", "cover-only"];
export const groupBys: GroupBy[] = ["none", "category", "source", "status"];
export const readStates: ReadState[] = ["unread", "in_progress", "completed"];
export const statuses = ["ongoing", "completed", "hiatus", "cancelled", "unknown"];

export const sortLabels: Record<LibrarySort, string> = {
  recent: "Recently updated",
  title: "Title",
  added: "Date added",
  last_read: "Last read",
  unread: "Unread chapters",
  chapters: "Chapter count",
  source: "Source",
  status: "Status",
  random: "Random",
};

export const readStateLabels: Record<ReadState, string> = {
  unread: "Unread",
  in_progress: "In progress",
  completed: "Completed",
};

export const statusLabels: Record<string, string> = {
  ongoing: "Ongoing",
  completed: "Completed",
  hiatus: "Hiatus",
  cancelled: "Cancelled",
  unknown: "Unknown",
};

export const cardSizeLabels: Record<CardSize, string> = {
  small: "Small",
  medium: "Medium",
  large: "Large",
  list: "List",
  "cover-only": "Cover only",
};

export const groupByLabels: Record<GroupBy, string> = {
  none: "No grouping",
  category: "Categories",
  source: "Sources",
  status: "Status",
};

// Card sizes map to fixed class strings so the Tailwind compiler can see them.
export const cardSizeClasses: Record<Exclude<CardSize, "list">, string> = {
  small: "grid grid-cols-3 gap-3 sm:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8",
  medium: "grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6",
  large: "grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5",
  "cover-only": "grid grid-cols-3 gap-3 sm:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8",
};

// Explicit column counts map to fixed classes for the same reason. Zero means
// the grid follows the card size.
export const columnClasses: Record<number, string> = {
  1: "grid grid-cols-1 gap-3",
  2: "grid grid-cols-2 gap-3",
  3: "grid grid-cols-3 gap-3",
  4: "grid grid-cols-4 gap-3",
  5: "grid grid-cols-5 gap-3",
  6: "grid grid-cols-6 gap-3",
  7: "grid grid-cols-7 gap-3",
  8: "grid grid-cols-8 gap-3",
  9: "grid grid-cols-9 gap-3",
  10: "grid grid-cols-10 gap-3",
};

export function gridClasses(view: LibraryView): string {
  if (view.cardSize === "list") return "grid grid-cols-1 gap-2";
  if (view.columns >= 1 && view.columns <= 10) return columnClasses[view.columns];
  return cardSizeClasses[view.cardSize];
}

export const emptyFilters: LibraryFilters = {
  readState: [],
  status: [],
  sources: [],
  downloaded: false,
  started: false,
  bookmarked: false,
};

export const defaultLibraryView: LibraryView = {
  sort: "recent",
  direction: "desc",
  cardSize: "medium",
  columns: 0,
  groupBy: "none",
  unreadBadge: true,
  progressBar: true,
  continueButton: true,
  category: 0,
  filters: emptyFilters,
};

// naturalDirection is the direction a sort mode starts with when it is picked:
// names read A to Z, everything else reads newest or largest first. Random
// carries no direction; the seed decides the order.
export function naturalDirection(sort: LibrarySort): SortDirection {
  return sort === "title" || sort === "source" || sort === "status" ? "asc" : "desc";
}

function splitList(value: unknown): string[] {
  if (typeof value !== "string") return [];
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function isOneOf<T extends string>(value: string, allowed: T[]): value is T {
  return (allowed as string[]).includes(value);
}

// libraryViewFromSettings reads stored view state, dropping values this build
// does not recognize so a stale or hand-edited row cannot break the page.
export function libraryViewFromSettings(settings: RuntimeSetting[]): LibraryView {
  const values = new Map(settings.map((setting) => [setting.key, setting.value]));
  const pick = <T extends string>(key: string, allowed: T[], fallback: T): T => {
    const value = values.get(key);
    return typeof value === "string" && isOneOf(value, allowed) ? value : fallback;
  };
  const flag = (key: string, fallback: boolean): boolean => {
    const value = values.get(key);
    return typeof value === "boolean" ? value : fallback;
  };
  const category = values.get(viewSettingKeys.category);
  const columns = values.get(viewSettingKeys.columns);
  return {
    sort: pick(viewSettingKeys.sort, librarySorts, "recent"),
    direction: pick(viewSettingKeys.direction, ["asc", "desc"], "desc"),
    cardSize: pick(viewSettingKeys.cardSize, cardSizes, "medium"),
    columns: typeof columns === "number" && columns >= 0 && columns <= 10 ? columns : 0,
    groupBy: pick(viewSettingKeys.groupBy, groupBys, "none"),
    unreadBadge: flag(viewSettingKeys.unreadBadge, true),
    progressBar: flag(viewSettingKeys.progressBar, true),
    continueButton: flag(viewSettingKeys.continueButton, true),
    category: typeof category === "number" && category > 0 ? category : 0,
    filters: {
      readState: splitList(values.get(viewSettingKeys.readState)).filter((value) =>
        isOneOf(value, readStates),
      ),
      status: splitList(values.get(viewSettingKeys.status)).filter((value) =>
        isOneOf(value, statuses),
      ),
      sources: splitList(values.get(viewSettingKeys.sources)),
      downloaded: flag(viewSettingKeys.downloaded, false),
      started: flag(viewSettingKeys.started, false),
      bookmarked: flag(viewSettingKeys.bookmarked, false),
    },
  };
}

// libraryViewSettings serializes the whole view; callers persist only the
// pairs whose value changed.
export function libraryViewSettings(view: LibraryView): Array<[string, unknown]> {
  return [
    [viewSettingKeys.sort, view.sort],
    [viewSettingKeys.direction, view.direction],
    [viewSettingKeys.cardSize, view.cardSize],
    [viewSettingKeys.columns, view.columns],
    [viewSettingKeys.groupBy, view.groupBy],
    [viewSettingKeys.unreadBadge, view.unreadBadge],
    [viewSettingKeys.progressBar, view.progressBar],
    [viewSettingKeys.continueButton, view.continueButton],
    [viewSettingKeys.category, view.category],
    [viewSettingKeys.readState, view.filters.readState.join(",")],
    [viewSettingKeys.status, view.filters.status.join(",")],
    [viewSettingKeys.sources, view.filters.sources.join(",")],
    [viewSettingKeys.downloaded, view.filters.downloaded],
    [viewSettingKeys.started, view.filters.started],
    [viewSettingKeys.bookmarked, view.filters.bookmarked],
  ];
}

export function mergeLibraryView(view: LibraryView, patch: Partial<LibraryView>): LibraryView {
  return { ...view, ...patch, filters: { ...view.filters, ...patch.filters } };
}

function altTitles(item: LibraryManga): string[] {
  if (!item.altTitles) return [];
  try {
    const parsed: unknown = JSON.parse(item.altTitles);
    return Array.isArray(parsed)
      ? parsed.filter((title): title is string => typeof title === "string")
      : [];
  } catch {
    return [];
  }
}

function matchesQuery(item: LibraryManga, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  if (item.title.toLowerCase().includes(needle)) return true;
  return altTitles(item).some((title) => title.toLowerCase().includes(needle));
}

export function readStateOf(item: LibraryManga): ReadState {
  if (item.progress?.isCompleted) return "completed";
  return item.progress ? "in_progress" : "unread";
}

// libraryMatches applies the text query and every filter except the selected
// category, so the same predicate can count the titles of each category.
export function libraryMatches(item: LibraryManga, view: LibraryView, query: string): boolean {
  if (!matchesQuery(item, query)) return false;
  const { readState, status, sources, downloaded, started, bookmarked } = view.filters;
  if (readState.length > 0 && !readState.includes(readStateOf(item))) return false;
  if (status.length > 0 && !status.includes((item.status || "unknown").toLowerCase())) return false;
  if (sources.length > 0 && !sources.includes(item.sourceId)) return false;
  if (downloaded && (item.downloadedChapters ?? 0) <= 0) return false;
  if (started && !item.progress) return false;
  if (bookmarked && (item.bookmarkedChapters ?? 0) <= 0) return false;
  return true;
}

// hashOrder spreads ids across the integer range for the random sort. The
// seed re-rolls the order without storing a permutation.
function hashOrder(id: string, seed: number): number {
  let hash = 0x811c9dc5 ^ (seed | 0);
  const text = `${seed}:${id}`;
  for (let index = 0; index < text.length; index += 1) {
    hash = Math.imul(hash ^ text.charCodeAt(index), 0x01000193);
  }
  return hash | 0;
}

function compare(a: LibraryManga, b: LibraryManga, sort: LibrarySort, seed: number): number {
  switch (sort) {
    case "title":
      return a.title.localeCompare(b.title);
    case "added":
      return a.createdAt - b.createdAt;
    case "last_read":
      return (a.progress?.lastReadAt ?? 0) - (b.progress?.lastReadAt ?? 0);
    case "unread":
      return a.unreadChapters - b.unreadChapters;
    case "chapters":
      return (a.totalChapters ?? 0) - (b.totalChapters ?? 0);
    case "source":
      return (a.sourceName || a.sourceId).localeCompare(b.sourceName || b.sourceId);
    case "status":
      return (a.status || "").localeCompare(b.status || "");
    case "random":
      return hashOrder(a.id, seed) - hashOrder(b.id, seed);
    default:
      return a.updatedAt - b.updatedAt;
  }
}

export function sortLibrary(
  items: LibraryManga[],
  sort: LibrarySort,
  direction: SortDirection,
  seed = 0,
): LibraryManga[] {
  const factor = direction === "asc" ? 1 : -1;
  return [...items].sort((a, b) => {
    const result = compare(a, b, sort, seed) * factor;
    return result !== 0 ? result : a.title.localeCompare(b.title);
  });
}

export function visibleLibrary(
  items: LibraryManga[],
  view: LibraryView,
  query: string,
  seed = 0,
): LibraryManga[] {
  const matches = items.filter(
    (item) =>
      libraryMatches(item, view, query) &&
      (view.category === 0 || item.categories.some((category) => category.id === view.category)),
  );
  return sortLibrary(matches, view.sort, view.direction, seed);
}

export type LibraryGroup = { key: string; label: string; items: LibraryManga[] };

// groupLibrary sections the visible titles for the grouped display. Category
// grouping repeats a title under each of its categories; an empty groupBy
// returns the whole list as a single unlabeled section.
export function groupLibrary(
  items: LibraryManga[],
  groupBy: GroupBy,
  categories: Category[],
): LibraryGroup[] {
  if (groupBy === "source") {
    const groups = new Map<string, LibraryManga[]>();
    for (const item of items) {
      const group = groups.get(item.sourceId) ?? [];
      group.push(item);
      groups.set(item.sourceId, group);
    }
    return [...groups.entries()]
      .map(([id, grouped]) => ({
        key: `source:${id}`,
        label: sourceDisplayName({ sourceId: id, sourceName: grouped[0]?.sourceName }),
        items: grouped,
      }))
      .sort((a, b) => a.label.localeCompare(b.label));
  }
  if (groupBy === "status") {
    const groups = new Map<string, LibraryManga[]>();
    for (const item of items) {
      const status = (item.status || "unknown").toLowerCase();
      const group = groups.get(status) ?? [];
      group.push(item);
      groups.set(status, group);
    }
    return [...groups.entries()]
      .map(([status, grouped]) => ({
        key: `status:${status}`,
        label: statusLabels[status] ?? status,
        items: grouped,
      }))
      .sort((a, b) => a.label.localeCompare(b.label));
  }
  if (groupBy === "category") {
    const groups: LibraryGroup[] = categories.map((category) => ({
      key: `category:${category.id}`,
      label: category.name,
      items: items.filter((item) =>
        item.categories.some((membership) => membership.id === category.id),
      ),
    }));
    const homeless = items.filter((item) => item.categories.length === 0);
    if (homeless.length > 0)
      groups.push({ key: "category:0", label: "Uncategorized", items: homeless });
    return groups.filter((group) => group.items.length > 0);
  }
  return [{ key: "all", label: "", items }];
}

// categoryCounts counts the titles each category would show, including the
// uncategorized total under id 0.
export function categoryCounts(
  items: LibraryManga[],
  view: LibraryView,
  query: string,
): Map<number, number> {
  const counts = new Map<number, number>();
  const bump = (id: number) => counts.set(id, (counts.get(id) ?? 0) + 1);
  for (const item of items) {
    if (!libraryMatches(item, view, query)) continue;
    bump(0);
    for (const category of item.categories) bump(category.id);
  }
  return counts;
}

export function librarySources(items: LibraryManga[]): LibrarySourceOption[] {
  const sources = new Map<string, string>();
  for (const item of items) {
    if (!sources.has(item.sourceId)) sources.set(item.sourceId, sourceDisplayName(item));
  }
  return [...sources]
    .map(([id, name]) => ({ id, name }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

// sourceDisplayName keeps one filter pill per plugin even when the plugin is
// gone: the short id tells apart repeated fallback labels.
export function sourceDisplayName(item: { sourceId: string; sourceName?: string }): string {
  if (item.sourceName) return item.sourceName;
  return `Unknown plugin ${item.sourceId.slice(0, 8)}`;
}

export function activeFilterCount(view: LibraryView): number {
  const flags =
    (view.filters.downloaded ? 1 : 0) +
    (view.filters.started ? 1 : 0) +
    (view.filters.bookmarked ? 1 : 0);
  return (
    view.filters.readState.length + view.filters.status.length + view.filters.sources.length + flags
  );
}

const flagLabels = {
  downloaded: "Downloaded",
  started: "Started",
  bookmarked: "Bookmarked",
} as const;

export function filterChips(view: LibraryView, sourceName: (id: string) => string): FilterChip[] {
  const flags = (Object.keys(flagLabels) as Array<keyof typeof flagLabels>)
    .filter((flag) => view.filters[flag])
    .map((flag) => ({
      id: `flag:${flag}`,
      group: flag as keyof LibraryFilters,
      value: flag,
      label: flagLabels[flag],
    }));
  return [
    ...view.filters.readState.map((value) => ({
      id: `readState:${value}`,
      group: "readState" as const,
      value,
      label: readStateLabels[value],
    })),
    ...view.filters.status.map((value) => ({
      id: `status:${value}`,
      group: "status" as const,
      value,
      label: statusLabels[value] ?? value,
    })),
    ...view.filters.sources.map((value) => ({
      id: `sources:${value}`,
      group: "sources" as const,
      value,
      label: sourceName(value),
    })),
    ...flags,
  ];
}

// withoutFilter drops a single selected value from the group its chip belongs
// to.
export function withoutFilter(filters: LibraryFilters, chip: FilterChip): LibraryFilters {
  switch (chip.group) {
    case "readState":
      return { ...filters, readState: filters.readState.filter((value) => value !== chip.value) };
    case "status":
      return { ...filters, status: filters.status.filter((value) => value !== chip.value) };
    case "downloaded":
      return { ...filters, downloaded: false };
    case "started":
      return { ...filters, started: false };
    case "bookmarked":
      return { ...filters, bookmarked: false };
    default:
      return { ...filters, sources: filters.sources.filter((value) => value !== chip.value) };
  }
}

// Library selection helpers. Selection is a set of manga ids scoped to the
// visible (filtered) list; a range selection extends from the last anchor
// within that visible order.

// toggleSelection adds or removes one id.
export function toggleSelection(selection: Set<string>, id: string): Set<string> {
  const next = new Set(selection);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
}

// toggleRangeSelection adds every visible id between the anchor and the target,
// inclusive. When either end is not in the visible order it falls back to a
// plain toggle of the target.
export function toggleRangeSelection(
  selection: Set<string>,
  orderedIds: string[],
  anchorId: string | null,
  targetId: string,
): Set<string> {
  const next = new Set(selection);
  const from = anchorId ? orderedIds.indexOf(anchorId) : -1;
  const to = orderedIds.indexOf(targetId);
  if (from < 0 || to < 0) {
    if (next.has(targetId)) next.delete(targetId);
    else next.add(targetId);
    return next;
  }
  const start = Math.min(from, to);
  const end = Math.max(from, to);
  for (let index = start; index <= end; index += 1) next.add(orderedIds[index]);
  return next;
}

// selectAllIds selects every visible id.
export function selectAllIds(visibleIds: string[]): Set<string> {
  return new Set(visibleIds);
}

// invertSelection selects the visible ids that are not currently selected.
// Ids selected outside the visible list are dropped.
export function invertSelection(selection: Set<string>, visibleIds: string[]): Set<string> {
  const next = new Set<string>();
  for (const id of visibleIds) {
    if (!selection.has(id)) next.add(id);
  }
  return next;
}
