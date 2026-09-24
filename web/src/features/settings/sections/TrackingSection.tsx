import { useCallback, useEffect, useState } from "react";
import { Check, LoaderCircle } from "lucide-react";
import { api } from "../../../api";
import type { TrackerInfo, TrackerSyncJob } from "../../../types";
import { useTrackerEvents } from "../../../hooks/useTrackerEvents";
import { useSettings } from "../SettingsLayout";
import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function TrackingSection() {
  const { setStatus, setError } = useSettings();
  const [trackers, setTrackers] = useState<TrackerInfo[]>([]);
  const [syncJobs, setSyncJobs] = useState<TrackerSyncJob[]>([]);
  const [token, setToken] = useState("");
  const [pat, setPat] = useState(false);
  const [advanced, setAdvanced] = useState<string>();
  // Sign-in forms are scoped per tracker so two password providers never
  // mirror each other's input.
  const [loginForms, setLoginForms] = useState<Record<string, { user: string; pass: string }>>({});
  const [loggingIn, setLoggingIn] = useState<string>();
  const [pendingOAuth, setPendingOAuth] = useState<string>();
  const [savingToken, setSavingToken] = useState(false);
  const [disconnecting, setDisconnecting] = useState<string>();

  const refreshTrackerState = useCallback(async () => {
    try {
      const [services, jobs] = await Promise.all([api.trackers(), api.syncJobs()]);
      setTrackers(services);
      setSyncJobs(jobs.slice(0, 5));
    } catch {
      // Keep the current snapshot until the next event or manual refresh.
    }
  }, []);
  useEffect(() => {
    void refreshTrackerState();
  }, [refreshTrackerState]);
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
      await refreshTrackerState();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save credentials");
    } finally {
      setSavingToken(false);
    }
  };
  // OAuth hands off to the provider in a new tab; the daemon completes the
  // flow through its callback endpoint while this screen listens for the
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
      await refreshTrackerState();
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
      await refreshTrackerState();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to disconnect tracker");
    } finally {
      setDisconnecting(undefined);
    }
  };

  return (
    <SectionPage section="tracking">
      <SettingsCard
        title="Synchronization"
        description="Push reading progress to connected services."
      >
        <SettingRows section="tracking" />
      </SettingsCard>
      <SettingsCard title="Tracker accounts" description="Connect and manage tracker credentials.">
        <div className="grid gap-3 sm:grid-cols-2">
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
                        Register <code className="break-all text-zinc-400">{callbackUrl}</code> as
                        the redirect URI.
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
                        className="min-w-40 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
                      />
                      <input
                        type="password"
                        value={loginForms[tracker.name]?.pass ?? ""}
                        onChange={(e) => updateLogin(tracker.name, { pass: e.target.value })}
                        placeholder="Password"
                        aria-label={`${tracker.name} password`}
                        autoComplete="new-password"
                        className="min-w-40 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
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
                      className="min-w-55 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
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
      </SettingsCard>
    </SectionPage>
  );
}
