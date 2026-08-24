import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  Bookmark,
  Check,
  Download,
  ExternalLink,
  LoaderCircle,
  Play,
  RefreshCw,
  RotateCw,
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

export function DetailsPage() {
  const { mangaId = "" } = useParams();
  const decodedManga = decodeURIComponent(mangaId);
  const navigate = useNavigate();
  const [data, setData] = useState<Aggregate>();
  const [categories, setCategories] = useState<Category[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [range, setRange] = useState("");
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
      .catch(() => undefined);
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
      await load();
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
      await load();
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
      await api.enqueue(manga.id, selected, range, manga.downloadFormat);
      setQueuedNote("Chapters queued for download.");
      await load();
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
              to={`/reader/${encodeURIComponent(manga.id)}/${encodeURIComponent(chapters[0]?.id || "")}`}
              className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-4 py-2 text-sm font-semibold text-zinc-950"
            >
              <Play size={15} /> Read
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
            <input
              value={range}
              onChange={(e) => setRange(e.target.value)}
              placeholder="Range 1-10"
              className="w-28 rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-xs"
            />
            <button
              onClick={() => setSelected(chapters.map((item) => item.id))}
              className="rounded-lg border border-zinc-700 px-3 py-2 text-xs"
            >
              Select all
            </button>
            <button
              onClick={() => void enqueue()}
              disabled={enqueueing}
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
          bindings={data.trackers}
          onClose={() => setModal(undefined)}
          onChanged={load}
        />
      )}
      {modal === "migration" && (
        <MigrationModal
          manga={manga}
          onClose={() => setModal(undefined)}
          onApplied={(nextMangaId) => navigate(`/manga/${encodeURIComponent(nextMangaId)}`)}
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

function relativeTime(unix: number) {
  const seconds = Math.max(0, Math.floor(Date.now() / 1000) - unix);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 365) return `${days}d ago`;
  return `${Math.floor(days / 365)}y ago`;
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
  const [active, setActive] = useState("");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<
    Array<{ remoteId: string; title: string; status?: string }>
  >([]);
  const [error, setError] = useState("");
  const [unbinding, setUnbinding] = useState(false);
  const [confirmUnbind, setConfirmUnbind] = useState(false);
  const [bindingRemote, setBindingRemote] = useState<string>();
  useEffect(() => {
    void api
      .trackers()
      .then((items) => {
        setTrackers(items);
        setActive(items[0]?.name || "");
      })
      .catch((e) => setError(e.message));
  }, []);
  const binding = bindings.find((item) => item.trackerType === active);
  const search = async () => {
    try {
      setResults(await api.trackerSearch(active, query));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Tracker search failed");
    }
  };
  // Unbinding severs the tracker link and stops syncing, so it asks for
  // confirmation and reports its outcome inline.
  const unbind = async () => {
    setUnbinding(true);
    setError("");
    try {
      await api.unbindTracker(mangaId, active);
      setConfirmUnbind(false);
      await onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to unbind tracker");
    } finally {
      setUnbinding(false);
    }
  };
  const bind = async (item: { remoteId: string; title: string; status?: string }) => {
    setBindingRemote(item.remoteId);
    setError("");
    try {
      await api.bindTracker(mangaId, active, {
        remoteId: item.remoteId,
        remoteTitle: item.title,
        remoteStatus: item.status,
      });
      await onChanged();
      setResults([]);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to bind tracker");
    } finally {
      setBindingRemote(undefined);
    }
  };
  return (
    <Modal title="Tracking" onClose={onClose}>
      <div className="grid gap-2 sm:grid-cols-2">
        {trackers.map((item) => (
          <button
            key={item.name}
            onClick={() => setActive(item.name)}
            className={`rounded-lg border p-3 text-left ${active === item.name ? "border-amber-400 bg-amber-400/10" : "border-zinc-800"}`}
          >
            <b className="block">{item.name}</b>
            <small className="text-zinc-500">
              {item.credential
                ? "Connected"
                : item.capabilities.oauth
                  ? "OAuth available"
                  : "Token required"}
            </small>
          </button>
        ))}
      </div>
      {active && !trackers.find((item) => item.name === active)?.capabilities.search && (
        <p className="mt-4 text-sm text-zinc-400">This provider does not support title search.</p>
      )}
      {active && trackers.find((item) => item.name === active)?.capabilities.search && (
        <div className="mt-4 flex gap-2">
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search provider"
            className="min-w-0 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
          />
          <button
            onClick={() => void search()}
            className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
          >
            Search
          </button>
        </div>
      )}
      {error && <p className="mt-3 text-sm text-red-300">{error}</p>}
      {binding && !confirmUnbind && (
        <button
          onClick={() => setConfirmUnbind(true)}
          disabled={unbinding}
          className="mt-4 inline-flex items-center gap-2 rounded-lg border border-red-900 px-3 py-2 text-sm text-red-300"
        >
          <X size={14} /> Unbind
        </button>
      )}
      {binding && confirmUnbind && (
        <div className="mt-4 rounded-lg border border-red-900/60 bg-red-950/30 p-3">
          <p className="text-sm text-zinc-300">
            Unbind {active}? Reading progress stops syncing to it.
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
      <div className="mt-4 grid gap-2">
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
    </Modal>
  );
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
  const [selected, setSelected] = useState<MigrationCandidate>();
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    void api
      .migrationCandidates(manga.id)
      .then(setItems)
      .catch((e) => setError(e.message));
  }, [manga.id]);
  return (
    <Modal title="Migrate plugin" onClose={onClose}>
      <p className="text-sm text-zinc-400">
        Select a matching title from another installed plugin. Reading state and tracker bindings
        are preserved.
      </p>
      {error && <p className="mt-3 text-sm text-red-300">{error}</p>}
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
