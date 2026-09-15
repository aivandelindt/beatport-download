# Analysis Result Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist audio analysis results in SQLite (default) or Postgres via GORM, reuse fresh cache on analyze, and expose a searchable Library panel in the Audio tab.

**Architecture:** New `internal/audio/store` package opens GORM with `modernc.org/sqlite` or Postgres, AutoMigrates `analysis_results`, and exposes GetFresh/Upsert/List/GetByID/Delete. Server holds optional `*store.Store`; analyze jobs check cache then upsert; Library HTTP + Audio tab UI browse the table.

**Tech Stack:** Go 1.22, GORM, modernc.org/sqlite, gorm.io/driver/postgres, existing `audio.Analysis` types, vanilla web UI.

**Spec:** [`docs/superpowers/specs/2026-09-11-analysis-store-design.md`](../specs/2026-09-11-analysis-store-design.md)

## Global Constraints

- Keep `CGO_ENABLED=0` builds working (use pure-Go SQLite driver).
- Propagate `context.Context` as first arg through store and handlers (`r.Context()` / `WithoutCancel` for background jobs).
- Do not crash the app if DB open fails — degrade to no-cache analyze.
- Verify with `go test ./internal/audio/... -count=1` and `go build ./...`.
- Do **not** create git commits unless the user explicitly asks (overrides frequent-commit steps below — treat commit steps as optional).
- Escape all user/path strings with `escHtml()` in the UI.
- YAGNI: no sidecars, no stems/normalize in DB, no dual-write SQLite↔Postgres.

## File map

- Create: `internal/audio/store/model.go`
- Create: `internal/audio/store/store.go`
- Create: `internal/audio/store/open.go`
- Create: `internal/audio/store/store_test.go`
- Create: `internal/audio/cached.go` (optional helper) or fold into handlers
- Modify: `internal/config/config.go`
- Modify: `internal/server/handlers.go` (`Server` struct)
- Modify: `main.go` or `server.NewServer` — open store
- Modify: `internal/server/server.go` — library routes
- Modify: `internal/server/audio_handlers.go` — cache + library handlers
- Modify: `web/index.html`, `web/js/app.js`, `web/css/style.css`
- Modify: `docs/audio-tools.md`, `README.md`, `.claude/skills/audio-tools/SKILL.md`
- Modify: `go.mod` / `go.sum` via `go get`

---

### Task 1: Store package + SQLite tests (TDD)

**Files:**
- Create: `internal/audio/store/model.go`
- Create: `internal/audio/store/open.go`
- Create: `internal/audio/store/store.go`
- Create: `internal/audio/store/store_test.go`

**Interfaces:**
- Consumes: `audio.Analysis` from `internal/audio`
- Produces: `store.Open`, `Store` methods per spec

- [ ] **Step 1: Add dependencies**

```bash
go get gorm.io/gorm@v1.25.12
go get gorm.io/driver/sqlite@v1.5.7
go get github.com/glebarez/sqlite@v1.11.0
go get gorm.io/driver/postgres@v1.5.11
```

Prefer **`github.com/glebarez/sqlite`** (wraps modernc, GORM-friendly, no CGO) over `gorm.io/driver/sqlite`+CGO. Import as:

```go
import (
  sqlite "github.com/glebarez/sqlite"
  "gorm.io/driver/postgres"
  "gorm.io/gorm"
)
```

- [ ] **Step 2: Write failing tests** in `store_test.go`

