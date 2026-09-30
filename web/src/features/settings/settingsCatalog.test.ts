import { describe, expect, it } from "vite-plus/test";
import type { RuntimeSetting } from "../../api";
import {
  durationLabel,
  searchSettings,
  sectionForKey,
  settingLabel,
  settingsForSection,
  settingsSections,
} from "./settingsCatalog";

function setting(key: string, value: RuntimeSetting["value"], description = ""): RuntimeSetting {
  return { key, value, default: value, type: "string", description };
}

describe("settings catalog", () => {
  it("maps a setting key to its section by prefix", () => {
    expect(sectionForKey("reader.default_mode")).toBe("reader");
    expect(sectionForKey("appearance.date_format")).toBe("appearance");
    expect(sectionForKey("backup.auto_keep")).toBe("data");
    expect(sectionForKey("tracking.auto_sync")).toBe("tracking");
  });

  it("falls back to advanced for an unknown prefix", () => {
    expect(sectionForKey("experimental.thing")).toBe("advanced");
    expect(sectionForKey("unprefixed")).toBe("advanced");
  });

  it("filters the settings that belong to a section", () => {
    const settings = [
      setting("reader.fit", "width"),
      setting("library.update_interval", 1),
      setting("tracking.auto_sync", true),
    ];
    expect(settingsForSection(settings, "reader").map((item) => item.key)).toEqual(["reader.fit"]);
    expect(settingsForSection(settings, "downloads")).toEqual([]);
  });

  it("labels a setting from the last key segment", () => {
    expect(settingLabel("reader.direction")).toBe("Direction");
  });

  it("overrides labels that read like developer terms", () => {
    expect(settingLabel("library.update_interval")).toBe("Check for new chapters");
    expect(settingLabel("browse.hide_nsfw")).toBe("Hide adult sources");
    expect(settingLabel("downloads.concurrent")).toBe("Parallel downloads");
    expect(settingLabel("downloads.page_interval")).toBe("Page request interval");
    expect(settingLabel("downloads.retry_attempts")).toBe("Retries per page");
  });

  it("ranks section and label matches above description matches", () => {
    const settings = [
      setting("reader.default_mode", "single", "Default reader mode"),
      setting("advanced.log_level", "info", "Daemon log level"),
    ];
    const results = searchSettings(settings, "reader");
    expect(results.map((result) => result.id)).toEqual([
      "section:reader",
      "setting:reader.default_mode",
    ]);
    expect(results[0].to).toBe("/settings/reader");
    expect(results[1].to).toBe("/settings/reader#reader.default_mode");
  });

  it("matches labels case-insensitively and ignores an empty query", () => {
    const settings = [setting("library.update_interval", 1, "Automatic library update interval")];
    expect(searchSettings(settings, "")).toEqual([]);
    expect(searchSettings(settings, "UPDATE").map((result) => result.title)).toEqual([
      "Check for new chapters",
    ]);
  });

  it("limits the number of results", () => {
    const settings = Array.from({ length: 14 }, (_, index) =>
      setting(`advanced.key_${index}`, index, "Shared description"),
    );
    expect(searchSettings(settings, "")).toHaveLength(0);
    expect(searchSettings(settings, "key").length).toBeLessThanOrEqual(10);
    expect(searchSettings(settings, "shared")).toHaveLength(10);
  });

  it("describes durations in plain terms", () => {
    expect(durationLabel(0)).toBe("Never");
    expect(durationLabel(86_400_000_000_000)).toBe("Every day");
    expect(durationLabel(14 * 24 * 3_600_000_000_000)).toBe("Every 2 weeks");
    expect(durationLabel(36 * 3_600_000_000_000)).toBe("Every 36 hours");
    expect(durationLabel(30 * 60_000_000_000)).toBe("Every 30 minutes");
  });

  it("covers every setting prefix with a section", () => {
    const ids = settingsSections.map((section) => section.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(ids).toContain("data");
  });
});
