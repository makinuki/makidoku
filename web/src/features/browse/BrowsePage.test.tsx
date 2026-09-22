import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { BrowsePage } from "./BrowsePage";

function source(id: string, name: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    name,
    version: "1.0.0",
    lang: "en",
    nsfw: false,
    iconUrl: "",
    installedAt: 1,
    loaded: true,
    hasClearance: false,
    pinned: false,
    ...extra,
  };
}

describe("BrowsePage plugins tab", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("surfaces the browser clearance badge on protected plugins", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/sources") {
          return Response.json([source("a", "Alpha"), source("b", "Beta", { hasClearance: true })]);
        }
        return Response.json([]);
      }),
    );
    render(
      <MemoryRouter initialEntries={["/browse?tab=plugins"]}>
        <BrowsePage />
      </MemoryRouter>,
    );
    expect(await screen.findByRole("button", { name: "Uninstall Alpha" })).toBeInTheDocument();
    expect(screen.getByLabelText("Beta has browser clearance")).toBeInTheDocument();
    expect(screen.queryByLabelText("Alpha has browser clearance")).not.toBeInTheDocument();
  });
});

describe("BrowsePage sources tab", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function stubSources(calls: string[]) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        calls.push(`${init?.method ?? "GET"} ${path} ${init?.body ?? ""}`);
        if (path === "/api/sources") return Response.json([source("a", "Alpha")]);
        if (path === "/api/settings") return Response.json([]);
        if (path === "/api/sources/a/filters") return Response.json([]);
        if (path.startsWith("/api/sources/a/search")) {
          return Response.json({ page: 1, hasNextPage: false, items: [] });
        }
        if (path === "/api/feeds") return Response.json([]);
        if (path.startsWith("/api/saved-searches") && (init?.method ?? "GET") === "GET") {
          return Response.json([]);
        }
        return Response.json({});
      }),
    );
  }

  it("saves the current single-plugin search", async () => {
    const calls: string[] = [];
    stubSources(calls);
    render(
      <MemoryRouter initialEntries={["/browse"]}>
        <BrowsePage />
      </MemoryRouter>,
    );
    await screen.findByText("Alpha");
    fireEvent.click(screen.getByRole("button", { name: /Alpha/ }));
    const field = await screen.findByPlaceholderText("Search installed plugins");
    fireEvent.change(field, { target: { value: "yosuga" } });
    fireEvent.submit(field.closest("form")!);
    fireEvent.click(await screen.findByRole("button", { name: "Save this search" }));
    const name = screen.getByLabelText("Saved search name");
    expect(name).toHaveValue("yosuga");
    fireEvent.click(screen.getByRole("button", { name: "Save search" }));
    await waitFor(() =>
      expect(calls.some((call) => call.startsWith("POST /api/saved-searches"))).toBe(true),
    );
    const post = calls.find((call) => call.startsWith("POST /api/saved-searches"));
    expect(post).toContain("yosuga");
    expect(await screen.findByText(/Saved "yosuga"/)).toBeInTheDocument();
  });
});

describe("BrowsePage migrate tab", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("sorts library sources by count or name", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/migration/sources") {
          return Response.json([
            { source: source("z", "Zeta"), count: 1, imported: false },
            { source: source("a", "Alpha"), count: 9, imported: false },
          ]);
        }
        return Response.json([]);
      }),
    );
    render(
      <MemoryRouter initialEntries={["/browse?tab=migrate"]}>
        <BrowsePage />
      </MemoryRouter>,
    );
    const alphaFirst = () =>
      Boolean(
        screen
          .getByRole("button", { name: /Alpha/ })
          .compareDocumentPosition(screen.getByRole("button", { name: /Zeta/ })) &
        Node.DOCUMENT_POSITION_FOLLOWING,
      );
    await screen.findByRole("button", { name: /Alpha/ });
    // Default: largest library first.
    expect(alphaFirst()).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "name" }));
    expect(alphaFirst()).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Sort descending" }));
    expect(alphaFirst()).toBe(true);
  });
});