```go
package store_test

func TestUpsertAndGetFresh_Hit(t *testing.T) {
    s := openTemp(t)
    path := "/music/a.flac"
    a := audio.Analysis{Kind: audio.KindFullAnalysis, Path: path, Source: "mcp",
        HarmonicAnalysis: &audio.HarmonicAnalysis{Key: "A", Mode: "minor"},
        RhythmAnalysis:   &audio.RhythmAnalysis{TempoBPM: 128},
        SpectralFeatures: &audio.SpectralFeatures{LUFSIntegrated: -9.5},
        AudioInfo:        &audio.AudioInfo{DurationSec: 180},
    }
    rec := store.RecordFromAnalysis(path, 1000, 1700000000, a)
    if err := s.Upsert(context.Background(), rec); err != nil {
        t.Fatal(err)
    }
    got, ok, err := s.GetFresh(context.Background(), path, audio.KindFullAnalysis, 1000, 1700000000)
    if err != nil || !ok {
        t.Fatalf("hit=%v err=%v", ok, err)
    }
    if got.HarmonicAnalysis == nil || got.HarmonicAnalysis.Key != "A" {
        t.Fatalf("%+v", got)
    }
}

func TestGetFresh_MissOnMtimeChange(t *testing.T) { /* upsert then GetFresh with mtime+1 → ok=false */ }
func TestGetFresh_FullSatisfiesPartial(t *testing.T) { /* store full_analysis; GetFresh kind=rhythm → hit */ }
func TestList_FilterByPathSubstring(t *testing.T) { /* two paths; Q=foo */ }
func TestDelete(t *testing.T) { /* upsert, delete, GetByID error */ }
```

Helper `openTemp` uses `Open("sqlite", filepath.Join(t.TempDir(), "t.db"))`.

- [ ] **Step 3: Run tests — expect FAIL**

```bash
go test ./internal/audio/store/ -count=1
```

Expected: package or symbols undefined.

- [ ] **Step 4: Implement model + Open + Store**

`model.go`:

```go
package store

type AnalysisResult struct {
    ID             uint `gorm:"primaryKey"`
    Path           string `gorm:"uniqueIndex:idx_path_kind;size:1024;not null"`
    Kind           string `gorm:"uniqueIndex:idx_path_kind;size:64;not null"`
    FileSize       int64
    MtimeUnix      int64
    Key            string  `gorm:"index"`
    Mode           string
    TempoBPM       float64 `gorm:"index"`
    LUFSIntegrated float64 `gorm:"index"`
    DurationSec    float64
    Source         string
    PayloadJSON    string `gorm:"type:text"`
    AnalyzedAt     time.Time `gorm:"index"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

func (AnalysisResult) TableName() string { return "analysis_results" }
```

`open.go`:

```go
func Open(driver, dsn string) (*Store, error) {
    var dialector gorm.Dialector
    switch strings.ToLower(driver) {
    case "", "sqlite":
        if dsn == "" {
            return nil, fmt.Errorf("sqlite dsn required")
        }
        dialector = sqlite.Open(dsn)
    case "postgres", "postgresql":
        if dsn == "" {
            return nil, fmt.Errorf("postgres dsn required")
        }
        dialector = postgres.Open(dsn)
    default:
        return nil, fmt.Errorf("unknown analysis_db_driver: %s", driver)
    }
    db, err := gorm.Open(dialector, &gorm.Config{})
    if err != nil {
        return nil, err
    }
    if err := db.AutoMigrate(&AnalysisResult{}); err != nil {
        return nil, err
    }
    return &Store{db: db}, nil
}

func DefaultSQLiteDSN(configDir string) string {
    return filepath.Join(configDir, "analysis.db")
}
```

`store.go`: implement `GetFresh`, `Upsert` (Clauses OnConflict on path+kind), `List`, `GetByID`, `Delete`, `RecordFromAnalysis`, `Close`. For `GetFresh` when requested kind ≠ stored: if row kind is `full_analysis`, unmarshal payload and optionally trim via existing `trim` logic — either export a small `audio.TrimAnalysis(full, kind)` or duplicate minimal field selection in store.

OnConflict upsert example:

```go
err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
    Columns:   []clause.Column{{Name: "path"}, {Name: "kind"}},
    UpdateAll: true,
}).Create(&row).Error
```

- [ ] **Step 5: Tests pass**

```bash
go test ./internal/audio/store/ -count=1
```

Expected: PASS

---

### Task 2: Config + server wire-up

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/server/handlers.go` (`Server` struct + `NewServer`)
- Modify: `main.go` (or open store inside `NewServer`)
- Modify: Settings form in `web/index.html` + save already maps fields by name

