import {
  ArrowLeft,
  Check,
  EyeOff,
  List,
  Maximize,
  Minimize,
  SlidersHorizontal,
} from "lucide-react";
import type { ReaderMode } from "./readerSettings";

// Top chrome bar: exit, title and session badges, the chapter drawer toggle,
// the per-title mode quick-switch, fullscreen, settings, and help.
export function ReaderHeader({
  visible,
  hasManga,
  title,
  chapterLabel,
  incognito,
  downloaded,
  online,
  drawerOpen,
  mode,
  isFullscreen,
  settingsOpen,
  onBack,
  onToggleDrawer,
  onMode,
  onToggleFullscreen,
  onToggleSettings,
  onShowHelp,
}: {
  visible: boolean;
  hasManga: boolean;
  title: string;
  chapterLabel: string;
  incognito: boolean;
  downloaded: boolean;
  online: boolean;
  drawerOpen: boolean;
  mode: ReaderMode;
  isFullscreen: boolean;
  settingsOpen: boolean;
  onBack: () => void;
  onToggleDrawer: () => void;
  onMode: (mode: ReaderMode) => void;
  onToggleFullscreen: () => void;
  onToggleSettings: () => void;
  onShowHelp: () => void;
}) {
  const backLabel = hasManga ? "Back to details" : "Back to library";
  return (
    <div
      className={`flex items-center gap-3 border-b border-(--reader-border) bg-(--reader-bar) px-3 py-2 ${visible ? "" : "hidden"}`}
    >
      <button
        aria-label={backLabel}
        title={backLabel}
        onClick={onBack}
        className="rounded-lg p-2 text-(--reader-dim) hover:bg-(--reader-border)"
      >
        <ArrowLeft size={18} />
      </button>
      <div className="min-w-0 flex-1">
        <b className="block truncate text-sm">{title}</b>
        <small className="text-(--reader-dim)">{chapterLabel}</small>
        {incognito && (
          <span className="mt-0.5 inline-flex items-center gap-1 rounded-full bg-amber-400/15 px-2 py-0.5 text-[11px] text-amber-300">
            <EyeOff size={12} /> Incognito
          </span>
        )}
        {downloaded && (
          <span
            title="This chapter is downloaded and reads offline"
            className="mt-0.5 inline-flex items-center gap-1 rounded-full bg-emerald-400/15 px-2 py-0.5 text-[11px] text-emerald-300"
          >
            <Check size={12} /> Saved
          </span>
        )}
        {!online && (
          <span
            title="No network connection; remote sources are unreachable"
            className="mt-0.5 inline-flex items-center gap-1 rounded-full bg-red-400/15 px-2 py-0.5 text-[11px] text-red-300"
          >
            No network
          </span>
        )}
      </div>
      <div className="flex gap-1" role="group" aria-label="Reader mode">
        <button
          aria-label="Chapters"
          title="Chapters"
          onClick={onToggleDrawer}
          className={`rounded-lg p-2 ${drawerOpen ? "bg-(--reader-border) text-amber-400" : "text-(--reader-dim) hover:bg-(--reader-border)"}`}
        >
          <List size={16} />
        </button>
        <button
          aria-pressed={mode === "single"}
          onClick={() => onMode("single")}
          className={`rounded-lg px-2 py-1 text-xs ${mode === "single" ? "bg-amber-400 text-zinc-950" : "text-(--reader-dim)"}`}
        >
          Single
        </button>
        <button
          aria-pressed={mode === "double"}
          onClick={() => onMode("double")}
          className={`rounded-lg px-2 py-1 text-xs ${mode === "double" ? "bg-amber-400 text-zinc-950" : "text-(--reader-dim)"}`}
        >
          Double
        </button>
        <button
          aria-pressed={mode === "webtoon"}
          onClick={() => onMode("webtoon")}
          className={`rounded-lg px-2 py-1 text-xs ${mode === "webtoon" ? "bg-amber-400 text-zinc-950" : "text-(--reader-dim)"}`}
        >
          Webtoon
        </button>
        <button
          aria-label={isFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
          aria-pressed={isFullscreen}
          title={isFullscreen ? "Exit fullscreen (F)" : "Enter fullscreen (F)"}
          onClick={onToggleFullscreen}
          className="rounded-lg p-2 text-(--reader-dim) hover:bg-(--reader-border)"
        >
          {isFullscreen ? <Minimize size={16} /> : <Maximize size={16} />}
        </button>
        <button
          aria-label="Reader settings"
          onClick={onToggleSettings}
          className={`rounded-lg p-2 ${settingsOpen ? "bg-(--reader-border) text-amber-400" : "text-(--reader-dim) hover:bg-(--reader-border)"}`}
        >
          <SlidersHorizontal size={16} />
        </button>
        <button
          aria-label="Keyboard shortcuts"
          title="Keyboard shortcuts (?)"
          onClick={onShowHelp}
          className="rounded-lg p-2 text-(--reader-dim) hover:bg-(--reader-border)"
        >
          ?
        </button>
      </div>
    </div>
  );
}
