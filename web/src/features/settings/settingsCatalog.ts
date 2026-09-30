import {
  BookOpen,
  Compass,
  Database,
  Download,
  EyeOff,
  Library,
  Palette,
  RefreshCw,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import type { RuntimeSetting } from "../../api";

export type SettingsSectionId =
  | "appearance"
  | "library"
  | "reader"
  | "downloads"
  | "tracking"
  | "browse"
  | "data"
  | "privacy"
  | "advanced";

export type SettingsSection = {
  id: SettingsSectionId;
  title: string;
  subtitle: string;
  icon: LucideIcon;
  // prefixes are the part of a setting key before its first dot.
  prefixes: string[];
};

export const settingsSections: SettingsSection[] = [
  {
    id: "appearance",
    title: "Appearance",
    subtitle: "Date format",
    icon: Palette,
    prefixes: ["appearance"],
  },
  {
    id: "library",
    title: "Library",
    subtitle: "Automatic updates and categories",
    icon: Library,
    prefixes: ["library"],
  },
  {
    id: "reader",
    title: "Reader",
    subtitle: "Modes, navigation zones, display, and themes",
    icon: BookOpen,
    prefixes: ["reader"],
  },
  {
    id: "downloads",
    title: "Downloads",
    subtitle: "Automatic and parallel downloads",
    icon: Download,
    prefixes: ["downloads"],
  },
  {
    id: "tracking",
    title: "Tracking",
    subtitle: "Tracker accounts and sync activity",
    icon: RefreshCw,
    prefixes: ["tracking"],
  },
  {
    id: "browse",
    title: "Browse",
    subtitle: "Source visibility and plugins",
    icon: Compass,
    prefixes: ["browse"],
  },
  {
    id: "data",
    title: "Data and storage",
    subtitle: "Backup, restore, and reading statistics",
    icon: Database,
    prefixes: ["backup"],
  },
  {
    id: "privacy",
    title: "Privacy",
    subtitle: "Incognito mode and recorded activity",
    icon: EyeOff,
    prefixes: ["privacy"],
  },
  {
    id: "advanced",
    title: "Advanced",
    subtitle: "Logging and image cache",
    icon: Wrench,
    prefixes: ["advanced"],
  },
];

// Unknown prefixes land in Advanced so a new setting is never unreachable.
const fallbackSection: SettingsSectionId = "advanced";

export function sectionForKey(key: string): SettingsSectionId {
  const prefix = key.split(".")[0];
  return (
    settingsSections.find((section) => section.prefixes.includes(prefix))?.id ?? fallbackSection
  );
}

export function sectionById(id: string): SettingsSection | undefined {
  return settingsSections.find((section) => section.id === id);
}

export function settingsForSection(
  settings: RuntimeSetting[],
  id: SettingsSectionId,
): RuntimeSetting[] {
  return settings.filter((setting) => sectionForKey(setting.key) === id);
}

// Labels for keys whose generated names read like developer terms.
const settingLabelOverrides: Record<string, string> = {
  "library.update_interval": "Check for new chapters",
  "library.update_on_launch": "Update on startup",
  "reader.default_mode": "Default reading mode",
  "reader.fit": "Page fit",
  "reader.navigation": "Tap zones",
  "reader.webtoon_gap": "Webtoon spacing",
  "downloads.auto_download": "Automatic downloads",
  "downloads.download_ahead": "Download ahead",
  "downloads.concurrent": "Parallel downloads",
  "downloads.page_interval": "Page request interval",
  "downloads.retry_attempts": "Retries per page",
  "tracking.auto_sync": "Automatic sync",
  "backup.auto_interval": "Automatic backups",
  "backup.auto_keep": "Backups to keep",
  "browse.hide_nsfw": "Hide adult sources",
  "privacy.incognito": "Incognito mode",
  "advanced.log_level": "Log detail",
  "advanced.image_cache_days": "Image cache lifetime",
};

export function settingLabel(key: string) {
  const override = settingLabelOverrides[key];
  if (override) return override;
  return key
    .split(".")
    .at(-1)!
    .replaceAll("_", " ")
    .replace(/^./, (letter) => letter.toUpperCase());
}

const durationHour = 3_600_000_000_000;

// Duration settings present preset waits; raw nanosecond values never reach
// the screen.
export const durationOptions: Array<{ value: number; label: string }> = [
  { value: 0, label: "Never" },
  { value: 6 * durationHour, label: "Every 6 hours" },
  { value: 12 * durationHour, label: "Every 12 hours" },
  { value: 24 * durationHour, label: "Every day" },
  { value: 48 * durationHour, label: "Every 2 days" },
  { value: 72 * durationHour, label: "Every 3 days" },
  { value: 168 * durationHour, label: "Every week" },
];

// durationLabel describes a duration outside the preset list, for values
// written through the API or before the presets existed.
export function durationLabel(ns: number): string {
  const preset = durationOptions.find((option) => option.value === ns);
  if (preset) return preset.label;
  if (ns <= 0) return "Never";
  const hours = ns / durationHour;
  if (hours < 1) return `Every ${Math.round(ns / 60_000_000_000)} minutes`;
  if (hours % 24 === 0) {
    const days = hours / 24;
    return days % 7 === 0 ? `Every ${days / 7} weeks` : `Every ${days} days`;
  }
  return `Every ${hours} hours`;
}

export const settingOptions: Record<string, Array<{ value: string; label: string }>> = {
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
    { value: "screen", label: "Fit screen" },
    { value: "original", label: "Original size" },
  ],
  "reader.navigation": [
    { value: "default-manga", label: "Default manga" },
    { value: "l-shaped", label: "L-shaped" },
    { value: "edge-only", label: "Edges only" },
    { value: "disabled", label: "Disabled" },
  ],
  "reader.theme": [
    { value: "dark", label: "Dark" },
    { value: "amoled", label: "AMOLED black" },
    { value: "paper", label: "Paper" },
    { value: "light", label: "Light" },
  ],
  "advanced.log_level": [
    { value: "debug", label: "Debug" },
    { value: "info", label: "Info" },
    { value: "warn", label: "Warning" },
    { value: "error", label: "Error" },
  ],
};

export type SettingsSearchResult = {
  id: string;
  title: string;
  breadcrumb: string;
  to: string;
};

const searchLimit = 10;

// searchSettings matches section titles and setting labels first, then setting
// descriptions, and returns at most searchLimit results.
export function searchSettings(settings: RuntimeSetting[], query: string): SettingsSearchResult[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return [];
  const titleMatches: SettingsSearchResult[] = [];
  const descriptionMatches: SettingsSearchResult[] = [];
  for (const section of settingsSections) {
    if (!section.title.toLowerCase().includes(needle)) continue;
    titleMatches.push({
      id: `section:${section.id}`,
      title: section.title,
      breadcrumb: "Settings",
      to: `/settings/${section.id}`,
    });
  }
  for (const setting of settings) {
    const sectionId = sectionForKey(setting.key);
    const section = sectionById(sectionId);
    if (!section) continue;
    const label = settingLabel(setting.key);
    const result: SettingsSearchResult = {
      id: `setting:${setting.key}`,
      title: label,
      breadcrumb: section.title,
      to: `/settings/${sectionId}#${setting.key}`,
    };
    if (label.toLowerCase().includes(needle)) {
      titleMatches.push(result);
    } else if (setting.description.toLowerCase().includes(needle)) {
      descriptionMatches.push(result);
    }
  }
  return [...titleMatches, ...descriptionMatches].slice(0, searchLimit);
}
