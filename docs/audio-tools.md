# Audio tools

BeatportDL-UI can analyze local files, split stems, and loudness-normalize them from the **Audio** tab. Work runs as Queue jobs (same in-memory job system as downloads).

## Quick start

1. Install [ffmpeg](https://ffmpeg.org/) (already required for tags).
2. Build optional binaries:

```bash
make audio-analyzer-mcp   # analysis via MCP
make audio-analyzer-cli   # analysis via CLI dump
make stem-splitter        # ONNX HT-Demucs (~200MB model on first run)
```

3. Open the UI → **Audio** → pick a file or directory (empty = Settings output folder).
4. Watch progress under **Queue**.

Paths can also be set under **Settings → Audio tools** (preferred over relying on `PATH`).

## What each panel does

### Analyze

Runs [audio-analyzer-rs](../third_party/audio-analyzer-rs) (git submodule) and turns its **formatted text** into structured JSON for the UI.

| Kind | Meaning |
|------|---------|
| `full_analysis` | Everything (default) |
| `audio_info` | Duration, sample rate, samples |
| `spectral_features` | Brightness, bands, LUFS, stereo, … |
| `harmonic_analysis` | Key / mode / pitch classes |
| `rhythm_analysis` | Tempo, beats, stability |

**Backends**

| Backend | Behavior |
|---------|----------|
| `auto` | Prefer MCP; fall back to CLI |
| `mcp` | Spawn `mcp-server`, NDJSON stdio JSON-RPC, call one tool |
| `cli` | Run `cli <file>` (always a full dump), then parse the requested kind |

The analyzer does not emit JSON. Go parsers in `internal/audio/parse.go` scrape the text. Do not patch the Rust crate for JSON unless you deliberately change that contract.

**Cursor vs UI:** `.cursor/mcp.json` can point Cursor/agents at the same `mcp-server` binary. The web UI never talks to Cursor; it shells out to the binary itself.

**Analysis cache**

When the analysis store is available, analyze jobs check the database before spawning the analyzer. A cache **hit** reuses the stored payload and marks the track message `cached` with `source: "cache"`. Enable **Force re-analyze** (Analyze panel checkbox or `"force": true` in the API) to skip the cache.

Cache freshness uses the absolute cleaned path plus `file_size` and `mtime_unix`. A stored `full_analysis` row satisfies requests for narrower kinds (payload is trimmed via `audio.TrimAnalysis`).

If the database is unavailable at startup, analyze still works without persistence; the Audio tab shows a banner and Library returns `503`.

### Library

Fourth Audio tab: browse persisted analysis results in a searchable table.

| Filter | Query param |
|--------|-------------|
| Path substring | `q` |
| Key | `key` |
| BPM range | `bpm_min`, `bpm_max` |
| Pagination | `limit` (default 50), `offset` |

Table columns: path (basename), key, BPM, LUFS, kind, analyzed_at. Row click opens detail (summary metrics + full JSON). Actions: **Delete** (remove row) and **Re-analyze** (`POST /api/audio/analyze` with `force: true`).

### Stems

Runs `stem-splitter` (crate `stem-splitter-core` 1.2.0) to write four WAV stems: vocals, drums, bass, other under `<basename>_stems/`.

| Provider | Typical use |
|----------|-------------|
| `auto` | Library picks; on Apple Silicon the app prefers CoreML |
| `coreml` | macOS Apple Silicon (Neural Engine / GPU) |
| `xnnpack` | Fast CPU path |
| `cpu` | Forced CPU (`STEMMER_FORCE_CPU=1`) |

Intel Macs should use `auto` / `xnnpack` / `cpu` (CoreML is not useful for this model there).

### Normalize

Two-pass ffmpeg EBU R128 `loudnorm`. Defaults: I=-14 LUFS, TP=-1.5 dBTP, LRA=11.

- Default output: sibling `*_normalized.<ext>`
- Optional overwrite (temp file then rename when replacing the original)

## Architecture

```
Audio tab ──POST /api/audio/{analyze|stems|normalize}──► Job (kind=…)
         └──GET /api/audio/library──────────────────────► analysis store
                                                              │
                     ┌────────────────────────────────────────┤
                     ▼                                        ▼
              WebSocket job_update                     Queue UI
                     │
        ┌────────────┼────────────┬───────────────┐
        ▼            ▼            ▼               ▼
   internal/audio  stem-splitter  ffmpeg   internal/audio/store
   Analyze()       SplitStems()   Normalize()  (GORM SQLite | Postgres)
        │                                        ▲ cache get/upsert
   mcp-server | cli  ──text──► Parse() ──► []Analysis JSON
```

| Layer | Location |
|-------|----------|
| Package | `internal/audio/` |
| Analysis store | `internal/audio/store/` (GORM; default SQLite at `{configDir}/analysis.db`) |
| HTTP | `internal/server/audio_handlers.go` |
| Routes | `internal/server/server.go` |
| UI | `web/index.html`, `web/js/app.js` (Audio view: Analyze \| Stems \| Normalize \| Library) |
| Config | `internal/config/config.go` → `~/.config/beatportdl-ui/config.yml` |

### Job kinds

`download` | `analyze` | `stems` | `normalize`

Analyze jobs attach `analysis` on the job payload when done. Stems/normalize attach output paths to `files` (ZIP via existing job ZIP endpoint when files exist).

### File listing

`ListAudioFiles` accepts a file or a **non-recursive** directory. Extensions: `.flac`, `.m4a`, `.mp3`, `.wav`, `.ogg`, `.aac`.

### Binary resolution

Configured path → next to the running executable → repo-relative `third_party/...` or `dist/tools/bin/...` → `PATH`.

## HTTP API

| Method | Path | Body / query |
|--------|------|--------------|
| GET | `/api/audio/tools` | — binary/ffmpeg presence; includes `"analysis_store": true/false` |
| POST | `/api/audio/analyze` | `{ "path", "kind", "backend", "force?" }` → `202 { "job_id" }` |
| POST | `/api/audio/normalize` | `{ "path", "overwrite", "target_lufs?" }` |
| POST | `/api/audio/stems` | `{ "path", "provider" }` |
| GET | `/api/audio/library` | `?q=&key=&bpm_min=&bpm_max=&limit=&offset=` → `{ "items", "total" }`; `503` if store unavailable |
| GET | `/api/audio/library/{id}` | Full row + `"analysis"` payload; `404` if missing |
| DELETE | `/api/audio/library/{id}` | Remove row; `{ "status": "deleted" }` |

Empty `path` uses `output_dir` from settings.

## Config keys

| Key | Default |
|-----|---------|
| `audio_analyzer_backend` | `auto` |
| `audio_analyzer_mcp_path` | _(auto-detect)_ |
| `audio_analyzer_cli_path` | _(auto-detect)_ |
| `stem_splitter_path` | _(auto-detect)_ |
| `stem_provider` | `auto` |
| `normalize_target_lufs` | `-14` |
| `normalize_true_peak` | `-1.5` |
| `normalize_lra` | `11` |
| `analysis_db_driver` | `sqlite` |
| `analysis_db_dsn` | _(empty)_ |

Note: `normalize_target_lufs: 0` is treated as unset and becomes `-14`.

**Analysis database:** Settings → **Analysis database**. `analysis_db_driver` is `sqlite` or `postgres`. When driver is `sqlite` and DSN is empty, the file defaults to `~/.config/beatportdl-ui/analysis.db`. Postgres requires a full connection URL (e.g. `postgres://user:pass@localhost/beatportdl?sslmode=disable`). If open or migrate fails at startup, the app logs an error and continues without cache or Library (`GET /api/audio/tools` reports `"analysis_store": false`).

## Tests

```bash
go test ./internal/audio/... -count=1
go build ./...
```

Agent-oriented workflow notes: [`.claude/skills/audio-tools/SKILL.md`](../.claude/skills/audio-tools/SKILL.md).  
Implementation plan: [`docs/superpowers/plans/2026-09-11-audio-tools.md`](superpowers/plans/2026-09-11-audio-tools.md).
