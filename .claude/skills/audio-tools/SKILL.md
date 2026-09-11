# Audio tools (Analyze / Stems / Normalize)

## When to use

Use this skill when changing BeatportDL-UI local audio processing: catalog analysis via audio-analyzer-rs, stem separation, loudness normalize, Audio tab UI, or `/api/audio/*` routes.

## Architecture

- Package: `internal/audio` — subprocesses, text parsers, ffmpeg loudnorm, stem-splitter
- Analysis store: `internal/audio/store` — GORM over SQLite (default `{configDir}/analysis.db`) or Postgres; `GetFresh` / `Upsert` / `List` / `GetByID` / `Delete`
- Handlers: `internal/server/audio_handlers.go`
- UI: top-bar **Audio** tab with panels Analyze | Stems | Normalize | **Library**
- Jobs: same in-memory `Job` map as downloads; `kind` = `analyze|stems|normalize|download`
- Analysis: spawn `mcp-server` or `cli` from `third_party/audio-analyzer-rs`; **parse formatted text** into typed JSON (do not patch the Rust crate for JSON)
- Cache: `runAnalyzeJob` checks store before analyze; `force: true` skips cache; upsert on success; `full_analysis` row satisfies partial kinds via `audio.TrimAnalysis`
- Degraded mode: if `store.Open` fails in `NewServer`, log error, leave `analysisStore` nil — analyze works, Library returns 503, banner in Audio tab
- Stems: `stem-splitter` CLI (`stem-splitter-core` 1.2.0 ONNX). Apple Silicon default CoreML; Intel auto/XNNPACK/CPU
- Normalize: two-pass ffmpeg `loudnorm` (EBU R128)

## API

| Method | Path | Body / query |
|--------|------|--------------|
| GET | `/api/audio/tools` | — binary presence + `analysis_store` bool |
| POST | `/api/audio/analyze` | `{path, kind, backend, force?}` |
| POST | `/api/audio/normalize` | `{path, overwrite, target_lufs?}` |
| POST | `/api/audio/stems` | `{path, provider}` |
| GET | `/api/audio/library` | `?q=&key=&bpm_min=&bpm_max=&limit=&offset=` |
| GET | `/api/audio/library/{id}` | — full payload in `analysis` |
| DELETE | `/api/audio/library/{id}` | — |

Analysis `kind`: `audio_info`, `spectral_features`, `harmonic_analysis`, `rhythm_analysis`, `full_analysis`.

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
- Commit unless the user asks
