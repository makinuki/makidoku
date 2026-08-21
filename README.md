# MakiDoku

Portable manga library, reader, and batch downloader. Single Go binary that hosts MakiNuki WASM plugins and serves an embedded React reader.

## Status

Current binary provides:

- Cobra CLI with `serve`, `download`, `sync`, `export` and `import` commands
- SQLite storage in WAL mode with initial schema for sources, manga, chapters, categories, reading progress, tracker bindings and download queue
- MakiNuki WASM host engine on Extism: host imports for fetching, plugin storage and logging, a 64 MB instance budget, and a per source instance pool
- Registry client that installs sources from a catalog with SHA-256 verification and ABI version enforcement
- Local REST API for source management, search, title details and chapter pages
- Persistent downloader queue with CBZ, ComicInfo.xml and extracted folder output
- Per-domain image throttling, retry backoff, pause, resume and cancel controls
- JSON backup export and restore for categories

Planned:

- Tracker integrations for AniList, MyAnimeList, MangaUpdates, Kitsu and MangaBaka
- React frontend with library views and reader
- Single binary embedding and system tray integration

## Quick start

```bash
go run . --help
go run . serve --port 8080 --bind 127.0.0.1
curl http://127.0.0.1:8080/api/health
```

Data is stored in `./data/makidoku.db` (WAL mode). Override with `--data-dir` or `MAKIDOKU_DATA_DIR`.

## Sources

The public catalog is used by default; point `--registry` or `MAKIDOKU_REGISTRY_URL` at another `index.json` URL, or at a path to a local mirror, to install from elsewhere. Every download is verified against the digest in the catalog before it is cached or executed, and a plugin built against another ABI version is rejected.

```bash
# Install a source headlessly, either from the catalog or from a local binary.
go run . install mangadex
go run . install --path /path/to/mangadex.wasm

# The daemon exposes the same operations when it is running.
curl http://127.0.0.1:8080/api/sources/catalog
curl -X POST http://127.0.0.1:8080/api/sources/install -d '{"id":"mangadex"}'
curl -X POST http://127.0.0.1:8080/api/sources/install -d '{"path":"/path/to/mangadex.wasm"}'

# List installed sources and remove one.
curl http://127.0.0.1:8080/api/sources
curl -X DELETE http://127.0.0.1:8080/api/sources/mangadex

# Browse a source. Title and chapter identifiers travel as query parameters.
curl http://127.0.0.1:8080/api/sources/mangadex/filters
curl 'http://127.0.0.1:8080/api/sources/mangadex/search?q=Yosuga+no+Sora&page=1'
curl 'http://127.0.0.1:8080/api/sources/mangadex/details?mangaId=<id>'
curl 'http://127.0.0.1:8080/api/sources/mangadex/pages?chapterId=<id>'
```

Requests run through the daemon's own network stack, so plugins are not subject to browser restrictions and send their headers unchanged. Upstream status codes reach the plugin verbatim; failures arrive as one of the standardized error codes, which the API reports as `{"error":{"code":"...","message":"..."}}`.

## Downloads

The downloader stores its queue in SQLite and resumes items that were interrupted while downloading. Image requests use the same per-source HTTP client, cookie jar and anti-bot clearance as plugin requests. Scrambled pages are passed through the source's `unscramble_image` export before they are written.

Downloaded chapters are stored under `<data-dir>/downloads/<source>/<title>/` by default. CBZ archives contain zero-padded page names and `ComicInfo.xml`. A title can instead use an extracted chapter directory.

```bash
# Download a range without starting the HTTP server. The source must already
# be installed in the selected data directory.
go run . download 'mangadex:<manga-id>' --chapters 1-50 --format cbz

# Override queue concurrency, per-domain page interval and output location.
go run . download 'mangadex:<manga-id>' --chapters 1-5 \
  --workers 2 --page-interval 750ms --download-dir ./manga
```

The daemon exposes queue snapshots, enqueue controls and a WebSocket event stream:

```bash
curl http://127.0.0.1:8080/api/download

curl -X POST http://127.0.0.1:8080/api/download \
  -H 'Content-Type: application/json' \
  -d '{"mangaId":"mangadex:<manga-id>","range":"1-10","format":"cbz"}'

curl -X POST http://127.0.0.1:8080/api/download/1/pause
curl -X POST http://127.0.0.1:8080/api/download/1/resume
curl -X POST http://127.0.0.1:8080/api/download/1/cancel
```

Connect to `ws://127.0.0.1:8080/api/download/events` for queued, progress, paused, resumed, canceled, completed and failed events. Each event includes the current queue item and aggregate downloader counters.

## Anti-bot challenges

When a source answers with an anti-bot challenge, the request fails with `CLOUDFLARE_BLOCKED`. To get past it, open the source in a normal browser, solve the challenge, then submit the resulting `cf_clearance` cookie together with that browser's user agent:

```bash
curl -X POST http://127.0.0.1:8080/api/sources/asurascans/clearance \
  -d '{"cookie":"<cf_clearance value>","userAgent":"<browser user agent>"}'
```

The cookie and agent are stored with the source and applied to its later requests. Set `--challenge-wait` to hold a blocked read while the clearance is submitted; the request is then replayed once. Reads are replayed transparently, while writes are always returned to the plugin so it decides whether re-invoking is safe.

## Tests

```bash
go test ./...
```

Source tests execute real plugins and reach the network. They are skipped unless `MAKIDOKU_NETWORK_TESTS=1` is set in the environment (for example in `.env`). They use the public catalog by default and read the same environment as the daemon; `MAKIDOKU_TEST_REGISTRY` overrides with another `index.json` URL or a path to a local mirror:

```bash
go test ./internal/engine/ -run TestSource -v
go test ./internal/downloader/ -run TestNetworkMangaDexDownload -v
```
