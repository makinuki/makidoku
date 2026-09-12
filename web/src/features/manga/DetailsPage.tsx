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
  Layers,
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
  Recommendation,
  TrackerSearchResult,
  TrackerStatus,
  TrackerInfo,
} from "../../types";
import { CoverImg } from "../../components/CoverImg";
import { Modal } from "../../components/Modal";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { formatTimestamp } from "../../time";
import { useDateFormat } from "../../hooks/useDateFormat";
import { TrackerLogo, trackerLabel } from "../../components/TrackerLogo";
import { useTrackerEvents } from "../../hooks/useTrackerEvents";
import { CustomInfoModal } from "./CustomInfoModal";
import { SourcesMetadataModal } from "./SourcesMetadataModal";

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
  const [modal, setModal] = useState<"tracker" | "migration" | "custom" | "sources">();
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState("");
  const [actionError, setActionError] = useState("");
  const [libraryBusy, setLibraryBusy] = useState(false);
  const [categoryBusy, setCategoryBusy] = useState<number>();
  const [enqueueing, setEnqueueing] = useState(false);
  const [enqueueError, setEnqueueError] = useState("");
  const [queuedNote, setQueuedNote] = useState("");
  const [languageFilter, setLanguageFilter] = useState<string>();
  const [chapterSort, setChapterSort] = useState<"number-desc" | "number-asc" | "source">(
    "number-desc",
  );
  const [chapterFilter, setChapterFilter] = useState<
    "all" | "unread" | "downloaded" | "bookmarked"
  >("all");
  const [autoDownloadBusy, setAutoDownloadBusy] = useState(false);
  const [suggestions, setSuggestions] = useState<Recommendation[]>([]);
  const [suggestionsLoading, setSuggestionsLoading] = useState(false);
  const [suggestionsLoaded, setSuggestionsLoaded] = useState(false);
  const dateFormat = useDateFormat();
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
  const visible = chapters.filter((chapter) => {
    if (languageFilter && chapter.language !== languageFilter) return false;
    if (chapterFilter === "unread" && chapter.read) return false;
    if (chapterFilter === "downloaded" && !chapter.downloaded) return false;
    if (chapterFilter === "bookmarked" && !chapter.bookmark) return false;
    return true;
  });
  const groups = groupByVolume(visible, chapterSort);
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
  const setRead = async (chapterIds: string[], read: boolean) => {
    setActionError("");
    try {
      await api.setMangaChaptersRead(manga.id, chapterIds, read);
      await reload();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Unable to update read state");
    }
  };
  const toggleAutoDownload = async () => {
    setAutoDownloadBusy(true);
    setActionError("");
    try {
      await api.setAutoDownload(manga.id, !manga.downloadNewChapters);
      await reload();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Unable to update automatic downloads");
    } finally {
      setAutoDownloadBusy(false);
    }
  };
  const loadSuggestions = async () => {
    setSuggestionsLoading(true);
    setActionError("");
    try {
      const items = await api.suggestions(manga.id);
      setSuggestions(items);
      setSuggestionsLoaded(true);
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Unable to load suggestions");
    } finally {
      setSuggestionsLoading(false);
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
          {(manga.authors || manga.artists || manga.altTitles) && (
            <dl className="mt-4 grid max-w-3xl gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
              {manga.authors && (
                <div>
                  <dt className="text-xs text-zinc-500">Authors</dt>
                  <dd className="text-zinc-300">{parseList(manga.authors).join(", ")}</dd>
                </div>
              )}
              {manga.artists && (
                <div>
                  <dt className="text-xs text-zinc-500">Artists</dt>
                  <dd className="text-zinc-300">{parseList(manga.artists).join(", ")}</dd>
                </div>
              )}
              {manga.altTitles && (
                <div className="sm:col-span-2">
                  <dt className="text-xs text-zinc-500">Alternative titles</dt>
                  <dd className="text-zinc-300">{parseList(manga.altTitles).join(" · ")}</dd>
                </div>
              )}
            </dl>
          )}
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
            {data.sourceUrl && (
              <a
                href={data.sourceUrl}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm"
              >
                <ExternalLink size={15} /> Open source site
              </a>
            )}
            <button
              onClick={() => setModal("tracker")}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm"
            >
              <ExternalLink size={15} /> Tracking
            </button>
            <button
              onClick={() => setModal("custom")}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm"
            >
              <Tag size={15} /> Custom info
            </button>
            <button
              onClick={() => setModal("sources")}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm"
            >
              <Layers size={15} /> Sources
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
                Updated {formatTimestamp(manga.detailsFetchedAt, dateFormat)}
              </small>
            )}
          </div>
          {(data.readingSeconds ?? 0) > 0 && (
            <p className="mt-4 text-xs text-zinc-500">
              Reading time: {formatReadingTime(data.readingSeconds ?? 0)}
            </p>
          )}
          <button
            type="button"
            role="switch"
            aria-checked={Boolean(manga.downloadNewChapters)}
            disabled={autoDownloadBusy}
            onClick={() => void toggleAutoDownload()}
            className="mt-4 inline-flex items-center gap-2 text-sm text-zinc-300 disabled:opacity-50"
          >
            <span
              className={`relative h-5 w-9 rounded-full border transition-colors ${
                manga.downloadNewChapters
                  ? "border-amber-400 bg-amber-400"
                  : "border-zinc-700 bg-zinc-900"
              }`}
            >
              <span
                className={`absolute top-0.5 size-3.5 rounded-full bg-zinc-950 transition-transform ${
                  manga.downloadNewChapters ? "translate-x-4" : "translate-x-0.5"
                }`}
              />
            </span>
            Automatically download new chapters
          </button>
          {refreshError && <p className="mt-3 text-xs text-red-300">{refreshError}</p>}
          {data.refreshError && (
            <p role="status" className="mt-3 text-xs text-amber-300">
              Showing saved details. The source could not be refreshed: {data.refreshError}
            </p>
          )}
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
              onClick={() =>
                void setRead(
                  visible.map((item) => item.id),
                  true,
                )
              }
              disabled={visible.length === 0}
              className="rounded-lg border border-zinc-700 px-3 py-2 text-xs disabled:opacity-50"
            >
              Mark visible read
            </button>
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
        <div className="mb-5 flex flex-wrap items-center gap-3">
          <label className="flex items-center gap-2 text-xs text-zinc-400">
            <span>Sort</span>
            <select
              aria-label="Chapter sort"
              value={chapterSort}
              onChange={(event) => setChapterSort(event.target.value as typeof chapterSort)}
              className="rounded-lg border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-xs text-zinc-200"
            >
              <option value="number-desc">Number, newest first</option>
              <option value="number-asc">Number, oldest first</option>
              <option value="source">Source order</option>
            </select>
          </label>
          <label className="flex items-center gap-2 text-xs text-zinc-400">
            <span>Show</span>
            <select
              aria-label="Chapter filter"
              value={chapterFilter}
              onChange={(event) => setChapterFilter(event.target.value as typeof chapterFilter)}
              className="rounded-lg border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-xs text-zinc-200"
            >
              <option value="all">All chapters</option>
              <option value="unread">Unread only</option>
              <option value="downloaded">Downloaded only</option>
              <option value="bookmarked">Bookmarked only</option>
            </select>
          </label>
        </div>
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
                  <div
                    key={chapter.id}
                    className={`flex items-center gap-3 p-4 hover:bg-zinc-900 ${chapter.read ? "text-zinc-500" : ""}`}
                  >
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
                    <button
                      type="button"
                      aria-label={`${chapter.bookmark ? "Remove bookmark from" : "Bookmark"} ${formatChapter(chapter.volume, chapter.chapterNumber, chapter.title)}`}
                      onClick={() =>
                        void api
                          .setChapterBookmark(chapter.id, !chapter.bookmark)
                          .then(reload)
                          .catch((e) =>
                            setActionError(
                              e instanceof Error ? e.message : "Unable to update the bookmark",
                            ),
                          )
                      }
                      className={`rounded-lg p-1.5 ${chapter.bookmark ? "text-amber-300" : "text-zinc-600 hover:text-amber-300"}`}
                    >
                      <Bookmark size={16} />
                    </button>
                    <button
                      type="button"
                      aria-label={`${chapter.read ? "Mark unread" : "Mark read"} ${formatChapter(chapter.volume, chapter.chapterNumber, chapter.title)}`}
                      onClick={() =>
                        void api
                          .setChapterRead(chapter.id, !chapter.read)
                          .then(reload)
                          .catch((e) =>
                            setActionError(
                              e instanceof Error ? e.message : "Unable to update read state",
                            ),
                          )
                      }
                      className={`rounded-lg p-1.5 ${chapter.read ? "text-amber-300" : "text-zinc-600 hover:text-amber-300"}`}
                    >
                      <Check size={16} />
                    </button>
                  </div>
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
      {(data.trackers ?? []).some((binding) => sameTracker(binding.trackerType, "anilist")) && (
        <section className="mt-10">
          <PageHeader title="Suggestions">
            {!suggestionsLoaded && (
              <button
                onClick={() => void loadSuggestions()}
                disabled={suggestionsLoading}
                className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-xs disabled:opacity-50"
              >
                {suggestionsLoading && <LoaderCircle size={13} className="animate-spin" />}
                {suggestionsLoading ? "Loading…" : "See recommendations"}
              </button>
            )}
          </PageHeader>
          {suggestionsLoaded && suggestions.length > 0 && (
            <>
              <div className="flex gap-3 overflow-x-auto pb-2">
                {suggestions.map((item) => (
                  <article key={item.remoteId} className="w-36 shrink-0">
                    <div className="aspect-3/4 overflow-hidden rounded-lg bg-zinc-900">
                      {item.coverUrl ? (
                        <img src={item.coverUrl} alt="" className="size-full object-cover" />
                      ) : (
                        <div className="grid size-full place-items-center text-xs text-zinc-600">
                          No cover
                        </div>
                      )}
                    </div>
                    <p className="mt-2 line-clamp-2 text-xs font-medium">{item.title}</p>
                    {item.score != null && (
                      <small className="text-zinc-500">Score {item.score.toFixed(1)}</small>
                    )}
                  </article>
                ))}
              </div>
              <Link
                to={`/manga/${encodeURIComponent(manga.id)}/recommendations`}
                className="mt-3 inline-flex text-xs text-amber-300 hover:text-amber-200"
              >
                See all recommendations
              </Link>
            </>
          )}
          {suggestionsLoaded && suggestions.length === 0 && (
            <p className="text-sm text-zinc-500">No recommendations available.</p>
          )}
        </section>
      )}
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
      {modal === "custom" && (
        <CustomInfoModal manga={manga} onClose={() => setModal(undefined)} onSaved={reload} />
      )}
      {modal === "sources" && (
        <SourcesMetadataModal
          mangaId={manga.id}
          title={manga.displayTitle ?? manga.title}
          onClose={() => setModal(undefined)}
        />
      )}
    </div>
  );
}

