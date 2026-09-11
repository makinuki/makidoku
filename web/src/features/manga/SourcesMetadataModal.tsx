import { useCallback, useEffect, useState } from "react";
import { LoaderCircle, Trash2 } from "lucide-react";
import { api } from "../../api";
import type { MangaMerge, MangaMetadata, Source } from "../../types";
import { Modal } from "../../components/Modal";

// SourcesMetadataModal shows the extra sources merged into a title and the
// metadata record a source attached to it.
export function SourcesMetadataModal({
  mangaId,
  title,
  onClose,
}: {
  mangaId: string;
  title: string;
  onClose: () => void;
}) {
  const [merges, setMerges] = useState<MangaMerge[]>([]);
  const [metadata, setMetadata] = useState<MangaMetadata>();
  const [sources, setSources] = useState<Source[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [sourceId, setSourceId] = useState("");
  const [sourceMangaId, setSourceMangaId] = useState("");
  const [url, setUrl] = useState("");

  const load = useCallback(async () => {
    try {
      const [mergeList, meta] = await Promise.all([
        api.mangaMerges(mangaId),
        api.mangaMetadata(mangaId),
      ]);
      setMerges(mergeList);
      setMetadata(meta);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load the sources");
    }
  }, [mangaId]);

  useEffect(() => {
    void load();
    void api
      .sources()
      .then(setSources)
      .catch(() => {
        // The add form needs the source list; a failure leaves it empty.
      });
  }, [load]);

  const sourceName = (id: string) => sources.find((source) => source.id === id)?.name ?? id;

  const add = async () => {
    if (!sourceId || !sourceMangaId.trim()) return;
    setBusy(true);
    setError("");
    try {
      await api.addMangaMerge(mangaId, {
        sourceId,
        sourceMangaId: sourceMangaId.trim(),
        url: url.trim() || undefined,
      });
      setSourceMangaId("");
      setUrl("");
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to add the merged source");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (mergeId: string) => {
    setBusy(true);
    setError("");
    try {
      await api.deleteMangaMerge(mangaId, mergeId);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to remove the merged source");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title={`Sources and metadata`} onClose={onClose}>
      <p className="mb-4 text-xs text-zinc-500">{title}</p>
      {error && (
        <p role="alert" className="mb-3 text-xs text-red-300">
          {error}
        </p>
      )}

      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-500">
        Merged sources
      </h3>
      {merges.length ? (
        <div className="space-y-2">
          {merges.map((merge) => (
            <div
              key={merge.id}
              className="flex items-center justify-between gap-2 rounded-lg border border-zinc-800 px-3 py-2"
            >
              <span className="min-w-0">
                <b className="block truncate text-sm">{sourceName(merge.sourceId)}</b>
                <small className="block truncate text-zinc-500">{merge.sourceMangaId}</small>
              </span>
              <button
                type="button"
                aria-label={`Remove merged source ${sourceName(merge.sourceId)}`}
                disabled={busy}
                onClick={() => void remove(merge.id)}
                className="rounded-lg border border-zinc-700 p-2 text-zinc-400 hover:text-red-300 disabled:opacity-40"
              >
                <Trash2 size={15} />
              </button>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs text-zinc-500">No additional sources are merged into this title.</p>
      )}

      <div className="mt-3 grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <select
          aria-label="Merge source"
          value={sourceId}
          onChange={(event) => setSourceId(event.target.value)}
          className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
        >
          <option value="">Choose a source</option>
          {sources.map((source) => (
            <option key={source.id} value={source.id}>
              {source.name}
            </option>
          ))}
        </select>
        <input
          aria-label="Source title id"
          value={sourceMangaId}
          onChange={(event) => setSourceMangaId(event.target.value)}
          placeholder="Source title id or url"
          className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
        />
        <button
          onClick={() => void add()}
          disabled={busy || !sourceId || !sourceMangaId.trim()}
          className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
        >
          {busy && <LoaderCircle size={15} className="animate-spin" />}
          Add
        </button>
      </div>
      <input
        aria-label="Merge source url"
        value={url}
        onChange={(event) => setUrl(event.target.value)}
        placeholder="Series page url (optional)"
        className="mt-2 w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
      />

      <h3 className="mt-6 mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-500">
        Metadata
      </h3>
      {metadata?.metadata ? (
        <dl className="space-y-1 text-xs text-zinc-400">
          {metadata.metadata.uploader && (
            <div className="flex gap-2">
              <dt className="text-zinc-500">Uploader</dt>
              <dd>{metadata.metadata.uploader}</dd>
            </div>
          )}
          <div className="flex gap-2">
            <dt className="text-zinc-500">Version</dt>
            <dd>{metadata.metadata.extraVersion}</dd>
          </div>
        </dl>
      ) : (
        <p className="text-xs text-zinc-500">No metadata record is stored for this title.</p>
      )}
      {metadata && metadata.titles.length > 0 && (
        <div className="mt-3">
          <p className="text-xs uppercase tracking-wide text-zinc-500">Alternative titles</p>
          <ul className="mt-1 space-y-0.5 text-xs text-zinc-300">
            {metadata.titles.map((entry) => (
              <li key={entry.id}>{entry.title}</li>
            ))}
          </ul>
        </div>
      )}
      {metadata && metadata.tags.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1">
          {metadata.tags.map((tag) => (
            <span
              key={tag.id}
              className="rounded-full border border-zinc-700 px-2 py-0.5 text-[11px] text-zinc-300"
            >
              {tag.namespace ? `${tag.namespace}:${tag.name}` : tag.name}
            </span>
          ))}
        </div>
      )}
    </Modal>
  );
}
