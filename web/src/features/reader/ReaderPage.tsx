import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams, useSearchParams, useNavigate, Navigate } from "react-router-dom";
import { ArrowLeft, Menu } from "lucide-react";
import { api } from "../../api";
import {
  attachFullscreen,
  attachWakeLock,
  type MekuriEngine,
  type MekuriFullscreenController,
  type MekuriKeyboardMap,
  type MekuriPage,
  type MekuriWakeLockController,
} from "@makinuki/mekuri/engine";
import {
  MekuriPageStatus,
  MekuriViewStyles,
  PagedView,
  WebtoonView,
  type MekuriAltLabeler,
} from "@makinuki/mekuri/views";
import type { Aggregate, Page } from "../../types";
import { ErrorState, EmptyState, LoadingState } from "../../components/States";
import { chapterLabel, prevNextChapter } from "./engine/chapters";
import {
  alignToSpread,
  defaultReaderDisplay,
  defaultReaderGlobals,
  emptyReaderOverrides,
  readerDisplayFromSettings,
  readerGlobalsFromSettings,
  readerImageFilter,
  readerOverridesFromManga,
  resolveReaderSettings,
  resumeIndex,
  spreadStep,
  type ReaderDirection as Direction,
  type ReaderDisplay,
  type ReaderFit as Fit,
  type ReaderGlobals,
  type ReaderMode as Mode,
  type ReaderOverrides,
} from "./readerSettings";
import { toMekuriPages } from "./mekuriAdapter";
import {
  useMekuriFailureIds,
  useMekuriHudVisible,
  useMekuriPageIndex,
  useMekuriReader,
} from "./useMekuriReader";
import { useReaderKeyboard } from "./useReaderKeyboard";
import { useReaderProgress } from "./useReaderProgress";
import { ChapterDrawer } from "./ChapterDrawer";
import { GoToDialog } from "./GoToDialog";
import { HostPageImage } from "./HostPageImage";
import { NextUpCard } from "./NextUpCard";
import { ReaderFooter } from "./ReaderFooter";
import { ReaderHeader } from "./ReaderHeader";
import { ReaderScreen } from "./ReaderScreen";
import { ReaderSettingsPopup } from "./ReaderSettingsPopup";
import { ShortcutsDialog } from "./ShortcutsDialog";

// Keyboard contract shared by both prebuilt views: the attached engine
// dispatchers own the keys this map expresses (arrows and D/A with RTL
// inversion, M for the HUD), while the host listener keeps everything else.
// Space stays host-side in paged modes because the engine cannot express
// shift-for-back or the paged-only gate; in webtoon mode the dispatcher
// ignores it, so the column scrolls natively. PgDn/PgUp/Home/End stay
// host-side because the engine map has no jump actions.
const READER_KEYBOARD_MAP = {
  nextPage: ["ArrowRight", "KeyD"],
  toggleHUD: ["KeyM"],
} satisfies Partial<MekuriKeyboardMap>;

