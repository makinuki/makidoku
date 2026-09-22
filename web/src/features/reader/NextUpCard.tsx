import { Check } from "lucide-react";
import type { Chapter } from "../../types";
import { chapterLabelFor } from "./engine/chapters";

export function NextUpCard({
  next,
  autoAdvance,
  queueNote,
  trackerCount,
  onNext,
  onDownload,
  onStay,
  onDetails,
}: {
  next: Chapter;
  autoAdvance: boolean;
  queueNote: string;
  trackerCount: number;
  onNext: () => void;
  onDownload: () => void;
  onStay: () => void;
  onDetails: () => void;
}) {
  return (
    <div className="absolute inset-x-0 bottom-16 z-40 mx-auto w-80 max-w-[90vw] rounded-xl border border-(--reader-border) bg-(--reader-bar) p-4 text-(--reader-text) shadow-2xl">
      <p className="text-xs uppercase tracking-wide text-(--reader-dim)">Chapter finished</p>
      <b className="mt-1 block truncate text-sm">Up next: {chapterLabelFor(next)}</b>
      {autoAdvance && (
        <p className="mt-1 text-xs text-(--reader-dim)">
          Auto-advancing shortly. Stay to keep reading.
        </p>
      )}
      {trackerCount > 0 && (
        <p className="mt-1 text-xs text-(--reader-dim)">
          Chapter progress syncs to {trackerCount} bound{" "}
          {trackerCount === 1 ? "tracker" : "trackers"}.
        </p>
      )}
      <div className="mt-3 flex flex-wrap gap-2">
        <button
          onClick={onNext}
          className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
        >
          Next chapter
        </button>
        {next.downloaded ? (
          <span className="inline-flex items-center gap-1 self-center text-xs text-emerald-300">
            <Check size={13} /> Saved
          </span>
        ) : (
          <button
            onClick={onDownload}
            className="rounded-lg border border-(--reader-border) px-3 py-2 text-sm"
          >
            Download next
          </button>
        )}
        <button
          onClick={onStay}
          className="rounded-lg border border-(--reader-border) px-3 py-2 text-sm"
        >
          Keep reading
        </button>
        <button
          onClick={onDetails}
          className="rounded-lg border border-(--reader-border) px-3 py-2 text-sm text-(--reader-dim)"
        >
          Details
        </button>
      </div>
      {queueNote && (
        <p role="status" className="mt-2 text-xs text-emerald-300">
          {queueNote}
        </p>
      )}
    </div>
  );
}
