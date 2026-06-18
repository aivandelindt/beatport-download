---
name: beatport-catalog-search
description: >
  Extend BeatportDL-UI catalog search: Beatport v4 typed search, genre browse,
  genres API, sortable track table with Camelot keys, and explicit Search-button
  UX in the embedded vanilla JS frontend. Use when modifying search backend/handlers,
  /api/search or /api/genres, or the default Search view.
license: MIT
metadata:
  author: BeatportDL-UI
  version: "1.1.0"
allowed-tools:
  - Read
  - Write
  - StrReplace
  - Grep
  - Glob
  - SemanticSearch
  - Shell
  - WebSearch
  - WebFetch
---

# Beatport Catalog Search

Authenticated track and artist search in BeatportDL-UI: Go Beatport client,
`GET /api/search`, `GET /api/genres`, and a Search view (default page) in embedded `web/`.

## Inputs

- `$feature_scope` (optional): `full` (default), `backend`, or `frontend` — limit work to one layer

## Goal

Catalog search is shipped end-to-end:

- `GET /api/search` — typed search, genre filter, genre-only browse, optional artist top tracks
- `GET /api/genres` — Beatport genre picklist for the UI
- Search view: explicit Search button, tabs, options, sortable track table, artist cards, download actions
- Downloads reuse `POST /api/download` with Beatport track/artist URLs
- `go build ./...` passes

## Prerequisites

- Go 1.22 monolith, embedded `web/` (vanilla HTML/CSS/JS, no build step)
- OAuth in Settings (`~/.config/beatportdl-ui/`)
- Beatport API base: `https://api.beatport.com/v4`
- Load `golang-context` skill for Go HTTP/API work

## Beatport API Reference

| Endpoint | Purpose |
|----------|---------|
| `GET /catalog/search/?q={query}&type=tracks&page=1&per_page=50` | Typed track search |
| `GET /catalog/search/?q={query}&type=artists&page=1&per_page=50` | Typed artist search |
| `GET /catalog/search/?q={query}&type=tracks&genre_id={id}` | Search within genre |
| `GET /catalog/tracks/?genre_id={id}&order_by=-publish_date` | Genre-only browse (no `q`) |
| `GET /catalog/genres/?page=1&per_page=100` | Genre list |
| `GET /catalog/artists/{id}/tracks/?per_page=10` | Artist top tracks (use this, not `top-10-tracks`) |

**Rules:**

- Authenticate before catalog calls (`client.Authenticate()`)
- Propagate `r.Context()` into client methods — never `context.Background()` mid-request
- Typed search JSON uses `tracks` or `artists` keys — **not** `results` (decode via `decodeTypedSearchPage`)
- Apply `order_by=-publish_date` for **track** search only; artist search with `order_by` returns `{}`
- API `per_page` max is 100; handler clamps UI to 50–200 in steps of 50 and paginates internally

## HTTP API (app)

### `GET /api/genres`

Returns `[{ id, name, slug }]`. Requires credentials (same as search).

### `GET /api/search`

| Param | Values | Notes |
|-------|--------|-------|
| `q` | string | Min 2 chars when used; optional if `genre_id` set |
| `genre_id` | int | Genre filter or genre-only browse |
| `type` | `all` \| `tracks` \| `artists` | Default `all` |
| `page` | int | Default 1 |
| `per_page` | 50–200 | Rounded down to nearest 50 |
| `include_artists` | `1` / `true` | On tracks tab: also return artists |
| `top_tracks` | `1` / `true` | Attach up to 10 tracks per artist |

**Validation:** `q` or `genre_id` required. Genre-only browse returns tracks only (empty artists).

**Response:** `{ query, type, tracks?: { count, page, items }, artists?: { count, page, items } }`

**`SearchTrackItem` fields:** `id, title, artists, bpm, genre, label, length, key, camelot, released, image_uri, url`

- `camelot` from `Key.CamelotCode()` in `types.go` (e.g. `"8A"`)
- `key` is the musical key name string

## Frontend (Search view)

**Default page:** `#view-search` is active on load; sidebar Search nav is first.

**Layout** (`web/index.html`):

