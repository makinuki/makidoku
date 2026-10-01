import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { QueueItem } from "../../types";
import { DownloadsPage } from "./DownloadsPage";

type Call = { method: string; path: string; body: unknown };

function row(overrides: Partial<QueueItem> & { id: number }): QueueItem {
  return {
    chapterId: `chapter-${overrides.id}`,
    status: "PENDING",
    progress: 0,
    totalPages: 10,
    downloadedPages: 0,
    mangaId: "m1",
    mangaTitle: "Yosuga no Sora",
    sourceId: "s1",
    sourceName: "MangaDex",
    chapterNumber: overrides.id,
    position: overrides.id,
    ...overrides,
  };
}

const stats = { downloadedPages: 0, retriedRequests: 0, throttledRequests: 0 };

type SocketStub = {
  onopen: (() => void) | null;
  onmessage: ((event: { data: string }) => void) | null;
  onclose: (() => void) | null;
  close: () => void;
};

// The page follows downloader state through WebSocket frames, so the stub keeps
// the sockets it hands out and lets a test deliver a frame.
let sockets: SocketStub[] = [];

function emitDownloadEvent(message: unknown) {
  const frame = { data: JSON.stringify(message) };
  for (const socket of sockets) socket.onmessage?.(frame);
}

// The page talks to the queue endpoints through fetch; the stub answers the
// snapshot routes from its own list so mutations are visible to a refetch.
function stubQueue(items: QueueItem[], paused = false, pausedSources: string[] = []) {
  const calls: Call[] = [];
  let current = items;
  let downloaderPaused = paused;
  let sourcePauses = [...pausedSources];
  sockets = [];
  vi.stubGlobal(
    "WebSocket",
    class {
      onopen: (() => void) | null = null;
      onmessage: ((event: { data: string }) => void) | null = null;
      onclose: (() => void) | null = null;
      constructor() {
        sockets.push(this);
      }
      close() {}
    },
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, path, body });
      const snapshot = () =>
        Response.json({ items: current, stats, paused: downloaderPaused, pausedSources: sourcePauses });
      if (path === "/api/download" && method === "GET") return snapshot();
      if (path === "/api/download/pause-all") {
        downloaderPaused = true;
        // The server releases in-flight rows to queued; the stub mirrors it.
        current = current.map((item) =>
          item.status === "DOWNLOADING" ? { ...item, status: "PENDING" } : item,
        );
        return snapshot();
      }
      if (path === "/api/download/resume-all") {
        downloaderPaused = false;
        return snapshot();
      }
      const sourceControl = /^\/api\/download\/sources\/([^/]+)\/(pause|resume)$/.exec(path);
      if (sourceControl) {
        const sourceId = decodeURIComponent(sourceControl[1]);
        if (sourceControl[2] === "pause") {
          sourcePauses = [...sourcePauses, sourceId];
          current = current.map((item) =>
            item.sourceId === sourceId && item.status === "DOWNLOADING"
              ? { ...item, status: "PENDING" }
              : item,
          );
        } else {
          sourcePauses = sourcePauses.filter((id) => id !== sourceId);
        }
        return snapshot();
      }
      if (path === "/api/download/cancel-all") {
        current = [];
        return snapshot();
      }
      if (path === "/api/download/cancel") {
        const ids = (body as { itemIds: number[] }).itemIds;
        current = current.filter((item) => !ids.includes(item.id));
        return snapshot();
      }
      if (path === "/api/download/reorder") {
        const ids = (body as { itemIds: number[] }).itemIds;
        current = ids
          .map((id) => current.find((item) => item.id === id))
          .filter((item): item is QueueItem => item !== undefined);
        return new Response(null, { status: 204 });
      }
      const control = /^\/api\/download\/(\d+)\/(retry|cancel)$/.exec(path);
      if (control) {
        const id = Number(control[1]);
        current =
          control[2] === "retry"
            ? current.map((item) => (item.id === id ? { ...item, status: "PENDING" } : item))
            : current.filter((item) => item.id !== id);
        return new Response(null, { status: 204 });
      }
      return Response.json({});
    }),
  );
  return calls;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/downloads"]}>
      <DownloadsPage />
    </MemoryRouter>,
  );
}

function sectionNames(): Array<string | null> {
  return screen
    .getAllByRole("button", { name: /^\w.* \(\d+\)$/ })
    .map((button) => button.getAttribute("aria-label")?.replace(/ \(\d+\)$/, "") ?? null);
}

function openRowMenu(chapter: string, title = "Yosuga no Sora") {
  fireEvent.click(screen.getByRole("button", { name: `Actions for ${chapter} of ${title}` }));
}

function reorderBody(calls: Call[]) {
  return calls.find((call) => call.path === "/api/download/reorder")?.body;
}

