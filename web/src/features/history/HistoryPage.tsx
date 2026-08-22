import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ChevronRight } from "lucide-react";
import { api } from "../../api";
import type { Chapter, LibraryManga, Progress } from "../../types";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";

export function HistoryPage() {
  const [items, setItems] = useState<
    Array<{ manga: LibraryManga; chapter: Chapter; progress: Progress }>
  >([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    api
      .history()
      .then(setItems)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);
  return (
    <div className="mx-auto max-w-5xl p-5 sm:p-8">
      <PageHeader eyebrow="Recently read" title="History" />
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Loading history" />
      ) : items.length ? (
        <div className="divide-y divide-zinc-800 rounded-xl border border-zinc-800 bg-zinc-900/50">
          {items.map((item) => (
            <Link
              key={item.manga.id}
              to={`/reader/${encodeURIComponent(item.manga.sourceId)}/${encodeURIComponent(item.manga.sourceMangaId)}/${encodeURIComponent(item.chapter.sourceChapterId)}`}
              className="flex items-center gap-4 p-4 hover:bg-zinc-900"
            >
              <div className="size-14 overflow-hidden rounded-lg bg-zinc-800">
                {item.manga.coverUrl && (
                  <img src={item.manga.coverUrl} alt="" className="size-full object-cover" />
                )}
              </div>
              <span className="min-w-0 flex-1">
                <b className="block truncate">{item.manga.title}</b>
                <small className="text-zinc-500">
                  {item.chapter.chapterNumber == null
                    ? item.chapter.title || "Special"
                    : `Chapter ${item.chapter.chapterNumber}`}{" "}
                  · page {item.progress.lastReadPage} of {item.progress.totalPages}
                </small>
              </span>
              <ChevronRight size={17} className="text-zinc-600" />
            </Link>
          ))}
        </div>
      ) : (
        <EmptyState
          title="No reading history"
          text="Progress appears here after you read a chapter."
        />
      )}
    </div>
  );
}
