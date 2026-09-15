## Learned User Preferences

- Propagate `r.Context()` through Beatport client and HTTP handler methods; load the golang-context skill for Go API work.
- Capture repeatable feature workflows as project skills under `.claude/skills/`.
- Only create git commits when explicitly requested.
- Prefer configuring audio tool binary paths in Settings / `config.yml` over requiring extras on `PATH`.

## Learned Workspace Facts

- Go 1.22 monolith with embedded `web/` (vanilla HTML/CSS/JS via `go:embed`); no frontend build step.
- Config at `~/.config/beatportdl-ui/config.yml`; OAuth credentials at `~/.config/beatportdl-ui/beatportdl-credentials.json`.
- Default HTTP server port is 8989. A second start on the same port SIGTERMs the previous BeatportDL-UI process, then binds; other programs on the port are left alone (`-port` to pick another). Ctrl+C / SIGTERM shuts down gracefully.
- Makefile supports cross-compilation plus `audio-analyzer-mcp`, `audio-analyzer-cli`, and `stem-splitter`; verify with `go test ./internal/audio/ -count=1` and `go build ./...`.
- Beatport API base is `https://api.beatport.com/v4`; OAuth is required for catalog search and downloads.
- Outbound Beatport HTTP calls are logged via `logging.BeatportAPI` in `internal/beatport/client.go`.
- Audio tools live in `internal/audio`; analyze backends come from submodule `third_party/audio-analyzer-rs`.
- Analyzer MCP speaks stdio NDJSON JSON-RPC (rmcp), not Content-Length framing; CLI output is formatted text parsed to JSON.

## UI

- Top bar navigation (not sidebar): Search | Download | Queue | Fix Tags | Audio | Settings.
- Search is the default view. `.main` and views are full width.

## Audio tools

Workflow: `.claude/skills/audio-tools/SKILL.md`. Analyze via audio-analyzer-rs MCP (NDJSON) or CLI (text → JSON); stems via `stem-splitter` ONNX (CoreML on darwin/arm64; auto/CPU/XNNPACK on Intel); normalize via ffmpeg two-pass loudnorm; optional MIR (`scripts/mir/worker.py`) for chords/notes; Library research includes VU-style meter and full beat lists. API: `GET /api/audio/tools`, `POST /api/audio/analyze|normalize|stems|chords|notes`. Jobs use `kind` = `analyze|stems|normalize|chords|notes|download`.

## Catalog search

Full workflow: `.claude/skills/beatport-catalog-search/SKILL.md` (v1.4.0). Cursor rules: `.cursor/rules/` (`project-overview`, `go-backend`, `web-frontend`, `catalog-search`).

**App API:** `GET /api/search?q=&type=all|tracks|artists|releases|labels|charts&genre_id=&per_page=50-200&include_artists=&top_tracks=` and `GET /api/genres`.

- **All tab:** single `SearchCombined` call; five sections trimmed by Settings limits.
- **Tracks tab:** `collectSearchTracks` merges four sources; `per_page` controls track count only.
- **Settings limits** (default 10 each): `search_limit_artists`, `search_limit_releases`, `search_limit_labels`, `search_limit_charts`.
- Typed Beatport search JSON uses category keys (`tracks`, `artists`, …), not `results`.
- Use `order_by=-publish_date` for track, release, and chart search; omit on artist search (returns `{}`).
- Releases and charts sorted newest-first; multi-track releases include nested tracks from `ListReleaseTracks`.

**Search UX:**

- Explicit Search button or Enter; no input debounce.
- Requires 2+ characters or a selected genre.
- `.search-toolbar`: type tabs + genre / max results / toggles on one row.
- Tab click and filter changes call `syncSearchControlsFromUI()` then re-search when criteria are met.
- Collapsible sections: Artists, Releases, Tracks, Labels, Charts; Expand all / Collapse all also toggles release nested tracks.
- Artists, Labels, Charts: horizontal `.artist-card-row` cards; chart cards equal height, publish date as `YYYY-MM-DD` (`formatDateISO()`).
- Releases: list rows with reserved 18px chevron column and click-to-expand nested track table; row action buttons align right.
- Camelot keys: colored text (`.camelot-code`), not pill badges.
- Downloads from results use `POST /api/download`.
