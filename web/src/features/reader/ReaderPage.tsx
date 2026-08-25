import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useParams, useSearchParams, useNavigate } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowLeft, ChevronLeft, ChevronRight, Maximize, Menu } from "lucide-react";
import { api } from "../../api";
import type { Aggregate, Page } from "../../types";
import { ErrorState, EmptyState, LoadingState } from "../../components/States";

type Mode = "single" | "double" | "webtoon";
export function ReaderPage() {
  const { mangaId: routeManga, chapterId: routeChapter } = useParams();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  let mangaId = "";
  let chapterId = "";
  if (routeManga && routeChapter) {
    mangaId = decodeURIComponent(routeManga);
    chapterId = decodeURIComponent(routeChapter);
  } else {
    const mangaParam = searchParams.get("manga") || "";
    const chapterParam = searchParams.get("chapter") || "";
    mangaId = mangaParam;
    chapterId = chapterParam;
  }

  const [aggregate, setAggregate] = useState<Aggregate>();
  const [pages, setPages] = useState<Page[]>([]);
  const [mode, setMode] = useState<Mode>("single");
  const [index, setIndex] = useState(0);
  const [menu, setMenu] = useState(true);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
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
        const saved = resumeIndex(
          data.progress?.lastReadChapterId === chapterId
            ? (data.progress?.lastReadPage ?? null)
            : null,
          loaded.length,
          mode,
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
    const visibleEnd = Math.min(pages.length, index + (mode === "double" ? 2 : 1));
    const save = () => {
      saver.current = null;
      api
        .progress(mangaId, chapterId, visibleEnd, pages.length, visibleEnd >= pages.length)
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
      if (event.key.toLowerCase() === "w")
        setMode((value) =>
          value === "single" ? "double" : value === "double" ? "webtoon" : "single",
        );
      const step = mode === "double" ? 2 : 1;
      if (event.key === "ArrowRight" || event.key.toLowerCase() === "d")
        setIndex((value) => Math.min(Math.max(pages.length - 1, 0), value + step));
      if (event.key === "ArrowLeft" || event.key.toLowerCase() === "a")
        setIndex((value) => Math.max(0, value - step));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [mode, pages.length]);
  if (error)
    return (
      <div className="grid min-h-screen place-items-center bg-zinc-950 p-5">
        <div className="max-w-lg">
          <ErrorState message={error} />
          <div className="mt-4 flex gap-2">
            <button
              onClick={() => navigate(-1)}
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
        <EmptyState
          title="No chapter selected"
          text="Open a chapter from a title's details page to start reading."
        />
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
            onClick={() => navigate(-1)}
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
    <div className="fixed inset-0 z-30 flex flex-col bg-black">
      <div
        className={`flex items-center gap-3 border-b border-zinc-800 bg-zinc-950 px-3 py-2 ${menu ? "" : "hidden"}`}
      >
        <button
          aria-label="Back"
          onClick={() => navigate(-1)}
          className="rounded-lg p-2 text-zinc-300 hover:bg-zinc-800"
        >
          <ArrowLeft size={18} />
        </button>
        <div className="min-w-0 flex-1">
          <b className="block truncate text-sm">{aggregate.manga.title}</b>
          <small className="text-zinc-500">{chapterLabel(aggregate, chapterId)}</small>
        </div>
        <div className="flex gap-1">
          <button
            onClick={() => setMode("single")}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "single" ? "bg-amber-400 text-zinc-950" : "text-zinc-400"}`}
          >
            Single
          </button>
          <button
            onClick={() => setMode("double")}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "double" ? "bg-amber-400 text-zinc-950" : "text-zinc-400"}`}
          >
            Double
          </button>
          <button
            onClick={() => setMode("webtoon")}
            className={`rounded-lg px-2 py-1 text-xs ${mode === "webtoon" ? "bg-amber-400 text-zinc-950" : "text-zinc-400"}`}
          >
            Webtoon
          </button>
          <button
            aria-label="Fullscreen"
            onClick={() =>
              document.fullscreenElement
                ? void document.exitFullscreen()
                : void document.documentElement.requestFullscreen()
            }
            className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800"
          >
            <Maximize size={16} />
          </button>
        </div>
      </div>
      {mode === "webtoon" ? (
        <Webtoon pages={pages} index={index} setIndex={setIndex} />
      ) : (
        <Paged pages={pages} index={index} setIndex={setIndex} double={mode === "double"} />
      )}
      <div
        className={`flex items-center gap-3 border-t border-zinc-800 bg-zinc-950 px-4 py-2 ${menu ? "" : "hidden"}`}
      >
        <span className="text-xs text-zinc-500">
          {Math.min(index + 1, pages.length)} / {pages.length}
        </span>
        <input
          type="range"
          min="0"
          max={Math.max(0, pages.length - 1)}
          value={index}
          onChange={(e) => setIndex(Number(e.target.value))}
          className="flex-1 accent-amber-400"
        />
        <button
          aria-label="Toggle menu"
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
}: {
  pages: Page[];
  index: number;
  setIndex: (value: number) => void;
  double: boolean;
}) {
  const count = double ? 2 : 1;
  return (
    <div className="relative flex min-h-0 flex-1 items-center justify-center gap-2 overflow-hidden bg-black p-2 sm:p-6">
      {pages.slice(index, index + count).map((page, offset) => (
        <PageImage
          key={page.index}
          page={page}
          alt={`Page ${index + offset + 1}`}
          className="max-h-full max-w-[calc(50%-0.5rem)] object-contain"
        />
      ))}
      <button
        aria-label="Previous page"
        onClick={() => setIndex(Math.max(0, index - count))}
        className="absolute left-2 top-1/2 rounded-full bg-black/60 p-3 text-white"
      >
        <ChevronLeft />
      </button>
      <button
        aria-label="Next page"
        onClick={() => setIndex(Math.min(pages.length - 1, index + count))}
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
// caller: paged and webtoon modes fit images to the screen differently.
function PageImage({
  page,
  alt,
  className,
  loading,
}: {
  page: Page;
  alt: string;
  className: string;
  loading?: "lazy" | "eager";
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
      src={`/api/pages/${encodeURIComponent(page.id)}/image${attempt ? `?retry=${attempt}` : ""}`}
      alt={alt}
      onError={() => setFailed(true)}
      className={className}
      loading={loading}
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
  const zeroBased = Math.min(Math.max(0, (lastReadPage ?? 1) - 1), Math.max(0, pageCount - 1));
  return mode === "double" ? zeroBased - (zeroBased % 2) : zeroBased;
}
