import { act, render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BrowserRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vite-plus/test";
import App from "./App";

describe("MakiDoku app shell", () => {
  const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
  const chapterId = "0198c0de-7a22-7000-8000-00000000cafe";
  const pageOne = "0198c0de-7a33-7000-8000-000000000001";
  const pageTwo = "0198c0de-7a33-7000-8000-000000000002";

  it("renders the library workspace from the root route", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/health") return Response.json({ ok: true });
        const body = path.includes("/categories") ? [] : [];
        return new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByRole("heading", { name: "Library" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Your library is empty" })).toBeInTheDocument();
    expect(screen.getByLabelText("Daemon status")).toHaveTextContent("Connected");
    expect(screen.queryByText("Local user")).not.toBeInTheDocument();
  });

  it("opens tracking and migration workflows from manga details", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") return Response.json([]);
        if (path.includes("/migration/candidates")) return Response.json([]);
        if (path.includes(`/api/manga/${mangaId}`)) {
          return Response.json({
            manga: {
              id: mangaId,
              sourceId: "0198c0de-7a00-7000-8000-00000000abcd",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: "/api/manga/" + mangaId + "/cover",
              inLibrary: true,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            categories: [],
            chapters: [],
            trackers: [],
          });
        }
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Tracking" }));
    expect(screen.getByRole("heading", { name: "Tracking" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close" }));
    await user.click(screen.getByRole("button", { name: "Migrate" }));
    expect(screen.getByRole("heading", { name: "Migrate plugin" })).toBeInTheDocument();
  });

  it("labels chapter volumes and falls back when the cover fails to load", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") return Response.json([]);
        if (path.includes("/migration/candidates")) return Response.json([]);
        if (path.includes(`/api/manga/${mangaId}`)) {
          return Response.json({
            manga: {
              id: mangaId,
              sourceId: "0198c0de-7a00-7000-8000-00000000abcd",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: "/api/manga/" + mangaId + "/cover",
              inLibrary: true,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            categories: [],
            chapters: [
              {
                id: chapterId,
                mangaId,
                chapterNumber: 10.5,
                volume: 3,
                language: "en",
                downloaded: false,
              },
            ],
            trackers: [],
          });
        }
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("Vol. 3 · Chapter 10.5")).toBeInTheDocument();

    const cover = screen.getAllByAltText("")[0];
    fireEvent.error(cover);
    expect(await screen.findByLabelText("No cover available")).toBeInTheDocument();
  });

  it("uploads a selected backup from settings", async () => {
    window.history.pushState({}, "", "/settings");
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sources" || path === "/api/categories") return Response.json([]);
      if (path === "/api/import") return new Response(null, { status: 204 });
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    const input = await screen.findByLabelText("Import JSON");
    await user.upload(input, new File(["{}"], "backup.json", { type: "application/json" }));
    expect(await screen.findByText("Backup imported.")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/import",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("offers single, double, and webtoon reader modes", async () => {
    window.history.pushState({}, "", `/reader/${mangaId}/${chapterId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.includes(`/api/manga/${mangaId}`)) {
          return Response.json({
            manga: {
              id: mangaId,
              sourceId: "0198c0de-7a00-7000-8000-00000000abcd",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: "/api/manga/" + mangaId + "/cover",
              inLibrary: true,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            categories: [],
            chapters: [
              {
                id: chapterId,
                mangaId,
                chapterNumber: 1,
                downloaded: false,
              },
            ],
            trackers: [],
          });
        }
        if (path.includes(`/api/chapters/${chapterId}/pages`)) {
          return Response.json([
            { id: pageOne, chapterId, index: 0, isScrambled: false },
            { id: pageTwo, chapterId, index: 1, isScrambled: false },
          ]);
        }
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByRole("button", { name: "Single" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Double" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Webtoon" })).toBeInTheDocument();
  });

  it("renders source filters and includes selected values in search", async () => {
    window.history.pushState({}, "", "/browse");
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/health") return Response.json({ ok: true });
      if (path === "/api/sources") {
        return Response.json([
          {
            id: "mangadex",
            name: "MangaDex",
            version: "1",
            abiVersion: 1,
            lang: "en",
            baseUrl: "https://mangadex.org",
            iconUrl: "",
            nsfw: false,
            installedAt: 1,
            loaded: true,
            hasClearance: false,
          },
        ]);
      }
      if (path === "/api/sources/mangadex/filters") {
        return Response.json([
          {
            id: "status",
            title: "Status",
            type: "select",
            options: [
              { label: "All", value: "" },
              { label: "Completed", value: "completed" },
            ],
            default: "",
          },
        ]);
      }
      if (path.startsWith("/api/sources/mangadex/search"))
        return Response.json({ page: 1, hasNextPage: false, items: [] });
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /MangaDex/ }));
    const status = await screen.findByRole("combobox", { name: "Status" });
    await user.selectOptions(status, "completed");
    await user.type(screen.getByPlaceholderText("Search installed plugins"), "Yosuga");
    await waitFor(() => {
      const requestPaths = fetchMock.mock.calls.map(([input]) => String(input));
      expect(requestPaths).toEqual(
        expect.arrayContaining([
          expect.stringContaining("filters=%7B%22status%22%3A%22completed%22%7D"),
        ]),
      );
    });
  });

  it("prompts to install plugins when browse has none installed", async () => {
    window.history.pushState({}, "", "/browse");
    let resolveSources!: (value: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/sources") {
          return new Promise<Response>((resolve) => {
            resolveSources = resolve;
          });
        }
        if (path === "/api/health") return Promise.resolve(Response.json({ ok: true }));
        return Promise.resolve(Response.json([]));
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(screen.getByRole("heading", { name: "No plugins installed" })).toBeInTheDocument();
    const link = screen.getByRole("link", { name: "Install plugins" });
    expect(link).toHaveAttribute("href", "/settings");
    await act(async () => {
      resolveSources(Response.json([]));
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
  });
});
