import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  Bookmark,
  ChevronLeft,
  ChevronRight,
  Download,
  Rss,
  LoaderCircle,
  Pin,
  PinOff,
  Plus,
  RefreshCw,
  Search,
  Settings,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../../api";
import { CoverImg } from "../../components/CoverImg";
import { Modal } from "../../components/Modal";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";
import type {
  CatalogEntry,
  Feed,
  FilterSchema,
  Manga,
  MigrationSource,
  SavedSearch,
  SearchResult,
  SolveResult,
  Source,
} from "../../types";
import { MigrationModal } from "../manga/DetailsPage";
import { SourceSettingsDialog } from "../sources/SourceSettingsDialog";

type Tab = "sources" | "plugins" | "migrate";

// Dispatched after a search is saved so the feeds panel reloads in place.
const FEEDS_REFRESH_EVENT = "makidoku:feeds-refresh";

export function BrowsePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedTab = searchParams.get("tab");
  const [tab, setTab] = useState<Tab>(
    requestedTab === "plugins" || requestedTab === "migrate" ? requestedTab : "sources",
  );
  useEffect(() => {
    if (requestedTab === "plugins" || requestedTab === "migrate") {
      if (requestedTab !== tab) setTab(requestedTab);
      return;
    }
    if (tab !== "sources") setTab("sources");
  }, [requestedTab, tab]);
  const selectTab = (next: Tab) => {
    setTab(next);
    setSearchParams(next === "sources" ? {} : { tab: next }, { replace: true });
  };
  return (
    <div className="mx-auto max-w-7xl p-5 sm:p-8">
      <PageHeader eyebrow="Catalog" title="Browse" />
      <div className="mb-8 flex gap-1 border-b border-zinc-800">
        {(["sources", "plugins", "migrate"] as const).map((item) => (
          <button
            key={item}
            onClick={() => selectTab(item)}
            className={`border-b-2 px-4 py-3 text-sm capitalize ${
              tab === item
                ? "border-amber-400 text-amber-300"
                : "border-transparent text-zinc-500 hover:text-zinc-200"
            }`}
          >
            {item}
          </button>
        ))}
      </div>
      {tab === "sources" ? (
        <SourcesTab onOpenPlugins={() => selectTab("plugins")} />
      ) : tab === "plugins" ? (
        <PluginsTab />
      ) : (
        <MigrateTab />
      )}
    </div>
  );
}

