import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowDown, ArrowUp, Play, Search, SlidersHorizontal, X } from "lucide-react";
import { api } from "../../api";
import type { Category, LibraryManga } from "../../types";
import { CoverImg } from "../../components/CoverImg";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { LibraryFiltersModal } from "./LibraryFiltersModal";
import {
  activeFilterCount,
  cardSizeClasses,
  categoryCounts,
  defaultLibraryView,
  emptyFilters,
  filterChips,
  librarySorts,
  librarySources,
  libraryViewFromSettings,
  libraryViewSettings,
  mergeLibraryView,
  naturalDirection,
  sortLabels,
  visibleLibrary,
  withoutFilter,
  type LibrarySort,
  type LibraryView,
} from "./libraryState";

export function LibraryPage() {
  const [items, setItems] = useState<LibraryManga[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [view, setView] = useState<LibraryView>(defaultLibraryView);
  const [query, setQuery] = useState("");
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  // persisted mirrors the view last written to the settings service, so a
  // change writes only the keys that actually differ.
  const persisted = useRef<LibraryView>(defaultLibraryView);
  const loaded = useRef(false);

  useEffect(() => {
    let active = true;
    setLoading(true);
    Promise.all([api.library(), api.categories(), api.settings()])
      .then(([library, categoryList, settings]) => {
        if (!active) return;
        const stored = libraryViewFromSettings(settings);
        setItems(library);
        setCategories(categoryList);
        persisted.current = stored;
        setView(stored);
        loaded.current = true;
      })
      .catch((e) => {
        if (active) setError(e instanceof Error ? e.message : "Could not load the library");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!loaded.current) return;
    const before = new Map(libraryViewSettings(persisted.current));
    persisted.current = view;
    for (const [key, value] of libraryViewSettings(view)) {
      if (before.get(key) === value) continue;
      void api.updateSetting(key, value).catch(() => setError("Could not save the library view."));
    }
  }, [view]);

  const updateView = useCallback((patch: Partial<LibraryView>) => {
    setView((current) => mergeLibraryView(current, patch));
  }, []);
  const visible = useMemo(() => visibleLibrary(items, view, query), [items, view, query]);
  const counts = useMemo(() => categoryCounts(items, view, query), [items, view, query]);
  const sources = useMemo(() => librarySources(items), [items]);
  const chips = filterChips(view, (id) => sources.find((source) => source.id === id)?.name ?? id);
  const activeCount = activeFilterCount(view);

  return (
    <div className="mx-auto max-w-7xl p-5 sm:p-8">
      <PageHeader title="Library">
        <span className="text-sm text-zinc-500">
          {visible.length} of {items.length} {items.length === 1 ? "title" : "titles"}
        </span>
      </PageHeader>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <label className="flex min-w-56 flex-1 items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2">
          <Search size={16} className="shrink-0 text-zinc-500" />
          <input
            id="library-search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search your library"
            aria-label="Search your library"
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-zinc-500"
          />
          {query && (
            <button
              type="button"
              aria-label="Clear search"
              onClick={() => setQuery("")}
              className="text-zinc-500 hover:text-white"
            >
              <X size={14} />
            </button>
          )}
        </label>
        <select
          aria-label="Category"
          value={view.category}
          onChange={(event) => updateView({ category: Number(event.target.value) })}
          className="rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-sm"
        >
          <option value={0}>All categories ({counts.get(0) ?? 0})</option>
          {categories.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name} ({counts.get(item.id) ?? 0})
            </option>
          ))}
        </select>
        <select
          aria-label="Sort by"
          value={view.sort}
          onChange={(event) => {
            const sort = event.target.value as LibrarySort;
            updateView({ sort, direction: naturalDirection(sort) });
          }}
          className="rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-sm"
        >
          {librarySorts.map((sort) => (
            <option key={sort} value={sort}>
              {sortLabels[sort]}
            </option>
          ))}
        </select>
        <button
          type="button"
          aria-label={view.direction === "asc" ? "Sort ascending" : "Sort descending"}
          onClick={() => updateView({ direction: view.direction === "asc" ? "desc" : "asc" })}
          className="rounded-lg border border-zinc-800 bg-zinc-900 p-2 text-zinc-300 hover:border-zinc-600"
        >
          {view.direction === "asc" ? <ArrowUp size={16} /> : <ArrowDown size={16} />}
        </button>
        <button
          type="button"
          onClick={() => setFiltersOpen(true)}
          className={`flex items-center gap-2 rounded-lg border px-3 py-2 text-sm ${
            activeCount > 0
              ? "border-amber-400/60 text-amber-300"
              : "border-zinc-800 bg-zinc-900 text-zinc-300 hover:border-zinc-600"
          }`}
        >
          <SlidersHorizontal size={16} />
          Filters
          {activeCount > 0 && (
            <span className="rounded-full bg-amber-400 px-1.5 text-xs font-semibold text-zinc-950">
              {activeCount}
            </span>
          )}
        </button>
      </div>
      {chips.length > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-2">
          {chips.map((chip) => (
            <button
              key={chip.id}
              type="button"
              onClick={() => updateView({ filters: withoutFilter(view.filters, chip) })}
              className="flex items-center gap-1.5 rounded-full border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-300 hover:border-zinc-500"
            >
              {chip.label}
              <X size={12} className="text-zinc-500" />
            </button>
          ))}
          <button
            type="button"
            onClick={() => updateView({ filters: emptyFilters })}
            className="text-xs text-zinc-400 hover:text-white hover:underline"
          >
            Clear all
          </button>
        </div>
      )}
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Loading library" />
      ) : items.length === 0 ? (
        <EmptyState
          title="Your library is empty"
          text="Browse an installed plugin and save a title to begin."
          action={
            <Link
              to="/browse"
              className="mt-3 rounded-lg bg-amber-400 px-4 py-2 text-sm font-semibold text-zinc-950"
            >
              Browse plugins
            </Link>
          }
        />
      ) : visible.length === 0 ? (
        <EmptyState
          title="No titles match"
          text="Adjust the search text or clear the active filters."
          action={
            <button
              type="button"
              onClick={() => {
                setQuery("");
                updateView({ filters: emptyFilters });
              }}
              className="mt-3 rounded-lg border border-zinc-700 px-4 py-2 text-sm text-zinc-200"
            >
              Clear filters
            </button>
          }
        />
      ) : (
        <div className={cardSizeClasses[view.cardSize]}>
          {visible.map((item) => (
            <LibraryCard key={item.id} item={item} view={view} />
          ))}
        </div>
      )}
      {filtersOpen && (
        <LibraryFiltersModal
          view={view}
          sources={sources}
          onPatch={updateView}
          onClose={() => setFiltersOpen(false)}
        />
      )}
    </div>
  );
}

