import { useEffect, useState, type ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { ChevronLeft } from "lucide-react";
import type { RuntimeSetting } from "../../api";
import { PageHeader } from "../../components/States";
import { useSettings } from "./SettingsLayout";
import {
  durationLabel,
  durationOptions,
  sectionById,
  settingLabel,
  settingOptions,
  settingsForSection,
  type SettingsSectionId,
} from "./settingsCatalog";

export function SectionPage({
  section,
  children,
}: {
  section: SettingsSectionId;
  children: ReactNode;
}) {
  const meta = sectionById(section);
  return (
    <div>
      <Link
        to="/settings"
        className="mb-3 inline-flex items-center gap-1 text-sm text-zinc-400 hover:text-white"
      >
        <ChevronLeft size={16} /> Settings
      </Link>
      <PageHeader title={meta?.title ?? "Settings"} />
      <div className="space-y-6">{children}</div>
    </div>
  );
}

export function SettingsCard({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-5">
      <h2 className="font-semibold">{title}</h2>
      {description && <p className="mt-1 text-sm text-zinc-500">{description}</p>}
      <div className="mt-4">{children}</div>
    </section>
  );
}

export function SettingRows({ section, keys }: { section: SettingsSectionId; keys?: string[] }) {
  const { settings } = useSettings();
  const rows = settingsForSection(settings, section).filter(
    (setting) => !keys || keys.includes(setting.key),
  );
  if (!rows.length) {
    return <p className="text-sm text-zinc-500">No values are available for this section.</p>;
  }
  return (
    <div className="divide-y divide-zinc-800 border-y border-zinc-800">
      {rows.map((setting) => (
        // Remount on a value change so the uncontrolled number input resets.
        <SettingRow key={`${setting.key}:${setting.value}`} setting={setting} />
      ))}
    </div>
  );
}

function SettingRow({ setting }: { setting: RuntimeSetting }) {
  const { saving, save } = useSettings();
  const highlighted = useHighlight(setting.key);
  const disabled = saving === setting.key;
  const parsed = setting.value;
  const options = settingOptions[setting.key];
  return (
    <label
      id={setting.key}
      data-highlighted={highlighted ? "true" : undefined}
      className={`grid gap-3 py-4 sm:grid-cols-[minmax(0,1fr)_13rem] sm:items-center ${
        highlighted ? "rounded-lg bg-amber-400/10 px-3 ring-1 ring-amber-400/60" : ""
      }`}
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
          onChange={(event) => void save(setting, event.target.checked)}
        />
      ) : setting.type === "duration" ? (
        <select
          className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
          value={String(parsed)}
          disabled={disabled}
          onChange={(event) => void save(setting, Number(event.target.value))}
        >
          {(durationOptions.some((option) => option.value === parsed)
            ? durationOptions
            : [...durationOptions, { value: Number(parsed), label: durationLabel(Number(parsed)) }]
          ).map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      ) : options ? (
        <select
          className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
          value={String(parsed)}
          disabled={disabled}
          onChange={(event) => void save(setting, event.target.value)}
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
          className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
          defaultValue={String(parsed)}
          disabled={disabled}
          onBlur={(event) => {
            const next = Number(event.target.value);
            if (next !== parsed) void save(setting, next);
          }}
        />
      )}
    </label>
  );
}

// useHighlight marks the setting named by the URL fragment for a moment so a
// search result lands on a visible target.
function useHighlight(key: string): boolean {
  const { hash, key: locationKey } = useLocation();
  const [active, setActive] = useState(false);
  useEffect(() => {
    const target = hash.startsWith("#") ? decodeURIComponent(hash.slice(1)) : "";
    if (target !== key) {
      setActive(false);
      return;
    }
    setActive(true);
    const timer = window.setTimeout(() => setActive(false), 2000);
    return () => window.clearTimeout(timer);
  }, [hash, key, locationKey]);
  useEffect(() => {
    if (!active) return;
    const element = document.getElementById(key);
    if (element && typeof element.scrollIntoView === "function") {
      element.scrollIntoView({ block: "center" });
    }
  }, [active, key]);
  return active;
}
