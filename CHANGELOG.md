# Changelog

All notable changes to MakiDoku will be documented in this file.

The format is based on Keep a Changelog and this project adheres to Semantic Versioning.

## [Unreleased]

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

## [0.1.0] - 2026-08-26

- Initial version: persisted settings, chapter read state, history, global updater, updates feed, details enrichment, tracker editing, reading statistics, browse rework, suggestions, auto-download, download-ahead, auto-backup, log level control, embedded SPA hardening, system tray with build tag, and goreleaser release workflow.
