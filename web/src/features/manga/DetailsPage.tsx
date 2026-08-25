import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  Bookmark,
  Check,
  Download,
  Ellipsis,
  ExternalLink,
  LoaderCircle,
  Play,
  RefreshCw,
  RotateCw,
  Save,
  Tag,
  X,
} from "lucide-react";
import { api } from "../../api";
import type {
  Aggregate,
  Binding,
  Category,
  Chapter,
  MigrationCandidate,
  TrackerInfo,
} from "../../types";
import { CoverImg } from "../../components/CoverImg";
import { Modal } from "../../components/Modal";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { relativeTime } from "../../time";
import { TrackerLogo, trackerLabel } from "../../components/TrackerLogo";
import { useTrackerEvents } from "../../hooks/useTrackerEvents";

export function DetailsPage() {
  const { mangaId = "" } = useParams();
  const decodedManga = decodeURIComponent(mangaId);
  const navigate = useNavigate();
  const [data, setData] = useState<Aggregate>();
  const [categories, setCategories] = useState<Category[]>([]);
  const [categoryError, setCategoryError] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [modal, setModal] = useState<"tracker" | "migration">();
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState("");
  const [actionError, setActionError] = useState("");
  const [libraryBusy, setLibraryBusy] = useState(false);
  const [categoryBusy, setCategoryBusy] = useState<number>();
  const [enqueueing, setEnqueueing] = useState(false);
  const [enqueueError, setEnqueueError] = useState("");
  const [queuedNote, setQueuedNote] = useState("");
  const [languageFilter, setLanguageFilter] = useState<string>();
  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setData(await api.manga(decodedManga));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load title");
    } finally {
      setLoading(false);
    }
  };
  // Background refetch used after mutations: stale content stays on screen
  // and open dialogs are not dismissed by a loading flash.
  const reload = async () => {
    try {
      setData(await api.manga(decodedManga));
    } catch {
      // keep the current view when a background refresh fails
    }
  };
  // The read is local-first, so a failed refresh only reports; the stale
  // content stays on screen for the next attempt.
  const refresh = async () => {
    setRefreshing(true);
    setRefreshError("");
    try {
      setData(await api.refreshManga(decodedManga));
    } catch (e) {
      setRefreshError(e instanceof Error ? e.message : "Unable to refresh");
    } finally {
      setRefreshing(false);
    }
  };
  useEffect(() => {
    void load();
    void api
      .categories()
      .then(setCategories)
      .catch((e) => setCategoryError(e instanceof Error ? e.message : "Unable to load categories"));
  }, [decodedManga]);
  useEffect(() => {
    if (!queuedNote) return;
    const timer = window.setTimeout(() => setQueuedNote(""), 4000);
    return () => window.clearTimeout(timer);
  }, [queuedNote]);
  if (loading) return <LoadingState label="Loading title" />;
  if (error || !data)
    return (
      <div className="mx-auto max-w-5xl p-5 sm:p-8">
        <ErrorState message={error || "Title not found"} />
      </div>
    );
  const { manga, chapters } = data;
  const languages = Array.from(
    new Set(chapters.map((chapter) => chapter.language).filter(Boolean) as string[]),
  ).sort();
  const visible = languageFilter
    ? chapters.filter((chapter) => chapter.language === languageFilter)
    : chapters;
  const groups = groupByVolume(visible);
  const toggle = (id: string) =>
    setSelected((items) =>
      items.includes(id) ? items.filter((item) => item !== id) : [...items, id],
    );
  // Library and category changes report failures inline: the title stays on
  // screen and the user can retry without losing their place.
  const toggleLibrary = async () => {
    setLibraryBusy(true);
    setActionError("");
    try {
      await api.setLibrary(manga.id, !manga.inLibrary);
      await reload();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Unable to update the library");
    } finally {
      setLibraryBusy(false);
    }
  };
  const toggleCategory = async (category: Category, active: boolean) => {
    setCategoryBusy(category.id);
    setActionError("");
    try {
      await api.setCategory(manga.id, category.id, !active);
      await reload();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Unable to update categories");
    } finally {
      setCategoryBusy(undefined);
    }
  };
  const enqueue = async () => {
    setEnqueueing(true);
    setEnqueueError("");
    setQueuedNote("");
    try {
      await api.enqueue(manga.id, selected, "", manga.downloadFormat);
      setQueuedNote("Chapters queued for download.");
      await reload();
    } catch (e) {
      setEnqueueError(e instanceof Error ? e.message : "Unable to queue chapters");
    } finally {
      setEnqueueing(false);
    }
  };
  return (
    <div className="mx-auto max-w-7xl p-5 sm:p-8">
      <button
        onClick={() => navigate(-1)}
        className="mb-5 flex items-center gap-2 text-sm text-zinc-400 hover:text-white"
      >
        <ArrowLeft size={16} /> Back
      </button>
      <section className="grid gap-6 rounded-2xl border border-zinc-800 bg-zinc-900/60 p-5 md:grid-cols-[180px_1fr] md:p-7">
        <div className="aspect-3/4 overflow-hidden rounded-xl bg-zinc-800">
          <CoverImg src={manga.coverUrl} className="size-full object-cover" />
        </div>
        <div>
          <p className="text-xs uppercase tracking-[0.18em] text-amber-400">
            {data.sourceName || "Unknown plugin"} · {manga.status || "Unknown status"}
          </p>
          <h1 className="mt-2 text-3xl font-semibold">{manga.title}</h1>
          {manga.description && (
            <p className="mt-4 max-w-3xl whitespace-pre-wrap text-sm leading-6 text-zinc-400">
              {manga.description}
            </p>
          )}
          <div className="mt-5 flex flex-wrap gap-2">
            {parseList(manga.genres).map((genre) => (
              <span
                key={genre}
                className="inline-flex items-center gap-1 rounded-full border border-zinc-700 px-2.5 py-1 text-xs text-zinc-300"
              >
                <Tag size={12} />
                {genre}
              </span>
            ))}
          </div>
          <div className="mt-6 flex flex-wrap gap-2">
            <Link
              to={`/reader/${encodeURIComponent(manga.id)}/${encodeURIComponent(resumeChapterId(data, chapters))}`}
              className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-4 py-2 text-sm font-semibold text-zinc-950"
            >
              <Play size={15} /> {data.progress ? "Continue" : "Read"}
            </Link>
            <button
              onClick={() => void toggleLibrary()}
              disabled={libraryBusy}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm disabled:opacity-50"
            >
              <Bookmark size={15} />{" "}
              {libraryBusy ? "Updating…" : manga.inLibrary ? "In library" : "Add to library"}
            </button>
            <button
              onClick={() => setModal("tracker")}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm"
            >
              <ExternalLink size={15} /> Tracking
            </button>
            <button
              onClick={() => setModal("migration")}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm"
            >
              <RefreshCw size={15} /> Migrate
            </button>
            <button
              aria-label="Refresh details"
              disabled={refreshing}
              onClick={() => void refresh()}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm disabled:opacity-50"
            >
              <RotateCw size={15} className={refreshing ? "animate-spin" : ""} /> Refresh
            </button>
            {manga.detailsFetchedAt && (
              <small className="self-center text-xs text-zinc-500">
                Updated {relativeTime(manga.detailsFetchedAt)}
              </small>
            )}
          </div>
          {refreshError && <p className="mt-3 text-xs text-red-300">{refreshError}</p>}
          {actionError && <p className="mt-3 text-xs text-red-300">{actionError}</p>}
        </div>
      </section>
      {categoryError && (
        <p role="alert" className="mt-8 text-xs text-red-300">
          {categoryError}
        </p>
      )}
      {categories.length > 0 && (
        <section className="mt-8">
          <PageHeader title="Categories" />{" "}
          <div className="flex flex-wrap gap-2">
            {categories.map((category) => {
              const active = data.categories.some((item) => item.id === category.id);
              return (
                <button
                  key={category.id}
                  onClick={() => void toggleCategory(category, active)}
                  disabled={categoryBusy !== undefined}
                  className={`rounded-full border px-3 py-2 text-xs disabled:opacity-50 ${active ? "border-amber-400 bg-amber-400 text-zinc-950" : "border-zinc-800 text-zinc-400"}`}
                >
                  {category.name}
                </button>
              );
            })}
          </div>
        </section>
      )}
      <section className="mt-10">
        <PageHeader title="Chapters">
          <div className="flex flex-wrap gap-2">
            <button
              onClick={() => setSelected(chapters.map((item) => item.id))}
              className="rounded-lg border border-zinc-700 px-3 py-2 text-xs"
            >
              Select all
            </button>
            <button
              onClick={() => void enqueue()}
              disabled={enqueueing || selected.length === 0}
              title="Select chapters to download"
              className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-xs font-semibold text-zinc-950 disabled:opacity-50"
            >
              <Download size={14} /> {enqueueing ? "Queueing…" : "Download"}
            </button>
          </div>
        </PageHeader>
        {enqueueError && (
          <p role="alert" className="mb-4 text-xs text-red-300">
            {enqueueError}
          </p>
        )}
        {queuedNote && <p className="mb-4 text-xs text-emerald-300">{queuedNote}</p>}
        {languages.length > 1 && (
          <div className="mb-5 flex flex-wrap gap-2">
            <button
              onClick={() => setLanguageFilter(undefined)}
              className={`rounded-full border px-3 py-1.5 text-xs ${languageFilter === undefined ? "border-amber-400 bg-amber-400 text-zinc-950" : "border-zinc-800 text-zinc-400 hover:border-zinc-600"}`}
            >
              All languages
            </button>
            {languages.map((code) => (
              <button
                key={code}
                onClick={() => setLanguageFilter(code)}
                className={`rounded-full border px-3 py-1.5 text-xs ${languageFilter === code ? "border-amber-400 bg-amber-400 text-zinc-950" : "border-zinc-800 text-zinc-400 hover:border-zinc-600"}`}
              >
                {languageLabel(code)}
              </button>
            ))}
          </div>
        )}
        {visible.length ? (
          groups.map((group) => (
            <section key={group.label} className="mt-5 first:mt-0">
              <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-400">
                {group.label}
              </h3>
              <div className="divide-y divide-zinc-800 rounded-xl border border-zinc-800 bg-zinc-900/40">
                {group.chapters.map((chapter) => (
                  <label key={chapter.id} className="flex items-center gap-3 p-4 hover:bg-zinc-900">
                    <input
                      type="checkbox"
                      checked={selected.includes(chapter.id)}
                      onChange={() => toggle(chapter.id)}
                      className="accent-amber-400"
                    />
                    <Link
                      to={`/reader/${encodeURIComponent(manga.id)}/${encodeURIComponent(chapter.id)}`}
                      className="min-w-0 flex-1"
                    >
                      <b className="block text-sm">
                        {formatChapter(chapter.volume, chapter.chapterNumber, chapter.title)}
                      </b>
                      <span className="text-xs text-zinc-500">{chapterMeta(chapter)}</span>
                    </Link>
                    {chapter.downloaded && <Check size={16} className="text-emerald-400" />}
                  </label>
                ))}
              </div>
            </section>
          ))
        ) : (
          <EmptyState
            title="No chapters"
            text="This plugin returned no chapter metadata for the title."
          />
        )}
      </section>
      {error && (
        <div className="mt-5">
          <ErrorState message={error} />
        </div>
      )}
      {modal === "tracker" && (
        <TrackerModal
          mangaId={manga.id}
          bindings={data.trackers ?? []}
          onClose={() => setModal(undefined)}
          onChanged={reload}
        />
      )}
      {modal === "migration" && (
        <MigrationModal
          manga={manga}
          onClose={() => setModal(undefined)}
          onApplied={(nextMangaId) => {
            // Migration keeps the canonical id, so the route often stays the
            // same: refetch explicitly instead of relying on remounting.
            void reload();
            navigate(`/manga/${encodeURIComponent(nextMangaId)}`);
          }}
        />
      )}
    </div>
  );
}