function LibraryCard({ item, view }: { item: LibraryManga; view: LibraryView }) {
  const progress =
    item.progress && item.progress.totalPages
      ? Math.round((item.progress.lastReadPage / item.progress.totalPages) * 100)
      : 0;
  const resume = item.progress?.lastReadChapterId;
  const mangaUrl = `/manga/${encodeURIComponent(item.id)}`;
  return (
    <div className="group min-w-0">
      <div className="relative aspect-3/4 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900">
        <Link to={mangaUrl} aria-label={item.title} className="absolute inset-0 block">
          <CoverImg
            src={item.coverUrl}
            className="size-full object-cover transition duration-200 group-hover:scale-105"
          />
        </Link>
        {view.unreadBadge && item.unreadChapters > 0 && (
          <span
            aria-label={`${item.unreadChapters} unread chapters`}
            className="pointer-events-none absolute right-2 top-2 rounded-full bg-amber-400 px-2 py-0.5 text-[11px] font-semibold text-zinc-950"
          >
            {item.unreadChapters}
          </span>
        )}
        {view.continueButton && resume && (
          <Link
            to={`/reader/${encodeURIComponent(item.id)}/${encodeURIComponent(resume)}`}
            aria-label={`Continue ${item.title}`}
            className="absolute inset-x-2 bottom-2 flex items-center justify-center gap-1.5 rounded-lg bg-zinc-950/85 px-2 py-1.5 text-xs font-medium text-white backdrop-blur hover:bg-zinc-950"
          >
            <Play size={13} /> Continue
          </Link>
        )}
      </div>
      <Link to={mangaUrl}>
        <h2 className="mt-2 line-clamp-2 text-sm font-semibold group-hover:text-amber-300">
          {item.title}
        </h2>
      </Link>
      <p className="mt-1 truncate text-xs text-zinc-500">
        {item.sourceName || "Unknown plugin"}
        {item.status ? ` · ${item.status}` : ""}
      </p>
      {view.progressBar && progress > 0 && (
        <div className="mt-2 h-1 rounded-full bg-zinc-800">
          <span
            className="block h-full rounded-full bg-amber-400"
            style={{ width: `${progress}%` }}
          />
        </div>
      )}
    </div>
  );
}
