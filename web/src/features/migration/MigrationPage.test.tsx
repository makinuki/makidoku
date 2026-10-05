import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { MigrationPage } from "./MigrationPage";

const source = {
  id: "asurascans",
  name: "Asura Scans",
  version: "1",
  abiVersion: 1,
  lang: "en",
  baseUrl: "https://asurascans.test",
  iconUrl: "",
  nsfw: false,
  installedAt: 1,
  loaded: true,
  hasClearance: false,
};

function matched(mangaId: string, title: string, replacement: string) {
  return {
    mangaId,
    title,
    status: "success",
    source,
    manga: {
      id: replacement,
      sourceId: "asurascans",
      title,
      status: "unknown",
      coverUrl: `/api/manga/${replacement}/cover`,
      inLibrary: false,
      downloadFormat: "cbz",
      createdAt: 1,
      updatedAt: 1,
    },
    score: 1,
    chapterCount: 4,
    latestChapter: 4,
  };
}

describe("MigrationPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The confirm dialog must report how many titles will move and how many are
  // skipped, and the skipped rows must not be applied.
  it("excludes skipped titles from migrate all", async () => {
    const sockets: Array<{
      url: string;
      onmessage: ((event: { data: string }) => void) | null;
    }> = [];
    class FakeSocket {
      onmessage: ((event: { data: string }) => void) | null = null;
      onclose: (() => void) | null = null;
      constructor(public url: string) {
        sockets.push(this);
      }
      close() {}
    }
    vi.stubGlobal("WebSocket", FakeSocket);
    const applied: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/migration/sources") {
          return Response.json([{ source, count: 2 }]);
        }
        if (path === "/api/migration/jobs" && init?.method === "POST") {
          return Response.json({ jobId: "job-1", count: 2 });
        }
        if (path.includes("/migration/apply") && init?.method === "POST") {
          applied.push(path);
          return Response.json({ manga: {}, source: "asurascans", chapterMap: {} });
        }
        return Response.json([]);
      }),
    );
    render(
      <MemoryRouter initialEntries={["/migration"]}>
        <MigrationPage />
      </MemoryRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /Asura Scans/ }));
    await waitFor(() => expect(sockets.length).toBe(1));
    act(() => {
      sockets[0].onmessage?.({
        data: JSON.stringify({
          type: "snapshot",
          jobId: "job-1",
          titles: [matched("lib1", "Alpha", "m1"), matched("lib2", "Beta", "m2")],
        }),
      });
    });
    await screen.findAllByText("Alpha");

    await user.click(screen.getAllByRole("button", { name: "Skip" })[0]);
    expect(await screen.findByText(/1 skipped/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Migrate all (1)" }));
    expect(screen.getByText(/Migrate 1 title/)).toBeInTheDocument();
    expect(screen.getByText(/1 skipped title will be left alone/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Migrate 1" }));

    await waitFor(() => expect(applied.length).toBe(1));
    expect(applied[0]).toContain("/lib2/");
  });

  // A row with no match can be pointed at a plugin and searched by hand; the
  // picked title becomes the row's match and is what Migrate applies.
  it("replaces a row match with a manual search result", async () => {
    const oldSource = { ...source, id: "old", name: "Old Plugin" };
    const sockets: Array<{
      url: string;
      onmessage: ((event: { data: string }) => void) | null;
    }> = [];
    class FakeSocket {
      onmessage: ((event: { data: string }) => void) | null = null;
      onclose: (() => void) | null = null;
      constructor(public url: string) {
        sockets.push(this);
      }
      close() {}
    }
    vi.stubGlobal("WebSocket", FakeSocket);
    const applied: Array<{ path: string; body: Record<string, unknown> }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/migration/sources") {
          return Response.json([{ source: oldSource, count: 1 }]);
        }
        if (path === "/api/sources") {
          return Response.json([oldSource, source]);
        }
        if (path === "/api/migration/jobs" && init?.method === "POST") {
          return Response.json({ jobId: "job-1", count: 1, sourceId: "old" });
        }
        if (path.startsWith("/api/sources/asurascans/search")) {
          return Response.json({
            page: 1,
            hasNextPage: false,
            items: [{ id: "m1", title: "Alpha Match", coverUrl: "/api/manga/m1/cover" }],
          });
        }
        if (path === "/api/manga/m1") {
          return Response.json({
            manga: {
              id: "m1",
              sourceId: "asurascans",
              title: "Alpha Match",
              status: "unknown",
              coverUrl: "/api/manga/m1/cover",
              inLibrary: false,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            categories: [],
            chapters: [
              { id: "c1", mangaId: "m1", chapterNumber: 1, downloaded: false },
              { id: "c2", mangaId: "m1", chapterNumber: 5, downloaded: false },
            ],
            trackers: [],
          });
        }
        if (path.includes("/migration/apply") && init?.method === "POST") {
          applied.push({ path, body: JSON.parse(String(init.body)) });
          return Response.json({ manga: {}, source: "asurascans", chapterMap: {} });
        }
        return Response.json([]);
      }),
    );
    render(
      <MemoryRouter initialEntries={["/migration"]}>
        <MigrationPage />
      </MemoryRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /Old Plugin/ }));
    await waitFor(() => expect(sockets.length).toBe(1));
    act(() => {
      sockets[0].onmessage?.({
        data: JSON.stringify({
          type: "snapshot",
          jobId: "job-1",
          titles: [{ mangaId: "lib1", title: "Alpha", status: "notFound" }],
        }),
      });
    });
    await screen.findByText("Alpha");

    await user.click(screen.getByRole("button", { name: "Search" }));
    await user.click(await screen.findByRole("button", { name: /Asura Scans/ }));
    const query = screen.getByPlaceholderText("Search title");
    await user.clear(query);
    await user.type(query, "Alpha{Enter}");
    await user.click(await screen.findByRole("button", { name: /Alpha Match/ }));

    expect(await screen.findByText("Manual match")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Migrate" }));

    await waitFor(() => expect(applied.length).toBe(1));
    expect(applied[0].path).toContain("/api/manga/lib1/migration/apply");
    expect(applied[0].body).toEqual({ sourceId: "asurascans", mangaId: "m1" });
  });
});
