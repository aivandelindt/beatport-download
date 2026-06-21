---
name: beatport-catalog-search
description: >
  Extend BeatportDL-UI catalog search: Beatport v4 combined and typed search,
  five result categories, genre browse, artist-name track enrichment, collapsible
  sections, release nested tracks, card layouts, Settings search limits, and
  explicit Search-button UX in the embedded vanilla JS frontend. Use when
  modifying search backend/handlers, /api/search or /api/genres, config limits,
  or the Search view.
license: MIT
metadata:
  author: BeatportDL-UI
  version: "1.4.0"
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

Authenticated catalog search in BeatportDL-UI: Go Beatport client,
`GET /api/search`, `GET /api/genres`, and a Search view (default page) in embedded `web/`.

## Inputs

- `$feature_scope` (optional): `full` (default), `backend`, or `frontend` — limit work to one layer

## Goal

Catalog search is shipped end-to-end:

- `GET /api/search` — combined search on **All** tab (one fast API call); typed search per category tab; genre browse; optional artist top tracks; per-section limits from config
- `GET /api/genres` — Beatport genre picklist for the UI
- Search view: top nav, toolbar (tabs + filters), collapsible sections, sortable track table, card layouts, release expand/collapse, download actions
- Downloads reuse `POST /api/download` with Beatport track/artist/release/chart URLs
- `go build ./...` passes

## Prerequisites

- Go 1.22 monolith, embedded `web/` (vanilla HTML/CSS/JS, no build step)
- OAuth in Settings (`~/.config/beatportdl-ui/`)
- Beatport API base: `https://api.beatport.com/v4`
- Load `golang-context` skill for Go HTTP/API work

## Beatport API Reference

| Endpoint | Purpose |
|----------|---------|
| `GET /catalog/search/?q={query}&page=1&per_page=50&order_by=-publish_date` | Combined search (no `type`) |
| `GET /catalog/search/?q={query}&type=tracks&page=1&per_page=50&order_by=-publish_date` | Typed track search |
| `GET /catalog/search/?q={query}&type=artists&page=1&per_page=50` | Typed artist search |
| `GET /catalog/search/?q={query}&type=releases&order_by=-publish_date` | Typed release search |
| `GET /catalog/search/?q={query}&type=labels` | Typed label search |
| `GET /catalog/search/?q={query}&type=charts` | Typed chart search |
| `GET /catalog/search/?q={query}&type=tracks&genre_id={id}` | Search within genre |
| `GET /catalog/tracks/?genre_id={id}&order_by=-publish_date` | Genre-only browse (no `q`) |
| `GET /catalog/genres/?page=1&per_page=100` | Genre list |
| `GET /catalog/artists/{id}/tracks/?per_page=N&order_by=-publish_date` | Artist catalog tracks |
| `GET /catalog/releases/{id}/tracks/` | Release track list (multi-track releases) |

**Rules:**

- Authenticate before catalog calls (`client.Authenticate()`)
- Propagate `r.Context()` into client methods — never `context.Background()` mid-request
- Typed search JSON uses category keys (`tracks`, `artists`, …) — **not** `results` (decode via `decodeTypedSearchPage`)
- Apply `order_by=-publish_date` for **track** and **release** typed search; omit on artist search (returns `{}`)
- Outbound Beatport calls logged via `logging.BeatportAPI` in `doRequest` (`internal/beatport/client.go`)
- API `per_page` max is 100; handler clamps UI to 50–200 in steps of 50 and paginates internally

## HTTP API (app)

### `GET /api/genres`

Returns `[{ id, name, slug }]`. Requires credentials (same as search).

### `GET /api/search`

| Param | Values | Notes |
|-------|--------|-------|
| `q` | string | Min 2 chars when used; optional if `genre_id` set |
| `genre_id` | int | Genre filter or genre-only browse |
| `type` | `all` \| `tracks` \| `artists` \| `releases` \| `labels` \| `charts` | Default `all` |
| `page` | int | Default 1 |
| `per_page` | 50–200 | Rounded down to nearest 50; controls **Tracks** fetch size |
| `include_artists` | `1` / `true` | On tracks tab: also return artists |
| `top_tracks` | `1` / `true` | Attach up to 10 tracks per artist |

**Validation:** `q` or `genre_id` required. Genre-only browse returns tracks only (empty artists/releases/labels/charts).

**Response:** `{ query, type, artists?, releases?, tracks?, labels?, charts? }` — each section is `{ count, page, items }`

