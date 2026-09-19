import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useParams, useSearchParams, useNavigate, Navigate } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  ArrowLeft,
  Check,
  ChevronLeft,
  ChevronRight,
  EyeOff,
  List,
  Maximize,
  Menu,
  Minimize,
  SlidersHorizontal,
  X,
} from "lucide-react";
import { api } from "../../api";
import type { Aggregate, Chapter, Page } from "../../types";
import { ErrorState, EmptyState, LoadingState } from "../../components/States";
import { chapterLabelFor, prevNextChapter } from "./engine/chapters";
import {
  alignToSpread,
  defaultReaderGlobals,
  emptyReaderOverrides,
  readerGlobalsFromSettings,
  readerOverridesFromManga,
  resolveReaderSettings,
  spreadStep,
  type ReaderDirection as Direction,
  type ReaderFit as Fit,
  type ReaderGlobals,
  type ReaderMode as Mode,
  type ReaderOverrides,
} from "./readerSettings";

export function ReaderPage() {
  const { mangaId: routeManga, chapterId: routeChapter } = useParams();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  let mangaId = "";
  let chapterId = "";
  let canonicalize = false;
  if (routeManga && routeChapter) {
    mangaId = decodeURIComponent(routeManga);
    chapterId = decodeURIComponent(routeChapter);
  } else {
    const mangaParam = searchParams.get("manga") || "";
    const chapterParam = searchParams.get("chapter") || "";
    mangaId = mangaParam;
    chapterId = chapterParam;
    // Legacy query URLs redirect to the canonical path form below so only
    // one reader URL scheme stays in use.
    if (mangaParam && chapterParam) canonicalize = true;
  }

  const [aggregate, setAggregate] = useState<Aggregate>();
  const [pages, setPages] = useState<Page[]>([]);
  const [mode, setMode] = useState<Mode>("single");
  const [direction, setDirection] = useState<Direction>("ltr");
  const [fit, setFit] = useState<Fit>("width");
  const [index, setIndex] = useState(0);
  const [menu, setMenu] = useState(true);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const sessionStartedAt = useRef(Date.now());
  const [overrides, setOverrides] = useState<ReaderOverrides>(emptyReaderOverrides);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [incognito, setIncognito] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [goToOpen, setGoToOpen] = useState(false);
  const [dismissedNextUp, setDismissedNextUp] = useState(false);
  const [autoAdvance, setAutoAdvance] = useState(() => {
    try {
      return window.localStorage.getItem("reader.auto_advance") === "1";
    } catch {
      return false;
    }
  });
  const globals = useRef<ReaderGlobals>(defaultReaderGlobals);
  const overridesRef = useRef<ReaderOverrides>(emptyReaderOverrides);
  const readerRef = useRef<HTMLDivElement>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);

  // Leaving the reader always lands on a real page: the title details when a
  // manga is open, otherwise the library. A bare history step would strand a
  // direct open or a new tab with nowhere to go.
  const exitReader = useCallback(() => {
    if (mangaId) navigate(`/manga/${encodeURIComponent(mangaId)}`);
    else navigate("/library");
  }, [mangaId, navigate]);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => {
        // Leaving fullscreen is best-effort; the chrome stays usable.
      });
      return;
    }
    void readerRef.current?.requestFullscreen().catch(() => {
      // Fullscreen may be unavailable (iframe permissions, headless test);
      // the reader remains fully usable inline.
    });
  }, []);
  useEffect(() => {
    const sync = () => setIsFullscreen(document.fullscreenElement != null);
    document.addEventListener("fullscreenchange", sync);
    return () => document.removeEventListener("fullscreenchange", sync);
  }, []);
  const incognitoRef = useRef(false);
  useEffect(() => {
    incognitoRef.current = incognito;
  }, [incognito]);

  // Chapter flow in reading order (oldest-first), derived from the loaded
  // aggregate. Neighbors drive the drawer highlight, prev/next jumps, the
  // end-of-chapter card, and next-chapter prefetch.
  const flow = useMemo(
    () => prevNextChapter(chapterId, aggregate?.chapters ?? []),
    [chapterId, aggregate],
  );
  const goChapter = useCallback(
    (id: string) => {
      setDrawerOpen(false);
      setGoToOpen(false);
      setDismissedNextUp(false);
      navigate(`/reader/${encodeURIComponent(mangaId)}/${encodeURIComponent(id)}`);
    },
    [mangaId, navigate],
  );
  const setAutoAdvanceStored = useCallback((value: boolean) => {
    setAutoAdvance(value);
    try {
      window.localStorage.setItem("reader.auto_advance", value ? "1" : "0");
    } catch {
      // Private browsing may refuse storage; the session value still applies.
    }
  }, []);

  // The effective reader settings are the per-title override on top of the
  // global preference; see readerSettings.ts for the resolution rules.
  const applyReaderSettings = useCallback(() => {
    const resolved = resolveReaderSettings(globals.current, overridesRef.current);
    setMode(resolved.mode);
    setDirection(resolved.direction);
    setFit(resolved.fit);
  }, []);
  useEffect(() => {
    let active = true;
    api
      .incognito()
      .then((state) => active && setIncognito(state.enabled))
      .catch(() => {
        // Incognito state is optional; the badge stays hidden if the read fails.
      });
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => {
    let active = true;
    api
      .settings()
      .then((items) => {
        if (!active) return;
        globals.current = readerGlobalsFromSettings(items);
        applyReaderSettings();
      })
      .catch(() => {
        // Reader defaults are optional; the local default remains usable offline.
      });
    return () => {
      active = false;
    };
  }, [applyReaderSettings]);
  useEffect(() => {
    if (!aggregate) return;
    overridesRef.current = readerOverridesFromManga(aggregate.manga);
    setOverrides(overridesRef.current);
    applyReaderSettings();
  }, [aggregate, applyReaderSettings]);
  const saveReaderOverride = useCallback(
    (patch: Partial<ReaderOverrides>) => {
      if (!mangaId) return;
      const next = { ...overridesRef.current, ...patch };
      overridesRef.current = next;
      setOverrides(next);
      applyReaderSettings();
      void api.setMangaReaderOverrides(mangaId, next).catch((err) => {
        console.error("saving reader overrides failed", err);
      });
    },
    [mangaId, applyReaderSettings],
  );
  useEffect(() => {
    if (!mangaId || !chapterId) {
      setLoading(false);
      return;
    }
    let active = true;
    setLoading(true);
    setError("");
    setPages([]);
    setDismissedNextUp(false);
    // The deep-link page is read once per chapter load: search params are
    // stable for the session, and later navigations re-run this effect.
    const deepLink = (() => {
      const raw = Number(searchParams.get("page"));
      return Number.isInteger(raw) && raw >= 1 ? raw : null;
    })();
    (async () => {
      try {
        const data = await api.manga(mangaId);
        const chapter = data.chapters.find((item) => item.id === chapterId);
        if (!chapter) throw new Error("Chapter is not available");
        const loaded = await api.pages(chapter.id);
        if (!active) return;
        setAggregate(data);
        setPages(loaded);
        sessionStartedAt.current = Date.now();
        // Resume from the resolved title settings, not the `mode` state: the
        // global and per-title preferences may still be loading when the
        // chapter fetch wins the race, and the stale mount-time default would
        // misalign the opening spread in double mode.
        const effective = resolveReaderSettings(
          globals.current,
          readerOverridesFromManga(data.manga),
        );
        const saved = resumeIndex(
          data.progress?.lastReadChapterId === chapterId
            ? (data.progress?.lastReadPage ?? null)
            : null,
          loaded.length,
          effective.mode,
        );
        setIndex(
          deepLink != null ? alignToSpread(deepLink - 1, loaded.length, effective.mode) : saved,
        );
      } catch (e) {
        if (active) setError(e instanceof Error ? e.message : "Unable to open chapter");
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [mangaId, chapterId, attempt]);
  type PendingProgress = {
    mangaId: string;
    chapterId: string;
    page: number;
    total: number;
    complete: boolean;
    sessionSeconds: number;
  };
  const saver = useRef<(() => void) | null>(null);
  const retryQueue = useRef<PendingProgress[]>([]);
  const postProgress = useCallback((entry: PendingProgress) => {
    // Incognito never leaves the device: the daemon also null-writes, but
    // the frontend must not emit the request in the first place.
    if (incognitoRef.current) return Promise.resolve();
    return api
      .progress(
        entry.mangaId,
        entry.chapterId,
        entry.page,
        entry.total,
        entry.complete,
        entry.sessionSeconds,
      )
      .catch((e) => {
        // Offline or transient failure queues for the next tick instead of
        // dropping the position; the queue is bounded and oldest-first.
        retryQueue.current = [...retryQueue.current, entry].slice(-5);
        console.error("saving reading progress failed", e);
      });
  }, []);
  const flushProgress = useCallback(() => {
    const queued = retryQueue.current;
    retryQueue.current = [];
    return (async () => {
      for (const entry of queued) await postProgress(entry);
      if (saver.current) {
        const save = saver.current;
        saver.current = null;
        save();
      }
    })();
  }, [postProgress]);
  useEffect(() => {
    if (!pages.length || !aggregate) return;
    const visibleEnd = Math.min(pages.length, index + spreadStep(mode));
    const save = () => {
      saver.current = null;
      const elapsed = Math.floor((Date.now() - sessionStartedAt.current) / 1000);
      sessionStartedAt.current = Date.now();
      void postProgress({
        mangaId,
        chapterId,
        page: visibleEnd,
        total: pages.length,
        complete: visibleEnd >= pages.length,
        sessionSeconds: Math.min(300, Math.max(0, elapsed)),
      });
    };
    saver.current = save;
    const timer = window.setTimeout(save, 500);
    return () => window.clearTimeout(timer);
  }, [index, mode, pages.length, aggregate, mangaId, chapterId, postProgress]);
  useEffect(() => {
    // A pending write is flushed when leaving the reader, switching
    // chapters, hiding the tab, or closing the page so the debounce window
    // cannot lose the final position.
    const flush = () => {
      void flushProgress();
    };
    window.addEventListener("pagehide", flush);
    document.addEventListener("visibilitychange", flush);
    window.addEventListener("online", flush);
    return () => {
      window.removeEventListener("pagehide", flush);
      document.removeEventListener("visibilitychange", flush);
      window.removeEventListener("online", flush);
      if (saver.current) {
        saver.current();
        saver.current = null;
      }
    };
  }, [mangaId, chapterId, flushProgress]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      // Shortcuts must not fire while a form control has focus: typing into
      // another field or stepping the page slider must stay local to it.
      // Buttons are left alone: arrows never activate a focused button, so
      // chevron focus must not trap page navigation.
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable)
      ) {
        return;
      }
      if (event.key.toLowerCase() === "m") setMenu((value) => !value);
      if (event.key.toLowerCase() === "f") toggleFullscreen();
      if (event.key === "Escape") {
        // Close the topmost layer first: go-to, then settings, then drawer.
        if (goToOpen) setGoToOpen(false);
        else if (settingsOpen) setSettingsOpen(false);
        else if (drawerOpen) setDrawerOpen(false);
        return;
      }
      if (event.key.toLowerCase() === "g") {
        setGoToOpen((value) => !value);
        return;
      }
      if (event.key.toLowerCase() === "n" && flow.next) {
        goChapter(flow.next.id);
        return;
      }
      if (event.key.toLowerCase() === "p" && flow.prev) {
        goChapter(flow.prev.id);
        return;
      }
      if (event.key.toLowerCase() === "w") {
        // The header quick-switcher and the `w` key share one path: both
        // persist the per-title override, so a late settings fetch can never
        // clobber a mode the reader just chose.
        const next = mode === "single" ? "double" : mode === "double" ? "webtoon" : "single";
        if (mangaId) saveReaderOverride({ mode: next });
        else setMode(next);
      }
      const step = spreadStep(mode);
      const forward = direction === "ltr" ? "ArrowRight" : "ArrowLeft";
      const backward = direction === "ltr" ? "ArrowLeft" : "ArrowRight";
      if (event.key === forward || event.key.toLowerCase() === "d")
        setIndex((value) => alignToSpread(value + step, pages.length, mode));
      if (event.key === backward || event.key.toLowerCase() === "a")
        setIndex((value) => alignToSpread(value - step, pages.length, mode));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [
    direction,
    drawerOpen,
    flow.next,
    flow.prev,
    goChapter,
    goToOpen,
    mangaId,
    mode,
    pages.length,
    saveReaderOverride,
    settingsOpen,
    toggleFullscreen,
  ]);
  // The last spread is fully visible: paged modes show 1-2 pages, webtoon
  // tracks the first visible page, so its end is the final page on screen.
  const atEnd =
    pages.length > 0 &&
    (mode === "webtoon" ? index >= pages.length - 1 : index + spreadStep(mode) >= pages.length);
  const visibleCount = Math.min(pages.length, index + (mode === "webtoon" ? 1 : spreadStep(mode)));
  const percent = pages.length ? Math.round((visibleCount / pages.length) * 100) : 0;
  useEffect(() => {
    // Warm the HTTP cache for the next chapter while the reader finishes
    // this one so the jump rarely shows a loading screen.
    if (atEnd && flow.next) void api.pages(flow.next.id).catch(() => {});
  }, [atEnd, flow.next]);
  // Auto-advance counts down on the end card, then jumps. Any navigation
  // away or an explicit stay cancels it via the effect cleanup.
  useEffect(() => {
    if (!atEnd || !flow.next || dismissedNextUp || !autoAdvance) return;
    const timer = window.setTimeout(() => goChapter(flow.next!.id), 5000);
    return () => window.clearTimeout(timer);
  }, [atEnd, flow.next, dismissedNextUp, autoAdvance, goChapter]);
  if (canonicalize) {
    return (
      <Navigate
        to={`/reader/${encodeURIComponent(mangaId)}/${encodeURIComponent(chapterId)}`}
        replace
      />
    );
  }
  if (error)
    return (
      <div className="grid min-h-screen place-items-center bg-zinc-950 p-5">
        <div className="max-w-lg">
          <ErrorState message={error} />
          <div className="mt-4 flex gap-2">
            <button
              onClick={exitReader}
              className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm"
            >
              <ArrowLeft size={15} /> Back
            </button>
            <button
              onClick={() => setAttempt((value) => value + 1)}
              className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
            >
              Retry
            </button>
          </div>
        </div>
      </div>
    );
  if (!mangaId || !chapterId)
    return (
      <div className="grid min-h-screen place-items-center bg-zinc-950 p-5">
        <div className="max-w-lg">
          <EmptyState
            title="No chapter selected"
            text="Open a chapter from a title's details page to start reading."
          />
          <button
            onClick={exitReader}
            className="mt-4 inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm"
          >
            <ArrowLeft size={15} /> Back to library
          </button>
        </div>
      </div>
    );
  if (loading)
    return (
      <div className="grid min-h-screen place-items-center bg-zinc-950">
        <LoadingState label="Loading reader" />
      </div>
    );
  if (aggregate && !pages.length)
    return (
      <div className="grid min-h-screen place-items-center bg-zinc-950 p-5">
        <div className="max-w-lg">
          <EmptyState
            title="This chapter has no pages"
            text="The plugin returned an empty page list. Try refreshing the title from its details page."
          />
          <button
            onClick={exitReader}
            className="mt-4 inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm"
          >
            <ArrowLeft size={15} /> Back
          </button>
        </div>
      </div>
    );
  if (!aggregate || !pages.length)
    return (
      <div className="grid min-h-screen place-items-center bg-zinc-950">
        <LoadingState label="Loading reader" />
      </div>
    );
  return (
    <div ref={readerRef} className="fixed inset-0 z-30 flex flex-col bg-black">
      <div
        className={`flex items-center gap-3 border-b border-zinc-800 bg-zinc-950 px-3 py-2 ${menu ? "" : "hidden"}`}
      >
        <button
          aria-label={mangaId ? "Back to details" : "Back to library"}
          title={mangaId ? "Back to details" : "Back to library"}
          onClick={exitReader}
          className="rounded-lg p-2 text-zinc-300 hover:bg-zinc-800"
        >
          <ArrowLeft size={18} />
        </button>
        <div className="min-w-0 flex-1">
          <b className="block truncate text-sm">{aggregate.manga.title}</b>
          <small className="text-zinc-500">{chapterLabel(aggregate, chapterId)}</small>
          {incognito && (
            <span className="mt-0.5 inline-flex items-center gap-1 rounded-full bg-amber-400/15 px-2 py-0.5 text-[11px] text-amber-300">
              <EyeOff size={12} /> Incognito
            </span>
          )}
        </div>
        <div className="flex gap-1" role="group" aria-label="Reader mode">
          <button
            aria-label="Chapters"
            title="Chapters"
            onClick={() => setDrawerOpen((value) => !value)}
            className={`rounded-lg p-2 ${drawerOpen ? "bg-zinc-800 text-amber-400" : "text-zinc-400 hover:bg-zinc-800"}`}
          >
            <List size={16} />
          </button>
          <button
            aria-pressed={mode === "single"}
            onClick={() => saveReaderOverride({ mode: "single" })}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "single" ? "bg-amber-400 text-zinc-950" : "text-zinc-400"}`}
          >
            Single
          </button>
          <button
            aria-pressed={mode === "double"}
            onClick={() => saveReaderOverride({ mode: "double" })}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "double" ? "bg-amber-400 text-zinc-950" : "text-zinc-400"}`}
          >
            Double
          </button>
          <button
            aria-pressed={mode === "webtoon"}
            onClick={() => saveReaderOverride({ mode: "webtoon" })}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "webtoon" ? "bg-amber-400 text-zinc-950" : "text-zinc-400"}`}
          >
            Webtoon
          </button>
          <button
            aria-label={isFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
            aria-pressed={isFullscreen}
            title={isFullscreen ? "Exit fullscreen (F)" : "Enter fullscreen (F)"}
            onClick={toggleFullscreen}
            className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800"
          >
            {isFullscreen ? <Minimize size={16} /> : <Maximize size={16} />}
          </button>
          <button
            aria-label="Reader settings"
            onClick={() => setSettingsOpen((value) => !value)}
            className={`rounded-lg p-2 ${settingsOpen ? "bg-zinc-800 text-amber-400" : "text-zinc-400 hover:bg-zinc-800"}`}
          >
            <SlidersHorizontal size={16} />
          </button>
        </div>
      </div>
      {settingsOpen && (
        <div className="absolute right-3 top-14 z-40 w-64 space-y-3 rounded-xl border border-zinc-800 bg-zinc-950 p-3 shadow-2xl">
          <div className="flex items-center justify-between">
            <b className="text-sm">Reader settings</b>
            <span className="text-xs text-zinc-500">This title</span>
          </div>
          <label className="block text-xs text-zinc-400">
            Mode
            <select
              value={overrides.mode ?? ""}
              onChange={(event) =>
                saveReaderOverride({ mode: (event.target.value || null) as Mode | null })
              }
              className="mt-1 w-full rounded-lg border border-zinc-700 bg-zinc-900 px-2 py-1 text-sm text-zinc-100"
            >
              <option value="">Default ({globals.current.mode})</option>
              <option value="single">Single</option>
              <option value="double">Double</option>
              <option value="webtoon">Webtoon</option>
            </select>
          </label>
          <label className="block text-xs text-zinc-400">
            Direction
            <select
              value={overrides.direction ?? ""}
              onChange={(event) =>
                saveReaderOverride({ direction: (event.target.value || null) as Direction | null })
              }
              className="mt-1 w-full rounded-lg border border-zinc-700 bg-zinc-900 px-2 py-1 text-sm text-zinc-100"
            >
              <option value="">Default ({globals.current.direction})</option>
              <option value="ltr">Left to right</option>
              <option value="rtl">Right to left</option>
            </select>
          </label>
          <label className="block text-xs text-zinc-400">
            Fit
            <select
              value={overrides.fit ?? ""}
              onChange={(event) =>
                saveReaderOverride({ fit: (event.target.value || null) as Fit | null })
              }
              className="mt-1 w-full rounded-lg border border-zinc-700 bg-zinc-900 px-2 py-1 text-sm text-zinc-100"
            >
              <option value="">Default ({globals.current.fit})</option>
              <option value="width">Fit width</option>
              <option value="height">Fit height</option>
              <option value="original">Original</option>
            </select>
          </label>
          <p className="text-[11px] leading-snug text-zinc-500">
            Overrides apply to this title only. Choose Default to follow the global reader setting.
          </p>
          <label className="flex items-center gap-2 text-xs text-zinc-300">
            <input
              type="checkbox"
              checked={autoAdvance}
              onChange={(event) => setAutoAdvanceStored(event.target.checked)}
              className="accent-amber-400"
            />
            Auto-advance to the next chapter
          </label>
        </div>
      )}
      {mode === "webtoon" ? (
        <Webtoon pages={pages} index={index} setIndex={setIndex} />
      ) : (
        <Paged
          pages={pages}
          index={index}
          setIndex={setIndex}
          double={mode === "double"}
          direction={direction}
          fit={fit}
        />
      )}
      {drawerOpen && (
        <ChapterDrawer
          ordered={flow.ordered}
          currentId={chapterId}
          onSelect={goChapter}
          onClose={() => setDrawerOpen(false)}
        />
      )}
      {atEnd && flow.next && !dismissedNextUp && (
        <NextUpCard
          next={flow.next}
          autoAdvance={autoAdvance}
          onNext={() => goChapter(flow.next!.id)}
          onStay={() => setDismissedNextUp(true)}
          onDetails={exitReader}
        />
      )}
      {goToOpen && (
        <GoToDialog
          pageCount={pages.length}
          current={Math.min(index + 1, pages.length)}
          onJump={(page) => {
            setIndex(alignToSpread(page - 1, pages.length, mode));
            setGoToOpen(false);
          }}
          onClose={() => setGoToOpen(false)}
        />
      )}
      <div
        className={`flex items-center gap-3 border-t border-zinc-800 bg-zinc-950 px-4 py-2 ${menu ? "" : "hidden"}`}
      >
        <button
          onClick={() => setGoToOpen(true)}
          title="Go to page (G)"
          aria-label={`Go to page, currently page ${Math.min(index + 1, pages.length)} of ${pages.length}, ${percent} percent read`}
          className="shrink-0 text-xs text-zinc-500 hover:text-zinc-200"
        >
          {Math.min(index + 1, pages.length)} / {pages.length} · {percent}%
        </button>
        <input
          type="range"
          aria-label="Page"
          aria-valuemin={1}
          aria-valuemax={pages.length}
          aria-valuenow={Math.min(index + 1, pages.length)}
          aria-valuetext={`Page ${Math.min(index + 1, pages.length)} of ${pages.length}`}
          min="0"
          max={Math.max(0, pages.length - 1)}
          value={index}
          onChange={(e) => setIndex(alignToSpread(Number(e.target.value), pages.length, mode))}
          className="flex-1 accent-amber-400"
        />
        <button
          aria-label="Hide reader menu"
          aria-expanded={menu}
          onClick={() => setMenu(false)}
          className="rounded-lg p-2 text-zinc-400"
        >
          <Menu size={16} />
        </button>
      </div>
      {!menu && (
        <button
          aria-label="Show reader menu"
          onClick={() => setMenu(true)}
          className="absolute right-4 top-4 rounded-lg bg-zinc-900/80 p-2 text-zinc-300"
        >
          <Menu size={17} />
        </button>
      )}
    </div>
  );
}

function ChapterDrawer({
  ordered,
  currentId,
  onSelect,
  onClose,
}: {
  ordered: Chapter[];
  currentId: string;
  onSelect: (id: string) => void;
  onClose: () => void;
}) {
  return (
    <div className="absolute inset-y-0 left-0 z-40 flex w-72 max-w-[80vw] flex-col border-r border-zinc-800 bg-zinc-950 shadow-2xl">
      <div className="flex items-center justify-between border-b border-zinc-800 px-3 py-2">
        <b className="text-sm">Chapters · {ordered.length}</b>
        <button
          aria-label="Close chapters"
          onClick={onClose}
          className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800"
        >
          <X size={16} />
        </button>
      </div>
      <ol className="min-h-0 flex-1 overflow-y-auto">
        {ordered.map((chapter) => (
          <li key={chapter.id}>
            <button
              onClick={() => onSelect(chapter.id)}
              aria-current={chapter.id === currentId ? "true" : undefined}
              className={`flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-zinc-900 ${
                chapter.id === currentId ? "bg-zinc-900 text-amber-300" : "text-zinc-200"
              } ${chapter.read ? "opacity-60" : ""}`}
            >
              {chapter.read && <Check size={14} className="shrink-0 text-emerald-400" />}
              <span className="min-w-0 flex-1 truncate">{chapterLabelFor(chapter)}</span>
              {chapter.downloaded && (
                <span className="shrink-0 text-[11px] text-zinc-500">saved</span>
              )}
            </button>
          </li>
        ))}
      </ol>
      <p className="border-t border-zinc-800 px-3 py-2 text-[11px] text-zinc-500">
        N / P jumps to the next / previous chapter.
      </p>
    </div>
  );
}

function NextUpCard({
  next,
  autoAdvance,
  onNext,
  onStay,
  onDetails,
}: {
  next: Chapter;
  autoAdvance: boolean;
  onNext: () => void;
  onStay: () => void;
  onDetails: () => void;
}) {
  return (
    <div className="absolute inset-x-0 bottom-16 z-40 mx-auto w-80 max-w-[90vw] rounded-xl border border-zinc-700 bg-zinc-950 p-4 shadow-2xl">
      <p className="text-xs uppercase tracking-wide text-zinc-500">Chapter finished</p>
      <b className="mt-1 block truncate text-sm">Up next: {chapterLabelFor(next)}</b>
      {autoAdvance && (
        <p className="mt-1 text-xs text-zinc-400">Auto-advancing shortly. Stay to keep reading.</p>
      )}
      <div className="mt-3 flex flex-wrap gap-2">
        <button
          onClick={onNext}
          className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
        >
          Next chapter
        </button>
        <button onClick={onStay} className="rounded-lg border border-zinc-700 px-3 py-2 text-sm">
          Keep reading
        </button>
        <button
          onClick={onDetails}
          className="rounded-lg border border-zinc-700 px-3 py-2 text-sm text-zinc-400"
        >
          Details
        </button>
      </div>
    </div>
  );
}

function GoToDialog({
  pageCount,
  current,
  onJump,
  onClose,
}: {
  pageCount: number;
  current: number;
  onJump: (page: number) => void;
  onClose: () => void;
}) {
  const [value, setValue] = useState(String(current));
  return (
    <div
      className="absolute inset-0 z-40 grid place-items-center bg-black/70 p-5"
      onMouseDown={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Go to page"
        className="w-64 rounded-xl border border-zinc-700 bg-zinc-950 p-4 shadow-2xl"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <b className="text-sm">Go to page</b>
        <form
          className="mt-3 flex gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            const page = Math.min(Math.max(1, Number(value) || 1), Math.max(1, pageCount));
            onJump(page);
          }}
        >
          <input
            // eslint-disable-next-line jsx-a11y/no-autofocus
            autoFocus
            aria-label="Page number"
            inputMode="numeric"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            className="min-w-0 flex-1 rounded-lg border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm"
          />
          <button
            type="submit"
            className="rounded-lg bg-amber-400 px-3 py-1.5 text-sm font-semibold text-zinc-950"
          >
            Go
          </button>
        </form>
        <p className="mt-2 text-xs text-zinc-500">
          Page {current} of {pageCount}
        </p>
      </div>
    </div>
  );
}

function Paged({
  pages,
  index,
  setIndex,
  double,
  direction,
  fit,
}: {
  pages: Page[];
  index: number;
  setIndex: (value: number) => void;
  double: boolean;
  direction: Direction;
  fit: Fit;
}) {
  const count = double ? 2 : 1;
  const mode: Mode = double ? "double" : "single";
  // Chevron sides stay fixed, but their actions follow the reading direction:
  // in RTL the left control advances and the right control goes back, so the
  // labels always describe what the button does.
  const goPrevious = () => setIndex(alignToSpread(index - count, pages.length, mode));
  const goNext = () => setIndex(alignToSpread(index + count, pages.length, mode));
  const leftAction = direction === "rtl" ? goNext : goPrevious;
  const rightAction = direction === "rtl" ? goPrevious : goNext;
  return (
    <div
      className={`relative flex min-h-0 flex-1 items-center justify-center gap-2 overflow-auto bg-black p-2 sm:p-6 ${direction === "rtl" ? "flex-row-reverse" : ""}`}
    >
      {pages.slice(index, index + count).map((page, offset) => (
        <PageImage
          key={page.index}
          page={page}
          alt={`Page ${index + offset + 1}`}
          priority={offset === 0}
          className={
            fit === "height"
              ? "max-h-full w-auto object-contain"
              : fit === "original"
                ? "max-h-none max-w-none object-contain"
                : double
                  ? "h-auto max-w-[calc(50%-0.5rem)] object-contain"
                  : "h-auto max-w-full object-contain"
          }
        />
      ))}
      <button
        aria-label={direction === "rtl" ? "Next page" : "Previous page"}
        onClick={leftAction}
        className="absolute left-2 top-1/2 rounded-full bg-black/60 p-3 text-white"
      >
        <ChevronLeft />
      </button>
      <button
        aria-label={direction === "rtl" ? "Previous page" : "Next page"}
        onClick={rightAction}
        className="absolute right-2 top-1/2 rounded-full bg-black/60 p-3 text-white"
      >
        <ChevronRight />
      </button>
    </div>
  );
}
function Webtoon({
  pages,
  index,
  setIndex,
}: {
  pages: Page[];
  index: number;
  setIndex: (value: number) => void;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const initialIndex = useRef(index);
  const indexRef = useRef(index);
  indexRef.current = index;
  const virtualizer = useVirtualizer({
    count: pages.length,
    getScrollElement: () => parent.current,
    estimateSize: () => 720,
    overscan: 2,
  });
  const virtualizerRef = useRef(virtualizer);
  virtualizerRef.current = virtualizer;
  // Entering webtoon mode aligns the scroll position with the restored page.
  // Scroll events during the alignment window are ignored so the
  // programmatic scroll can never clobber the restored position with page
  // one; afterwards every scroll source (wheel, touch, keyboard, scrollbar)
  // adopts the first visible virtual item.
  const mountedAt = useRef(Date.now());
  useLayoutEffect(() => {
    virtualizer.scrollToIndex(initialIndex.current);
    // Alignment runs once per mount; later index changes come from scrolling.
  }, []);
  useEffect(() => {
    const el = parent.current;
    if (!el) return;
    let frame = 0;
    const onScroll = () => {
      if (Date.now() - mountedAt.current < 750) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const first = virtualizerRef.current.getVirtualItems()[0];
        if (first && first.index !== indexRef.current) setIndex(first.index);
      });
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      el.removeEventListener("scroll", onScroll);
      cancelAnimationFrame(frame);
    };
  }, [setIndex]);
  return (
    <div ref={parent} className="min-h-0 flex-1 overflow-y-auto bg-black">
      <div className="relative mx-auto max-w-3xl" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((item) => (
          <div
            key={item.key}
            ref={virtualizer.measureElement}
            data-index={item.index}
            className="absolute left-0 w-full px-2 pb-2"
            style={{ transform: `translateY(${item.start}px)` }}
          >
            <PageImage
              page={pages[item.index]}
              alt={`Page ${item.index + 1}`}
              className="w-full rounded-sm"
              loading="lazy"
            />
          </div>
        ))}
      </div>
    </div>
  );
}

// PageImage degrades a failed delivery into an inline retry. The retry
// re-requests the image with a cache-busting parameter so an intermediary
// cache cannot serve the failed response again. Layout classes come from the
// caller: paged and webtoon modes fit images to the screen differently. The
// current spread decodes async with high fetch priority; surrounding images
// stay lazy so page turns usually hit the cache instead of the network.
function PageImage({
  page,
  alt,
  className,
  loading,
  priority,
}: {
  page: Page;
  alt: string;
  className: string;
  loading?: "lazy" | "eager";
  priority?: boolean;
}) {
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  if (failed)
    return (
      <button
        onClick={() => {
          setFailed(false);
          setAttempt((value) => value + 1);
        }}
        className={`grid h-64 w-full place-items-center rounded-sm border border-zinc-800 bg-zinc-900 text-sm text-zinc-300 ${className}`}
      >
        Retry page
      </button>
    );
  return (
    <img
      src={`${api.readerImage(page)}${attempt ? `?retry=${attempt}` : ""}`}
      alt={alt}
      onError={() => setFailed(true)}
      className={className}
      loading={loading ?? (priority ? "eager" : undefined)}
      decoding="async"
      fetchPriority={priority ? "high" : "auto"}
    />
  );
}
function chapterLabel(data: Aggregate, chapterID: string) {
  const item = data.chapters.find((chapter) => chapter.id === chapterID);
  return item?.chapterNumber == null ? item?.title || "Special" : `Chapter ${item.chapterNumber}`;
}

// resumeIndex converts a stored 1-based last-read page into the 0-based page
// index a reader session opens on. A double-page spread opens on the pair
// containing that page, and a missing or out-of-range value clamps to the
// available pages.
export function resumeIndex(lastReadPage: number | null, pageCount: number, mode: Mode): number {
  const zeroBased = (lastReadPage ?? 1) - 1;
  return alignToSpread(zeroBased, pageCount, mode);
}
