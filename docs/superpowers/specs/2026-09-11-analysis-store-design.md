# Analysis Result Store — Design

**Date:** 2026-09-11  
**Status:** Approved  
**Goal:** Persist audio analysis results so the app has a durable, searchable library and can skip re-analysis when a file is unchanged, with SQLite by default and Postgres as an optional backend via GORM.

## Context

Today, `POST /api/audio/analyze` attaches `[]audio.Analysis` only to the in-memory Queue `Job`. Results vanish on process restart. There is no database in BeatportDL-UI yet.

## Requirements

1. **Durable library** — survive restarts; browse/search past analyses.
2. **Cache** — if the same path is analyzed again and the file is unchanged, reuse stored payload.
3. **Multi-backend** — SQLite default; Postgres optional through one ORM (GORM).
4. **Library UI** — searchable/filterable table with detail view (not cache-only).

## Non-goals

- Sidecar `.analysis.json` next to audio files.
- Storing stems or normalize outputs in this store (v1).
- Multi-user auth or row-level security on Postgres.
- Dual-write / sync between SQLite and Postgres.
- Crashing the app if the DB is unavailable.

## Architecture

```
config (driver + DSN)
        │
        ▼
 gorm.Open(sqlite | postgres)
        │
 internal/audio/store  ←── GORM model + Store API
        │
   ┌────┴────┐
Analyze job  Library HTTP + Audio “Library” panel
(cache get/  list / get / delete / re-analyze
 upsert)
```

- Package: `internal/audio/store`
- ORM: **GORM** with dialects SQLite (`modernc.org/sqlite`, pure Go, keeps `CGO_ENABLED=0`) and Postgres (`gorm.io/driver/postgres`)
- Analyze path continues to use `internal/audio.Analyze`; store wraps cache around it in `runAnalyzeJob` (or a small helper `AnalyzeCached`)

## Configuration

New fields on `config.Config` (YAML/JSON):

| Key | Default | Meaning |
|-----|---------|---------|
| `analysis_db_driver` | `sqlite` | `sqlite` \| `postgres` |
| `analysis_db_dsn` | empty | SQLite: empty → `{configDir}/analysis.db`. Postgres: required URL, e.g. `postgres://user:pass@localhost/beatportdl?sslmode=disable` |

Settings UI: “Analysis database” section (driver select + DSN text field). Prefer Settings over env-only.

Open DB once at server start; hold `*store.Store` on `Server` (or lazy-init). If open/`AutoMigrate` fails: log error, set store to nil, analyze works without persist; Audio tab shows a banner.

## Schema (`analysis_results`)

| Column | Type | Notes |
|--------|------|--------|
| `id` | uint PK | |
| `path` | string | Absolute cleaned path; unique with `kind` |
| `kind` | string | `audio_info`, `spectral_features`, `harmonic_analysis`, `rhythm_analysis`, `full_analysis` |
| `file_size` | int64 | Freshness |
| `mtime_unix` | int64 | Freshness |
| `key` | string | Denormalized for filters |
| `mode` | string | e.g. major/minor |
| `tempo_bpm` | float64 | |
| `lufs_integrated` | float64 | |
| `duration_sec` | float64 | |
| `source` | string | `mcp` \| `cli` (of producing run; responses from cache may set `source` to `cache` in API only) |
| `payload_json` | text | Full `audio.Analysis` JSON (includes `raw_text` inside payload if present) |
| `analyzed_at` | time | |
| `created_at` / `updated_at` | time | GORM |

**Unique index:** `(path, kind)`.

Indexes for Library filters: `key`, `tempo_bpm`, `lufs_integrated`, `analyzed_at`.

No separate `raw_text` column — keep raw text inside `payload_json` only.

## Caching rules

1. Normalize path with `filepath.Abs` + `filepath.Clean`.
2. `os.Stat` → `size`, `mtime`.
3. **Hit** if a row exists where:
   - `path` matches, and
   - `file_size` / `mtime_unix` match, and
   - `kind` equals requested **or** stored kind is `full_analysis` (satisfies any narrower kind by returning/trimming payload as needed).
4. **Miss / stale** or request `force=true` → run analyzer → upsert for the **requested** kind (if requested was partial but we ran full via CLI, store as `full_analysis` and optionally also the requested kind — **v1: store one row for the kind actually parsed/returned**; if CLI always yields full parse, upsert `full_analysis`).
5. Clarification for v1 implementers: after a successful analyze, upsert using `result.Kind` and denormalized fields from the struct; if `force` or cache miss produced `full_analysis`, that row is the one that later serves as a hit for partial kinds.

## Store API

```go
type Store struct { db *gorm.DB }

func Open(driver, dsn string) (*Store, error)
func (s *Store) Close() error

func (s *Store) GetFresh(ctx context.Context, path, kind string, size, mtime int64) (audio.Analysis, bool, error)
func (s *Store) Upsert(ctx context.Context, rec Record) error
func (s *Store) List(ctx context.Context, f ListFilter) ([]ListItem, int64, error)
func (s *Store) GetByID(ctx context.Context, id uint) (Record, error)
func (s *Store) Delete(ctx context.Context, id uint) error
```

`ListFilter`: `Q` (path substring), `Key`, `BPMMin`/`BPMMax`, `Limit`/`Offset`, `Sort` (default `analyzed_at desc`).

Propagate `context.Context` as first argument on all methods.

## HTTP API

| Method | Path | Behavior |
|--------|------|----------|
| GET | `/api/audio/library` | Query list; 503 if store unavailable |
| GET | `/api/audio/library/{id}` | Full payload |
| DELETE | `/api/audio/library/{id}` | Remove row |
| POST | `/api/audio/analyze` | Body adds optional `force` bool; uses cache when store non-nil |

Existing WebSocket job progress unchanged; cached files can complete quickly with track message `cached`.

## UI

Audio tab panels: **Analyze | Stems | Normalize | Library**

Library panel:

- Filter row: search (path), optional key, optional BPM range
- Table columns: path, key, BPM, LUFS, kind, analyzed_at
- Row click → detail (summary metrics + JSON)
- Actions: Delete; Re-analyze (`force: true` for that path)

Match existing Audio/Fix Tags visual patterns (vanilla HTML/CSS/JS).

## Error handling

| Case | Behavior |
|------|----------|
| DB open / migrate fail | Log; `store=nil`; analyze without cache; banner |
| Upsert fail after analyze | Job still `done`; slog warning |
| List/Get with nil store | `503` + clear error |
| File missing on re-analyze | Job track error as today |

## Testing

- Unit tests for store with temp SQLite file: upsert, GetFresh hit/miss on mtime change, List `q` filter, Delete.
- No Postgres required in default CI; postgres driver linked for compile.
- Verify: `go test ./internal/audio/... -count=1` and `go build ./...`.

## Documentation

- Update `docs/audio-tools.md` and README API table.
- Extend `.claude/skills/audio-tools/SKILL.md` with store/library notes.

## Implementation order (for the later plan)

1. GORM deps + `store` package + SQLite tests  
2. Wire Open into server + config/settings  
3. Cache in `runAnalyzeJob` + `force`  
4. Library HTTP handlers  
5. Library UI panel  
6. Docs  

## Decisions log

| Decision | Choice |
|----------|--------|
| Purpose | Durable library + cache |
| Default DB | SQLite file in config dir |
| Optional DB | Postgres via same GORM models |
| ORM | GORM |
| UI | Full Library panel (searchable table) |
| Freshness | `file_size` + `mtime_unix` |
| Cache coverage | `full_analysis` satisfies partial kinds |
