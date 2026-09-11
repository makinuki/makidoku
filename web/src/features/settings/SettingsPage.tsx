import { useCallback, useEffect, useState } from "react";
import { Check, Download, FolderOpen, LoaderCircle, X } from "lucide-react";
import { Link } from "react-router-dom";
import { api, type RuntimeSetting } from "../../api";
import type { Category, TrackerInfo, TrackerSyncJob } from "../../types";
import { Modal } from "../../components/Modal";
import { ErrorState, PageHeader } from "../../components/States";
import { useTrackerEvents } from "../../hooks/useTrackerEvents";

export function SettingsPage() {
  const [categories, setCategories] = useState<Category[]>([]);
  const [trackers, setTrackers] = useState<TrackerInfo[]>([]);
  const [syncJobs, setSyncJobs] = useState<TrackerSyncJob[]>([]);
  const [name, setName] = useState("");
  const [token, setToken] = useState("");
  const [pat, setPat] = useState(false);
  const [advanced, setAdvanced] = useState<string>();
  // Sign-in forms are scoped per tracker so two password providers never
  // mirror each other's input.
  const [loginForms, setLoginForms] = useState<Record<string, { user: string; pass: string }>>({});
  const [loggingIn, setLoggingIn] = useState<string>();
  const [pendingOAuth, setPendingOAuth] = useState<string>();
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [confirmImport, setConfirmImport] = useState(false);
  const [importing, setImporting] = useState(false);
  const [savingToken, setSavingToken] = useState(false);
  const [creating, setCreating] = useState(false);
  const [confirmCategoryRemoval, setConfirmCategoryRemoval] = useState<Category>();
  const [removingCategory, setRemovingCategory] = useState(false);
  const [disconnecting, setDisconnecting] = useState<string>();
  const [exporting, setExporting] = useState(false);
  const [runtimeSettings, setRuntimeSettings] = useState<RuntimeSetting[]>([]);
  const [readingSeconds, setReadingSeconds] = useState(0);
  const [titleCount, setTitleCount] = useState(0);
  const [chapterCount, setChapterCount] = useState(0);
  const [savingSetting, setSavingSetting] = useState<string>();
  useEffect(() => {
    if (!status) return;
    const timer = window.setTimeout(() => setStatus(""), 4000);
    return () => window.clearTimeout(timer);
  }, [status]);
  const refresh = async () => {
    try {
      const [groups, services, jobs, persisted, stats] = await Promise.all([
        api.categories(),
        api.trackers(),
        api.syncJobs(),
        api.settings(),
        api.stats(),
      ]);
      setCategories(groups);
      setTrackers(services);
      setSyncJobs(jobs.slice(0, 5));
      // View state owned by a screen is stored by the daemon but is not a
      // user-editable value here.
      setRuntimeSettings(persisted.filter((setting) => !setting.hidden));
      setReadingSeconds(stats.readingSeconds || 0);
      setTitleCount(stats.titleCount || 0);
      setChapterCount(stats.chapterCount || 0);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load settings");
    }
  };
  useEffect(() => {
    void refresh();
  }, []);
  const refreshTrackerState = useCallback(async () => {
    try {
      const [services, jobs] = await Promise.all([api.trackers(), api.syncJobs()]);
      setTrackers(services);
      setSyncJobs(jobs.slice(0, 5));
    } catch {
      // Keep the current snapshot until the next event or manual refresh.
    }
  }, []);
  useTrackerEvents(() => {
    void refreshTrackerState();
  });
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
  const updateLogin = (trackerName: string, patch: Partial<{ user: string; pass: string }>) => {
    setLoginForms((forms) => ({
      ...forms,
      [trackerName]: {
        user: forms[trackerName]?.user ?? "",
        pass: forms[trackerName]?.pass ?? "",
        ...patch,
      },
    }));
  };
  const login = async (tracker: TrackerInfo) => {
    const form = loginForms[tracker.name];
    if (!form?.user.trim() || !form.pass) return;
    setLoggingIn(tracker.name);
    setError("");
    try {
      await api.trackerLogin(tracker.name, form.user.trim(), form.pass);
      setStatus(`${tracker.name} connected.`);
      updateLogin(tracker.name, { user: "", pass: "" });
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
  const saveSetting = async (setting: RuntimeSetting, value: unknown) => {
    setSavingSetting(setting.key);
    setError("");
    try {
      await api.updateSetting(setting.key, value);
      setRuntimeSettings((current) =>
        current.map((item) =>
          item.key === setting.key ? { ...item, value: value as RuntimeSetting["value"] } : item,
        ),
      );
      setStatus(`${setting.key} updated.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save setting");
    } finally {
      setSavingSetting(undefined);
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
      {runtimeSettings.length > 0 && (
        <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
          <h2 className="font-semibold">Runtime preferences</h2>
          <p className="mt-1 text-sm text-zinc-500">
            Values are stored by the daemon and apply across desktop sessions.
          </p>
          <div className="mt-4 divide-y divide-zinc-800 border-y border-zinc-800">
            {runtimeSettings.map((setting) => {
              const parsed = setting.value;
              const disabled = savingSetting === setting.key;
              const options = settingOptions[setting.key];
              return (
                <label
                  key={`${setting.key}:${setting.value}`}
                  className="grid gap-3 py-4 sm:grid-cols-[minmax(0,1fr)_13rem] sm:items-center"
                >
                  <span>
                    <span className="block text-sm font-medium">{settingLabel(setting.key)}</span>
                    <span className="mt-1 block text-xs text-zinc-500">{setting.description}</span>
                  </span>
                  {typeof parsed === "boolean" ? (
                    <input
                      type="checkbox"
                      className="size-4 justify-self-start accent-amber-400 sm:justify-self-end"
                      checked={parsed}
                      disabled={disabled}
                      onChange={(event) => void saveSetting(setting, event.target.checked)}
                    />
                  ) : options ? (
                    <select
                      className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                      value={String(parsed)}
                      disabled={disabled}
                      onChange={(event) => void saveSetting(setting, event.target.value)}
                    >
                      {options.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  ) : (
                    <input
                      type="number"
                      className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                      defaultValue={String(parsed)}
                      disabled={disabled}
                      onBlur={(event) => {
                        const next = Number(event.target.value);
                        if (next !== parsed) void saveSetting(setting, next);
                      }}
                    />
                  )}
                </label>
              );
            })}
          </div>
        </section>
      )}
      <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
        <h2 className="font-semibold">Reading statistics</h2>
        <p className="mt-1 text-sm text-zinc-500">Time recorded by the desktop reader.</p>
        <div className="mt-4 flex items-baseline gap-3">
          <strong className="text-3xl font-semibold text-amber-300">
            {formatReadingTime(readingSeconds)}
          </strong>
          <span className="text-xs uppercase tracking-wide text-zinc-500">total reading time</span>
        </div>
        <div className="mt-4 flex gap-6 text-xs text-zinc-500">
          <span>{titleCount} titles</span>
          <span>{chapterCount} chapters</span>
        </div>
      </section>
      <section>
        <PageHeader title="Plugins">
          <Link
            to="/browse?tab=plugins"
            className="rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-300 hover:border-zinc-500"
          >
            Open Browse plugins
          </Link>
        </PageHeader>
        <p className="text-sm text-zinc-500">
          Installation, updates, removal, and browser clearance are managed from Browse.
        </p>
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
                        value={loginForms[tracker.name]?.user ?? ""}
                        onChange={(e) => updateLogin(tracker.name, { user: e.target.value })}
                        placeholder="Email or username"
                        aria-label={`${tracker.name} username`}
                        autoComplete="off"
                        className="min-w-40 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                      />
                      <input
                        type="password"
                        value={loginForms[tracker.name]?.pass ?? ""}
                        onChange={(e) => updateLogin(tracker.name, { pass: e.target.value })}
                        placeholder="Password"
                        aria-label={`${tracker.name} password`}
                        autoComplete="new-password"
                        className="min-w-40 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                      />
                    </div>
                    <button
                      onClick={() => void login(tracker)}
                      disabled={
                        loggingIn === tracker.name ||
                        !loginForms[tracker.name]?.user.trim() ||
                        !loginForms[tracker.name]?.pass
                      }
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

const settingOptions: Record<string, Array<{ value: string; label: string }>> = {
  "appearance.date_format": [
    { value: "relative", label: "Relative" },
    { value: "absolute", label: "Absolute" },
  ],
  "reader.default_mode": [
    { value: "single", label: "Single page" },
    { value: "double", label: "Double page" },
    { value: "webtoon", label: "Webtoon" },
  ],
  "reader.direction": [
    { value: "ltr", label: "Left to right" },
    { value: "rtl", label: "Right to left" },
  ],
  "reader.fit": [
    { value: "width", label: "Fit width" },
    { value: "height", label: "Fit height" },
    { value: "original", label: "Original size" },
  ],
  "advanced.log_level": [
    { value: "debug", label: "Debug" },
    { value: "info", label: "Info" },
    { value: "warn", label: "Warning" },
    { value: "error", label: "Error" },
  ],
};

function settingLabel(key: string) {
  return key
    .split(".")
    .at(-1)!
    .replaceAll("_", " ")
    .replace(/^./, (letter) => letter.toUpperCase());
}

function formatReadingTime(seconds: number) {
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder ? `${hours}h ${remainder}m` : `${hours}h`;
}
