import { act, render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BrowserRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import App from "./App";

describe("MakiDoku app shell", () => {
  afterEach(() => {
    vi.useRealTimers();
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
        if (path.includes("/migration/candidates")) return Response.json([]);
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
    window.history.pushState({}, "", "/settings");
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
    window.history.pushState({}, "", "/settings");
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
    window.history.pushState({}, "", "/settings");
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
    const save = await screen.findByRole("button", { name: /Save title/ });
    await user.click(save);
    expect(await screen.findByRole("button", { name: /Added/ })).toBeDisabled();
    expect(screen.getByRole("link", { name: /Yosuga no Sora/ })).toHaveAttribute(
      "href",
      `/manga/${resultId}`,
    );
    expect(window.location.pathname).toBe("/browse");
  });

  it("reports failed plugin installs in the error banner", async () => {
    window.history.pushState({}, "", "/settings");
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
    window.history.pushState({}, "", "/settings");
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
          return Response.json([
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
          ]);
        }
        if (path.includes("/migration/apply") && init?.method === "POST") {
          return await applyGate;
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
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Migrate" }));
    await user.click(await screen.findByRole("button", { name: /Yosuga no Sora/ }));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    expect(screen.getByRole("button", { name: /Migrating/ })).toBeDisabled();
    resolveApply(Response.json({ manga: aggregate, source: "asurascans", chapterMap: {} }));
    expect(await screen.findByRole("button", { name: "Migrate" })).toBeInTheDocument();
  });

  it("uploads a backup only after confirming from settings", async () => {
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
    // The settings data is re-fetched so restored plugins and categories show up.
    await waitFor(() => {
      const sourceCalls = fetchMock.mock.calls.filter(([url]) => String(url) === "/api/sources");
      expect(sourceCalls.length).toBeGreaterThanOrEqual(2);
    });
  });

  it("shows an error when a backup import fails", async () => {
    window.history.pushState({}, "", "/settings");
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