**Per-section limits:** After building results, `applySearchLimits` trims Artists, Releases, Labels, Charts to config values (`search_limit_*` in `config.yml`, default 10 each). Tracks use `per_page` only.

### All-tab fast path

`type=all` with a query uses a **single** `SearchCombined` call and maps all five categories in one round trip. Use the **Tracks** tab for deep track enrichment (`collectSearchTracks`).

### Track search merge (Tracks tab)

When `q` is set and `type=tracks`, `collectSearchTracks` merges four sources (deduped, relevance-ranked, capped at `per_page`):

**`SearchTrackItem` fields:** `id, title, artists, bpm, genre, label, length, key, camelot, released, image_uri, url`

1. **Combined search** — `SearchCombined` (`/catalog/search/?q=...` without `type`)
2. **Typed track search** — `searchTracksPaginated` (`type=tracks`)
3. **Artist-catalog title match** — artists from combined results → `ListArtistTracks` → filter where title matches query
4. **Artist-name match** — artists whose name contains `q` → their catalog tracks

`rankTracksByQuery` sorts by title/artist relevance after merge.

### Release enrichment

`releasePageFromReleases` sorts releases by `new_release_date` descending, then for `track_count > 1` fetches nested tracks via `ListReleaseTracks` and attaches them to `SearchReleaseItem.tracks`.

**`SearchReleaseItem` fields:** `id, title, artists, label, track_count, released, image_uri, url, tracks[]`

**`SearchChartItem` fields:** include `image_uri` (from curator `person.image`), `curator`, `name`, `url`

## Config (Settings)

Search result caps (Settings → Search results):

| Field | Default | Applies to |
|-------|---------|------------|
| `search_limit_artists` | 10 | Artists section |
| `search_limit_releases` | 10 | Releases section |
| `search_limit_labels` | 10 | Labels section |
| `search_limit_charts` | 10 | Charts section |

Stored in `~/.config/beatportdl-ui/config.yml`. Read in `handleSearch` via `s.searchLimits()` → `applySearchLimits`.

## Frontend (Search view)

**Default page:** `#view-search` is active on load.

**Navigation:** horizontal `.topbar` with Search | Download | Queue | Fix Tags | Settings (not sidebar).

**Layout** (`web/index.html`):

- `.search-input-row`: `#search-input` + `#btn-search` (explicit trigger)
- `.search-toolbar`: tabs on left, options on right (same row)
  - Tabs: All | Artists | Releases | Tracks | Labels | Charts
  - Options: genre (`#search-genre`), max results 50–200 (`#search-per-page`), Include artists, Artist top 10
- `#search-status-bar`: status + Expand all / Collapse all buttons

**Search trigger** (`web/js/app.js`):

- `startSearch()` on `#btn-search` click or Enter
- **No input debounce** — typing alone does not search
- Requires 2+ characters **or** selected genre; warn toast otherwise
- `syncSearchControlsFromUI()` reads genre + max results from DOM before each search
- `getSearchQuery()` reads `#search-input` (not stale `state.searchQuery` alone)
- Tab click, genre change, max results change, toggles → `triggerSearchIfReady()` or direct `runSearch()` when criteria met
- `AbortController` cancels in-flight requests

**Collapsible sections:**

- `COLLAPSIBLE_SEARCH_SECTIONS`: `artists`, `releases`, `tracks`, `labels`, `charts`
- `state.searchSectionCollapsed` — chevron toggle on section headers via `bindSearchSectionToggles`
- **Expand all** — expands every section + sets `state.searchReleaseExpanded[id]=true` for multi-track releases
- **Collapse all** — collapses every section + clears release expansion

**Track results table:**

- Columns: Cover, Track, Artist, Label, Genre, BPM, Key, Released, Time, Actions
- Client-side sort via `state.searchTrackSort` (default: Released desc)
- Key column: Camelot as **colored text** (`.camelot-code` + `camelotColorStyle()`), musical key below — no badge/pill

**Result sections** (All tab order): Artists → Releases → Tracks → Labels → Charts

**Artist results:** horizontal `.artist-card-row` cards; optional nested top-10 track table when `top_tracks` enabled

**Release results:** `.search-list` rows with fixed grid (chevron | cover | info | actions)

- Multi-track releases: click row toggles `.release-nested-tracks` (sortable track table)
- Chevron column always reserved (`.is-placeholder` when single-track) for vertical alignment
- Sorted newest-first (`sortReleasesByDate` client + `sortReleasesByDateDesc` server)

**Label / Chart results:** horizontal `.artist-card-row` with `.chart-card` layout (cover + name + actions)

