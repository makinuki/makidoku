import { act, fireEvent, render, screen } from "@testing-library/react";
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

function stubServiceWorker(): void {
  Object.defineProperty(window.navigator, "serviceWorker", { value: {}, configurable: true });
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

async function renderBanner(): Promise<void> {
  // Fresh module copies per scenario so the singleton stores do not leak
  // state between tests.
  vi.resetModules();
  callbacks = {};
  mocks.updater.mockReset().mockResolvedValue(undefined);
  mocks.registerSW.mockReset().mockImplementation((options?: RegisterCallbacks) => {
    callbacks = options ?? {};
    return mocks.updater;
  });
  const { UpdateBanner } = await import("./UpdateBanner");
  render(<UpdateBanner />);
}

afterEach(() => {
  delete (window.navigator as { serviceWorker?: unknown }).serviceWorker;
  delete (window as { matchMedia?: unknown }).matchMedia;
});

describe("UpdateBanner", () => {
  it("stays hidden while no update waits", async () => {
    stubServiceWorker();
    await renderBanner();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("offers a reload that applies the waiting update", async () => {
    stubServiceWorker();
    await renderBanner();
    act(() => {
      callbacks.onNeedRefresh?.();
    });
    expect(screen.getByRole("status")).toHaveTextContent("A new version of MakiDoku is ready.");
    fireEvent.click(screen.getByRole("button", { name: "Reload" }));
    expect(mocks.updater).toHaveBeenCalledWith(true);
  });

  it("hides on dismiss and reappears when a further update arrives", async () => {
    stubServiceWorker();
    await renderBanner();
    act(() => {
      callbacks.onNeedRefresh?.();
    });
    fireEvent.click(screen.getByRole("button", { name: "Dismiss notice" }));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    act(() => {
      callbacks.onNeedRefresh?.();
    });
    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("stays hidden in standalone display even with a waiting update", async () => {
    stubStandaloneMedia(true);
    stubServiceWorker();
    await renderBanner();
    act(() => {
      callbacks.onNeedRefresh?.();
    });
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
