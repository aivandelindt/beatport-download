Beatport UI Summary

BeatportDL-UI Repository Summary
A compact Go 1.22 monolith with an embedded vanilla HTML/CSS/JS frontend. There is no separate frontend build step, no React/Vue, and no tests. The app is a local web server for downloading Beatport/Beatsource content via pasted URLs.

1. Project Structure
```
/Users/dvandelindt/Projects/BeatportDL-UI/
├── main.go                          # Entry: config load, server start, browser open
├── go.mod / go.sum
├── Makefile                         # Cross-compile (macOS/Windows/Linux), Docker
├── Dockerfile / docker-compose.yml  # Alpine + ffmpeg, port 8989
├── build-macos.sh
├── internal/
│   ├── config/config.go             # YAML config (~/.config/beatportdl-ui/config.yml)
│   ├── beatport/
│   │   ├── client.go                # Beatport API client + auth + downloads
│   │   ├── types.go                 # Domain types (Track, Artist, SearchResults, etc.)
│   │   ├── metadata.go              # ffmpeg metadata embed + FixMetadata
│   │   └── utils.go                 # Filename/path sanitization
│   └── server/
│       ├── server.go                # Route registration
│       ├── handlers.go              # HTTP handlers + job runner
│       └── ws.go                    # WebSocket hub + payload types
└── web/                             # Static UI (go:embed)
    ├── index.html
    ├── css/style.css
    └── js/app.js
```
Layer	Role
Go backend	HTTP API, download jobs, Beatport auth, file I/O, WebSocket progress
Frontend	Single-page app with 4 sidebar views, fetch + WebSocket
Config	~/.config/beatportdl-ui/config.yml (credentials, quality, output dir, port)
Credentials cache	~/.config/beatportdl-ui/beatportdl-credentials.json (OAuth tokens)
Dependencies (go.mod): gorilla/websocket, google/uuid, gopkg.in/yaml.v3. Runtime needs ffmpeg for metadata embedding.

1. Beatport API Integration
Base URLs & client
* Beatport API: https://api.beatport.com/v4 (beatportAPIBase)
* Beatsource API (constant only): https://api.beatsource.com/v4 — not wired into NewClient
* Client: /Users/dvandelindt/Projects/BeatportDL-UI/internal/beatport/client.go — Client, NewClient(username, password, configDir)
Authentication (OAuth2-style, 3-step)
Client.Authenticate() → cache check → refresh → fullAuth():
1. POST /auth/login/ — JSON username/password → session cookie
2. GET /auth/o/authorize/?client_id=...&response_type=code — redirect with code
3. POST /auth/o/token/ — exchange code for tokens
* Client ID: hardcoded beatportClientID in client.go
* Token storage: Credentials struct (AccessToken, RefreshToken, ExpiresAt, LoginID)
* Bearer auth: apiRequest() sets Authorization: Bearer <token>, auto-retry on 401
* Test endpoint: POST /api/auth/test → handleTestAuth creates client and calls Authenticate()
Catalog API methods (all via apiGet)
Method	Endpoint	Purpose
GetTrack(id)	GET /catalog/tracks/{id}/	Single track
GetTrackDownload(id, quality)	GET /catalog/tracks/{id}/download/?quality=	Download URL
GetTrackStream(id)	GET /catalog/tracks/{id}/stream/	Stream URL
GetRelease(id)	GET /catalog/releases/{id}/	Release metadata
GetReleaseTracks(id)	GET /catalog/releases/{id}/tracks/	Paginated tracks
GetPlaylist(id)	GET /catalog/playlists/{id}/	Playlist metadata
GetPlaylistTracks(id)	GET /catalog/playlists/{id}/tracks/	Wrapped track items
GetChart(id)	GET /catalog/charts/{id}/	Chart metadata
GetChartTracks(id)	GET /catalog/charts/{id}/tracks/	Chart tracks
GetArtist(id)	GET /catalog/artists/{id}/	Artist metadata
GetArtistTracks(id)	GET /catalog/artists/{id}/tracks/	Artist catalog tracks
GetLabel(id)	GET /catalog/labels/{id}/	Label metadata
GetLabelTracks(id)	GET /catalog/labels/{id}/releases/	Likely wrong — paginates releases as Track
Search(query)	GET /catalog/search/?q={query}&order_by=-publish_date	Exists but unused
Pagination helpers: getAllTracks, getAllPlaylistItems (handles {track: {...}} wrapper).
URL parsing
ParseLink(rawURL) in client.go supports:
* Types: track, release, playlist, chart, label, artist (LinkType in types.go)
* Platforms detected: beatport vs beatsource (stored on ParsedLink.Platform)
* Gap: Platform is never used when creating the API client — all requests go to Beatport API base
Download flow (job runner)
runJob() in handlers.go resolves URL → fetches tracks → worker pool downloads:
* Supported in switch: track, release, playlist, chart, artist
* Not supported: LinkTypeLabel → "Unsupported URL type"
* Per-track: GetTrackDownload with quality fallback chain, DownloadFile, optional cover, WriteMetadata via ffmpeg

