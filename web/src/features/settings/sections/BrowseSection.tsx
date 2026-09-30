import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { RotateCcw } from "lucide-react";
import { api } from "../../../api";
import { languageLabel } from "../../../languages";
import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";
import { useSettings } from "../SettingsLayout";

export function BrowseSection() {
  return (
    <SectionPage section="browse">
      <SettingsCard title="Sources" description="What the browse catalog shows.">
        <SettingRows section="browse" keys={["browse.hide_nsfw"]} />
      </SettingsCard>
      <ChapterLanguagesCard />
      <SettingsCard
        title="Plugins"
        description="Installation, updates, removal, and browser clearance are managed from Browse."
      >
        <Link
          to="/browse?tab=plugins"
          className="inline-flex rounded-lg border border-zinc-700 px-3 py-2 text-sm text-zinc-200 hover:border-zinc-500"
        >
          Manage plugins
        </Link>
      </SettingsCard>
    </SectionPage>
  );
}

// ChapterLanguagesCard writes the default chapter language selection sources
// fall back to. The codes come from what the installed sources have actually
// returned, so the control only ever offers languages that exist.
function ChapterLanguagesCard() {
  const { settings, save } = useSettings();
  const [available, setAvailable] = useState<string[]>([]);

  useEffect(() => {
    let active = true;
    api
      .sources()
      .then((sources) => {
        if (!active) return;
        const codes = new Set<string>();
        sources.forEach((source) =>
          (source.availableLanguages ?? []).forEach((code) => codes.add(code)),
        );
        setAvailable([...codes].sort());
      })
      .catch(() => {
        // The card still renders; the selection can be cleared without the list.
      });
    return () => {
      active = false;
    };
  }, []);

  const setting = settings.find((item) => item.key === "browse.chapter_languages");
  if (!setting) return null;
  const selected = String(setting.value ?? "")
    .split(",")
    .map((code) => code.trim())
    .filter(Boolean);
  const toggle = (code: string) => {
    const next = selected.includes(code)
      ? selected.filter((item) => item !== code)
      : [...selected, code];
    void save(setting, next.join(","));
  };

  return (
    <SettingsCard
      title="Chapter languages"
      description="The default for sources that do not choose their own languages."
    >
      {available.length === 0 ? (
        <p className="text-sm text-zinc-500">No chapter languages have been seen yet.</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {available.map((code) => (
            <label
              key={code}
              className={`inline-flex min-h-11 cursor-pointer items-center gap-2 rounded-full border px-4 py-1.5 text-xs ${
                selected.includes(code)
                  ? "border-amber-400 bg-amber-400/10 text-amber-200"
                  : "border-zinc-800 text-zinc-400"
              }`}
            >
              <input
                type="checkbox"
                checked={selected.includes(code)}
                onChange={() => toggle(code)}
                className="size-4 accent-amber-400"
              />
              {languageLabel(code)}
            </label>
          ))}
        </div>
      )}
      {selected.length > 0 && (
        <button
          type="button"
          onClick={() => void save(setting, "")}
          className="mt-3 inline-flex items-center gap-1 text-xs text-zinc-400 hover:text-white"
        >
          <RotateCcw size={12} /> Show every language
        </button>
      )}
    </SettingsCard>
  );
}
