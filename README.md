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

- Single binary embedding and system tray integration

## Web UI

MakiDoku includes a React 19 reader and library workspace in `web/`. The
frontend uses Vite+ for development, checking, testing, and production builds.
The generated `web/dist` directory is embedded by the Go daemon, so the
portable binary serves the same UI without a separate web server.

```bash
cd web
pnpm install --ignore-scripts
pnpm run dev           # development server with /api proxy
pnpm run test:run      # Vitest and Testing Library
pnpm run check         # Vite+ formatting and linting
pnpm run typecheck     # TypeScript compiler checks
pnpm run build         # refresh web/dist before Go embedding
```

The UI provides library categories, cross-source browsing, manga details,
chapter downloads, a source-grouped download queue with drag reordering and
downloader controls, tracker-aware title actions, reading history, backup
import/export, and paged or virtualized webtoon reading. Reader images are
served through the daemon image proxy, which reuses source cookies, headers,
anti-bot clearance, and optional unscrambling rather than exposing those
credentials to the browser.

## Tracker sync

Tracker credentials are encrypted in SQLite with `MAKIDOKU_SECRET`; the secret
is never included in backups or API responses. Configure provider OAuth client
settings in `.env`, then start authorization from the local API. The callback
must be the loopback URL shown by the API. MangaBaka also accepts an explicit
PAT through MakiDoku's local credential endpoint:

```bash
curl http://127.0.0.1:6254/api/trackers
curl 'http://127.0.0.1:6254/api/trackers/anilist/auth/start'
curl -X POST http://127.0.0.1:6254/api/trackers/mangabaka/token \
  -H 'Content-Type: application/json' \
  -d '{"accessToken":"mb-your-personal-access-token","metadata":{"auth":"pat"}}'
go run . sync --tracker anilist
```

The MangaBaka PAT is encrypted before it is stored. Authenticated MangaBaka
requests then send the stored PAT in the `X-API-Key` header.

Reader progress is stored locally and enqueues one durable job per bound
tracker when a chapter reaches 90 percent or is explicitly completed. AniList,
MyAnimeList, and MangaBaka expose status and progress writes; MangaUpdates and
Kitsu currently expose search only and report unsupported write capabilities.

## Quick start

```bash
go run . --help
go run . serve --port 6254 --bind 127.0.0.1
curl http://127.0.0.1:6254/api/health
```

Data is stored in `./data/makidoku.db` (WAL mode). Override with `--data-dir` or `MAKIDOKU_DATA_DIR`.

## Sources

The public catalog is used by default; point `--registry` or `MAKIDOKU_REGISTRY_URL` at another `index.json` URL, or at a path to a local mirror, to install from elsewhere. Every download is verified against the digest in the catalog before it is cached or executed, and a plugin built against another ABI version is rejected.

```bash
# Install a source headlessly, either from the catalog or from a local binary.
go run . install mangadex
go run . install --path /path/to/mangadex.wasm

# The daemon exposes the same operations when it is running.
curl http://127.0.0.1:6254/api/sources/catalog
curl -X POST http://127.0.0.1:6254/api/sources/install -d '{"id":"mangadex"}'
curl -X POST http://127.0.0.1:6254/api/sources/install -d '{"path":"/path/to/mangadex.wasm"}'

# List installed sources and remove one.
curl http://127.0.0.1:6254/api/sources
curl -X DELETE http://127.0.0.1:6254/api/sources/mangadex

# Browse a source. Title and chapter identifiers travel as query parameters.
curl http://127.0.0.1:6254/api/sources/mangadex/filters
curl 'http://127.0.0.1:6254/api/sources/mangadex/search?q=Yosuga+no+Sora&page=1'
curl 'http://127.0.0.1:6254/api/sources/mangadex/details?mangaId=<id>'
curl 'http://127.0.0.1:6254/api/sources/mangadex/pages?chapterId=<id>'
```

Requests run through the daemon's own network stack, so plugins are not subject to browser restrictions and send their headers unchanged. Upstream status codes reach the plugin verbatim; failures arrive as one of the standardized error codes, which the API reports as `{"error":{"code":"...","message":"..."}}`.

