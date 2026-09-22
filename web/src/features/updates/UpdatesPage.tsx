import { useEffect, useMemo, useState } from "react";
import { Bookmark, BookmarkCheck, Check, ChevronDown, Download, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import { api } from "../../api";
import { refreshBadges, setBottomNavVisible } from "../../app/nav";
import type { LibraryUpdateState, Manga, UpdateLog } from "../../types";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { CoverImg } from "../../components/CoverImg";
import { dateGroupLabel, formatTimestamp } from "../../time";
import { useDateFormat, type DateFormat } from "../../hooks/useDateFormat";

type MangaGroup = { manga: Manga; items: UpdateLog[] };
type DateGroup = { label: string; groups: MangaGroup[] };

function chapterLabel(item: UpdateLog): string {
  const chapter = item.chapter;
  if (chapter.chapterNumber == null) return chapter.title || "Special";
  return `Chapter ${chapter.chapterNumber}`;
}

export function UpdatesPage() {
  const dateFormat = useDateFormat();
  const [items, setItems] = useState<UpdateLog[]>([]);
  const [state, setState] = useState<LibraryUpdateState | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [queued, setQueued] = useState<Set<string>>(new Set());
  const load = () =>
    Promise.all([api.updates(), api.updateState()])
      .then(([updates, status]) => {
        setItems(updates);
        setState(status);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "Unable to load updates"));
  useEffect(() => {
    load().finally(() => setLoading(false));
  }, []);
  useEffect(() => {
    setBottomNavVisible(!selecting);
  }, [selecting]);
  const groups = useMemo<DateGroup[]>(() => {
    const dates = new Map<string, Map<string, MangaGroup>>();
    for (const item of items) {
      const label = dateGroupLabel(item.seenAt);
      let mangaGroups = dates.get(label);
      if (!mangaGroups) {
        mangaGroups = new Map();
        dates.set(label, mangaGroups);
      }
      const group = mangaGroups.get(item.manga.id);
      if (group) group.items.push(item);
      else mangaGroups.set(item.manga.id, { manga: item.manga, items: [item] });
    }
    return [...dates.entries()].map(([label, mangaGroups]) => ({
      label,
      groups: [...mangaGroups.values()],
    }));
  }, [items]);
  const clearSelection = () => {
    setSelected(new Set());
    setSelecting(false);
  };
  useEffect(() => {
    if (!selecting) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") clearSelection();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selecting]);
  const run = async () => {
    setBusy(true);
    setError("");
    try {
      await api.runUpdates();
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to update library");
    } finally {
      setBusy(false);
    }
  };
  const drop = (ids: Set<string>) =>
    setItems((current) => current.filter((item) => !ids.has(item.id)));
  const acknowledge = async (ids: string[]) => {
    try {
      await api.acknowledgeUpdates(ids);
      drop(new Set(ids));
      setSelected((current) => {
        const next = new Set(current);
        for (const id of ids) next.delete(id);
        return next;
      });
      refreshBadges();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to acknowledge update");
    }
  };
  const download = async (logs: UpdateLog[]) => {
    setError("");
    const byManga = new Map<string, string[]>();
    for (const log of logs) {
      const chapters = byManga.get(log.manga.id) ?? [];
      chapters.push(log.chapter.id);
      byManga.set(log.manga.id, chapters);
    }
    try {
      for (const [mangaId, chapterIds] of byManga) {
        await api.enqueue(mangaId, chapterIds);
      }
      setQueued((current) => {
        const next = new Set(current);
        for (const log of logs) next.add(log.id);
        return next;
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to queue download");
    }
  };
  const toggleBookmark = async (log: UpdateLog) => {
    const next = !log.chapter.bookmark;
    try {
      await api.setChapterBookmark(log.chapter.id, next);
      setItems((current) =>
        current.map((item) =>
          item.id === log.id ? { ...item, chapter: { ...item.chapter, bookmark: next } } : item,
        ),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to update the bookmark");
    }
  };
  const toggleExpanded = (key: string) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  const toggleSelected = (id: string) =>
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const selectedLogs = items.filter((item) => selected.has(item.id));
  return (
    <div className={`mx-auto max-w-5xl p-5 sm:p-8 ${selecting ? "pb-28" : ""}`}>
      <PageHeader eyebrow="Library changes" title="Updates">
        <div className="flex flex-wrap gap-2">
          {items.length > 0 && (
            <button
              type="button"
              onClick={() => (selecting ? clearSelection() : setSelecting(true))}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm text-zinc-300 hover:border-zinc-500 active:border-zinc-500"
            >
              {selecting ? "Cancel" : "Select"}
            </button>
          )}
          <Link
            to="/downloads"
            className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm text-zinc-300 hover:border-zinc-500 active:border-zinc-500"
          >
            View queue
          </Link>
          <button
            type="button"
            onClick={() => void run()}
            disabled={busy}
            className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-4 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-50"
          >
            <RefreshCw size={16} className={busy ? "animate-spin" : ""} />{" "}
            {busy ? "Checking…" : "Check now"}
          </button>
        </div>
      </PageHeader>
      {state?.lastRunAt && (
        <p className="mb-6 text-sm text-zinc-500">
          Library last updated {formatTimestamp(state.lastRunAt, dateFormat)}
          {state.lastStatus === "completed_with_errors" ? " with some source errors" : ""}.
        </p>
      )}
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Loading updates" />
      ) : groups.length === 0 ? (
        <EmptyState title="No new chapters" text="Run a library check to look for changes." />
      ) : (
        <div className="space-y-8">
          {groups.map((date) => (
            <section key={date.label}>
              <h2 className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-amber-400">
                {date.label}{" "}
                <span className="text-zinc-600">
                  {date.groups.reduce((total, group) => total + group.items.length, 0)}
                </span>
              </h2>
              <div className="divide-y divide-zinc-800 rounded-xl border border-zinc-800 bg-zinc-900/50">
                {date.groups.map((group) => {
                  const key = `${date.label}:${group.manga.id}`;
                  const open = group.items.length === 1 || expanded.has(key);
                  return (
                    <div key={group.manga.id}>
                      <div className="flex items-center gap-4 p-4">
                        <Link
                          to={`/manga/${encodeURIComponent(group.manga.id)}`}
                          aria-label={group.manga.title}
                          className="size-14 shrink-0 overflow-hidden rounded-lg bg-zinc-800"
                        >
                          <CoverImg src={group.manga.coverUrl} className="size-full object-cover" />
                        </Link>
                        <div className="min-w-0 flex-1">
                          <span className="block truncate font-medium">{group.manga.title}</span>
                          <span className="text-sm text-zinc-500">
                            {group.items.length === 1
                              ? `${chapterLabel(group.items[0])} · ${formatTimestamp(group.items[0].seenAt, dateFormat)}`
                              : `${group.items.length} chapters`}
                          </span>
                        </div>
                        {group.items.length > 1 && (
                          <button
                            type="button"
                            aria-label={`${open ? "Collapse" : "Expand"} updates for ${group.manga.title}`}
                            aria-expanded={open}
                            onClick={() => toggleExpanded(key)}
                            className="flex min-h-11 min-w-11 items-center justify-center rounded-lg border border-zinc-700 text-zinc-300 hover:border-zinc-500 active:border-zinc-500"
                          >
                            <ChevronDown
                              size={17}
                              className={`transition-transform ${open ? "rotate-180" : ""}`}
                            />
                          </button>
                        )}
                      </div>
                      {open && (
                        <ul className="divide-y divide-zinc-800/70 border-t border-zinc-800/70">
                          {group.items.map((item) => (
                            <ChapterRow
                              key={item.id}
                              item={item}
                              selecting={selecting}
                              checked={selected.has(item.id)}
                              queued={queued.has(item.id)}
                              dateFormat={dateFormat}
                              onToggle={() => toggleSelected(item.id)}
                              onAcknowledge={() => void acknowledge([item.id])}
                              onDownload={() => void download([item])}
                              onBookmark={() => void toggleBookmark(item)}
                            />
                          ))}
                        </ul>
                      )}
                    </div>
                  );
                })}
              </div>
            </section>
          ))}
        </div>
      )}
      {selecting && (
        <div className="fixed inset-x-0 bottom-[calc(var(--nav-height)+var(--sat-bottom))] z-30 border-t border-zinc-800 bg-zinc-950/95 backdrop-blur md:bottom-0">
          <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-2 px-4 py-3">
            <span className="mr-2 text-sm font-medium text-amber-200">
              {selected.size} selected
            </span>
            <button
              type="button"
              disabled={selected.size === 0}
              onClick={() => void acknowledge(selectedLogs.map((item) => item.id))}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm text-zinc-200 hover:border-amber-400 hover:text-amber-300 active:border-amber-400 disabled:opacity-40"
            >
              <Check size={15} /> Mark read
            </button>
            <button
              type="button"
              disabled={selected.size === 0}
              onClick={() => void download(selectedLogs)}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm text-zinc-200 hover:border-zinc-500 active:border-zinc-500 disabled:opacity-40"
            >
              <Download size={15} /> Download
            </button>
            <button
              type="button"
              onClick={clearSelection}
              className="ml-auto rounded-lg px-3 py-2 text-sm text-zinc-400 hover:text-white"
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function ChapterRow({
  item,
  selecting,
  checked,
  queued,
  dateFormat,
  onToggle,
  onAcknowledge,
  onDownload,
  onBookmark,
}: {
  item: UpdateLog;
  selecting: boolean;
  checked: boolean;
  queued: boolean;
  dateFormat: DateFormat;
  onToggle: () => void;
  onAcknowledge: () => void;
  onDownload: () => void;
  onBookmark: () => void;
}) {
  const readerUrl = `/reader/${encodeURIComponent(item.manga.id)}/${encodeURIComponent(item.chapter.id)}`;
  return (
    <li
      className={`flex items-center gap-3 py-3 pl-4 pr-4 sm:pl-[4.5rem] ${checked ? "bg-amber-400/5" : ""}`}
    >
      {selecting && (
        <button
          type="button"
          role="checkbox"
          aria-checked={checked}
          aria-label={`Select ${chapterLabel(item)}`}
          onClick={onToggle}
          className={`grid size-6 shrink-0 place-items-center rounded-full border ${
            checked
              ? "border-amber-400 bg-amber-400 text-zinc-950"
              : "border-zinc-500 text-transparent"
          }`}
        >
          <Check size={14} />
        </button>
      )}
      <div className="min-w-0 flex-1">
        <Link
          to={readerUrl}
          onClick={(event) => {
            if (selecting) {
              event.preventDefault();
              onToggle();
            }
          }}
          className="block truncate text-sm font-medium hover:text-amber-300"
        >
          {chapterLabel(item)}
        </Link>
        <p className="text-xs text-zinc-500">
          {formatTimestamp(item.seenAt, dateFormat)}
          {item.chapter.downloaded ? " · Saved" : ""}
        </p>
      </div>
      <button
        type="button"
        aria-label={`${item.chapter.bookmark ? "Remove bookmark from" : "Bookmark"} ${chapterLabel(item)}`}
        aria-pressed={!!item.chapter.bookmark}
        onClick={onBookmark}
        className={`flex min-h-11 min-w-11 items-center justify-center rounded-lg ${
          item.chapter.bookmark
            ? "text-amber-300"
            : "text-zinc-500 hover:bg-zinc-800 hover:text-amber-300 active:bg-zinc-800"
        }`}
      >
        {item.chapter.bookmark ? <BookmarkCheck size={17} /> : <Bookmark size={17} />}
      </button>
      {item.chapter.downloaded || queued ? (
        <span
          aria-label={`${chapterLabel(item)} saved`}
          className="inline-flex min-h-11 items-center gap-1.5 rounded-lg px-2 text-xs text-emerald-300"
        >
          <Download size={15} /> {queued && !item.chapter.downloaded ? "Queued" : "Saved"}
        </span>
      ) : (
        <button
          type="button"
          aria-label={`Download ${chapterLabel(item)}`}
          onClick={onDownload}
          className="flex min-h-11 min-w-11 items-center justify-center rounded-lg border border-zinc-700 text-zinc-300 hover:border-zinc-500 active:border-zinc-500"
        >
          <Download size={16} />
        </button>
      )}
      <button
        type="button"
        onClick={onAcknowledge}
        className="inline-flex min-h-11 items-center gap-2 rounded-lg border border-zinc-700 px-3 text-sm text-zinc-300 hover:border-amber-400 hover:text-amber-300 active:border-amber-400"
      >
        <Check size={16} /> Mark read
      </button>
    </li>
  );
}
