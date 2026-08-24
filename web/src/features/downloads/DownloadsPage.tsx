import { useEffect, useRef, useState } from "react";
import { Pause, Play, X } from "lucide-react";
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
  const socket = useRef<WebSocket | undefined>(undefined);
  const refresh = () =>
    api
      .downloads()
      .then(setSnapshot)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  useEffect(() => {
    void refresh();
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${location.host}/api/download/events`);
    socket.current = ws;
    ws.onmessage = (event) => {
      const message = JSON.parse(event.data) as DownloadEvent;
      setSnapshot((current) => ({
        ...current,
        items: current.items.some((item) => item.id === message.item.id)
          ? current.items.map((item) => (item.id === message.item.id ? message.item : item))
          : [...current.items, message.item],
        stats: message.stats,
      }));
    };
    ws.onerror = () => {
      const timer = window.setInterval(() => void refresh(), 5000);
      ws.onclose = () => window.clearInterval(timer);
    };
    return () => ws.close();
  }, []);
  return (
    <div className="mx-auto max-w-5xl p-5 sm:p-8">
      <PageHeader eyebrow="Queue" title="Downloads">
        <div className="text-right text-xs text-zinc-500">
          <p>{snapshot.stats.downloadedPages} pages saved</p>
          <p>
            {snapshot.stats.retriedRequests} retries · {snapshot.stats.throttledRequests} throttled
          </p>
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
  const action =
    item.status === "PAUSED"
      ? "resume"
      : ["PENDING", "DOWNLOADING"].includes(item.status)
        ? "pause"
        : undefined;
  const control = async (value: "pause" | "resume" | "cancel") => {
    setBusy(true);
    try {
      await api.controlDownload(item.id, value);
      onChange();
    } finally {
      setBusy(false);
    }
  };
  return (
    <article className="rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-semibold">
            {item.mangaTitle} · {item.chapterTitle || item.chapterNumber || "Special"}
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
          {item.errorMessage && <p className="mt-2 text-xs text-red-300">{item.errorMessage}</p>}
        </div>
        <strong className="text-sm text-zinc-300">{item.progress}%</strong>
        <div className="flex gap-1">
          {action && (
            <button
              disabled={busy}
              aria-label={action}
              title={action}
              onClick={() => void control(action)}
              className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800 hover:text-white"
            >
              {action === "pause" ? <Pause size={16} /> : <Play size={16} />}
            </button>
          )}
          {["PENDING", "DOWNLOADING", "PAUSED"].includes(item.status) && (
            <button
              disabled={busy}
              aria-label="cancel"
              title="Cancel"
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
