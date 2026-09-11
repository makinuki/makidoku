# Changelog

All notable changes to MakiDoku will be documented in this file.

The format is based on Keep a Changelog and this project adheres to Semantic Versioning.

## [Unreleased]

- Honor automatic downloads on the per-title refresh path, not only during the
  scheduled library update.
- Reuse an existing chapter record when a source re-issues the same chapter
  under a new identifier, instead of creating a duplicate and reporting a new
  release.

## [0.1.0] - 2026-08-26

- Initial version: persisted settings, chapter read state, history, global updater, updates feed, details enrichment, tracker editing, reading statistics, browse rework, suggestions, auto-download, download-ahead, auto-backup, log level control, embedded SPA hardening, system tray with build tag, and goreleaser release workflow.
