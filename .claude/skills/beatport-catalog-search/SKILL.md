---
name: beatport-catalog-search
description: >
  Add track and artist search to BeatportDL-UI by wiring the Beatport v4
  /catalog/search/ API through a context-aware Go handler and embedded vanilla
  JS frontend. Use when extending BeatportDL-UI with catalog search, exposing
  Client.Search to HTTP, or building a Search sidebar view with download actions.
license: MIT
metadata:
  author: BeatportDL-UI
  version: "1.0.0"
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

Add authenticated track and artist search to BeatportDL-UI: extend the Go Beatport
client, expose `GET /api/search`, and build a Search view in the embedded web UI.

## Inputs

- `$feature_scope` (optional): `full` (default), `backend`, or `frontend` — limit work to one layer if the other already exists

## Goal

Ship catalog search end-to-end:

- `GET /api/search?q=...&type=all|tracks|artists` returns trimmed JSON
- Search sidebar with debounced input, tabs, result cards, and download buttons
- Downloads reuse existing `POST /api/download` with Beatport track/artist URLs
- `go build ./...` passes

## Prerequisites

- BeatportDL-UI stack: Go 1.22 monolith, embedded `web/` (vanilla HTML/CSS/JS)
- OAuth credentials configured in Settings (same auth as downloads)
- Beatport API base: `https://api.beatport.com/v4`
- Load `golang-context` skill when touching Go HTTP/API code

## Beatport API Reference

| Endpoint | Purpose |
|----------|---------|
| `GET /catalog/search/?q={query}&type=tracks&page=1&per_page=25` | Search tracks |
| `GET /catalog/search/?q={query}&type=artists&page=1&per_page=25` | Search artists |
| `GET /catalog/search/?q={query}&order_by=-publish_date` | Combined search (nested `{data:[]}` format) |

**Rules:**

- Always authenticate before catalog calls (`client.Authenticate()`)
- Propagate `r.Context()` into new Beatport client methods — never `context.Background()` mid-request
- Typed search returns paginated `results`; combined search may nest under `data`
- Search types: `tracks`, `artists`, `labels`, `releases`, `charts`

## Steps

### 1. Explore codebase and API integration

Read these files first:

- `internal/beatport/client.go` — auth, `apiGet`, existing `Search()`
- `internal/beatport/types.go` — `Track`, `Artist`, `Paginated`, `SearchResults`
- `internal/server/handlers.go` — handler patterns (`handleTestAuth`, `handleDownload`)
- `internal/server/server.go` — route registration
- `web/index.html`, `web/js/app.js`, `web/css/style.css` — UI patterns

Check whether `Client.Search()` exists but is unused (common in this repo). Note gaps:
no HTTP route, no frontend view, possible response-format mismatch with nested `data`.

Use web search or Beatport API docs if response shape is unclear.

**Success criteria**: You can describe current auth flow, unused search client code, and exact files to modify.

**Artifacts**: Mental map of backend vs frontend gaps.

---

### 2. Extend Go Beatport client (context-aware search)

In `internal/beatport/client.go`:

1. Add `apiGetContext(ctx, path, result)` using `http.NewRequestWithContext`
2. Delegate existing `apiGet` to `apiGetContext(context.Background(), ...)`
3. Add typed search methods:

```go
func (c *Client) SearchTracks(ctx context.Context, query string, page, perPage int) (*Paginated[Track], error)
func (c *Client) SearchArtists(ctx context.Context, query string, page, perPage int) (*Paginated[Artist], error)
```

4. Build path: `/catalog/search/?q=...&type=tracks|artists&page=N&per_page=N&order_by=-publish_date`
5. Handle both paginated (`results`) and nested (`data`) response shapes via flexible decode

In `internal/beatport/types.go`:

- Add `Image` to `Artist` if missing (search results include cover art)
- Add `SearchType` constants if helpful

**Rules:**

- `ctx` is first parameter, named `ctx context.Context`
- Do not store context in structs
- Clamp `per_page` (e.g. max 100 client-side, 50 in handler)

**Success criteria**: Client compiles; search methods accept context and return typed paginated results.

**Execution**: Direct

---

### 3. Add GET /api/search handler and route

In `internal/server/server.go`:

```go
mux.HandleFunc("GET /api/search", s.handleSearch)
```

In `internal/server/handlers.go`, implement `handleSearch`:

1. Parse query params: `q` (required), `type` (`all|tracks|artists`, default `all`), `page`, `per_page`
2. Validate credentials (same check as `handleDownload`)
3. `ctx := r.Context()` — propagate to client search calls
4. Call `SearchTracks` and/or `SearchArtists` based on `type`
5. Map to UI DTOs (`SearchTrackItem`, `SearchArtistItem`) with Beatport URLs:

```
https://www.beatport.com/track/{slug}/{id}
https://www.beatport.com/artist/{slug}/{id}
```

6. Return JSON `{ query, type, tracks?: { count, page, items }, artists?: { count, page, items } }`

**Success criteria**: Handler returns 400 without `q`, 401 without credentials, 200 with search results for valid requests.

**Artifacts**: `SearchResponsePayload` types, working `/api/search` endpoint.

---

### 4. Build Search frontend view

**HTML** (`web/index.html`):

- Add sidebar nav item `data-view="search"`
- Add `#view-search` section: search input, type tabs (All / Tracks / Artists), status line, results container

**JS** (`web/js/app.js`):

- State: `searchType`, `searchQuery`, `searchController` (AbortController)
- Debounced input (350ms, min 2 chars)
- `fetch('/api/search?q=...&type=...')` with abort on new query
- Render track rows: thumb, title, artists, BPM, genre, key, label
- Render artist rows: thumb, name
- Download button → `POST /api/download` with result URL → toast + switch to Queue view
- External link to Beatport

**CSS** (`web/css/style.css`):

- Match existing card/pill/btn patterns (`search-card`, `search-item`, `btn-search-download`)

**Rules:**

- Reuse existing `state.quality` for download requests
- Reuse `escHtml()` for XSS safety
- Do not add a frontend build step — vanilla JS only

**Success criteria**: Search view renders, debounced queries work, download queues a job.

**Execution**: Direct

---

### 5. Verify

```bash
go build ./...
```

Manually (requires credentials):

1. Settings → Test Connection
2. Search → query an artist or track
3. Click download on a result → job appears in Queue

**Success criteria**: Build passes; search and download flow work with valid Beatport credentials.

---

## Trigger Phrases

Invoke this skill when the user says:

- "add search to BeatportDL"
- "track and artist search"
- "expose catalog search API"
- "search view in the UI"
- "wire up Client.Search"

## Edge Cases

| Issue | Handling |
|-------|----------|
| Empty `q` or `< 2` chars | Return 400 (backend) or show hint (frontend) |
| No credentials | 400 with message pointing to Settings |
| Auth failure | 401 from handler |
| Request cancelled | Respect `ctx.Done()` / AbortController |
| Combined vs typed response shape | Flexible JSON decode in client |
| Artist download | Uses existing artist URL parsing in `runJob` — downloads full catalog |

## Optional Extensions

- Pagination ("Load more") using `page` param
- Beatsource support (requires platform-aware API base in client)
- Label/release search tabs
- Preview playback via `GetTrackStream`

## Key Files

| Layer | Path |
|-------|------|
| Beatport client | `internal/beatport/client.go` |
| Types | `internal/beatport/types.go` |
| Handler | `internal/server/handlers.go` |
| Routes | `internal/server/server.go` |
| UI | `web/index.html`, `web/js/app.js`, `web/css/style.css` |
