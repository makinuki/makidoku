import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Check,
  LoaderCircle,
  Search,
  X,
} from "lucide-react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../../api";
import { CoverImg } from "../../components/CoverImg";
import { Modal } from "../../components/Modal";
import { SourceIcon } from "../../components/SourceIcon";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { useMigrationEvents } from "../../hooks/useMigrationEvents";
import type { MigrationSource, MigrationTitleEvent } from "../../types";

type Job = { jobId: string; count: number };

// MigrationPage reviews a source migration before it is committed. A bulk run
// lists every library title of the chosen plugin and streams a match for each;
// a single-title run is reached from a details page with ?mangaId and skips
// the picker. Nothing is written until a row is migrated.
export function MigrationPage() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const mangaId = searchParams.get("mangaId")?.trim() || undefined;

  const [sources, setSources] = useState<MigrationSource[]>();
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [job, setJob] = useState<Job>();
  const [activeSource, setActiveSource] = useState<MigrationSource>();
  const [rows, setRows] = useState<Record<string, MigrationTitleEvent>>({});
  const [order, setOrder] = useState<string[]>([]);
  const [skipped, setSkipped] = useState<Set<string>>(new Set());
  const [applied, setApplied] = useState<Set<string>>(new Set());
  const [complete, setComplete] = useState(false);
  const [busyId, setBusyId] = useState<string>();
  const [bulk, setBulk] = useState<{ done: number; total: number }>();
  const [confirmAll, setConfirmAll] = useState(false);
  const [leaveOpen, setLeaveOpen] = useState(false);
  const abortBulk = useRef(false);

  useEffect(() => {
    void api
      .migrationSources()
      .then(setSources)
      .catch((e) => setError(e instanceof Error ? e.message : "Unable to load migration sources"));
  }, []);

  const startJob = useCallback(
    async (request: { sourceId?: string; mangaId?: string }, source?: MigrationSource) => {
      setError("");
      setNotice("");
      try {
        const started = await api.startMigrationJob(request);
        setActiveSource(source);
        setJob(started);
        setRows({});
        setOrder([]);
        setSkipped(new Set());
        setApplied(new Set());
        setComplete(false);
      } catch (e) {
        setError(e instanceof Error ? e.message : "Unable to start migration");
      }
    },
    [],
  );

  // A single-title entry point starts as soon as the page opens.
  useEffect(() => {
    if (mangaId && !job) void startJob({ mangaId });
  }, [mangaId, job, startJob]);

  useMigrationEvents(job?.jobId ?? null, {
    onSnapshot: (titles) => {
      setRows(Object.fromEntries(titles.map((title) => [title.mangaId, title])));
      setOrder(titles.map((title) => title.mangaId));
    },
    onTitle: (title) => setRows((prev) => ({ ...prev, [title.mangaId]: title })),
    onComplete: () => setComplete(true),
  });

  const visible = order.map((id) => rows[id]).filter((row): row is MigrationTitleEvent => Boolean(row));
  const matched = visible.filter((row) => row.status === "success");
  const targets = matched.filter((row) => !skipped.has(row.mangaId) && !applied.has(row.mangaId));
  const skippedCount = visible.filter((row) => skipped.has(row.mangaId)).length;
  const streaming = job !== undefined && !complete;
  const inProgress = streaming || bulk !== undefined;

  const cancelTitle = (id: string) => {
    if (!job) return;
    void api.cancelMigrationTitle(job.jobId, id).catch(() => {});
  };
  const toggleSkip = (id: string) => {
    setSkipped((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const applyRow = async (row: MigrationTitleEvent) => {
    if (!row.source || !row.manga) return;
    setBusyId(row.mangaId);
    setNotice("");
    try {
      await api.applyMigration(row.mangaId, row.source.id, row.manga.id);
      setApplied((prev) => new Set(prev).add(row.mangaId));
    } catch (e) {
      setNotice(e instanceof Error ? e.message : "Unable to migrate this title");
    } finally {
      setBusyId(undefined);
    }
  };
  const migrateAll = async () => {
    setConfirmAll(false);
    abortBulk.current = false;
    const list = targets;
    setBulk({ done: 0, total: list.length });
    let done = 0;
    for (const row of list) {
      if (abortBulk.current) return;
      if (row.source && row.manga) {
        try {
          await api.applyMigration(row.mangaId, row.source.id, row.manga.id);
          setApplied((prev) => new Set(prev).add(row.mangaId));
        } catch (e) {
          setNotice(e instanceof Error ? e.message : "Some titles could not be migrated");
        }
      }
      done += 1;
      setBulk({ done, total: list.length });
    }
    setBulk(undefined);
  };
  const leave = () => {
    if (inProgress) {
      setLeaveOpen(true);
      return;
    }
    navigate("/library");
  };
  const confirmLeave = () => {
    abortBulk.current = true;
    setLeaveOpen(false);
    if (job) void api.cancelMigrationJob(job.jobId).catch(() => {});
    navigate("/library");
  };

  if (error) {
    return (
      <div className="mx-auto max-w-5xl p-5 sm:p-8">
        <PageHeader eyebrow="Library" title="Migrate" />
        <ErrorState message={error} />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-5xl p-5 sm:p-8">
      <PageHeader eyebrow="Library" title="Migrate">
        <button
          onClick={leave}
          disabled={bulk !== undefined}
          className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm disabled:opacity-50"
        >
          <ArrowLeft size={15} /> Back
        </button>
      </PageHeader>

      {notice && (
        <div className="mb-4 flex items-center justify-between gap-3 rounded-xl border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-200">
          <span>{notice}</span>
          <button aria-label="Dismiss" onClick={() => setNotice("")}>
            <X size={15} />
          </button>
        </div>
      )}

      {job ? (
        <>
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-zinc-800 bg-zinc-900/50 p-3">
            <div className="text-sm text-zinc-400">
              {activeSource ? `${activeSource.source.name} · ` : ""}
              {streaming ? "Searching other plugins" : "Search complete"} · {matched.length}{" "}
              matched
              {skippedCount > 0 ? ` · ${skippedCount} skipped` : ""}
            </div>
            <button
              disabled={targets.length === 0 || bulk !== undefined}
              onClick={() => setConfirmAll(true)}
              className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-50"
            >
              <Check size={15} /> Migrate all{targets.length ? ` (${targets.length})` : ""}
            </button>
          </div>
          {bulk && (
            <p className="mb-3 text-sm text-amber-300">
              Migrating {bulk.done} of {bulk.total}&hellip;
            </p>
          )}
          {order.length === 0 ? (
            complete ? (
              <EmptyState
                title="Nothing to migrate"
                text="This plugin has no library titles to move."
              />
            ) : (
              <LoadingState label="Starting searches" />
            )
          ) : (
            <div className="grid gap-3">
              {visible.map((row) => (
                <MigrationRow
                  key={row.mangaId}
                  row={row}
                  skipped={skipped.has(row.mangaId)}
                  applied={applied.has(row.mangaId)}
                  busy={busyId === row.mangaId}
                  onCancel={cancelTitle}
                  onSkip={toggleSkip}
                  onMigrate={(item) => void applyRow(item)}
                />
              ))}
            </div>
          )}
        </>
      ) : mangaId ? (
        <LoadingState label="Starting migration" />
      ) : (
        <SourcePicker sources={sources} onSelect={(item) => void startJob({ sourceId: item.source.id }, item)} />
      )}

      {confirmAll && (
        <Modal title="Migrate all" onClose={() => setConfirmAll(false)}>
          <p className="text-sm text-zinc-400">
            Migrate {targets.length} {targets.length === 1 ? "title" : "titles"} to their matches?
            {skippedCount > 0
              ? ` ${skippedCount} skipped ${skippedCount === 1 ? "title" : "titles"} will be left alone.`
              : ""}
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <button
              onClick={() => setConfirmAll(false)}
              className="rounded-lg border border-zinc-700 px-3 py-2 text-sm"
            >
              Cancel
            </button>
            <button
              onClick={() => void migrateAll()}
              className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
            >
              <Check size={15} /> Migrate {targets.length}
            </button>
          </div>
        </Modal>
      )}

      {leaveOpen && (
        <Modal title="Leave migration" onClose={() => setLeaveOpen(false)}>
          <p className="text-sm text-zinc-400">
            Searches still running will stop. Titles already migrated stay migrated.
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <button
              onClick={() => setLeaveOpen(false)}
              className="rounded-lg border border-zinc-700 px-3 py-2 text-sm"
            >
              Stay
            </button>
            <button
              onClick={confirmLeave}
              className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
            >
              Leave
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}

function MigrationRow({
  row,
  skipped,
  applied,
  busy,
  onCancel,
  onSkip,
  onMigrate,
}: {
  row: MigrationTitleEvent;
  skipped: boolean;
  applied: boolean;
  busy: boolean;
  onCancel: (id: string) => void;
  onSkip: (id: string) => void;
  onMigrate: (row: MigrationTitleEvent) => void;
}) {
  const searching = row.status === "searching";
  const matched = row.status === "success" && row.manga && row.source;
  return (
    <div
      className={`grid items-center gap-3 rounded-xl border border-zinc-800 bg-zinc-900/50 p-3 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_auto] ${
        skipped || applied ? "opacity-60" : ""
      }`}
    >
      <div className="flex min-w-0 items-center gap-3">
        <CoverImg
          src={`/api/manga/${row.mangaId}/cover`}
          className="h-20 w-14 shrink-0 rounded-md object-cover"
        />
        <div className="min-w-0">
          <b className="line-clamp-2 text-sm">{row.title}</b>
          <small className="block text-zinc-500">Current</small>
        </div>
      </div>
      <ArrowRight className="hidden shrink-0 text-zinc-600 sm:block" size={18} />
      <div className="min-w-0">
        {searching ? (
          <span className="flex items-center gap-2 text-sm text-zinc-400">
            <LoaderCircle className="animate-spin" size={16} /> Searching other plugins
          </span>
        ) : matched ? (
          <div className="flex min-w-0 items-center gap-3">
            <CoverImg
              src={row.manga?.coverUrl}
              className="h-20 w-14 shrink-0 rounded-md object-cover"
            />
            <div className="min-w-0">
              <b className="line-clamp-2 text-sm">{row.manga?.title}</b>
              <small className="block text-zinc-500">{row.source?.name}</small>
              <div className="mt-1 flex flex-wrap gap-1 text-[11px]">
                {typeof row.score === "number" && row.score > 0 && (
                  <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-emerald-300">
                    {Math.round(row.score * 100)}% match
                  </span>
                )}
                {typeof row.chapterCount === "number" && row.chapterCount > 0 && (
                  <span className="rounded-full bg-zinc-800 px-2 py-0.5 text-zinc-400">
                    {row.chapterCount} chapters
                  </span>
                )}
                {typeof row.latestChapter === "number" && (
                  <span className="rounded-full bg-zinc-800 px-2 py-0.5 text-zinc-400">
                    Latest {row.latestChapter}
                  </span>
                )}
              </div>
            </div>
          </div>
        ) : (
          <span className="text-sm text-zinc-500">{migrationStatusLabel(row)}</span>
        )}
      </div>
      <div className="flex items-center gap-2 sm:justify-end">
        {applied ? (
          <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-2 py-1 text-xs text-emerald-300">
            <Check size={14} /> Migrated
          </span>
        ) : skipped ? (
          <button
            onClick={() => onSkip(row.mangaId)}
            className="rounded-lg border border-zinc-700 px-3 py-1.5 text-xs"
          >
            Undo skip
          </button>
        ) : (
          <>
            {searching && (
              <button
                onClick={() => onCancel(row.mangaId)}
                className="rounded-lg border border-zinc-700 px-3 py-1.5 text-xs"
              >
                Cancel
              </button>
            )}
            {row.status === "success" && (
              <button
                disabled={busy}
                onClick={() => onMigrate(row)}
                className="inline-flex items-center gap-1 rounded-lg bg-amber-400 px-3 py-1.5 text-xs font-semibold text-zinc-950 disabled:opacity-50"
              >
                {busy && <LoaderCircle size={13} className="animate-spin" />}
                {busy ? "Migrating…" : "Migrate"}
              </button>
            )}
            {!searching && (
              <button
                onClick={() => onSkip(row.mangaId)}
                className="rounded-lg border border-zinc-700 px-3 py-1.5 text-xs"
              >
                Skip
              </button>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function migrationStatusLabel(row: MigrationTitleEvent) {
  switch (row.status) {
    case "notFound":
      return "No match found";
    case "failed":
      return row.error || "Search failed";
    case "cancelled":
      return "Search cancelled";
    default:
      return row.status;
  }
}

function SourcePicker({
  sources,
  onSelect,
}: {
  sources?: MigrationSource[];
  onSelect: (item: MigrationSource) => void;
}) {
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<"count" | "name">("count");
  const [ascending, setAscending] = useState(false);
  if (!sources) return <LoadingState label="Loading sources" />;
  if (!sources.length) {
    return (
      <EmptyState title="Nothing to migrate" text="No installed plugin holds library titles." />
    );
  }
  const filtered = sources.filter((item) =>
    item.source.name.toLowerCase().includes(query.trim().toLowerCase()),
  );
  const ordered = [...filtered].sort((a, b) => {
    const order = sort === "count" ? a.count - b.count : a.source.name.localeCompare(b.source.name);
    return ascending ? order : -order;
  });
  return (
    <div>
      <p className="mb-4 text-sm text-zinc-400">
        Choose the plugin whose titles you want to move. Migration reviews every match before
        anything is written.
      </p>
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="flex min-w-48 flex-1 items-center gap-2 rounded-xl border border-zinc-800 bg-zinc-900 px-3 py-2">
          <Search size={16} className="text-zinc-500" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Filter plugin names"
            className="min-w-0 flex-1 bg-transparent text-sm outline-none"
          />
        </div>
        <div className="flex items-center gap-1" role="group" aria-label="Sort sources">
          {(["count", "name"] as const).map((mode) => (
            <button
              key={mode}
              type="button"
              aria-pressed={sort === mode}
              onClick={() => setSort(mode)}
              className={`min-h-11 rounded-lg px-3 text-xs font-medium capitalize ${
                sort === mode ? "bg-zinc-800 text-white" : "text-zinc-400 hover:text-white"
              }`}
            >
              {mode}
            </button>
          ))}
          <button
            type="button"
            aria-label={ascending ? "Sort ascending" : "Sort descending"}
            onClick={() => setAscending((value) => !value)}
            className="flex min-h-11 min-w-11 items-center justify-center rounded-lg text-zinc-400 hover:bg-zinc-800 hover:text-white"
          >
            {ascending ? <ArrowUp size={15} /> : <ArrowDown size={15} />}
          </button>
        </div>
      </div>
      <div className="grid gap-2">
        {ordered.map((item) => (
          <button
            key={item.source.id}
            onClick={() => onSelect(item)}
            className="flex items-center gap-3 rounded-xl border border-zinc-800 bg-zinc-900/50 p-3 text-left hover:border-amber-500/50"
          >
            <SourceIcon source={item.source} />
            <span className="min-w-0 flex-1 truncate text-sm">
              {item.source.name}
              {item.imported && (
                <small className="ml-1 rounded bg-zinc-800 px-1 text-[10px] text-amber-300">
                  imported
                </small>
              )}
            </span>
            <span className="rounded-full bg-zinc-800 px-2 py-0.5 text-xs text-zinc-400">
              {item.count}
            </span>
            <ArrowRight size={16} className="text-zinc-600" />
          </button>
        ))}
        {filtered.length === 0 && (
          <p className="p-4 text-sm text-zinc-500">No plugins match this filter.</p>
        )}
      </div>
    </div>
  );
}
