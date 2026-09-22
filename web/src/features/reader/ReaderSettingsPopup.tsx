import { ZONE_MAP_PRESETS, type MekuriEngine } from "@makinuki/mekuri/engine";
import { MekuriZoneOverlay } from "@makinuki/mekuri/views";
import type {
  ReaderDirection,
  ReaderDisplay,
  ReaderFit,
  ReaderGlobals,
  ReaderMode,
  ReaderNavigation,
  ReaderOverrides,
  ReaderTheme,
} from "./readerSettings";

// In-reader settings panel: per-title overrides on top of the global
// preferences, then the global display and reading toggles. Writes go
// straight through to the daemon while the parent keeps local state for
// instant feedback.
export function ReaderSettingsPopup({
  overrides,
  globals,
  display,
  engine,
  autoAdvance,
  keepAwake,
  onOverride,
  onDisplaySetting,
  onAutoAdvance,
  onKeepAwake,
}: {
  overrides: ReaderOverrides;
  globals: ReaderGlobals;
  display: ReaderDisplay;
  engine: MekuriEngine;
  autoAdvance: boolean;
  keepAwake: boolean;
  onOverride: (patch: Partial<ReaderOverrides>) => void;
  onDisplaySetting: (key: string, value: string | number | boolean) => void;
  onAutoAdvance: (value: boolean) => void;
  onKeepAwake: (value: boolean) => void;
}) {
  return (
    <div className="absolute right-3 top-14 z-40 max-h-[80vh] w-72 max-w-[90vw] space-y-4 overflow-y-auto rounded-xl border border-(--reader-border) bg-(--reader-bar) p-3 shadow-2xl">
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <b className="text-sm">This title</b>
          <span className="text-xs text-(--reader-dim)">Overrides</span>
        </div>
        <label className="block text-xs text-(--reader-dim)">
          Mode
          <select
            value={overrides.mode ?? ""}
            onChange={(event) =>
              onOverride({
                mode: (event.target.value || null) as ReaderMode | null,
              })
            }
            className="mt-1 w-full rounded-lg border border-(--reader-border) bg-(--reader-canvas) px-2 py-1 text-sm text-(--reader-text)"
          >
            <option value="">Default ({globals.mode})</option>
            <option value="single">Single</option>
            <option value="double">Double</option>
            <option value="webtoon">Webtoon</option>
          </select>
        </label>
        <label className="block text-xs text-(--reader-dim)">
          Direction
          <select
            value={overrides.direction ?? ""}
            onChange={(event) =>
              onOverride({
                direction: (event.target.value || null) as ReaderDirection | null,
              })
            }
            className="mt-1 w-full rounded-lg border border-(--reader-border) bg-(--reader-canvas) px-2 py-1 text-sm text-(--reader-text)"
          >
            <option value="">Default ({globals.direction})</option>
            <option value="ltr">Left to right</option>
            <option value="rtl">Right to left</option>
          </select>
        </label>
        <label className="block text-xs text-(--reader-dim)">
          Fit
          <select
            value={overrides.fit ?? ""}
            onChange={(event) =>
              onOverride({
                fit: (event.target.value || null) as ReaderFit | null,
              })
            }
            className="mt-1 w-full rounded-lg border border-(--reader-border) bg-(--reader-canvas) px-2 py-1 text-sm text-(--reader-text)"
          >
            <option value="">Default ({globals.fit})</option>
            <option value="width">Fit width</option>
            <option value="height">Fit height</option>
            <option value="screen">Fit screen</option>
            <option value="original">Original</option>
          </select>
        </label>
      </section>
      <section className="space-y-3 border-t border-(--reader-border) pt-3">
        <b className="text-sm">Display</b>
        <label className="block text-xs text-(--reader-dim)">
          Tap and click zones
          <select
            aria-label="Tap and click zones"
            value={display.navigation}
            onChange={(event) =>
              onDisplaySetting("reader.navigation", event.target.value as ReaderNavigation)
            }
            className="mt-1 w-full rounded-lg border border-(--reader-border) bg-(--reader-canvas) px-2 py-1 text-sm text-(--reader-text)"
          >
            <option value="default-manga">Default manga</option>
            <option value="l-shaped">L-shaped</option>
            <option value="edge-only">Edges only</option>
            <option value="disabled">Disabled</option>
          </select>
        </label>
        <div
          aria-hidden="true"
          className="relative h-16 w-full overflow-hidden rounded-lg border border-(--reader-border) bg-(--reader-canvas)"
        >
          <MekuriZoneOverlay engine={engine} zoneMap={ZONE_MAP_PRESETS[display.navigation]} />
        </div>
        {display.navigation === "disabled" && (
          <p className="text-[11px] text-(--reader-dim)">Zones off. Use keys or swipe.</p>
        )}
        <label className="block text-xs text-(--reader-dim)">
          Theme
          <select
            aria-label="Reader theme"
            value={display.theme}
            onChange={(event) =>
              onDisplaySetting("reader.theme", event.target.value as ReaderTheme)
            }
            className="mt-1 w-full rounded-lg border border-(--reader-border) bg-(--reader-canvas) px-2 py-1 text-sm text-(--reader-text)"
          >
            <option value="dark">Dark</option>
            <option value="amoled">AMOLED black</option>
            <option value="paper">Paper</option>
            <option value="light">Light</option>
          </select>
        </label>
        <label className="block text-xs text-(--reader-dim)">
          Image brightness · {display.brightness}%
          <input
            type="range"
            aria-label="Image brightness"
            min={50}
            max={150}
            step={5}
            value={display.brightness}
            onChange={(event) => onDisplaySetting("reader.brightness", Number(event.target.value))}
            className="mt-1 w-full accent-amber-400"
          />
        </label>
        <label className="block text-xs text-(--reader-dim)">
          Webtoon gap · {display.gap}px
          <input
            type="range"
            aria-label="Webtoon gap"
            min={0}
            max={48}
            step={4}
            value={display.gap}
            onChange={(event) => onDisplaySetting("reader.webtoon_gap", Number(event.target.value))}
            className="mt-1 w-full accent-amber-400"
          />
        </label>
        <label className="flex items-center gap-2 text-xs text-(--reader-text)">
          <input
            type="checkbox"
            checked={display.grayscale}
            onChange={(event) => onDisplaySetting("reader.grayscale", event.target.checked)}
            className="accent-amber-400"
          />
          Grayscale images
        </label>
        <label className="flex items-center gap-2 text-xs text-(--reader-text)">
          <input
            type="checkbox"
            checked={display.invert}
            onChange={(event) => onDisplaySetting("reader.invert", event.target.checked)}
            className="accent-amber-400"
          />
          Invert image colors
        </label>
      </section>
      <section className="space-y-2 border-t border-(--reader-border) pt-3">
        <b className="text-sm">Reading</b>
        <label className="flex items-center gap-2 text-xs text-(--reader-text)">
          <input
            type="checkbox"
            checked={autoAdvance}
            onChange={(event) => onAutoAdvance(event.target.checked)}
            className="accent-amber-400"
          />
          Auto-advance to the next chapter
        </label>
        <label className="flex items-center gap-2 text-xs text-(--reader-text)">
          <input
            type="checkbox"
            checked={keepAwake}
            onChange={(event) => onKeepAwake(event.target.checked)}
            className="accent-amber-400"
          />
          Keep screen on while reading
        </label>
      </section>
    </div>
  );
}