function formatChapter(volume?: number, number?: number, title?: string) {
  const label = number == null ? title || "Special" : `Chapter ${number}`;
  return volume == null ? label : `Vol. ${volume} · ${label}`;
}

// Groups chapters into volume sections in descending order, with chapters
// numbered descending inside each group and specials at the end.
function groupByVolume(chapters: Chapter[]) {
  const byVolume = new Map<number | null, Chapter[]>();
  for (const chapter of chapters) {
    const key = chapter.volume ?? null;
    const bucket = byVolume.get(key);
    if (bucket) bucket.push(chapter);
    else byVolume.set(key, [chapter]);
  }
  const keys = Array.from(byVolume.keys());
  const volumes = keys.filter((key): key is number => key != null).sort((a, b) => b - a);
  const ordered: Array<{ key: number | null; label: string }> = volumes.map((volume) => ({
    key: volume as number | null,
    label: `Volume ${volume}`,
  }));
  if (keys.includes(null)) ordered.push({ key: null, label: "No volume" });
  return ordered.map(({ key, label }) => {
    const bucket = byVolume.get(key) ?? [];
    const sorted = [...bucket].sort((a, b) => {
      if (a.chapterNumber == null && b.chapterNumber == null) return 0;
      if (a.chapterNumber == null) return 1;
      if (b.chapterNumber == null) return -1;
      return b.chapterNumber - a.chapterNumber;
    });
    return { label, chapters: sorted };
  });
}