describe("DownloadsPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("groups the queue by source and hides terminal rows", async () => {
    stubQueue([
      row({ id: 1 }),
      row({ id: 2 }),
      row({ id: 3, status: "COMPLETED" }),
      row({
        id: 4,
        mangaId: "m2",
        mangaTitle: "Cheolsu Saves the World",
        sourceId: "s2",
        sourceName: "Asura Scans",
      }),
    ]);
    renderPage();
    expect(await screen.findAllByText("Yosuga no Sora")).toHaveLength(2);
    expect(sectionNames()).toEqual(["MangaDex", "Asura Scans"]);
    expect(screen.getByText("(2)")).toBeInTheDocument();
    expect(screen.getByText("(1)")).toBeInTheDocument();
    // A terminal row is not part of the queue; the count pill follows the
    // live rows.
    expect(screen.queryByText("Chapter 3")).not.toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("collapses and expands a source section", async () => {
    stubQueue([row({ id: 1 })]);
    renderPage();
    await screen.findByText("Chapter 1");
    fireEvent.click(screen.getByRole("button", { name: "MangaDex (1)" }));
    expect(screen.getByRole("button", { name: "MangaDex (1)" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(screen.queryByText("Chapter 1")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "MangaDex (1)" }));
    expect(await screen.findByText("Chapter 1")).toBeInTheDocument();
  });

  it("shows the row status, the page count, and the failure reason", async () => {
    stubQueue([
      row({ id: 1, status: "FAILED", errorMessage: "connection reset" }),
      row({ id: 2, status: "DOWNLOADING", progress: 40, downloadedPages: 4, totalPages: 10 }),
    ]);
    renderPage();
    expect(await screen.findByText("connection reset")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("Downloading")).toBeInTheDocument();
    expect(screen.getByText("4/10")).toBeInTheDocument();
  });

  it("moves a row to the top of its source and persists the order", async () => {
    const calls = stubQueue([row({ id: 1 }), row({ id: 2 }), row({ id: 3 })]);
    renderPage();
    await screen.findByText("Chapter 1");
    await waitFor(() => expect(sectionNames()).toEqual(["MangaDex"]));
    openRowMenu("Chapter 2");
    fireEvent.click(screen.getByRole("menuitem", { name: "Move to top" }));
    await waitFor(() => expect(reorderBody(calls)).toEqual({ itemIds: [2, 1, 3] }));
  });

  it("hides the moves that would do nothing", async () => {
    stubQueue([row({ id: 1 }), row({ id: 2 })]);
    renderPage();
    await screen.findByText("Chapter 1");
    openRowMenu("Chapter 1");
    expect(screen.queryByRole("menuitem", { name: "Move to top" })).not.toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Move to bottom" })).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "Escape" });
    openRowMenu("Chapter 2");
    expect(screen.getByRole("menuitem", { name: "Move to top" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Move to bottom" })).not.toBeInTheDocument();
  });

  it("moves a whole series to the top, raising its source", async () => {
    const calls = stubQueue([
      row({ id: 1 }),
      row({ id: 2 }),
      row({
        id: 3,
        mangaId: "m2",
        mangaTitle: "Cheolsu Saves the World",
        sourceId: "s2",
        sourceName: "Asura Scans",
        chapterNumber: 1,
      }),
    ]);
    renderPage();
    await screen.findByText("Cheolsu Saves the World");
    expect(sectionNames()).toEqual(["MangaDex", "Asura Scans"]);
    openRowMenu("Chapter 1", "Cheolsu Saves the World");
    fireEvent.click(screen.getByRole("menuitem", { name: "Move series to top" }));
    await waitFor(() => expect(reorderBody(calls)).toEqual({ itemIds: [3, 1, 2] }));
    expect(sectionNames()).toEqual(["Asura Scans", "MangaDex"]);
  });

  it("cancels a single row from its menu", async () => {
    const calls = stubQueue([row({ id: 1 }), row({ id: 2 })]);
    renderPage();
    await screen.findByText("Chapter 1");
    openRowMenu("Chapter 1");
    fireEvent.click(screen.getByRole("menuitem", { name: "Cancel" }));
    await waitFor(() =>
      expect(calls.some((call) => call.path === "/api/download/1/cancel")).toBe(true),
    );
    await waitFor(() => expect(screen.queryByText("Chapter 1")).not.toBeInTheDocument());
  });

  it("cancels every queued chapter of the series", async () => {
    const calls = stubQueue([row({ id: 1 }), row({ id: 2 })]);
    renderPage();
    await screen.findByText("Chapter 1");
    openRowMenu("Chapter 1");
    fireEvent.click(screen.getByRole("menuitem", { name: "Cancel all for this series" }));
    await waitFor(() =>
      expect(calls.find((call) => call.path === "/api/download/cancel")?.body).toEqual({
        itemIds: [1, 2],
      }),
    );
    expect(await screen.findByRole("heading", { name: "No downloads" })).toBeInTheDocument();
  });

  it("retries a failed row", async () => {
    const calls = stubQueue([row({ id: 5, status: "FAILED", errorMessage: "connection reset" })]);
    renderPage();
    await screen.findByText("connection reset");
    openRowMenu("Chapter 5");
    fireEvent.click(screen.getByRole("menuitem", { name: "Retry" }));
    await waitFor(() =>
      expect(calls.some((call) => call.path === "/api/download/5/retry")).toBe(true),
    );
    expect(await screen.findByText("Queued")).toBeInTheDocument();
  });

  it("sorts each source and persists the new order", async () => {
    const calls = stubQueue([
      row({ id: 1, uploadedAt: 300 }),
      row({ id: 2, uploadedAt: 100 }),
      row({ id: 3, uploadedAt: 200 }),
    ]);
    renderPage();
    await screen.findByText("Chapter 1");
    fireEvent.click(screen.getByRole("button", { name: "Sort queue" }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Oldest" }));
    await waitFor(() => expect(reorderBody(calls)).toEqual({ itemIds: [2, 3, 1] }));
    fireEvent.click(screen.getByRole("button", { name: "Sort queue" }));
    expect(screen.getByRole("menuitemradio", { name: "Oldest" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("cancels every row from the page menu", async () => {
    const calls = stubQueue([row({ id: 1 }), row({ id: 2, status: "FAILED" })]);
    renderPage();
    await screen.findByText("Chapter 1");
    fireEvent.click(screen.getByRole("button", { name: "Queue actions" }));
    expect(screen.getAllByRole("menuitem").map((item) => item.textContent)).toEqual(["Cancel all"]);
    fireEvent.click(screen.getByRole("menuitem", { name: "Cancel all" }));
    await waitFor(() =>
      expect(calls.some((call) => call.path === "/api/download/cancel-all")).toBe(true),
    );
    await screen.findByRole("heading", { name: "No downloads" });
  });

  it("pauses and resumes the downloader from the queue control", async () => {
    const calls = stubQueue([row({ id: 1 })]);
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Pause downloads" }));
    await waitFor(() =>
      expect(calls.some((call) => call.path === "/api/download/pause-all")).toBe(true),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Resume downloads" }));
    await waitFor(() =>
      expect(calls.some((call) => call.path === "/api/download/resume-all")).toBe(true),
    );
    expect(await screen.findByRole("button", { name: "Pause downloads" })).toBeInTheDocument();
  });

  it("follows a downloader state event that carries no item", async () => {
    const calls = stubQueue([row({ id: 1 })]);
    renderPage();
    await screen.findByRole("button", { name: "Pause downloads" });
    const before = calls.length;
    await act(async () => {
      emitDownloadEvent({ type: "state", stats, paused: true });
    });
    expect(await screen.findByRole("button", { name: "Resume downloads" })).toBeInTheDocument();
    expect(calls.length).toBe(before);
  });

  it("pauses and resumes one source from its group header", async () => {
    const calls = stubQueue([row({ id: 1 })]);
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Pause MangaDex" }));
    await waitFor(() => {
      expect(
        calls.some((call) => call.method === "POST" && call.path === "/api/download/sources/s1/pause"),
      ).toBe(true);
    });
    expect(await screen.findByRole("button", { name: "Resume MangaDex" })).toBeInTheDocument();
    expect(await screen.findByText("Paused")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Resume MangaDex" }));
    await waitFor(() => {
      expect(
        calls.some((call) => call.method === "POST" && call.path === "/api/download/sources/s1/resume"),
      ).toBe(true);
    });
    expect(await screen.findByRole("button", { name: "Pause MangaDex" })).toBeInTheDocument();
  });

  it("disables the failed retry action while the source is paused", async () => {
    stubQueue([row({ id: 1, status: "FAILED" })], false, ["s1"]);
    renderPage();
    const retry = await screen.findByRole("button", { name: "Retry 1 failed downloads from MangaDex" });
    expect(retry).toBeDisabled();
  });

  it("shows an in-flight row as queued right after a pause", async () => {
    stubQueue([row({ id: 1, status: "DOWNLOADING" })]);
    renderPage();
    expect(await screen.findByText("Downloading")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Pause MangaDex" }));
    await waitFor(() => {
      expect(screen.getByText("Queued")).toBeInTheDocument();
    });
    expect(screen.queryByText("Downloading")).not.toBeInTheDocument();
  });

  it("marks every source as paused and locks the toggles while the downloader is paused", async () => {
    stubQueue([row({ id: 1 })], true);
    renderPage();
    // The header chips the pause and the toggle steps aside until the
    // downloader-wide pause is lifted.
    expect(await screen.findByText("Paused")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Resume MangaDex" })).toBeDisabled();
  });

  it("links a row to its title", async () => {
    stubQueue([row({ id: 1 })]);
    renderPage();
    await screen.findByText("Chapter 1");
    openRowMenu("Chapter 1");
    expect(screen.getByRole("menuitem", { name: "Show title" })).toHaveAttribute(
      "href",
      "/manga/m1",
    );
  });

  it("shows the empty state without the queue control", async () => {
    stubQueue([]);
    renderPage();
    expect(await screen.findByRole("heading", { name: "No downloads" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Pause downloads" })).not.toBeInTheDocument();
  });
});
