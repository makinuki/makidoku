// Shared engine lifecycle for one chapter: creates the store once and keeps
// the live options (pages, callbacks) fresh without recreating it. Mode and
// direction flow through setMode/setDirection effects so engine transitions
// (spread alignment on entering double mode) apply exactly once.

import { useEffect, useMemo } from "react";
import {
  ZONE_MAP_PRESETS,
  createMekuriEngine,
  type ChapterBoundary,
  type MekuriDirection,
  type MekuriEngine,
  type MekuriEngineOptions,
  type MekuriMode,
  type MekuriPage,
  type MekuriReadingPosition,
} from "@makinuki/mekuri/engine";
import { toMekuriMode, toZoneMapName } from "./mekuriAdapter";
import type { ReaderDirection, ReaderDisplay, ReaderMode } from "./readerSettings";

export interface UseMekuriReaderArgs {
  pages: MekuriPage[];
  mode: ReaderMode;
  direction: ReaderDirection;
  navigation: ReaderDisplay["navigation"];
  initialPageIndex: number;
  enabled: boolean;
  resolveImage: (pageId: string | number, attempt: number) => string;
  onPositionSample?: (position: MekuriReadingPosition) => void;
  onBoundaryReached?: (boundary: ChapterBoundary) => void;
}

// Creates and syncs the chapter engine. Returns null until enabled (pages
// loaded) so hooks stay unconditional while loading states render early.
export function useMekuriReader(args: UseMekuriReaderArgs): MekuriEngine | null {
  const { initialPageIndex, enabled } = args;
  const optionsRef = useMemo<MekuriEngineOptions | null>(() => {
    if (!enabled) return null;
    const options: MekuriEngineOptions = {
      pages: args.pages,
      initialState: {
        pageIndex: Math.min(Math.max(0, initialPageIndex), Math.max(0, args.pages.length - 1)),
        mode: toMekuriMode(args.mode),
        direction: args.direction,
      },
      zoneMap: ZONE_MAP_PRESETS[toZoneMapName(args.navigation)],
    };
    return options;
  }, [enabled]);
  const live = optionsRef;

  if (live !== null) {
    live.pages = args.pages;
    live.zoneMap = ZONE_MAP_PRESETS[toZoneMapName(args.navigation)];
    live.resolveSrc = (page, attempt) => args.resolveImage(page.id, attempt);
    live.onPositionSample = args.onPositionSample;
    live.onBoundaryReached = args.onBoundaryReached;
  }

  const engine = useMemo(() => (live === null ? null : createMekuriEngine(live)), [live]);
  const mode: MekuriMode = toMekuriMode(args.mode);
  const direction: MekuriDirection = args.direction;

  useEffect(() => {
    engine?.setMode(mode);
  }, [engine, mode]);

  useEffect(() => {
    engine?.setDirection(direction);
  }, [engine, direction]);

  return engine;
}
