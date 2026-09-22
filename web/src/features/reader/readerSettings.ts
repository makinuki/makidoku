export type ReaderMode = "single" | "double" | "webtoon";
export type ReaderDirection = "ltr" | "rtl";
export type ReaderFit = "width" | "height" | "screen" | "original";
export type ReaderNavigation = "default-manga" | "l-shaped" | "edge-only" | "disabled";
export type ReaderTheme = "dark" | "amoled" | "paper" | "light";

export type ReaderGlobals = {
  mode: ReaderMode;
  direction: ReaderDirection;
  fit: ReaderFit;
};

export type ReaderOverrides = {
  mode: ReaderMode | null;
  direction: ReaderDirection | null;
  fit: ReaderFit | null;
};

export const defaultReaderGlobals: ReaderGlobals = {
  mode: "single",
  direction: "ltr",
  fit: "width",
};

export const emptyReaderOverrides: ReaderOverrides = {
  mode: null,
  direction: null,
  fit: null,
};

// Reads the global reader preferences from the runtime settings service,
// ignoring unknown values so a stale or malformed entry cannot break the
// reader.
export function readerGlobalsFromSettings(
  settings: Iterable<{ key: string; value: string | number | boolean }>,
): ReaderGlobals {
  const values = new Map<string, string>();
  for (const item of settings) values.set(item.key, String(item.value));
  const mode = values.get("reader.default_mode");
  const direction = values.get("reader.direction");
  const fit = values.get("reader.fit");
  return {
    mode: mode === "double" || mode === "webtoon" ? mode : "single",
    direction: direction === "rtl" ? "rtl" : "ltr",
    fit: fit === "height" || fit === "screen" || fit === "original" ? fit : "width",
  };
}

// Maps the per-title override fields stored on a manga. A missing value means
// the title follows the global setting.
export function readerOverridesFromManga(manga: {
  readerMode?: ReaderMode | null;
  readerDirection?: ReaderDirection | null;
  readerFit?: ReaderFit | null;
}): ReaderOverrides {
  return {
    mode: manga.readerMode ?? null,
    direction: manga.readerDirection ?? null,
    fit: manga.readerFit ?? null,
  };
}

// A double-page spread opens on the pair containing a page. All paged
// navigation (slider, hotkeys, chevrons) and resume share this helper so a
// spread can never land on a mispaired odd index.
export function alignToSpread(index: number, pageCount: number, mode: ReaderMode): number {
  const clamped = Math.min(Math.max(0, index), Math.max(0, pageCount - 1));
  return mode === "double" ? clamped - (clamped % 2) : clamped;
}

export function spreadStep(mode: ReaderMode): number {
  return mode === "double" ? 2 : 1;
}
// Display preferences are global-only (no per-title columns): navigation
// zones, webtoon gap, theme, and image filters apply to every title.
export type ReaderDisplay = {
  navigation: ReaderNavigation;
  gap: number;
  theme: ReaderTheme;
  brightness: number;
  grayscale: boolean;
  invert: boolean;
};

export const defaultReaderDisplay: ReaderDisplay = {
  navigation: "default-manga",
  gap: 8,
  theme: "dark",
  brightness: 100,
  grayscale: false,
  invert: false,
};

// Retired preset names stored before the rename. The daemon maps them on
// read, and this mirrors that mapping so stale values never break the reader.
const retiredNavigationNames: Record<string, ReaderNavigation> = {
  default: "default-manga",
  l: "l-shaped",
  edge: "edge-only",
};

function toReaderNavigation(value: string | undefined): ReaderNavigation {
  if (
    value === "default-manga" ||
    value === "l-shaped" ||
    value === "edge-only" ||
    value === "disabled"
  ) {
    return value;
  }
  return (value !== undefined ? retiredNavigationNames[value] : undefined) ?? "default-manga";
}

export function readerDisplayFromSettings(
  settings: Iterable<{ key: string; value: string | number | boolean }>,
): ReaderDisplay {
  const values = new Map<string, string>();
  for (const item of settings) values.set(item.key, String(item.value));
  const navigation = values.get("reader.navigation");
  const theme = values.get("reader.theme");
  const gap = Number(values.get("reader.webtoon_gap"));
  const brightness = Number(values.get("reader.brightness"));
  return {
    navigation: toReaderNavigation(navigation),
    gap: Number.isFinite(gap) ? Math.min(48, Math.max(0, Math.round(gap))) : 8,
    theme: theme === "amoled" || theme === "paper" || theme === "light" ? theme : "dark",
    brightness: Number.isFinite(brightness)
      ? Math.min(150, Math.max(50, Math.round(brightness)))
      : 100,
    grayscale: values.get("reader.grayscale") === "true",
    invert: values.get("reader.invert") === "true",
  };
}

// The CSS filter chain applied to reader images. Defaults collapse to "none"
// so the compositor can skip filtering entirely.
export function readerImageFilter(display: ReaderDisplay): string {
  const parts: string[] = [];
  if (display.brightness !== 100) parts.push(`brightness(${display.brightness}%)`);
  if (display.grayscale) parts.push("grayscale(1)");
  if (display.invert) parts.push("invert(1)");
  return parts.length ? parts.join(" ") : "none";
}
// A per-title override wins over the global setting; a null override falls back
// to the global value.
export function resolveReaderSettings(
  globals: ReaderGlobals,
  overrides: ReaderOverrides,
): ReaderGlobals {
  return {
    mode: overrides.mode ?? globals.mode,
    direction: overrides.direction ?? globals.direction,
    fit: overrides.fit ?? globals.fit,
  };
}

// resumeIndex converts a stored 1-based last-read page into the 0-based page
// index a reader session opens on. A double-page spread opens on the pair
// containing that page, and a missing or out-of-range value clamps to the
// available pages.
export function resumeIndex(
  lastReadPage: number | null,
  pageCount: number,
  mode: ReaderMode,
): number {
  const zeroBased = (lastReadPage ?? 1) - 1;
  return alignToSpread(zeroBased, pageCount, mode);
}