// resumeChapterId picks where the Read/Continue action opens: the saved
// chapter when its progress still points at this title, otherwise the first
// listed chapter.
function resumeChapterId(data: Aggregate, chapters: Chapter[]): string {
  const saved = data.progress?.lastReadChapterId;
  if (saved && chapters.some((chapter) => chapter.id === saved)) {
    return saved;
  }
  return chapters[0]?.id || "";
}

const LANGUAGE_NAMES: Record<string, string> = {
  ar: "Arabic",
  bg: "Bulgarian",
  bn: "Bengali",
  ca: "Catalan",
  cs: "Czech",
  da: "Danish",
  de: "German",
  el: "Greek",
  en: "English",
  es: "Spanish",
  "es-la": "Spanish (Latin America)",
  fa: "Persian",
  fi: "Finnish",
  fr: "French",
  he: "Hebrew",
  hi: "Hindi",
  hr: "Croatian",
  hu: "Hungarian",
  id: "Indonesian",
  it: "Italian",
  ja: "Japanese",
  ko: "Korean",
  ms: "Malay",
  nl: "Dutch",
  no: "Norwegian",
  pl: "Polish",
  pt: "Portuguese",
  "pt-br": "Portuguese (Brazil)",
  ro: "Romanian",
  ru: "Russian",
  sv: "Swedish",
  ta: "Tamil",
  th: "Thai",
  tl: "Tagalog",
  tr: "Turkish",
  uk: "Ukrainian",
  vi: "Vietnamese",
  zh: "Chinese",
  "zh-cn": "Chinese (Simplified)",
  "zh-tw": "Chinese (Traditional)",
};