function formatReadingTime(seconds: number) {
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder ? `${hours}h ${remainder}m` : `${hours}h`;
}

function formatChapter(volume?: number, number?: number, title?: string) {
  const label = number == null ? title || "Special" : `Chapter ${number}`;
  return volume == null ? label : `Vol. ${volume} · ${label}`;
}

// Groups chapters into volume sections in descending order, with chapters
// numbered descending inside each group and specials at the end.
function groupByVolume(
  chapters: Chapter[],
  sort: "number-desc" | "number-asc" | "source" = "number-desc",
) {
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
      if (sort === "source") return 0;
      if (a.chapterNumber == null && b.chapterNumber == null) return 0;
      if (a.chapterNumber == null) return 1;
      if (b.chapterNumber == null) return -1;
      return sort === "number-asc"
        ? a.chapterNumber - b.chapterNumber
        : b.chapterNumber - a.chapterNumber;
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
  const [statuses, setStatuses] = useState<TrackerStatus[]>([]);
  const [error, setError] = useState("");
  const refreshTrackers = useCallback(async () => {
    try {
      setTrackers(await api.trackers());
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load trackers");
    }
  }, []);
  const refreshStatuses = useCallback(async () => {
    try {
      setStatuses(await api.trackerStatuses(mangaId));
    } catch {
      // Status data enriches the modal but should not prevent local bindings from rendering.
      setStatuses([]);
    }
  }, [mangaId]);
  const bindingKey = bindings
    .map((binding) => `${binding.trackerType}:${binding.remoteId}`)
    .sort()
    .join("|");
  useEffect(() => {
    void refreshTrackers();
  }, [refreshTrackers]);
  useEffect(() => {
    void refreshStatuses();
  }, [bindingKey, refreshStatuses]);
  useTrackerEvents(() => {
    void refreshTrackers();
  });
  const visibleTrackers = trackers.filter((tracker) => tracker.configured && tracker.credential);
  return (
    <Modal title="Tracking" onClose={onClose}>
      <div className="space-y-3">
        {visibleTrackers.map((tracker) => {
          const binding = bindings.find((item) => sameTracker(item.trackerType, tracker.name));
          return (
            <TrackerSection
              key={tracker.name}
              mangaId={mangaId}
              tracker={tracker}
              binding={binding}
              status={
                binding
                  ? statuses.find(
                      (item) =>
                        (!item.trackerType || sameTracker(item.trackerType, binding.trackerType)) &&
                        item.remoteId === binding.remoteId,
                    )
                  : undefined
              }
              onChanged={onChanged}
            />
          );
        })}
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
  status,
  onChanged,
}: {
  mangaId: string;
  tracker: TrackerInfo;
  binding?: Binding;
  status?: TrackerStatus;
  onChanged: () => Promise<void>;
}) {
  const label = trackerLabel(tracker.name);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<TrackerSearchResult[]>([]);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [searching, setSearching] = useState(false);
  const [bindingRemote, setBindingRemote] = useState<string>();
  const [menuOpen, setMenuOpen] = useState(false);
  const [confirmUnbind, setConfirmUnbind] = useState(false);
  const [unbinding, setUnbinding] = useState(false);
  const [refreshingStatus, setRefreshingStatus] = useState(false);
  const [liveStatus, setLiveStatus] = useState<TrackerStatus>();
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
  const bind = async (item: TrackerSearchResult) => {
    setBindingRemote(item.remoteId);
    setError("");
    setNote("");
    try {
      await api.bindTracker(mangaId, tracker.name, {
        remoteId: item.remoteId,
        remoteTitle: item.title,
        totalRemoteChapters: item.chapters,
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
  const currentStatus = liveStatus ?? status;
  const score = binding?.remoteScore ?? currentStatus?.score;
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
              {currentStatus?.title || binding.remoteTitle}
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
          status={currentStatus}
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
  const scoreEdited = useRef(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  useEffect(() => {
    if (!scoreEdited.current && scoreValue === "" && score != null) {
      setScoreValue(String(score));
    }
  }, [score, scoreValue]);
  useEffect(() => {
    if (!startedAt && status?.startedAt) setStartedAt(formatTrackerDate(status.startedAt));
    if (!finishedAt && status?.finishedAt) setFinishedAt(formatTrackerDate(status.finishedAt));
  }, [finishedAt, startedAt, status?.finishedAt, status?.startedAt]);
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
            onChange={(event) => {
              scoreEdited.current = true;
              setScoreValue(event.target.value);
            }}
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

export function MigrationModal({
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
