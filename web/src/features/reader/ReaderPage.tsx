import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { useParams, useSearchParams, useNavigate, Navigate } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  TransformWrapper,
  TransformComponent,
  type ReactZoomPanPinchRef,
} from "react-zoom-pan-pinch";
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
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { api } from "../../api";
import {
  attachKeyboard,
  type MekuriDirection,
  type MekuriEngine,
  type MekuriKeyboardEngine,
  type MekuriKeyboardMap,
} from "@makinuki/mekuri/engine";
import type { Aggregate, Chapter, Page } from "../../types";
import { ErrorState, EmptyState, LoadingState } from "../../components/States";
import { chapterLabelFor, prevNextChapter } from "./engine/chapters";
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
  spreadStep,
  type ReaderDirection as Direction,
  type ReaderDisplay,
  type ReaderFit as Fit,
  type ReaderGlobals,
  type ReaderMode as Mode,
  type ReaderNavigation,
  type ReaderOverrides,
  type ReaderTheme,
} from "./readerSettings";
import { toMekuriPages } from "./mekuriAdapter";
import { useMekuriHudVisible, useMekuriPageIndex, useMekuriReader } from "./useMekuriReader";

// Keyboard contract: the attached engine dispatcher owns the keys its map
// can express (arrows and D/A with RTL inversion, M for the HUD), while the
// host listener keeps everything else. Space stays host-side because the
// engine cannot express shift-for-back or the paged-only gate;
// PgDn/PgUp/Home/End stay host-side because the engine map has no jump
// actions; zoom keys stay disabled while zoom has no visible surface.
const MEKURI_HEADLESS_KEYBOARD_MAP = {
  nextPage: ["ArrowRight", "KeyD"],
  toggleHUD: ["KeyM"],
  zoomIn: [],
  zoomOut: [],
  resetZoom: [],
} satisfies Partial<MekuriKeyboardMap>;

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
  const sessionStartedAt = useRef(Date.now());
  // Whole seconds not yet reported; sub-second remainders carry over so a save
  // per page turn cannot truncate the session away.
  const pendingSeconds = useRef(0);
  const [overrides, setOverrides] = useState<ReaderOverrides>(emptyReaderOverrides);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [incognito, setIncognito] = useState(false);
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
  // Failed page deliveries, by page id. PageImage reports failures up so the
  // reader can offer a single retry-all instead of per-image buttons only.
  const [failedPages, setFailedPages] = useState<string[]>([]);
  // Retry counts live per page id rather than per mounted image: a failed page
  // that sits outside the current spread unmounts and comes back later, and
  // only a persisted counter can hand it the cache-busting URL that retry
  // promised for it.
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
  const reportFailed = useCallback((id: string) => {
    setFailedPages((current) => (current.includes(id) ? current : [...current, id]));
  }, []);
  const reportRecovered = useCallback((id: string) => {
    // Bail out when the id is not tracked so successful first loads do not
    // churn state; a page that reloads cleanly after a failure still clears.
    setFailedPages((current) =>
      current.includes(id) ? current.filter((item) => item !== id) : current,
    );
  }, []);
  const retryPage = useCallback((id: string) => {
    setRetryAttempts((current) => ({ ...current, [id]: (current[id] ?? 0) + 1 }));
    setFailedPages((current) => current.filter((item) => item !== id));
  }, []);
  const retryAllFailed = useCallback(() => {
    // Every failed id keeps its own count, so pages that are off screen right
    // now reload with the bumped URL once they mount again.
    setRetryAttempts((current) => {
      const next = { ...current };
      for (const id of failedPages) next[id] = (next[id] ?? 0) + 1;
      return next;
    });
    setFailedPages([]);
  }, [failedPages]);
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
  // Progress writes are blocked until the incognito state has been read once;
  // the 500 ms debounce can otherwise fire before the fetch resolves and leak
  // a history write for a session the user believes is incognito.
  const incognitoLoadedRef = useRef(false);
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
      setFailedPages([]);
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
  useEffect(() => {
    // Screen wake lock is best-effort: unsupported browsers and denied
    // requests simply leave the reader usable without it.
    if (!keepAwake || !("wakeLock" in navigator)) return;
    let cancelled = false;
    let lock: {
      release: () => void;
      addEventListener?: (type: string, listener: () => void) => void;
    } | null = null;
    const acquire = () => {
      try {
        (
          navigator as Navigator & { wakeLock: { request: (kind: string) => Promise<unknown> } }
        ).wakeLock
          .request("screen")
          .then((sentinel) => {
            const next = sentinel as {
              release: () => void;
              addEventListener?: (type: string, listener: () => void) => void;
            };
            if (cancelled) {
              next.release();
              return;
            }
            lock = next;
            // The browser releases the lock when the tab hides; clearing the
            // reference lets the visibilitychange handler re-acquire it
            // instead of treating the stale sentinel as still held.
            next.addEventListener?.("release", () => {
              lock = null;
            });
          })
          .catch(() => {});
      } catch {
        // Older engines throw synchronously; ignore and carry on.
      }
    };
    acquire();
    const reacquire = () => {
      if (document.visibilityState === "visible" && !lock) acquire();
    };
    document.addEventListener("visibilitychange", reacquire);
    return () => {
      cancelled = true;
      document.removeEventListener("visibilitychange", reacquire);
      try {
        lock?.release();
      } catch {
        // Already released; nothing to do.
      }
    };
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
      .incognito()
      .then((state) => {
        if (!active) return;
        incognitoRef.current = state.enabled;
        setIncognito(state.enabled);
        incognitoLoadedRef.current = true;
      })
      .catch(() => {
        // Incognito state is optional; a failed read fails closed so no
        // progress leaves the device until a later mount can confirm it.
        incognitoRef.current = true;
        incognitoLoadedRef.current = true;
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
    setFailedPages([]);
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
        sessionStartedAt.current = Date.now();
        pendingSeconds.current = 0;
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
    // the frontend must not emit the request in the first place. Until the
    // incognito state has been read once, unknown is treated as enabled.
    if (!incognitoLoadedRef.current || incognitoRef.current) return Promise.resolve();
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
  // Engine behind the existing markup. One engine per chapter: created once
  // the pages load, replaced in place while reading, discarded when the pages
  // clear on chapter switch. Mode and direction sync through
  // setMode/setDirection effects inside useMekuriReader.
  const mekuriPages = useMemo(() => toMekuriPages(pages), [pages]);
  const engineRef = useRef<MekuriEngine | null>(null);
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
  // Last progress write, to drop saves that carry no new information: the
  // engine samples immediately on every discrete move, so without this the
  // exit flush would re-post the position a page turn just recorded.
  const lastPostedRef = useRef<{ key: string; page: number } | null>(null);
  const saveAtPage = useCallback(
    (pageIndex: number) => {
      if (!pages.length || !aggregate) return;
      const visibleEnd = spreadEndForPage(pageIndex);
      const now = Date.now();
      // Reading time accrues between writes. Most intervals cover well under
      // a second; flooring each one on its own would drop them all. Whole
      // seconds are reported and the remainder stays pending. The daemon
      // stores one reading session per write, so the value is the delta since
      // the previous write, capped at 300 seconds.
      pendingSeconds.current = Math.min(
        300,
        pendingSeconds.current + (now - sessionStartedAt.current) / 1000,
      );
      sessionStartedAt.current = now;
      const seconds = Math.floor(pendingSeconds.current);
      pendingSeconds.current -= seconds;
      const key = `${mangaId}/${chapterId}`;
      const last = lastPostedRef.current;
      if (last && last.key === key && last.page === visibleEnd && seconds === 0) return;
      lastPostedRef.current = { key, page: visibleEnd };
      void postProgress({
        mangaId,
        chapterId,
        page: visibleEnd,
        total: pages.length,
        complete: visibleEnd >= pages.length,
        sessionSeconds: seconds,
      });
    },
    [pages, aggregate, mangaId, chapterId, postProgress, spreadEndForPage],
  );
  const engine = useMekuriReader({
    pages: mekuriPages,
    mode,
    direction,
    navigation: display.navigation,
    initialPageIndex: resumePage,
    enabled: pages.length > 0,
    keyboardMap: MEKURI_HEADLESS_KEYBOARD_MAP,
    // Open drawers and dialogs own the keyboard; the dispatcher and the host
    // listener both stand down while any of them is open.
    suppressKeyboard: helpOpen || goToOpen || settingsOpen || drawerOpen,
    // The views below keep their own image URLs; this resolver only feeds
    // preloads until the views report through the engine pipeline.
    resolveImage: (pageId, attempt) => {
      const page = pages.find((candidate) => candidate.id === pageId);
      if (!page) return "";
      return `${api.readerImage(page)}${attempt > 1 ? `?retry=${attempt}` : ""}`;
    },
    onPositionSample: (position) => saveAtPage(position.pageIndex),
  });
  engineRef.current = engine;
  const index = useMekuriPageIndex(engine, resumePage);
  const menu = useMekuriHudVisible(engine, menuFallback);
  const hideChrome = useCallback(() => {
    if (engineRef.current) engineRef.current.toggleHUD(false);
    else setMenuFallback(false);
  }, []);
  const toggleChrome = useCallback(() => {
    if (engineRef.current) engineRef.current.toggleHUD();
    else setMenuFallback((value) => !value);
  }, []);
  const goToPageIndex = useCallback(
    (page: number) => {
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
  // Spread start under engine pairing for the paged slice below: the engine
  // keeps a resumed mid-spread page until the first navigation, but the slice
  // must already show its whole spread.
  let pagedIndex = index;
  const engineSnapshot = engine?.getState();
  if (engineSnapshot && mode !== "webtoon") {
    const spread = engineSnapshot.activeSpreads.find((candidate) => candidate.includes(index));
    if (spread && spread[0] !== undefined) pagedIndex = spread[0];
  }
  // The last spread is fully visible: progress counts the end of the spread
  // containing the reading position, so its end is the final page on screen.
  const visibleEnd = spreadEndForPage(index);
  const atEnd = pages.length > 0 && visibleEnd >= pages.length;
  const percent = pages.length ? Math.round((visibleEnd / pages.length) * 100) : 0;
  useEffect(() => {
    // Discrete engine moves sample immediately (same-page scroll samples
    // throttle to 1s inside the engine), so persistence rides
    // onPositionSample above; the pending write below only covers
    // unload and chapter switch.
    saver.current = () => {
      const position = engineRef.current?.getReadingPosition();
      saveAtPage(position ? position.pageIndex : resumePage);
    };
  }, [saveAtPage, resumePage]);
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
    // The attached dispatcher owns the engine map (arrows, D/A with RTL
    // inversion, M) against the live engine state. Window is the target so
    // real bubbled keys and window-dispatched key events both arrive;
    // suppression and editable-focus guards live inside the dispatcher.
    // Keyboard page turns hide the host chrome, matching the chevrons, zones,
    // and swipes; the dispatcher only moves, so the policy wraps next/prev.
    const surface = readerRef.current;
    if (!engine || !surface) return;
    const keyboardEngine: MekuriKeyboardEngine = {
      getState: (): { direction: MekuriDirection } => engine.getState(),
      next: () => {
        engine.next();
        engine.toggleHUD(false);
      },
      prev: () => {
        engine.prev();
        engine.toggleHUD(false);
      },
      toggleHUD: (force?: boolean) => engine.toggleHUD(force),
      zoomIn: () => engine.zoomIn(),
      zoomOut: () => engine.zoomOut(),
      resetZoom: () => engine.resetZoom(),
      isKeyboardSuppressed: () => engine.isKeyboardSuppressed(),
    };
    const controller = attachKeyboard({ engine: keyboardEngine, element: surface, target: window });
    return () => controller.detach();
  }, [engine]);
  useEffect(() => {
    // Host-only keys: chapter flow, fullscreen, dialogs, mode cycling, and
    // the page extras the engine map cannot express (Space with its
    // shift-for-back rule, PgDn/PgUp, Home/End). Everything the dispatcher
    // owns above stays out of here so no key ever turns twice.
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
      if (event.key === "Escape") {
        // Close the topmost layer first: help, go-to, settings, drawer.
        if (helpOpen) setHelpOpen(false);
        else if (goToOpen) setGoToOpen(false);
        else if (settingsOpen) setSettingsOpen(false);
        else if (drawerOpen) setDrawerOpen(false);
        return;
      }
      // An open dialog owns the keyboard: the reader behind it must not turn
      // pages, cycle modes, or hide its own chrome while the user is choosing.
      if (helpOpen || goToOpen || settingsOpen || drawerOpen) return;
      if (event.key.toLowerCase() === "f") toggleFullscreen();
      if (event.key === "?") {
        setHelpOpen(true);
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
      // Paged extras. Webtoon owns its own scroll keys; Space on a focused
      // button keeps its native activation so chevrons never double-fire.
      if (mode !== "webtoon") {
        if (event.key === " " && target?.tagName !== "BUTTON") {
          event.preventDefault();
          if (event.shiftKey) turnPrevious();
          else turnNext();
        } else if (event.key === "PageDown" || event.key === "PageUp") {
          event.preventDefault();
          if (event.key === "PageDown") turnNext();
          else turnPrevious();
        } else if (event.key === "Home") {
          event.preventDefault();
          goToPageIndex(0);
        } else if (event.key === "End") {
          event.preventDefault();
          goToPageIndex(pages.length - 1);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [
    drawerOpen,
    flow.next,
    flow.prev,
    goChapter,
    goToOpen,
    goToPageIndex,
    helpOpen,
    mangaId,
    mode,
    pages.length,
    saveReaderOverride,
    settingsOpen,
    toggleFullscreen,
    turnNext,
    turnPrevious,
  ]);
  const imgFilter = useMemo(() => readerImageFilter(display), [display]);
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
        img.src = `${api.readerImage(page)}${retryAttempts[page.id] ? `?retry=${retryAttempts[page.id]}` : ""}`;
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
      <div
        data-theme={display.theme}
        className="reader grid min-h-screen place-items-center bg-[var(--reader-canvas)] text-[var(--reader-text)] p-5"
      >
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
      <div
        data-theme={display.theme}
        className="reader grid min-h-screen place-items-center bg-[var(--reader-canvas)] text-[var(--reader-text)] p-5"
      >
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
      <div
        data-theme={display.theme}
        className="reader grid min-h-screen place-items-center bg-[var(--reader-canvas)] text-[var(--reader-text)]"
      >
        <LoadingState label="Loading reader" />
      </div>
    );
  if (aggregate && !pages.length)
    return (
      <div
        data-theme={display.theme}
        className="reader grid min-h-screen place-items-center bg-[var(--reader-canvas)] text-[var(--reader-text)] p-5"
      >
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
      <div
        data-theme={display.theme}
        className="reader grid min-h-screen place-items-center bg-[var(--reader-canvas)] text-[var(--reader-text)]"
      >
        <LoadingState label="Loading reader" />
      </div>
    );
  return (
    <div
      ref={readerRef}
      data-theme={display.theme}
      className="reader fixed inset-0 z-30 flex flex-col bg-[var(--reader-canvas)] text-[var(--reader-text)]"
    >
      <div
        className={`flex items-center gap-3 border-b border-[var(--reader-border)] bg-[var(--reader-bar)] px-3 py-2 ${menu ? "" : "hidden"}`}
      >
        <button
          aria-label={mangaId ? "Back to details" : "Back to library"}
          title={mangaId ? "Back to details" : "Back to library"}
          onClick={exitReader}
          className="rounded-lg p-2 text-[var(--reader-dim)] hover:bg-[var(--reader-border)]"
        >
          <ArrowLeft size={18} />
        </button>
        <div className="min-w-0 flex-1">
          <b className="block truncate text-sm">{aggregate.manga.title}</b>
          <small className="text-[var(--reader-dim)]">{chapterLabel(aggregate, chapterId)}</small>
          {incognito && (
            <span className="mt-0.5 inline-flex items-center gap-1 rounded-full bg-amber-400/15 px-2 py-0.5 text-[11px] text-amber-300">
              <EyeOff size={12} /> Incognito
            </span>
          )}
          {currentChapter?.downloaded && (
            <span
              title="This chapter is downloaded and reads offline"
              className="mt-0.5 inline-flex items-center gap-1 rounded-full bg-emerald-400/15 px-2 py-0.5 text-[11px] text-emerald-300"
            >
              <Check size={12} /> Saved
            </span>
          )}
          {!online && (
            <span
              title="No network connection; remote sources are unreachable"
              className="mt-0.5 inline-flex items-center gap-1 rounded-full bg-red-400/15 px-2 py-0.5 text-[11px] text-red-300"
            >
              No network
            </span>
          )}
        </div>
        <div className="flex gap-1" role="group" aria-label="Reader mode">
          <button
            aria-label="Chapters"
            title="Chapters"
            onClick={() => setDrawerOpen((value) => !value)}
            className={`rounded-lg p-2 ${drawerOpen ? "bg-[var(--reader-border)] text-amber-400" : "text-[var(--reader-dim)] hover:bg-[var(--reader-border)]"}`}
          >
            <List size={16} />
          </button>
          <button
            aria-pressed={mode === "single"}
            onClick={() => saveReaderOverride({ mode: "single" })}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "single" ? "bg-amber-400 text-zinc-950" : "text-[var(--reader-dim)]"}`}
          >
            Single
          </button>
          <button
            aria-pressed={mode === "double"}
            onClick={() => saveReaderOverride({ mode: "double" })}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "double" ? "bg-amber-400 text-zinc-950" : "text-[var(--reader-dim)]"}`}
          >
            Double
          </button>
          <button
            aria-pressed={mode === "webtoon"}
            onClick={() => saveReaderOverride({ mode: "webtoon" })}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "webtoon" ? "bg-amber-400 text-zinc-950" : "text-[var(--reader-dim)]"}`}
          >
            Webtoon
          </button>
          <button
            aria-label={isFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
            aria-pressed={isFullscreen}
            title={isFullscreen ? "Exit fullscreen (F)" : "Enter fullscreen (F)"}
            onClick={toggleFullscreen}
            className="rounded-lg p-2 text-[var(--reader-dim)] hover:bg-[var(--reader-border)]"
          >
            {isFullscreen ? <Minimize size={16} /> : <Maximize size={16} />}
          </button>
          <button
            aria-label="Reader settings"
            onClick={() => setSettingsOpen((value) => !value)}
            className={`rounded-lg p-2 ${settingsOpen ? "bg-[var(--reader-border)] text-amber-400" : "text-[var(--reader-dim)] hover:bg-[var(--reader-border)]"}`}
          >
            <SlidersHorizontal size={16} />
          </button>
          <button
            aria-label="Keyboard shortcuts"
            title="Keyboard shortcuts (?)"
            onClick={() => setHelpOpen(true)}
            className="rounded-lg p-2 text-[var(--reader-dim)] hover:bg-[var(--reader-border)]"
          >
            ?
          </button>
        </div>
      </div>
      {settingsOpen && (
        <div className="absolute right-3 top-14 z-40 max-h-[80vh] w-72 max-w-[90vw] space-y-4 overflow-y-auto rounded-xl border border-[var(--reader-border)] bg-[var(--reader-bar)] p-3 shadow-2xl">
          <section className="space-y-3">
            <div className="flex items-center justify-between">
              <b className="text-sm">This title</b>
              <span className="text-xs text-[var(--reader-dim)]">Overrides</span>
            </div>
            <label className="block text-xs text-[var(--reader-dim)]">
              Mode
              <select
                value={overrides.mode ?? ""}
                onChange={(event) =>
                  saveReaderOverride({ mode: (event.target.value || null) as Mode | null })
                }
                className="mt-1 w-full rounded-lg border border-[var(--reader-border)] bg-[var(--reader-canvas)] px-2 py-1 text-sm text-[var(--reader-text)]"
              >
                <option value="">Default ({globals.current.mode})</option>
                <option value="single">Single</option>
                <option value="double">Double</option>
                <option value="webtoon">Webtoon</option>
              </select>
            </label>
            <label className="block text-xs text-[var(--reader-dim)]">
              Direction
              <select
                value={overrides.direction ?? ""}
                onChange={(event) =>
                  saveReaderOverride({
                    direction: (event.target.value || null) as Direction | null,
                  })
                }
                className="mt-1 w-full rounded-lg border border-[var(--reader-border)] bg-[var(--reader-canvas)] px-2 py-1 text-sm text-[var(--reader-text)]"
              >
                <option value="">Default ({globals.current.direction})</option>
                <option value="ltr">Left to right</option>
                <option value="rtl">Right to left</option>
              </select>
            </label>
            <label className="block text-xs text-[var(--reader-dim)]">
              Fit
              <select
                value={overrides.fit ?? ""}
                onChange={(event) =>
                  saveReaderOverride({ fit: (event.target.value || null) as Fit | null })
                }
                className="mt-1 w-full rounded-lg border border-[var(--reader-border)] bg-[var(--reader-canvas)] px-2 py-1 text-sm text-[var(--reader-text)]"
              >
                <option value="">Default ({globals.current.fit})</option>
                <option value="width">Fit width</option>
                <option value="height">Fit height</option>
                <option value="screen">Fit screen</option>
                <option value="original">Original</option>
              </select>
            </label>
          </section>
          <section className="space-y-3 border-t border-[var(--reader-border)] pt-3">
            <b className="text-sm">Display</b>
            <label className="block text-xs text-[var(--reader-dim)]">
              Tap and click zones
              <select
                aria-label="Tap and click zones"
                value={display.navigation}
                onChange={(event) =>
                  saveDisplaySetting("reader.navigation", event.target.value as ReaderNavigation)
                }
                className="mt-1 w-full rounded-lg border border-[var(--reader-border)] bg-[var(--reader-canvas)] px-2 py-1 text-sm text-[var(--reader-text)]"
              >
                <option value="default">Default thirds</option>
                <option value="l">L-shaped</option>
                <option value="edge">Edges only</option>
                <option value="disabled">Disabled</option>
              </select>
            </label>
            <ZonePreview navigation={display.navigation} direction={direction} />
            <label className="block text-xs text-[var(--reader-dim)]">
              Theme
              <select
                aria-label="Reader theme"
                value={display.theme}
                onChange={(event) =>
                  saveDisplaySetting("reader.theme", event.target.value as ReaderTheme)
                }
                className="mt-1 w-full rounded-lg border border-[var(--reader-border)] bg-[var(--reader-canvas)] px-2 py-1 text-sm text-[var(--reader-text)]"
              >
                <option value="dark">Dark</option>
                <option value="amoled">AMOLED black</option>
                <option value="paper">Paper</option>
                <option value="light">Light</option>
              </select>
            </label>
            <label className="block text-xs text-[var(--reader-dim)]">
              Image brightness · {display.brightness}%
              <input
                type="range"
                aria-label="Image brightness"
                min={50}
                max={150}
                step={5}
                value={display.brightness}
                onChange={(event) =>
                  saveDisplaySetting("reader.brightness", Number(event.target.value))
                }
                className="mt-1 w-full accent-amber-400"
              />
            </label>
            <label className="block text-xs text-[var(--reader-dim)]">
              Webtoon gap · {display.gap}px
              <input
                type="range"
                aria-label="Webtoon gap"
                min={0}
                max={48}
                step={4}
                value={display.gap}
                onChange={(event) =>
                  saveDisplaySetting("reader.webtoon_gap", Number(event.target.value))
                }
                className="mt-1 w-full accent-amber-400"
              />
            </label>
            <label className="flex items-center gap-2 text-xs text-[var(--reader-text)]">
              <input
                type="checkbox"
                checked={display.grayscale}
                onChange={(event) => saveDisplaySetting("reader.grayscale", event.target.checked)}
                className="accent-amber-400"
              />
              Grayscale images
            </label>
            <label className="flex items-center gap-2 text-xs text-[var(--reader-text)]">
              <input
                type="checkbox"
                checked={display.invert}
                onChange={(event) => saveDisplaySetting("reader.invert", event.target.checked)}
                className="accent-amber-400"
              />
              Invert image colors
            </label>
          </section>
          <section className="space-y-2 border-t border-[var(--reader-border)] pt-3">
            <b className="text-sm">Reading</b>
            <label className="flex items-center gap-2 text-xs text-[var(--reader-text)]">
              <input
                type="checkbox"
                checked={autoAdvance}
                onChange={(event) => setAutoAdvanceStored(event.target.checked)}
                className="accent-amber-400"
              />
              Auto-advance to the next chapter
            </label>
            <label className="flex items-center gap-2 text-xs text-[var(--reader-text)]">
              <input
                type="checkbox"
                checked={keepAwake}
                onChange={(event) => setKeepAwakeStored(event.target.checked)}
                className="accent-amber-400"
              />
              Keep screen on while reading
            </label>
          </section>
        </div>
      )}
      {mode === "webtoon" ? (
        <Webtoon
          pages={pages}
          index={index}
          setIndex={goToPageIndex}
          gap={display.gap}
          imgFilter={imgFilter}
          retryAttempts={retryAttempts}
          onFail={reportFailed}
          onRecover={reportRecovered}
          onRetry={retryPage}
        />
      ) : (
        <Paged
          pages={pages}
          index={pagedIndex}
          double={mode === "double"}
          direction={direction}
          fit={fit}
          navigation={display.navigation}
          imgFilter={imgFilter}
          retryAttempts={retryAttempts}
          onFail={reportFailed}
          onRecover={reportRecovered}
          onRetry={retryPage}
          onNext={turnNext}
          onPrevious={turnPrevious}
          onToggleMenu={toggleChrome}
          onNavigateTurn={hideChrome}
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
          queueNote={queueNote}
          trackerCount={aggregate.trackers.length}
          onNext={() => goChapter(flow.next!.id)}
          onDownload={() => queueChapter(flow.next!.id)}
          onStay={() => setDismissedNextUp(true)}
          onDetails={exitReader}
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
      <div
        className={`flex items-center gap-3 border-t border-[var(--reader-border)] bg-[var(--reader-bar)] px-4 py-2 ${menu ? "" : "hidden"}`}
      >
        <button
          onClick={() => setGoToOpen(true)}
          title="Go to page (G)"
          aria-label={`Go to page, currently page ${Math.min(index + 1, pages.length)} of ${pages.length}, ${percent} percent read`}
          className="shrink-0 text-xs text-[var(--reader-dim)] hover:text-[var(--reader-text)]"
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
          onChange={(e) => goToPageIndex(Number(e.target.value))}
          className="flex-1 accent-amber-400"
        />
        <button
          aria-label="Hide reader menu"
          aria-expanded={menu}
          onClick={hideChrome}
          className="rounded-lg p-2 text-[var(--reader-dim)]"
        >
          <Menu size={16} />
        </button>
      </div>
      {helpOpen && <ShortcutsDialog onClose={() => setHelpOpen(false)} />}
      {!menu && (
        <button
          aria-label="Show reader menu"
          onClick={toggleChrome}
          className="absolute right-4 top-4 rounded-lg border border-[var(--reader-border)] bg-[var(--reader-bar)] p-2 text-[var(--reader-text)]"
        >
          <Menu size={17} />
        </button>
      )}
      {failedPages.length > 0 && (
        <button
          onClick={retryAllFailed}
          className={`absolute left-1/2 z-40 -translate-x-1/2 rounded-full bg-red-400 px-4 py-2 text-sm font-semibold text-zinc-950 shadow-2xl ${
            atEnd && flow.next && !dismissedNextUp ? "bottom-72" : "bottom-16"
          }`}
        >
          Retry {failedPages.length} failed {failedPages.length === 1 ? "page" : "pages"}
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
    <div className="absolute inset-y-0 left-0 z-40 flex w-72 max-w-[80vw] flex-col border-r border-[var(--reader-border)] bg-[var(--reader-bar)] text-[var(--reader-text)] shadow-2xl">
      <div className="flex items-center justify-between border-b border-[var(--reader-border)] px-3 py-2">
        <b className="text-sm">Chapters · {ordered.length}</b>
        <button
          aria-label="Close chapters"
          onClick={onClose}
          className="rounded-lg p-2 text-[var(--reader-dim)] hover:bg-[var(--reader-border)]"
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
              className={`flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-[var(--reader-border)] ${
                chapter.id === currentId ? "bg-[var(--reader-border)] text-amber-400" : ""
              } ${chapter.read ? "opacity-60" : ""}`}
            >
              {chapter.read && <Check size={14} className="shrink-0 text-emerald-400" />}
              <span className="min-w-0 flex-1 truncate">{chapterLabelFor(chapter)}</span>
              {chapter.downloaded && (
                <span className="shrink-0 text-[11px] text-[var(--reader-dim)]">saved</span>
              )}
            </button>
          </li>
        ))}
      </ol>
      <p className="border-t border-[var(--reader-border)] px-3 py-2 text-[11px] text-[var(--reader-dim)]">
        N / P jumps to the next / previous chapter.
      </p>
    </div>
  );
}

function NextUpCard({
  next,
  autoAdvance,
  queueNote,
  trackerCount,
  onNext,
  onDownload,
  onStay,
  onDetails,
}: {
  next: Chapter;
  autoAdvance: boolean;
  queueNote: string;
  trackerCount: number;
  onNext: () => void;
  onDownload: () => void;
  onStay: () => void;
  onDetails: () => void;
}) {
  return (
    <div className="absolute inset-x-0 bottom-16 z-40 mx-auto w-80 max-w-[90vw] rounded-xl border border-[var(--reader-border)] bg-[var(--reader-bar)] p-4 text-[var(--reader-text)] shadow-2xl">
      <p className="text-xs uppercase tracking-wide text-[var(--reader-dim)]">Chapter finished</p>
      <b className="mt-1 block truncate text-sm">Up next: {chapterLabelFor(next)}</b>
      {autoAdvance && (
        <p className="mt-1 text-xs text-[var(--reader-dim)]">
          Auto-advancing shortly. Stay to keep reading.
        </p>
      )}
      {trackerCount > 0 && (
        <p className="mt-1 text-xs text-[var(--reader-dim)]">
          Chapter progress syncs to {trackerCount} bound{" "}
          {trackerCount === 1 ? "tracker" : "trackers"}.
        </p>
      )}
      <div className="mt-3 flex flex-wrap gap-2">
        <button
          onClick={onNext}
          className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
        >
          Next chapter
        </button>
        {next.downloaded ? (
          <span className="inline-flex items-center gap-1 self-center text-xs text-emerald-300">
            <Check size={13} /> Saved
          </span>
        ) : (
          <button
            onClick={onDownload}
            className="rounded-lg border border-[var(--reader-border)] px-3 py-2 text-sm"
          >
            Download next
          </button>
        )}
        <button
          onClick={onStay}
          className="rounded-lg border border-[var(--reader-border)] px-3 py-2 text-sm"
        >
          Keep reading
        </button>
        <button
          onClick={onDetails}
          className="rounded-lg border border-[var(--reader-border)] px-3 py-2 text-sm text-[var(--reader-dim)]"
        >
          Details
        </button>
      </div>
      {queueNote && (
        <p role="status" className="mt-2 text-xs text-emerald-300">
          {queueNote}
        </p>
      )}
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
        className="w-64 rounded-xl border border-[var(--reader-border)] bg-[var(--reader-bar)] p-4 text-[var(--reader-text)] shadow-2xl"
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
            className="min-w-0 flex-1 rounded-lg border border-[var(--reader-border)] bg-[var(--reader-canvas)] px-2 py-1.5 text-sm"
          />
          <button
            type="submit"
            className="rounded-lg bg-amber-400 px-3 py-1.5 text-sm font-semibold text-zinc-950"
          >
            Go
          </button>
        </form>
        <p className="mt-2 text-xs text-[var(--reader-dim)]">
          Page {current} of {pageCount}
        </p>
      </div>
    </div>
  );
}

function ZonePreview({
  navigation,
  direction,
}: {
  navigation: ReaderNavigation;
  direction: Direction;
}) {
  if (navigation === "disabled") {
    return (
      <p className="text-[11px] text-[var(--reader-dim)]">Zones off. Use edges, keys, or swipe.</p>
    );
  }
  const prev = direction === "rtl" ? "Next" : "Prev";
  const next = direction === "rtl" ? "Prev" : "Next";
  return (
    <div
      aria-hidden="true"
      className="relative h-16 w-full overflow-hidden rounded-lg border border-[var(--reader-border)] bg-[var(--reader-canvas)] text-[10px] text-[var(--reader-dim)]"
    >
      {navigation === "l" && (
        <>
          <div className="absolute inset-y-0 left-0 grid w-[30%] place-items-center bg-white/5">
            {prev}
          </div>
          <div className="absolute inset-y-0 right-0 grid w-[70%] place-items-center">{next}</div>
          <div className="absolute inset-x-0 top-0 grid h-4 place-items-center bg-white/10">
            Menu
          </div>
        </>
      )}
      {navigation === "edge" && (
        <>
          <div className="absolute inset-y-0 left-0 grid w-[15%] place-items-center bg-white/5">
            {prev}
          </div>
          <div className="absolute inset-0 grid place-items-center">Menu</div>
          <div className="absolute inset-y-0 right-0 grid w-[15%] place-items-center bg-white/5">
            {next}
          </div>
        </>
      )}
      {navigation === "default" && (
        <>
          <div className="absolute inset-y-0 left-0 grid w-[20%] place-items-center bg-white/5">
            {prev}
          </div>
          <div className="absolute inset-y-0 left-[20%] right-[20%] grid place-items-center">
            Menu
          </div>
          <div className="absolute inset-y-0 right-0 grid w-[20%] place-items-center bg-white/5">
            {next}
          </div>
        </>
      )}
    </div>
  );
}

function ShortcutsDialog({ onClose }: { onClose: () => void }) {
  const rows: Array<[string, string]> = [
    ["← / → or A / D", "Previous / next spread"],
    ["Space / Shift+Space, PgDn / PgUp", "Next / previous (webtoon scrolls)"],
    ["Home / End", "First / last page"],
    ["N / P", "Next / previous chapter"],
    ["W", "Cycle single, double, webtoon"],
    ["G", "Go to page"],
    ["F", "Fullscreen"],
    ["M", "Show or hide the menu"],
    ["Double-tap / double-click center, Ctrl+wheel, pinch", "Zoom"],
    ["Swipe left / right", "Turn pages on touch screens"],
    ["?", "This list"],
    ["Esc", "Close dialogs"],
  ];
  return (
    <div
      className="absolute inset-0 z-40 grid place-items-center bg-black/70 p-5"
      onMouseDown={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Keyboard shortcuts"
        className="w-80 max-w-full rounded-xl border border-[var(--reader-border)] bg-[var(--reader-bar)] p-4 text-[var(--reader-text)] shadow-2xl"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <b className="text-sm">Shortcuts</b>
          <button
            aria-label="Close shortcuts"
            onClick={onClose}
            className="rounded-lg p-2 text-[var(--reader-dim)] hover:bg-[var(--reader-border)]"
          >
            <X size={16} />
          </button>
        </div>
        <dl className="mt-3 space-y-2 text-xs">
          {rows.map(([keys, action]) => (
            <div key={keys} className="flex items-center justify-between gap-3">
              <dt className="shrink-0 rounded bg-[var(--reader-border)] px-1.5 py-0.5 font-mono text-[11px]">
                {keys}
              </dt>
              <dd className="text-right text-[var(--reader-dim)]">{action}</dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}

function Paged({
  pages,
  index,
  double,
  direction,
  fit,
  retryAttempts,
  onFail,
  onRecover,
  onRetry,
  navigation,
  imgFilter,
  onNext,
  onPrevious,
  onToggleMenu,
  onNavigateTurn,
}: {
  pages: Page[];
  index: number;
  double: boolean;
  direction: Direction;
  fit: Fit;
  retryAttempts: Record<string, number>;
  onFail: (id: string) => void;
  onRecover: (id: string) => void;
  onRetry: (id: string) => void;
  navigation: ReaderNavigation;
  imgFilter: string;
  onNext: () => void;
  onPrevious: () => void;
  onToggleMenu: () => void;
  onNavigateTurn: () => void;
}) {
  const count = double ? 2 : 1;
  // Chevron sides stay fixed, but their actions follow the reading direction:
  // in RTL the left control advances and the right control goes back, so the
  // labels always describe what the button does. Both share the zone callbacks
  // so every page turn hides the chrome, whichever control caused it.
  // Navigation is engine-relative (next/prev move one spread under the
  // engine's own pairing); the index prop only positions the slice.
  const goPrevious = () => {
    onPrevious();
  };
  const goNext = () => {
    onNext();
  };
  const leftAction = direction === "rtl" ? goNext : goPrevious;
  const rightAction = direction === "rtl" ? goPrevious : goNext;
  const [zoomed, setZoomed] = useState(false);
  const zoomRef = useRef<ReactZoomPanPinchRef | null>(null);
  const touchStart = useRef<{ x: number; y: number } | null>(null);
  const swiped = useRef(false);
  // A new spread always opens unzoomed; leftover pan state would strand the
  // reader on an empty viewport corner.
  useEffect(() => {
    swiped.current = false;
    setZoomed(false);
    void zoomRef.current?.resetTransform(0);
  }, [index, double]);
  const fitClass =
    fit === "height"
      ? "max-h-full w-auto object-contain"
      : fit === "screen"
        ? "max-h-full max-w-full object-contain"
        : fit === "original"
          ? "max-h-none max-w-none object-contain"
          : double
            ? "h-auto max-w-[calc(50%-0.5rem)] object-contain"
            : "h-auto max-w-full object-contain";
  const onTouchStart = (event: React.TouchEvent) => {
    const touch = event.touches[0];
    touchStart.current = { x: touch.clientX, y: touch.clientY };
    swiped.current = false;
  };
  const onTouchEnd = (event: React.TouchEvent) => {
    const start = touchStart.current;
    touchStart.current = null;
    if (!start || zoomed) return;
    const touch = event.changedTouches[0];
    const dx = touch.clientX - start.x;
    const dy = touch.clientY - start.y;
    if (Math.abs(dx) > 48 && Math.abs(dx) > Math.abs(dy) * 1.5) {
      swiped.current = true;
      // A leftward swipe advances in LTR and retreats in RTL. Swipes are the
      // touch-immersive path, so the chrome gets out of the way.
      const forward = direction === "ltr" ? dx < 0 : dx > 0;
      if (forward) goNext();
      else goPrevious();
    }
  };
  // A swipe is followed by a synthetic click; without suppression the swipe
  // would also trigger the zone underneath the finger.
  const suppressAfterSwipe = (event: React.SyntheticEvent) => {
    if (swiped.current) {
      event.preventDefault();
      event.stopPropagation();
      swiped.current = false;
    }
  };
  return (
    <div
      onTouchStart={onTouchStart}
      onTouchEnd={onTouchEnd}
      className="relative flex min-h-0 flex-1 items-stretch justify-center overflow-auto bg-[var(--reader-canvas)]"
    >
      <TransformWrapper
        ref={zoomRef}
        minScale={1}
        maxScale={4}
        limitToBounds
        centerZoomedOut
        wheel={{ activationKeys: ["Control"] }}
        doubleClick={{ mode: "toggle", excluded: ["reader-zone"] }}
        panning={{ excluded: ["reader-zone"] }}
        onTransform={(_, state) => setZoomed(state.scale > 1.01)}
      >
        <TransformComponent
          wrapperClass="!h-full !w-full"
          contentClass={`flex min-h-full min-w-full items-center justify-center gap-2 p-2 sm:p-6 ${direction === "rtl" ? "flex-row-reverse" : ""}`}
        >
          {pages.slice(index, index + count).map((page, offset) => (
            <PageImage
              key={page.index}
              page={page}
              alt={`Page ${index + offset + 1}`}
              priority={offset === 0}
              attempt={retryAttempts[page.id] ?? 0}
              onFail={onFail}
              onRecover={onRecover}
              onRetry={onRetry}
              className={fitClass}
              style={{ filter: imgFilter }}
            />
          ))}
        </TransformComponent>
      </TransformWrapper>
      {!zoomed && navigation !== "disabled" && (
        <ZoneLayer
          navigation={navigation}
          direction={direction}
          onPrevious={direction === "rtl" ? goNext : goPrevious}
          onNext={direction === "rtl" ? goPrevious : goNext}
          onToggleMenu={onToggleMenu}
          onNavigateTurn={onNavigateTurn}
          onZoomToggle={() => {
            // The gesture toggles: a second double tap while the spread is
            // zoomed resets it instead of zooming further.
            if (zoomed) void zoomRef.current?.resetTransform();
            else void zoomRef.current?.zoomIn();
          }}
          onGuardClick={suppressAfterSwipe}
        />
      )}
      <button
        aria-label={direction === "rtl" ? "Next page" : "Previous page"}
        onClick={leftAction}
        className="absolute left-2 top-1/2 z-10 rounded-full bg-black/60 p-3 text-white"
      >
        <ChevronLeft />
      </button>
      <button
        aria-label={direction === "rtl" ? "Previous page" : "Next page"}
        onClick={rightAction}
        className="absolute right-2 top-1/2 z-10 rounded-full bg-black/60 p-3 text-white"
      >
        <ChevronRight />
      </button>
      <div className="absolute bottom-2 right-2 z-10 flex gap-1" role="group" aria-label="Zoom">
        <button
          aria-label="Zoom out"
          onClick={() => void zoomRef.current?.zoomOut()}
          className="rounded-full bg-black/60 p-2 text-white"
        >
          <ZoomOut size={16} />
        </button>
        <button
          aria-label="Zoom in"
          onClick={() => void zoomRef.current?.zoomIn()}
          className="rounded-full bg-black/60 p-2 text-white"
        >
          <ZoomIn size={16} />
        </button>
        {zoomed && (
          <button
            aria-label="Reset zoom"
            onClick={() => void zoomRef.current?.resetTransform()}
            className="rounded-full bg-black/60 px-2 py-2 text-xs font-semibold text-white"
          >
            1×
          </button>
        )}
      </div>
    </div>
  );
}

// ZoneLayer maps taps and clicks to navigation without delaying them: side
// zones act immediately, the center zone resolves its own tap pair so that a
// single tap toggles the menu and a double tap zooms. Zones hide while zoomed
// so pan and pinch gestures reach the image instead of navigating away.
//
// Touch input produces no dblclick, so the pair is detected from the click
// stream instead: the first tap holds the menu toggle back for DOUBLE_TAP_MS
// (which doubles as the double tap window, keeping the pending toggle
// cancellable), and a second tap inside that window and the slop cancels the
// toggle and zooms.
const DOUBLE_TAP_MS = 300;
const DOUBLE_TAP_SLOP = 30;
function ZoneLayer({
  navigation,
  direction,
  onPrevious,
  onNext,
  onToggleMenu,
  onNavigateTurn,
  onZoomToggle,
  onGuardClick,
}: {
  navigation: ReaderNavigation;
  direction: Direction;
  onPrevious: () => void;
  onNext: () => void;
  onToggleMenu: () => void;
  onNavigateTurn: () => void;
  onZoomToggle: () => void;
  onGuardClick: (event: React.SyntheticEvent) => void;
}) {
  const menuTimer = useRef(0);
  const lastTap = useRef<{ at: number; x: number; y: number } | null>(null);
  useEffect(() => () => window.clearTimeout(menuTimer.current), []);
  const centerTap = (event: React.MouseEvent<HTMLButtonElement>) => {
    const now = Date.now();
    const previous = lastTap.current;
    window.clearTimeout(menuTimer.current);
    if (
      previous &&
      now - previous.at < DOUBLE_TAP_MS &&
      Math.abs(event.clientX - previous.x) <= DOUBLE_TAP_SLOP &&
      Math.abs(event.clientY - previous.y) <= DOUBLE_TAP_SLOP
    ) {
      // Consume the pair so a third tap starts a fresh sequence.
      lastTap.current = null;
      onZoomToggle();
      return;
    }
    lastTap.current = { at: now, x: event.clientX, y: event.clientY };
    menuTimer.current = window.setTimeout(onToggleMenu, DOUBLE_TAP_MS);
  };
  const prevLabel = direction === "rtl" ? "Next page" : "Previous page";
  const nextLabel = direction === "rtl" ? "Previous page" : "Next page";
  // Zone labels stay distinct from the chevron labels so assistive tech
  // announces each control exactly once.
  const prevZoneLabel = `Tap zone: ${prevLabel.toLowerCase()}`;
  const nextZoneLabel = `Tap zone: ${nextLabel.toLowerCase()}`;
  const side = navigation === "edge" ? "w-[15%]" : navigation === "l" ? "w-[30%]" : "w-[20%]";
  // The L preset has no center zone: the top strip toggles the menu and the
  // whole area right of the previous zone advances, matching ZonePreview.
  const centerInset = navigation === "edge" ? "left-[15%] right-[15%]" : "left-[20%] right-[20%]";
  return (
    <div className="absolute inset-0 z-[5]">
      {navigation === "l" && (
        <button
          aria-label="Tap zone: toggle menu"
          onClick={(event) => {
            onGuardClick(event);
            onToggleMenu();
          }}
          className="reader-zone absolute inset-x-0 top-0 h-12"
        />
      )}
      <button
        aria-label={prevZoneLabel}
        onClick={(event) => {
          onGuardClick(event);
          onPrevious();
        }}
        className={`reader-zone group absolute inset-y-0 left-0 ${side} ${navigation === "l" ? "top-12" : ""}`}
      >
        <span className="absolute left-1 top-1/2 -translate-y-1/2 text-white/0 group-hover:text-white/70">
          <ChevronLeft size={20} />
        </span>
      </button>
      <button
        aria-label={nextZoneLabel}
        onClick={(event) => {
          onGuardClick(event);
          onNext();
        }}
        className={`reader-zone group absolute inset-y-0 right-0 ${navigation === "l" ? "w-[70%]" : side} ${navigation === "l" ? "top-12" : ""}`}
      >
        <span className="absolute right-1 top-1/2 -translate-y-1/2 text-white/0 group-hover:text-white/70">
          <ChevronRight size={20} />
        </span>
      </button>
      {navigation !== "l" && (
        <button
          aria-label="Tap zone: toggle menu"
          onClick={centerTap}
          className={`reader-zone absolute inset-y-0 ${centerInset}`}
        />
      )}
    </div>
  );
}
function Webtoon({
  pages,
  index,
  setIndex,
  gap,
  imgFilter,
  retryAttempts,
  onFail,
  onRecover,
  onRetry,
}: {
  pages: Page[];
  index: number;
  setIndex: (value: number) => void;
  gap: number;
  imgFilter: string;
  retryAttempts: Record<string, number>;
  onFail: (id: string) => void;
  onRecover: (id: string) => void;
  onRetry: (id: string) => void;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const initialIndex = useRef(index);
  const indexRef = useRef(index);
  indexRef.current = index;
  const virtualizer = useVirtualizer({
    count: pages.length,
    getScrollElement: () => parent.current,
    estimateSize: () => 720,
    overscan: 4,
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
  // Keyboard scrolling lives here because the scroll container does: the
  // reader-level shortcuts skip these keys in webtoon mode.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable ||
          (target.tagName === "BUTTON" && event.key === " "))
      ) {
        return;
      }
      const el = parent.current;
      if (!el) return;
      const jump = Math.max(1, Math.floor(el.clientHeight * 0.75));
      if (event.key === " " || event.key === "PageDown" || event.key === "PageUp") {
        event.preventDefault();
        el.scrollBy({
          top: event.key === "PageUp" || event.shiftKey ? -jump : jump,
          behavior: "smooth",
        });
      } else if (event.key === "Home") {
        event.preventDefault();
        el.scrollTo({ top: 0 });
      } else if (event.key === "End") {
        event.preventDefault();
        el.scrollTo({ top: el.scrollHeight });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return (
    <div ref={parent} className="min-h-0 flex-1 overflow-y-auto bg-[var(--reader-canvas)]">
      <div className="relative mx-auto max-w-3xl" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((item) => (
          <div
            key={item.key}
            ref={virtualizer.measureElement}
            data-index={item.index}
            className="absolute left-0 w-full px-2"
            style={{ transform: `translateY(${item.start}px)`, paddingBottom: gap }}
          >
            <div style={{ filter: imgFilter }}>
              <PageImage
                page={pages[item.index]}
                alt={`Page ${item.index + 1}`}
                className="w-full rounded-sm"
                loading="lazy"
                attempt={retryAttempts[pages[item.index].id] ?? 0}
                onFail={onFail}
                onRecover={onRecover}
                onRetry={onRetry}
              />
            </div>
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
// PageImage is memoized: paged spreads re-render on every index change, and
// only the images whose props changed may re-render with it.
const PageImage = memo(function PageImage({
  page,
  alt,
  className,
  style,
  loading,
  priority,
  attempt,
  onFail,
  onRecover,
  onRetry,
}: {
  page: Page;
  alt: string;
  className: string;
  style?: CSSProperties;
  loading?: "lazy" | "eager";
  priority?: boolean;
  attempt: number;
  onFail: (id: string) => void;
  onRecover: (id: string) => void;
  onRetry: (id: string) => void;
}) {
  // Failure is per mount: a page that comes back after a spread change loads
  // again and clears the badge when it succeeds. The retry count is the part
  // that has to survive the unmount, so a page retried while off screen still
  // reloads with the cache-busting URL.
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (attempt > 0) setFailed(false);
  }, [attempt]);
  if (failed)
    return (
      <button
        onClick={() => onRetry(page.id)}
        className={`grid h-64 w-full place-items-center rounded-sm border border-zinc-800 bg-zinc-900 text-sm text-zinc-300 ${className}`}
      >
        Retry page
      </button>
    );
  return (
    <img
      src={`${api.readerImage(page)}${attempt ? `?retry=${attempt}` : ""}`}
      alt={alt}
      onError={() => {
        setFailed(true);
        onFail(page.id);
      }}
      onLoad={() => onRecover(page.id)}
      className={className}
      style={style}
      loading={loading ?? (priority ? "eager" : undefined)}
      decoding="async"
      fetchPriority={priority ? "high" : "auto"}
    />
  );
});
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