**Interfaces:**
- Consumes: `store.Open`, `store.DefaultSQLiteDSN`
- Produces: `Server.analysisStore *store.Store` (may be nil)

- [ ] **Step 1: Config fields**

```go
AnalysisDBDriver string `yaml:"analysis_db_driver" json:"analysis_db_driver"`
AnalysisDBDSN    string `yaml:"analysis_db_dsn"    json:"analysis_db_dsn"`
```

In `applyDefaults`: if `AnalysisDBDriver == ""` → `"sqlite"`.

Helper on config or server:

```go
func analysisDSN(cfg *config.Config) (driver, dsn string) {
    driver = cfg.AnalysisDBDriver
    if driver == "" {
        driver = "sqlite"
    }
    dsn = cfg.AnalysisDBDSN
    if driver == "sqlite" && dsn == "" {
        dsn = store.DefaultSQLiteDSN(filepath.Dir(config.ConfigPath()))
    }
    return driver, dsn
}
```

- [ ] **Step 2: Server field + open**

```go
type Server struct {
    // ...
    analysisStore *store.Store
}
```

In `NewServer` or `main` after `NewServer`:

```go
driver, dsn := analysisDSN(cfg)
st, err := store.Open(driver, dsn)
if err != nil {
    slog.Error("analysis store unavailable", "err", err)
} else {
    srv.analysisStore = st
}
```

Defer `st.Close()` on shutdown if process has clean shutdown hooks; otherwise process exit is fine for SQLite.

- [ ] **Step 3: Settings UI fields** under Audio tools:

```html
<select name="analysis_db_driver">
  <option value="sqlite">SQLite</option>
  <option value="postgres">Postgres</option>
</select>
<input name="analysis_db_dsn" placeholder="empty = default analysis.db / postgres URL" />
```

- [ ] **Step 4: Extend `GET /api/audio/tools`**

Add `"analysis_store": s.analysisStore != nil`.

- [ ] **Step 5: Build**

```bash
go build ./...
```

Expected: success

---

### Task 3: Cache-aware analyze + force flag

**Files:**
- Modify: `internal/server/audio_handlers.go`

**Interfaces:**
- Consumes: `analysisStore.GetFresh` / `Upsert`
- Produces: analyze body `{force: bool}`; cached tracks marked in progress message

- [ ] **Step 1: Extend request + job runner signature**

```go
var req struct {
    Path    string `json:"path"`
    Kind    string `json:"kind"`
    Backend string `json:"backend"`
    Force   bool   `json:"force"`
}
// pass force into runAnalyzeJob
```

- [ ] **Step 2: Per-file cache logic** (inside loop before `Analyze`)

```go
abs, _ := filepath.Abs(file)
abs = filepath.Clean(abs)
fi, err := os.Stat(abs)
// ...
if !force && s.analysisStore != nil {
    if cached, ok, _ := s.analysisStore.GetFresh(fileCtx, abs, kind, fi.Size(), fi.ModTime().Unix()); ok {
        cached.Source = "cache"
        job.Analysis = append(job.Analysis, cached)
        job.Completed++
        // progress message "cached"
        continue
    }
}
// existing Analyze...
if err == nil && s.analysisStore != nil {
    rec := store.RecordFromAnalysis(abs, fi.Size(), fi.ModTime().Unix(), result)
    if upErr := s.analysisStore.Upsert(fileCtx, rec); upErr != nil {
        slog.Warn("analysis upsert failed", "path", abs, "err", upErr)
    }
}
```

- [ ] **Step 3: Manual smoke** — analyze same file twice; second should be fast / source cache in payload when store works.

```bash
go build ./...
go test ./internal/audio/... -count=1
```

---

