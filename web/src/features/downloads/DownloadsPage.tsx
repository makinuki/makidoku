import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { DragDropProvider } from "@dnd-kit/react";
import type { DragEndEvent, DragStartEvent } from "@dnd-kit/react";
import { isSortableOperation, useSortable } from "@dnd-kit/react/sortable";
import { ArrowUpDown, ChevronDown, Ellipsis, GripVertical, Pause, Play } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { api } from "../../api";
import type { DownloadEvent, DownloadSnapshot, QueueItem } from "../../types";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import {
  buildSections,
  chapterLabel,
  dragDestination,
  flattenSections,
  isSortSelected,
  mergeLiveOrder,
  moveRow,
  moveRowToEdge,
  moveSection,
  moveSeries,
  queueRowIds,
  rowIndex,
  sectionIdOf,
  sectionIndex,
  sectionSortableId,
  seriesRowIds,
  sortOptions,
  sortSections,
  statusLabel,
  type QueueSection,
  type QueueSort,
} from "./downloadsState";

// Rows are dragged as chapters and headers as sources, so a chapter can only be
// dropped on another chapter of the same source.
const chapterType = "chapter";
const sourceType = "source";

type RowAction =
  | "move-top"
  | "move-bottom"
  | "series-top"
  | "series-bottom"
  | "retry"
  | "cancel"
  | "cancel-series";

const emptySnapshot: DownloadSnapshot = {
  items: [],
  stats: { downloadedPages: 0, retriedRequests: 0, throttledRequests: 0 },
  paused: false,
};

const sortMenuGroups = sortOptions.reduce<Array<{ label: string; options: typeof sortOptions }>>(
  (groups, option) => {
    const group = groups.find((entry) => entry.label === option.group);
    if (group) group.options.push(option);
    else groups.push({ label: option.group, options: [option] });
    return groups;
  },
  [],
);