1. Frontend Web UI
Single HTML page — /Users/dvandelindt/Projects/BeatportDL-UI/web/index.htmlLogic — /Users/dvandelindt/Projects/BeatportDL-UI/web/js/app.jsStyles — /Users/dvandelindt/Projects/BeatportDL-UI/web/css/style.css
Views (sidebar navigation)
View	ID	Features
Download	view-download	Paste/drag Beatport URL, quality pills (FLAC/AAC), start download, recent activity
Queue	view-queue	Job cards, per-track progress, ZIP download, delete job
Fix Tags	view-fix	Run metadata fix on a directory
Settings	view-settings	Credentials, output dir, quality, workers, cover options, port
Frontend ↔ backend
API	Used by
GET/POST /api/settings	Settings load/save
POST /api/auth/test	Test connection
POST /api/download	Start job {url, quality}
GET /api/jobs	Initial load + 3s polling
DELETE /api/jobs/{id}	Remove job
GET /api/jobs/{id}/zip	Download ZIP
POST /api/fix	Fix tags
GET /api/ws	Real-time job_update, track_progress, fix_progress
No client-side router — view switching is CSS class toggles. No search UI anywhere.

1. Existing Search Functionality
Backend: client-only, not exposed
client.go
Lines 348-354
// Search

func (c *Client) Search(query string) (*SearchResults, error) {
	var results SearchResults
	err := c.apiGet(fmt.Sprintf("/catalog/search/?q=%s&order_by=-publish_date", url.QueryEscape(query)), &results)
	return &results, err
}

Result type (types.go):
types.go
Lines 134-139
type SearchResults struct {
	Tracks    []Track    `json:"tracks"`
	Releases  []Release  `json:"releases"`
	Artists   []Artist   `json:"artists"`
	Labels    []Label    `json:"labels"`
}
* Search() is never called elsewhere in the repo
* No HTTP route for search in server.go
* No handler in handlers.go
* No frontend references to search
What search would need auth for
Same as downloads: credentials in config → beatport.NewClient → Authenticate() before apiGet.

5. API Routes / Handlers (Tracks, Artists, Search)
Registered routes (internal/server/server.go)
Route	Handler	Track/artist relevance
POST /api/download	handleDownload	Indirect — resolves URL to tracks via Beatport client
GET /api/jobs	handleListJobs	Returns TrackSummary[] per job
—	runJob	GetTrack, GetArtist, GetArtistTracks, etc.
—	downloadTrack	GetTrackDownload, file write
Key server types (internal/server/ws.go)
* TrackSummary — {id, artist, title, status, message} (job progress only)
* JobPayload — job metadata + track list
* ProgressPayload — per-track download progress over WebSocket
Artist handling today
* By URL: LinkTypeArtist in runJob → GetArtist + GetArtistTracks → download all tracks
* No GET /api/artists/{id} or search-by-name endpoint
Track handling today
* By URL: single track or bulk from release/playlist/chart/artist
* No GET /api/tracks/{id} preview endpoint
* Track metadata only surfaces inside job queue UI

