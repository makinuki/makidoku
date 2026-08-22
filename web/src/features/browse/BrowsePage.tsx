import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Plus, Search } from "lucide-react";
import { api } from "../../api";
import type { FilterSchema, SearchResult, Source } from "../../types";
import { EmptyState, ErrorState, LoadingState, PageHeader } from "../../components/States";

export function BrowsePage() {
  const [sources, setSources] = useState<Source[]>([]);
  const [selected, setSelected] = useState("all");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Array<SearchResult & { source: Source }>>([]);
  const [filterSchemas, setFilterSchemas] = useState<FilterSchema[]>([]);
  const [filterValues, setFilterValues] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (selected === "all") {
      setFilterSchemas([]);
      setFilterValues({});
      return;
    }
    let active = true;
    void api
      .sourceFilters(selected)
      .then((schemas) => {
        if (!active) return;
        setFilterSchemas(schemas);
        setFilterValues(defaultFilterValues(schemas));
      })
      .catch(() => {
        if (active) {
          setFilterSchemas([]);
          setFilterValues({});
        }
      });
    return () => {
      active = false;
    };
  }, [selected]);
  useEffect(() => {
    void api
      .sources()
      .then(setSources)
      .catch((e) => setError(e.message));
  }, []);
  useEffect(() => {
    if (!query.trim()) {
      setResults([]);
      return;
    }
    const timer = window.setTimeout(() => {
      setLoading(true);
      setError("");
      const wanted = selected === "all" ? sources : sources.filter((item) => item.id === selected);
      Promise.all(
        wanted.map(async (source) => {
          try {
            const page = await api.search(
              source.id,
              query,
              1,
              selected === source.id ? filterValues : undefined,
            );
            return page.items.map((item) => ({ ...item, source }));
          } catch {
            return [];
          }
        }),
      )
        .then((items) => setResults(items.flat()))
        .catch((e) => setError(e.message))
        .finally(() => setLoading(false));
    }, 300);
    return () => window.clearTimeout(timer);
  }, [query, selected, sources, filterValues]);
  return (
    <div className="mx-auto max-w-7xl p-5 sm:p-8">
      <PageHeader eyebrow="Source explorer" title="Browse">
        <Link
          to="/settings"
          className="rounded-lg border border-zinc-800 px-3 py-2 text-sm text-zinc-300 hover:border-zinc-600"
        >
          Manage sources
        </Link>
      </PageHeader>
      <div className="mb-8 flex gap-3 overflow-x-auto pb-1">
        <button
          onClick={() => setSelected("all")}
          className={`flex min-w-40 items-center gap-3 rounded-xl border px-4 py-3 text-left ${selected === "all" ? "border-amber-400 bg-amber-400/10" : "border-zinc-800 bg-zinc-900"}`}
        >
          <span className="grid size-9 place-items-center rounded-full bg-zinc-800">
            <Search size={16} />
          </span>
          <span>
            <b className="block text-sm">All sources</b>
            <small className="text-zinc-500">{sources.length} installed</small>
          </span>
        </button>
        {sources.map((source) => (
          <button
            key={source.id}
            onClick={() => setSelected(source.id)}
            className={`flex min-w-48 items-center gap-3 rounded-xl border px-4 py-3 text-left ${selected === source.id ? "border-amber-400 bg-amber-400/10" : "border-zinc-800 bg-zinc-900"}`}
          >
            <span className="grid size-9 place-items-center rounded-full bg-zinc-800 text-xs font-bold">
              {source.name.slice(0, 2).toUpperCase()}
            </span>
            <span className="min-w-0">
              <b className="block truncate text-sm">{source.name}</b>
              <small className="text-zinc-500">
                v{source.version} · {source.lang}
              </small>
            </span>
          </button>
        ))}
      </div>
      <label className="mb-6 flex items-center gap-3 rounded-xl border border-zinc-800 bg-zinc-900 px-4 py-3">
        <Search size={18} className="text-zinc-500" />
        <input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search installed sources"
          className="min-w-0 flex-1 bg-transparent outline-none"
        />
      </label>
      {selected !== "all" && filterSchemas.length > 0 && (
        <section className="mb-6 rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
          <h2 className="text-sm font-semibold">Source filters</h2>
          <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {filterSchemas.map((schema) => (
              <FilterControl
                key={schema.id}
                schema={schema}
                value={filterValues[schema.id]}
                onChange={(value) =>
                  setFilterValues((current) => ({ ...current, [schema.id]: value }))
                }
              />
            ))}
          </div>
        </section>
      )}
      {error && <ErrorState message={error} />}
      {loading ? (
        <LoadingState label="Searching sources" />
      ) : results.length ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {results.map((item) => (
            <SourceResult key={`${item.source.id}:${item.id}`} item={item} />
          ))}
        </div>
      ) : (
        <EmptyState
          title={query ? "No matching titles" : "Search installed sources"}
          text={
            query
              ? "Try another title or select a different source."
              : "Search uses the installed source plugins and the daemon's shared HTTP stack."
          }
        />
      )}
    </div>
  );
}

