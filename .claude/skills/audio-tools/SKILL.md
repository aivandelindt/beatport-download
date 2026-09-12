# Audio tools (Analyze / Stems / Normalize / Library research)

## When to use

Use this skill when changing BeatportDL-UI local audio processing: catalog analysis via audio-analyzer-rs, stem separation, loudness normalize, Library research overlays (Camelot, beats, spectrogram, issues), Audio tab UI, or `/api/audio/*` routes.

For the broader research/editing skill (MIDI, mashups, waveform editing), see `.claude/skills/audio-research/SKILL.md`. Those capabilities are **deferred** unless explicitly requested.

## Architecture

- Package: `internal/audio` — subprocesses, text parsers, enrich (Camelot/findings), ffmpeg loudnorm / peaks / spectrogram / clipping / timelines, stem-splitter, export zip
- Analysis store: `internal/audio/store` — GORM over SQLite (default `{configDir}/analysis.db`) or Postgres; `GetFresh` / `Upsert` / `List` / `GetByID` / `Delete`
- Handlers: `internal/server/audio_handlers.go`
- UI: top-bar **Audio** tab with panels Analyze | Stems | Normalize | **Library** (inspector: play + waveforms + research overlays)
- Jobs: same in-memory `Job` map as downloads; `kind` = `analyze|stems|normalize|download`
- Analysis: spawn `mcp-server` or `cli` from `third_party/audio-analyzer-rs`; **parse formatted text** into typed JSON (do not patch the Rust crate for JSON)
- Enrich: after parse, `EnrichAnalysis` adds Camelot, half/double BPM, estimated beat grid, SHA-256, scalar findings
- Research (on demand): `BuildResearch` + ffmpeg RMS/ebur128/clipping + section labels; spectrogram cached under `{configDir}/cache/audio-viz/`
- Cache: `runAnalyzeJob` checks store before analyze; `force: true` skips cache; upsert on success; `full_analysis` row satisfies partial kinds via `audio.TrimAnalysis`
- Degraded mode: if `store.Open` fails in `NewServer`, log error, leave `analysisStore` nil — analyze works, Library returns 503, banner in Audio tab
- Stems: `stem-splitter` CLI (`stem-splitter-core` 1.2.0 ONNX). Apple Silicon default CoreML; Intel auto/XNNPACK/CPU
- Library inspector: `DiscoverStems` / `AllowedInspectFile`; `ComputePeaks` via ffmpeg; stream `/file` and `/stems/{stem}`; overlays from `/research` + `/spectrogram`
- Normalize: two-pass ffmpeg `loudnorm` (EBU R128)

## API

| Method | Path | Body / query |
|--------|------|--------------|
| GET | `/api/audio/tools` | — binary presence + `analysis_store` bool |
| POST | `/api/audio/analyze` | `{path, kind, backend, force?}` |
| POST | `/api/audio/normalize` | `{path, overwrite, target_lufs?}` |
| POST | `/api/audio/stems` | `{path, provider}` |
| GET | `/api/audio/library` | `?q=&key=&bpm_min=&bpm_max=&limit=&offset=` (null-safe tempo/LUFS) |
| GET | `/api/audio/library/{id}` | full payload + camelot/issues + `stems` + `file_missing` |
| GET | `/api/audio/library/{id}/waveforms` | mix (+ stem) peaks; needs ffmpeg |
| GET | `/api/audio/library/{id}/research` | timelines, beats, labeled sections, clipping, findings |
| GET | `/api/audio/library/{id}/spectrogram` | PNG spectrogram (cached) |
| GET | `/api/audio/library/{id}/export` | research zip |
| GET | `/api/audio/library/{id}/file` | stream mix |
| GET | `/api/audio/library/{id}/stems/{stem}` | stream stem WAV |
| DELETE | `/api/audio/library/{id}` | — |

Analysis `kind`: `audio_info`, `spectral_features`, `harmonic_analysis`, `rhythm_analysis`, `full_analysis`.

## Research deliverables (milestone 1)

Measured / estimated / not performed:

- **Measured:** analyzer metrics, measured first-N beats, ffmpeg clipping runs, RMS/LUFS series when filters work
- **Estimated:** Camelot, half/double BPM, extrapolated beat grid, labeled sections, masking findings
- **Not performed:** MIDI, chords, VU meter, waveform editing, mashups

## Build helpers

```bash
make audio-analyzer-mcp
make audio-analyzer-cli
make stem-splitter
go test ./internal/audio/... -count=1
go build ./...
```

## Config keys

`audio_analyzer_backend`, `audio_analyzer_mcp_path`, `audio_analyzer_cli_path`, `stem_splitter_path`, `stem_provider`, `normalize_target_lufs`, `normalize_true_peak`, `normalize_lra`, `analysis_db_driver` (`sqlite`|`postgres`), `analysis_db_dsn` (empty SQLite → `config.Dir()/analysis.db`; Postgres URL required when driver is postgres).

## Do not

- Add a new Cursor MCP for stems/normalize (UI shells out to local binaries)
- Recursively walk directories when listing audio files
- Patch `audio-analyzer-rs` for JSON unless deliberately changing that contract
- Claim loudest section is chorus / invent VU compliance
- Commit unless the user asks
