// Test stand-in for the vite-plugin-pwa virtual module, which the Vitest
// module runner cannot resolve (it is served as a dev URL, not a file).
// Mirrors the registerSW contract: options are accepted and ignored, and the
// returned updater resolves without doing anything.
interface RegisterSWOptions {
  onNeedRefresh?: () => void;
  onOfflineReady?: () => void;
}

export function registerSW(_options?: RegisterSWOptions) {
  return async (_reloadPage?: boolean) => {};
}