- `.search-input-row`: `#search-input` + `#btn-search` (explicit trigger)
- Tabs: All / Tracks / Artists
- Options: genre (`#search-genre`), max results 50–200, Include artists, Artist top 10

**Search trigger** (`web/js/app.js`):

- `startSearch()` on `#btn-search` click or Enter
- **No input debounce** — typing alone does not search
- Requires 2+ characters **or** selected genre; warn toast otherwise
- `triggerSearchIfReady()` re-runs when filters/tabs change **if** a search is already active
- `AbortController` cancels in-flight requests

**Track results table:**

- Columns: Cover, Track, Artist, Label, Genre, BPM, Key, Released, Time, Actions
- Cover thumbnail in its own column; label and duration in dedicated columns
- Client-side sort via `state.searchTrackSort` (default: Released desc)
- Key column: Camelot as **colored text** (`.camelot-code` + `camelotColorStyle()`), musical key name below in `.musical-key-name` — no badge/pill background

**Artist results:**

- Cards with optional nested top-10 track table when `top_tracks` enabled

**Downloads:** `btn-search-download` → `POST /api/download` → toast + Queue view

## Steps (extend or fix)

### 1. Read current implementation

| Layer | Path |
|-------|------|
| Client | `internal/beatport/client.go` — `SearchTyped`, `ListTracksByGenre`, `GetGenres`, `GetArtistTopTracks` |
| Types | `internal/beatport/types.go` — `Key.CamelotCode()` |
| Handlers | `internal/server/handlers.go` — `handleSearch`, `handleGenres`, `trackToSearchItem` |
| Routes | `internal/server/server.go` |
| Logging | `internal/logging/logging.go`, `main.go` middleware |
| UI | `web/index.html`, `web/js/app.js`, `web/css/style.css` |

### 2. Backend changes

When adding search params or fields:

1. Extend `SearchTyped` path builder in `client.go` (genre_id, order_by rules)
2. Update `handleSearch` query parsing and DTO mapping
3. Use `searchTracksPaginated` / `listTracksByGenrePaginated` for multi-page fetches up to `per_page`
4. Log with `slog` in handlers; HTTP middleware logs requests (skips `/api/ws`)

### 3. Frontend changes

1. Add controls in `#view-search` matching existing `.search-option` / `.pill` patterns
2. Extend `state` and wire `startSearch()` / `runSearch()` query string
3. Update `searchTrackTableHTML` / `keyCellHTML` for display changes
4. Reuse `escHtml()` for all user/API strings

### 4. Verify

```bash
go build ./...
```

Manual (credentials required):

1. Settings → Test Connection
2. Search: keyword, genre-only browse, each tab
3. Sort track columns; confirm Camelot text colors
4. Download from a result → job in Queue

## Trigger Phrases

- "add search to BeatportDL"
- "catalog search API"
- "search view / genre picklist"
- "camelot key display"
- "search button / debounce"
- "wire up Client.Search"

## Edge Cases

| Issue | Handling |
|-------|----------|
| Empty `q` and no `genre_id` | 400 backend; warn toast frontend |
| `q` &lt; 2 chars without genre | Do not search; show hint |
| No credentials | 400 → Settings |
| Typed response shape | Decode `tracks`/`artists` keys, not `results` |
| Artist `order_by=-publish_date` | Omit — API returns empty |
| `top-10-tracks` endpoint | 404; use `/catalog/artists/{id}/tracks/` |
| Genre-only + artists tab | Empty artists (no browse-without-query for artists) |
| Request cancelled | `ctx.Done()` / AbortController |

## Optional Extensions

- Server-side sort or pagination UI ("Load more")
- Beatsource (platform-aware API base)
- Label/release search tabs
- Preview playback via `GetTrackStream`

## Key Files

| Layer | Path |
|-------|------|
| Beatport client | `internal/beatport/client.go` |
| Types / Camelot | `internal/beatport/types.go` |
| Handlers | `internal/server/handlers.go` |
| Routes | `internal/server/server.go` |
| Logging | `internal/logging/logging.go` |
| UI | `web/index.html`, `web/js/app.js`, `web/css/style.css` |
