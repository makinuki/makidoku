export type ReaderMode = "single" | "double" | "webtoon";
export type ReaderDirection = "ltr" | "rtl";
export type ReaderFit = "width" | "height" | "original";

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
    fit: fit === "height" || fit === "original" ? fit : "width",
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
