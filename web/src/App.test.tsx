import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BrowserRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vite-plus/test";
import App from "./App";

describe("MakiDoku app shell", () => {
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
    window.history.pushState({}, "", "/manga/mangadex/yosuga");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.includes("/api/library/mangadex/yosuga")) {
          return Response.json({
            manga: {
              id: "mangadex:yosuga",
              sourceId: "mangadex",
              sourceMangaId: "yosuga",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: "cover",
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
        if (path === "/api/trackers") return Response.json([]);
        if (path.includes("/migration/candidates")) return Response.json([]);
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
    expect(screen.getByRole("heading", { name: "Migrate source" })).toBeInTheDocument();
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
    window.history.pushState({}, "", "/reader/mangadex/yosuga/chapter-1");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.includes("/api/library/mangadex/yosuga")) {
          return Response.json({
            manga: {
              id: "mangadex:yosuga",
              sourceId: "mangadex",
              sourceMangaId: "yosuga",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: "cover",
              inLibrary: true,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            categories: [],
            chapters: [
              {
                id: "mangadex:yosuga:chapter-1",
                mangaId: "mangadex:yosuga",
                sourceChapterId: "chapter-1",
                chapterNumber: 1,
                downloaded: false,
              },
            ],
            trackers: [],
          });
        }
        if (path.includes("/pages")) {
          return Response.json([
            { index: 0, url: "https://example.test/1.jpg", isScrambled: false },
            { index: 1, url: "https://example.test/2.jpg", isScrambled: false },
          ]);
        }
        if (path === "/api/progress") return Response.json({});
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
    await user.type(screen.getByPlaceholderText("Search installed sources"), "Yosuga");
    await waitFor(() => {
      const requestPaths = fetchMock.mock.calls.map(([input]) => String(input));
      expect(requestPaths).toEqual(
        expect.arrayContaining([
          expect.stringContaining("filters=%7B%22status%22%3A%22completed%22%7D"),
        ]),
      );
    });
  });
});
