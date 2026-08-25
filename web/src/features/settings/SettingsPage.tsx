import { useEffect, useState } from "react";
import { Check, Download, FolderOpen, LoaderCircle, RefreshCw, Trash2, X } from "lucide-react";
import { api } from "../../api";
import type { CatalogEntry, Category, Source, TrackerInfo, TrackerSyncJob } from "../../types";
import { Modal } from "../../components/Modal";
import { ErrorState, PageHeader } from "../../components/States";

export function SettingsPage() {
  const [sources, setSources] = useState<Source[]>([]);
  const [catalog, setCatalog] = useState<CatalogEntry[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [trackers, setTrackers] = useState<TrackerInfo[]>([]);
  const [syncJobs, setSyncJobs] = useState<TrackerSyncJob[]>([]);
  const [name, setName] = useState("");
  const [token, setToken] = useState("");
  const [pat, setPat] = useState(false);
  const [advanced, setAdvanced] = useState<string>();
  const [loginUser, setLoginUser] = useState("");
  const [loginPass, setLoginPass] = useState("");
  const [loggingIn, setLoggingIn] = useState<string>();
  const [pendingOAuth, setPendingOAuth] = useState<string>();
  const [cookieSource, setCookieSource] = useState("");
  const [cookie, setCookie] = useState("");
  const [userAgent, setUserAgent] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [installing, setInstalling] = useState<string[]>([]);
  const [removing, setRemoving] = useState<string>();
  const [refreshing, setRefreshing] = useState(false);
  const [confirmRemoval, setConfirmRemoval] = useState<Source>();
  const [confirmImport, setConfirmImport] = useState(false);
  const [importing, setImporting] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [savingToken, setSavingToken] = useState(false);
  const [creating, setCreating] = useState(false);
  const [confirmCategoryRemoval, setConfirmCategoryRemoval] = useState<Category>();
  const [removingCategory, setRemovingCategory] = useState(false);
  const [disconnecting, setDisconnecting] = useState<string>();
  const [exporting, setExporting] = useState(false);
  useEffect(() => {
    if (!status) return;
    const timer = window.setTimeout(() => setStatus(""), 4000);
    return () => window.clearTimeout(timer);
  }, [status]);
  const refresh = async () => {
    setRefreshing(true);
    try {
      const [installed, entries, groups, services, jobs] = await Promise.all([
        api.sources(),
        api.catalog(),
        api.categories(),
        api.trackers(),
        api.syncJobs(),
      ]);
      setSources(installed);
      setCatalog(entries);
      setCategories(groups);
      setTrackers(services);
      setSyncJobs(jobs.slice(0, 5));
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
  // Credential changes arrive over the tracker event socket, so an
  // authorization completed in another tab reflects here without polling.
  // The socket only refetches tracker state; the rest of the page is untouched.
  useEffect(() => {
    let socket: WebSocket | undefined;
    let reconnectTimer: number | undefined;
    let disposed = false;
    const refreshTrackers = async () => {
      try {
        const [services, jobs] = await Promise.all([api.trackers(), api.syncJobs()]);
        setTrackers(services);
        setSyncJobs(jobs.slice(0, 5));
      } catch {
        // A failed background refetch keeps the previous snapshot; the next
        // event or manual refresh reconciles.
      }
    };
    const connect = () => {
      if (disposed) return;
      const protocol = location.protocol === "https:" ? "wss:" : "ws:";
      const next = new WebSocket(`${protocol}//${location.host}/api/trackers/events`);
      socket = next;
      next.onmessage = (event) => {
        try {
          const message = JSON.parse(String(event.data)) as { type?: string };
          if (message.type === "credentials") void refreshTrackers();
        } catch {
          // A malformed frame is skipped; the next event reconciles.
        }
      };
      next.onclose = () => {
        if (disposed) return;
        reconnectTimer = window.setTimeout(connect, 2000);
      };
    };
    connect();
    return () => {
      disposed = true;
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, []);
  // A pending authorization resolves when the tracker list reports a stored
  // credential for it, whether through the socket or a manual refresh.
  useEffect(() => {
    if (!pendingOAuth) return;
    if (trackers.some((tracker) => tracker.name === pendingOAuth && tracker.credential)) {
      setPendingOAuth(undefined);
    }
  }, [trackers, pendingOAuth]);
  // Importing overwrites library, progress and categories, so the file is
  // only picked after an explicit confirmation. Failures land in the banner
  // and a success re-reads everything the restore may have changed.
  const runImport = async (file: File) => {
    setImporting(true);
    setError("");
    try {
      await api.importBackup(file);
      setStatus("Backup imported.");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to import backup");
    } finally {
      setImporting(false);
    }
  };
  const addCategory = async () => {
    if (!name.trim()) return;
    setCreating(true);
    setError("");
    try {
      const next = await api.createCategory(name.trim());
      setCategories((items) => [...items, next]);
      setName("");
      setStatus(`Category ${next.name} added.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to create category");
    } finally {
      setCreating(false);
    }
  };
  // Removing a category unlinks it from titles but deletes no titles, so a
  // single confirmation step is enough.
  const removeCategory = async (category: Category) => {
    setRemovingCategory(true);
    setError("");
    try {
      await api.deleteCategory(category.id);
      setConfirmCategoryRemoval(undefined);
      setStatus(`Category ${category.name} removed.`);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to remove category");
    } finally {
      setRemovingCategory(false);
    }
  };
  // Clearance and credential writes report their outcome in the same banner
  // and status line as the rest of the page; a rejected submission keeps the
  // entered values so they can be corrected.
  const submitClearance = async () => {
    setClearing(true);
    setError("");
    try {
      await api.submitClearance(cookieSource, cookie, userAgent);
      setCookie("");
      setUserAgent("");
      setStatus("Clearance submitted.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to submit clearance");
    } finally {
      setClearing(false);
    }
  };
  const saveToken = async (trackerName: string) => {
    if (!token) return;
    setSavingToken(true);
    setError("");
    try {
      await api.saveTrackerToken(
        trackerName,
        token,
        trackerName === "mangabaka" && pat ? { auth: "pat" } : undefined,
      );
      setToken("");
      setAdvanced(undefined);
      setStatus(`${trackerName} credentials saved.`);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save credentials");
    } finally {
      setSavingToken(false);
    }
  };
  // OAuth hands off to the provider in a new tab; the daemon completes the
  // flow through its callback endpoint while this page listens for the
  // credential event on the tracker websocket.
  const connectOAuth = async (tracker: TrackerInfo) => {
    setError("");
    try {
      const { authorizationUrl } = await api.startTrackerAuth(tracker.name);
      window.open(authorizationUrl, "_blank", "noopener");
      setPendingOAuth(tracker.name);
      setStatus(`Waiting for ${tracker.name} authorization.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to start authorization");
    }
  };
  const login = async (tracker: TrackerInfo) => {
    if (!loginUser.trim() || !loginPass) return;
    setLoggingIn(tracker.name);
    setError("");
    try {
      await api.trackerLogin(tracker.name, loginUser.trim(), loginPass);
      setStatus(`${tracker.name} connected.`);
      setLoginUser("");
      setLoginPass("");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to sign in");
    } finally {
      setLoggingIn(undefined);
    }
  };
  const disconnectTracker = async (tracker: TrackerInfo) => {
    setDisconnecting(tracker.name);
    setError("");
    try {
      await api.deleteTrackerCredentials(tracker.name);
      setStatus(`${tracker.name} disconnected.`);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to disconnect tracker");
    } finally {
      setDisconnecting(undefined);
    }
  };
  // The export is fetched to a blob instead of a plain link so a failed
  // request surfaces as a message rather than navigating away from the app.
  const exportJson = async () => {
    setExporting(true);
    setError("");
    try {
      const response = await fetch("/api/export");
      if (!response.ok) throw new Error(`Export failed (${response.status})`);
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `makidoku-backup-${new Date().toISOString().slice(0, 10)}.json`;
      anchor.click();
      URL.revokeObjectURL(url);
      setStatus("Backup exported.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to export backup");
    } finally {
      setExporting(false);
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
              aria-label="Plugin"
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
            onClick={() => void submitClearance()}
            disabled={!cookieSource || !cookie || !userAgent || clearing}
            className="mt-3 rounded-lg border border-zinc-700 px-3 py-2 text-sm disabled:opacity-40"
          >
            {clearing ? "Submitting…" : "Submit clearance"}
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
            disabled={creating || !name.trim()}
            className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
          >
            {creating ? "Adding…" : "Add"}
          </button>
        </div>
        <div className="mt-3 flex flex-wrap gap-2">
          {categories.map((item) => (
            <span
              key={item.id}
              className="inline-flex items-center gap-1 rounded-full border border-zinc-700 py-1 pl-3 pr-1.5 text-xs"
            >
              {item.name}
              <button
                aria-label={`Delete category ${item.name}`}
                onClick={() => setConfirmCategoryRemoval(item)}
                className="rounded-full p-0.5 text-zinc-500 hover:text-red-300"
              >
                <X size={12} />
              </button>
            </span>
          ))}
        </div>
        {confirmCategoryRemoval && (
          <Modal
            title="Remove category"
            onClose={() => {
              if (!removingCategory) setConfirmCategoryRemoval(undefined);
            }}
          >
            <p className="text-sm text-zinc-400">
              Remove the category {confirmCategoryRemoval.name}? Titles keep their other categories
              and stay in the library.
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <button
                onClick={() => setConfirmCategoryRemoval(undefined)}
                disabled={removingCategory}
                className="rounded-lg border border-zinc-700 px-3 py-2 text-sm disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                onClick={() => void removeCategory(confirmCategoryRemoval)}
                disabled={removingCategory}
                className="rounded-lg bg-red-500 px-3 py-2 text-sm font-semibold text-white disabled:opacity-50"
              >
                {removingCategory ? "Removing…" : "Remove"}
              </button>
            </div>
          </Modal>
        )}
      </section>
      <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
        <h2 className="font-semibold">Trackers</h2>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {trackers.map((tracker) => {
            const callbackUrl = `${window.location.origin}/api/trackers/${tracker.name}/auth/callback`;
            return (
              <div key={tracker.name} className="space-y-2 rounded-lg border border-zinc-800 p-3">
                <div className="flex items-center justify-between gap-2">
                  <b className="block">{tracker.name}</b>
                  {tracker.credential ? (
                    <small className="shrink-0 text-emerald-300">
                      Connected{tracker.connectedAs ? ` as ${tracker.connectedAs}` : ""}
                    </small>
                  ) : !tracker.configured ? (
                    <small className="shrink-0 text-zinc-500">Not configured</small>
                  ) : pendingOAuth === tracker.name ? (
                    <small className="flex shrink-0 items-center gap-1 text-amber-300">
                      <LoaderCircle size={12} className="animate-spin" /> Waiting for authorization
                    </small>
                  ) : null}
                </div>
                {tracker.credential ? (
                  <div>
                    <button
                      onClick={() => void disconnectTracker(tracker)}
                      disabled={disconnecting === tracker.name}
                      className="rounded-lg border border-red-900 px-2 py-1 text-xs text-red-300 disabled:opacity-50"
                    >
                      {disconnecting === tracker.name ? "Disconnecting…" : "Disconnect"}
                    </button>
                  </div>
                ) : !tracker.configured ? (
                  <p className="text-xs leading-relaxed text-zinc-500">
                    {tracker.configHint} on the server before this tracker can connect.
                    {tracker.authType === "oauth" && (
                      <>
                        {" "}
                        Register <code className="text-zinc-400">{callbackUrl}</code> as the
                        redirect URI.
                      </>
                    )}
                  </p>
                ) : tracker.authType === "password" ? (
                  <div className="space-y-2">
                    <div className="flex flex-wrap gap-2">
                      <input
                        value={loginUser}
                        onChange={(e) => setLoginUser(e.target.value)}
                        placeholder="Email or username"
                        aria-label={`${tracker.name} username`}
                        autoComplete="off"
                        className="min-w-40 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                      />
                      <input
                        type="password"
                        value={loginPass}
                        onChange={(e) => setLoginPass(e.target.value)}
                        placeholder="Password"
                        aria-label={`${tracker.name} password`}
                        autoComplete="new-password"
                        className="min-w-40 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                      />
                    </div>
                    <button
                      onClick={() => void login(tracker)}
                      disabled={loggingIn === tracker.name || !loginUser.trim() || !loginPass}
                      className="rounded-lg bg-amber-400 px-3 py-1.5 text-xs font-semibold text-zinc-950 disabled:opacity-40"
                    >
                      {loggingIn === tracker.name ? "Signing in…" : "Log in"}
                    </button>
                  </div>
                ) : tracker.authType === "oauth" ? (
                  <div>
                    <button
                      onClick={() => void connectOAuth(tracker)}
                      className="rounded-lg border border-amber-400 px-2 py-1 text-xs text-amber-300"
                    >
                      Connect
                    </button>
                  </div>
                ) : (
                  <p className="text-xs text-zinc-500">Paste an access token below.</p>
                )}
                {!tracker.credential && (
                  <div>
                    <button
                      onClick={() =>
                        setAdvanced(advanced === tracker.name ? undefined : tracker.name)
                      }
                      className="text-xs text-zinc-500 underline hover:text-zinc-300"
                    >
                      Paste token manually
                    </button>
                  </div>
                )}
                {advanced === tracker.name && !tracker.credential && (
                  <div className="flex flex-wrap gap-2">
                    <input
                      type="password"
                      value={token}
                      onChange={(e) => setToken(e.target.value)}
                      placeholder={`${tracker.name} access token`}
                      aria-label={`${tracker.name} access token`}
                      className="min-w-55 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                    />
                    <button
                      onClick={() => void saveToken(tracker.name)}
                      disabled={savingToken || !token}
                      className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
                    >
                      <Check size={14} className="mr-1 inline" />
                      {savingToken ? "Saving…" : "Save"}
                    </button>
                    {tracker.name === "mangabaka" && (
                      <label className="flex items-center gap-2 text-xs text-zinc-400">
                        <input
                          type="checkbox"
                          checked={pat}
                          onChange={(e) => setPat(e.target.checked)}
                        />{" "}
                        Personal access token
                      </label>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
        {syncJobs.length > 0 && (
          <div className="mt-4">
            <h3 className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
              Recent sync activity
            </h3>
            <ul className="mt-2 space-y-1">
              {syncJobs.map((job) => (
                <li key={job.id} className="text-xs text-zinc-500">
                  {job.status === "FAILED" ? (
                    <span className="text-red-300">Failed</span>
                  ) : job.status === "COMPLETED" ? (
                    <span className="text-emerald-300">Synced</span>
                  ) : (
                    job.status
                  )}
                  {" · chapter "}
                  {job.chapterNumber}
                  {job.errorMessage ? ` · ${job.errorMessage}` : ""}
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>
      <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
        <h2 className="font-semibold">Backup</h2>
        <div className="mt-3 flex flex-wrap gap-2">
          <button
            onClick={() => void exportJson()}
            disabled={exporting}
            className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
          >
            {exporting ? (
              "Preparing…"
            ) : (
              <>
                <Download size={15} /> Export JSON
              </>
            )}
          </button>
          <button
            onClick={() => setConfirmImport(true)}
            className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm"
          >
            <FolderOpen size={15} /> Import JSON
          </button>
        </div>
        {confirmImport && (
          <Modal
            title="Import backup"
            onClose={() => {
              if (!importing) setConfirmImport(false);
            }}
          >
            <p className="text-sm text-zinc-400">
              Importing replaces your library, categories, progress and tracker links with the
              contents of the selected file. This cannot be undone.
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <button
                onClick={() => setConfirmImport(false)}
                disabled={importing}
                className="rounded-lg border border-zinc-700 px-3 py-2 text-sm disabled:opacity-50"
              >
                Cancel
              </button>
              <label
                className={`inline-flex cursor-pointer items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 ${
                  importing ? "pointer-events-none opacity-60" : ""
                }`}
              >
                {importing && <LoaderCircle size={15} className="animate-spin" />}
                {importing ? "Importing…" : "Choose file"}
                <input
                  type="file"
                  accept="application/json"
                  hidden
                  disabled={importing}
                  onChange={(event) => {
                    const file = event.target.files?.[0];
                    event.target.value = "";
                    if (!file) return;
                    setConfirmImport(false);
                    void runImport(file);
                  }}
                />
              </label>
            </div>
          </Modal>
        )}
      </section>
    </div>
  );
}
