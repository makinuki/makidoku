// Pure adapter between makidoku reader settings and the mekuri engine.
// This module owns no state and touches no DOM: it maps makidoku's persisted
// setting vocabularies onto mekuri engine options so ReaderPage can stay thin.

import type { MekuriMode, MekuriPage } from "@makinuki/mekuri/engine";
import type { Page } from "../../types";
import type { ReaderMode } from "./readerSettings";

export const MEKURI_SPREAD_CONFIG = {
  firstPageIsCover: true,
  landscapeThreshold: 1.2,
} as const;

// Maps the persisted webtoon value onto the continuous mode with a
// configurable gap. The gapless continuous-webtoon contract stays unused
// until a host distinguishes the two.
export function toMekuriMode(mode: ReaderMode): MekuriMode {
  return mode === "webtoon" ? "continuous-vertical" : mode;
}

export function toMekuriPages(pages: Page[]): MekuriPage[] {
  return pages.map((page) => ({ id: page.id }));
}