function languageLabel(code: string) {
  return LANGUAGE_NAMES[code.toLowerCase()] || code;
}

function chapterMeta(chapter: Chapter) {
  return [chapter.language ? languageLabel(chapter.language) : "", chapter.scanlator || ""]
    .filter(Boolean)
    .join(" · ");
}
function parseList(value?: string) {
  if (!value) return [];
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return value
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
  }
}

function TrackerModal({
  mangaId,
  bindings,
  onClose,
  onChanged,
}: {
  mangaId: string;
  bindings: Binding[];
  onClose: () => void;
  onChanged: () => Promise<void>;
}) {
  const [trackers, setTrackers] = useState<TrackerInfo[]>([]);
  const [error, setError] = useState("");
  const refreshTrackers = useCallback(async () => {
    try {
      setTrackers(await api.trackers());
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load trackers");
    }
  }, []);
  useEffect(() => {
    void refreshTrackers();
  }, [refreshTrackers]);
  useTrackerEvents(() => {
    void refreshTrackers();
  });
  const visibleTrackers = trackers.filter((tracker) => tracker.configured && tracker.credential);
  return (
    <Modal title="Tracking" onClose={onClose}>
      <div className="space-y-3">
        {visibleTrackers.map((tracker) => (
          <TrackerSection
            key={tracker.name}
            mangaId={mangaId}
            tracker={tracker}
            binding={bindings.find((binding) => sameTracker(binding.trackerType, tracker.name))}
            onChanged={onChanged}
          />
        ))}
        {!visibleTrackers.length && !error && (
          <p className="text-sm text-zinc-500">No trackers available.</p>
        )}
      </div>
      {error && <p className="mt-3 text-sm text-red-300">{error}</p>}
    </Modal>
  );
}

