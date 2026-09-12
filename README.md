# BeatportDL-UI

Local web UI for downloading Beatport (and Beatsource URL parsing) content, with catalog search against the Beatport v4 API.

Go 1.22 monolith with an embedded vanilla HTML/CSS/JS frontend — no separate frontend build step.

## Features

- **Search** (default view) — keyword and/or genre browse, track and artist results
- **Download** — paste Beatport URLs (track, release, playlist, chart, artist)
- **Queue** — job progress over WebSocket, ZIP export
- **Fix Tags** — batch metadata repair with ffmpeg
- **Audio** — analyze local files (with optional cache), split stems (HT-Demucs ONNX), loudness-normalize, browse analysis Library
- **Settings** — credentials, output directory, quality, workers, audio tool paths

## Requirements

- Go 1.22+
- [ffmpeg](https://ffmpeg.org/) (metadata embedding and normalize)
- Beatport subscription credentials
- Optional (Audio tab): Rust/`cargo` to build analyzer + stem-splitter binaries

## Quick start

```bash
go run .                    # opens browser on http://localhost:8989
go run . -port 8990         # alternate port
go run . -no-open           # don't open browser
```

Starting again on the same port stops the previous BeatportDL-UI process, then binds. Ctrl+C shuts the server down gracefully. Other programs occupying the port are left alone (`-port` to pick another).

Config: `~/.config/beatportdl-ui/config.yml`  
OAuth token cache: `~/.config/beatportdl-ui/beatportdl-credentials.json`

## Search

Search runs via the **Search** button or **Enter** (no auto-search while typing). Requires at least 2 characters **or** a selected genre.

### Options

| Control | Description |
|---------|-------------|
| Genre | Filter or browse catalog by Beatport genre |
| Max results | 50, 100, 150, or 200 |
| Include artists | On Tracks tab, also return matching artists |
| Artist top 10 | Attach up to 10 tracks per artist card |

### Track results table

Columns (left to right):

| Cover | Track | Artist | Label | Genre | BPM | Key | Released | Time | Actions |
|-------|-------|--------|-------|-------|-----|-----|----------|------|---------|

- **Cover** — track artwork thumbnail
- **Key** — Camelot code (colored text) with musical key name below
- **Time** — track duration from Beatport (`length`, e.g. `6:45`)
- Sortable headers (client-side); default sort: Released descending
- **Actions** — open on Beatport, download track

Artist results show cards with optional nested top-tracks table (same column layout).

### API

```
GET /api/genres
GET /api/search?q=...&type=all|tracks|artists&page=1&per_page=50&genre_id=...&include_artists=1&top_tracks=1
```

Requires configured credentials (same OAuth flow as downloads).

## Download

Paste a Beatport URL, choose quality (FLAC / AAC), and start. Supported URL types: track, release, playlist, chart, artist.

```
POST /api/download   { "url": "...", "quality": "lossless" }
```

## Project layout

```
├── main.go
├── internal/
│   ├── audio/        # analyze / stems / normalize
│   ├── beatport/     # API client, types, metadata
│   ├── config/       # YAML config
│   ├── logging/      # slog + HTTP middleware
│   └── server/       # routes, handlers, jobs, WebSocket
├── third_party/
│   └── audio-analyzer-rs/  # submodule (MCP + CLI)
└── web/              # embedded UI (go:embed)
    ├── index.html
    ├── css/style.css
    └── js/app.js
```

## Audio tools

The **Audio** tab analyzes, stem-splits, and normalizes files under a chosen path (or the settings output directory). Progress appears in **Queue**. Analysis results persist in SQLite by default (`~/.config/beatportdl-ui/analysis.db`); optional Postgres via Settings.

| Panel | Backend |
|-------|---------|
| Analyze | `audio-analyzer-rs` MCP (`mcp-server`) or CLI (`cli`); formatted text parsed to JSON; cache reuse unless **Force re-analyze** |
| Stems | `stem-splitter` (crate `stem-splitter-core` 1.2.0 ONNX). Apple Silicon defaults to CoreML; Intel uses CPU/XNNPACK |
| Normalize | ffmpeg two-pass EBU R128 `loudnorm` (sidecar `*_normalized` by default) |
| Library | Searchable analyses; inspector with play, mix/stem waveforms (ffmpeg peaks), metrics, delete, re-analyze |

```bash
make audio-analyzer-mcp   # third_party/.../target/release/mcp-server
make audio-analyzer-cli   # third_party/.../target/release/cli
make stem-splitter        # dist/tools/bin/stem-splitter
```

The UI shells out to these local binaries. Cursor’s `.cursor/mcp.json` audio-analyzer entry remains a separate local stdio MCP for agents (not Runlayer-managed); the web UI does not go through Cursor MCP.

Full guide: [docs/audio-tools.md](docs/audio-tools.md).

## Build

```bash
go build ./...
go test ./internal/audio/ -count=1
make macos            # cross-compile via Makefile
```

Docker: `docker compose up` (port 8989, includes ffmpeg).

## MCP server

This repo includes two local stdio MCP servers. Neither is Runlayer-managed.

### Beatport catalog

```bash
go run ./cmd/mcp-server
# or: make mcp-server
```

Tools: `beatport_test_auth`, `beatport_parse_url`, `beatport_get_genres`, `beatport_search`, `beatport_download_url`.

Uses the same config and credentials as the UI app:

- Config: `~/.config/beatportdl-ui/config.yml`
- OAuth cache: `~/.config/beatportdl-ui/beatportdl-credentials.json`

### Audio analyzer

Submodule at `third_party/audio-analyzer-rs`. Build the binary, then Cursor loads it from `.cursor/mcp.json`:

```bash
make audio-analyzer-mcp
make audio-analyzer-cli   # also used by the Audio tab CLI backend
```

Tools: `audio_info`, `spectral_features`, `harmonic_analysis`, `rhythm_analysis`, `full_analysis`, `compare`. Pass absolute local file paths.

Project MCP client config (`.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "audio-analyzer": {
      "command": "/absolute/path/to/beatport-download/third_party/audio-analyzer-rs/target/release/mcp-server"
    }
  }
}
```

## API routes

| Method | Path | Purpose |
|--------|------|---------|
| GET/POST | `/api/settings` | Load/save config |
| POST | `/api/auth/test` | Test Beatport credentials |
| GET | `/api/genres` | Genre list for search |
| GET | `/api/search` | Catalog search |
| POST | `/api/download` | Queue download job |
| GET | `/api/jobs` | List jobs |
| DELETE | `/api/jobs/{id}` | Remove job |
| GET | `/api/jobs/{id}/zip` | Download ZIP |
| POST | `/api/fix` | Fix tags in directory |
| GET | `/api/audio/tools` | Analyzer / stem-splitter / ffmpeg presence; `analysis_store` flag |
| POST | `/api/audio/analyze` | Queue analysis job (`force` skips cache) |
| POST | `/api/audio/normalize` | Queue loudnorm job |
| POST | `/api/audio/stems` | Queue stem-split job |
| GET | `/api/audio/library` | List persisted analyses (`q`, `key`, `bpm_min`, `bpm_max`, pagination) |
| GET | `/api/audio/library/{id}` | Single analysis + stems-on-disk map + `file_missing` |
| GET | `/api/audio/library/{id}/waveforms` | Mix (+ stem) peaks for canvas (needs ffmpeg) |
| GET | `/api/audio/library/{id}/file` | Stream mix audio |
| GET | `/api/audio/library/{id}/stems/{stem}` | Stream stem WAV (`vocals`\|`drums`\|`bass`\|`other`) |
| DELETE | `/api/audio/library/{id}` | Remove analysis row |
| GET | `/api/ws` | WebSocket progress |

## Further reading

- Catalog search: `.claude/skills/beatport-catalog-search/SKILL.md`
- Audio tools: [docs/audio-tools.md](docs/audio-tools.md) and `.claude/skills/audio-tools/SKILL.md`
