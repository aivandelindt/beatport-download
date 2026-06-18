# BeatportDL-UI

Local web UI for downloading Beatport (and Beatsource URL parsing) content, with catalog search against the Beatport v4 API.

Go 1.22 monolith with an embedded vanilla HTML/CSS/JS frontend — no separate frontend build step.

## Features

- **Search** (default view) — keyword and/or genre browse, track and artist results
- **Download** — paste Beatport URLs (track, release, playlist, chart, artist)
- **Queue** — job progress over WebSocket, ZIP export
- **Fix Tags** — batch metadata repair with ffmpeg
- **Settings** — credentials, output directory, quality, workers

## Requirements

- Go 1.22+
- [ffmpeg](https://ffmpeg.org/) (metadata embedding)
- Beatport subscription credentials

## Quick start

```bash
go run .                    # opens browser on http://localhost:8989
go run . -port 8990         # alternate port
go run . -no-open           # don't open browser
```

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
│   ├── beatport/     # API client, types, metadata
│   ├── config/       # YAML config
│   ├── logging/      # slog + HTTP middleware
│   └── server/       # routes, handlers, jobs, WebSocket
└── web/              # embedded UI (go:embed)
    ├── index.html
    ├── css/style.css
    └── js/app.js
```

## Build

```bash
go build ./...
make build            # cross-compile via Makefile
```

Docker: `docker compose up` (port 8989, includes ffmpeg).

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
| GET | `/api/ws` | WebSocket progress |

## Further reading

Catalog search workflow for contributors: `.claude/skills/beatport-catalog-search/SKILL.md`