**Layout:** `.main` and `.view` are full width (no max-width cap).

**Downloads:** `btn-search-download` → `POST /api/download` → toast + Queue view

## Steps (extend or fix)

### 1. Read current implementation

| Layer | Path |
|-------|------|
| Client | `internal/beatport/client.go` — `SearchCombined`, `SearchTyped`, `ListReleaseTracks`, `ListTracksByGenre`, `ListArtistTracks`, `GetGenres`, `GetArtistTopTracks` |
| Types | `internal/beatport/types.go` — `Key.CamelotCode()`, `SearchResults` |
| Handlers | `internal/server/handlers.go` — `handleSearch`, `fillAllSearchResults`, `collectSearchTracks`, `releasePageFromReleases`, `applySearchLimits` |
| Config | `internal/config/config.go` — `SearchLimit*` fields |
| Routes | `internal/server/server.go` |
| Logging | `internal/logging/logging.go` — `BeatportAPI` |
| UI | `web/index.html`, `web/js/app.js`, `web/css/style.css` |

### 2. Backend changes

When adding search params or fields:

1. Extend `SearchTyped` path builder in `client.go` (genre_id, order_by rules)
2. Update `handleSearch` query parsing and DTO mapping
3. Apply `applySearchLimits` after building payload
4. Use paginated helpers for multi-page fetches up to `per_page`
5. For artist-name discovery, extend `tracksFromMatchingArtists` — do not duplicate merge logic in the handler body
6. Log with `slog` in handlers; Beatport outbound via `logging.BeatportAPI`

### 3. Frontend changes

1. Add controls in `.search-toolbar` matching existing `.search-option` / `.pill` patterns
2. Extend `state`; wire `syncSearchControlsFromUI`, `getSearchQuery`, `runSearch()`
3. Section UI: `searchSectionWrap`, `searchSectionHeading`, collapsible toggles
4. Card helpers: `searchArtistCardHTML`, `searchChartCardHTML`, `searchLabelCardHTML`
5. Release helpers: `searchReleaseHTML`, `bindReleaseRows`, `state.searchReleaseExpanded`
6. Update `searchTrackTableHTML` / `keyCellHTML` for display changes
7. Reuse `escHtml()` for all user/API strings

### 4. Verify

```bash
go build ./...
```

Manual (credentials required):

1. Settings → Test Connection; set search limits if testing caps
2. Search by artist name fragment — track results include that artist's catalog tracks
3. Search: keyword, genre-only browse, each tab (tab click re-searches with genre + max results)
4. Collapse/expand sections; Expand all / Collapse all (including release nested tracks)
5. Click multi-track release row to toggle nested tracks
6. Sort track columns; confirm Camelot text colors
7. Download from a result → job in Queue

## Trigger Phrases

- "add search to BeatportDL"
- "catalog search API"
- "search by artist name"
- "search view / genre picklist"
- "camelot key display"
- "search button / debounce"
- "collapsible sections / expand all"
- "release nested tracks"
- "search limits settings"
- "wire up Client.Search"

## Edge Cases

| Issue | Handling |
|-------|----------|
| Empty `q` and no `genre_id` | 400 backend; warn toast frontend |
| `q` &lt; 2 chars without genre | Do not search; show hint |
| No credentials | 400 → Settings |
| Typed response shape | Decode category keys, not `results` |
| Artist `order_by=-publish_date` | Omit on artist **search** only; OK on artist **tracks** list |
| `top-10-tracks` endpoint | 404; use `/catalog/artists/{id}/tracks/` |
| Genre-only + non-tracks tabs | Empty sections (no browse-without-query for artists/releases/labels/charts) |
| Artist enrichment fails | Log warning; return direct track search results |
| Release tracks fetch fails | Log warning; release shown without nested tracks |
| Request cancelled | `ctx.Done()` / AbortController |
| Tab click without prior search | Runs if input has 2+ chars or genre selected |

## Optional Extensions

- Server-side sort or pagination UI ("Load more")
- Beatsource (platform-aware API base)
- Preview playback via `GetTrackStream`
- Per-section limits in UI (currently Settings only)

## Key Files

| Layer | Path |
|-------|------|
| Beatport client | `internal/beatport/client.go` |
| Types / Camelot | `internal/beatport/types.go` |
| Handlers | `internal/server/handlers.go` |
| Config | `internal/config/config.go` |
| Routes | `internal/server/server.go` |
| Logging | `internal/logging/logging.go` |
| UI | `web/index.html`, `web/js/app.js`, `web/css/style.css` |
