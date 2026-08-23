import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { BookOpen, Search } from "lucide-react";
import { api } from "../../api";
import type { Category, LibraryManga } from "../../types";
import {
  EmptyState,
  ErrorState,
  LoadingState,
  PageHeader,
} from "../../components/States";

export function LibraryPage() {
  const [items, setItems] = useState<LibraryManga[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<number>();
  const [sort, setSort] = useState<"recent" | "title">("recent");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    void api
      .categories()
      .then(setCategories)
      .catch((e) => setError(e.message));
  }, []);
  useEffect(() => {
    setLoading(true);
    setError("");
    api
      .library(query, category)
      .then(setItems)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [query, category]);
  const visible = useMemo(
    () =>
      [...items].sort((a, b) =>
        sort === "title"
          ? a.title.localeCompare(b.title)
          : b.updatedAt - a.updatedAt,
      ),
    [items, sort],
  );
  const filters: Array<[number | undefined, string]> = [
    [undefined, "All"],
    ...categories.map((item) => [item.id, item.name] as [number, string]),
  ];
  return (
    <div className="mx-auto max-w-7xl p-5 sm:p-8">
      <PageHeader title="Library">
        <select
          value={sort}
          onChange={(e) => setSort(e.target.value as typeof sort)}
          className="rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-sm"
        >
          <option value="recent">Recently updated</option>
          <option value="title">Title</option>
        </select>
      </PageHeader>
      <div className="mb-6 flex flex-wrap gap-3">
        <label className="flex min-w-60 flex-1 items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2">
          <Search size={16} className="text-zinc-500" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search your library"
            className="min-w-0 flex-1 bg-transparent text-sm outline-none"
          />
        </label>
        <div className="flex gap-2 overflow-x-auto">
          {filters.map(([id, label]) => (
            <button
              key={label}
              onClick={() => setCategory(id)}
              className={`rounded-full border px-3 py-2 text-xs font-medium ${category === id ? "border-amber-400 bg-amber-400 text-zinc-950" : "border-zinc-800 text-zinc-400 hover:border-zinc-600"}`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Loading library" />
      ) : visible.length ? (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
          {visible.map((item) => (
            <LibraryCard key={item.id} item={item} />
          ))}
        </div>
      ) : (
        <EmptyState
          title="Your library is empty"
          text="Browse an installed source and save a title to begin."
          action={
            <Link
              to="/browse"
              className="mt-3 rounded-lg bg-amber-400 px-4 py-2 text-sm font-semibold text-zinc-950"
            >
              Browse sources
            </Link>
          }
        />
      )}
    </div>
  );
}

function LibraryCard({ item }: { item: LibraryManga }) {
  const progress =
    item.progress && item.progress.totalPages
      ? Math.round(
          (item.progress.lastReadPage / item.progress.totalPages) * 100,
        )
      : 0;
  return (
    <Link
      to={`/manga/${encodeURIComponent(item.id)}`}
      className="group min-w-0"
    >
      <div className="aspect-3/4 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900">
        {item.coverUrl ? (
          <img
            src={item.coverUrl}
            alt=""
            className="size-full object-cover transition duration-200 group-hover:scale-105"
            loading="lazy"
          />
        ) : (
          <div className="grid size-full place-items-center text-zinc-600">
            <BookOpen size={30} />
          </div>
        )}
      </div>
      <h2 className="mt-2 truncate text-sm font-semibold group-hover:text-amber-300">
        {item.title}
      </h2>
      <p className="mt-1 truncate text-xs text-zinc-500">
        {item.sourceId} · {item.status || "Unknown"}
      </p>
      {progress > 0 && (
        <div className="mt-2 h-1 rounded-full bg-zinc-800">
          <span
            className="block h-full rounded-full bg-amber-400"
            style={{ width: `${progress}%` }}
          />
        </div>
      )}
    </Link>
  );
}
