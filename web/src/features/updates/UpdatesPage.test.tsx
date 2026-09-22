import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { UpdatesPage } from "./UpdatesPage";

const now = Math.floor(Date.now() / 1000);

function log(id: string, mangaId: string, title: string, chapterNumber: number, seenAt = now) {
  return {
    id,
    seenAt,
    acknowledged: false,
    manga: {
      id: mangaId,
      sourceId: "source",
      title,
      status: "ongoing",
      coverUrl: "",
      inLibrary: true,
      downloadFormat: "cbz",
      createdAt: 1,
      updatedAt: 1,
    },
    chapter: { id: `chapter-${id}`, mangaId, chapterNumber, downloaded: false, bookmark: false },
  };
}

function stub(calls: string[], updates: unknown[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      calls.push(`${init?.method ?? "GET"} ${path}`);
      if (path === "/api/updates") return Response.json(updates);
      if (path === "/api/updates/state") return Response.json({ lastRunAt: now, lastStatus: "ok" });
      if (path === "/api/settings") return Response.json([]);
      return Response.json({});
    }),
  );
}

describe("UpdatesPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("groups chapters per manga with expandable leaders", async () => {
    const calls: string[] = [];
    stub(calls, [
      log("u1", "m1", "Alpha", 10),
      log("u2", "m1", "Alpha", 11),
      log("u3", "m2", "Beta", 3),
    ]);
    render(
      <MemoryRouter initialEntries={["/updates"]}>
        <UpdatesPage />
      </MemoryRouter>,
    );
    expect(await screen.findByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("2 chapters")).toBeInTheDocument();
    // A multi-chapter group starts collapsed; the second chapter is hidden.
    expect(screen.queryByText("Chapter 11")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand updates for Alpha" }));
    expect(screen.getByText("Chapter 11")).toBeInTheDocument();
    // Single-chapter groups render their row directly.
    expect(screen.getByText("Chapter 3")).toBeInTheDocument();
  });

  it("queues a chapter download from its row", async () => {
    const calls: string[] = [];
    stub(calls, [log("u1", "m1", "Alpha", 10)]);
    render(
      <MemoryRouter initialEntries={["/updates"]}>
        <UpdatesPage />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Download Chapter 10" }));
    await waitFor(() => expect(calls).toContain("POST /api/download"));
    expect(await screen.findByLabelText("Chapter 10 saved")).toBeInTheDocument();
  });

  it("toggles the bookmark from its row", async () => {
    const calls: string[] = [];
    stub(calls, [log("u1", "m1", "Alpha", 10)]);
    render(
      <MemoryRouter initialEntries={["/updates"]}>
        <UpdatesPage />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Bookmark Chapter 10" }));
    await waitFor(() =>
      expect(calls).toContain("POST /api/chapters/chapter-u1/bookmark"),
    );
    expect(
      await screen.findByRole("button", { name: "Remove bookmark from Chapter 10" }),
    ).toBeInTheDocument();
  });

  it("acknowledges selected updates in bulk", async () => {
    const calls: string[] = [];
    stub(calls, [
      log("u1", "m1", "Alpha", 10),
      log("u2", "m2", "Beta", 3),
    ]);
    render(
      <MemoryRouter initialEntries={["/updates"]}>
        <UpdatesPage />
      </MemoryRouter>,
    );
    await screen.findByText("Alpha");
    fireEvent.click(screen.getByRole("button", { name: "Select" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select Chapter 10" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select Chapter 3" }));
    expect(screen.getByText("2 selected")).toBeInTheDocument();
    // Row-level buttons share the bulk bar label; the bulk action renders last.
    const actions = screen.getAllByRole("button", { name: "Mark read" });
    fireEvent.click(actions[actions.length - 1]);
    await waitFor(() => expect(calls).toContain("POST /api/updates/ack"));
    expect(await screen.findByRole("heading", { name: "No new chapters" })).toBeInTheDocument();
  });
});
