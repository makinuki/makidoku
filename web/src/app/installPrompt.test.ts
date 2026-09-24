import { afterEach, describe, expect, it, vi } from "vite-plus/test";

const originalUserAgent = window.navigator.userAgent;

type Store = typeof import("./installPrompt");

async function freshStore(): Promise<Store> {
  // The module keeps process-wide singleton state; every scenario starts
  // from a fresh copy.
  vi.resetModules();
  return import("./installPrompt");
}

function stubUserAgent(value: string): void {
  Object.defineProperty(window.navigator, "userAgent", { value, configurable: true });
}

function stubStandaloneMedia(matches: boolean): void {
  Object.defineProperty(window, "matchMedia", {
    value: vi.fn().mockReturnValue({
      matches,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }),
    configurable: true,
    writable: true,
  });
}

function dispatchInstallPrompt(outcome: "accepted" | "dismissed") {
  const prompt = vi.fn().mockResolvedValue(undefined);
  const event = new Event("beforeinstallprompt", { cancelable: true });
  Object.assign(event, { prompt, userChoice: Promise.resolve({ outcome }) });
  window.dispatchEvent(event);
  return { event, prompt };
}

afterEach(() => {
  Object.defineProperty(window.navigator, "userAgent", {
    value: originalUserAgent,
    configurable: true,
  });
  delete (window as { matchMedia?: unknown }).matchMedia;
});

describe("install prompt store", () => {
  it("starts without a prompt outside standalone display", async () => {
    const store = await freshStore();
    expect(store.getInstallState()).toEqual({
      standalone: false,
      canPrompt: false,
      isIos: false,
    });
  });

  it("captures beforeinstallprompt and suppresses the browser infobar", async () => {
    const store = await freshStore();
    const listener = vi.fn();
    store.subscribeInstallPrompt(listener);
    const { event } = dispatchInstallPrompt("accepted");
    expect(event.defaultPrevented).toBe(true);
    expect(store.getInstallState().canPrompt).toBe(true);
    expect(listener).toHaveBeenCalled();
  });

  it("prompts once and reports the user choice", async () => {
    const store = await freshStore();
    store.initInstallPrompt();
    const { prompt } = dispatchInstallPrompt("dismissed");
    await expect(store.promptInstall()).resolves.toBe("dismissed");
    expect(prompt).toHaveBeenCalledTimes(1);
    // The deferred event is single-use; a later call reports unavailability.
    expect(store.getInstallState().canPrompt).toBe(false);
    await expect(store.promptInstall()).resolves.toBe("unavailable");
  });

  it("drops the captured prompt when the app is installed", async () => {
    const store = await freshStore();
    store.initInstallPrompt();
    dispatchInstallPrompt("accepted");
    window.dispatchEvent(new Event("appinstalled"));
    expect(store.getInstallState().canPrompt).toBe(false);
    await expect(store.promptInstall()).resolves.toBe("unavailable");
  });

  it("detects standalone display through the media query", async () => {
    stubStandaloneMedia(true);
    const store = await freshStore();
    expect(store.getInstallState().standalone).toBe(true);
  });

  it("detects iOS from the user agent so the manual flow can be offered", async () => {
    stubUserAgent("Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15");
    const store = await freshStore();
    expect(store.getInstallState().isIos).toBe(true);
  });
});
