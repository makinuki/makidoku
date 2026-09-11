import type { RuntimeSetting } from "../../api";
import type { LibraryManga } from "../../types";

export type LibrarySort = "recent" | "title" | "added" | "last_read" | "unread";
export type SortDirection = "asc" | "desc";
export type CardSize = "small" | "medium" | "large";
export type ReadState = "unread" | "in_progress" | "completed";

export type LibraryFilters = {
  readState: ReadState[];
  status: string[];
  sources: string[];
};

export type LibraryView = {
  sort: LibrarySort;
  direction: SortDirection;
  cardSize: CardSize;
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
  unreadBadge: "library.view.unread_badge",
  progressBar: "library.view.progress_bar",
  continueButton: "library.view.continue_button",
  category: "library.view.category",
  readState: "library.view.filter_read_state",
  status: "library.view.filter_status",
  sources: "library.view.filter_sources",
} as const;

export const librarySorts: LibrarySort[] = ["recent", "title", "added", "last_read", "unread"];
export const cardSizes: CardSize[] = ["small", "medium", "large"];
export const readStates: ReadState[] = ["unread", "in_progress", "completed"];
export const statuses = ["ongoing", "completed", "hiatus", "cancelled", "unknown"];

export const sortLabels: Record<LibrarySort, string> = {
  recent: "Recently updated",
  title: "Title",
  added: "Date added",
  last_read: "Last read",
  unread: "Unread chapters",
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
};

// Card sizes map to fixed class strings so the Tailwind compiler can see them.
export const cardSizeClasses: Record<CardSize, string> = {
  small: "grid grid-cols-3 gap-3 sm:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8",
  medium: "grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6",
  large: "grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5",
};

export const emptyFilters: LibraryFilters = { readState: [], status: [], sources: [] };

export const defaultLibraryView: LibraryView = {
  sort: "recent",
  direction: "desc",
  cardSize: "medium",
  unreadBadge: true,
  progressBar: true,
  continueButton: true,
  category: 0,
  filters: emptyFilters,
};

// naturalDirection is the direction a sort mode starts with when it is picked:
// names read A to Z, everything else reads newest or largest first.
export function naturalDirection(sort: LibrarySort): SortDirection {
  return sort === "title" ? "asc" : "desc";
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
  return {
    sort: pick(viewSettingKeys.sort, librarySorts, "recent"),
    direction: pick(viewSettingKeys.direction, ["asc", "desc"], "desc"),
    cardSize: pick(viewSettingKeys.cardSize, cardSizes, "medium"),
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
    [viewSettingKeys.unreadBadge, view.unreadBadge],
    [viewSettingKeys.progressBar, view.progressBar],
    [viewSettingKeys.continueButton, view.continueButton],
    [viewSettingKeys.category, view.category],
    [viewSettingKeys.readState, view.filters.readState.join(",")],
    [viewSettingKeys.status, view.filters.status.join(",")],
    [viewSettingKeys.sources, view.filters.sources.join(",")],
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
  const { readState, status, sources } = view.filters;
  if (readState.length > 0 && !readState.includes(readStateOf(item))) return false;
  if (status.length > 0 && !status.includes((item.status || "unknown").toLowerCase())) return false;
  if (sources.length > 0 && !sources.includes(item.sourceId)) return false;
  return true;
}

function compare(a: LibraryManga, b: LibraryManga, sort: LibrarySort): number {
  switch (sort) {
    case "title":
      return a.title.localeCompare(b.title);
    case "added":
      return a.createdAt - b.createdAt;
    case "last_read":
      return (a.progress?.lastReadAt ?? 0) - (b.progress?.lastReadAt ?? 0);
    case "unread":
      return a.unreadChapters - b.unreadChapters;
    default:
      return a.updatedAt - b.updatedAt;
  }
}

export function sortLibrary(
  items: LibraryManga[],
  sort: LibrarySort,
  direction: SortDirection,
): LibraryManga[] {
  const factor = direction === "asc" ? 1 : -1;
  return [...items].sort((a, b) => {
    const result = compare(a, b, sort) * factor;
    return result !== 0 ? result : a.title.localeCompare(b.title);
  });
}

export function visibleLibrary(
  items: LibraryManga[],
  view: LibraryView,
  query: string,
): LibraryManga[] {
  const matches = items.filter(
    (item) =>
      libraryMatches(item, view, query) &&
      (view.category === 0 || item.categories.some((category) => category.id === view.category)),
  );
  return sortLibrary(matches, view.sort, view.direction);
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
    if (!sources.has(item.sourceId))
      sources.set(item.sourceId, item.sourceName || "Unknown plugin");
  }
  return [...sources]
    .map(([id, name]) => ({ id, name }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function activeFilterCount(view: LibraryView): number {
  return view.filters.readState.length + view.filters.status.length + view.filters.sources.length;
}

export function filterChips(view: LibraryView, sourceName: (id: string) => string): FilterChip[] {
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
    default:
      return { ...filters, sources: filters.sources.filter((value) => value !== chip.value) };
  }
}
