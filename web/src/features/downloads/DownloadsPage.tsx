import { useEffect, useState } from "react";
import { Pause, Play, RotateCw, X } from "lucide-react";
import { api } from "../../api";
import type { DownloadEvent, DownloadSnapshot, QueueItem } from "../../types";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";

export function DownloadsPage() {
  const [snapshot, setSnapshot] = useState<DownloadSnapshot>({
    items: [],
    stats: { downloadedPages: 0, retriedRequests: 0, throttledRequests: 0 },
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [clearing, setClearing] = useState(false);
  const refresh = () =>
    api
      .downloads()
      .then(setSnapshot)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  const clearFinished = async () => {
    setClearing(true);
    try {
      await api.clearFinishedDownloads();
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to clear finished downloads");
    } finally {
      setClearing(false);
    }
  };
  useEffect(() => {
    void refresh();
    let socket: WebSocket | undefined;
    let reconnectTimer: number | undefined;
    let disposed = false;
    const applyMessage = (data: unknown) => {
      try {
        const message = JSON.parse(String(data)) as DownloadEvent;
        setSnapshot((current) => ({
          ...current,
          items: current.items.some((item) => item.id === message.item.id)
            ? current.items.map((item) => (item.id === message.item.id ? message.item : item))
            : [...current.items, message.item],
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
  }, []);
  return (
    <div className="mx-auto max-w-5xl p-5 sm:p-8">
      <PageHeader eyebrow="Queue" title="Downloads">
        <div className="flex items-center gap-4">
          <div className="text-right text-xs text-zinc-500">
            <p>{snapshot.stats.downloadedPages} pages saved</p>
            <p>
              {snapshot.stats.retriedRequests} retried · {snapshot.stats.throttledRequests} slowed
              by the source
            </p>
          </div>
          {snapshot.items.some(
            (item) =>
              item.status === "COMPLETED" || item.status === "CANCELED" || item.status === "FAILED",
          ) && (
            <button
              onClick={() => void clearFinished()}
              disabled={clearing}
              className="rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-300 disabled:opacity-50"
            >
              {clearing ? "Clearing…" : "Clear finished"}
            </button>
          )}
        </div>
      </PageHeader>
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Loading queue" />
      ) : snapshot.items.length ? (
        <div className="space-y-3">
          {snapshot.items.map((item) => (
            <QueueRow key={item.id} item={item} onChange={refresh} />
          ))}
        </div>
      ) : (
        <EmptyState
          title="Download queue is empty"
          text="Select chapters from a title to start a download."
        />
      )}
    </div>
  );
}

function QueueRow({ item, onChange }: { item: QueueItem; onChange: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const action =
    item.status === "PAUSED"
      ? "resume"
      : ["PENDING", "DOWNLOADING"].includes(item.status)
        ? "pause"
        : undefined;
  const retryable = item.status === "FAILED";
  const control = async (value: "pause" | "resume" | "cancel" | "retry") => {
    setBusy(true);
    setError("");
    try {
      await api.controlDownload(item.id, value);
      onChange();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to update the download");
    } finally {
      setBusy(false);
    }
  };
  return (
    <article className="rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-semibold">
            {item.mangaTitle} ·{" "}
            {item.chapterTitle ||
              (item.chapterNumber != null ? `Chapter ${item.chapterNumber}` : "Special chapter")}
          </h2>
          <p className="mt-1 text-xs text-zinc-500">
            {item.sourceName} · {item.status.toLowerCase()}
          </p>
          <div className="mt-3 h-2 rounded-full bg-zinc-800">
            <span
              className="block h-full rounded-full bg-amber-400"
              style={{ width: `${item.progress}%` }}
            />
          </div>
          {(item.errorMessage || error) && (
            <p className="mt-2 text-xs text-red-300">{error || item.errorMessage}</p>
          )}
        </div>
        <strong className="text-sm text-zinc-300">{item.progress}%</strong>
        <div className="flex gap-1">
          {action && (
            <button
              disabled={busy}
              aria-label={action === "pause" ? "Pause download" : "Resume download"}
              title={action === "pause" ? "Pause download" : "Resume download"}
              onClick={() => void control(action)}
              className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800 hover:text-white"
            >
              {action === "pause" ? <Pause size={16} /> : <Play size={16} />}
            </button>
          )}
          {retryable && (
            <button
              disabled={busy}
              aria-label="Retry download"
              title="Retry download"
              onClick={() => void control("retry")}
              className="rounded-lg p-2 text-amber-300 hover:bg-zinc-800"
            >
              <RotateCw size={16} />
            </button>
          )}
          {["PENDING", "DOWNLOADING", "PAUSED", "FAILED"].includes(item.status) && (
            <button
              disabled={busy}
              aria-label="Cancel download"
              title="Cancel download"
              onClick={() => void control("cancel")}
              className="rounded-lg p-2 text-red-300 hover:bg-red-950/50"
            >
              <X size={16} />
            </button>
          )}
        </div>
      </div>
    </article>
  );
}
