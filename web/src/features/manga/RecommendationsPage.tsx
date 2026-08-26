import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { api } from "../../api";
import type { Recommendation } from "../../types";
import { CoverImg } from "../../components/CoverImg";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";

export function RecommendationsPage() {
  const { mangaId = "" } = useParams();
  const decoded = decodeURIComponent(mangaId);
  const [title, setTitle] = useState("");
  const [items, setItems] = useState<Recommendation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    Promise.all([api.manga(decoded), api.suggestions(decoded)])
      .then(([aggregate, recommendations]) => {
        if (!active) return;
        setTitle(aggregate.manga.title);
        setItems(recommendations);
      })
      .catch((value) => {
        if (active)
          setError(value instanceof Error ? value.message : "Unable to load recommendations");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [decoded]);
  if (loading) return <LoadingState label="Loading recommendations" />;
  return (
    <div className="mx-auto max-w-7xl p-5 sm:p-8">
      <Link
        to={`/manga/${encodeURIComponent(decoded)}`}
        className="mb-5 inline-flex items-center gap-2 text-sm text-zinc-400 hover:text-white"
      >
        <ArrowLeft size={16} /> Back to title
      </Link>
      <PageHeader
        eyebrow="AniList recommendations"
        title={title ? `Recommendations for ${title}` : "Recommendations"}
      />
      {error && <ErrorState message={error} />}
      {!error && items.length === 0 ? (
        <EmptyState
          title="No recommendations available"
          text="This tracker did not return any related titles."
        />
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
          {items.map((item) => (
            <article key={item.remoteId} className="min-w-0">
              <div className="aspect-3/4 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900">
                <CoverImg src={item.coverUrl} className="size-full object-cover" />
              </div>
              <h2 className="mt-2 line-clamp-2 text-sm font-semibold">{item.title}</h2>
              {item.score != null && (
                <p className="mt-1 text-xs text-zinc-500">Score {item.score.toFixed(1)}</p>
              )}
            </article>
          ))}
        </div>
      )}
    </div>
  );
}
