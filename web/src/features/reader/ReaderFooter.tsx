import { Menu } from "lucide-react";

// Bottom chrome bar: reading position with the go-to entry point, the page
// slider, and the chrome hide control.
export function ReaderFooter({
  visible,
  index,
  pageCount,
  percent,
  onShowGoTo,
  onSeek,
  onHide,
}: {
  visible: boolean;
  index: number;
  pageCount: number;
  percent: number;
  onShowGoTo: () => void;
  onSeek: (pageIndex: number) => void;
  onHide: () => void;
}) {
  const current = Math.min(index + 1, pageCount);
  return (
    <div
      className={`flex items-center gap-3 border-t border-(--reader-border) bg-(--reader-bar) px-4 py-2 ${visible ? "" : "hidden"}`}
    >
      <button
        onClick={onShowGoTo}
        title="Go to page (G)"
        aria-label={`Go to page, currently page ${current} of ${pageCount}, ${percent} percent read`}
        className="shrink-0 text-xs text-(--reader-dim) hover:text-(--reader-text)"
      >
        {current} / {pageCount} · {percent}%
      </button>
      <input
        type="range"
        aria-label="Page"
        aria-valuemin={1}
        aria-valuemax={pageCount}
        aria-valuenow={current}
        aria-valuetext={`Page ${current} of ${pageCount}`}
        min="0"
        max={Math.max(0, pageCount - 1)}
        value={index}
        onChange={(e) => onSeek(Number(e.target.value))}
        className="flex-1 accent-amber-400"
      />
      <button
        aria-label="Hide reader menu"
        aria-expanded={visible}
        onClick={onHide}
        className="rounded-lg p-2 text-(--reader-dim)"
      >
        <Menu size={16} />
      </button>
    </div>
  );
}