// Single localization point for page descriptions, shared by the views, the
// page images, and the status announcement.
const altLabeler: MekuriAltLabeler = (_page, index) => `Page ${index + 1}`;

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
  // The headless engine owns the live reading position and chrome
  // visibility; these states only seed it before pages load and never drive
  // the UI once the chapter engine exists.
  const [resumePage, setResumePage] = useState(0);
  const [menuFallback, setMenuFallback] = useState(true);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const [overrides, setOverrides] = useState<ReaderOverrides>(emptyReaderOverrides);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [goToOpen, setGoToOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [display, setDisplay] = useState<ReaderDisplay>(defaultReaderDisplay);
  const [keepAwake, setKeepAwake] = useState(() => {
    try {
      return window.localStorage.getItem("reader.keep_awake") === "1";
    } catch {
      return false;
    }
  });
  const [dismissedNextUp, setDismissedNextUp] = useState(false);
  const [autoAdvance, setAutoAdvance] = useState(() => {
    try {
      return window.localStorage.getItem("reader.auto_advance") === "1";
    } catch {
      return false;
    }
  });
  // Retry counts live per page id: a failed page that sits outside the
  // current spread unmounts and comes back later, and only a persisted
  // counter can hand it the cache-busting URL that retry promised for it.
  // Delivery failures themselves live in the engine failure registry, which
  // the retry pill below reads.
  const [retryAttempts, setRetryAttempts] = useState<Record<string, number>>({});
  const [online, setOnline] = useState(() =>
    typeof navigator === "undefined" ? true : navigator.onLine,
  );
  const [queueNote, setQueueNote] = useState("");
  useEffect(() => {
    const sync = () => setOnline(navigator.onLine);
    window.addEventListener("online", sync);
    window.addEventListener("offline", sync);
    return () => {
      window.removeEventListener("online", sync);
      window.removeEventListener("offline", sync);
    };
  }, []);
  const retryHostPage = useCallback((id: string | number) => {
    // Every failed id keeps its own count, so pages that are off screen right
    // now reload with the bumped URL once they mount again. The engine retry
    // clears the registry entry; the count bump reloads the image itself.
    const key = String(id);
    setRetryAttempts((current) => ({ ...current, [key]: (current[key] ?? 0) + 1 }));
    engineRef.current?.retryPage(id);
  }, []);
  const globals = useRef<ReaderGlobals>(defaultReaderGlobals);
  const overridesRef = useRef<ReaderOverrides>(emptyReaderOverrides);
  const readerRef = useRef<HTMLDivElement>(null);
  const engineRef = useRef<MekuriEngine | null>(null);
  const fullscreenRef = useRef<MekuriFullscreenController | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);

  // Leaving the reader always lands on a real page: the title details when a
  // manga is open, otherwise the library. A bare history step would strand a
  // direct open or a new tab with nowhere to go.
  const exitReader = useCallback(() => {
    if (mangaId) navigate(`/manga/${encodeURIComponent(mangaId)}`);
    else navigate("/library");
  }, [mangaId, navigate]);

  const toggleFullscreen = useCallback(() => {
    const controller = fullscreenRef.current;
    if (!controller) return;
    if (controller.isActive()) {
      void controller.exit().then(() => setIsFullscreen(controller.isActive()));
    } else {
      void controller.enter().then(() => setIsFullscreen(controller.isActive()));
    }
  }, []);
  // The reader surface mounts once loading resolves; the controller attaches
  // then and re-attaches after chapter switches. Native promotion can close
  // outside this code through the platform gesture, so the state also syncs
  // off the fullscreenchange event. Exits the module owns itself (its Escape
  // binding, its exit control) correct the state on the next toggle.
  const readerReady = pages.length > 0 && !loading;
  useEffect(() => {
    if (!readerReady) return;
    const element = readerRef.current;
    if (!element || fullscreenRef.current) return;
    const controller = attachFullscreen({ element });
    fullscreenRef.current = controller;
    const sync = () => setIsFullscreen(controller.isActive());
    document.addEventListener("fullscreenchange", sync);
    return () => {
      document.removeEventListener("fullscreenchange", sync);
      controller.detach();
      fullscreenRef.current = null;
    };
  }, [readerReady]);

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
      setRetryAttempts({});
      setQueueNote("");
      navigate(`/reader/${encodeURIComponent(mangaId)}/${encodeURIComponent(id)}`);
    },
    [mangaId, navigate],
  );
  const currentChapter = useMemo(
    () => aggregate?.chapters.find((item) => item.id === chapterId),
    [aggregate, chapterId],
  );
  const queueChapter = useCallback(
    (id: string) => {
      if (!aggregate) return;
      setQueueNote("");
      api
        .enqueue(mangaId, [id], "", aggregate.manga.downloadFormat)
        .then(() => setQueueNote("Chapter queued for download."))
        .catch((e) =>
          setQueueNote(e instanceof Error ? e.message : "Unable to queue the download"),
        );
    },
    [aggregate, mangaId],
  );
  const setAutoAdvanceStored = useCallback((value: boolean) => {
    setAutoAdvance(value);
    try {
      window.localStorage.setItem("reader.auto_advance", value ? "1" : "0");
    } catch {
      // Private browsing may refuse storage; the session value still applies.
    }
  }, []);
  const setKeepAwakeStored = useCallback((value: boolean) => {
    setKeepAwake(value);
    try {
      window.localStorage.setItem("reader.keep_awake", value ? "1" : "0");
    } catch {
      // Private browsing may refuse storage; the session value still applies.
    }
  }, []);
  const wakeLockRef = useRef<MekuriWakeLockController | null>(null);
  useEffect(() => {
    const controller = attachWakeLock();
    wakeLockRef.current = controller;
    return () => {
      controller.detach();
      wakeLockRef.current = null;
    };
  }, []);
  useEffect(() => {
    // Screen wake lock is best-effort: unsupported browsers and denied
    // requests simply leave the reader usable without it.
    const controller = wakeLockRef.current;
    if (!controller) return;
    if (keepAwake) {
      void controller.enable();
    } else {
      void controller.disable();
    }
  }, [keepAwake]);

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
      .settings()
      .then((items) => {
        if (!active) return;
        globals.current = readerGlobalsFromSettings(items);
        setDisplay(readerDisplayFromSettings(items));
        applyReaderSettings();
      })
      .catch(() => {
        // Reader defaults are optional; the local default remains usable offline.
      });
    return () => {
      active = false;
    };
  }, [applyReaderSettings]);
  // Display preferences (navigation zones, gap, theme, filters) are global
  // settings shared with the settings page. The popup writes them straight
  // through while updating the local state for instant feedback.
  const saveDisplaySetting = useCallback((key: string, value: string | number | boolean) => {
    setDisplay((current) => {
      const next = { ...current };
      if (key === "reader.navigation") next.navigation = value as ReaderDisplay["navigation"];
      else if (key === "reader.webtoon_gap")
        next.gap = Math.min(48, Math.max(0, Math.round(Number(value) || 0)));
      else if (key === "reader.theme") next.theme = value as ReaderDisplay["theme"];
      else if (key === "reader.brightness")
        next.brightness = Math.min(150, Math.max(50, Math.round(Number(value) || 100)));
      else if (key === "reader.grayscale") next.grayscale = value === true;
      else if (key === "reader.invert") next.invert = value === true;
      return next;
    });
    void api.updateSetting(key, value).catch((err) => {
      console.error("saving display setting failed", err);
    });
  }, []);
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
  // Last visible 1-based page under the engine's own spread pairing, so
  // progress, percent, and the end card agree with the spreads the engine
  // reports.
  const spreadEndForPage = useCallback(
    (pageIndex: number): number => {
      const snapshot = engineRef.current?.getState();
      const spread = snapshot?.activeSpreads.find((candidate) => candidate.includes(pageIndex));
      if (spread && spread.length > 0) {
        return Math.min(pages.length, spread[spread.length - 1] + 1);
      }
      return Math.min(pages.length, pageIndex + spreadStep(mode));
    },
    [pages.length, mode],
  );
  const currentPageIndex = useCallback(
    () => engineRef.current?.getReadingPosition().pageIndex ?? resumePage,
    [resumePage],
  );
  const { incognito, saveAtPage, resetReadingSession } = useReaderProgress({
    mangaId,
    chapterId,
    aggregate,
    pageCount: pages.length,
    spreadEndForPage,
    currentPageIndex,
  });

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
    setRetryAttempts({});
    setQueueNote("");
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
        resetReadingSession();
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
        setResumePage(
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
  }, [mangaId, chapterId, attempt, resetReadingSession]);
  // Engine behind the existing markup. One engine per chapter: created once
  // the pages load, replaced in place while reading, discarded when the pages
  // clear on chapter switch. Mode and direction sync through
  // setMode/setDirection effects inside useMekuriReader.
  const mekuriPages = useMemo(() => toMekuriPages(pages), [pages]);
  const engine = useMekuriReader({
    pages: mekuriPages,
    mode,
    direction,
    navigation: display.navigation,
    initialPageIndex: resumePage,
    enabled: pages.length > 0,
    keyboardMap: READER_KEYBOARD_MAP,
    // Open drawers and dialogs own the keyboard; the view dispatchers and
    // the host listener all stand down while any of them is open.
    suppressKeyboard: helpOpen || goToOpen || settingsOpen || drawerOpen,
    onPositionSample: (position) => saveAtPage(position.pageIndex),
  });
  engineRef.current = engine;
  const index = useMekuriPageIndex(engine, resumePage);
  const menu = useMekuriHudVisible(engine, menuFallback);
  const failureIds = useMekuriFailureIds(engine);
  const hideChrome = useCallback(() => {
    if (engineRef.current) engineRef.current.toggleHUD(false);
    else setMenuFallback(false);
  }, []);
  const toggleChrome = useCallback(() => {
    if (engineRef.current) engineRef.current.toggleHUD();
    else setMenuFallback((value) => !value);
  }, []);
  // Slider, go-to, and jump keys land without hiding the chrome; every other
  // discrete move hides it through the subscription below.
  const hideOnNavRef = useRef(true);
  const goToPageIndex = useCallback(
    (page: number) => {
      hideOnNavRef.current = false;
      if (engineRef.current) engineRef.current.goToIndex(page);
      else setResumePage(Math.min(Math.max(0, page), Math.max(0, pages.length - 1)));
    },
    [pages.length],
  );
  const turnNext = useCallback(() => {
    engineRef.current?.next();
    hideChrome();
  }, [hideChrome]);
  const turnPrevious = useCallback(() => {
    engineRef.current?.prev();
    hideChrome();
  }, [hideChrome]);
  useEffect(() => {
    // Gesture taps and swipes dispatch straight into the engine, past any
    // host callback, so the chrome-hide policy for them lives here: a
    // discrete move outside the continuous column hides the chrome. Mode
    // transitions own their position, and the hide runs a microtask later so
    // it never notifies inside the engine's own notification loop.
    if (!engine) return undefined;
    let lastIndex = engine.getReadingPosition().pageIndex;
    let lastMode = engine.getState().mode;
    return engine.subscribe(() => {
      const state = engine.getState();
      const moved = state.pageIndex !== lastIndex;
      const modeChanged = state.mode !== lastMode;
      lastIndex = state.pageIndex;
      lastMode = state.mode;
      if (!moved || modeChanged) return;
      if (state.mode === "continuous-vertical") return;
      if (!hideOnNavRef.current) {
        hideOnNavRef.current = true;
        return;
      }
      const target = engine;
      queueMicrotask(() => {
        if (engineRef.current !== target) return;
        target.toggleHUD(false);
      });
    });
  }, [engine]);

  // The last spread is fully visible: progress counts the end of the spread
  // containing the reading position, so its end is the final page on screen.
  const visibleEnd = spreadEndForPage(index);
  const atEnd = pages.length > 0 && visibleEnd >= pages.length;
  const percent = pages.length ? Math.round((visibleEnd / pages.length) * 100) : 0;
  // Host page lookup for the view page bodies below.
  const pageById = useMemo(() => new Map(pages.map((page) => [page.id, page])), [pages]);
  const imgFilter = useMemo(() => readerImageFilter(display), [display]);
  const pagedFitClass =
    fit === "height"
      ? "max-h-full w-auto object-contain"
      : fit === "screen"
        ? "max-h-full max-w-full object-contain"
        : fit === "original"
          ? "max-h-none max-w-none object-contain"
          : "h-auto max-w-full object-contain";
  // Current spread for eager decoding: pages on screen decode first, the
  // rest stay lazy so page turns usually hit the cache instead of the
  // network.
  const priorityIds = useMemo(() => {
    if (!engine || mode === "webtoon") return null;
    const spread = engine.getState().activeSpreads.find((candidate) => candidate.includes(index));
    return spread ? new Set(spread) : null;
  }, [engine, mode, index]);
  const renderPagedPage = (page: MekuriPage, pageIndex: number) => {
    const activeEngine = engineRef.current;
    const hostPage = pageById.get(String(page.id));
    if (!activeEngine || !hostPage) return null;
    const priority = priorityIds?.has(pageIndex) ?? false;
    return (
      <HostPageImage
        page={hostPage}
        alt={altLabeler(page, pageIndex)}
        className={pagedFitClass}
        style={{ filter: imgFilter }}
        priority={priority}
        attempt={retryAttempts[String(hostPage.id)] ?? 0}
        engine={activeEngine}
        onRetry={retryHostPage}
      />
    );
  };
  const renderWebtoonPage = (page: MekuriPage, pageIndex: number) => {
    const activeEngine = engineRef.current;
    const hostPage = pageById.get(String(page.id));
    if (!activeEngine || !hostPage) return null;
    return (
      <div style={{ filter: imgFilter }}>
        <HostPageImage
          page={hostPage}
          alt={altLabeler(page, pageIndex)}
          className="w-full rounded-sm"
          loading="lazy"
          attempt={retryAttempts[String(hostPage.id)] ?? 0}
          engine={activeEngine}
          onRetry={retryHostPage}
        />
      </div>
    );
  };
  const retryAllHostFailed = useCallback(() => {
    // Every failed id keeps its own count, so pages that are off screen right
    // now reload with the bumped URL once they mount again. The engine call
    // clears the registry the pill reads.
    setRetryAttempts((current) => {
      const next = { ...current };
      for (const id of failureIds) next[String(id)] = (next[String(id)] ?? 0) + 1;
      return next;
    });
    engineRef.current?.retryAllFailures();
  }, [failureIds]);
  // End-of-chapter card for the view boundary mount points. The wrapper keeps
  // the card docked at the bottom while leaving the rest of the overlay
  // transparent to taps.
  const nextUpSlot =
    atEnd && flow.next && !dismissedNextUp ? (
      <div
        style={{
          position: "absolute",
          inset: 0,
          pointerEvents: "none",
          display: "flex",
          alignItems: "flex-end",
          justifyContent: "center",
          paddingBottom: 64,
        }}
      >
        <div style={{ pointerEvents: "auto" }}>
          <NextUpCard
            next={flow.next}
            autoAdvance={autoAdvance}
            queueNote={queueNote}
            trackerCount={aggregate?.trackers.length ?? 0}
            onNext={() => goChapter(flow.next!.id)}
            onDownload={() => queueChapter(flow.next!.id)}
            onStay={() => setDismissedNextUp(true)}
            onDetails={exitReader}
          />
        </div>
      </div>
    ) : null;

  useReaderKeyboard({
    helpOpen,
    goToOpen,
    settingsOpen,
    drawerOpen,
    mode,
    mangaId,
    pageCount: pages.length,
    prev: flow.prev,
    next: flow.next,
    setHelpOpen,
    setGoToOpen,
    setSettingsOpen,
    setDrawerOpen,
    hideChrome,
    toggleFullscreen,
    goChapter,
    saveReaderOverride,
    setMode,
    turnNext,
    turnPrevious,
    goToPageIndex,
  });
  useEffect(() => {
    // Warm the HTTP cache for the next chapter while the reader finishes
    // this one so the jump rarely shows a loading screen.
    if (atEnd && flow.next) void api.pages(flow.next.id).catch(() => {});
  }, [atEnd, flow.next]);
  useEffect(() => {
    // Warm the engine's preload window through the host image URLs so page
    // turns usually hit the browser cache. The window follows the reading
    // position, so it is re-warmed on every engine move. Failures are
    // ignored: the image element retries through the normal path.
    if (!engine || !pages.length) return;
    const warm = () => {
      for (const pageIndex of engine.getPreloadWindow()) {
        const page = pages[pageIndex];
        if (!page) continue;
        const img = new Image();
        img.decoding = "async";
        img.src = `${api.readerImage(page)}${retryAttempts[String(page.id)] ? `?retry=${retryAttempts[String(page.id)]}` : ""}`;
      }
    };
    warm();
    return engine.subscribe(warm);
  }, [engine, pages, retryAttempts]);
  // Auto-advance counts down on the end card, then jumps. Any navigation
  // away or an explicit stay cancels it via the effect cleanup.
  useEffect(() => {
    if (!atEnd || !flow.next || dismissedNextUp || !autoAdvance) return;
    const timer = window.setTimeout(() => goChapter(flow.next!.id), 5000);
    return () => window.clearTimeout(timer);
  }, [atEnd, flow.next, dismissedNextUp, autoAdvance, goChapter]);
  if (canonicalize) {
    // Extra query parameters survive the scheme change so a legacy deep link
    // such as ?page=5 still opens on the requested page.
    const rest = new URLSearchParams(searchParams);
    rest.delete("manga");
    rest.delete("chapter");
    const query = rest.toString();
    return (
      <Navigate
        to={`/reader/${encodeURIComponent(mangaId)}/${encodeURIComponent(chapterId)}${query ? `?${query}` : ""}`}
        replace
      />
    );
  }

  if (error)
    return (
      <ReaderScreen theme={display.theme} pad>
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
      </ReaderScreen>
    );
  if (!mangaId || !chapterId)
    return (
      <ReaderScreen theme={display.theme} pad>
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
      </ReaderScreen>
    );
  if (loading)
    return (
      <ReaderScreen theme={display.theme}>
        <LoadingState label="Loading reader" />
      </ReaderScreen>
    );
  if (aggregate && !pages.length)
    return (
      <ReaderScreen theme={display.theme} pad>
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
      </ReaderScreen>
    );
  if (!aggregate || !pages.length || !engine)
    return (
      <ReaderScreen theme={display.theme}>
        <LoadingState label="Loading reader" />
      </ReaderScreen>
    );

  return (
    <div
      ref={readerRef}
      data-theme={display.theme}
      className="reader fixed inset-0 z-30 flex flex-col bg-(--reader-canvas) text-(--reader-text)"
    >
      <ReaderHeader
        visible={menu}
        hasManga={Boolean(mangaId)}
        title={aggregate.manga.title}
        chapterLabel={chapterLabel(aggregate, chapterId)}
        incognito={incognito}
        downloaded={currentChapter?.downloaded ?? false}
        online={online}
        drawerOpen={drawerOpen}
        mode={mode}
        isFullscreen={isFullscreen}
        settingsOpen={settingsOpen}
        onBack={exitReader}
        onToggleDrawer={() => setDrawerOpen((value) => !value)}
        onMode={(next) => saveReaderOverride({ mode: next })}
        onToggleFullscreen={toggleFullscreen}
        onToggleSettings={() => setSettingsOpen((value) => !value)}
        onShowHelp={() => setHelpOpen(true)}
      />
      {settingsOpen && (
        <ReaderSettingsPopup
          overrides={overrides}
          globals={globals.current}
          display={display}
          engine={engine}
          autoAdvance={autoAdvance}
          keepAwake={keepAwake}
          onOverride={saveReaderOverride}
          onDisplaySetting={saveDisplaySetting}
          onAutoAdvance={setAutoAdvanceStored}
          onKeepAwake={setKeepAwakeStored}
        />
      )}
      <MekuriViewStyles />
      <MekuriPageStatus engine={engine} pages={mekuriPages} altLabeler={altLabeler} />
      {mode === "webtoon" ? (
        <WebtoonView
          engine={engine}
          pages={mekuriPages}
          maxWidth="48rem"
          gap={display.gap}
          renderPage={renderWebtoonPage}
          altLabeler={altLabeler}
          hud={false}
          boundarySlot={nextUpSlot}
          keyboardOptions={{ map: READER_KEYBOARD_MAP }}
          className="min-h-0 flex-1"
        />
      ) : (
        <PagedView
          engine={engine}
          pages={mekuriPages}
          renderPage={renderPagedPage}
          altLabeler={altLabeler}
          hud={false}
          boundarySlot={nextUpSlot}
          keyboardOptions={{ map: READER_KEYBOARD_MAP }}
          className="min-h-0 flex-1"
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
      {goToOpen && (
        <GoToDialog
          pageCount={pages.length}
          current={Math.min(index + 1, pages.length)}
          onJump={(page) => {
            goToPageIndex(page - 1);
            setGoToOpen(false);
          }}
          onClose={() => setGoToOpen(false)}
        />
      )}
      <ReaderFooter
        visible={menu}
        index={index}
        pageCount={pages.length}
        percent={percent}
        onShowGoTo={() => setGoToOpen(true)}
        onSeek={goToPageIndex}
        onHide={hideChrome}
      />
      {helpOpen && (
        <ShortcutsDialog engine={engine} mode={mode} onClose={() => setHelpOpen(false)} />
      )}
      {!menu && (
        <button
          aria-label="Show reader menu"
          onClick={toggleChrome}
          className="absolute right-4 top-4 rounded-lg border border-(--reader-border) bg-(--reader-bar) p-2 text-(--reader-text)"
        >
          <Menu size={17} />
        </button>
      )}
      {failureIds.length > 0 && (
        <button
          onClick={retryAllHostFailed}
          className={`absolute left-1/2 z-40 -translate-x-1/2 rounded-full bg-red-400 px-4 py-2 text-sm font-semibold text-zinc-950 shadow-2xl ${
            atEnd && flow.next && !dismissedNextUp ? "bottom-72" : "bottom-16"
          }`}
        >
          Retry {failureIds.length} failed {failureIds.length === 1 ? "page" : "pages"}
        </button>
      )}
    </div>
  );
}
