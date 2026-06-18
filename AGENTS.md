## Learned User Preferences

- Propagate `r.Context()` through Beatport client and HTTP handler methods; load the golang-context skill for Go API work.
- Capture repeatable feature workflows as project skills under `.claude/skills/`.

## Learned Workspace Facts

- Go 1.22 monolith with embedded `web/` (vanilla HTML/CSS/JS via go:embed).
- Config at `~/.config/beatportdl-ui/config.yml`; OAuth credentials at `~/.config/beatportdl-ui/beatportdl-credentials.json`.
- Default HTTP server port is 8989 (falls back to 8990 when 8989 is busy).
- Makefile supports cross-compilation; no `*_test.go` files yet — verify with `go build ./...`.
- Beatport API base is `https://api.beatport.com/v4`; OAuth is required for catalog search and downloads.
- Catalog search: Beatport `GET /catalog/search/?q=...&type=tracks|artists&page=N&per_page=N`; app exposes `GET /api/search?q=...&type=all|tracks|artists&page=1&per_page=25`.
- Typed search responses use `tracks` or `artists` keys — not a `results` array.
- Do not pass `order_by=-publish_date` on artist search (API returns empty `{}`).
- Search is the default web view; queries run explicitly via `#btn-search` or Enter (no input debounce); requires 2+ characters or a selected genre; filter/tab changes re-run via `triggerSearchIfReady()` when a search is active.
- Track search results render Camelot keys as colored text (`.camelot-code`), not background pill badges.
- Downloads from search results reuse existing `POST /api/download`.
- Catalog search workflow is documented in `.claude/skills/beatport-catalog-search/SKILL.md`.