6. Gaps for Adding Track/Artist Search
Already in place (good foundation)
1. Client.Search(query) — Beatport catalog search with tracks + artists (+ releases/labels)
2. Rich Track and Artist types — enough for result cards (name, slug, BPM, artists, release, image URIs)
3. Download pipeline — can download by track URL or enqueue https://www.beatport.com/track/{slug}/{id}
4. Auth flow — settings + test auth already work
5. ArtistNames, Track.FullTitle(), Track.Filename() — display helpers exist
Missing backend work
Gap	Detail
Search HTTP handler	e.g. GET /api/search?q=... — authenticate, call client.Search, return JSON
Optional filters	No support for `type=tracks
Dedicated track/artist endpoints	No GET /api/tracks/{id} or GET /api/artists/{id}/tracks for drill-down without full download job
Beatsource	ParsedLink.Platform ignored; client always uses Beatport API base
Label downloads	GetLabel/GetLabelTracks exist; runJob doesn't handle LinkTypeLabel; GetLabelTracks path may be incorrect
In-memory jobs	Jobs lost on restart; search results wouldn't persist unless stored client-side
No tests	Zero *_test.go files
Missing frontend work
Gap	Detail
Search view / nav item	No search input, results list, or tabs for tracks vs artists
Result → action wiring	No "Download track" or "Download artist catalog" from search hits
Debouncing / loading states	N/A today
Result rendering	No cover art, BPM, label, or Beatport deep links in UI
Empty/error states	Auth-required messaging for search not designed
Suggested integration path (guidance only)
1. Backend: Add GET /api/search?q= in server.go + handleSearch — mirror handleTestAuth credential check, return SearchResults (or a trimmed DTO for UI).
2. Frontend: New "Search" view with input, debounced fetch('/api/search?q=...'), sections for tracks and artists.
    * Actions:
    * Track row → POST /api/download with https://www.beatport.com/track/{slug}/{id}
    * Artist row → POST /api/download with https://www.beatport.com/artist/{slug}/{id} (already supported in runJob)
3. Optional: GET /api/artists/{id}/tracks for preview before bulk download; pagination if Beatport search returns limited result sets.
Minor inconsistencies worth noting
* medium-hls appears in settings UI and qualityFallbackChain, but downloadTrack never calls GetTrackStream / DownloadHLSStream
* auto_fix_metadata config field exists but isn't invoked after download in runJob
* README is essentially empty (Beatport UI only)

Quick Reference: Key Files
Concern	Path
Entry point	/Users/dvandelindt/Projects/BeatportDL-UI/main.go
Routes	/Users/dvandelindt/Projects/BeatportDL-UI/internal/server/server.go
Handlers + jobs	/Users/dvandelindt/Projects/BeatportDL-UI/internal/server/handlers.go
Beatport client + Search	/Users/dvandelindt/Projects/BeatportDL-UI/internal/beatport/client.go
Types (Track, Artist, SearchResults)	/Users/dvandelindt/Projects/BeatportDL-UI/internal/beatport/types.go
Frontend app	/Users/dvandelindt/Projects/BeatportDL-UI/web/js/app.js
UI layout	/Users/dvandelindt/Projects/BeatportDL-UI/web/index.html
Config	/Users/dvandelindt/Projects/BeatportDL-UI/internal/config/config.go
Bottom line: The repo is a URL-paste downloader with solid Beatport v4 client code. Search exists only at the client layer (Client.Search); exposing it requires a thin API handler and a new frontend view. Artist bulk download already works via artist URLs — search mainly needs discovery UI and a bridge from results to the existing /api/download flow.
