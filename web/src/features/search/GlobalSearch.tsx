import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ChevronRight, Search, X } from "lucide-react";
import { api } from "../../api";
import type { LibraryManga } from "../../types";
import { CoverImg } from "../../components/CoverImg";

export function GlobalSearch({ onClose }: { onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [items, setItems] = useState<LibraryManga[]>([]);
  const [error, setError] = useState("");
  const [searching, setSearching] = useState(false);
  useEffect(() => {
    // Stale responses are dropped: only the latest query may update the list.
    let active = true;
    setSearching(true);
    const timer = window.setTimeout(
      () =>
        void api
          .library(query)
          .then((items) => {
            if (!active) return;
            setItems(items);
            setError("");
          })
          .catch((e) => {
            if (active) setError(e instanceof Error ? e.message : "Search failed");
          })
          .finally(() => {
            if (active) setSearching(false);
          }),
      200,
    );
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [query]);
  return (
    <div className="fixed inset-0 z-50 bg-black/70 p-4" onMouseDown={onClose}>
      <section
        className="mx-auto mt-12 max-w-2xl overflow-hidden rounded-2xl border border-zinc-700 bg-zinc-900 shadow-2xl"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="flex items-center gap-3 border-b border-zinc-800 p-4">
          <Search size={18} className="text-zinc-500" />
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search your library"
            className="min-w-0 flex-1 bg-transparent outline-none"
          />
          <button
            aria-label="Close"
            onClick={onClose}
            className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800"
          >
            <X size={16} />
          </button>
        </div>
        <div className="max-h-[60vh] overflow-y-auto p-2">
          {items.slice(0, 12).map((item) => (
            <Link
              key={item.id}
              to={`/manga/${encodeURIComponent(item.id)}`}
              onClick={onClose}
              className="flex items-center gap-3 rounded-lg p-2 hover:bg-zinc-800"
            >
              <div className="size-10 overflow-hidden rounded bg-zinc-800">
                <CoverImg src={item.coverUrl} className="size-full object-cover" />
              </div>
              <span className="min-w-0 flex-1">
                <b className="block truncate text-sm">{item.title}</b>
                <small className="text-zinc-500">{item.sourceName || "Unknown plugin"}</small>
              </span>
              <ChevronRight size={16} className="text-zinc-600" />
            </Link>
          ))}
          {error && (
            <p role="alert" className="p-8 text-center text-sm text-red-300">
              {error}
            </p>
          )}
          {!items.length && !error && !searching && (
            <p className="p-8 text-center text-sm text-zinc-500">No library matches.</p>
          )}
        </div>
      </section>
    </div>
  );
}
