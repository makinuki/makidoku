import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ChevronRight, Play, Trash2 } from "lucide-react";
import { api } from "../../api";
import { resumeLastRead } from "../../app/nav";
import type { HistoryEvent } from "../../types";
import { CoverImg } from "../../components/CoverImg";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { dateGroupLabel, formatTimestamp } from "../../time";
import { useDateFormat } from "../../hooks/useDateFormat";

export function HistoryPage() {
  const dateFormat = useDateFormat();
  const navigate = useNavigate();
  const [items, setItems] = useState<HistoryEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = () => {
    setLoading(true);
    api
      .history()
      .then(setItems)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  };
  useEffect(load, []);
  const remove = async (id: string) => {
    try {
      await api.deleteHistoryEvent(id);
      setItems((current) => current.filter((item) => item.id !== id));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to remove this entry");
    }
  };
  const groups = useMemo(() => {
    const grouped = new Map<string, HistoryEvent[]>();
    for (const item of items) {
      const label = dateGroupLabel(item.occurredAt);
      grouped.set(label, [...(grouped.get(label) || []), item]);
    }
    return [...grouped.entries()];
  }, [items]);
  return (
    <div className="mx-auto max-w-5xl p-5 sm:p-8">
      <PageHeader eyebrow="Recently read" title="History">
        <button
          type="button"
          onClick={() => void resumeLastRead(navigate)}
          className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-4 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-50"
        >
          <Play size={16} /> Resume last
        </button>
      </PageHeader>
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Loading history" />
      ) : groups.length ? (
        <div className="space-y-8">
          {groups.map(([label, group]) => (
            <section key={label}>
              <h2 className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-amber-400">
                {label}
              </h2>
              <div className="divide-y divide-zinc-800 rounded-xl border border-zinc-800 bg-zinc-900/50">
                {group.map((item) => {
                  const target = item.chapter
                    ? `/reader/${encodeURIComponent(item.manga.id)}/${encodeURIComponent(item.chapter.id)}${item.page != null ? `?page=${item.page}` : ""}`
                    : `/manga/${encodeURIComponent(item.manga.id)}`;
                  return (
                    <div key={item.id} className="flex items-center gap-4 p-4">
                      <Link
                        to={`/manga/${encodeURIComponent(item.manga.id)}`}
                        aria-label={`Open ${item.manga.title}`}
                        className="size-14 shrink-0 overflow-hidden rounded-lg bg-zinc-800"
                      >
                        <CoverImg src={item.manga.coverUrl} className="size-full object-cover" />
                      </Link>
                      <Link
                        to={target}
                        className="flex min-w-0 flex-1 items-center gap-4 hover:bg-zinc-900"
                      >
                        <span className="min-w-0 flex-1">
                          <b className="block truncate">{item.manga.title}</b>
                          <small className="text-zinc-500">
                            {item.chapter?.chapterNumber == null
                              ? item.chapter?.title || "Details viewed"
                              : `Chapter ${item.chapter.chapterNumber}`}
                            {item.page != null && ` · page ${item.page}`}
                            {item.occurredAt > 0 &&
                              ` · ${formatTimestamp(item.occurredAt, dateFormat)}`}
                          </small>
                        </span>
                        <ChevronRight size={17} className="shrink-0 text-zinc-600" />
                      </Link>
                      <button
                        type="button"
                        aria-label={`Delete history for ${item.manga.title}`}
                        className="min-h-11 min-w-11 rounded-lg p-2 text-zinc-500 hover:bg-zinc-800 hover:text-red-300 active:bg-zinc-800 active:text-red-300"
                        onClick={() => void remove(item.id)}
                      >
                        <Trash2 size={17} />
                      </button>
                    </div>
                  );
                })}
              </div>
            </section>
          ))}
        </div>
      ) : (
        <EmptyState
          title="No reading history"
          text="Progress appears here after you read a chapter."
        />
      )}
    </div>
  );
}
