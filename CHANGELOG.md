# Changelog

All notable changes to MakiDoku will be documented in this file.

The format is based on Keep a Changelog and this project adheres to Semantic Versioning.

## [Unreleased]

- Import a backup exported by the Android app. A .tachibk file is validated
  against the installed sources before anything is written, sources can be
  mapped by hand, and titles whose source has no match are kept against a
  placeholder so their read state and history survive. Categories, chapters,
  read state, resume position, history, reading sessions, reader overrides,
  and tracker links are restored, and re-importing the same file merges onto
  the existing library.
- Restore the remaining fields a library backup carries: per-title custom
  info, notes and memo, source version and update strategy, per-chapter
  bookmark and source metadata, hidden categories, tracker remote url and
  privacy, browse feeds, saved searches, merged sources, alternative titles,
  tags, and the source metadata record. A title marked fetch-once is left out
  of the scheduled update, and a custom cover overrides the source cover.
- Derive chapter numbers for releases that do not declare one. A number
  introduced by a "ch." marker wins over a bare number, volume, version, and
  season markers are ignored, and alphabetic suffixes map onto fractional
  parts.
- Store per-title reader overrides. A title can pin its reader mode,
  direction, and image fit; the reader settings panel writes them, and an
  unset value follows the global setting.
- Add a statistics page with grouped counters, a daily reading chart, and a
  most-read titles table, fed by additive fields on the statistics endpoint.
- Add batch actions to the library. A selection mode offers bulk read,
  library, category, download, and remove operations and reports the titles
  that failed.
- Add incognito mode. While it is on, reading progress, history, session time,
  and automatic tracker updates are not recorded; the reader reports the
  stored position without writing anything. The flag persists as a privacy
  setting, has a header toggle with a banner, a reader badge, and its own
  settings section.
- Honor automatic downloads on the per-title refresh path, not only during the
  scheduled library update.
- Reuse an existing chapter record when a source re-issues the same chapter
  under a new identifier, instead of creating a duplicate and reporting a new
  release.
- Drop the unused manga.source_manga_id column. A title's source-side
  identifier is stored only in manga_sources, which is the authoritative link.
- Open the series page on the source site from the details view. The link
  previously pointed at the plugin base URL; the locator is now recorded from
  listings, refreshed when a source rotates it, and resolved through the
  search export for titles stored before it was recorded.
- Rework the library around one toolbar: a category selector, sort, and a
  filter, sort, and display sheet. Titles wrap to two lines, cards show an
  unread count and reading progress, and a continue action opens the last read
  chapter. The selected view is stored in the settings service, and the header
  control is the single search surface on every page.
- Split settings into a landing list and eight sections. The header search is
  contextual: on settings routes it searches every setting label, description,
  and section, and a result opens the owning section with the setting
  highlighted.

## [0.1.0] - 2026-08-26

- Initial version: persisted settings, chapter read state, history, global updater, updates feed, details enrichment, tracker editing, reading statistics, browse rework, suggestions, auto-download, download-ahead, auto-backup, log level control, embedded SPA hardening, system tray with build tag, and goreleaser release workflow.
