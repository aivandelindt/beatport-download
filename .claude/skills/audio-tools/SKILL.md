# Audio tools (Analyze / Stems / Normalize / Library research)

## When to use

Use this skill when changing BeatportDL-UI local audio processing: catalog analysis via audio-analyzer-rs, stem separation, loudness normalize, Library research overlays (Camelot, beats, VU-style, chords, notes, spectrogram, issues), Audio tab UI, or `/api/audio/*` routes.

For broader research/editing (mashups, waveform editing), see `.claude/skills/audio-research/SKILL.md`. Mashups and waveform editing remain **deferred** unless explicitly requested.

## Architecture

- Package: `internal/audio` — subprocesses, text parsers, enrich (Camelot/findings), ffmpeg loudnorm / peaks / spectrogram / clipping / timelines / VU-style, stem-splitter, optional MIR worker, export zip
- Analysis store: `internal/audio/store` — GORM over SQLite (default `{configDir}/analysis.db`) or Postgres; `GetFresh` / `Upsert` / `List` / `GetByID` / `Delete`
- Handlers: `internal/server/audio_handlers.go`
- UI: top-bar **Audio** tab with panels Analyze | Stems | Normalize | **Library** (inspector: play + waveforms + research overlays)
- Jobs: same in-memory `Job` map as downloads; `kind` = `analyze|stems|normalize|chords|notes|download`
- Analysis: spawn `mcp-server` or `cli` from `third_party/audio-analyzer-rs`; **parse formatted text** into typed JSON (do not patch the Rust crate for JSON). Analyzer prints `Beat times:` with **all** measured times (rebuild MCP/CLI after crate changes).
- Enrich: after parse, `EnrichAnalysis` adds Camelot, half/double BPM, estimated beat grid (skipped when `beats_complete`), SHA-256, scalar findings
- Research (on demand): `BuildResearch` + ffmpeg RMS/ebur128/clipping/VU-style + section labels; merges `<basename>_mir/` chords/notes; spectrogram cached under `{configDir}/cache/audio-viz/`
- Cache: `runAnalyzeJob` checks store before analyze; `force: true` skips cache; upsert on success; `full_analysis` row satisfies partial kinds via `audio.TrimAnalysis`
- Degraded mode: if `store.Open` fails in `NewServer`, log error, leave `analysisStore` nil — analyze works, Library returns 503, banner in Audio tab
- Stems: `stem-splitter` CLI (`stem-splitter-core` 1.2.0 ONNX). Apple Silicon default CoreML; Intel auto/XNNPACK/CPU
- MIR (optional): `scripts/mir/worker.py` via configured `mir_python_path` / `mir_worker_path` — librosa chroma chords, Basic Pitch notes → `<basename>_mir/`
- Library inspector: `DiscoverStems` / `AllowedInspectFile`; `ComputePeaks` via ffmpeg (rekordbox-style RGB bands); waveform zoom 1–16× with horizontal scroll; rekordbox phrase bar under waveforms; stream `/file` and `/stems/{stem}`; overlays from `/research` + `/spectrogram`; live VU-style needle (Web Audio AnalyserNode)
- Normalize: two-pass ffmpeg `loudnorm` (EBU R128)

## API

| Method | Path | Body / query |
|--------|------|--------------|
| GET | `/api/audio/tools` | — binary presence + `analysis_store` + MIR (`librosa`, `basic_pitch`, …) |
| POST | `/api/audio/analyze` | `{path, kind, backend, force?}` |
| POST | `/api/audio/normalize` | `{path, overwrite, target_lufs?}` |
| POST | `/api/audio/stems` | `{path, provider}` |
| POST | `/api/audio/chords` | `{path, force?}` |
| POST | `/api/audio/notes` | `{path, sources?, force?}` |
| GET | `/api/audio/library` | `?q=&key=&bpm_min=&bpm_max=&limit=&offset=` (null-safe tempo/LUFS) |
| GET | `/api/audio/library/{id}` | full payload + camelot/issues + `stems` + `file_missing` |
| GET | `/api/audio/library/{id}/waveforms` | mix (+ stem) peaks + rekordbox-style `rgb_low`/`rgb_mid`/`rgb_high` (R bass · G mid · B high); needs ffmpeg |
| GET | `/api/audio/library/{id}/research` | timelines, VU-style, beats, chords/notes, sections, clipping, findings |
| GET | `/api/audio/library/{id}/spectrogram` | PNG spectrogram (cached) |
| GET | `/api/audio/library/{id}/export` | research zip (beats/chords/notes when present) |
| GET | `/api/audio/library/{id}/file` | stream mix |
| GET | `/api/audio/library/{id}/stems/{stem}` | stream stem WAV |
| DELETE | `/api/audio/library/{id}` | — |

Analysis `kind`: `audio_info`, `spectral_features`, `harmonic_analysis`, `rhythm_analysis`, `full_analysis`.

## Research deliverables

Measured / estimated / not performed:

- **Measured:** analyzer metrics, full measured beat times when analyzer prints `Beat times:`, ffmpeg clipping runs, RMS/LUFS series when filters work
- **Estimated:** Camelot, half/double BPM, extrapolated beat grid (only if beats incomplete), labeled sections, masking findings, **VU-style** timeline (τ≈65.1 ms, 0 VU = −18 dBFS — **not** IEC 60268-17), chord timeline (librosa), MIDI/notes (Basic Pitch)
- **Not performed:** capabilities missing tools/artifacts; **waveform editing**, mashups

## Build helpers

```bash
make audio-analyzer-mcp
make audio-analyzer-cli
make stem-splitter
# Optional MIR venv (Python 3.10–3.11 required for Basic Pitch notes):
#   mise install python@3.11   # or: brew install python@3.11
#   python3.11 -m venv .venv-mir
#   .venv-mir/bin/pip install -U pip
#   .venv-mir/bin/pip install -r scripts/mir/requirements.txt
#   # Settings → mir_python_path → <repo>/.venv-mir/bin/python
# Chords-only on any Python: pip install -r scripts/mir/requirements-chords.txt
go test ./internal/audio/... -count=1
go build ./...
```

Rebuild analyzer binaries after changing `third_party/audio-analyzer-rs` beat text output.

## Config keys

`audio_analyzer_backend`, `audio_analyzer_mcp_path`, `audio_analyzer_cli_path`, `stem_splitter_path`, `stem_provider`, `mir_python_path`, `mir_worker_path`, `normalize_target_lufs`, `normalize_true_peak`, `normalize_lra`, `analysis_db_driver` (`sqlite`|`postgres`), `analysis_db_dsn` (empty SQLite → `config.Dir()/analysis.db`; Postgres URL required when driver is postgres).

## Do not

- Add a new Cursor MCP for stems/normalize/MIR (UI shells out to local binaries)
- Recursively walk directories when listing audio files
- Patch `audio-analyzer-rs` for JSON unless deliberately changing that contract
- Claim loudest section is chorus / invent IEC VU compliance
- Commit unless the user asks
