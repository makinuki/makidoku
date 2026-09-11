import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api";
import type { ReadingStats } from "../../types";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import { dailyBars, formatReadingTime } from "./statsFormat";

export function StatsPage() {
  const [stats, setStats] = useState<ReadingStats>();
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    api
      .stats()
      .then((data) => active && setStats(data))
      .catch(
        (e) => active && setError(e instanceof Error ? e.message : "Could not load statistics"),
      );
    return () => {
      active = false;
    };
  }, []);

  if (error) {
    return (
      <div className="p-5 lg:p-8">
        <ErrorState message={error} />
      </div>
    );
  }
  if (!stats) {
    return (
      <div className="grid min-h-[60vh] place-items-center">
        <LoadingState label="Loading statistics" />
      </div>
    );
  }

  const started = stats.titles.startedMangaCount > 0 || stats.readingSeconds > 0;
  const bars = dailyBars(stats.daily);

  return (
    <div className="p-5 lg:p-8">
      <PageHeader eyebrow="Library" title="Statistics" />
      {!started ? (
        <EmptyState
          title="No reading activity yet"
          text="Statistics are recorded as you read. Open a chapter from your library to start."
        />
      ) : (
        <div className="space-y-6">
          <section className="grid gap-4 sm:grid-cols-3">
            <Metric
              label="Total reading time"
              value={formatReadingTime(stats.overview.totalReadDuration)}
            />
            <Metric label="Titles in library" value={stats.overview.libraryMangaCount} />
            <Metric label="Titles completed" value={stats.overview.completedMangaCount} />
          </section>

          <div className="grid gap-4 lg:grid-cols-3">
            <StatCard title="Titles">
              <StatRow label="Updates enabled" value={stats.titles.updateEnabledCount} />
              <StatRow label="Started" value={stats.titles.startedMangaCount} />
            </StatCard>
            <StatCard title="Chapters">
              <StatRow label="Total" value={stats.chapters.totalChapterCount} />
              <StatRow label="Read" value={stats.chapters.readChapterCount} />
              <StatRow label="Downloaded" value={stats.chapters.downloadCount} />
            </StatCard>
            <StatCard title="Trackers">
              <StatRow label="Tracked titles" value={stats.trackers.trackedTitleCount} />
              <StatRow label="Trackers linked" value={stats.trackers.trackerCount} />
              <StatRow
                label="Mean score"
                value={stats.trackers.meanScore > 0 ? stats.trackers.meanScore.toFixed(1) : "-"}
              />
            </StatCard>
          </div>

          <StatCard title="Reading per day">
            {bars.length ? (
              <div
                className="flex h-40 items-end gap-1"
                role="img"
                aria-label="Reading time per day"
              >
                {bars.map((bar) => (
                  <div
                    key={bar.date}
                    className="group relative flex-1 rounded-t bg-amber-400/70 hover:bg-amber-300"
                    style={{ height: `${Math.max(bar.height, 2)}%` }}
                    title={`${bar.date}: ${formatReadingTime(bar.seconds)}`}
                  />
                ))}
              </div>
            ) : (
              <p className="text-sm text-zinc-500">No daily reading time recorded yet.</p>
            )}
          </StatCard>

          {stats.topTitles.length > 0 && (
            <StatCard title="Most read titles">
              <ul className="divide-y divide-zinc-800">
                {stats.topTitles.map((item) => (
                  <li key={item.mangaId} className="flex items-center gap-3 py-2 text-sm">
                    <Link
                      to={`/manga/${encodeURIComponent(item.mangaId)}`}
                      className="min-w-0 flex-1 truncate hover:text-amber-300"
                    >
                      {item.title}
                    </Link>
                    <span className="text-xs text-zinc-500">{item.chaptersRead} chapters</span>
                    <span className="w-20 text-right font-medium text-amber-300">
                      {formatReadingTime(item.seconds)}
                    </span>
                  </li>
                ))}
              </ul>
            </StatCard>
          )}
        </div>
      )}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-5">
      <div className="text-3xl font-semibold text-amber-300">{value}</div>
      <div className="mt-1 text-xs uppercase tracking-wide text-zinc-500">{label}</div>
    </div>
  );
}

function StatCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-5">
      <h2 className="mb-3 text-sm font-semibold text-zinc-200">{title}</h2>
      {children}
    </section>
  );
}

function StatRow({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="flex items-center justify-between py-1 text-sm">
      <span className="text-zinc-400">{label}</span>
      <span className="font-medium text-zinc-100">{value}</span>
    </div>
  );
}
