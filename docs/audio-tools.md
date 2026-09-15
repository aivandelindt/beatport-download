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

Table columns: path (basename), key (with Camelot when known), BPM, LUFS, kind, analyzed_at. Unmeasured metrics show as `—` (not `0.0`).

Row click opens an **inspector**:

- Mix play + waveform with **measured** (yellow) beat ticks; **estimated** ticks only when the measured list is incomplete; section + chord labels
- Within-track **energy** strip (custom 0–100 RMS percentile — labeled as such)
- **VU-style** timeline strip + live needle during playback (0 VU = −18 dBFS; labeled **not** IEC 60268-17)
- Log-frequency **spectrogram** PNG (`ffmpeg showspectrumpic`)
- Camelot, half/double BPM, clipping/masking **issues** (click Seek)
- Optional stem waveforms when `<basename>_stems/` exists
- **Estimate chords** / **Estimate notes** (optional MIR Python worker → `<basename>_mir/`)
- Actions: **Export** (zip: report.md, analysis.json, CSVs, beats.json, chords/notes when present, spectrogram), **Re-analyze**, **Delete**

Post-parse enrich (on analyze): Camelot from key+mode, half/double BPM, estimated beat grid (skipped when `beats_complete`), file SHA-256, true-peak/masking findings. On-demand research (Library open): ffmpeg RMS/LUFS/VU-style timelines, clipping scan, labeled sections heuristic, merge MIR artifacts.

**Section labels** (`intro`/`verse`/`chorus`/`build`/`drop`/`breakdown`/`outro`/`unknown`) combine novelty boundaries from the analyzer with RMS vs median — never “loudest = chorus”. Reliability is `estimated` or `low`.

**Beat grid honesty:** rebuilt analyzer prints `Beat times:` with **all** measured times (`beats_complete` when count matches). Older binaries that only print first N still get an extrapolated estimated grid.

**VU-style:** digital linear-amplitude one-pole (τ ≈ 65.1 ms). Display calibration 0 VU = −18 dBFS. Always labeled VU-style — not a standards-compliant VU meter.

**MIR (optional):** `scripts/mir/worker.py` — librosa chroma major/minor chord templates; Spotify Basic Pitch for notes/MIDI. Configure `mir_python_path` / `mir_worker_path`. Missing tools → buttons disabled; research keeps `chord_timeline` / `midi_transcription` in `not_performed`.

**MIR install (macOS):** Basic Pitch only supports Python **3.10–3.11**. On Python ≥3.12 (including system/mise `latest`), `pip install basic-pitch` tries to pull `tensorflow-macos`, which has no wheels — that is the conflict you hit. Use a dedicated 3.11 venv instead:

```bash
mise install python@3.11   # or brew install python@3.11
python3.11 -m venv .venv-mir
.venv-mir/bin/pip install -U pip
.venv-mir/bin/pip install -r scripts/mir/requirements.txt
```

Then set Settings → **MIR Python path** to the absolute path of `.venv-mir/bin/python`. Chords-only (no notes): `pip install -r scripts/mir/requirements-chords.txt` on any recent Python.

**Still deferred** (reported as not performed): waveform editing, mashups, patching audio-analyzer-rs for JSON.

Waveform peaks and spectrograms are cached under `~/.config/beatportdl-ui/cache/audio-viz/` (keyed by path+size+mtime). Original files are never modified by research/export.

### Stems

Runs `stem-splitter` (crate `stem-splitter-core` 1.2.0) to write four WAV stems under `<basename>_stems/`: `<basename>_vocals.wav`, `_drums.wav`, `_bass.wav`, `_other.wav` (plain `vocals.wav` etc. also accepted by the Library inspector).

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
Audio tab ──POST /api/audio/{analyze|stems|normalize|chords|notes}──► Job (kind=…)
         └──GET /api/audio/library──────────────────────► analysis store
                                                              │
                     ┌────────────────────────────────────────┤
                     ▼                                        ▼
              WebSocket job_update                     Queue UI
                     │
        ┌────────────┼────────────┬───────────────┬────────────┐
        ▼            ▼            ▼               ▼            ▼
   internal/audio  stem-splitter  ffmpeg   mir worker   store
   Analyze()       SplitStems()   Normalize()  chords/notes
        │                                        ▲ cache get/upsert
   mcp-server | cli  ──text──► Parse() ──► []Analysis JSON
```

| Layer | Location |
|-------|----------|
| Package | `internal/audio/` |
| Analysis store | `internal/audio/store/` (GORM; default SQLite at `{configDir}/analysis.db`) |
| HTTP | `internal/server/audio_handlers.go` |
| Routes | `internal/server/server.go` |
| UI | `web/index.html`, `web/js/app.js`, `web/js/library-player.js` (Audio view: Analyze \| Stems \| Normalize \| Library) |
| MIR worker | `scripts/mir/worker.py` + `scripts/mir/requirements.txt` |
| Config | `internal/config/config.go` → `~/.config/beatportdl-ui/config.yml` |

### Job kinds

`download` | `analyze` | `stems` | `normalize` | `chords` | `notes`

Analyze jobs attach `analysis` on the job payload when done. Stems/normalize attach output paths to `files` (ZIP via existing job ZIP endpoint when files exist). Chords/notes write `<basename>_mir/`.

### File listing

`ListAudioFiles` accepts a file or a **non-recursive** directory. Extensions: `.flac`, `.m4a`, `.mp3`, `.wav`, `.ogg`, `.aac`.

### Binary resolution

Configured path → next to the running executable → repo-relative `third_party/...` or `dist/tools/bin/...` → `PATH`.

## HTTP API

| Method | Path | Body / query |
|--------|------|--------------|
| GET | `/api/audio/tools` | — binary/ffmpeg/MIR presence; includes `"analysis_store"`, `"librosa"`, `"basic_pitch"` |
| POST | `/api/audio/analyze` | `{ "path", "kind", "backend", "force?" }` → `202 { "job_id" }` |
| POST | `/api/audio/normalize` | `{ "path", "overwrite", "target_lufs?" }` |
| POST | `/api/audio/stems` | `{ "path", "provider" }` |
| POST | `/api/audio/chords` | `{ "path", "force?" }` → `202 { "job_id" }` |
| POST | `/api/audio/notes` | `{ "path", "sources?", "force?" }` → `202 { "job_id" }` |
| GET | `/api/audio/library` | `?q=&key=&bpm_min=&bpm_max=&limit=&offset=` → `{ "items", "total" }`; `503` if store unavailable |
| GET | `/api/audio/library/{id}` | Full row + `"analysis"`; `"camelot"`; `"issues"`; null-safe tempo/LUFS/duration; `"stems"`; `"file_missing"` |
| GET | `/api/audio/library/{id}/waveforms` | Mix (+ stem) peak arrays for canvas; needs ffmpeg |
| GET | `/api/audio/library/{id}/research` | On-demand RMS/LUFS/energy/VU-style timelines, beat grid, chords/notes, labeled sections, clipping, findings |
| GET | `/api/audio/library/{id}/spectrogram` | `image/png` log-frequency spectrogram (cached) |
| GET | `/api/audio/library/{id}/export` | Zip of report.md, analysis.json, sections/beats/issues CSV, beats.json, chords/notes when present, spectrogram |
| GET | `/api/audio/library/{id}/file` | Stream mix audio (`Accept-Ranges`) |
| GET | `/api/audio/library/{id}/stems/{stem}` | Stream `vocals` \| `drums` \| `bass` \| `other` WAV |
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
| `mir_python_path` | _(python3 on PATH)_ |
| `mir_worker_path` | _(scripts/mir/worker.py)_ |
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
