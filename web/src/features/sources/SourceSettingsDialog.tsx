import { useEffect, useState } from "react";
import { LoaderCircle, RotateCcw } from "lucide-react";
import { api } from "../../api";
import { Modal } from "../../components/Modal";
import { ErrorState, LoadingState } from "../../components/States";
import { languageLabel } from "../../languages";
import type { Source, SourceSetting } from "../../types";

// SourceSettingsDialog renders the settings a source declares through
// get_settings. Values are written per field as they change and stored in the
// source's own namespace, so the source reads the current value on its next
// call. A sensitive value is write-only: the dialog shows whether one is
// stored but never reads it back.
export function SourceSettingsDialog({
  source,
  onClose,
}: {
  source: Source;
  onClose: () => void;
}) {
  const [settings, setSettings] = useState<SourceSetting[]>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [languages, setLanguages] = useState<string[]>(source.languages ?? []);
  const [languagesBusy, setLanguagesBusy] = useState(false);
  // A source offers a language choice only once it has returned chapters in
  // more than one language. A single-language or language-less source has
  // nothing to filter.
  const available = [...(source.availableLanguages ?? [])].sort();
  const multiLanguage = available.length > 1;

  useEffect(() => {
    let active = true;
    api
      .sourceSettings(source.id)
      .then((items) => {
        if (active) setSettings(items);
      })
      .catch((e) => {
        if (active) setError(e instanceof Error ? e.message : "Unable to load source settings");
      });
    return () => {
      active = false;
    };
  }, [source.id]);

  const save = async (setting: SourceSetting, value: boolean | string | null) => {
    setBusy(setting.id);
    setError("");
    try {
      const updated = await api.setSourceSetting(source.id, setting.id, value);
      setSettings((list) => (list ?? []).map((item) => (item.id === updated.id ? updated : item)));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save the setting");
    } finally {
      setBusy("");
    }
  };

  const saveLanguages = async (next: string[]) => {
    setLanguagesBusy(true);
    setError("");
    try {
      const updated = await api.setSourceLanguages(source.id, next.length ? next : null);
      setLanguages(updated.languages ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save the language selection");
    } finally {
      setLanguagesBusy(false);
    }
  };

  const toggleLanguage = (code: string) => {
    void saveLanguages(
      languages.includes(code) ? languages.filter((item) => item !== code) : [...languages, code],
    );
  };

  return (
    <Modal title={`${source.name} settings`} onClose={onClose}>
      {error && <ErrorState message={error} />}
      {!settings && !error && <LoadingState label="Loading settings" />}
      {multiLanguage && (
        <div className="mb-3 rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <b className="block text-sm">Chapter languages</b>
              <span className="mt-0.5 block text-xs text-zinc-500">
                {languages.length
                  ? "Only these languages appear for this source."
                  : "Empty follows the default language selection from settings."}
              </span>
            </div>
            {languagesBusy && (
              <LoaderCircle size={14} className="mt-0.5 shrink-0 animate-spin text-zinc-400" />
            )}
          </div>
          <div className="mt-3 flex flex-wrap gap-2">
            {available.map((code) => (
              <label
                key={code}
                className={`inline-flex min-h-11 cursor-pointer items-center gap-2 rounded-full border px-4 py-1.5 text-xs ${
                  languages.includes(code)
                    ? "border-amber-400 bg-amber-400/10 text-amber-200"
                    : "border-zinc-800 text-zinc-400"
                }`}
              >
                <input
                  type="checkbox"
                  checked={languages.includes(code)}
                  disabled={languagesBusy}
                  onChange={() => toggleLanguage(code)}
                  className="size-4 accent-amber-400"
                />
                {languageLabel(code)}
              </label>
            ))}
          </div>
          {languages.length > 0 && (
            <button
              type="button"
              disabled={languagesBusy}
              onClick={() => void saveLanguages([])}
              className="mt-2 inline-flex items-center gap-1 text-xs text-zinc-400 hover:text-white disabled:opacity-40"
            >
              <RotateCcw size={12} /> Use the default
            </button>
          )}
        </div>
      )}
      {settings && settings.length === 0 && !multiLanguage && (
        <p className="text-sm text-zinc-500">This source does not declare any settings.</p>
      )}
      {settings && settings.length > 0 && (
        <div className="space-y-3">
          {settings.map((setting) => (
            <SettingField
              key={setting.id}
              setting={setting}
              busy={busy === setting.id}
              onSave={save}
            />
          ))}
        </div>
      )}
    </Modal>
  );
}

function SettingField({
  setting,
  busy,
  onSave,
}: {
  setting: SourceSetting;
  busy: boolean;
  onSave: (setting: SourceSetting, value: boolean | string | null) => void;
}) {
  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <b className="block text-sm">{setting.title}</b>
          {setting.description && (
            <span className="mt-0.5 block text-xs text-zinc-500">{setting.description}</span>
          )}
        </div>
        {busy && <LoaderCircle size={14} className="mt-0.5 shrink-0 animate-spin text-zinc-400" />}
      </div>
      <div className="mt-3">
        {setting.type === "checkbox" && (
          <input
            type="checkbox"
            aria-label={setting.title}
            checked={setting.value === true}
            disabled={busy}
            onChange={(event) => onSave(setting, event.target.checked)}
            className="size-4 accent-amber-400"
          />
        )}
        {setting.type === "select" && (
          <select
            aria-label={setting.title}
            value={typeof setting.value === "string" ? setting.value : ""}
            disabled={busy}
            onChange={(event) => onSave(setting, event.target.value)}
            className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
          >
            {(setting.options ?? []).map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        )}
        {setting.type === "text" && (
          <TextSettingInput setting={setting} busy={busy} onSave={onSave} />
        )}
      </div>
      {setting.hasValue && (
        <button
          type="button"
          disabled={busy}
          onClick={() => onSave(setting, null)}
          className="mt-2 inline-flex items-center gap-1 text-xs text-zinc-400 hover:text-white disabled:opacity-40"
        >
          <RotateCcw size={12} /> Reset to default
        </button>
      )}
    </div>
  );
}

function TextSettingInput({
  setting,
  busy,
  onSave,
}: {
  setting: SourceSetting;
  busy: boolean;
  onSave: (setting: SourceSetting, value: boolean | string | null) => void;
}) {
  const current = typeof setting.value === "string" ? setting.value : "";
  const [draft, setDraft] = useState(current);
  useEffect(() => {
    setDraft(current);
  }, [current, setting.hasValue]);

  const commit = () => {
    if (busy || draft === current) return;
    onSave(setting, draft);
  };

  return (
    <input
      type={setting.sensitive ? "password" : "text"}
      aria-label={setting.title}
      autoComplete="off"
      value={draft}
      disabled={busy}
      placeholder={setting.sensitive && setting.hasValue ? "Saved" : setting.placeholder}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={commit}
      className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
    />
  );
}
