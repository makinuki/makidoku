import { act, render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BrowserRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import App from "./App";
import { resumeIndex } from "./features/reader/ReaderPage";

// A socket that never opens. Components subscribing to daemon events must not
// drag real connection attempts and reconnect timers into the test run; tests
// that need events install their own capturing stub.
class InertSocket {
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  constructor(public url: string) {}
  close() {}
  send() {}
}
vi.stubGlobal("WebSocket", InertSocket);

describe("MakiDoku app shell", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.stubGlobal("WebSocket", InertSocket);
  });
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
    expect(
      await screen.findByRole("heading", { name: "Your library is empty" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Daemon status")).toHaveTextContent("Connected");
    expect(screen.queryByText("Local user")).not.toBeInTheDocument();
  });

  it("opens tracking and migration workflows from manga details", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "anilist",
              capabilities: {
                search: true,
                status: true,
                scrobble: true,
                oauth: true,
                token: true,
              },
              credential: true,
              authType: "oauth",
              configured: true,
              connectedAs: "example-reader",
            },
            {
              name: "kitsu",
              capabilities: {
                search: true,
                status: false,
                scrobble: false,
                oauth: false,
                token: true,
              },
              credential: false,
              authType: "password",
              configured: true,
            },
            {
              name: "mangabaka",
              capabilities: {
                search: true,
                status: true,
                scrobble: true,
                oauth: true,
                token: true,
              },
              credential: true,
              authType: "oauth",
              configured: true,
              connectedAs: "example-reader",
            },
            {
              name: "mangaupdates",
              capabilities: {
                search: true,
                status: false,
                scrobble: false,
                oauth: false,
                token: true,
              },
              credential: false,
              authType: "password",
              configured: true,
            },
            {
              name: "myanimelist",
              capabilities: {
                search: true,
                status: true,
                scrobble: true,
                oauth: true,
                token: true,
              },
              credential: true,
              authType: "oauth",
              configured: true,
              connectedAs: "example-reader",
            },
          ]);
        }
        if (path.includes("/migration/candidates")) {
          return Response.json({ candidates: [], failedSources: 0, searched: 0 });
        }
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
            sourceName: "MangaDex",
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
    expect(await screen.findByText("MangaDex · completed")).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Tracking" }));
    expect(screen.getByRole("heading", { name: "Tracking" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "AniList" })).toBeInTheDocument();
    expect(screen.getAllByText("Connected as example-reader").length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByRole("heading", { name: "Kitsu" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close" }));
    await user.click(screen.getByRole("button", { name: "Migrate" }));
    expect(screen.getByRole("heading", { name: "Migrate plugin" })).toBeInTheDocument();
  });

  it("opens the tracking modal when the aggregate carries null collections", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") return Response.json([]);
        if (path.includes("/migration/candidates")) {
          return Response.json({ candidates: [], failedSources: 0, searched: 0 });
        }
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
            trackers: null,
            sourceName: "MangaDex",
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
    expect(await screen.findByText("MangaDex · completed")).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Tracking" }));
    expect(screen.getByRole("heading", { name: "Tracking" })).toBeInTheDocument();
  });

  it("labels chapter volumes and falls back when the cover fails to load", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") return Response.json([]);
        if (path.includes("/migration/candidates")) {
          return Response.json({ candidates: [], failedSources: 0, searched: 0 });
        }
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
              {
                id: pageOne,
                mangaId,
                chapterNumber: 2,
                language: "en",
                downloaded: false,
              },
              {
                id: pageTwo,
                mangaId,
                chapterNumber: 1,
                language: "ja",
                downloaded: false,
              },
            ],
            trackers: [],
            sourceName: "MangaDex",
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
    expect(screen.getByRole("heading", { name: "Volume 3" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "No volume" })).toBeInTheDocument();
    const noVolumeSection = screen.getByRole("heading", { name: "No volume" }).nextElementSibling;
    expect(noVolumeSection).toHaveTextContent("Chapter 2");
    expect(noVolumeSection).toHaveTextContent("Chapter 1");
    expect(noVolumeSection?.textContent?.indexOf("Chapter 2")).toBeLessThan(
      Number(noVolumeSection?.textContent?.indexOf("Chapter 1")),
    );
    expect(screen.getByRole("button", { name: "English" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Japanese" })).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Japanese" }));
    expect(screen.queryByText("Vol. 3 · Chapter 10.5")).not.toBeInTheDocument();
    expect(screen.getByText("Chapter 1")).toBeInTheDocument();
    expect(screen.queryByText("Chapter 2")).not.toBeInTheDocument();

    const cover = screen.getAllByAltText("")[0];
    fireEvent.error(cover);
    expect(await screen.findByLabelText("No cover available")).toBeInTheDocument();
  });

  it("refreshes details from the title actions and keeps stale content on failure", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    const aggregate = {
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
        detailsFetchedAt: 1700000000,
      },
      categories: [],
      chapters: [],
      trackers: [],
      sourceName: "MangaDex",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/trackers") return Response.json([]);
        if (path.includes("/migration/candidates")) {
          return Response.json({ candidates: [], failedSources: 0, searched: 0 });
        }
        if (path.includes(`/api/manga/${mangaId}/refresh`)) {
          if (init?.method === "POST") {
            return Response.json({
              ...aggregate,
              manga: { ...aggregate.manga, status: "hiatus" },
            });
          }
        }
        if (path.includes(`/api/manga/${mangaId}`)) return Response.json(aggregate);
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("MangaDex · completed")).toBeInTheDocument();
    expect(screen.getByText(/Updated /)).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Refresh details" }));
    expect(await screen.findByText("MangaDex · hiatus")).toBeInTheDocument();
  });

  it("shows install progress and surfaces the outcome", async () => {
    window.history.pushState({}, "", "/browse?tab=plugins");
    const source = {
      id: "mangadex",
      name: "MangaDex",
      version: "1.1.1",
      abiVersion: 1,
      lang: "en",
      baseUrl: "https://mangadex.org",
      iconUrl: "",
      nsfw: false,
      installedAt: 1,
      loaded: true,
      hasClearance: false,
    };
    let installed = false;
    let resolveInstall!: (value: Response) => void;
    const installGate = new Promise<Response>((resolve) => {
      resolveInstall = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/sources") return Response.json(installed ? [source] : []);
        if (path === "/api/sources/catalog") {
          return Response.json(
            installed ? [] : [{ ...source, installed: false, compatible: true }],
          );
        }
        if (path === "/api/sources/install" && init?.method === "POST") {
          const response = await installGate;
          installed = true;
          return response;
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
    const installButton = await screen.findByRole("button", { name: "Install" });
    await user.click(installButton);
    expect(screen.getByRole("button", { name: /Installing/ })).toBeDisabled();
    resolveInstall(Response.json(source));
    expect(await screen.findByText("MangaDex installed.")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /Uninstall MangaDex/ })).toBeInTheDocument();
  });

  it("installs multiple plugins simultaneously", async () => {
    window.history.pushState({}, "", "/browse?tab=plugins");
    const plugins = [
      { id: "mangadex", name: "MangaDex" },
      { id: "asurascans", name: "Asura Scans" },
    ] as const;
    const source = (plugin: (typeof plugins)[number]) => ({
      id: plugin.id,
      name: plugin.name,
      version: "1.1.1",
      abiVersion: 1,
      lang: "en",
      baseUrl: "https://example.test",
      iconUrl: "",
      nsfw: false,
      installedAt: 1,
      loaded: true,
      hasClearance: false,
    });
    const gates = new Map<string, (value: Response) => void>();
    const installed = new Set<string>();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/sources") {
          return Response.json(plugins.filter((plugin) => installed.has(plugin.id)).map(source));
        }
        if (path === "/api/sources/catalog") {
          return Response.json(
            plugins
              .filter((plugin) => !installed.has(plugin.id))
              .map((plugin) => ({ ...source(plugin), installed: false, compatible: true })),
          );
        }
        if (path === "/api/sources/install" && init?.method === "POST") {
          const id = String(JSON.parse(String(init.body)).id);
          const response = await new Promise<Response>((resolve) => {
            gates.set(id, resolve);
          });
          installed.add(id);
          return response;
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
    const installButtons = await screen.findAllByRole("button", { name: "Install" });
    expect(installButtons).toHaveLength(2);
    await user.click(installButtons[0]);
    await user.click(screen.getByRole("button", { name: "Install" }));
    expect(screen.getAllByRole("button", { name: /Installing/ })).toHaveLength(2);
    gates.get("mangadex")!(Response.json(source(plugins[0])));
    expect(await screen.findByText("MangaDex installed.")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Installing/ })).toHaveLength(1);
    gates.get("asurascans")!(Response.json(source(plugins[1])));
    expect(await screen.findByText("Asura Scans installed.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Installing/ })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Uninstall/ })).toHaveLength(2);
  });

  it("clears status messages after a timeout", async () => {
    vi.useFakeTimers();
    window.history.pushState({}, "", "/browse?tab=plugins");
    const source = {
      id: "mangadex",
      name: "MangaDex",
      version: "1.1.1",
      abiVersion: 1,
      lang: "en",
      baseUrl: "https://mangadex.org",
      iconUrl: "",
      nsfw: false,
      installedAt: 1,
      loaded: true,
      hasClearance: false,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/sources") return Response.json([]);
        if (path === "/api/sources/catalog") {
          return Response.json([{ ...source, installed: false, compatible: true }]);
        }
        if (path === "/api/sources/install" && init?.method === "POST") {
          return Response.json(source);
        }
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const flush = async () => {
      for (let tick = 0; tick < 5; tick++) await act(async () => {});
    };
    await flush();
    fireEvent.click(screen.getByRole("button", { name: "Install" }));
    await flush();
    expect(screen.getByText("MangaDex installed.")).toBeInTheDocument();
    await act(async () => {
      vi.advanceTimersByTime(4000);
    });
    expect(screen.queryByText("MangaDex installed.")).not.toBeInTheDocument();
  });

  it("saves search results in place and links the card to details", async () => {
    window.history.pushState({}, "", "/browse");
    const resultId = "0198c0de-7a33-7000-8000-00000000abcd";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
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
        if (path.startsWith("/api/sources/mangadex/search")) {
          return Response.json({
            page: 1,
            hasNextPage: false,
            items: [
              {
                id: resultId,
                sourceId: "mangadex",
                title: "Yosuga no Sora",
                coverUrl: "https://example.test/cover.jpg",
              },
            ],
          });
        }
        if (path === `/api/manga/${resultId}/library` && init?.method === "POST") {
          return Response.json({
            manga: { id: resultId, inLibrary: true },
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
    await user.type(await screen.findByPlaceholderText("Search installed plugins"), "Yosuga");
    await user.keyboard("{Enter}");
    const save = await screen.findByRole("button", { name: /Save title/ });
    await user.click(save);
    expect(await screen.findByRole("button", { name: /Added/ })).toBeDisabled();
    expect(screen.getByRole("link", { name: /Yosuga no Sora/ })).toHaveAttribute(
      "href",
      `/manga/${resultId}`,
    );
    expect(window.location.pathname).toBe("/browse");
  });

  it("submits source searches only when Enter is pressed", async () => {
    window.history.pushState({}, "", "/browse");
    let searchCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
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
        if (path.startsWith("/api/sources/mangadex/search")) {
          searchCalls++;
          return Response.json({ page: 1, hasNextPage: false, items: [] });
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
    const input = await screen.findByPlaceholderText("Search installed plugins");
    await user.type(input, "Yosuga");
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 350));
    });
    expect(searchCalls).toBe(0);

    await user.keyboard("{Enter}");
    await waitFor(() => expect(searchCalls).toBe(1));
  });

  it("reports failed plugin installs in the error banner", async () => {
    window.history.pushState({}, "", "/browse?tab=plugins");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/sources") return Response.json([]);
        if (path === "/api/sources/catalog") {
          return Response.json([
            {
              id: "mangadex",
              name: "MangaDex",
              version: "1.1.1",
              abiVersion: 1,
              lang: "en",
              baseUrl: "https://mangadex.org",
              iconUrl: "",
              nsfw: false,
              installed: false,
              compatible: true,
            },
          ]);
        }
        if (path === "/api/sources/install" && init?.method === "POST") {
          return Response.json(
            { error: { code: "REGISTRY_UNREACHABLE", message: "registry unreachable" } },
            { status: 502 },
          );
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
    await user.click(await screen.findByRole("button", { name: "Install" }));
    expect(await screen.findByText("registry unreachable")).toBeInTheDocument();
  });

  it("confirms plugin removal before uninstalling", async () => {
    window.history.pushState({}, "", "/browse?tab=plugins");
    const source = {
      id: "mangadex",
      name: "MangaDex",
      version: "1.1.1",
      abiVersion: 1,
      lang: "en",
      baseUrl: "https://mangadex.org",
      iconUrl: "",
      nsfw: false,
      installedAt: 1,
      loaded: true,
      hasClearance: false,
    };
    let removed = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/api/sources") return Response.json(removed ? [] : [source]);
      if (path === "/api/sources/mangadex/" && init?.method === "DELETE") {
        removed = true;
        return new Response(null, { status: 204 });
      }
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Uninstall MangaDex" }));
    expect(screen.getByRole("heading", { name: "Remove plugin" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("heading", { name: "Remove plugin" })).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalledWith(
      "/api/sources/mangadex/",
      expect.objectContaining({ method: "DELETE" }),
    );
    await user.click(screen.getByRole("button", { name: "Uninstall MangaDex" }));
    await user.click(screen.getByRole("button", { name: "Remove" }));
    expect(await screen.findByText("MangaDex removed.")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/sources/mangadex/",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("shows progress while a migration applies", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    const aggregate = {
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
      sourceName: "MangaDex",
    };
    let mangaGets = 0;
    let resolveApply!: (value: Response) => void;
    const applyGate = new Promise<Response>((resolve) => {
      resolveApply = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/trackers") return Response.json([]);
        if (path.includes("/migration/candidates")) {
          return Response.json({
            candidates: [
              {
                source: {
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
                },
                result: {
                  id: "discovered-1",
                  sourceId: "asurascans",
                  title: "Yosuga no Sora",
                  coverUrl: "https://example.test/cover.jpg",
                },
              },
            ],
            failedSources: 0,
            searched: 1,
          });
        }
        if (path.includes("/migration/apply") && init?.method === "POST") {
          return await applyGate;
        }
        if (path.includes(`/api/manga/${mangaId}`)) {
          if (!init?.method) mangaGets++;
          return Response.json(aggregate);
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
    await user.click(await screen.findByRole("button", { name: "Migrate" }));
    await user.click(await screen.findByRole("button", { name: /Yosuga no Sora/ }));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    expect(screen.getByRole("button", { name: /Migrating/ })).toBeDisabled();

    // The daemon now reports the replacement plugin; the details view must
    // refetch even though migration kept the canonical id.
    const getsBeforeApplyResolved = mangaGets;
    aggregate.sourceName = "Asura Scans";
    resolveApply(Response.json({ manga: aggregate, source: "asurascans", chapterMap: {} }));
    await waitFor(() => expect(mangaGets).toBe(getsBeforeApplyResolved + 1));
    expect(await screen.findByText("Asura Scans · completed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Migrate" })).toBeInTheDocument();
  });

  it("uploads a backup only after confirming from settings", async () => {
    window.history.pushState({}, "", "/settings/data");
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
    await user.click(await screen.findByRole("button", { name: "Import JSON" }));
    expect(screen.getByRole("heading", { name: "Import backup" })).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalledWith("/api/import", expect.anything());
    await user.upload(
      screen.getByLabelText("Choose file"),
      new File(["{}"], "backup.json", { type: "application/json" }),
    );
    expect(await screen.findByText("Backup imported.")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/import",
      expect.objectContaining({ method: "POST" }),
    );
    // Restored preferences and statistics are re-read after the import.
    await waitFor(() => {
      const calls = (path: string) =>
        fetchMock.mock.calls.filter(([url]) => String(url) === path).length;
      expect(calls("/api/settings")).toBeGreaterThanOrEqual(2);
      expect(calls("/api/stats")).toBeGreaterThanOrEqual(2);
    });
  });

  it("shows an error when a backup import fails", async () => {
    window.history.pushState({}, "", "/settings/data");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/sources" || path === "/api/categories") return Response.json([]);
        if (path === "/api/import") {
          return Response.json(
            { error: { message: "unsupported backup version 9" } },
            { status: 400 },
          );
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
    await user.click(await screen.findByRole("button", { name: "Import JSON" }));
    await user.upload(
      screen.getByLabelText("Choose file"),
      new File(["{}"], "backup.json", { type: "application/json" }),
    );
    expect(await screen.findByText("unsupported backup version 9")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Import backup" })).not.toBeInTheDocument();
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
    await user.keyboard("{Enter}");
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
    fireEvent.click(screen.getByRole("button", { name: "Install plugins" }));
    expect(screen.getByRole("heading", { name: "Available plugins" })).toBeInTheDocument();
    await act(async () => {
      resolveSources(Response.json([]));
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
  });

  // Leaving the reader inside the debounce window must still record the
  // position the reader showed at exit, not drop the write.
  it("flushes a pending progress write when leaving the reader", async () => {
    vi.useFakeTimers();
    window.history.pushState({}, "", "/");
    window.history.pushState({}, "", `/reader/${mangaId}/${chapterId}`);
    const progressPosts: Record<string, unknown>[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/progress" || path === "/api/progress/complete") {
          progressPosts.push(JSON.parse(String(init?.body ?? "{}")));
          return Response.json({});
        }
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
            chapters: [{ id: chapterId, mangaId, chapterNumber: 1, downloaded: false }],
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
    const view = render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    let loaded = false;
    await act(async () => {
      for (let i = 0; i < 30 && !loaded; i++) {
        await vi.advanceTimersByTimeAsync(50);
        loaded = !!screen.queryByRole("button", { name: "Single" });
      }
    });
    expect(screen.getByRole("button", { name: "Single" })).toBeInTheDocument();
    const baseline = progressPosts.length;

    fireEvent.keyDown(window, { key: "ArrowRight" });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(progressPosts).toHaveLength(baseline);

    act(() => {
      view.unmount();
    });
    expect(progressPosts).toHaveLength(baseline + 1);
    expect(progressPosts[progressPosts.length - 1]).toMatchObject({
      mangaId,
      lastReadChapterId: chapterId,
      lastReadPage: 2,
    });
  });

  // Reader shortcuts must not fire while the user operates a form control,
  // for example the page slider or an open search field.
  it("ignores reader shortcuts while a form control has focus", async () => {
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
            chapters: [{ id: chapterId, mangaId, chapterNumber: 1, downloaded: false }],
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
    const position = () => screen.getByText("1 / 2");
    expect(position()).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByText("2 / 2")).toBeInTheDocument();

    const slider = screen.getByRole("slider");
    fireEvent.keyDown(slider, { key: "ArrowRight" });
    fireEvent.keyDown(slider, { key: "ArrowLeft" });
    fireEvent.keyDown(slider, { key: "w" });
    expect(screen.getByText("2 / 2")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Webtoon" })).not.toHaveClass("bg-amber-400");
  });

  // A transient failure while opening the chapter offers a direct retry
  // instead of forcing the reader back out.
  it("retries a failed chapter load", async () => {
    window.history.pushState({}, "", `/reader/${mangaId}/${chapterId}`);
    let pageRequests = 0;
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
            chapters: [{ id: chapterId, mangaId, chapterNumber: 1, downloaded: false }],
            trackers: [],
          });
        }
        if (path.includes(`/api/chapters/${chapterId}/pages`)) {
          pageRequests++;
          if (pageRequests === 1) {
            return Response.json({ error: { message: "source unreachable" } }, { status: 503 });
          }
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
    expect(await screen.findByText("source unreachable")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Retry/i }));
    expect(await screen.findByAltText("Page 1")).toBeInTheDocument();
    expect(screen.queryByAltText("Page 2")).not.toBeInTheDocument();
  });

  // A broken page image degrades into an inline retry instead of a permanent
  // broken image for the session.
  it("offers a retry when a page image fails to load", async () => {
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
            chapters: [{ id: chapterId, mangaId, chapterNumber: 1, downloaded: false }],
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
    const image = await screen.findByAltText("Page 1");
    // Paged pages must fit the viewport instead of rendering at full width.
    expect(image).toHaveClass("object-contain");
    fireEvent.error(image);
    fireEvent.click(screen.getByRole("button", { name: "Retry page" }));
    expect(await screen.findByAltText("Page 1")).toHaveAttribute(
      "src",
      `/api/pages/${pageOne}/image?retry=1`,
    );
  });

  it("shows a clear state when the reader opens without a chapter", async () => {
    window.history.pushState({}, "", "/reader");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json([])),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("No chapter selected")).toBeInTheDocument();
    expect(screen.queryByText("Loading reader")).not.toBeInTheDocument();
  });

  it("shows an empty state for a chapter without pages", async () => {
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
            chapters: [{ id: chapterId, mangaId, chapterNumber: 1, downloaded: false }],
            trackers: [],
          });
        }
        if (path.includes(`/api/chapters/${chapterId}/pages`)) {
          return Response.json([]);
        }
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("This chapter has no pages")).toBeInTheDocument();
    expect(screen.queryByText("Loading reader")).not.toBeInTheDocument();
  });
});

