import { useEffect, useRef, useState } from "react";
import { useParams, useSearchParams, useNavigate } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowLeft, ChevronLeft, ChevronRight, Maximize, Menu } from "lucide-react";
import { api } from "../../api";
import type { Aggregate, Page } from "../../types";
import { ErrorState, LoadingState } from "../../components/States";

type Mode = "single" | "double" | "webtoon";
export function ReaderPage() {
  const { sourceId: routeSource, mangaId: routeManga, chapterId: routeChapter } = useParams();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  let sourceId = "";
  let mangaId = "";
  let chapterId = "";
  if (routeSource && routeManga && routeChapter) {
    sourceId = decodeURIComponent(routeSource);
    mangaId = decodeURIComponent(routeManga);
    chapterId = decodeURIComponent(routeChapter);
  } else {
    const mangaParam = searchParams.get("manga") || "";
    const chapterParam = searchParams.get("chapter") || "";
    if (mangaParam.includes(":")) {
      const idx = mangaParam.indexOf(":");
      sourceId = mangaParam.slice(0, idx);
      mangaId = mangaParam.slice(idx + 1);
    } else {
      mangaId = mangaParam;
    }
    if (chapterParam) {
      if (chapterParam.includes(":")) {
        chapterId = chapterParam.split(":").pop() || chapterParam;
        if (!sourceId && chapterParam.includes(":")) {
          const firstIdx = chapterParam.indexOf(":");
          const possibleSource = chapterParam.slice(0, firstIdx);
          if (!sourceId && possibleSource) sourceId = possibleSource;
        }
      } else {
        chapterId = chapterParam;
      }
    }
  }

  const [aggregate, setAggregate] = useState<Aggregate>();
  const [pages, setPages] = useState<Page[]>([]);
  const [mode, setMode] = useState<Mode>("single");
  const [index, setIndex] = useState(0);
  const [menu, setMenu] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!sourceId || !mangaId || !chapterId) return;
    let active = true;
    (async () => {
      try {
        const data = await api.manga(sourceId, mangaId);
        const chapter = data.chapters.find((item) => item.sourceChapterId === chapterId);
        if (!chapter) throw new Error("Chapter is not available");
        const loaded = await api.pages(data.manga.sourceId, chapter.sourceChapterId);
        if (!active) return;
        setAggregate(data);
        setPages(loaded);
        const savedChapterSourceId = data.progress?.lastReadChapterId.split(":").pop() || "";
        const saved =
          savedChapterSourceId === chapterId ? (data.progress?.lastReadPage ?? 1) - 1 : 0;
        setIndex(Math.max(0, Math.min(saved, loaded.length - 1)));
      } catch (e) {
        if (active) setError(e instanceof Error ? e.message : "Unable to open chapter");
      }
    })();
    return () => {
      active = false;
    };
  }, [sourceId, mangaId, chapterId]);
  useEffect(() => {
    if (!pages.length || !aggregate) return;
    const visibleEnd = Math.min(pages.length, index + (mode === "double" ? 2 : 1));
    const timer = window.setTimeout(() => {
      void api.progress(sourceId, mangaId, chapterId, visibleEnd, pages.length, visibleEnd >= pages.length);
    }, 500);
    return () => window.clearTimeout(timer);
  }, [index, mode, pages.length, aggregate, sourceId, mangaId, chapterId]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
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
        <Webtoon
          pages={pages}
          source={aggregate.manga.sourceId}
          index={index}
          setIndex={setIndex}
        />
      ) : (
        <Paged
          pages={pages}
          source={aggregate.manga.sourceId}
          index={index}
          setIndex={setIndex}
          double={mode === "double"}
        />
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
  source,
  index,
  setIndex,
  double,
}: {
  pages: Page[];
  source: string;
  index: number;
  setIndex: (value: number) => void;
  double: boolean;
}) {
  const count = double ? 2 : 1;
  return (
    <div className="relative flex min-h-0 flex-1 items-center justify-center gap-2 overflow-hidden bg-black p-2 sm:p-6">
      {pages.slice(index, index + count).map((page, offset) => (
        <img
          key={page.index}
          src={api.readerImage(source, page)}
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
  source,
  index,
  setIndex,
}: {
  pages: Page[];
  source: string;
  index: number;
  setIndex: (value: number) => void;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: pages.length,
    getScrollElement: () => parent.current,
    estimateSize: () => 720,
    overscan: 2,
  });
  useEffect(() => {
    const first = virtualizer.getVirtualItems()[0];
    if (first && first.index !== index) setIndex(first.index);
  }, [virtualizer, index, setIndex]);
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
            <img
              src={api.readerImage(source, pages[item.index])}
              alt={`Page ${item.index + 1}`}
              className="w-full rounded-sm"
            />
          </div>
        ))}
      </div>
    </div>
  );
}
function chapterLabel(data: Aggregate, sourceChapterId: string) {
  const item = data.chapters.find((chapter) => chapter.sourceChapterId === sourceChapterId);
  return item?.chapterNumber == null ? item?.title || "Special" : `Chapter ${item.chapterNumber}`;
}