function TrackerSection({
  mangaId,
  tracker,
  binding,
  onChanged,
}: {
  mangaId: string;
  tracker: TrackerInfo;
  binding?: Binding;
  onChanged: () => Promise<void>;
}) {
  const label = trackerLabel(tracker.name);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<
    Array<{ remoteId: string; title: string; status?: string }>
  >([]);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [searching, setSearching] = useState(false);
  const [bindingRemote, setBindingRemote] = useState<string>();
  const [menuOpen, setMenuOpen] = useState(false);
  const [confirmUnbind, setConfirmUnbind] = useState(false);
  const [unbinding, setUnbinding] = useState(false);
  const [refreshingStatus, setRefreshingStatus] = useState(false);
  const [liveStatus, setLiveStatus] = useState<import("../../types").TrackerStatus>();
  const searchSeq = useRef(0);
  const search = async () => {
    const seq = ++searchSeq.current;
    setSearching(true);
    setError("");
    setNote("");
    try {
      const found = await api.trackerSearch(tracker.name, query);
      if (seq === searchSeq.current) setResults(found);
    } catch (e) {
      if (seq === searchSeq.current)
        setError(e instanceof Error ? e.message : "Tracker search failed");
    } finally {
      if (seq === searchSeq.current) setSearching(false);
    }
  };
  const bind = async (item: { remoteId: string; title: string; status?: string }) => {
    setBindingRemote(item.remoteId);
    setError("");
    setNote("");
    try {
      await api.bindTracker(mangaId, tracker.name, {
        remoteId: item.remoteId,
        remoteTitle: item.title,
        remoteStatus: item.status,
      });
      setNote(`Bound to ${item.title}.`);
      await onChanged();
      setResults([]);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to bind tracker");
    } finally {
      setBindingRemote(undefined);
    }
  };
  const unbind = async () => {
    setUnbinding(true);
    setError("");
    setNote("");
    try {
      await api.unbindTracker(mangaId, tracker.name);
      setConfirmUnbind(false);
      setNote(`Unbound from ${label}.`);
      await onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to unbind tracker");
    } finally {
      setUnbinding(false);
    }
  };
  const refreshStatus = async () => {
    if (!binding) return;
    setRefreshingStatus(true);
    setError("");
    setNote("");
    try {
      const statuses = await api.trackerStatuses(mangaId);
      const status = statuses.find((item) => item.remoteId === binding.remoteId);
      if (!status) throw new Error(`${label} did not return tracking status`);
      setLiveStatus(status);
      setNote(`${label} status refreshed.`);
      setMenuOpen(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to refresh tracker status");
    } finally {
      setRefreshingStatus(false);
    }
  };
  const score = liveStatus?.score ?? binding?.remoteScore;
  return (
    <section className="rounded-xl border border-zinc-800 bg-zinc-950/40 p-4">
      <div className="flex items-start gap-3">
        <TrackerLogo name={tracker.name} className="size-11 shrink-0" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h3 className="font-semibold">{label}</h3>
            <span className="text-xs text-emerald-300">
              {tracker.connectedAs ? `Connected as ${tracker.connectedAs}` : "Connected"}
            </span>
          </div>
          {binding && (
            <p className="mt-0.5 truncate text-sm text-zinc-400">
              {liveStatus?.title || binding.remoteTitle}
            </p>
          )}
        </div>
        {binding && (
          <div className="relative">
            <button
              aria-label={`${label} actions`}
              onClick={() => setMenuOpen((open) => !open)}
              className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800 hover:text-white"
            >
              <Ellipsis size={17} />
            </button>
            {menuOpen && (
              <div className="absolute right-0 top-10 z-10 w-36 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-xl">
                <button
                  onClick={() => void refreshStatus()}
                  disabled={refreshingStatus}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-xs hover:bg-zinc-800 disabled:opacity-50"
                >
                  <RefreshCw size={13} className={refreshingStatus ? "animate-spin" : ""} /> Refresh
                </button>
                <button
                  onClick={() => {
                    setMenuOpen(false);
                    setConfirmUnbind(true);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-xs text-red-300 hover:bg-red-950/40"
                >
                  <X size={13} /> Unbind
                </button>
              </div>
            )}
          </div>
        )}
      </div>
      {binding ? (
        <TrackingEditor
          key={binding.id}
          mangaId={mangaId}
          tracker={tracker}
          binding={binding}
          status={liveStatus}
          score={score}
          onChanged={onChanged}
        />
      ) : (
        <div className="mt-4">
          <div className="flex gap-2">
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && query.trim()) void search();
              }}
              aria-label={`${label} search`}
              placeholder="Search provider"
              className="min-w-0 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
            />
            <button
              onClick={() => void search()}
              disabled={searching || !query.trim()}
              className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
            >
              {searching ? "Searching…" : "Search"}
            </button>
          </div>
          <div className="mt-2 grid gap-2">
            {results.map((item) => (
              <button
                key={item.remoteId}
                onClick={() => void bind(item)}
                disabled={bindingRemote !== undefined}
                className="rounded-lg border border-zinc-800 p-3 text-left hover:border-amber-400 disabled:opacity-50"
              >
                <b>{item.title}</b>
                <small className="ml-2 text-zinc-500">
                  {bindingRemote === item.remoteId ? "Binding…" : item.remoteId}
                </small>
              </button>
            ))}
          </div>
        </div>
      )}
      {confirmUnbind && binding && (
        <div className="mt-4 border-t border-red-900/50 pt-4">
          <p className="text-sm text-zinc-300">
            Unbind {label}? Reading progress stops syncing to it.
          </p>
          <div className="mt-3 flex justify-end gap-2">
            <button
              onClick={() => setConfirmUnbind(false)}
              disabled={unbinding}
              className="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm disabled:opacity-50"
            >
              Cancel
            </button>
            <button
              onClick={() => void unbind()}
              disabled={unbinding}
              className="rounded-lg bg-red-500 px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
            >
              {unbinding ? "Unbinding…" : "Confirm unbind"}
            </button>
          </div>
        </div>
      )}
      {error && <p className="mt-3 text-xs text-red-300">{error}</p>}
      {note && !error && <p className="mt-3 text-xs text-emerald-300">{note}</p>}
    </section>
  );
}