function SourcesTab({ onOpenPlugins }: { onOpenPlugins: () => void }) {
  const [sources, setSources] = useState<Source[]>([]);
  const [selected, setSelected] = useState("all");
  const [draftQuery, setDraftQuery] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [hasNextPage, setHasNextPage] = useState(false);
  const [results, setResults] = useState<Array<SearchResult & { source: Source }>>([]);
  const [filterSchemas, setFilterSchemas] = useState<FilterSchema[]>([]);
  const [filterValues, setFilterValues] = useState<Record<string, unknown>>({});
  const [hideNsfw, setHideNsfw] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [failedSources, setFailedSources] = useState({ failed: 0, total: 0 });
  const [saveOpen, setSaveOpen] = useState(false);
  const [saveName, setSaveName] = useState("");
  const [saveNote, setSaveNote] = useState("");
  const [saving, setSaving] = useState(false);
  const pendingFilters = useRef<Record<string, unknown>>({});

  const loadSources = async () => {
    try {
      setSources(await api.sources());
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load plugins");
    }
  };
  useEffect(() => {
    void loadSources();
    void api
      .settings()
      .then((items) => {
        const setting = items.find((item) => item.key === "browse.hide_nsfw");
        setHideNsfw(setting?.value === true);
      })
      .catch(() => undefined);
  }, []);
  useEffect(() => {
    setPage(1);
    if (selected === "all") {
      setFilterSchemas([]);
      setFilterValues({});
      return;
    }
    let active = true;
    void api
      .sourceFilters(selected)
      .then((schemas) => {
        if (!active) return;
        setFilterSchemas(schemas);
        const restored: Record<string, unknown> = {};
        for (const schema of schemas) {
          if (schema.id in pendingFilters.current) {
            restored[schema.id] = pendingFilters.current[schema.id];
          }
        }
        pendingFilters.current = {};
        setFilterValues({ ...defaultFilterValues(schemas), ...restored });
      })
      .catch(() => {
        if (!active) return;
        setFilterSchemas([]);
        setFilterValues({});
      });
    return () => {
      active = false;
    };
  }, [selected]);
  const visibleSources = useMemo(
    () =>
      sources
        .filter((source) => !hideNsfw || !source.nsfw)
        .sort((a, b) => {
          if (Boolean(a.pinned) !== Boolean(b.pinned)) return a.pinned ? -1 : 1;
          if ((a.lastUsedAt ?? 0) !== (b.lastUsedAt ?? 0))
            return (b.lastUsedAt ?? 0) - (a.lastUsedAt ?? 0);
          return a.name.localeCompare(b.name);
        }),
    [hideNsfw, sources],
  );
  const sourceGroups = useMemo(
    () => [
      { label: "Pinned", items: visibleSources.filter((source) => source.pinned) },
      {
        label: "Last used",
        items: visibleSources.filter((source) => !source.pinned && source.lastUsedAt),
      },
      {
        label: "Other",
        items: visibleSources.filter((source) => !source.pinned && !source.lastUsedAt),
      },
    ],
    [visibleSources],
  );
  useEffect(() => {
    if (!query.trim()) {
      setResults([]);
      setHasNextPage(false);
      setFailedSources({ failed: 0, total: 0 });
      setLoading(false);
      return;
    }
    let active = true;
    setLoading(true);
    setError("");
    const wanted =
      selected === "all"
        ? visibleSources
        : visibleSources.filter((source) => source.id === selected);
    let failed = 0;
    void Promise.all(
      wanted.map(async (source) => {
        try {
          const response = await api.search(
            source.id,
            query,
            page,
            selected === source.id ? filterValues : undefined,
          );
          return {
            items: response.items.map((item) => ({ ...item, source })),
            hasNext: response.hasNextPage,
          };
        } catch {
          failed++;
          return { items: [] as Array<SearchResult & { source: Source }>, hasNext: false };
        }
      }),
    )
      .then((groups) => {
        if (!active) return;
        setFailedSources({ failed, total: wanted.length });
        setResults(groups.flatMap((group) => group.items));
        setHasNextPage(groups.some((group) => group.hasNext));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [filterValues, page, query, selected, visibleSources]);

  const pin = async (source: Source) => {
    try {
      const updated = await api.pinSource(source.id, !source.pinned);
      setSources((items) => items.map((item) => (item.id === updated.id ? updated : item)));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to update plugin pin");
    }
  };

  // openSavedSearch replays a stored search: it selects the source, applies the
  // saved query and hands the stored filter values to the schema loader, which
  // keeps only the ids the plugin still defines.
  const openSavedSearch = (
    sourceId: string,
    savedQuery: string,
    filters: Record<string, unknown>,
  ) => {
    pendingFilters.current = filters;
    setSelected(sourceId);
    setDraftQuery(savedQuery);
    setQuery(savedQuery.trim());
    setPage(1);
  };

  if (sources.length === 0 && !error) {
    return (
      <EmptyState
        title="No plugins installed"
        text="Install a plugin to search a source site from the desktop library."
        action={
          <button
            onClick={onOpenPlugins}
            className="mt-3 rounded-lg bg-amber-400 px-4 py-2 text-sm font-semibold text-zinc-950"
          >
            Install plugins
          </button>
        }
      />
    );
  }

  return (
    <>
      {error && <ErrorState message={error} />}
      <div className="mb-8 space-y-5">
        <button
          onClick={() => setSelected("all")}
          className={`flex min-w-40 items-center gap-3 rounded-xl border px-4 py-3 text-left ${
            selected === "all" ? "border-amber-400 bg-amber-400/10" : "border-zinc-800 bg-zinc-900"
          }`}
        >
          <span className="grid size-9 place-items-center rounded-lg bg-zinc-800">
            <Search size={16} />
          </span>
          <span>
            <b className="block text-sm">All plugins</b>
            <small className="text-zinc-500">{visibleSources.length} available</small>
          </span>
        </button>
        {sourceGroups.map(
          (group) =>
            group.items.length > 0 && (
              <section key={group.label}>
                <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-500">
                  {group.label}
                </h2>
                <div className="flex snap-x gap-3 overflow-x-auto pb-1">
                  {group.items.map((source) => (
                    <div
                      key={source.id}
                      className={`flex min-w-52 snap-start items-center gap-2 rounded-xl border px-3 py-3 ${
                        selected === source.id
                          ? "border-amber-400 bg-amber-400/10"
                          : "border-zinc-800 bg-zinc-900"
                      }`}
                    >
                      <button
                        onClick={() => setSelected(source.id)}
                        className="flex min-w-0 flex-1 items-center gap-3 text-left"
                      >
                        <SourceIcon source={source} />
                        <span className="min-w-0">
                          <b className="block truncate text-sm">{source.name}</b>
                          <small className="text-zinc-500">
                            {source.lang}
                            {source.nsfw ? " Â· 18+" : ""}
                          </small>
                        </span>
                      </button>
                      <button
                        aria-label={source.pinned ? "Unpin source" : "Pin source"}
                        title={`${source.pinned ? "Unpin" : "Pin"} ${source.name}`}
                        onClick={() => void pin(source)}
                        className="flex min-h-11 min-w-11 items-center justify-center rounded-md p-1.5 text-zinc-500 hover:bg-zinc-800 hover:text-amber-300 active:bg-zinc-800 active:text-amber-300"
                      >
                        {source.pinned ? <PinOff size={14} /> : <Pin size={14} />}
                      </button>
                    </div>
                  ))}
                </div>
              </section>
            ),
        )}
      </div>
      <FeedsPanel sources={sources} onOpen={openSavedSearch} />
      <form
        onSubmit={(event) => {
          event.preventDefault();
          setPage(1);
          setQuery(draftQuery.trim());
        }}
        className="mb-6 flex items-center gap-3 rounded-xl border border-zinc-800 bg-zinc-900 px-4 py-3"
      >
        <Search size={18} className="text-zinc-500" />
        <input
          autoFocus
          value={draftQuery}
          onChange={(event) => setDraftQuery(event.target.value)}
          placeholder="Search installed plugins"
          className="min-w-0 flex-1 bg-transparent outline-none"
        />
      </form>
      {selected !== "all" && query.trim() && (
        <div className="mb-6">
          {saveOpen ? (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                const name = saveName.trim() || query.trim();
                setSaving(true);
                setSaveNote("");
                void api
                  .createSavedSearch({
                    sourceId: selected,
                    name,
                    query: query.trim(),
                    filters: JSON.stringify(filterValues),
                  })
                  .then(() => {
                    setSaveOpen(false);
                    setSaveName("");
                    setSaveNote(`Saved "${name}" to feeds and saved searches.`);
                    window.dispatchEvent(new CustomEvent(FEEDS_REFRESH_EVENT));
                  })
                  .catch((e) =>
                    setSaveNote(e instanceof Error ? e.message : "Unable to save this search"),
                  )
                  .finally(() => setSaving(false));
              }}
              className="flex flex-wrap items-center gap-2 rounded-xl border border-zinc-800 bg-zinc-900/60 p-3"
            >
              <input
                value={saveName}
                onChange={(event) => setSaveName(event.target.value)}
                placeholder={query.trim()}
                aria-label="Saved search name"
                className="min-h-11 min-w-0 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 text-base outline-none sm:text-sm"
              />
              <button
                type="submit"
                disabled={saving}
                className="inline-flex min-h-11 items-center gap-2 rounded-lg bg-amber-400 px-4 text-sm font-semibold text-zinc-950 disabled:opacity-50"
              >
                {saving ? "Savingâ€¦" : "Save search"}
              </button>
              <button
                type="button"
                onClick={() => setSaveOpen(false)}
                className="min-h-11 rounded-lg px-3 text-sm text-zinc-400 hover:text-white"
              >
                Cancel
              </button>
            </form>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={() => {
                  setSaveName(query.trim());
                  setSaveNote("");
                  setSaveOpen(true);
                }}
                className="inline-flex min-h-11 items-center gap-2 rounded-lg border border-zinc-700 px-3 text-sm text-zinc-300 hover:border-zinc-500 active:border-zinc-500"
              >
                <Plus size={15} /> Save this search
              </button>
              {saveNote && <p className="text-xs text-emerald-300">{saveNote}</p>}
            </div>
          )}
        </div>
      )}
      {selected !== "all" && filterSchemas.length > 0 && (
        <section className="mb-6 rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
          <h2 className="text-sm font-semibold">Plugin filters</h2>
          <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {filterSchemas.map((schema) => (
              <FilterControl
                key={schema.id}
                schema={schema}
                value={filterValues[schema.id]}
                onChange={(value) => {
                  setPage(1);
                  setFilterValues((current) => ({ ...current, [schema.id]: value }));
                }}
              />
            ))}
          </div>
        </section>
      )}
      {failedSources.failed > 0 && (
        <p role="alert" className="mb-4 text-xs text-red-300">
          {failedSources.failed} of {failedSources.total} plugins failed to respond.
        </p>
      )}
      {loading ? (
        <LoadingState label="Searching plugins" />
      ) : results.length ? (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {results.map((item) => (
              <SourceResult key={`${item.source.id}:${item.id}`} item={item} />
            ))}
          </div>
          <div className="mt-6 flex justify-center gap-2">
            <button
              aria-label="Previous search page"
              disabled={page === 1}
              onClick={() => setPage((value) => Math.max(1, value - 1))}
              className="rounded-lg border border-zinc-700 p-2 disabled:opacity-30"
            >
              <ChevronLeft size={16} />
            </button>
            <span className="px-3 py-2 text-sm text-zinc-500">Page {page}</span>
            <button
              aria-label="Next search page"
              disabled={!hasNextPage}
              onClick={() => setPage((value) => value + 1)}
              className="rounded-lg border border-zinc-700 p-2 disabled:opacity-30"
            >
              <ChevronRight size={16} />
            </button>
          </div>
        </>
      ) : (
        <EmptyState
          title={query ? "No matching titles" : "Search installed plugins"}
          text={query ? "Try another title or plugin." : "Search runs after you press Enter."}
        />
      )}
    </>
  );
}

// FeedsPanel lists the browse feeds a restore carried together with their
// saved searches. A feed whose source is not installed stays visible but its
// searches cannot run; a search replays by selecting the matching plugin.
function FeedsPanel({
  sources,
  onOpen,
}: {
  sources: Source[];
  onOpen: (sourceId: string, query: string, filters: Record<string, unknown>) => void;
}) {
  const [feeds, setFeeds] = useState<Feed[]>([]);
  const [searches, setSearches] = useState<SavedSearch[]>([]);
  const [error, setError] = useState("");

  const load = () => {
    void Promise.all([api.feeds(), api.savedSearches("")])
      .then(([feedList, searchList]) => {
        setFeeds(feedList);
        setSearches(searchList);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "Unable to load feeds"));
  };
  useEffect(load, []);
  // Saving a search from the form below refreshes the panel in place.
  useEffect(() => {
    window.addEventListener(FEEDS_REFRESH_EVENT, load);
    return () => window.removeEventListener(FEEDS_REFRESH_EVENT, load);
  }, []);

  if (error)
    return (
      <p role="alert" className="mb-6 text-xs text-red-300">
        {error}
      </p>
    );
  if (feeds.length === 0 && searches.length === 0) return null;

  const remove = async (searchId: string) => {
    try {
      await api.deleteSavedSearch(searchId);
      setSearches((items) => items.filter((item) => item.id !== searchId));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to remove the saved search");
    }
  };

  const sourceName = (id: string) => sources.find((source) => source.id === id)?.name ?? id;
  const installed = (id: string) => sources.some((source) => source.id === id);
  const sourceIds = Array.from(
    new Set([...feeds.map((feed) => feed.sourceId), ...searches.map((search) => search.sourceId)]),
  );

  const row = (search: SavedSearch) => {
    const available = installed(search.sourceId);
    return (
      <div
        key={search.id}
        className="flex items-center justify-between gap-2 rounded-lg border border-zinc-800 bg-zinc-950/60 px-3 py-2"
      >
        <span className="min-w-0">
          <b className="block truncate text-sm">{search.name}</b>
          <small className="block truncate text-zinc-500">{search.query || "No query"}</small>
        </span>
        <span className="flex shrink-0 items-center gap-1">
          <button
            type="button"
            disabled={!available}
            title={available ? `Search ${sourceName(search.sourceId)}` : "Source not installed"}
            onClick={() => onOpen(search.sourceId, search.query, parseFilters(search.filters))}
            className="rounded-md border border-zinc-700 px-2 py-1 text-xs disabled:opacity-40"
          >
            Open
          </button>
          <button
            type="button"
            aria-label={`Remove saved search ${search.name}`}
            onClick={() => void remove(search.id)}
            className="rounded-md p-1.5 text-zinc-500 hover:bg-zinc-800 hover:text-red-300"
          >
            <Trash2 size={14} />
          </button>
        </span>
      </div>
    );
  };

  return (
    <section className="mb-6 rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
      <h2 className="flex items-center gap-2 text-sm font-semibold">
        <Rss size={16} /> Feeds and saved searches
      </h2>
      <div className="mt-3 space-y-4">
        {sourceIds.map((sourceId) => {
          const entries = searches.filter((search) => search.sourceId === sourceId);
          return (
            <div key={sourceId}>
              <p className="mb-1.5 flex items-center gap-2 text-xs uppercase tracking-wide text-zinc-500">
                <Bookmark size={13} /> {sourceName(sourceId)}
                {!installed(sourceId) && <span className="text-amber-300">not installed</span>}
              </p>
              {entries.length ? (
                <div className="space-y-1.5">{entries.map((search) => row(search))}</div>
              ) : (
                <p className="text-xs text-zinc-600">No saved searches.</p>
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}

function parseFilters(raw: string): Record<string, unknown> {
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    return {};
  }
  return {};
}

function PluginsTab() {
  const [sources, setSources] = useState<Source[]>([]);
  const [catalog, setCatalog] = useState<CatalogEntry[]>([]);
  const [busy, setBusy] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [confirmRemoval, setConfirmRemoval] = useState<Source>();
  const [settingsSource, setSettingsSource] = useState<Source>();
  const [cookieSource, setCookieSource] = useState("");
  const [cookie, setCookie] = useState("");
  const [userAgent, setUserAgent] = useState("");
  const clearanceSection = useRef<HTMLElement | null>(null);
  // solverAvailable is null until it has been answered, which keeps the browser
  // check from being hidden during the first paint. Null must not read as
  // unavailable, because that would offer the paste form before we know better.
  const [solverAvailable, setSolverAvailable] = useState<boolean | null>(null);
  const [solvingSource, setSolvingSource] = useState("");
  const [solveResult, setSolveResult] = useState<SolveResult | null>(null);

  // blockedSources lists the sources refused access outright, where a
  // browser check cannot help.
  const blockedSources = new Set(
    sources
      .filter((s) => s.challenge?.state === "blocked")
      .map((s) => s.id),
  );

  // focusClearanceForm selects a source in the clearance form and scrolls
  // to it. The form is the interim path until a source can be solved in
  // place; the WebView flow reuses the same entry point.
  // Capability is a property of the machine, so it is fetched once and reused for
  // every source rather than asked per source.
  useEffect(() => {
    let live = true;
    void api
      .solverCapability()
      .then((capability) => {
        if (live) setSolverAvailable(capability.available);
      })
      .catch(() => {
        // An unanswered capability means the browser check is not offered, which
        // leaves the paste form as the route rather than showing a control that
        // cannot work.
        if (live) setSolverAvailable(false);
      });
    return () => {
      live = false;
    };
  }, []);

  // checkInBrowser opens a visible window on the source's origin and waits for it
  // to finish. The request is held for the duration, so the checking state is not
  // a spinner over a short call; it is the window being answered.
  const checkInBrowser = (sourceId: string) => {
    setSolvingSource(sourceId);
    setSolveResult(null);
    void run("solve", async () => {
      const result = await api.solve(sourceId);
      setSolveResult(result);
    }, "Browser check finished.").finally(() => setSolvingSource(""));
  };

  const focusClearanceForm = (sourceId: string) => {
    if (blockedSources.has(sourceId)) return;
    setCookieSource(sourceId);
    clearanceSection.current?.scrollIntoView({
      behavior: "smooth",
      block: "center",
    });
  };
  const load = async () => {
    try {
      const [installed, entries, settings] = await Promise.all([
        api.sources(),
        api.catalog(),
        api.settings(),
      ]);
      const hide = settings.find((item) => item.key === "browse.hide_nsfw")?.value === true;
      setSources(installed.filter((source) => !hide || !source.nsfw));
      setCatalog(entries.filter((entry) => !hide || !entry.nsfw));
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load plugins");
    }
  };
  useEffect(() => {
    void load();
  }, []);
  useEffect(() => {
    if (!status) return;
    const timer = window.setTimeout(() => setStatus(""), 4000);
    return () => window.clearTimeout(timer);
  }, [status]);
  const run = async (key: string, action: () => Promise<unknown>, success: string) => {
    setBusy((keys) => [...keys, key]);
    setError("");
    try {
      await action();
      await load();
      setStatus(success);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Plugin action failed");
    } finally {
      setBusy((keys) => keys.filter((item) => item !== key));
    }
  };
  const updates = catalog.filter((entry) => entry.installed && entry.updateAvailable);
  return (
    <div className="space-y-8">
      {error && <ErrorState message={error} />}
      {status && <p className="text-sm text-emerald-300">{status}</p>}
      {updates.length > 0 && (
        <section className="rounded-xl border border-amber-500/30 bg-amber-500/5 p-5">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h2 className="font-semibold">Updates pending</h2>
              <p className="mt-1 text-xs text-zinc-500">
                {updates.length} plugin updates available
              </p>
            </div>
            <button
              onClick={() => void run("all", api.updateAllSources, "Plugin updates installed.")}
              disabled={busy.length > 0}
              className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
            >
              Update all
            </button>
          </div>
        </section>
      )}
      <section>
        <PageHeader title="Installed plugins">
          <button
            onClick={() => void load()}
            aria-label="Refresh plugins"
            className="rounded-lg border border-zinc-700 p-2"
          >
            <RefreshCw size={15} />
          </button>
        </PageHeader>
        <div className="grid gap-3 md:grid-cols-2">
          {sources.map((source) => {
            const update = updates.find((entry) => entry.name === source.name);
            return (
              <div
                key={source.id}
                className="flex items-center gap-3 rounded-xl border border-zinc-800 bg-zinc-900/50 p-4"
              >
                <SourceIcon source={source} />
                <div className="min-w-0 flex-1">
                  <b className="block truncate">{source.name}</b>
                  <small className="text-zinc-500">
                    v{source.version} Â· {source.lang}
                    {source.nsfw ? " Â· 18+" : ""}
                  </small>
                </div>
                {source.challenge ? (
                  <button
                    onClick={() => {
                      // The grid button is the entry point: it selects the source
                      // for the section and starts the check, so the reader does
                      // not have to find the form as well.
                      setCookieSource(source.id);
                      focusClearanceForm(source.id);
                      if (source.challenge?.state !== "blocked" && solverAvailable !== false) {
                        checkInBrowser(source.id);
                      }
                    }}
                    disabled={solvingSource !== ""}
                    title={
                      source.challenge.state === "blocked"
                        ? "This source refused access, and a browser check will not clear it."
                        : `This source is checking your browser (${source.challenge.origins.join(", ")}).`
                    }
                    className="shrink-0 rounded-lg border border-amber-500/40 px-2 py-1.5 text-xs text-amber-300 disabled:opacity-40"
                  >
                    {solvingSource === source.id
                      ? "Checking"
                      : source.challenge.state === "blocked"
                        ? "Blocked"
                        : "Needs check"}
                  </button>
                ) : (
                  source.hasClearance && (
                    <span
                      role="img"
                      aria-label={`${source.name} has browser clearance`}
                      title="Browser clearance active"
                      className="grid size-8 shrink-0 place-items-center rounded-lg border border-emerald-500/40 text-emerald-300"
                    >
                      <ShieldCheck size={15} />
                    </span>
                  )
                )}
                {(source.hasSettings || (source.availableLanguages?.length ?? 0) > 1) && (
                  <button
                    aria-label={`Settings for ${source.name}`}
                    title={`${source.name} settings`}
                    onClick={() => setSettingsSource(source)}
                    className="shrink-0 rounded-lg border border-zinc-700 p-2 text-zinc-300 hover:text-white"
                  >
                    <Settings size={15} />
                  </button>
                )}
                {update && (
                  <button
                    onClick={() =>
                      void run(
                        update.id,
                        () => api.updateSource(source.id),
                        `${source.name} updated.`,
                      )
                    }
                    disabled={busy.includes(update.id)}
                    className="rounded-lg border border-amber-500/40 px-2 py-1.5 text-xs text-amber-300 disabled:opacity-40"
                  >
                    Update
                  </button>
                )}
                <button
                  aria-label={`Uninstall ${source.name}`}
                  onClick={() => setConfirmRemoval(source)}
                  className="rounded-lg p-2 text-red-300 hover:bg-red-950/40"
                >
                  <Trash2 size={15} />
                </button>
              </div>
            );
          })}
        </div>
      </section>
      <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
        <h2 className="font-semibold">Available plugins</h2>
        <div className="mt-4 grid gap-2">
          {catalog
            .filter((entry) => !entry.installed)
            .map((entry) => (
              <div
                key={entry.id}
                className="flex items-center gap-3 rounded-lg border border-zinc-800 p-3"
              >
                <div className="min-w-0 flex-1">
                  <b className="block">{entry.name}</b>
                  <small className="text-zinc-500">
                    v{entry.version} Â· {entry.lang} Â·{" "}
                    {entry.compatible ? "Compatible" : entry.incompatibility}
                  </small>
                </div>
                <button
                  onClick={() =>
                    void run(
                      entry.id,
                      () => api.installSource(entry.id),
                      `${entry.name} installed.`,
                    )
                  }
                  disabled={!entry.compatible || busy.includes(entry.id)}
                  className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-xs font-semibold text-zinc-950 disabled:opacity-40"
                >
                  {busy.includes(entry.id) ? (
                    <LoaderCircle size={13} className="animate-spin" />
                  ) : (
                    <Download size={13} />
                  )}
                  {busy.includes(entry.id) ? "Installingâ€¦" : "Install"}
                </button>
              </div>
            ))}
        </div>
      </section>
      <section
        ref={clearanceSection}
        className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5"
      >
        <div className="flex items-center gap-2">
          <ShieldCheck size={17} className="text-amber-300" />
          <h2 className="font-semibold">Browser clearance</h2>
        </div>
        {solverAvailable !== false ? (
          <>
            <p className="mt-1 text-xs text-zinc-500">
              A window opens on the site and answers the check for you. It may clear on its own
              or wait for a click.
            </p>
            <div className="mt-4 grid gap-2 sm:grid-cols-3">
              <select
                value={cookieSource}
                onChange={(event) => setCookieSource(event.target.value)}
                aria-label="Plugin"
                className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
              >
                <option value="">Plugin</option>
                {sources.map((source) => (
                  <option key={source.id} value={source.id}>
                    {source.name}
                  </option>
                ))}
              </select>
              <button
                disabled={!cookieSource || solvingSource !== ""}
                onClick={() => cookieSource && checkInBrowser(cookieSource)}
                className="rounded-lg border border-amber-500/40 px-3 py-2 text-sm text-amber-300 disabled:opacity-40 sm:col-span-2"
              >
                {solvingSource ? "Waiting for the site..." : "Check in browser"}
              </button>
            </div>
            {solveResult && (
              <div className="mt-3 rounded-lg border border-zinc-800 bg-zinc-950/60 p-3 text-xs">
                <div className="flex flex-wrap items-center gap-2">
                  <span
                    className={
                      solveResult.verified
                        ? "text-emerald-300"
                        : solveResult.challenge
                          ? "text-amber-300"
                          : "text-zinc-400"
                    }
                  >
                    {solveResult.verified
                      ? "Cleared"
                      : solveResult.captured
                        ? "Stored, not accepted"
                        : solveResult.needsInteraction
                          ? "Waiting for you"
                          : "No check needed"}
                  </span>
                  <span className="text-zinc-500">{solveResult.message}</span>
                </div>
                {solveResult.cookies.length > 0 && (
                  <p className="mt-1 text-zinc-500">
                    Stored {solveResult.cookies.join(", ")}
                  </p>
                )}
              </div>
            )}
          </>
        ) : (
          <>
            <p className="mt-1 text-xs text-zinc-500">
              The browser check is unavailable on this machine. Paste the cookie and user agent
              from a browser session where you have passed the check. The app applies them to
              every later request for that source.
            </p>
            <div className="mt-4 grid gap-2 sm:grid-cols-3">
              <select
                value={cookieSource}
                onChange={(event) => setCookieSource(event.target.value)}
                aria-label="Plugin"
                className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
              >
                <option value="">Plugin</option>
                {sources.map((source) => (
                  <option key={source.id} value={source.id}>
                    {source.name}
                  </option>
                ))}
              </select>
              <input
                value={cookie}
                onChange={(event) => setCookie(event.target.value)}
                placeholder="Clearance cookie"
                className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
              />
              <input
                value={userAgent}
                onChange={(event) => setUserAgent(event.target.value)}
                placeholder="Browser user agent"
                className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
              />
            </div>
            <button
              disabled={!cookieSource || !cookie || !userAgent || busy.includes("clearance")}
              onClick={() =>
                void run(
                  "clearance",
                  async () => {
                    await api.submitClearance(cookieSource, cookie, userAgent);
                    setCookie("");
                    setUserAgent("");
                  },
                  "Browser session saved.",
                )
              }
              className="mt-3 rounded-lg border border-zinc-700 px-3 py-2 text-sm disabled:opacity-40"
            >
              Save browser session
            </button>
          </>
        )}
      </section>
      {confirmRemoval && (
        <Modal title="Remove plugin" onClose={() => setConfirmRemoval(undefined)}>
          <p className="text-sm text-zinc-400">
            Remove {confirmRemoval.name}? Library records and downloaded chapters remain available.
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <button
              onClick={() => setConfirmRemoval(undefined)}
              className="rounded-lg border border-zinc-700 px-3 py-2 text-sm"
            >
              Cancel
            </button>
            <button
              onClick={() => {
                const source = confirmRemoval;
                setConfirmRemoval(undefined);
                void run(
                  source.id,
                  () => api.uninstallSource(source.id),
                  `${source.name} removed.`,
                );
              }}
              className="rounded-lg bg-red-500 px-3 py-2 text-sm font-semibold text-white"
            >
              Remove
            </button>
          </div>
        </Modal>
      )}
      {settingsSource && (
        <SourceSettingsDialog source={settingsSource} onClose={() => setSettingsSource(undefined)} />
      )}
    </div>
  );
}

function MigrateTab() {
  const navigate = useNavigate();
  const [sources, setSources] = useState<MigrationSource[]>([]);
  const [selectedSource, setSelectedSource] = useState<string>();
  const [manga, setManga] = useState<Manga[]>([]);
  const [query, setQuery] = useState("");
  const [selectedManga, setSelectedManga] = useState<Manga>();
  const [sort, setSort] = useState<"name" | "count">("count");
  const [ascending, setAscending] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    void api
      .migrationSources()
      .then(setSources)
      .catch((e) => setError(e instanceof Error ? e.message : "Unable to load migration sources"));
  }, []);
  useEffect(() => {
    if (!selectedSource) {
      setManga([]);
      return;
    }
    void api
      .migrationSourceManga(selectedSource)
      .then(setManga)
      .catch((e) => setError(e instanceof Error ? e.message : "Unable to load library titles"));
  }, [selectedSource]);
  const visible = manga.filter((item) =>
    item.title.toLowerCase().includes(query.trim().toLowerCase()),
  );
  const ordered = [...sources].sort((a, b) => {
    const order = sort === "count" ? a.count - b.count : a.source.name.localeCompare(b.source.name);
    return ascending ? order : -order;
  });
  return (
    <div className="grid gap-6 lg:grid-cols-[18rem_1fr]">
      {error && (
        <div className="lg:col-span-2">
          <ErrorState message={error} />
        </div>
      )}
      <aside className="rounded-xl border border-zinc-800 bg-zinc-900/50 p-3">
        <h2 className="px-2 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500">
          Library sources
        </h2>
        <div className="mb-2 flex items-center gap-1 px-2" role="group" aria-label="Sort sources">
          {(["count", "name"] as const).map((mode) => (
            <button
              key={mode}
              type="button"
              aria-pressed={sort === mode}
              onClick={() => setSort(mode)}
              className={`min-h-11 rounded-lg px-3 text-xs font-medium capitalize ${
                sort === mode ? "bg-zinc-800 text-white" : "text-zinc-400 hover:text-white"
              }`}
            >
              {mode}
            </button>
          ))}
          <button
            type="button"
            aria-label={ascending ? "Sort ascending" : "Sort descending"}
            onClick={() => setAscending((value) => !value)}
            className="flex min-h-11 min-w-11 items-center justify-center rounded-lg text-zinc-400 hover:bg-zinc-800 hover:text-white"
          >
            {ascending ? <ArrowUp size={15} /> : <ArrowDown size={15} />}
          </button>
        </div>
        <div className="mt-1 max-h-72 space-y-1 overflow-y-auto lg:max-h-none">
          {ordered.map((item) => (
            <button
              key={item.source.id}
              onClick={() => setSelectedSource(item.source.id)}
              className={`flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left ${
                selectedSource === item.source.id
                  ? "bg-amber-400/10 text-amber-200"
                  : "hover:bg-zinc-800"
              }`}
            >
              <SourceIcon source={item.source} />
              <span className="min-w-0 flex-1 truncate text-sm">
                {item.source.name}
                {item.imported && (
                  <small className="ml-1 rounded bg-zinc-800 px-1 text-[10px] text-amber-300">
                    imported
                  </small>
                )}
              </span>
              <span className="rounded-full bg-zinc-800 px-2 py-0.5 text-xs text-zinc-400">
                {item.count}
              </span>
            </button>
          ))}
        </div>
      </aside>
      <section>
        <div className="mb-4 flex items-center gap-2 rounded-xl border border-zinc-800 bg-zinc-900 px-3 py-2">
          <Search size={16} className="text-zinc-500" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Filter library titles"
            className="min-w-0 flex-1 bg-transparent text-sm outline-none"
          />
        </div>
        {selectedSource ? (
          visible.length ? (
            <div className="grid gap-3 sm:grid-cols-2">
              {visible.map((item) => (
                <button
                  key={item.id}
                  onClick={() => setSelectedManga(item)}
                  className="flex items-center gap-3 rounded-xl border border-zinc-800 bg-zinc-900/50 p-3 text-left hover:border-amber-500/50"
                >
                  <CoverImg src={item.coverUrl} className="h-20 w-14 rounded-md object-cover" />
                  <span className="min-w-0">
                    <b className="line-clamp-2 text-sm">{item.title}</b>
                    <small className="mt-1 block text-zinc-500">Choose replacement source</small>
                  </span>
                </button>
              ))}
            </div>
          ) : (
            <EmptyState title="No titles" text="No library titles match this filter." />
          )
        ) : (
          <EmptyState
            title="Choose a source"
            text="Select the current source of a library title to begin migration."
          />
        )}
      </section>
      {selectedManga && (
        <MigrationModal
          manga={selectedManga}
          onClose={() => setSelectedManga(undefined)}
          onApplied={(mangaId) => {
            setSelectedManga(undefined);
            navigate(`/manga/${encodeURIComponent(mangaId)}`);
          }}
        />
      )}
    </div>
  );
}

function SourceIcon({ source }: { source: Source }) {
  const [failed, setFailed] = useState(false);
  return source.iconUrl && !failed ? (
    <img
      src={api.sourceIcon(source.id)}
      alt=""
      onError={() => setFailed(true)}
      className="size-9 shrink-0 rounded-lg object-cover"
    />
  ) : (
    <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-zinc-800 text-xs font-bold">
      {source.name.slice(0, 2).toUpperCase()}
    </span>
  );
}

function defaultFilterValues(schemas: FilterSchema[]) {
  const values: Record<string, unknown> = {};
  for (const schema of schemas) {
    if (schema.type === "tri_state") {
      if (schema.default) values[schema.id] = schema.default;
    } else if (schema.default !== undefined && schema.default !== "") {
      values[schema.id] = schema.default;
    }
  }
  return values;
}

function FilterControl({
  schema,
  value,
  onChange,
}: {
  schema: FilterSchema;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  if (schema.type === "checkbox") {
    return (
      <label className="flex items-center gap-2 text-sm text-zinc-300">
        <input
          type="checkbox"
          checked={Boolean(value)}
          onChange={(event) => onChange(event.target.checked)}
          className="accent-amber-400"
        />
        {schema.title}
      </label>
    );
  }
  if (schema.type === "select") {
    return (
      <label className="grid gap-1 text-xs text-zinc-400">
        <span>{schema.title}</span>
        <select
          aria-label={schema.title}
          value={typeof value === "string" ? value : ""}
          onChange={(event) => onChange(event.target.value)}
          className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base text-zinc-100 sm:text-sm"
        >
          {schema.options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>
    );
  }
  if (schema.type === "tri_state") {
    const selected = typeof value === "object" && value ? (value as Record<string, string>) : {};
    return (
      <fieldset className="grid gap-2 text-xs text-zinc-400">
        <legend>{schema.title}</legend>
        {schema.options.map((option) => (
          <label key={option.value} className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate">{option.label}</span>
            <select
              aria-label={`${schema.title}: ${option.label}`}
              value={selected[option.value] || ""}
              onChange={(event) => {
                const next = { ...selected };
                if (event.target.value) next[option.value] = event.target.value;
                else delete next[option.value];
                onChange(next);
              }}
              className="rounded-lg border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-base text-zinc-100 sm:text-xs"
            >
              <option value="">Any</option>
              <option value="+">Include</option>
              <option value="-">Exclude</option>
            </select>
          </label>
        ))}
      </fieldset>
    );
  }
  return (
    <label className="grid gap-1 text-xs text-zinc-400">
      <span>{schema.title}</span>
      <input
        aria-label={schema.title}
        value={typeof value === "string" ? value : ""}
        placeholder={schema.placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base text-zinc-100 sm:text-sm"
      />
    </label>
  );
}

function SourceResult({ item }: { item: SearchResult & { source: Source } }) {
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");
  const save = async () => {
    setBusy(true);
    setError("");
    try {
      await api.saveManga(item.id);
      setSaved(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save title");
    } finally {
      setBusy(false);
    }
  };
  return (
    <article className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900">
      <Link to={`/manga/${encodeURIComponent(item.id)}`} className="block aspect-3/4 bg-zinc-800">
        <CoverImg src={item.coverUrl} className="size-full object-cover" />
      </Link>
      <div className="space-y-2 p-4">
        <p className="text-xs uppercase tracking-wide text-amber-400">{item.source.name}</p>
        <h2 className="line-clamp-2 font-semibold">
          <Link to={`/manga/${encodeURIComponent(item.id)}`} className="hover:text-amber-300">
            {item.title}
          </Link>
        </h2>
        <p className="text-xs text-zinc-500">{item.latestChapter || "No chapter metadata"}</p>
        {error && <p className="text-xs text-red-300">{error}</p>}
        <button
          disabled={busy || saved}
          onClick={() => void save()}
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-50"
        >
          <Plus size={15} /> {busy ? "Saving" : saved ? "Added" : "Save title"}
        </button>
      </div>
    </article>
  );
}