function defaultFilterValues(schemas: FilterSchema[]) {
  const values: Record<string, unknown> = {};
  for (const schema of schemas) {
    if (schema.type === "tri_state") {
      if (schema.default) values[schema.id] = schema.default;
    } else if (schema.default !== undefined && schema.default !== "") {
      values[schema.id] = schema.default;
    }
  }
  return values;
}

function FilterControl({
  schema,
  value,
  onChange,
}: {
  schema: FilterSchema;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  if (schema.type === "checkbox") {
    return (
      <label className="flex items-center gap-2 text-sm text-zinc-300">
        <input
          type="checkbox"
          checked={Boolean(value)}
          onChange={(event) => onChange(event.target.checked)}
          className="accent-amber-400"
        />
        {schema.title}
      </label>
    );
  }
  if (schema.type === "select") {
    return (
      <label className="grid gap-1 text-xs text-zinc-400">
        <span>{schema.title}</span>
        <select
          aria-label={schema.title}
          value={typeof value === "string" ? value : ""}
          onChange={(event) => onChange(event.target.value)}
          className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm text-zinc-100"
        >
          {schema.options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>
    );
  }
  if (schema.type === "tri_state") {
    const selected = typeof value === "object" && value ? (value as Record<string, string>) : {};
    const update = (option: string, nextValue: string) => {
      const next = { ...selected };
      if (nextValue) next[option] = nextValue;
      else delete next[option];
      onChange(next);
    };
    return (
      <fieldset className="grid gap-2 text-xs text-zinc-400">
        <legend>{schema.title}</legend>
        {schema.options.map((option) => (
          <label key={option.value} className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate">{option.label}</span>
            <select
              aria-label={`${schema.title}: ${option.label}`}
              value={selected[option.value] || ""}
              onChange={(event) => update(option.value, event.target.value)}
              className="rounded-lg border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-xs text-zinc-100"
            >
              <option value="">Any</option>
              <option value="+">Include</option>
              <option value="-">Exclude</option>
            </select>
          </label>
        ))}
      </fieldset>
    );
  }
  return (
    <label className="grid gap-1 text-xs text-zinc-400">
      <span>{schema.title}</span>
      <input
        aria-label={schema.title}
        value={typeof value === "string" ? value : ""}
        placeholder={schema.placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm text-zinc-100"
      />
    </label>
  );
}

function SourceResult({ item }: { item: SearchResult & { source: Source } }) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const aggregate = await api.saveManga(item.source.id, item.id);
      navigate(
        `/manga/${encodeURIComponent(aggregate.manga.sourceId)}/${encodeURIComponent(aggregate.manga.sourceMangaId)}`,
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save title");
    } finally {
      setBusy(false);
    }
  };
  return (
    <article className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900">
      <div className="aspect-[3/4] bg-zinc-800">
        {item.coverUrl && (
          <img src={item.coverUrl} alt="" className="size-full object-cover" loading="lazy" />
        )}
      </div>
      <div className="space-y-2 p-4">
        <p className="text-xs uppercase tracking-wide text-amber-400">{item.source.name}</p>
        <h2 className="line-clamp-2 font-semibold">{item.title}</h2>
        <p className="text-xs text-zinc-500">{item.latestChapter || "No chapter metadata"}</p>
        {error && <p className="text-xs text-red-300">{error}</p>}
        <button
          disabled={busy}
          onClick={() => void save()}
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-50"
        >
          <Plus size={15} /> {busy ? "Saving" : "Save title"}
        </button>
      </div>
    </article>
  );
}