function TrackingEditor({
  mangaId,
  tracker,
  binding,
  status,
  score,
  onChanged,
}: {
  mangaId: string;
  tracker: TrackerInfo;
  binding: Binding;
  status?: import("../../types").TrackerStatus;
  score?: number;
  onChanged: () => Promise<void>;
}) {
  const label = trackerLabel(tracker.name);
  const [scoreValue, setScoreValue] = useState(score == null ? "" : String(score));
  const [startedAt, setStartedAt] = useState(formatTrackerDate(binding.startedAt));
  const [finishedAt, setFinishedAt] = useState(formatTrackerDate(binding.finishedAt));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  useEffect(() => {
    setScoreValue(score == null ? "" : String(score));
  }, [score]);
  const save = async () => {
    const nextScore = scoreValue === "" ? undefined : Number(scoreValue);
    if (
      nextScore !== undefined &&
      (!Number.isFinite(nextScore) || nextScore < 0 || nextScore > 10)
    ) {
      setError("Score must be between 0 and 10.");
      return;
    }
    setSaving(true);
    setError("");
    setNote("");
    try {
      await api.updateTrackerBinding(mangaId, tracker.name, {
        ...(nextScore === undefined ? {} : { score: nextScore }),
        ...(startedAt ? { startedAt: parseTrackerDate(startedAt) } : {}),
        ...(finishedAt ? { finishedAt: parseTrackerDate(finishedAt) } : {}),
      });
      await onChanged();
      setNote(`${label} tracking updated.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to update tracking");
    } finally {
      setSaving(false);
    }
  };
  const progress = status?.progress ?? binding.lastSyncedChapter;
  const total = status?.totalChapters ?? binding.totalRemoteChapters;
  return (
    <div className="mt-4">
      <div className="grid grid-cols-2 gap-x-3 gap-y-4 border-y border-zinc-800 py-4 sm:grid-cols-5">
        <TrackerStat label="Status" value={status?.status || binding.remoteStatus || "Unknown"} />
        <TrackerStat
          label="Progress"
          value={`${formatTrackerNumber(progress)}${total == null ? "" : ` / ${total}`}`}
        />
        <label className="min-w-0">
          <span className="block text-[11px] uppercase text-zinc-500">Score</span>
          <input
            type="number"
            min="0"
            max="10"
            step="0.1"
            value={scoreValue}
            onChange={(event) => setScoreValue(event.target.value)}
            aria-label={`${label} score`}
            className="mt-1 w-full rounded-md border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-sm"
          />
        </label>
        <label className="min-w-0">
          <span className="block text-[11px] uppercase text-zinc-500">Started</span>
          <input
            type="date"
            value={startedAt}
            onChange={(event) => setStartedAt(event.target.value)}
            aria-label={`${label} start date`}
            className="mt-1 w-full rounded-md border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-xs"
          />
        </label>
        <label className="min-w-0">
          <span className="block text-[11px] uppercase text-zinc-500">Finished</span>
          <input
            type="date"
            value={finishedAt}
            onChange={(event) => setFinishedAt(event.target.value)}
            aria-label={`${label} finish date`}
            className="mt-1 w-full rounded-md border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-xs"
          />
        </label>
      </div>
      <div className="mt-3 flex items-center justify-between gap-3">
        <div>
          {error && <p className="text-xs text-red-300">{error}</p>}
          {note && !error && <p className="text-xs text-emerald-300">{note}</p>}
        </div>
        <button
          onClick={() => void save()}
          disabled={saving}
          aria-label={`Save ${label} tracking`}
          className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-xs font-semibold disabled:opacity-50"
        >
          {saving ? <LoaderCircle size={13} className="animate-spin" /> : <Save size={13} />}
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}

function TrackerStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <span className="block text-[11px] uppercase text-zinc-500">{label}</span>
      <b className="mt-1 block truncate text-sm font-medium text-zinc-200">{value}</b>
    </div>
  );
}

function sameTracker(left: string, right: string) {
  return left.toLowerCase() === right.toLowerCase();
}

function formatTrackerDate(value?: number) {
  return value ? new Date(value * 1000).toISOString().slice(0, 10) : "";
}

function parseTrackerDate(value: string) {
  return Date.parse(`${value}T00:00:00Z`) / 1000;
}

function formatTrackerNumber(value: number) {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

function MigrationModal({
  manga,
  onClose,
  onApplied,
}: {
  manga: SourceManga;
  onClose: () => void;
  onApplied: (mangaId: string) => void;
}) {
  const [items, setItems] = useState<MigrationCandidate[]>([]);
  const [failedSources, setFailedSources] = useState(0);
  const [selected, setSelected] = useState<MigrationCandidate>();
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    void api
      .migrationCandidates(manga.id)
      .then((payload) => {
        setItems(payload.candidates);
        setFailedSources(payload.failedSources);
      })
      .catch((e) => setError(e.message));
  }, [manga.id]);
  return (
    <Modal title="Migrate plugin" onClose={onClose}>
      <p className="text-sm text-zinc-400">
        Select a matching title from another installed plugin. Reading state and tracker bindings
        are preserved.
      </p>
      {error && <p className="mt-3 text-sm text-red-300">{error}</p>}
      {failedSources > 0 && !error && (
        <p className="mt-3 text-sm text-red-300">
          {failedSources} plugin{failedSources === 1 ? "" : "s"} failed to respond.
        </p>
      )}
      <div className="mt-4 grid gap-2">
        {items.map((item) => (
          <button
            key={`${item.source.id}:${item.result.id}`}
            onClick={() => setSelected(item)}
            className={`flex items-center gap-3 rounded-lg border p-3 text-left ${selected?.result.id === item.result.id ? "border-amber-400 bg-amber-400/10" : "border-zinc-800"}`}
          >
            <span className="grid size-9 place-items-center rounded-full bg-zinc-800 text-xs">
              {item.source.name.slice(0, 2).toUpperCase()}
            </span>
            <span>
              <b className="block">{item.result.title}</b>
              <small className="text-zinc-500">{item.source.name}</small>
            </span>
          </button>
        ))}
        {!items.length && !error && (
          <p className="text-sm text-zinc-500">No replacement candidates found.</p>
        )}
      </div>
      <div className="mt-5 flex justify-end gap-2">
        <button onClick={onClose} className="rounded-lg border border-zinc-700 px-3 py-2 text-sm">
          Cancel
        </button>
        <button
          disabled={!selected || applying}
          onClick={async () => {
            if (!selected || applying) return;
            setApplying(true);
            setError("");
            try {
              // Applying re-fetches the replacement's chapters server-side,
              // so the round-trip can take a while.
              const result = await api.applyMigration(
                manga.id,
                selected.source.id,
                selected.result.id,
              );
              onApplied(result.manga.manga.id);
            } catch (e) {
              setError(e instanceof Error ? e.message : "Unable to apply migration");
            } finally {
              setApplying(false);
            }
          }}
          className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-50"
        >
          {applying && <LoaderCircle size={14} className="animate-spin" />}
          {applying ? "Migrating…" : "Apply"}
        </button>
      </div>
    </Modal>
  );
}

type SourceManga = Aggregate["manga"];
