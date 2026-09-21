// Pure adapter between makidoku reader settings and the mekuri engine.
// This module owns no state and touches no DOM: it maps makidoku's persisted
// setting vocabularies onto mekuri engine options so ReaderPage can stay thin.

import type {
  MekuriEngineOptions,
  MekuriMode,
  MekuriPage,
  MekuriZoneMap,
} from "@makinuki/mekuri/engine";
import type { Page } from "../../types";
import type { ReaderDirection, ReaderDisplay, ReaderGlobals, ReaderMode } from "./readerSettings";

export const MEKURI_SPREAD_CONFIG = {
  firstPageIsCover: true,
  landscapeThreshold: 1.2,
} as const;

const ZONE_MAP_BY_NAVIGATION: Record<ReaderDisplay["navigation"], MekuriZoneMap["name"]> = {
  default: "default-manga",
  l: "l-shaped",
  edge: "edge-only",
  disabled: "disabled",
};

// Maps the persisted webtoon value onto the continuous mode with a
// configurable gap. The gapless continuous-webtoon contract stays unused
// until a host distinguishes the two.
export function toMekuriMode(mode: ReaderMode): MekuriMode {
  return mode === "webtoon" ? "continuous-vertical" : mode;
}

export function toZoneMapName(navigation: ReaderDisplay["navigation"]): MekuriZoneMap["name"] {
  return ZONE_MAP_BY_NAVIGATION[navigation];
}

export function toMekuriPages(pages: Page[]): MekuriPage[] {
  return pages.map((page) => ({ id: page.id }));
}

export interface MekuriReaderInput {
  pages: Page[];
  globals: ReaderGlobals;
  direction: ReaderDirection;
  index: number;
  zoneMap?: MekuriZoneMap;
  resolveImage: (page: Page, attempt: number) => string;
  onPositionSample?: MekuriEngineOptions["onPositionSample"];
  onBoundaryReached?: MekuriEngineOptions["onBoundaryReached"];
}

// Builds engine options for one chapter. The resolveSrc closure adds the
// attempt cache-buster the current reader appends by hand.
export function buildMekuriEngineOptions(input: MekuriReaderInput): MekuriEngineOptions {
  const mekuriPages = toMekuriPages(input.pages);
  const byId = new Map(mekuriPages.map((page, at) => [page.id, input.pages[at] as Page]));
  return {
    pages: mekuriPages,
    initialState: {
      pageIndex: Math.min(Math.max(0, input.index), Math.max(0, mekuriPages.length - 1)),
      mode: toMekuriMode(input.globals.mode),
      direction: input.direction,
      spreadConfig: { ...MEKURI_SPREAD_CONFIG },
    },
    zoneMap: input.zoneMap,
    resolveSrc: (page, attempt) => {
      const source = byId.get(page.id);
      if (source === undefined) return "";
      return input.resolveImage(source, attempt);
    },
    onPositionSample: input.onPositionSample,
    onBoundaryReached: input.onBoundaryReached,
  };
}