### Task 4: Library HTTP API

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/audio_handlers.go`

**Interfaces:**
- Produces: list/get/delete handlers

- [ ] **Step 1: Register routes**

```go
mux.HandleFunc("GET /api/audio/library", s.handleAudioLibraryList)
mux.HandleFunc("GET /api/audio/library/{id}", s.handleAudioLibraryGet)
mux.HandleFunc("DELETE /api/audio/library/{id}", s.handleAudioLibraryDelete)
```

- [ ] **Step 2: Implement handlers**

```go
func (s *Server) handleAudioLibraryList(w http.ResponseWriter, r *http.Request) {
    if s.analysisStore == nil {
        respondErr(w, 503, "analysis store unavailable")
        return
    }
    q := r.URL.Query()
    f := store.ListFilter{
        Q: q.Get("q"), Key: q.Get("key"),
        Limit: atoiDefault(q.Get("limit"), 50),
        Offset: atoiDefault(q.Get("offset"), 0),
    }
    // parse bpm_min / bpm_max optional floats into f
    items, total, err := s.analysisStore.List(r.Context(), f)
    // respond {items, total}
}
```

Get/Delete: parse `{id}` with `strconv.ParseUint`, 404 on not found.

List JSON item shape:

```json
{"id":1,"path":"...","kind":"full_analysis","key":"A","mode":"minor","tempo_bpm":128,"lufs_integrated":-9.5,"duration_sec":180,"analyzed_at":"...","source":"mcp"}
```

Get includes `analysis` object (unmarshaled payload).

- [ ] **Step 3: Build**

```bash
go build ./...
```

---

### Task 5: Library UI panel

**Files:**
- Modify: `web/index.html` — fourth Audio tab `Library`
- Modify: `web/css/style.css` — table styles (reuse search/job patterns)
- Modify: `web/js/app.js` — `loadLibrary`, filters, detail, delete, re-analyze

- [ ] **Step 1: HTML** — panel with filters + `<table id="library-table">` + detail `#library-detail`

- [ ] **Step 2: JS**

```js
async function loadLibrary() {
  const q = $('#library-q')?.value.trim() || '';
  const params = new URLSearchParams({ q, limit: '100' });
  const res = await fetch('/api/audio/library?' + params);
  // render rows; on 503 show banner
}
// delete: DELETE /api/audio/library/${id}
// reanalyze: POST /api/audio/analyze { path, kind:'full_analysis', force:true }
```

Wire Audio tools banner to `analysis_store` from `/api/audio/tools`.

- [ ] **Step 3: Browser check** — open Audio → Library; empty state; after analyze, row appears; detail JSON; force re-analyze.

---

### Task 6: Docs

**Files:**
- Modify: `docs/audio-tools.md`
- Modify: `README.md` API table
- Modify: `.claude/skills/audio-tools/SKILL.md`
- Modify: `docs/superpowers/specs/2026-09-11-analysis-store-design.md` — set Status: Approved

- [ ] **Step 1: Document** driver/DSN, cache rules, library endpoints, Library panel.

- [ ] **Step 2: Final verify**

```bash
go test ./internal/audio/... -count=1
go build ./...
```

Expected: PASS / success

---

## Spec coverage

| Spec item | Task |
|-----------|------|
| GORM + SQLite/Postgres Open | 1 |
| Schema + unique (path,kind) | 1 |
| GetFresh / Upsert / List / Delete | 1 |
| Config + Settings | 2 |
| Degraded mode if DB fails | 2 |
| Cache in analyze + force | 3 |
| Library HTTP | 4 |
| Library UI | 5 |
| Docs | 6 |

## Placeholder scan

No TBD left. `audio.TrimAnalysis` — implement in Task 1 if `GetFresh` needs partial trim from `full_analysis` (copy fields into new `Analysis` with requested `Kind`).

## Type consistency

`store.Record`, `ListFilter`, `ListItem`, config keys `analysis_db_driver` / `analysis_db_dsn`, API paths `/api/audio/library` match the approved spec.
