# MakiDoku

Portable manga library, reader, and batch downloader. Single Go binary that hosts MakiNuki WASM plugins and serves an embedded React reader.

## Status

Current binary provides:

- Cobra CLI with `serve`, `download`, `sync`, `export` and `import` commands
- SQLite storage in WAL mode with initial schema for sources, manga, chapters, categories, reading progress, tracker bindings and download queue
- Local REST API with health and categories endpoints
- JSON backup export and restore for categories

Planned:

- MakiNuki WASM host engine with Extism and registry client
- Downloader engine with CBZ and ComicInfo support
- Tracker integrations for AniList, MyAnimeList, MangaUpdates, Kitsu and MangaBaka
- React frontend with library views and reader
- Single binary embedding and system tray integration

## Quick start

```bash
go run . --help
go run . serve --port 8080 --bind 127.0.0.1
# API
curl http://127.0.0.1:8080/api/health
curl http://127.0.0.1:8080/api/categories
# Backup
go run . export --output backup.json
go run . import backup.json
```

Data is stored in `./data/makidoku.db` (WAL mode). Override with `--data-dir` or `MAKIDOKU_DATA_DIR`.