// Shared engine lifecycle for one chapter: creates the store once and keeps
// the live options (pages, callbacks) fresh without recreating it. Mode and
// direction flow through setMode/setDirection effects so engine transitions
// (spread alignment on entering double mode) apply exactly once.

import { useEffect, useMemo, useSyncExternalStore } from "react";
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
  type MekuriState,
} from "@makinuki/mekuri/engine";
import { MEKURI_SPREAD_CONFIG, toMekuriMode } from "./mekuriAdapter";
import type { ReaderDirection, ReaderDisplay, ReaderMode } from "./readerSettings";

export interface UseMekuriReaderArgs {
  pages: MekuriPage[];
  mode: ReaderMode;
  direction: ReaderDirection;
  navigation: ReaderDisplay["navigation"];
  initialPageIndex: number;
  enabled: boolean;
  keyboardMap?: MekuriEngineOptions["keyboardMap"];
  // Host-owned suppression flag, copied into the live boolean option on every
  // render so open drawers and focused inputs own the keyboard.
  suppressKeyboard?: boolean;
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
        spreadConfig: { ...MEKURI_SPREAD_CONFIG },
      },
      zoneMap: ZONE_MAP_PRESETS[args.navigation],
    };
    return options;
  }, [enabled]);
  const live = optionsRef;

  if (live !== null) {
    live.pages = args.pages;
    live.zoneMap = ZONE_MAP_PRESETS[args.navigation];
    live.keyboardMap = args.keyboardMap;
    live.isKeyboardSuppressed = args.suppressKeyboard;
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

// Subscribes to engine page position for host chrome. Returns the fallback
// while the engine is not yet created so chrome renders during loading.
export function useMekuriPageIndex(engine: MekuriEngine | null, fallback: number): number {
  const snapshot = useEngineStateFragment(engine, (state) => state.pageIndex);
  return snapshot ?? fallback;
}

// Subscribes to engine HUD visibility for host chrome. Returns the fallback
// while the engine is not yet created so chrome renders during loading.
export function useMekuriHudVisible(engine: MekuriEngine | null, fallback: boolean): boolean {
  const snapshot = useEngineStateFragment(engine, (state) => state.isHUDVisible);
  return snapshot ?? fallback;
}

// Subscribes to the engine failure registry for the host retry pill. Returns
// an empty list while the engine is not yet created.
export function useMekuriFailureIds(engine: MekuriEngine | null): Array<string | number> {
  const snapshot = useEngineStateFragment(engine, (state) => state.failures);
  if (snapshot === null) return [];
  return Object.keys(snapshot);
}

// Minimal selector over the engine store so callers subscribe to one field.
// The hook below always subscribes, even when the engine is null, so host
// components keep a stable hook order across loading states.
function useEngineStateFragment<T>(
  engine: MekuriEngine | null,
  select: (state: MekuriState) => T,
): T | null {
  const state = useSyncExternalStore(
    (notify) => engine?.subscribe(notify) ?? (() => {}),
    () => (engine === null ? null : select(engine.getState())),
    () => (engine === null ? null : select(engine.getState())),
  );
  return state;
}
