import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  BarChart3,
  ChevronRight,
  Database,
  Download,
  EyeOff,
  OctagonAlert,
  Settings,
  Tags,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { api } from "../../api";
import { Logo } from "../../components/Logo";
import { ErrorState, LoadingState } from "../../components/States";
import { Switch } from "../../components/Switch";
import { formatTimestamp } from "../../time";
import { useDateFormat } from "../../hooks/useDateFormat";

function MoreRow({
  to,
  Icon,
  title,
  subtitle,
}: {
  to: string;
  Icon: LucideIcon;
  title: string;
  subtitle: string;
}) {
  return (
    <Link
      to={to}
      className="flex min-h-11 items-center gap-4 border-b border-zinc-800 px-4 py-3.5 last:border-b-0 hover:bg-zinc-800/50 active:bg-zinc-800/50"
    >
      <Icon size={18} className="shrink-0 text-amber-400" />
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block truncate text-xs text-zinc-500">{subtitle}</span>
      </span>
      <ChevronRight size={16} className="shrink-0 text-zinc-600" />
    </Link>
  );
}

export function MorePage() {
  const dateFormat = useDateFormat();
  const [incognito, setIncognito] = useState(false);
  const [queueSummary, setQueueSummary] = useState("Loading queue");
  const [categorySummary, setCategorySummary] = useState("Loading categories");
  const [updateSummary, setUpdateSummary] = useState("Loading update state");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    void Promise.all([api.incognito(), api.downloads(), api.categories(), api.updateState()])
      .then(([incognitoState, snapshot, categories, updateState]) => {
        if (!active) return;
        setIncognito(incognitoState.enabled);
        const live = snapshot.items.filter((item) =>
          ["PENDING", "DOWNLOADING", "PAUSED"].includes(item.status),
        );
        setQueueSummary(
          live.length === 0
            ? "Queue is empty"
            : snapshot.paused
              ? `${live.length} active · downloader paused`
              : `${live.length} active`,
        );
        setCategorySummary(
          categories.length === 0 ? "No categories yet" : `${categories.length} categories`,
        );
        if (!updateState?.lastRunAt) {
          setUpdateSummary("Library has not been checked yet");
        } else if (updateState.lastStatus === "completed_with_errors") {
          setUpdateSummary(
            `Some sources failed · last check ${formatTimestamp(updateState.lastRunAt, dateFormat)}`,
          );
        } else {
          setUpdateSummary(`Last check ${formatTimestamp(updateState.lastRunAt, dateFormat)}`);
        }
      })
      .catch((e) => active && setError(e instanceof Error ? e.message : "Unable to load overview"))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, [dateFormat]);
  const toggleIncognito = () => {
    const next = !incognito;
    setIncognito(next);
    void api.setIncognito(next).catch(() => setIncognito(!next));
  };
  return (
    <div className="mx-auto max-w-5xl p-5 sm:p-8">
      <div className="mb-8 flex items-center gap-3">
        <Logo className="size-10 shrink-0" />
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-amber-400">
            Library and app
          </p>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">More</h1>
        </div>
      </div>
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Loading overview" />
      ) : (
        <div className="space-y-6">
          <section className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/40">
            <Switch
              checked={incognito}
              onChange={toggleIncognito}
              trackPosition="end"
              size="md"
              className="flex min-h-11 w-full items-center gap-4 px-4 py-3.5 text-left hover:bg-zinc-800/50 active:bg-zinc-800/50"
            >
              <EyeOff size={18} className="shrink-0 text-amber-400" />
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium">Incognito mode</span>
                <span className="block truncate text-xs text-zinc-500">Pauses reading history</span>
              </span>
            </Switch>
          </section>
          <section className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/40">
            <MoreRow
              to="/downloads"
              Icon={Download}
              title="Download queue"
              subtitle={queueSummary}
            />
            <MoreRow
              to="/settings/library"
              Icon={Tags}
              title="Categories"
              subtitle={categorySummary}
            />
            <MoreRow
              to="/stats"
              Icon={BarChart3}
              title="Statistics"
              subtitle="Reading activity and totals"
            />
            <MoreRow
              to="/updates"
              Icon={OctagonAlert}
              title="Library update errors"
              subtitle={updateSummary}
            />
            <MoreRow
              to="/settings/data"
              Icon={Database}
              title="Data and storage"
              subtitle="Backup, restore, and reading statistics"
            />
          </section>
          <section className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/40">
            <MoreRow
              to="/settings"
              Icon={Settings}
              title="Settings"
              subtitle="Appearance, reader, tracking, and more"
            />
          </section>
        </div>
      )}
    </div>
  );
}
