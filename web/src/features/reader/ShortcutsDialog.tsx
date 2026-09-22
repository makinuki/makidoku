import { X } from "lucide-react";
import type { MekuriEngine } from "@makinuki/mekuri/engine";
import type { ReaderMode } from "./readerSettings";

// Human-readable label for a KeyboardEvent.code as stored in the engine
// keyboard map. Unknown codes fall through verbatim so a remapped binding
// stays visible even when it has no friendly name here.
function describeKeyCode(code: string): string {
  if (code === "ArrowRight") return "→";
  if (code === "ArrowLeft") return "←";
  if (code === "ArrowUp") return "↑";
  if (code === "ArrowDown") return "↓";
  if (code === "Space") return "Space";
  if (code === "Escape") return "Esc";
  if (code === "Equal") return "=";
  if (code === "Minus") return "-";
  if (code === "NumpadAdd") return "Num+";
  if (code === "NumpadSubtract") return "Num-";
  if (code === "Numpad0") return "Num0";
  if (code.startsWith("Key")) return code.slice(3);
  if (code.startsWith("Digit")) return code.slice(5);
  return code;
}

export function ShortcutsDialog({
  engine,
  mode,
  onClose,
}: {
  engine: MekuriEngine;
  mode: ReaderMode;
  onClose: () => void;
}) {
  // Engine rows render from the bindings in force, so a remapped dispatcher
  // and this list cannot drift apart. The dispatcher mirrors the page-turn
  // pair in right-to-left mode.
  const map = engine.getKeyboardMap();
  const engineRows: Array<{ keys: string[]; label: string }> = [
    { keys: map.nextPage, label: "Next spread" },
    { keys: map.prevPage, label: "Previous spread" },
    { keys: map.toggleHUD, label: "Show or hide the menu" },
    { keys: map.zoomIn, label: "Zoom in" },
    { keys: map.zoomOut, label: "Zoom out" },
    { keys: map.resetZoom, label: "Reset zoom" },
  ];
  const rows: Array<[string, string]> = [
    ...engineRows
      .filter((row) => row.keys.length > 0)
      .map((row) => [row.keys.map(describeKeyCode).join(" / "), row.label] as [string, string]),
    ...(mode !== "webtoon"
      ? ([
          ["Space / Shift+Space, PgDn / PgUp", "Next / previous spread"],
          ["Home / End", "First / last page"],
        ] as Array<[string, string]>)
      : []),
    ["N / P", "Next / previous chapter"],
    ["W", "Cycle single, double, webtoon"],
    ["G", "Go to page"],
    ["F", "Fullscreen"],
    ["Double-tap / double-click center, pinch", "Zoom"],
    ["Swipe left / right", "Turn pages on touch screens"],
    ["?", "This list"],
    ["Esc", "Close dialogs"],
  ];
  return (
    <div
      className="absolute inset-0 z-40 grid place-items-center bg-black/70 p-5"
      onMouseDown={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Keyboard shortcuts"
        className="w-80 max-w-full rounded-xl border border-(--reader-border) bg-(--reader-bar) p-4 text-(--reader-text) shadow-2xl"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <b className="text-sm">Shortcuts</b>
          <button
            aria-label="Close shortcuts"
            onClick={onClose}
            className="rounded-lg p-2 text-(--reader-dim) hover:bg-(--reader-border)"
          >
            <X size={16} />
          </button>
        </div>
        <dl className="mt-3 space-y-2 text-xs">
          {rows.map(([keys, action]) => (
            <div key={keys} className="flex items-center justify-between gap-3">
              <dt className="shrink-0 rounded bg-(--reader-border) px-1.5 py-0.5 font-mono text-[11px]">
                {keys}
              </dt>
              <dd className="text-right text-(--reader-dim)">{action}</dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}
