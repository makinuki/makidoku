import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  Bookmark,
  Check,
  Download,
  ExternalLink,
  Play,
  RefreshCw,
  Tag,
  X,
} from "lucide-react";
import { api } from "../../api";
import type { Aggregate, Binding, Category, MigrationCandidate, TrackerInfo } from "../../types";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";

export function DetailsPage() {
  const { sourceId = "", mangaId = "" } = useParams();
  const decodedSource = decodeURIComponent(sourceId);
  const decodedManga = decodeURIComponent(mangaId);
  const navigate = useNavigate();
  const [data, setData] = useState<Aggregate>();
  const [categories, setCategories] = useState<Category[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [range, setRange] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [modal, setModal] = useState<"tracker" | "migration">();
  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setData(await api.manga(decodedSource, decodedManga));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load title");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    void load();
    void api
      .categories()
      .then(setCategories)
      .catch(() => undefined);
  }, [decodedSource, decodedManga]);
  if (loading) return <LoadingState label="Loading title" />;
  if (error || !data)
    return (
      <div className="mx-auto max-w-5xl p-5 sm:p-8">
        <ErrorState message={error || "Title not found"} />
      </div>
    );
  const { manga, chapters } = data;
  const toggle = (id: string) =>
    setSelected((items) =>
      items.includes(id) ? items.filter((item) => item !== id) : [...items, id],
    );
  const enqueue = async () => {
    try {
      await api.enqueue(manga.sourceId, manga.sourceMangaId, selected, range, manga.downloadFormat);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to enqueue chapters");
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
        <div className="aspect-[3/4] overflow-hidden rounded-xl bg-zinc-800">
          {manga.coverUrl && <img src={manga.coverUrl} alt="" className="size-full object-cover" />}
        </div>
        <div>
          <p className="text-xs uppercase tracking-[0.18em] text-amber-400">
            {manga.sourceId} · {manga.status || "Unknown status"}
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
              to={`/reader/${encodeURIComponent(manga.sourceId)}/${encodeURIComponent(manga.sourceMangaId)}/${encodeURIComponent(chapters[0]?.sourceChapterId || "")}`}
              className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-4 py-2 text-sm font-semibold text-zinc-950"
            >
              <Play size={15} /> Read
            </Link>
            <button
              onClick={async () => {
                await api.setLibrary(manga.sourceId, manga.sourceMangaId, !manga.inLibrary);
                await load();
              }}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-4 py-2 text-sm"
            >
              <Bookmark size={15} /> {manga.inLibrary ? "In library" : "Add to library"}
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
          </div>
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
                  onClick={async () => {
                    await api.setCategory(manga.sourceId, manga.sourceMangaId, category.id, !active);
                    await load();
                  }}
                  className={`rounded-full border px-3 py-2 text-xs ${active ? "border-amber-400 bg-amber-400 text-zinc-950" : "border-zinc-800 text-zinc-400"}`}
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
              onClick={() => setSelected(chapters.map((item) => item.sourceChapterId))}
              className="rounded-lg border border-zinc-700 px-3 py-2 text-xs"
            >
              Select all
            </button>
            <button
              onClick={() => void enqueue()}
              className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-xs font-semibold text-zinc-950"
            >
              <Download size={14} /> Download
            </button>
          </div>
        </PageHeader>
        {chapters.length ? (
          <div className="divide-y divide-zinc-800 rounded-xl border border-zinc-800 bg-zinc-900/40">
            {chapters.map((chapter) => (
              <label key={chapter.id} className="flex items-center gap-3 p-4 hover:bg-zinc-900">
                <input
                  type="checkbox"
                  checked={selected.includes(chapter.sourceChapterId)}
                  onChange={() => toggle(chapter.sourceChapterId)}
                  className="accent-amber-400"
                />
                <Link
                  to={`/reader/${encodeURIComponent(manga.sourceId)}/${encodeURIComponent(manga.sourceMangaId)}/${encodeURIComponent(chapter.sourceChapterId)}`}
                  className="min-w-0 flex-1"
                >
                  <b className="block text-sm">
                    {formatChapter(chapter.chapterNumber, chapter.title)}
                  </b>
                  <span className="text-xs text-zinc-500">
                    {chapter.language || "Unknown language"}
                    {chapter.scanlator ? ` · ${chapter.scanlator}` : ""}
                  </span>
                </Link>
                {chapter.downloaded && <Check size={16} className="text-emerald-400" />}
              </label>
            ))}
          </div>
        ) : (
          <EmptyState
            title="No chapters"
            text="This source returned no chapter metadata for the title."
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
          sourceId={manga.sourceId}
          mangaId={manga.sourceMangaId}
          bindings={data.trackers}
          onClose={() => setModal(undefined)}
          onChanged={load}
        />
      )}
      {modal === "migration" && (
        <MigrationModal
          manga={manga}
          onClose={() => setModal(undefined)}
          onApplied={(sourceId, mangaId) =>
            navigate(`/manga/${encodeURIComponent(sourceId)}/${encodeURIComponent(mangaId)}`)
          }
        />
      )}
    </div>
  );
}

