// Deferred-capture store for the browser install prompt. Chromium fires
// beforeinstallprompt once installability criteria are met; the event is
// stashed here so the More page can trigger the prompt from its own row
// instead of relying on the browser's mini-infobar. iOS has no such event,
// so callers fall back to manual Add to Home Screen instructions there.

export interface BeforeInstallPromptEvent extends Event {
  readonly userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
  prompt(): Promise<void>;
}

export interface InstallState {
  readonly standalone: boolean;
  readonly canPrompt: boolean;
  readonly isIos: boolean;
}

let deferredPrompt: BeforeInstallPromptEvent | null = null;
let state: InstallState = { standalone: false, canPrompt: false, isIos: false };
let initialized = false;
const listeners = new Set<() => void>();

function detectStandalone(): boolean {
  const nav = window.navigator as Navigator & { standalone?: boolean };
  if (nav.standalone === true) return true;
  return (
    typeof window.matchMedia === "function" &&
    window.matchMedia("(display-mode: standalone)").matches
  );
}

function detectIos(): boolean {
  const ua = window.navigator.userAgent;
  if (/iP(hone|ad|od)/.test(ua)) return true;
  // iPadOS reports a Macintosh user agent; touch points distinguish it.
  return /Macintosh/.test(ua) && window.navigator.maxTouchPoints > 1;
}

function refresh(): void {
  state = {
    standalone: detectStandalone(),
    canPrompt: deferredPrompt !== null,
    isIos: state.isIos,
  };
  for (const listener of listeners) listener();
}

export function initInstallPrompt(): void {
  if (initialized || typeof window === "undefined") return;
  initialized = true;
  state = { standalone: detectStandalone(), canPrompt: false, isIos: detectIos() };
  window.addEventListener("beforeinstallprompt", (event) => {
    // The mini-infobar stays suppressed; the More page row is the entry point.
    event.preventDefault();
    deferredPrompt = event as BeforeInstallPromptEvent;
    refresh();
  });
  window.addEventListener("appinstalled", () => {
    deferredPrompt = null;
    refresh();
  });
  if (typeof window.matchMedia === "function") {
    window.matchMedia("(display-mode: standalone)").addEventListener("change", refresh);
  }
}

export function subscribeInstallPrompt(listener: () => void): () => void {
  initInstallPrompt();
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getInstallState(): InstallState {
  initInstallPrompt();
  return state;
}

export async function promptInstall(): Promise<"accepted" | "dismissed" | "unavailable"> {
  const event = deferredPrompt;
  if (!event) return "unavailable";
  // The deferred event is single-use; clear it before prompting so the row
  // never offers a stale prompt.
  deferredPrompt = null;
  refresh();
  await event.prompt();
  const choice = await event.userChoice;
  return choice.outcome;
}
