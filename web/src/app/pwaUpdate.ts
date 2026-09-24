// Subscribable wrapper around the service worker registration emitted by
// vite-plugin-pwa. Components consume update lifecycle events (a waiting
// worker, the first offline-ready transition) through the store instead of
// importing the virtual module directly. Registration is a no-op outside
// production builds and where navigator.serviceWorker is unavailable.
import { registerSW } from "virtual:pwa-register";

export interface PwaUpdateState {
  readonly needRefresh: boolean;
  readonly offlineReady: boolean;
  // Incremented on every onNeedRefresh event so a dismissed banner can
  // reappear when a further update arrives without a state transition.
  readonly refreshEvents: number;
}

let state: PwaUpdateState = { needRefresh: false, offlineReady: false, refreshEvents: 0 };
let reloadWithUpdate: ((reloadPage?: boolean) => Promise<void>) | null = null;
let initialized = false;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

export function initPwaUpdate(): void {
  if (initialized || typeof window === "undefined") return;
  initialized = true;
  reloadWithUpdate = registerSW({
    onNeedRefresh() {
      state = { ...state, needRefresh: true, refreshEvents: state.refreshEvents + 1 };
      emit();
    },
    onOfflineReady() {
      state = { ...state, offlineReady: true };
      emit();
    },
  });
}

export function subscribePwaUpdate(listener: () => void): () => void {
  initPwaUpdate();
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getPwaUpdateState(): PwaUpdateState {
  initPwaUpdate();
  return state;
}

// Activates the waiting worker and reloads the page so all clients run the
// new build.
export async function applyPwaUpdate(): Promise<void> {
  if (!reloadWithUpdate) return;
  await reloadWithUpdate(true);
}
