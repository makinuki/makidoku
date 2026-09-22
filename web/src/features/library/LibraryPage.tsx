import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type ReactNode,
} from "react";
import { Link } from "react-router-dom";
import {
  ArrowDown,
  ArrowUp,
  BookCheck,
  BookX,
  Check,
  Download,
  FolderPlus,
  Play,
  SlidersHorizontal,
  Trash2,
  X,
} from "lucide-react";
import { api } from "../../api";
import { onReselect, setBottomNavVisible } from "../../app/nav";
import type { BulkResult, Category, LibraryManga } from "../../types";
import { CoverImg } from "../../components/CoverImg";
import { Modal } from "../../components/Modal";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { LibraryFiltersModal } from "./LibraryFiltersModal";
import {
  activeFilterCount,
  cardSizeClasses,
  categoryCounts,
  defaultLibraryView,
  emptyFilters,
  filterChips,
  invertSelection,
  librarySorts,
  librarySources,
  libraryViewFromSettings,
  libraryViewSettings,
  mergeLibraryView,
  naturalDirection,
  selectAllIds,
  sortLabels,
  toggleRangeSelection,
  toggleSelection,
  visibleLibrary,
  withoutFilter,
  type LibrarySort,
  type LibraryView,
} from "./libraryState";

export function LibraryPage() {
  const [items, setItems] = useState<LibraryManga[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [view, setView] = useState<LibraryView>(defaultLibraryView);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const [selectionActive, setSelectionActive] = useState(false);
  const [anchor, setAnchor] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [categoryOpen, setCategoryOpen] = useState(false);
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
  const visible = useMemo(() => visibleLibrary(items, view, ""), [items, view]);
  const counts = useMemo(() => categoryCounts(items, view, ""), [items, view]);
  const sources = useMemo(() => librarySources(items), [items]);
  const chips = filterChips(view, (id) => sources.find((source) => source.id === id)?.name ?? id);
  const activeCount = activeFilterCount(view);
  const orderedIds = useMemo(() => visible.map((item) => item.id), [visible]);
  const selectionMode = selectionActive;

  const reloadLibrary = useCallback(async () => {
    const library = await api.library();
    setItems(library);
  }, []);

  const clearSelection = useCallback(() => {
    setSelection(new Set());
    setAnchor(null);
    setSelectionActive(false);
    setNotice("");
  }, []);

  const handleSelect = useCallback(
    (id: string, event: MouseEvent) => {
      setSelectionActive(true);
      setSelection((current) =>
        event.shiftKey && anchor
          ? toggleRangeSelection(current, orderedIds, anchor, id)
          : toggleSelection(current, id),
      );
      setAnchor(id);
    },
    [anchor, orderedIds],
  );

  // Selection is scoped to the visible list: a filter change drops ids that
  // are no longer shown so a batch action cannot touch hidden titles.
  useEffect(() => {
    if (!selectionActive) return;
    setSelection((current) => {
      const visibleSet = new Set(orderedIds);
      const next = new Set([...current].filter((id) => visibleSet.has(id)));
      return next.size === current.size ? current : next;
    });
  }, [orderedIds, selectionActive]);

  useEffect(() => {
    if (!selectionMode) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") clearSelection();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectionMode, clearSelection]);

  // The active Library tab reopens the layout sheet; selection mode lends the
  // bottom edge to the bulk action bar while it lasts.
  useEffect(
    () =>
      onReselect((tab) => {
        if (tab === "library") setFiltersOpen(true);
      }),
    [],
  );
  useEffect(() => {
    setBottomNavVisible(!selectionMode);
  }, [selectionMode]);

  const runBulk = useCallback(
    async (action: () => Promise<BulkResult>, message: string) => {
      setBusy(true);
      setError("");
      setNotice("");
      try {
        const result = await action();
        setNotice(
          result.failed.length
            ? `${message}: ${result.updated} applied, ${result.failed.length} failed.`
            : `${message}: ${result.updated} applied.`,
        );
        await reloadLibrary();
        clearSelection();
      } catch (e) {
        setError(e instanceof Error ? e.message : "Batch action failed");
      } finally {
        setBusy(false);
      }
    },
    [clearSelection, reloadLibrary],
  );

  const selectedIds = useMemo(() => [...selection], [selection]);

  return (
    <div className={`mx-auto max-w-7xl p-5 sm:p-8 ${selectionMode ? "pb-28" : ""}`}>
      <PageHeader title="Library">
        <span className="text-sm text-zinc-500">
          {visible.length} of {items.length} {items.length === 1 ? "title" : "titles"}
        </span>
      </PageHeader>
      <div className="mb-4 flex flex-wrap items-center gap-2">
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
        {items.length > 0 && (
          <button
            type="button"
            onClick={() => (selectionActive ? clearSelection() : setSelectionActive(true))}
            className="ml-auto rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-sm text-zinc-300 hover:border-zinc-600"
          >
            {selectionActive ? "Cancel" : "Select"}
          </button>
        )}
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
      {notice && <p className="mb-4 text-sm text-amber-300">{notice}</p>}
      {error && <ErrorState message={error} />}
      {selectionMode && (
        <div className="mb-4 flex flex-wrap items-center gap-3 rounded-xl border border-amber-400/40 bg-amber-400/5 px-3 py-2 text-sm">
          <span className="font-medium text-amber-200">{selection.size} selected</span>
          <button
            type="button"
            onClick={() => {
              setSelection(selectAllIds(orderedIds));
              setAnchor(null);
            }}
            className="text-zinc-300 hover:text-white"
          >
            Select all
          </button>
          <button
            type="button"
            onClick={() => setSelection(invertSelection(selection, orderedIds))}
            className="text-zinc-300 hover:text-white"
          >
            Invert
          </button>
          <button type="button" onClick={clearSelection} className="text-zinc-300 hover:text-white">
            Clear
          </button>
        </div>
      )}
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
          text="Select another category or clear the active filters."
          action={
            <button
              type="button"
              onClick={() => updateView({ filters: emptyFilters, category: 0 })}
              className="mt-3 rounded-lg border border-zinc-700 px-4 py-2 text-sm text-zinc-200"
            >
              Clear filters
            </button>
          }
        />
      ) : (
        <div className={cardSizeClasses[view.cardSize]}>
          {visible.map((item) => (
            <LibraryCard
              key={item.id}
              item={item}
              view={view}
              selectionMode={selectionMode}
              selected={selection.has(item.id)}
              onSelect={(event) => handleSelect(item.id, event)}
            />
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
      {selectionMode && (
        <div className="fixed inset-x-0 bottom-[calc(var(--nav-height)+var(--sat-bottom))] z-30 border-t border-zinc-800 bg-zinc-950/95 backdrop-blur md:bottom-0">
          <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-2 px-4 py-3">
            <SelectionAction
              icon={<BookCheck size={15} />}
              label="Mark read"
              disabled={busy || selection.size === 0}
              onClick={() => void runBulk(() => api.bulkSetRead(selectedIds, true), "Marked read")}
            />
            <SelectionAction
              icon={<BookX size={15} />}
              label="Mark unread"
              disabled={busy || selection.size === 0}
              onClick={() =>
                void runBulk(() => api.bulkSetRead(selectedIds, false), "Marked unread")
              }
            />
            <SelectionAction
              icon={<FolderPlus size={15} />}
              label="Category"
              disabled={busy || selection.size === 0}
              onClick={() => setCategoryOpen(true)}
            />
            <SelectionAction
              icon={<Download size={15} />}
              label="Download unread"
              disabled={busy || selection.size === 0}
              onClick={() =>
                void runBulk(() => api.bulkDownload(selectedIds, "unread"), "Queued downloads")
              }
            />
            <SelectionAction
              icon={<Trash2 size={15} />}
              label="Remove"
              danger
              disabled={busy || selection.size === 0}
              onClick={() =>
                void runBulk(() => api.bulkRemove(selectedIds), "Removed from library")
              }
            />
          </div>
        </div>
      )}
      {categoryOpen && (
        <Modal title="Change category" onClose={() => setCategoryOpen(false)}>
          {categories.length === 0 ? (
            <p className="text-sm text-zinc-400">
              Create a category from a title's page before grouping titles.
            </p>
          ) : (
            <ul className="space-y-2">
              {categories.map((category) => (
                <li
                  key={category.id}
                  className="flex items-center justify-between rounded-lg border border-zinc-800 px-3 py-2 text-sm"
                >
                  <span>{category.name}</span>
                  <span className="flex gap-2">
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        void runBulk(
                          () => api.bulkSetCategory(selectedIds, category.id, true),
                          "Added to category",
                        )
                      }
                      className="rounded-lg bg-amber-400 px-3 py-1 text-xs font-semibold text-zinc-950 disabled:opacity-40"
                    >
                      Add
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        void runBulk(
                          () => api.bulkSetCategory(selectedIds, category.id, false),
                          "Removed from category",
                        )
                      }
                      className="rounded-lg border border-zinc-700 px-3 py-1 text-xs disabled:opacity-40"
                    >
                      Remove
                    </button>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Modal>
      )}
    </div>
  );
}

function SelectionAction({
  icon,
  label,
  onClick,
  disabled,
  danger,
}: {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm disabled:opacity-40 ${
        danger
          ? "border-red-900 text-red-300 hover:bg-red-950/40"
          : "border-zinc-700 text-zinc-200 hover:bg-zinc-800"
      }`}
    >
      {icon}
      {label}
    </button>
  );
}

function LibraryCard({
  item,
  view,
  selectionMode,
  selected,
  onSelect,
}: {
  item: LibraryManga;
  view: LibraryView;
  selectionMode: boolean;
  selected: boolean;
  onSelect: (event: MouseEvent) => void;
}) {
  const progress =
    item.progress && item.progress.totalPages
      ? Math.round((item.progress.lastReadPage / item.progress.totalPages) * 100)
      : 0;
  const resume = item.progress?.lastReadChapterId;
  const mangaUrl = `/manga/${encodeURIComponent(item.id)}`;
  const intercept = (event: MouseEvent) => {
    if (selectionMode || event.shiftKey || event.metaKey || event.ctrlKey) {
      event.preventDefault();
      onSelect(event);
    }
  };
  return (
    <div className={`group min-w-0 ${selected ? "rounded-xl ring-2 ring-amber-400" : ""}`}>
      <div className="relative aspect-3/4 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900">
        <Link
          to={mangaUrl}
          aria-label={item.title}
          className="absolute inset-0 block"
          onClick={intercept}
        >
          <CoverImg
            src={item.coverUrl}
            className="size-full object-cover transition duration-200 group-hover:scale-105"
          />
        </Link>
        {selectionMode && (
          <span
            aria-hidden="true"
            className={`pointer-events-none absolute left-2 top-2 grid size-6 place-items-center rounded-full border ${
              selected
                ? "border-amber-400 bg-amber-400 text-zinc-950"
                : "border-zinc-400/80 bg-zinc-950/70 text-transparent"
            }`}
          >
            <Check size={14} />
          </span>
        )}
        {view.unreadBadge && item.unreadChapters > 0 && (
          <span
            aria-label={`${item.unreadChapters} unread chapters`}
            className="pointer-events-none absolute right-2 top-2 rounded-full bg-amber-400 px-2 py-0.5 text-[11px] font-semibold text-zinc-950"
          >
            {item.unreadChapters}
          </span>
        )}
        {view.continueButton && resume && !selectionMode && (
          <Link
            to={`/reader/${encodeURIComponent(item.id)}/${encodeURIComponent(resume)}`}
            aria-label={`Continue ${item.title}`}
            className="absolute inset-x-2 bottom-2 flex items-center justify-center gap-1.5 rounded-lg bg-zinc-950/85 px-2 py-1.5 text-xs font-medium text-white backdrop-blur hover:bg-zinc-950"
          >
            <Play size={13} /> Continue
          </Link>
        )}
      </div>
      <Link to={mangaUrl} onClick={intercept}>
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
