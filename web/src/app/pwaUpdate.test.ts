import { afterEach, describe, expect, it, vi } from "vite-plus/test";

const mocks = vi.hoisted(() => ({
  registerSW: vi.fn(),
  updater: vi.fn(),
}));

vi.mock("virtual:pwa-register", () => ({ registerSW: mocks.registerSW }));

interface RegisterCallbacks {
  onNeedRefresh?: () => void;
  onOfflineReady?: () => void;
}

let callbacks: RegisterCallbacks = {};

type Store = typeof import("./pwaUpdate");

async function freshStore(): Promise<Store> {
  // The module keeps process-wide singleton state; every scenario starts
  // from a fresh copy with a clean registration mock.
  vi.resetModules();
  callbacks = {};
  mocks.updater.mockReset().mockResolvedValue(undefined);
  mocks.registerSW.mockReset().mockImplementation((options?: RegisterCallbacks) => {
    callbacks = options ?? {};
    return mocks.updater;
  });
  return import("./pwaUpdate");
}

function stubServiceWorker(): void {
  Object.defineProperty(window.navigator, "serviceWorker", { value: {}, configurable: true });
}

afterEach(() => {
  delete (window.navigator as { serviceWorker?: unknown }).serviceWorker;
});

describe("pwa update store", () => {
  it("stays inert when navigator.serviceWorker is unavailable", async () => {
    const store = await freshStore();
    store.initPwaUpdate();
    expect(mocks.registerSW).not.toHaveBeenCalled();
    expect(store.getPwaUpdateState()).toEqual({
      needRefresh: false,
      offlineReady: false,
      refreshEvents: 0,
    });
  });

  it("flags a waiting update and notifies subscribers on every event", async () => {
    stubServiceWorker();
    const store = await freshStore();
    const seen: number[] = [];
    store.subscribePwaUpdate(() => seen.push(store.getPwaUpdateState().refreshEvents));
    expect(mocks.registerSW).toHaveBeenCalledTimes(1);
    callbacks.onNeedRefresh?.();
    callbacks.onNeedRefresh?.();
    expect(store.getPwaUpdateState()).toEqual({
      needRefresh: true,
      offlineReady: false,
      refreshEvents: 2,
    });
    expect(seen).toEqual([1, 2]);
  });

  it("marks the app offline-ready", async () => {
    stubServiceWorker();
    const store = await freshStore();
    store.initPwaUpdate();
    callbacks.onOfflineReady?.();
    expect(store.getPwaUpdateState().offlineReady).toBe(true);
  });

  it("applies the update with a page reload", async () => {
    stubServiceWorker();
    const store = await freshStore();
    store.initPwaUpdate();
    await store.applyPwaUpdate();
    expect(mocks.updater).toHaveBeenCalledWith(true);
  });

  it("ignores apply calls when registration never ran", async () => {
    const store = await freshStore();
    await store.applyPwaUpdate();
    expect(mocks.updater).not.toHaveBeenCalled();
  });
});
