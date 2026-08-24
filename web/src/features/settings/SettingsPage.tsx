import { useEffect, useState } from "react";
import { Check, Download, FolderOpen, LoaderCircle, RefreshCw, Trash2 } from "lucide-react";
import { api } from "../../api";
import type { CatalogEntry, Category, Source, TrackerInfo } from "../../types";
import { Modal } from "../../components/Modal";
import { ErrorState, PageHeader } from "../../components/States";

export function SettingsPage() {
  const [sources, setSources] = useState<Source[]>([]);
  const [catalog, setCatalog] = useState<CatalogEntry[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [trackers, setTrackers] = useState<TrackerInfo[]>([]);
  const [name, setName] = useState("");
  const [tokenType, setTokenType] = useState("");
  const [token, setToken] = useState("");
  const [pat, setPat] = useState(false);
  const [cookieSource, setCookieSource] = useState("");
  const [cookie, setCookie] = useState("");
  const [userAgent, setUserAgent] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [installing, setInstalling] = useState<string[]>([]);
  const [removing, setRemoving] = useState<string>();
  const [refreshing, setRefreshing] = useState(false);
  const [confirmRemoval, setConfirmRemoval] = useState<Source>();
  useEffect(() => {
    if (!status) return;
    const timer = window.setTimeout(() => setStatus(""), 4000);
    return () => window.clearTimeout(timer);
  }, [status]);
  const refresh = async () => {
    setRefreshing(true);
    try {
      const [installed, entries, groups, services] = await Promise.all([
        api.sources(),
        api.catalog(),
        api.categories(),
        api.trackers(),
      ]);
      setSources(installed);
      setCatalog(entries);
      setCategories(groups);
      setTrackers(services);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load settings");
    } finally {
      setRefreshing(false);
    }
  };
  // Install and removal surface their outcome immediately: the clicked action
  // shows a busy state, failures land in the banner, successes in the status
  // line at the top of the page. Installs run concurrently per plugin.
  const install = async (entry: CatalogEntry) => {
    setInstalling((ids) => [...ids, entry.id]);
    setError("");
    try {
      await api.installSource(entry.id);
      setStatus(`${entry.name} installed.`);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to install plugin");
    } finally {
      setInstalling((ids) => ids.filter((id) => id !== entry.id));
    }
  };
  const uninstall = async (source: Source) => {
    setRemoving(source.id);
    setError("");
    try {
      await api.uninstallSource(source.id);
      setStatus(`${source.name} removed.`);
      setConfirmRemoval(undefined);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to remove plugin");
    } finally {
      setRemoving(undefined);
    }
  };
  useEffect(() => {
    void refresh();
  }, []);
  const addCategory = async () => {
    if (!name.trim()) return;
    try {
      const next = await api.createCategory(name.trim());
      setCategories((items) => [...items, next]);
      setName("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to create category");
    }
  };
  return (
    <div className="mx-auto max-w-5xl space-y-8 p-5 sm:p-8">
      <PageHeader eyebrow="Local configuration" title="Settings" />
      {status && <p className="text-sm text-emerald-300">{status}</p>}
      {error && <ErrorState message={error} />}
      <section>
        <PageHeader title="Plugins">
          <button
            onClick={() => void refresh()}
            aria-label="Refresh plugins"
            disabled={refreshing}
            className="rounded-lg border border-zinc-700 p-2 text-zinc-300 disabled:opacity-50"
          >
            <RefreshCw size={16} className={refreshing ? "animate-spin" : ""} />
          </button>
        </PageHeader>
        <div className="grid gap-3 md:grid-cols-2">
          {sources.map((source) => (
            <div key={source.id} className="rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
              <div className="flex items-center gap-3">
                <span className="grid size-10 place-items-center rounded-full bg-zinc-800 text-xs font-bold">
                  {source.name.slice(0, 2).toUpperCase()}
                </span>
                <span className="min-w-0 flex-1">
                  <b className="block truncate">{source.name}</b>
                  <small className="text-zinc-500">
                    v{source.version} · {source.lang}
                  </small>
                </span>
                <button
                  onClick={() => setConfirmRemoval(source)}
                  aria-label={`Uninstall ${source.name}`}
                  disabled={removing !== undefined}
                  className="rounded-lg p-2 text-red-300 hover:bg-red-950/50 disabled:opacity-50"
                >
                  <Trash2 size={15} />
                </button>
              </div>
            </div>
          ))}
        </div>
        <div className="mt-4 rounded-xl border border-zinc-800 bg-zinc-900/40 p-4">
          <h3 className="font-semibold">Available plugins</h3>
          <div className="mt-3 grid gap-2">
            {catalog
              .filter((entry) => !entry.installed)
              .map((entry) => (
                <div
                  key={entry.id}
                  className="flex items-center gap-3 rounded-lg border border-zinc-800 p-3"
                >
                  <span className="min-w-0 flex-1">
                    <b className="block">{entry.name}</b>
                    <small className="text-zinc-500">
                      v{entry.version} ·{" "}
                      {entry.compatible ? "Compatible" : entry.incompatibility || "Not compatible"}
                    </small>
                  </span>
                  <button
                    disabled={!entry.compatible || installing.includes(entry.id)}
                    onClick={() => void install(entry)}
                    className="flex items-center gap-1.5 rounded-lg bg-amber-400 px-3 py-2 text-xs font-semibold text-zinc-950 disabled:opacity-40"
                  >
                    {installing.includes(entry.id) && (
                      <LoaderCircle size={13} className="animate-spin" />
                    )}
                    {installing.includes(entry.id) ? "Installing…" : "Install"}
                  </button>
                </div>
              ))}
          </div>
        </div>
        <div className="mt-4 rounded-xl border border-zinc-800 bg-zinc-900/40 p-4">
          <h3 className="font-semibold">Cloudflare clearance</h3>
          <p className="mt-1 text-xs text-zinc-500">
            Submit browser clearance for a protected plugin. MakiDoku stores it; the UI never reads
            it back.
          </p>
          <div className="mt-3 grid gap-2 sm:grid-cols-3">
            <select
              value={cookieSource}
              onChange={(e) => setCookieSource(e.target.value)}
              className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
            >
              <option value="">Plugin</option>
              {sources.map((source) => (
                <option key={source.id} value={source.id}>
                  {source.name}
                </option>
              ))}
            </select>
            <input
              value={cookie}
              onChange={(e) => setCookie(e.target.value)}
              placeholder="cf_clearance cookie"
              className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
            />
            <input
              value={userAgent}
              onChange={(e) => setUserAgent(e.target.value)}
              placeholder="Browser user agent"
              className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
            />
          </div>
          <button
            onClick={async () => {
              await api.submitClearance(cookieSource, cookie, userAgent);
              setCookie("");
              setUserAgent("");
              setStatus("Clearance submitted.");
            }}
            disabled={!cookieSource || !cookie || !userAgent}
            className="mt-3 rounded-lg border border-zinc-700 px-3 py-2 text-sm disabled:opacity-40"
          >
            Submit clearance
          </button>
        </div>
        {confirmRemoval && (
          <Modal title="Remove plugin" onClose={() => setConfirmRemoval(undefined)}>
            <p className="text-sm text-zinc-400">
              Remove {confirmRemoval.name}? Downloaded chapters and reading progress stay on disk.
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <button
                onClick={() => setConfirmRemoval(undefined)}
                className="rounded-lg border border-zinc-700 px-3 py-2 text-sm"
              >
                Cancel
              </button>
              <button
                disabled={removing !== undefined}
                onClick={() => void uninstall(confirmRemoval)}
                className="rounded-lg bg-red-500 px-3 py-2 text-sm font-semibold text-white disabled:opacity-50"
              >
                {removing === confirmRemoval.id ? "Removing…" : "Remove"}
              </button>
            </div>
          </Modal>
        )}
      </section>
      <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
        <h2 className="font-semibold">Categories</h2>
        <div className="mt-3 flex gap-2">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="New category"
            className="min-w-0 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
          />
          <button
            onClick={() => void addCategory()}
            className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
          >
            Add
          </button>
        </div>
        <div className="mt-3 flex flex-wrap gap-2">
          {categories.map((item) => (
            <span key={item.id} className="rounded-full border border-zinc-700 px-3 py-1 text-xs">
              {item.name}
            </span>
          ))}
        </div>
      </section>
      <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
        <h2 className="font-semibold">Trackers</h2>
        <div className="mt-3 grid gap-2 sm:grid-cols-2">
          {trackers.map((tracker) => (
            <button
              key={tracker.name}
              onClick={() => setTokenType(tracker.name)}
              className="rounded-lg border border-zinc-800 p-3 text-left hover:border-amber-400"
            >
              <b className="block">{tracker.name}</b>
              <small className="text-zinc-500">
                {tracker.credential
                  ? "Connected"
                  : tracker.capabilities.oauth
                    ? "OAuth available"
                    : "Token required"}
              </small>
            </button>
          ))}
        </div>
        {tokenType && (
          <div className="mt-4 flex flex-wrap gap-2">
            <input
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={`${tokenType} access token`}
              className="min-w-55 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
            />
            <button
              onClick={async () => {
                await api.saveTrackerToken(
                  tokenType,
                  token,
                  tokenType === "mangabaka" && pat ? { auth: "pat" } : undefined,
                );
                setToken("");
                setStatus(`${tokenType} credentials saved.`);
                await refresh();
              }}
              className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
            >
              <Check size={14} className="mr-1 inline" />
              Save
            </button>
            {tokenType === "mangabaka" && (
              <label className="flex items-center gap-2 text-xs text-zinc-400">
                <input type="checkbox" checked={pat} onChange={(e) => setPat(e.target.checked)} />{" "}
                Personal access token
              </label>
            )}
          </div>
        )}
      </section>
      <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
        <h2 className="font-semibold">Backup</h2>
        <div className="mt-3 flex flex-wrap gap-2">
          <a
            href="/api/export"
            className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950"
          >
            <Download size={15} /> Export JSON
          </a>
          <label className="inline-flex cursor-pointer items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm">
            <FolderOpen size={15} /> Import JSON
            <input
              type="file"
              accept="application/json"
              hidden
              onChange={async (event) => {
                const file = event.target.files?.[0];
                if (!file) return;
                await api.importBackup(file);
                setStatus("Backup imported.");
              }}
            />
          </label>
        </div>
      </section>
    </div>
  );
}
