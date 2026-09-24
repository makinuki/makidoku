import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

const originalUserAgent = window.navigator.userAgent;

async function renderRow(): Promise<void> {
  // Fresh module copies per scenario so the singleton store does not leak
  // state between tests.
  vi.resetModules();
  const { InstallAppRow } = await import("./InstallAppRow");
  render(<InstallAppRow />);
}

function dispatchInstallPrompt() {
  const prompt = vi.fn().mockResolvedValue(undefined);
  const event = new Event("beforeinstallprompt", { cancelable: true });
  Object.assign(event, { prompt, userChoice: Promise.resolve({ outcome: "accepted" }) });
  act(() => {
    window.dispatchEvent(event);
  });
  return prompt;
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

afterEach(() => {
  Object.defineProperty(window.navigator, "userAgent", {
    value: originalUserAgent,
    configurable: true,
  });
  delete (window as { matchMedia?: unknown }).matchMedia;
});

describe("InstallAppRow", () => {
  it("stays hidden when the platform can neither prompt nor instruct", async () => {
    await renderRow();
    expect(screen.queryByRole("button", { name: /install app/i })).not.toBeInTheDocument();
  });

  it("appears once the browser offers a prompt and triggers it on click", async () => {
    await renderRow();
    const prompt = dispatchInstallPrompt();
    const row = screen.getByRole("button", { name: /install app/i });
    fireEvent.click(row);
    expect(prompt).toHaveBeenCalledTimes(1);
  });

  it("stays hidden in standalone display even with a captured prompt", async () => {
    stubStandaloneMedia(true);
    await renderRow();
    dispatchInstallPrompt();
    expect(screen.queryByRole("button", { name: /install app/i })).not.toBeInTheDocument();
  });

  it("offers manual instructions on iOS where no prompt event exists", async () => {
    stubUserAgent("Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15");
    await renderRow();
    fireEvent.click(screen.getByRole("button", { name: /install app/i }));
    const dialog = screen.getByRole("dialog", { name: "Install MakiDoku" });
    expect(dialog).toHaveTextContent("Add to Home Screen");
  });
});
