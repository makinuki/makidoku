import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useParams, useSearchParams, useNavigate, Navigate } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  ArrowLeft,
  ChevronLeft,
  ChevronRight,
  EyeOff,
  Maximize,
  Menu,
  Minimize,
  SlidersHorizontal,
} from "lucide-react";
import { api } from "../../api";
import type { Aggregate, Page } from "../../types";
import { ErrorState, EmptyState, LoadingState } from "../../components/States";
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
        setIndex(saved);
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
  const saver = useRef<(() => void) | null>(null);
  useEffect(() => {
    if (!pages.length || !aggregate) return;
    const visibleEnd = Math.min(pages.length, index + spreadStep(mode));
    const save = () => {
      saver.current = null;
      const elapsed = Math.floor((Date.now() - sessionStartedAt.current) / 1000);
      sessionStartedAt.current = Date.now();
      api
        .progress(
          mangaId,
          chapterId,
          visibleEnd,
          pages.length,
          visibleEnd >= pages.length,
          Math.min(300, Math.max(0, elapsed)),
        )
        .catch((e) => console.error("saving reading progress failed", e));
    };
    saver.current = save;
    const timer = window.setTimeout(save, 500);
    return () => window.clearTimeout(timer);
  }, [index, mode, pages.length, aggregate, mangaId, chapterId]);
  useEffect(() => {
    // A pending write is flushed when leaving the reader or switching
    // chapters so the debounce window cannot lose the final position.
    return () => {
      if (saver.current) {
        saver.current();
        saver.current = null;
      }
    };
  }, [mangaId, chapterId]);
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
  }, [direction, mangaId, mode, pages.length, saveReaderOverride, toggleFullscreen]);
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
      <div
        className={`flex items-center gap-3 border-t border-zinc-800 bg-zinc-950 px-4 py-2 ${menu ? "" : "hidden"}`}
      >
        <span className="text-xs text-zinc-500" aria-hidden="true">
          {Math.min(index + 1, pages.length)} / {pages.length}
        </span>
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
  const interacted = useRef(false);
  const virtualizer = useVirtualizer({
    count: pages.length,
    getScrollElement: () => parent.current,
    estimateSize: () => 720,
    overscan: 2,
  });
  // Entering webtoon mode aligns the scroll position with the restored page
  // before the reader interacts. The visible page is adopted only afterwards:
  // a programmatic alignment must never overwrite the reading position with
  // page one.
  useLayoutEffect(() => {
    virtualizer.scrollToIndex(initialIndex.current);
    // Alignment runs once per mount; later index changes come from scrolling.
  }, []);
  useEffect(() => {
    if (!interacted.current) return;
    const first = virtualizer.getVirtualItems()[0];
    if (first && first.index !== index) setIndex(first.index);
  }, [virtualizer, index, setIndex]);
  useEffect(() => {
    const el = parent.current;
    if (!el) return;
    const mark = () => {
      interacted.current = true;
    };
    // Scroll events also fire for the programmatic alignment above, so the
    // interaction latch listens to input events instead. A scrollbar drag is
    // covered by its pointerdown.
    el.addEventListener("wheel", mark, { passive: true });
    el.addEventListener("touchmove", mark, { passive: true });
    el.addEventListener("pointerdown", mark);
    return () => {
      el.removeEventListener("wheel", mark);
      el.removeEventListener("touchmove", mark);
      el.removeEventListener("pointerdown", mark);
    };
  }, []);
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