function messageOf(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

export function DownloadsPage() {
  const [snapshot, setSnapshot] = useState<DownloadSnapshot>(emptySnapshot);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [draggingSource, setDraggingSource] = useState(false);
  const [sort, setSort] = useState<QueueSort | null>(null);

  const refresh = useCallback(
    () =>
      api
        .downloads()
        .then(setSnapshot)
        .catch((e) => setError(messageOf(e, "Unable to load the download queue"))),
    [],
  );

  useEffect(() => {
    void refresh().finally(() => setLoading(false));
    let socket: WebSocket | undefined;
    let reconnectTimer: number | undefined;
    let disposed = false;
    const applyMessage = (data: unknown) => {
      try {
        const message = JSON.parse(String(data)) as DownloadEvent;
        // A reorder rewrites the order of the whole queue, so the client
        // refetches the snapshot instead of merging a single event.
        if (message.type === "reordered") {
          void refresh();
          return;
        }
        // An item-less event only announces downloader state, so the rows the
        // client already holds stay as they are.
        const next = message.item;
        setSnapshot((current) => ({
          ...current,
          paused: message.paused ?? current.paused,
          items: !next
            ? current.items
            : current.items.some((item) => item.id === next.id)
              ? current.items.map((item) => (item.id === next.id ? next : item))
              : [...current.items, next],
          stats: message.stats,
        }));
      } catch {
        // A malformed frame is skipped; the next refetch reconciles state.
      }
    };
    const connect = () => {
      if (disposed) return;
      const protocol = location.protocol === "https:" ? "wss:" : "ws:";
      const next = new WebSocket(`${protocol}//${location.host}/api/download/events`);
      socket = next;
      next.onmessage = (event) => applyMessage(event.data);
      // Every successful connection refetches the snapshot so events missed
      // while offline are recovered.
      next.onopen = () => void refresh();
      next.onclose = () => {
        if (disposed) return;
        reconnectTimer = window.setTimeout(connect, 2000);
      };
    };
    connect();
    return () => {
      disposed = true;
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [refresh]);

  const sections = useMemo(() => buildSections(snapshot.items), [snapshot.items]);
  const queuedCount = sections.reduce((total, section) => total + section.rows.length, 0);

  // Order changes are applied locally first and persisted as the ids of the
  // live rows; a failed request falls back to the stored order.
  const applyOrder = async (next: QueueSection[]) => {
    setSnapshot((current) => ({
      ...current,
      items: mergeLiveOrder(current.items, flattenSections(next)),
    }));
    setError("");
    try {
      await api.reorderDownloads(queueRowIds(next));
    } catch (e) {
      setError(messageOf(e, "Unable to save the queue order"));
      await refresh();
    }
  };

  const applySort = async (next: QueueSort) => {
    setSort(next);
    await applyOrder(sortSections(sections, next));
  };

  const downloaderState = async (resume: boolean) => {
    setBusy(true);
    setError("");
    try {
      setSnapshot(resume ? await api.resumeAllDownloads() : await api.pauseAllDownloads());
    } catch (e) {
      setError(messageOf(e, "Unable to change the downloader state"));
    } finally {
      setBusy(false);
    }
  };

  const cancelAll = async () => {
    setBusy(true);
    setError("");
    try {
      setSnapshot(await api.cancelAllDownloads());
    } catch (e) {
      setError(messageOf(e, "Unable to cancel the queue"));
    } finally {
      setBusy(false);
    }
  };

  const clearFinished = async () => {
    setBusy(true);
    setError("");
    try {
      await api.clearFinishedDownloads();
      await refresh();
    } catch (e) {
      setError(messageOf(e, "Unable to clear finished downloads"));
    } finally {
      setBusy(false);
    }
  };

  const controlRow = async (itemId: number, action: "retry" | "cancel", fallback: string) => {
    setError("");
    try {
      await api.controlDownload(itemId, action);
      await refresh();
    } catch (e) {
      setError(messageOf(e, fallback));
    }
  };

  const cancelSeries = async (mangaId: string) => {
    setError("");
    try {
      setSnapshot(await api.cancelDownloads(seriesRowIds(sections, mangaId)));
    } catch (e) {
      setError(messageOf(e, "Unable to cancel this series"));
    }
  };

  const handleRowAction = (action: RowAction, item: QueueItem) => {
    switch (action) {
      case "move-top":
        void applyOrder(moveRowToEdge(sections, item.id, "top"));
        return;
      case "move-bottom":
        void applyOrder(moveRowToEdge(sections, item.id, "bottom"));
        return;
      case "series-top":
        void applyOrder(moveSeries(sections, item.mangaId, "top"));
        return;
      case "series-bottom":
        void applyOrder(moveSeries(sections, item.mangaId, "bottom"));
        return;
      case "retry":
        void controlRow(item.id, "retry", "Unable to retry the download");
        return;
      case "cancel":
        void controlRow(item.id, "cancel", "Unable to cancel the download");
        return;
      case "cancel-series":
        void cancelSeries(item.mangaId);
        return;
    }
  };

  const handleDragStart = (event: DragStartEvent) => {
    // Dragging a header collapses the sections, so the whole queue fits on
    // screen while the source is being moved.
    if (sectionIdOf(event.operation.source?.id)) setDraggingSource(true);
  };

  const handleDragEnd = (event: DragEndEvent) => {
    setDraggingSource(false);
    const operation = event.operation;
    if (event.canceled || !isSortableOperation(operation)) return;
    const { source, target } = operation;
    if (!source || !target) return;
    const destination = dragDestination(source.index, target.index);
    const sourceId = sectionIdOf(source.id);
    if (sourceId) {
      if (sectionIndex(sections, sourceId) === destination) return;
      void applyOrder(moveSection(sections, sourceId, destination));
      return;
    }
    // A row never leaves its source group, so a drop onto another group keeps
    // the current order; a release on the row itself still moves it.
    if (typeof source.id !== "number" || source.group !== target.group) return;
    if (rowIndex(sections, source.id) === destination) return;
    void applyOrder(moveRow(sections, source.id, destination));
  };

  return (
    <div className="mx-auto max-w-5xl p-5 sm:p-8">
      <PageHeader
        eyebrow="Queue"
        title={
          <span className="flex items-center gap-3">
            Download queue
            {queuedCount > 0 && (
              <span className="rounded-full bg-zinc-800 px-2.5 py-0.5 text-base font-medium text-zinc-300">
                {queuedCount}
              </span>
            )}
          </span>
        }
      >
        <div className="flex items-center gap-2">
          <SortMenu sort={sort} onSelect={(next) => void applySort(next)} />
          <QueueMenu label="Queue actions" Icon={Ellipsis}>
            {(close) => (
              <>
                <MenuItem
                  onSelect={() => {
                    close();
                    void cancelAll();
                  }}
                >
                  Cancel all
                </MenuItem>
                <MenuItem
                  onSelect={() => {
                    close();
                    void clearFinished();
                  }}
                >
                  Clear finished
                </MenuItem>
              </>
            )}
          </QueueMenu>
          {queuedCount > 0 && (
            <DownloaderControl
              paused={snapshot.paused}
              busy={busy}
              onToggle={() => void downloaderState(snapshot.paused)}
            />
          )}
        </div>
      </PageHeader>
      <p className="mb-6 text-sm text-zinc-500">
        {snapshot.stats.downloadedPages} pages saved · {snapshot.stats.retriedRequests} retried ·{" "}
        {snapshot.stats.throttledRequests} slowed by the source
      </p>
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Loading queue" />
      ) : queuedCount === 0 ? (
        <EmptyState title="No downloads" text="Select chapters from a title to start a download." />
      ) : (
        <DragDropProvider onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
          <div className="space-y-5">
            {sections.map((section, index) => (
              <SourceSection
                key={section.sourceId}
                section={section}
                index={index}
                collapsed={draggingSource || collapsed.has(section.sourceId)}
                onToggle={() =>
                  setCollapsed((current) => {
                    const next = new Set(current);
                    if (next.has(section.sourceId)) next.delete(section.sourceId);
                    else next.add(section.sourceId);
                    return next;
                  })
                }
                onAction={handleRowAction}
              />
            ))}
          </div>
        </DragDropProvider>
      )}
    </div>
  );
}

function SourceSection({
  section,
  index,
  collapsed,
  onToggle,
  onAction,
}: {
  section: QueueSection;
  index: number;
  collapsed: boolean;
  onToggle: () => void;
  onAction: (action: RowAction, item: QueueItem) => void;
}) {
  const { ref, handleRef } = useSortable({
    id: sectionSortableId(section.sourceId),
    index,
    type: sourceType,
    accept: sourceType,
  });
  return (
    <section ref={ref} className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/40">
      <div className="flex items-center gap-1 px-2 py-1">
        <button
          type="button"
          aria-expanded={!collapsed}
          aria-label={`${section.sourceName} (${section.rows.length})`}
          onClick={onToggle}
          className="flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-lg px-2 text-left hover:bg-zinc-800/50 active:bg-zinc-800/50"
        >
          <ChevronDown
            size={16}
            className={`shrink-0 text-zinc-500 transition-transform ${collapsed ? "-rotate-90" : ""}`}
          />
          <span className="truncate text-sm font-semibold text-amber-400">
            {section.sourceName}
          </span>
          <span className="shrink-0 text-xs text-zinc-500">({section.rows.length})</span>
        </button>
        <button
          type="button"
          ref={handleRef}
          aria-label={`Reorder ${section.sourceName}`}
          className="flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-lg p-2 text-zinc-500 hover:bg-zinc-800 hover:text-white active:bg-zinc-800 active:text-white"
        >
          <GripVertical size={18} />
        </button>
      </div>
      {!collapsed && (
        <ul className="divide-y divide-zinc-800/70 border-t border-zinc-800/70">
          {section.rows.map((row, rowIndex) => (
            <QueueRow
              key={row.id}
              item={row}
              index={rowIndex}
              count={section.rows.length}
              onAction={onAction}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function QueueRow({
  item,
  index,
  count,
  onAction,
}: {
  item: QueueItem;
  index: number;
  count: number;
  onAction: (action: RowAction, item: QueueItem) => void;
}) {
  const { ref, handleRef } = useSortable({
    id: item.id,
    index,
    group: item.sourceId,
    type: chapterType,
    accept: chapterType,
  });
  const label = chapterLabel(item);
  const pages = item.totalPages > 0 ? `${item.downloadedPages}/${item.totalPages}` : "";
  return (
    <li ref={ref} className="flex items-start gap-1 p-3">
      <button
        type="button"
        ref={handleRef}
        aria-label={`Reorder ${label} of ${item.mangaTitle}`}
        className="flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-lg text-zinc-500 hover:bg-zinc-800 hover:text-white active:bg-zinc-800 active:text-white"
      >
        <GripVertical size={18} />
      </button>
      <div className="min-w-0 flex-1 pt-1.5">
        <div className="flex items-baseline gap-3">
          <span className="min-w-0 flex-1 truncate text-sm font-medium">{item.mangaTitle}</span>
          {pages && <span className="shrink-0 text-xs text-zinc-500">{pages}</span>}
        </div>
        <div className="mt-0.5 flex items-baseline gap-3">
          <span className="min-w-0 flex-1 truncate text-xs text-zinc-500">{label}</span>
          <span className="shrink-0 text-xs text-zinc-500">{statusLabel(item)}</span>
        </div>
        <div className="mt-2 h-1.5 rounded-full bg-zinc-800">
          <span
            className="block h-full rounded-full bg-amber-400"
            style={{ width: `${item.progress}%` }}
          />
        </div>
        {item.status === "FAILED" && item.errorMessage && (
          <p className="mt-2 text-xs text-red-300">{item.errorMessage}</p>
        )}
      </div>
      <QueueMenu label={`Actions for ${label} of ${item.mangaTitle}`} Icon={Ellipsis}>
        {(close) => (
          <>
            {index > 0 && (
              <MenuItem
                onSelect={() => {
                  close();
                  onAction("move-top", item);
                }}
              >
                Move to top
              </MenuItem>
            )}
            <MenuItem
              onSelect={() => {
                close();
                onAction("series-top", item);
              }}
            >
              Move series to top
            </MenuItem>
            {index < count - 1 && (
              <MenuItem
                onSelect={() => {
                  close();
                  onAction("move-bottom", item);
                }}
              >
                Move to bottom
              </MenuItem>
            )}
            <MenuItem
              onSelect={() => {
                close();
                onAction("series-bottom", item);
              }}
            >
              Move series to bottom
            </MenuItem>
            {item.status === "FAILED" && (
              <MenuItem
                onSelect={() => {
                  close();
                  onAction("retry", item);
                }}
              >
                Retry
              </MenuItem>
            )}
            <MenuItem
              danger
              onSelect={() => {
                close();
                onAction("cancel", item);
              }}
            >
              Cancel
            </MenuItem>
            <MenuItem
              danger
              onSelect={() => {
                close();
                onAction("cancel-series", item);
              }}
            >
              Cancel all for this series
            </MenuItem>
            <Link
              to={`/manga/${encodeURIComponent(item.mangaId)}`}
              role="menuitem"
              onClick={close}
              className={menuItemClass}
            >
              Show title
            </Link>
          </>
        )}
      </QueueMenu>
    </li>
  );
}

function SortMenu({
  sort,
  onSelect,
}: {
  sort: QueueSort | null;
  onSelect: (next: QueueSort) => void;
}) {
  return (
    <QueueMenu label="Sort queue" Icon={ArrowUpDown}>
      {(close) => (
        <>
          {sortMenuGroups.map((group) => (
            <div key={group.label} role="group" aria-label={group.label}>
              <p aria-hidden="true" className="px-2 pt-2 pb-1 text-xs text-zinc-500">
                {group.label}
              </p>
              {group.options.map((option) => (
                <button
                  key={option.label}
                  type="button"
                  role="menuitemradio"
                  aria-checked={isSortSelected(sort, option)}
                  onClick={() => {
                    close();
                    onSelect({ field: option.field, direction: option.direction });
                  }}
                  className={menuItemClass}
                >
                  {option.label}
                </button>
              ))}
            </div>
          ))}
        </>
      )}
    </QueueMenu>
  );
}

function DownloaderControl({
  paused,
  busy,
  onToggle,
}: {
  paused: boolean;
  busy: boolean;
  onToggle: () => void;
}) {
  const label = paused ? "Resume downloads" : "Pause downloads";
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onToggle}
      disabled={busy}
      className="fixed bottom-[calc(var(--nav-height)+var(--sat-bottom)+1rem)] left-1/2 z-30 flex min-h-11 -translate-x-1/2 items-center gap-2 rounded-full border border-zinc-700 bg-zinc-900 px-4 text-sm font-medium text-zinc-100 shadow-lg hover:border-zinc-500 disabled:opacity-50 md:static md:translate-x-0 md:rounded-lg md:px-3 md:py-2 md:shadow-none"
    >
      {paused ? <Play size={16} /> : <Pause size={16} />}
      {paused ? "Resume" : "Pause"}
    </button>
  );
}

const menuItemClass =
  "flex min-h-11 w-full items-center rounded-md px-2 py-2 text-left text-sm hover:bg-zinc-800 active:bg-zinc-800";

function MenuItem({
  children,
  onSelect,
  danger,
}: {
  children: ReactNode;
  onSelect: () => void;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onSelect}
      className={`${menuItemClass} ${danger ? "text-red-300 hover:bg-red-950/40 active:bg-red-950/40" : ""}`}
    >
      {children}
    </button>
  );
}

// Menus close on outside tap and on Escape: touch users have no hover escape
// hatch, so the trigger behaves like a modal toggle.
function QueueMenu({
  label,
  Icon,
  children,
}: {
  label: string;
  Icon: LucideIcon;
  children: (close: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) close();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [open, close]);
  return (
    <div className="relative" ref={menuRef}>
      <button
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
        className="flex min-h-11 min-w-11 items-center justify-center rounded-lg p-2 text-zinc-400 hover:bg-zinc-800 hover:text-white active:bg-zinc-800 active:text-white"
      >
        <Icon size={18} />
      </button>
      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-20 mt-1 w-56 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-xl"
        >
          {children(close)}
        </div>
      )}
    </div>
  );
}
