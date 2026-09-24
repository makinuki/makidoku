import { useState } from "react";

export function GoToDialog({
  pageCount,
  current,
  onJump,
  onClose,
}: {
  pageCount: number;
  current: number;
  onJump: (page: number) => void;
  onClose: () => void;
}) {
  const [value, setValue] = useState(String(current));
  return (
    <div
      className="absolute inset-0 z-40 grid place-items-center bg-black/70 p-5"
      onMouseDown={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Go to page"
        className="w-64 rounded-xl border border-(--reader-border) bg-(--reader-bar) p-4 text-(--reader-text) shadow-2xl"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <b className="text-sm">Go to page</b>
        <form
          className="mt-3 flex gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            const page = Math.min(Math.max(1, Number(value) || 1), Math.max(1, pageCount));
            onJump(page);
          }}
        >
          <input
            // eslint-disable-next-line jsx-a11y/no-autofocus
            autoFocus
            aria-label="Page number"
            inputMode="numeric"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            className="min-w-0 flex-1 rounded-lg border border-(--reader-border) bg-(--reader-canvas) px-2 py-1.5 text-base sm:text-sm"
          />
          <button
            type="submit"
            className="rounded-lg bg-amber-400 px-3 py-1.5 text-sm font-semibold text-zinc-950"
          >
            Go
          </button>
        </form>
        <p className="mt-2 text-xs text-(--reader-dim)">
          Page {current} of {pageCount}
        </p>
      </div>
    </div>
  );
}