describe("reader resume position", () => {
  it("aligns the restored page with the double-page spread", () => {
    // Page 4 was the last read page; a double spread must open on its pair.
    expect(resumeIndex(4, 10, "double")).toBe(2);
    expect(resumeIndex(null, 10, "double")).toBe(0);
    expect(resumeIndex(3, 10, "single")).toBe(2);
    expect(resumeIndex(99, 10, "single")).toBe(9);
    expect(resumeIndex(99, 10, "double")).toBe(8);
  });
});

describe("tracker binding feedback", () => {
  const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
  it("requires confirmation to unbind and surfaces failures", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    let unbindCalls = 0;
    let mangaGets = 0;
    const aggregate = () => ({
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
      trackers: [
        {
          id: 1,
          mangaId,
          trackerType: "AniList",
          remoteId: "42",
          remoteTitle: "Yosuga no Sora",
          lastSyncedChapter: 0,
        },
      ],
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers")
          return Response.json([
            {
              name: "AniList",
              capabilities: {
                search: true,
                status: true,
                scrobble: true,
                oauth: true,
                token: true,
              },
              credential: true,
              configured: true,
            },
          ]);
        if (path === `/api/manga/${mangaId}`) {
          mangaGets++;
          return Response.json(aggregate());
        }
        if (path === `/api/manga/${mangaId}/trackers/AniList`) {
          unbindCalls++;
          if (unbindCalls === 1) {
            return Response.json(
              { error: { message: "tracker session expired" } },
              { status: 500 },
            );
          }
          return new Response(null, { status: 204 });
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
    await user.click(await screen.findByRole("button", { name: "AniList actions" }));
    await user.click(screen.getByRole("button", { name: "Unbind" }));
    // The destructive step asks first.
    expect(unbindCalls).toBe(0);
    await user.click(screen.getByRole("button", { name: "Confirm unbind" }));
    expect(await screen.findByText("tracker session expired")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm unbind" }));
    await waitFor(() => expect(unbindCalls).toBe(2));
    expect(mangaGets).toBeGreaterThanOrEqual(2);
    expect(screen.queryByText("tracker session expired")).not.toBeInTheDocument();
  });

  it("disables bind results while a bind runs and reports failures", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    const bindResolvers: Array<(value: Response) => void> = [];
    let bindBody: Record<string, unknown> | undefined;
    let bound = false;
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/trackers")
          return Promise.resolve(
            Response.json([
              {
                name: "AniList",
                capabilities: {
                  search: true,
                  status: true,
                  scrobble: true,
                  oauth: true,
                  token: true,
                },
                credential: true,
                configured: true,
              },
            ]),
          );
        if (path.includes(`/api/manga/${mangaId}`) && !path.includes("/trackers")) {
          return Promise.resolve(
            Response.json({
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
              trackers: bound
                ? [
                    {
                      id: 9,
                      mangaId,
                      trackerType: "AniList",
                      remoteId: "42",
                      remoteTitle: "Yosuga no Sora",
                      lastSyncedChapter: 0,
                    },
                  ]
                : [],
            }),
          );
        }
        if (path.startsWith("/api/trackers/AniList/search")) {
          return Promise.resolve(
            Response.json([{ remoteId: "42", title: "Yosuga no Sora", score: 8, chapters: 12 }]),
          );
        }
        if (path.endsWith("/trackers/AniList/bind")) {
          bindBody = JSON.parse(String(init?.body));
          return new Promise<Response>((resolve) => {
            bindResolvers.push(resolve);
          });
        }
        return Promise.resolve(Response.json([]));
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Tracking" }));
    await user.type(await screen.findByPlaceholderText("Search provider"), "Yosuga");
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    const result = await screen.findByRole("button", { name: /Yosuga no Sora 42/ });
    await user.click(result);
    expect(bindBody).toEqual({
      remoteId: "42",
      remoteTitle: "Yosuga no Sora",
      totalRemoteChapters: 12,
    });
    expect(screen.getByRole("button", { name: /Binding/ })).toBeDisabled();
    await act(async () => {
      bindResolvers.shift()!(Response.json({ error: { message: "no session" } }, { status: 500 }));
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(await screen.findByText("no session")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Yosuga no Sora 42/ })).toBeEnabled();

    // A successful rebind updates the list in place and keeps the dialog open.
    await user.click(screen.getByRole("button", { name: /Yosuga no Sora 42/ }));
    await act(async () => {
      bound = true;
      bindResolvers.shift()!(
        Response.json({
          id: 9,
          mangaId,
          trackerType: "AniList",
          remoteId: "42",
          remoteTitle: "Yosuga no Sora",
          lastSyncedChapter: 0,
        }),
      );
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(await screen.findByRole("button", { name: "AniList actions" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Tracking" })).toBeInTheDocument();
    expect(await screen.findByText("Bound to Yosuga no Sora.")).toBeInTheDocument();
  });

  it("disables the tracker search button while it runs", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    let resolveSearch!: (value: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers")
          return Promise.resolve(
            Response.json([
              {
                name: "AniList",
                capabilities: {
                  search: true,
                  status: true,
                  scrobble: true,
                  oauth: true,
                  token: true,
                },
                credential: true,
                configured: true,
              },
            ]),
          );
        if (path.includes(`/api/manga/${mangaId}`) && !path.includes("/trackers")) {
          return Promise.resolve(
            Response.json({
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
            }),
          );
        }
        if (path.startsWith("/api/trackers/AniList/search")) {
          return new Promise<Response>((resolve) => {
            resolveSearch = resolve;
          });
        }
        return Promise.resolve(Response.json([]));
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Tracking" }));
    await user.type(await screen.findByPlaceholderText("Search provider"), "Yosuga");
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(screen.getByRole("button", { name: /Searching/ })).toBeDisabled();
    await act(async () => {
      resolveSearch(Response.json([{ remoteId: "42", title: "Yosuga no Sora" }]));
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(await screen.findByRole("button", { name: /Yosuga no Sora 42/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Search" })).toBeEnabled();
  });

  it("renders bound tracker status, progress, score, and dates", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    let statusCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "anilist",
              capabilities: { oauth: true, token: true },
              credential: true,
              authType: "oauth",
              configured: true,
              connectedAs: "example-reader",
            },
          ]);
        }
        if (path === `/api/manga/${mangaId}`) {
          return Response.json({
            manga: {
              id: mangaId,
              sourceId: "0198c0de-7a00-7000-8000-00000000abcd",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: `/api/manga/${mangaId}/cover`,
              inLibrary: true,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            categories: [],
            chapters: [],
            trackers: [
              {
                id: 1,
                mangaId,
                trackerType: "anilist",
                remoteId: "45821",
                remoteTitle: "Yosuga no Sora",
                remoteScore: 7.5,
                remoteStatus: "CURRENT",
                lastSyncedChapter: 8,
                totalRemoteChapters: 12,
                startedAt: Date.parse("2024-01-02T00:00:00Z") / 1000,
                finishedAt: Date.parse("2024-02-01T00:00:00Z") / 1000,
              },
            ],
          });
        }
        if (path === `/api/manga/${mangaId}/trackers/status`) {
          statusCalls++;
          return Response.json([
            {
              remoteId: "45821",
              title: "Yosuga no Sora",
              status: "CURRENT",
              score: 9,
              progress: statusCalls === 1 ? 10 : 11,
              totalChapters: 12,
            },
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
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Tracking" }));

    expect(screen.getByRole("img", { name: "AniList logo" })).toBeInTheDocument();
    expect(screen.getAllByText("Yosuga no Sora").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText("CURRENT")).toBeInTheDocument();
    expect(screen.getByText("10 / 12")).toBeInTheDocument();
    expect(screen.getByLabelText("AniList score")).toHaveValue(7.5);
    expect(screen.getByLabelText("AniList start date")).toHaveValue("2024-01-02");
    expect(screen.getByLabelText("AniList finish date")).toHaveValue("2024-02-01");
    expect(statusCalls).toBe(1);

    await user.click(screen.getByRole("button", { name: "Close" }));
    await user.click(await screen.findByRole("button", { name: "Tracking" }));
    expect(await screen.findByText("11 / 12")).toBeInTheDocument();
    expect(screen.getByLabelText("AniList score")).toHaveValue(7.5);
    expect(statusCalls).toBe(2);
  });

  it("updates tracker score and dates in one request", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    let updateBody: Record<string, unknown> | undefined;
    let updated = false;
    const binding = () => ({
      id: 1,
      mangaId,
      trackerType: "anilist",
      remoteId: "45821",
      remoteTitle: "Yosuga no Sora",
      remoteScore: updated ? 8.5 : 7.5,
      remoteStatus: "CURRENT",
      lastSyncedChapter: 8,
      totalRemoteChapters: 12,
      startedAt: Date.parse(updated ? "2024-01-03T00:00:00Z" : "2024-01-02T00:00:00Z") / 1000,
      finishedAt: Date.parse(updated ? "2024-02-02T00:00:00Z" : "2024-02-01T00:00:00Z") / 1000,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "anilist",
              capabilities: { oauth: true, token: true },
              credential: true,
              authType: "oauth",
              configured: true,
            },
          ]);
        }
        if (path === `/api/manga/${mangaId}/trackers/anilist`) {
          updateBody = JSON.parse(String(init?.body));
          updated = true;
          return Response.json(binding());
        }
        if (path === `/api/manga/${mangaId}`) {
          return Response.json({
            manga: {
              id: mangaId,
              sourceId: "0198c0de-7a00-7000-8000-00000000abcd",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: `/api/manga/${mangaId}/cover`,
              inLibrary: true,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            categories: [],
            chapters: [],
            trackers: [binding()],
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
    const score = screen.getByLabelText("AniList score");
    await user.clear(score);
    await user.type(score, "8.5");
    fireEvent.change(screen.getByLabelText("AniList start date"), {
      target: { value: "2024-01-03" },
    });
    fireEvent.change(screen.getByLabelText("AniList finish date"), {
      target: { value: "2024-02-02" },
    });
    await user.click(screen.getByRole("button", { name: "Save AniList tracking" }));

    await waitFor(() =>
      expect(updateBody).toEqual({
        score: 8.5,
        startedAt: Date.parse("2024-01-03T00:00:00Z") / 1000,
        finishedAt: Date.parse("2024-02-02T00:00:00Z") / 1000,
      }),
    );
    expect(await screen.findByText("AniList tracking updated.")).toBeInTheDocument();
  });

  it("hides trackers unless they are configured and connected", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "anilist",
              capabilities: { oauth: true, token: true },
              credential: false,
              authType: "oauth",
              configured: true,
            },
            {
              name: "kitsu",
              capabilities: { oauth: false, token: true },
              credential: false,
              authType: "password",
              configured: true,
            },
            {
              name: "mangaupdates",
              capabilities: { oauth: false, token: true },
              credential: true,
              authType: "password",
              configured: false,
            },
          ]);
        }
        if (path === `/api/manga/${mangaId}`) {
          return Response.json({
            manga: {
              id: mangaId,
              sourceId: "0198c0de-7a00-7000-8000-00000000abcd",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: `/api/manga/${mangaId}/cover`,
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

    expect(screen.queryByRole("heading", { name: "AniList" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Kitsu" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "MangaUpdates" })).not.toBeInTheDocument();
    expect(screen.getByText("No trackers available.")).toBeInTheDocument();
  });
});

describe("settings sections", () => {
  const settingRows = () => [
    {
      key: "appearance.date_format",
      value: "relative",
      default: "relative",
      type: "string",
      description: "Timestamp display format",
      hidden: false,
    },
    {
      key: "library.update_interval",
      value: 86400000000000,
      default: 86400000000000,
      type: "duration",
      description: "Automatic library update interval in nanoseconds",
      hidden: false,
    },
    {
      key: "reader.default_mode",
      value: "single",
      default: "single",
      type: "string",
      description: "Default reader mode",
      hidden: false,
    },
    {
      key: "library.view.sort",
      value: "recent",
      default: "recent",
      type: "string",
      description: "Library ordering",
      hidden: true,
    },
  ];

  it("lists the settings sections and opens one", async () => {
    window.history.pushState({}, "", "/settings");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/settings") return Response.json(settingRows());
        if (path === "/api/health") return Response.json({ ok: true });
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    const main = await screen.findByRole("main");
    for (const title of [
      "Appearance",
      "Library",
      "Reader",
      "Downloads",
      "Tracking",
      "Browse",
      "Data and storage",
      "Advanced",
    ]) {
      expect(within(main).getByText(title)).toBeInTheDocument();
    }
    await user.click(within(main).getByText("Reader"));
    expect(await screen.findByText("Default mode")).toBeInTheDocument();
    expect(within(main).queryByText("Date format")).not.toBeInTheDocument();
    // Hidden view state never reaches a section.
    expect(within(main).queryByText("Library ordering")).not.toBeInTheDocument();
  });

  it("saves a setting from its section", async () => {
    window.history.pushState({}, "", "/settings/reader");
    const writes: Array<{ url: string; body: string }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/settings") return Response.json(settingRows());
        if (path.startsWith("/api/settings/") && init?.method === "PUT") {
          writes.push({ url: path, body: String(init.body) });
          return Response.json({});
        }
        if (path === "/api/health") return Response.json({ ok: true });
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.selectOptions(
      await screen.findByRole("combobox", { name: /Default mode/ }),
      "double",
    );
    await waitFor(() => {
      const write = writes.find(
        (call) => decodeURIComponent(call.url) === "/api/settings/reader.default_mode",
      );
      expect(write).toBeTruthy();
      expect(JSON.parse(write!.body)).toEqual({ value: "double" });
    });
    expect(await screen.findByText("reader.default_mode updated.")).toBeInTheDocument();
  });

  it("searches all settings from the header control", async () => {
    window.history.pushState({}, "", "/settings");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/settings") return Response.json(settingRows());
        if (path === "/api/health") return Response.json({ ok: true });
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Search settings" }));
    fireEvent.change(screen.getByPlaceholderText("Search settings"), {
      target: { value: "update interval" },
    });
    const result = await screen.findByRole("link", { name: /Update interval/ });
    await user.click(result);
    await waitFor(() => {
      expect(document.getElementById("library.update_interval")).toHaveAttribute(
        "data-highlighted",
        "true",
      );
    });
    expect(window.location.hash).toBe("#library.update_interval");
  });
});

describe("settings credential feedback", () => {
  it("surfaces clearance failures and clears inputs on success", async () => {
    window.history.pushState({}, "", "/browse?tab=plugins");
    let clearanceCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/sources") {
          return Response.json([
            {
              id: "0198c0de-7a00-7000-8000-00000000abcd",
              name: "MangaDex",
              version: "1",
              abiVersion: 1,
              lang: "en",
              baseUrl: "",
              iconUrl: "",
              nsfw: false,
              installedAt: 1,
              loaded: false,
              hasClearance: false,
            },
          ]);
        }
        if (path === "/api/sources/0198c0de-7a00-7000-8000-00000000abcd/clearance") {
          clearanceCalls++;
          if (clearanceCalls === 1) {
            return Response.json({ error: { message: "clearance rejected" } }, { status: 400 });
          }
          return Response.json({});
        }
        if (path === "/api/catalog" || path === "/api/categories" || path === "/api/trackers") {
          return Response.json([]);
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
    await user.selectOptions(await screen.findByRole("combobox", { name: /plugin/i }), "MangaDex");
    await user.type(screen.getByPlaceholderText("cf_clearance cookie"), "cf-token");
    await user.type(screen.getByPlaceholderText("Browser user agent"), "Mozilla/test");
    fireEvent.click(screen.getByRole("button", { name: "Submit clearance" }));
    expect(await screen.findByText("clearance rejected")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Submit clearance" }));
    expect(await screen.findByText("Clearance submitted.")).toBeInTheDocument();
    expect(clearanceCalls).toBe(2);
    expect(screen.getByPlaceholderText("cf_clearance cookie")).toHaveValue("");
  });

  it("reports tracker token save failures", async () => {
    window.history.pushState({}, "", "/settings/tracking");
    let tokenCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "AniList",
              capabilities: {
                search: true,
                status: true,
                scrobble: true,
                oauth: true,
                token: true,
              },
              credential: false,
              authType: "oauth",
              configured: true,
            },
          ]);
        }
        if (path === "/api/trackers/AniList/token") {
          tokenCalls++;
          if (tokenCalls === 1) {
            return Response.json(
              { error: { message: "credential storage unavailable" } },
              { status: 500 },
            );
          }
          return new Response(null, { status: 204 });
        }
        if (path === "/api/sources" || path === "/api/categories") return Response.json([]);
        if (path === "/api/catalog") return Response.json([]);
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Paste token manually" }));
    const tokenInput = await screen.findByPlaceholderText("AniList access token");
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    // A rejected save surfaces the server's reason and keeps the input.
    await user.type(tokenInput, "secret");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("credential storage unavailable")).toBeInTheDocument();
    expect(tokenInput).toHaveValue("secret");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("AniList credentials saved.")).toBeInTheDocument();
    // A successful save closes the manual entry panel and clears the value.
    expect(screen.queryByPlaceholderText("AniList access token")).not.toBeInTheDocument();
  });

  // Recent tracker sync outcomes are visible so failures are not silent.
  it("shows recent tracker sync activity", async () => {
    const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
    window.history.pushState({}, "", "/settings/tracking");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") return Response.json([]);
        if (path === "/api/tracker-sync") {
          return Response.json([
            {
              id: 1,
              mangaId,
              bindingId: 3,
              chapterNumber: 4.5,
              status: "FAILED",
              attempts: 2,
              errorMessage: "tracker unreachable",
            },
          ]);
        }
        if (path === "/api/sources" || path === "/api/categories" || path === "/api/catalog") {
          return Response.json([]);
        }
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("Recent sync activity")).toBeInTheDocument();
    expect(screen.getByText(/tracker unreachable/)).toBeInTheDocument();
  });

  it("connects and disconnects tracker credentials", async () => {
    const open = vi.fn();
    const originalOpen = Object.getOwnPropertyDescriptor(window, "open");
    Object.defineProperty(window, "open", { value: open, configurable: true, writable: true });
    window.history.pushState({}, "", "/settings/tracking");
    let deleted = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/trackers") {
        return Response.json([
          {
            name: "AniList",
            capabilities: { search: true, status: true, scrobble: true, oauth: true, token: true },
            credential: true,
            authType: "oauth",
            configured: true,
          },
          {
            name: "Kitsu",
            capabilities: { search: true, status: true, scrobble: true, oauth: true, token: true },
            credential: false,
            authType: "oauth",
            configured: true,
          },
        ]);
      }
      if (path === "/api/trackers/Kitsu/auth/start") {
        return Response.json({
          authorizationUrl: "https://kitsu.test/auth",
          redirectUri: "http://127.0.0.1:6254/api/trackers/kitsu/auth/callback",
        });
      }
      if (path === "/api/trackers/AniList/credentials") {
        deleted++;
        return new Response(null, { status: 204 });
      }
      if (
        path === "/api/sources" ||
        path === "/api/categories" ||
        path === "/api/catalog" ||
        path === "/api/tracker-sync"
      ) {
        return Response.json([]);
      }
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );

    fireEvent.click(await screen.findByRole("button", { name: "Connect" }));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(fetchMock).toHaveBeenCalledWith("/api/trackers/Kitsu/auth/start", expect.anything());
    expect(open).toHaveBeenCalledWith("https://kitsu.test/auth", "_blank", "noopener");
    expect(await screen.findByText(/Waiting for authorization/)).toBeInTheDocument();

    fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
    expect(await screen.findByText("AniList disconnected.")).toBeInTheDocument();
    expect(deleted).toBe(1);
    if (originalOpen) {
      Object.defineProperty(window, "open", originalOpen);
    } else {
      delete (window as { open?: unknown }).open;
    }
  });

  it("explains unconfigured browser authorization instead of offering connect", async () => {
    window.history.pushState({}, "", "/settings/tracking");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "MangaBaka",
              capabilities: {
                search: true,
                status: true,
                scrobble: true,
                oauth: true,
                token: true,
              },
              credential: false,
              authType: "oauth",
              configured: false,
              configHint: "Set MANGABAKA_CLIENT_ID",
            },
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
    expect(await screen.findByText(/Set MANGABAKA_CLIENT_ID/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Connect" })).not.toBeInTheDocument();
  });

  it("shows the encryption secret hint instead of sign-in forms when credentials are unavailable", async () => {
    window.history.pushState({}, "", "/settings/tracking");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "kitsu",
              capabilities: {
                search: true,
                status: false,
                scrobble: false,
                oauth: false,
                token: true,
              },
              credential: false,
              authType: "password",
              configured: false,
              configHint: "Set MAKIDOKU_SECRET (required to encrypt tracker credentials)",
            },
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
    expect(await screen.findByText(/Set MAKIDOKU_SECRET/)).toBeInTheDocument();
    expect(screen.queryByLabelText("kitsu username")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Log in" })).not.toBeInTheDocument();
  });

  it("signs in to a username and password tracker and surfaces rejections", async () => {
    window.history.pushState({}, "", "/settings/tracking");
    let loginCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "kitsu",
              capabilities: {
                search: true,
                status: false,
                scrobble: false,
                oauth: false,
                token: true,
              },
              credential: false,
              authType: "password",
              configured: true,
            },
            {
              name: "mangaupdates",
              capabilities: {
                search: true,
                status: false,
                scrobble: false,
                oauth: false,
                token: true,
              },
              credential: false,
              authType: "password",
              configured: true,
            },
          ]);
        }
        if (path === "/api/trackers/kitsu/login") {
          loginCalls++;
          if (loginCalls === 1) {
            return Response.json(
              { error: { message: "the tracker rejected these credentials" } },
              { status: 401 },
            );
          }
          return new Response(null, { status: 204 });
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
    const loginButtons = await screen.findAllByRole("button", { name: "Log in" });
    expect(loginButtons).toHaveLength(2);
    const kitsuLogin = loginButtons[0];
    expect(kitsuLogin).toBeDisabled();
    await user.type(screen.getByLabelText("kitsu username"), "user@example.com");
    await user.type(screen.getByLabelText("kitsu password"), "secret");
    // Each provider owns its form state; typing into one card never fills the other.
    expect(screen.getByLabelText("mangaupdates username")).toHaveValue("");
    expect(screen.getByLabelText("mangaupdates password")).toHaveValue("");
    fireEvent.click(kitsuLogin);
    expect(await screen.findByText("the tracker rejected these credentials")).toBeInTheDocument();

    fireEvent.click(kitsuLogin);
    expect(await screen.findByText("kitsu connected.")).toBeInTheDocument();
    expect(loginCalls).toBe(2);
    expect(screen.getByLabelText("kitsu username")).toHaveValue("");
  });

  it("resolves a pending authorization when the daemon broadcasts the credential event", async () => {
    class FakeSocket {
      static instances: FakeSocket[] = [];
      onmessage: ((event: { data: string }) => void) | null = null;
      onclose: (() => void) | null = null;
      constructor(public url: string) {
        FakeSocket.instances.push(this);
      }
      close() {}
    }
    vi.stubGlobal("WebSocket", FakeSocket);

    const open = vi.fn();
    const originalOpen = Object.getOwnPropertyDescriptor(window, "open");
    Object.defineProperty(window, "open", { value: open, configurable: true, writable: true });

    window.history.pushState({}, "", "/settings/tracking");
    let connected = false;
    let socketRef: FakeSocket | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/tracker-sync") return Response.json([]);
        if (path === "/api/trackers") {
          return Response.json([
            {
              name: "AniList",
              capabilities: {
                search: true,
                status: true,
                scrobble: true,
                oauth: true,
                token: true,
              },
              credential: connected,
              authType: "oauth",
              configured: true,
              ...(connected ? { connectedAs: "viewer-user" } : {}),
            },
          ]);
        }
        if (path === "/api/trackers/AniList/auth/start") {
          return Response.json({
            authorizationUrl: "https://anilist.test/auth",
            redirectUri: "http://127.0.0.1:6254/api/trackers/anilist/auth/callback",
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
    await user.click(await screen.findByRole("button", { name: "Connect" }));
    await waitFor(() =>
      expect(
        FakeSocket.instances.some((socket) => socket.url.includes("/api/trackers/events")),
      ).toBe(true),
    );
    expect(open).toHaveBeenCalledWith("https://anilist.test/auth", "_blank", "noopener");
    expect(await screen.findByText(/Waiting for authorization/)).toBeInTheDocument();

    connected = true;
    socketRef = FakeSocket.instances.find((entry) => entry.url.includes("/api/trackers/events"));
    await act(async () => {
      socketRef?.onmessage?.({ data: JSON.stringify({ type: "credentials", tracker: "anilist" }) });
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(await screen.findByText(/Connected as viewer-user/)).toBeInTheDocument();
    expect(screen.queryByText(/Waiting for authorization/)).not.toBeInTheDocument();

    if (originalOpen) {
      Object.defineProperty(window, "open", originalOpen);
    } else {
      delete (window as { open?: unknown }).open;
    }
    vi.unstubAllGlobals();
  });

  it("removes a category after confirmation", async () => {
    window.history.pushState({}, "", "/settings/library");
    let deleteCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/categories" && !init?.method) {
          return Response.json([{ id: 7, name: "Reading", sortOrder: 1 }]);
        }
        if (path === "/api/categories/7") {
          deleteCalls++;
          return new Response(null, { status: 204 });
        }
        if (path === "/api/sources" || path === "/api/catalog" || path === "/api/trackers") {
          return Response.json([]);
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
    await user.click(await screen.findByRole("button", { name: "Delete category Reading" }));
    expect(await screen.findByRole("heading", { name: "Remove category" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Remove" }));
    expect(await screen.findByText("Category Reading removed.")).toBeInTheDocument();
    expect(deleteCalls).toBe(1);
  });

  // The event stream must survive malformed frames and reconcile after a
  // reconnect instead of drifting from the daemon.
  it("recovers the download snapshot after websocket hiccups", async () => {
    const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
    const chapterId = "0198c0de-7a22-7000-8000-00000000cafe";
    const sockets: Array<{
      onmessage: ((event: { data: string }) => void) | null;
      onopen: (() => void) | null;
      onclose: (() => void) | null;
      onerror: (() => void) | null;
    }> = [];
    class FakeWebSocket {
      onmessage: ((event: { data: string }) => void) | null = null;
      onopen: (() => void) | null = null;
      onclose: (() => void) | null = null;
      onerror: (() => void) | null = null;
      constructor(_url: string) {
        sockets.push(this);
      }
      close() {}
    }
    vi.stubGlobal("WebSocket", FakeWebSocket);
    window.history.pushState({}, "", "/downloads");
    let downloads = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/download" && !init?.method) {
          downloads++;
          return Response.json({
            items: [
              {
                id: 5,
                mangaId,
                chapterId,
                mangaTitle: "Yosuga no Sora",
                chapterTitle: "Chapter 1",
                chapterNumber: 1,
                sourceName: "MangaDex",
                status: "DOWNLOADING",
                progress: 10,
              },
            ],
            stats: { downloadedPages: downloads, retriedRequests: 0, throttledRequests: 0 },
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
    await screen.findByText("Yosuga no Sora · Chapter 1");
    expect(downloads).toBe(1);

    // A malformed frame is ignored without tearing the page down.
    act(() => {
      sockets[0].onmessage?.({ data: "{not json" });
    });
    expect(screen.getByText("Yosuga no Sora · Chapter 1")).toBeInTheDocument();

    // Reconnecting refetches the snapshot so missed events are recovered.
    act(() => {
      sockets[0].onopen?.();
    });
    await waitFor(() => expect(downloads).toBe(2));
    expect(screen.getByText("Yosuga no Sora · Chapter 1")).toBeInTheDocument();
  });

  it("retries failed downloads and clears finished rows", async () => {
    const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
    const chapterId = "0198c0de-7a22-7000-8000-00000000cafe";
    // jsdom has no WebSocket; a silent stub keeps the page's subscription
    // from interfering with the HTTP assertions.
    class FakeWebSocket {
      onmessage: ((event: { data: string }) => void) | null = null;
      onerror: (() => void) | null = null;
      onclose: (() => void) | null = null;
      close() {}
    }
    vi.stubGlobal("WebSocket", FakeWebSocket);
    window.history.pushState({}, "", "/downloads");
    let retries = 0;
    let cleared = 0;
    const failedItem = {
      id: 5,
      mangaId,
      chapterId: chapterId,
      mangaTitle: "Yosuga no Sora",
      chapterTitle: "Chapter 1",
      chapterNumber: 1,
      sourceName: "MangaDex",
      status: "FAILED",
      progress: 40,
      errorMessage: "connection reset",
    };
    const doneItem = {
      ...failedItem,
      id: 6,
      status: "COMPLETED",
      progress: 100,
      errorMessage: null,
    };
    let items: Array<Record<string, unknown>> = [failedItem, doneItem];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/download" && !init?.method) {
          return Response.json({
            items,
            stats: { downloadedPages: 0, retriedRequests: 0, throttledRequests: 0 },
          });
        }
        if (path === "/api/download/5/retry") {
          retries++;
          items = items.map((item) =>
            item.id === 5 ? { ...failedItem, status: "PENDING", progress: 0 } : item,
          );
          return new Response(null, { status: 204 });
        }
        if (path === "/api/download/clear") {
          cleared++;
          items = [];
          return Response.json({ removed: 1 });
        }
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("connection reset")).toBeInTheDocument();

    // The finished row keeps "Clear finished" available while the failed one
    // offers retry.
    fireEvent.click(await screen.findByRole("button", { name: "retry" }));
    expect(retries).toBe(1);
    expect(await screen.findByText(/· pending/)).toBeInTheDocument();

    fireEvent.click(await screen.findByRole("button", { name: "Clear finished" }));
    expect(cleared).toBe(1);
    expect(await screen.findByText("Download queue is empty")).toBeInTheDocument();
  });
});

describe("details page action feedback", () => {
  const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
  const aggregate = (overrides: Record<string, unknown> = {}) => ({
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
      ...overrides,
    },
    categories: [] as unknown[],
    chapters: [],
    trackers: [],
  });

  // A failed library toggle or category change must not blank the page.
  it("reports library and category failures inline", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === `/api/manga/${mangaId}` && !init?.method) {
          return Response.json(aggregate());
        }
        if (path === `/api/manga/${mangaId}/library`) {
          return Response.json({ error: { message: "library write rejected" } }, { status: 500 });
        }
        if (/\/api\/manga\/[^/]+\/categories\/\d+$/.test(path)) {
          return Response.json({ error: { message: "category write rejected" } }, { status: 500 });
        }
        if (path === "/api/categories") {
          return Response.json([{ id: 7, name: "Reading", sortOrder: 1 }]);
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
    expect(await screen.findByRole("heading", { name: "Yosuga no Sora" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /In library/ }));
    expect(await screen.findByText("library write rejected")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Yosuga no Sora" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Reading" }));
    expect(await screen.findByText("category write rejected")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Yosuga no Sora" })).toBeInTheDocument();
  });

  it("shows busy and confirmation states when queueing downloads", async () => {
    window.history.pushState({}, "", `/manga/${mangaId}`);
    let resolveQueue!: (value: Response) => void;
    const chapterId = "0198c0de-7a22-7000-8000-00000000cafe";
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const path = String(input);
        if (path === `/api/manga/${mangaId}` && !init?.method) {
          return Promise.resolve(
            Response.json({
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
                { id: chapterId, mangaId, chapterNumber: 1, language: "en", downloaded: false },
              ],
              trackers: [],
            }),
          );
        }
        if (path === "/api/download" && init?.method === "POST") {
          return new Promise<Response>((resolve) => {
            resolveQueue = resolve;
          });
        }
        if (path === "/api/categories") return Promise.resolve(Response.json([]));
        return Promise.resolve(Response.json([]));
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    expect(await screen.findByRole("heading", { name: "Yosuga no Sora" })).toBeInTheDocument();

    // Nothing is selected yet, so queueing is blocked at the button.
    expect(screen.getByRole("button", { name: /Download/ })).toBeDisabled();
    await user.click(screen.getByRole("checkbox"));
    expect(screen.getByRole("button", { name: /Download/ })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: /Download/ }));
    expect(screen.getByRole("button", { name: /Queueing/ })).toBeDisabled();

    await act(async () => {
      resolveQueue(Response.json({ items: [] }));
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(await screen.findByText(/queued for download/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Download/ })).toBeEnabled();
  });
});

describe("library layout", () => {
  it("keeps a single search surface and stores the layout in the settings service", async () => {
    window.history.pushState({}, "", "/");
    const writes: Array<{ url: string; body: string }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/health") return Response.json({ ok: true });
        if (path.startsWith("/api/library")) {
          return Response.json([
            {
              id: "alpha",
              title: "Alpha",
              coverUrl: "",
              updatedAt: 3,
              status: "ongoing",
              sourceId: "source-one",
              sourceName: "Source One",
              categories: [],
              unreadChapters: 4,
            },
            {
              id: "beta",
              title: "Beta",
              coverUrl: "",
              updatedAt: 1,
              status: "completed",
              sourceId: "source-one",
              sourceName: "Source One",
              categories: [],
              unreadChapters: 0,
            },
          ]);
        }
        if (path.startsWith("/api/settings/") && init?.method === "PUT") {
          writes.push({ url: path, body: String(init.body) });
          return Response.json({});
        }
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Beta")).toBeInTheDocument();

    // The header control is the only search surface; the library page carries
    // no second one.
    expect(screen.queryByPlaceholderText("Search your library")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Search library" }));
    expect(screen.getByPlaceholderText("Search your library")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    expect(screen.getByLabelText("4 unread chapters")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Filters" }));
    fireEvent.click(screen.getByRole("button", { name: "Display" }));
    fireEvent.click(screen.getByRole("switch", { name: /Unread badge/ }));
    expect(screen.queryByLabelText("4 unread chapters")).not.toBeInTheDocument();
    await waitFor(() => {
      const write = writes.find(
        (call) => decodeURIComponent(call.url) === "/api/settings/library.view.unread_badge",
      );
      expect(write).toBeTruthy();
      expect(JSON.parse(write!.body)).toEqual({ value: false });
    });
  });
});

describe("search robustness", () => {
  it("debounces library search and drops stale responses", async () => {
    vi.useFakeTimers();
    window.history.pushState({}, "", "/browse");
    const libraryCalls: string[] = [];
    let resolveInitial!: (value: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL): Promise<Response> => {
        const path = String(input);
        if (path.startsWith("/api/library")) {
          libraryCalls.push(path);
          if (libraryCalls.length === 1) {
            // The opening empty query stalls; a later query must still win.
            return new Promise<Response>((resolve) => {
              resolveInitial = resolve;
            });
          }
          return Promise.resolve(
            Response.json([
              {
                id: "fresh",
                title: "Fresh Title",
                coverUrl: "",
                updatedAt: 1,
                categories: [],
                unreadChapters: 0,
              },
            ]),
          );
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

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    fireEvent.click(screen.getByRole("button", { name: "Search library" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    const input = screen.getByPlaceholderText("Search your library");
    fireEvent.change(input, { target: { value: "yo" } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    fireEvent.change(input, { target: { value: "yosuga" } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(screen.getByText("Fresh Title")).toBeInTheDocument();

    // The cancelled "yo" request never fired; only the settled queries ran.
    expect(libraryCalls.filter((call) => call.endsWith("q=yo"))).toHaveLength(0);

    await act(async () => {
      resolveInitial(
        Response.json([
          {
            id: "stale",
            title: "Stale Title",
            coverUrl: "",
            updatedAt: 2,
            categories: [],
            unreadChapters: 0,
          },
        ]),
      );
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(screen.queryByText("Stale Title")).not.toBeInTheDocument();
    expect(screen.getByText("Fresh Title")).toBeInTheDocument();
    vi.useRealTimers();
  });

  it("reports library errors in global search", async () => {
    window.history.pushState({}, "", "/browse");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.startsWith("/api/library")) {
          return Response.json({ error: { message: "database busy" } }, { status: 500 });
        }
        if (path === "/api/health") return Response.json({ ok: true });
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Search library" }));
    const input = await screen.findAllByPlaceholderText("Search your library");
    fireEvent.change(input[input.length - 1], { target: { value: "anything" } });
    expect((await screen.findAllByText("database busy")).length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText("No library matches.")).not.toBeInTheDocument();
  });

  it("reports partial plugin failures in browse results", async () => {
    window.history.pushState({}, "", "/browse");
    const sourceOne = "0198c0de-7a00-7000-8000-000000000001";
    const sourceTwo = "0198c0de-7a00-7000-8000-000000000002";
    const source = (id: string, name: string) => ({
      id,
      name,
      version: "1",
      abiVersion: 1,
      lang: "en",
      baseUrl: "",
      iconUrl: "",
      nsfw: false,
      installedAt: 1,
      loaded: false,
      hasClearance: false,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/sources") {
          return Response.json([source(sourceOne, "MangaDex"), source(sourceTwo, "Asura")]);
        }
        if (path.startsWith(`/api/sources/${sourceOne}/search`)) {
          return Response.json({ error: { message: "blocked" } }, { status: 503 });
        }
        if (path.startsWith(`/api/sources/${sourceTwo}/search`)) {
          return Response.json({
            page: 1,
            hasNextPage: false,
            items: [{ id: "remote-1", title: "Yosuga no Sora", coverUrl: "" }],
          });
        }
        if (path === "/api/health") return Response.json({ ok: true });
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.type(await screen.findByPlaceholderText("Search installed plugins"), "yosuga");
    await user.keyboard("{Enter}");
    expect(await screen.findByText("Yosuga no Sora")).toBeInTheDocument();
    expect(screen.getByText("1 of 2 plugins failed to respond.")).toBeInTheDocument();
  });

  it("hides NSFW plugins across the Browse tabs when configured", async () => {
    window.history.pushState({}, "", "/browse");
    const safe = {
      id: "safe",
      name: "Safe source",
      version: "1",
      abiVersion: 1,
      lang: "en",
      baseUrl: "",
      iconUrl: "",
      nsfw: false,
      installedAt: 1,
      loaded: false,
      hasClearance: false,
    };
    const adult = {
      id: "adult",
      name: "Adult source",
      version: "1",
      abiVersion: 1,
      lang: "en",
      baseUrl: "",
      iconUrl: "",
      nsfw: true,
      installedAt: 1,
      loaded: false,
      hasClearance: false,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/sources") return Response.json([safe, adult]);
        if (path === "/api/sources/catalog")
          return Response.json([
            { ...safe, installed: false, compatible: true },
            { ...adult, installed: false, compatible: true },
          ]);
        if (path === "/api/settings")
          return Response.json([
            {
              key: "browse.hide_nsfw",
              value: true,
              default: false,
              type: "boolean",
              description: "",
            },
          ]);
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "plugins" }));
    expect((await screen.findAllByText("Safe source")).length).toBeGreaterThan(0);
    expect(screen.queryByText("Adult source")).not.toBeInTheDocument();
  });

  it("groups Browse sources by pinned and recent usage", async () => {
    window.history.pushState({}, "", "/browse");
    const source = (id: string, name: string, pinned: boolean, lastUsedAt?: number) => ({
      id,
      name,
      version: "1",
      abiVersion: 1,
      lang: "en",
      baseUrl: "",
      iconUrl: "",
      nsfw: false,
      installedAt: 1,
      loaded: false,
      hasClearance: false,
      pinned,
      lastUsedAt,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/sources") {
          return Response.json([
            source("pinned", "Pinned source", true),
            source("recent", "Recent source", false, 20),
            source("other", "Other source", false),
          ]);
        }
        if (path === "/api/settings") return Response.json([]);
        if (path === "/api/stats")
          return Response.json({ readingSeconds: 0, titleCount: 0, chapterCount: 0, daily: [] });
        return Response.json([]);
      }),
    );
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("Pinned")).toBeInTheDocument();
    expect(screen.getByText("Last used")).toBeInTheDocument();
    expect(screen.getByText("Other")).toBeInTheDocument();
    expect(screen.getByText("Pinned source")).toBeInTheDocument();
    expect(screen.getByText("Recent source")).toBeInTheDocument();
    expect(screen.getByText("Other source")).toBeInTheDocument();
  });

  it("acknowledges a library update from the updates page", async () => {
    const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
    const chapterId = "0198c0de-7a22-7000-8000-00000000cafe";
    window.history.pushState({}, "", "/updates");
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/api/health") return Response.json({ ok: true });
      if (path === "/api/updates/state") {
        return Response.json({ lastRunAt: Math.floor(Date.now() / 1000), lastStatus: "completed" });
      }
      if (path === "/api/updates") {
        return Response.json([
          {
            id: "update-1",
            seenAt: Math.floor(Date.now() / 1000),
            acknowledged: false,
            manga: {
              id: mangaId,
              sourceId: "source",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: `/api/manga/${mangaId}/cover`,
              inLibrary: true,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            chapter: { id: chapterId, mangaId, chapterNumber: 2, downloaded: false },
          },
        ]);
      }
      if (path === "/api/updates/ack" && init?.method === "POST") {
        return new Response(null, { status: 204 });
      }
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    expect(await screen.findByText("Yosuga no Sora")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Mark read" }));
    expect(await screen.findByRole("heading", { name: "No new chapters" })).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/updates/ack",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ ids: ["update-1"] }) }),
    );
  });

  it("marks a chapter read from manga details", async () => {
    const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
    const chapterId = "0198c0de-7a22-7000-8000-00000000cafe";
    window.history.pushState({}, "", `/manga/${mangaId}`);
    let read = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/api/health") return Response.json({ ok: true });
      if (path === "/api/categories") return Response.json([]);
      if (path === `/api/chapters/${chapterId}/read` && init?.method === "POST") {
        read = true;
        return Response.json({ chapterId, mangaId, read: true });
      }
      if (path === `/api/manga/${mangaId}`) {
        return Response.json({
          manga: {
            id: mangaId,
            sourceId: "source",
            title: "Yosuga no Sora",
            status: "completed",
            coverUrl: `/api/manga/${mangaId}/cover`,
            inLibrary: true,
            downloadFormat: "cbz",
            createdAt: 1,
            updatedAt: 1,
            detailsFetchedAt: 1,
          },
          categories: [],
          chapters: [{ id: chapterId, mangaId, chapterNumber: 1, downloaded: false, read }],
          trackers: [],
        });
      }
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    const mark = await screen.findByRole("button", { name: "Mark read Chapter 1" });
    fireEvent.click(mark);
    expect(
      await screen.findByRole("button", { name: "Mark unread Chapter 1" }),
    ).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      `/api/chapters/${chapterId}/read`,
      expect.objectContaining({ method: "POST", body: JSON.stringify({ read: true }) }),
    );
  });

  it("opens the full recommendations view after loading suggestions", async () => {
    const mangaId = "0198c0de-7a11-7000-8000-00000000beef";
    window.history.pushState({}, "", `/manga/${mangaId}`);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/health") return Response.json({ ok: true });
        if (path === "/api/categories") return Response.json([]);
        if (path === "/api/trackers") return Response.json([]);
        if (path === `/api/manga/${mangaId}/suggestions`)
          return Response.json([{ remoteId: "99", title: "Recommended title", score: 8.5 }]);
        if (path === `/api/manga/${mangaId}`)
          return Response.json({
            manga: {
              id: mangaId,
              sourceId: "source",
              title: "Yosuga no Sora",
              status: "completed",
              coverUrl: "",
              inLibrary: true,
              downloadFormat: "cbz",
              createdAt: 1,
              updatedAt: 1,
            },
            categories: [],
            chapters: [],
            trackers: [
              {
                id: 1,
                mangaId,
                trackerType: "anilist",
                remoteId: "1",
                remoteTitle: "Yosuga no Sora",
              },
            ],
          });
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
    await user.click(screen.getByRole("button", { name: "Close" }));
    await user.click(await screen.findByRole("button", { name: "See recommendations" }));
    expect(await screen.findByText("Recommended title")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "See all recommendations" })).toHaveAttribute(
      "href",
      `/manga/${mangaId}/recommendations`,
    );
  });
});