## Downloads

The downloader stores its queue in SQLite and resumes items that were interrupted while downloading. Image requests use the same per-source HTTP client, cookie jar and anti-bot clearance as plugin requests. Scrambled pages are passed through the source's `unscramble_image` export before they are written.

Every queue entry carries a position and workers claim the lowest position first, so the order a client shows is the order downloads start in. Clients rewrite that order through the reorder endpoint and it is kept across restarts. The downloader itself can be paused: a paused downloader claims nothing, and the chapter it was fetching returns to the queue with the pages it already saved. Pausing lasts for the session, so a restart resumes downloads.

The queue holds only the rows that can still make progress. A chapter leaves it when its artifact is written, cancelling removes the row outright, and a failed row stays with its error until it is retried or cancelled. Rows that finished under an earlier version are dropped when the queue is upgraded.

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
curl http://127.0.0.1:6254/api/download

curl -X POST http://127.0.0.1:6254/api/download \
  -H 'Content-Type: application/json' \
  -d '{"mangaId":"mangadex:<manga-id>","range":"1-10","format":"cbz"}'

curl -X POST http://127.0.0.1:6254/api/download/1/pause
curl -X POST http://127.0.0.1:6254/api/download/1/resume
curl -X POST http://127.0.0.1:6254/api/download/1/cancel

# Control the downloader itself. Both answer with the queue snapshot.
curl -X POST http://127.0.0.1:6254/api/download/pause-all
curl -X POST http://127.0.0.1:6254/api/download/resume-all

# Cancel every active entry, or a chosen set of them.
curl -X POST http://127.0.0.1:6254/api/download/cancel-all
curl -X POST http://127.0.0.1:6254/api/download/cancel \
  -H 'Content-Type: application/json' -d '{"itemIds":[1,2]}'

# Store the queue order as the item ids in display order. Answers 204.
curl -X POST http://127.0.0.1:6254/api/download/reorder \
  -H 'Content-Type: application/json' -d '{"itemIds":[2,1,3]}'
```

`GET /api/download` answers with the queue rows, the aggregate counters, and the downloader paused flag; the enqueue, pause-all, resume-all, cancel-all and cancel endpoints answer with the same payload so a client does not need a follow-up read.

Connect to `ws://127.0.0.1:6254/api/download/events` for queued, progress, pending, paused, resumed, canceled, completed and failed events, and for the item-less state and reordered events. Every event includes the aggregate counters and the paused flag, and an item event also carries the queue row it concerns. The completed and canceled events announce a row that has just left the queue, so a client drops it. A state event announces that the downloader was paused or resumed; a reordered event announces that the order changed, so clients refetch the snapshot for the order.

## Anti-bot challenges

When a source answers with an anti-bot challenge, the request fails with `CLOUDFLARE_BLOCKED`. To get past it, open the source in a normal browser, solve the challenge, then submit the resulting `cf_clearance` cookie together with that browser's user agent:

```bash
curl -X POST http://127.0.0.1:6254/api/sources/asurascans/clearance \
  -d '{"cookie":"<cf_clearance value>","userAgent":"<browser user agent>"}'
```

The cookie and agent are stored with the source and applied to its later requests. Set `--challenge-wait` to hold a blocked read while the clearance is submitted; the request is then replayed once. Reads are replayed transparently, while writes are always returned to the plugin so it decides whether re-invoking is safe.

## Tests

```bash
cd web
pnpm run build
cd ..
go test ./...
```

Source tests execute real plugins and reach the network. They are skipped unless `MAKIDOKU_NETWORK_TESTS=1` is set in the environment (for example in `.env`). They use the public catalog by default and read the same environment as the daemon; `MAKIDOKU_TEST_REGISTRY` overrides with another `index.json` URL or a path to a local mirror:

```bash
go test ./internal/engine/ -run TestSource -v
go test ./internal/downloader/ -run TestNetworkMangaDexDownload -v
```
