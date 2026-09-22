import { useEffect, useMemo, useState } from "react";
import { Check, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import { api } from "../../api";
import { refreshBadges } from "../../app/nav";
import type { LibraryUpdateState, UpdateLog } from "../../types";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { CoverImg } from "../../components/CoverImg";
import { dateGroupLabel, formatTimestamp } from "../../time";
import { useDateFormat } from "../../hooks/useDateFormat";

export function UpdatesPage() {
  const dateFormat = useDateFormat();
  const [items, setItems] = useState<UpdateLog[]>([]);
  const [state, setState] = useState<LibraryUpdateState | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
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
  const groups = useMemo(() => {
    const grouped = new Map<string, UpdateLog[]>();
    for (const item of items)
      grouped.set(dateGroupLabel(item.seenAt), [
        ...(grouped.get(dateGroupLabel(item.seenAt)) || []),
        item,
      ]);
    return [...grouped.entries()];
  }, [items]);
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
  const acknowledge = async (id: string) => {
    try {
      await api.acknowledgeUpdates([id]);
      setItems((current) => current.filter((item) => item.id !== id));
      refreshBadges();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to acknowledge update");
    }
  };
  return (
    <div className="mx-auto max-w-5xl p-5 sm:p-8">
      <PageHeader eyebrow="Library changes" title="Updates">
        <div className="flex flex-wrap gap-2">
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
          {groups.map(([label, group]) => (
            <section key={label}>
              <h2 className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-amber-400">
                {label} <span className="text-zinc-600">{group.length}</span>
              </h2>
              <div className="divide-y divide-zinc-800 rounded-xl border border-zinc-800 bg-zinc-900/50">
                {group.map((item) => (
                  <div key={item.id} className="flex items-center gap-4 p-4">
                    <Link
                      to={`/manga/${encodeURIComponent(item.manga.id)}`}
                      className="size-14 shrink-0 overflow-hidden rounded-lg bg-zinc-800"
                    >
                      <CoverImg src={item.manga.coverUrl} className="size-full object-cover" />
                    </Link>
                    <div className="min-w-0 flex-1">
                      <Link
                        to={`/reader/${encodeURIComponent(item.manga.id)}/${encodeURIComponent(item.chapter.id)}`}
                        className="font-medium hover:text-amber-300"
                      >
                        {item.manga.title}
                      </Link>
                      <p className="text-sm text-zinc-500">
                        {item.chapter.chapterNumber == null
                          ? item.chapter.title || "Special"
                          : `Chapter ${item.chapter.chapterNumber}`}{" "}
                        · {formatTimestamp(item.seenAt, dateFormat)}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => void acknowledge(item.id)}
                      className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm text-zinc-300 hover:border-amber-400 hover:text-amber-300"
                    >
                      <Check size={16} /> Mark read
                    </button>
                  </div>
                ))}
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
