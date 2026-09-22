import { Check, X } from "lucide-react";
import type { Chapter } from "../../types";
import { chapterLabelFor } from "./engine/chapters";

export function ChapterDrawer({
  ordered,
  currentId,
  onSelect,
  onClose,
}: {
  ordered: Chapter[];
  currentId: string;
  onSelect: (id: string) => void;
  onClose: () => void;
}) {
  return (
    <div className="absolute inset-y-0 left-0 z-40 flex w-72 max-w-[80vw] flex-col border-r border-(--reader-border) bg-(--reader-bar) text-(--reader-text) shadow-2xl">
      <div className="flex items-center justify-between border-b border-(--reader-border) px-3 py-2">
        <b className="text-sm">Chapters · {ordered.length}</b>
        <button
          aria-label="Close chapters"
          onClick={onClose}
          className="rounded-lg p-2 text-(--reader-dim) hover:bg-(--reader-border)"
        >
          <X size={16} />
        </button>
      </div>
      <ol className="min-h-0 flex-1 overflow-y-auto">
        {ordered.map((chapter) => (
          <li key={chapter.id}>
            <button
              onClick={() => onSelect(chapter.id)}
              aria-current={chapter.id === currentId ? "true" : undefined}
              className={`flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-(--reader-border) ${
                chapter.id === currentId ? "bg-(--reader-border) text-amber-400" : ""
              } ${chapter.read ? "opacity-60" : ""}`}
            >
              {chapter.read && <Check size={14} className="shrink-0 text-emerald-400" />}
              <span className="min-w-0 flex-1 truncate">{chapterLabelFor(chapter)}</span>
              {chapter.downloaded && (
                <span className="shrink-0 text-[11px] text-(--reader-dim)">saved</span>
              )}
            </button>
          </li>
        ))}
      </ol>
      <p className="border-t border-(--reader-border) px-3 py-2 text-[11px] text-(--reader-dim)">
        N / P jumps to the next / previous chapter.
      </p>
    </div>
  );
}
