import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vite-plus/test";

afterEach(() => {
  cleanup();
});

// jsdom implements no layout observers, and the drag-and-drop engine reaches
// for ResizeObserver as soon as it loads. Tests only need the collaborator to
// exist; the queue tests never measure sizes.
if (!("ResizeObserver" in globalThis)) {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
}
