## Learned User Preferences

- Propagate `r.Context()` through Beatport client and HTTP handler methods; load the golang-context skill for Go API work.
- Capture repeatable feature workflows as project skills under `.claude/skills/`.
- Only create git commits when explicitly requested.

## Learned Workspace Facts

- Go 1.22 monolith with embedded `web/` (vanilla HTML/CSS/JS via `go:embed`); no frontend build step.
- Config at `~/.config/beatportdl-ui/config.yml`; OAuth credentials at `~/.config/beatportdl-ui/beatportdl-credentials.json`.
- Default HTTP server port is 8989 (falls back to 8990 when 8989 is busy).
- Makefile supports cross-compilation; no `*_test.go` files yet — verify with `go build ./...`.
- Beatport API base is `https://api.beatport.com/v4`; OAuth is required for catalog search and downloads.
- Outbound Beatport HTTP calls are logged via `logging.BeatportAPI` in `internal/beatport/client.go`.

## UI

- Top bar navigation (not sidebar): Search | Download | Queue | Fix Tags | Settings.
- Search is the default view. `.main` and views are full width.

## Catalog search

Full workflow: `.claude/skills/beatport-catalog-search/SKILL.md` (v1.4.0). Cursor rules: `.cursor/rules/` (`project-overview`, `go-backend`, `web-frontend`, `catalog-search`).

**App API:** `GET /api/search?q=&type=all|tracks|artists|releases|labels|charts&genre_id=&per_page=50-200&include_artists=&top_tracks=` and `GET /api/genres`.

- **All tab:** single `SearchCombined` call; five sections trimmed by Settings limits.
- **Tracks tab:** `collectSearchTracks` merges four sources; `per_page` controls track count only.
- **Settings limits** (default 10 each): `search_limit_artists`, `search_limit_releases`, `search_limit_labels`, `search_limit_charts`.
- Typed Beatport search JSON uses category keys (`tracks`, `artists`, …), not `results`.
- Use `order_by=-publish_date` for track, release, and chart search; omit on artist search (returns `{}`).
- Releases and charts sorted newest-first; multi-track releases include nested tracks from `ListReleaseTracks`.

**Search UX:**

- Explicit Search button or Enter; no input debounce.
- Requires 2+ characters or a selected genre.
- `.search-toolbar`: type tabs + genre / max results / toggles on one row.
- Tab click and filter changes call `syncSearchControlsFromUI()` then re-search when criteria are met.
- Collapsible sections: Artists, Releases, Tracks, Labels, Charts; Expand all / Collapse all also toggles release nested tracks.
- Artists, Labels, Charts: horizontal `.artist-card-row` cards; chart cards equal height, publish date as `YYYY-MM-DD` (`formatDateISO()`).
- Releases: list rows with reserved 18px chevron column and click-to-expand nested track table; row action buttons align right.
- Camelot keys: colored text (`.camelot-code`), not pill badges.
- Downloads from results use `POST /api/download`.
