import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { Outlet } from "react-router-dom";
import { api, type RuntimeSetting } from "../../api";
import { ErrorState, LoadingState } from "../../components/States";
import { settingLabel } from "./settingsCatalog";

export type SettingsContextValue = {
  settings: RuntimeSetting[];
  saving?: string;
  status: string;
  error: string;
  save: (setting: RuntimeSetting, value: RuntimeSetting["value"]) => Promise<void>;
  reload: () => Promise<void>;
  setStatus: (message: string) => void;
  setError: (message: string) => void;
};

const SettingsContext = createContext<SettingsContextValue | null>(null);

export function useSettings(): SettingsContextValue {
  const value = useContext(SettingsContext);
  if (!value) throw new Error("useSettings must be used inside the settings layout");
  return value;
}

export function SettingsLayout() {
  const [settings, setSettings] = useState<RuntimeSetting[]>([]);
  const [saving, setSaving] = useState<string>();
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);

  const reload = useCallback(async () => {
    try {
      const persisted = await api.settings();
      // View state owned by a screen is stored by the daemon but is not a
      // user-editable value here.
      setSettings(persisted.filter((setting) => !setting.hidden));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load settings");
    } finally {
      setLoaded(true);
    }
  }, []);
  useEffect(() => {
    void reload();
  }, [reload]);
  useEffect(() => {
    if (!status) return;
    const timer = window.setTimeout(() => setStatus(""), 4000);
    return () => window.clearTimeout(timer);
  }, [status]);

  const save = useCallback(async (setting: RuntimeSetting, value: RuntimeSetting["value"]) => {
    setSaving(setting.key);
    setError("");
    try {
      await api.updateSetting(setting.key, value);
      setSettings((current) =>
        current.map((item) => (item.key === setting.key ? { ...item, value } : item)),
      );
      setStatus(`${settingLabel(setting.key)} updated.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save setting");
    } finally {
      setSaving(undefined);
    }
  }, []);

  return (
    <SettingsContext.Provider
      value={{ settings, saving, status, error, save, reload, setStatus, setError }}
    >
      <div className="mx-auto max-w-5xl p-5 sm:p-8">
        <SettingsStatus />
        {loaded ? <Outlet /> : <LoadingState label="Loading settings" />}
      </div>
    </SettingsContext.Provider>
  );
}

function SettingsStatus() {
  const { status, error } = useSettings();
  if (!status && !error) return null;
  return (
    <div className="mb-6 space-y-3">
      {status && <p className="text-sm text-emerald-300">{status}</p>}
      {error && <ErrorState message={error} />}
    </div>
  );
}
