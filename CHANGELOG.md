# Changelog

All notable changes to MakiDoku will be documented in this file.

The format is based on Keep a Changelog and this project adheres to Semantic Versioning.

## [Unreleased]

- Rebuild the downloads page as a download queue. Entries are grouped by
  source with expandable headers, rows carry a drag handle and can be
  reordered by mouse or keyboard, the queue sorts by upload date or chapter
  number inside each source, and the row menu offers move to top, move series
  to top, move to bottom, move series to bottom, retry on failure, cancel,
  cancel all for this series, and a link to the title. A floating control
  pauses and resumes the downloader, and the queue overflow holds cancel all.
  The queue keeps only the entries that can still make progress: a chapter
  leaves it as its artifact is written, cancelling removes the row, and a
  migration drops the finished rows an earlier version left behind.
- Order the download queue and expose downloader controls. Queue entries carry
  a position and workers claim the lowest position first, so the order a
  client shows is the order downloads start in. The API gains pause-all,
  resume-all, cancel-all, cancel by item ids, and reorder, the queue snapshot
  reports whether the downloader is paused, and pausing returns the chapter
  being fetched to the queue with the pages it already saved.
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
- Edit a title's custom info, bookmark a chapter, manage merged sources, and
  list browse feeds and saved searches through the API. A validated backup is
  staged so importing it does not upload the file a second time, and the
  import report names the site an unmatched source appears to be.
- Manage merged sources and their metadata from the web client. The details
  page gains a sources dialog, and the browse tab lists feeds and saved
  searches and replays a stored search against the installed plugin.
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
- Keep the rest of the app responsive while covers load. A cover is proxied at
  the source's own resolution, so one image can be several megabytes: the
  transfer now runs against its own budget and a small concurrency limit,
  finishes and caches even when the view changes, and every card requests its
  cover only when it nears the viewport.
- Fix a source that stores a token or writes a log entry while a page list is
  fetched. The host answers such a call with an empty payload instead of an
  absent value, which a plugin build read as a missing string and reported as
  a parsing failure, so the chapter would not open.
- Load a source under its installation id even when the caller names it by its
  plugin key, so per-source storage and clearance stay with one identity.
- Import a backup's source identifiers exactly as recorded. A series or chapter
  locator is stored as the app that wrote the backup recorded it, so an
  installed source reads the value it knows, and a recorded page is kept only
  when the backup already carried an absolute URL.
- Adopt a stored chapter when a source re-issues it under a new locator. A
  refresh matches by published id or page URL, then by number with scanlator
  and language, then by number with title, then by title alone, and pairs only
  one-to-one matches so reading state, bookmarks, and downloads stay on one
  row.
- Pair a stored chapter by chapter number and release group when a backup
  records no language. The previous rung required the number, scanlator, and
  language together, which can never match an imported row, so a source that
  re-issued a chapter under a new locator gained a second row on every open.
  Two release groups publishing the same number stay separate.
- Resolve a stored series link through a ladder before giving up: the recorded
  locator, the recorded page, the last path segment of the locator, the
  trailing token of that segment for a source that names its series
  name.identifier, then a title search. The first candidate the source answers
  is persisted, so a title imported from another application stops failing to
  open and its source link stops falling back to the site root.
- Serve a title's stored details when an on-demand refresh fails, and report
  the failure on the aggregate as `refreshError`. The details view shows a
  non-blocking notice, so a source that is unreachable or still rejects its
  locator does not turn an imported title into an error page.
- Store a chapter upload time in seconds. A source declares it in
  milliseconds, and the refresh path wrote it as written while every other
  stored timestamp is in seconds. The value is folded at the boundary and a
  migration normalizes the rows written before it.
- Keep the locator a backup recorded beside the locator a source is read with.
  A refresh can rewrite the latter after a source rotates its identifiers, and
  a later import of the same backup still merges onto the same title and
  chapter instead of creating a second link and a second row.
- Pair a stored chapter whose locator ends in the identifier the source
  publishes. A backup records the site path, so a stored `/chapter/<id>` names
  the same chapter as the source id `<id>`. The ladder recognises that before
  it falls back to the number and title matches, which keeps a chapter paired
  even when the source publishes no title or release group for it.

## [0.1.0] - 2026-08-26

- Initial version: persisted settings, chapter read state, history, global updater, updates feed, details enrichment, tracker editing, reading statistics, browse rework, suggestions, auto-download, download-ahead, auto-backup, log level control, embedded SPA hardening, system tray with build tag, and goreleaser release workflow.
