import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { MorePage } from "../features/more/MorePage";
import { BottomBar } from "./MobileNav";
import { RESELECT_EVENT, historyResumeTarget, type MobileTabId } from "./nav";

function renderBar(path = "/library", updates = 0, pluginUpdates = 0) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <BottomBar
        onSearch={() => undefined}
        visible
        updates={updates}
        pluginUpdates={pluginUpdates}
      />
    </MemoryRouter>,
  );
}

function listenReselect() {
  const tabs: MobileTabId[] = [];
  const listener = (event: Event) => {
    tabs.push((event as CustomEvent<{ tab: MobileTabId }>).detail.tab);
  };
  window.addEventListener(RESELECT_EVENT, listener);
  return {
    tabs,
    stop: () => window.removeEventListener(RESELECT_EVENT, listener),
  };
}

describe("BottomBar", () => {
  it("renders the five tabs with labels and no badges when counts are zero", () => {
    renderBar();
    for (const label of ["Library", "Browse", "Updates", "History", "More"]) {
      expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
    }
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("shows pending counts on the Updates and More tabs", () => {
    renderBar("/library", 3, 12);
    expect(screen.getByRole("status", { name: "3 pending updates" })).toBeInTheDocument();
    expect(screen.getByRole("status", { name: "12 pending plugin updates" })).toBeInTheDocument();
  });

  it("dispatches a reselect event when the active tab is tapped", () => {
    const seen = listenReselect();
    try {
      renderBar("/library");
      fireEvent.click(screen.getByRole("link", { name: "Library" }));
      expect(seen.tabs).toEqual(["library"]);
    } finally {
      seen.stop();
    }
  });

  it("navigates normally when an inactive tab is tapped", () => {
    const seen = listenReselect();
    try {
      renderBar("/library");
      fireEvent.click(screen.getByRole("link", { name: "Browse" }));
      expect(seen.tabs).toEqual([]);
    } finally {
      seen.stop();
    }
  });
});

describe("historyResumeTarget", () => {
  it("returns the latest readable chapter with its page", () => {
    expect(
      historyResumeTarget([
        { manga: { id: "m1" }, chapter: undefined },
        { manga: { id: "m2" }, chapter: { id: "c9" }, page: 14 },
      ]),
    ).toBe("/reader/m2/c9?page=14");
  });

  it("falls back to the history list when nothing is readable", () => {
    expect(historyResumeTarget([{ manga: { id: "m1" } }])).toBe("/history");
    expect(historyResumeTarget([])).toBe("/history");
  });
});

describe("MorePage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function stubOverview() {
    const calls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        calls.push(`${init?.method ?? "GET"} ${path}`);
        if (path === "/api/incognito") return Response.json({ enabled: false });
        if (path === "/api/download") {
          return Response.json({
            items: [
              {
                id: 1,
                chapterId: "c1",
                status: "DOWNLOADING",
                progress: 1,
                totalPages: 10,
                downloadedPages: 1,
                mangaId: "m1",
                mangaTitle: "Demo",
                sourceName: "Demo",
              },
            ],
            stats: { downloadedPages: 1, retriedRequests: 0, throttledRequests: 0 },
          });
        }
        if (path === "/api/categories") return Response.json([{ id: 2, name: "manga" }]);
        if (path === "/api/updates/state") return Response.json({ lastRunAt: 0, lastStatus: "ok" });
        return Response.json([]);
      }),
    );
    return calls;
  }

  it("renders overview rows with live summaries and toggles incognito", async () => {
    const calls = stubOverview();
    render(
      <MemoryRouter>
        <MorePage />
      </MemoryRouter>,
    );
    expect(await screen.findByRole("link", { name: /Download queue/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Categories/ })).toHaveTextContent("1 categories");
    expect(screen.getByRole("link", { name: /Library update errors/ })).toBeInTheDocument();
    expect(await screen.findByText("1 active")).toBeInTheDocument();
    const toggle = screen.getByRole("switch", { name: /Incognito mode/ });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    fireEvent.click(toggle);
    await waitFor(() => expect(calls).toContain("POST /api/incognito"));
    expect(toggle).toHaveAttribute("aria-checked", "true");
  });
});