function formatChapter(number?: number, title?: string) {
  return number == null ? title || "Special" : `Chapter ${number}`;
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
  sourceId,
  mangaId,
  bindings,
  onClose,
  onChanged,
}: {
  sourceId: string;
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
      {binding && (
        <button
          onClick={async () => {
            await api.unbindTracker(sourceId, mangaId, active);
            await onChanged();
          }}
          className="mt-4 inline-flex items-center gap-2 rounded-lg border border-red-900 px-3 py-2 text-sm text-red-300"
        >
          <X size={14} /> Unbind
        </button>
      )}
      <div className="mt-4 grid gap-2">
        {results.map((item) => (
          <button
            key={item.remoteId}
            onClick={async () => {
              await api.bindTracker(sourceId, mangaId, active, {
                remoteId: item.remoteId,
                remoteTitle: item.title,
                remoteStatus: item.status,
              });
              await onChanged();
              setResults([]);
            }}
            className="rounded-lg border border-zinc-800 p-3 text-left hover:border-amber-400"
          >
            <b>{item.title}</b>
            <small className="ml-2 text-zinc-500">{item.remoteId}</small>
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
  onApplied: (sourceId: string, mangaId: string) => void;
}) {
  const [items, setItems] = useState<MigrationCandidate[]>([]);
  const [selected, setSelected] = useState<MigrationCandidate>();
  const [error, setError] = useState("");
  useEffect(() => {
    void api
      .migrationCandidates(manga.sourceId, manga.sourceMangaId)
      .then(setItems)
      .catch((e) => setError(e.message));
  }, [manga.sourceId, manga.sourceMangaId]);
  return (
    <Modal title="Migrate source" onClose={onClose}>
      <p className="text-sm text-zinc-400">
        Select a matching title from another installed source. Reading state and tracker bindings
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
          disabled={!selected}
          onClick={async () => {
            if (!selected) return;
            const result = await api.applyMigration(
              manga.sourceId,
              manga.sourceMangaId,
              selected.source.id,
              selected.result.id,
            );
            onApplied(result.manga.manga.sourceId, result.manga.manga.sourceMangaId);
          }}
          className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-50"
        >
          Apply
        </button>
      </div>
    </Modal>
  );
}

type SourceManga = Aggregate["manga"];
function Modal({
  title,
  children,
  onClose,
}: {
  title: string;
  children: React.ReactNode;
  onClose: () => void;
}) {
  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4"
      onMouseDown={onClose}
    >
      <section
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-2xl border border-zinc-700 bg-zinc-900 p-5 shadow-2xl"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="mb-5 flex items-center justify-between">
          <h2 className="text-xl font-semibold">{title}</h2>
          <button
            aria-label="Close"
            onClick={onClose}
            className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800 hover:text-white"
          >
            <X size={17} />
          </button>
        </div>
        {children}
      </section>
    </div>
  );
}
