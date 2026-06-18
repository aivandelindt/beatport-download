package server

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"beatportdl-ui/internal/beatport"
	"beatportdl-ui/internal/config"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	hub    *Hub
	cfg    *config.Config
	cfgMu  sync.RWMutex
	jobs   map[string]*Job
	jobsMu sync.RWMutex
}

type Job struct {
	ID        string
	URL       string
	Name      string // resolved collection name (playlist/release/artist)
	Status    string
	Total     int
	Completed int
	Failed    int
	Tracks    []TrackSummary
	OutputDir string
	Files     []string
	filesMu   sync.Mutex
	CreatedAt time.Time
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		hub:  NewHub(),
		cfg:  cfg,
		jobs: make(map[string]*Job),
	}
}

func respond(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func respondErr(w http.ResponseWriter, code int, msg string) {
	respond(w, code, map[string]string{"error": msg})
}

// GET /api/settings
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	respond(w, 200, s.cfg)
}

// POST /api/settings
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		respondErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	s.cfgMu.Lock()
	s.cfg = &newCfg
	s.cfgMu.Unlock()
	if err := newCfg.Save(); err != nil {
		respondErr(w, 500, "failed to save config: "+err.Error())
		return
	}
	respond(w, 200, map[string]string{"status": "saved"})
}

// POST /api/auth/test
func (s *Server) handleTestAuth(w http.ResponseWriter, r *http.Request) {
	s.cfgMu.RLock()
	username := s.cfg.Username
	password := s.cfg.Password
	s.cfgMu.RUnlock()

	if username == "" || password == "" {
		respondErr(w, 400, "username and password required")
		return
	}
	client := beatport.NewClient(username, password, credentialsDir())
	if err := client.Authenticate(); err != nil {
		respondErr(w, 401, "authentication failed: "+err.Error())
		return
	}
	respond(w, 200, map[string]string{"status": "authenticated"})
}

type SearchTrackItem struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Artists  string `json:"artists"`
	BPM      int    `json:"bpm,omitempty"`
	Genre    string `json:"genre,omitempty"`
	Label    string `json:"label,omitempty"`
	Length   string `json:"length,omitempty"`
	Key      string `json:"key,omitempty"`
	Camelot  string `json:"camelot,omitempty"`
	Released string `json:"released,omitempty"`
	ImageURI string `json:"image_uri,omitempty"`
	URL      string `json:"url"`
}

type SearchArtistItem struct {
	ID        int               `json:"id"`
	Name      string            `json:"name"`
	ImageURI  string            `json:"image_uri,omitempty"`
	URL       string            `json:"url"`
	TopTracks []SearchTrackItem `json:"top_tracks,omitempty"`
}

type SearchResultPage[T any] struct {
	Count int `json:"count"`
	Page  int `json:"page"`
	Items []T `json:"items"`
}

type SearchResponsePayload struct {
	Query   string                        `json:"query"`
	Type    string                        `json:"type"`
	Tracks  *SearchResultPage[SearchTrackItem]  `json:"tracks,omitempty"`
	Artists *SearchResultPage[SearchArtistItem] `json:"artists,omitempty"`
}

// GET /api/genres
func (s *Server) handleGenres(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	s.cfgMu.RLock()
	username := s.cfg.Username
	password := s.cfg.Password
	s.cfgMu.RUnlock()

	if username == "" || password == "" {
		respondErr(w, 400, "credentials not configured — go to Settings first")
		return
	}

	client := beatport.NewClient(username, password, credentialsDir())
	if err := client.Authenticate(); err != nil {
		slog.Error("genres auth failed", "error", err)
		respondErr(w, 401, "authentication failed: "+err.Error())
		return
	}

	genres, err := client.GetGenres(ctx)
	if err != nil {
		slog.Error("genres fetch failed", "error", err)
		respondErr(w, 502, "failed to load genres: "+err.Error())
		return
	}

	type genreItem struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	items := make([]genreItem, 0, len(genres))
	for _, g := range genres {
		items = append(items, genreItem{ID: g.ID, Name: g.Name, Slug: g.Slug})
	}
	slog.Info("genres loaded", "count", len(items))
	respond(w, 200, items)
}

// GET /api/search?q=...&type=tracks|artists|all&page=1&per_page=50&genre_id=...&include_artists=1&top_tracks=1
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	genreID, _ := strconv.Atoi(r.URL.Query().Get("genre_id"))

	if query == "" && genreID <= 0 {
		respondErr(w, 400, "q or genre_id required")
		return
	}

	searchType := strings.TrimSpace(r.URL.Query().Get("type"))
	if searchType == "" {
		searchType = "all"
	}
	switch searchType {
	case "all", "tracks", "artists":
	default:
		respondErr(w, 400, "type must be all, tracks, or artists")
		return
	}

	includeArtists := r.URL.Query().Get("include_artists") == "1" || r.URL.Query().Get("include_artists") == "true"
	topTracks := r.URL.Query().Get("top_tracks") == "1" || r.URL.Query().Get("top_tracks") == "true"

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 200 {
		perPage = 200
	}
	// Beatport API allows max 100 per page; round down to nearest 50
	perPage = (perPage / 50) * 50
	if perPage < 50 {
		perPage = 50
	}

	s.cfgMu.RLock()
	username := s.cfg.Username
	password := s.cfg.Password
	s.cfgMu.RUnlock()

	if username == "" || password == "" {
		respondErr(w, 400, "credentials not configured — go to Settings first")
		return
	}

	client := beatport.NewClient(username, password, credentialsDir())
	if err := client.Authenticate(); err != nil {
		slog.Error("search auth failed", "error", err)
		respondErr(w, 401, "authentication failed: "+err.Error())
		return
	}

	slog.Info("search",
		"q", query,
		"type", searchType,
		"genre_id", genreID,
		"page", page,
		"per_page", perPage,
		"include_artists", includeArtists,
		"top_tracks", topTracks,
	)

	payload := SearchResponsePayload{
		Query: query,
		Type:  searchType,
	}

	if searchType == "all" || searchType == "tracks" {
		var tracks []beatport.Track
		var err error
		if query == "" && genreID > 0 {
			tracks, err = listTracksByGenrePaginated(ctx, client, genreID, page, perPage)
		} else {
			tracks, err = searchTracksPaginated(ctx, client, query, page, perPage, genreID)
		}
		if err != nil {
			slog.Error("track search failed", "error", err)
			respondErr(w, 502, "track search failed: "+err.Error())
			return
		}
		items := make([]SearchTrackItem, 0, len(tracks))
		for _, t := range tracks {
			items = append(items, trackToSearchItem(t))
		}
		payload.Tracks = &SearchResultPage[SearchTrackItem]{
			Count: len(items),
			Page:  page,
			Items: items,
		}
	}

	fetchArtists := searchType == "all" || searchType == "artists" || (searchType == "tracks" && includeArtists)
	if fetchArtists {
		if query == "" {
			payload.Artists = &SearchResultPage[SearchArtistItem]{Count: 0, Page: page, Items: []SearchArtistItem{}}
		} else {
			artists, err := searchArtistsPaginated(ctx, client, query, page, perPage, genreID)
			if err != nil {
				slog.Error("artist search failed", "error", err)
				respondErr(w, 502, "artist search failed: "+err.Error())
				return
			}
			items := make([]SearchArtistItem, 0, len(artists))
			for _, a := range artists {
				item := artistToSearchItem(a)
				if topTracks {
					top, err := client.GetArtistTopTracks(ctx, a.ID, 10)
					if err == nil {
						for _, t := range top {
							item.TopTracks = append(item.TopTracks, trackToSearchItem(t))
						}
					}
				}
				items = append(items, item)
			}
			payload.Artists = &SearchResultPage[SearchArtistItem]{
				Count: len(items),
				Page:  page,
				Items: items,
			}
		}
	}

	trackCount := 0
	if payload.Tracks != nil {
		trackCount = len(payload.Tracks.Items)
	}
	slog.Info("search complete", "tracks", trackCount, "artists", artistResultCount(payload.Artists))
	respond(w, 200, payload)
}

func artistResultCount(page *SearchResultPage[SearchArtistItem]) int {
	if page == nil {
		return 0
	}
	return len(page.Items)
}

func listTracksByGenrePaginated(ctx context.Context, client *beatport.Client, genreID, page, perPage int) ([]beatport.Track, error) {
	const apiMax = 100
	remaining := perPage
	apiPage := page
	var out []beatport.Track

	for remaining > 0 {
		batch := remaining
		if batch > apiMax {
			batch = apiMax
		}
		raw, err := client.ListTracksByGenre(ctx, genreID, apiPage, batch)
		if err != nil {
			return nil, err
		}
		if len(raw.Results) == 0 {
			break
		}
		out = append(out, raw.Results...)
		remaining -= len(raw.Results)
		apiPage++
		if len(raw.Results) < batch {
			break
		}
	}
	return out, nil
}

func searchTracksPaginated(ctx context.Context, client *beatport.Client, query string, page, perPage, genreID int) ([]beatport.Track, error) {
	const apiMax = 100
	remaining := perPage
	apiPage := page
	var out []beatport.Track

	for remaining > 0 {
		batch := remaining
		if batch > apiMax {
			batch = apiMax
		}
		raw, err := client.SearchTracks(ctx, query, apiPage, batch, genreID)
		if err != nil {
			return nil, err
		}
		if len(raw.Results) == 0 {
			break
		}
		out = append(out, raw.Results...)
		remaining -= len(raw.Results)
		apiPage++
		if len(raw.Results) < batch {
			break
		}
	}
	return out, nil
}

func searchArtistsPaginated(ctx context.Context, client *beatport.Client, query string, page, perPage, genreID int) ([]beatport.Artist, error) {
	const apiMax = 100
	remaining := perPage
	apiPage := page
	var out []beatport.Artist

	for remaining > 0 {
		batch := remaining
		if batch > apiMax {
			batch = apiMax
		}
		raw, err := client.SearchArtists(ctx, query, apiPage, batch, genreID)
		if err != nil {
			return nil, err
		}
		if len(raw.Results) == 0 {
			break
		}
		out = append(out, raw.Results...)
		remaining -= len(raw.Results)
		apiPage++
		if len(raw.Results) < batch {
			break
		}
	}
	return out, nil
}

func trackToSearchItem(t beatport.Track) SearchTrackItem {
	item := SearchTrackItem{
		ID:      t.ID,
		Title:   t.FullTitle(),
		Artists: beatport.ArtistNames(t.Artists),
		BPM:     t.BPM,
		Length:  t.Length,
		URL:     fmt.Sprintf("https://www.beatport.com/track/%s/%d", t.Slug, t.ID),
	}
	if t.Genre.Name != "" {
		item.Genre = t.Genre.Name
	}
	if t.Release.Label.Name != "" {
		item.Label = t.Release.Label.Name
	}
	if t.Key != nil {
		item.Key = t.Key.Name
		item.Camelot = t.Key.CamelotCode()
	}
	if t.PublishDate != "" {
		item.Released = t.PublishDate
	} else if t.NewReleaseDate != "" {
		item.Released = t.NewReleaseDate
	} else if t.Release.NewReleaseDate != "" {
		item.Released = t.Release.NewReleaseDate
	}
	if t.Image.URI != "" {
		item.ImageURI = t.Image.URI
	} else if t.Release.Image.URI != "" {
		item.ImageURI = t.Release.Image.URI
	}
	return item
}

func artistToSearchItem(a beatport.Artist) SearchArtistItem {
	item := SearchArtistItem{
		ID:   a.ID,
		Name: a.Name,
		URL:  fmt.Sprintf("https://www.beatport.com/artist/%s/%d", a.Slug, a.ID),
	}
	if a.Image.URI != "" {
		item.ImageURI = a.Image.URI
	}
	return item
}

// POST /api/download
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     string `json:"url"`
		Quality string `json:"quality,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "invalid JSON")
		return
	}
	if req.URL == "" {
		respondErr(w, 400, "url required")
		return
	}

	s.cfgMu.RLock()
	cfg := *s.cfg
	s.cfgMu.RUnlock()

	if req.Quality != "" {
		cfg.Quality = req.Quality
	}
	if cfg.Username == "" || cfg.Password == "" {
		respondErr(w, 400, "credentials not configured — go to Settings first")
		return
	}

	jobID := uuid.New().String()[:8]
	job := &Job{
		ID:        jobID,
		URL:       req.URL,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	s.jobsMu.Lock()
	s.jobs[jobID] = job
	s.jobsMu.Unlock()

	s.broadcastJob(job)
	slog.Info("download queued", "job_id", jobID, "url", req.URL, "quality", cfg.Quality)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				job.Status = "error"
				s.broadcastJob(job)
			}
		}()
		s.runJob(job, &cfg)
	}()

	respond(w, 202, map[string]string{"job_id": jobID})
}

// GET /api/jobs
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()

	jobs := make([]JobPayload, 0, len(s.jobs))
	for _, j := range s.jobs {
		j.filesMu.Lock()
		hasFiles := len(j.Files) > 0
		j.filesMu.Unlock()

		jobs = append(jobs, JobPayload{
			JobID:     j.ID,
			URL:       j.URL,
			Name:      j.Name,
			Status:    j.Status,
			Total:     j.Total,
			Completed: j.Completed,
			Failed:    j.Failed,
			Tracks:    j.Tracks,
			HasFiles:  hasFiles,
		})
	}
	respond(w, 200, jobs)
}

// DELETE /api/jobs/{id}
func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.jobsMu.Lock()
	delete(s.jobs, id)
	s.jobsMu.Unlock()
	respond(w, 200, map[string]string{"status": "deleted"})
}

// GET /api/jobs/{id}/zip — writes zip to temp file first so Content-Length is known
func (s *Server) handleJobZip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.jobsMu.RLock()
	job, ok := s.jobs[id]
	s.jobsMu.RUnlock()

	if !ok {
		respondErr(w, 404, "job not found")
		return
	}

	job.filesMu.Lock()
	files := make([]string, len(job.Files))
	copy(files, job.Files)
	job.filesMu.Unlock()

	if len(files) == 0 {
		respondErr(w, 400, "no downloaded files for this job")
		return
	}

	// Write to temp file so we can set Content-Length
	tmp, err := os.CreateTemp("", "beatportdl-*.zip")
	if err != nil {
		respondErr(w, 500, "failed to create temp file: "+err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	zw := zip.NewWriter(tmp)
	for _, path := range files {
		if err := addFileToZip(zw, path, filepath.Base(path)); err != nil {
			continue // skip unreadable files
		}
	}
	zw.Close()
	tmp.Close()

	// Stat for size
	info, err := os.Stat(tmpPath)
	if err != nil {
		respondErr(w, 500, "temp file stat failed")
		return
	}

	// Use job name for the zip filename
	zipName := "beatport"
	if job.Name != "" {
		zipName = job.Name
	}
	zipName = sanitizeFilenameSimple(zipName) + ".zip"

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+zipName+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))

	f, err := os.Open(tmpPath)
	if err != nil {
		respondErr(w, 500, "failed to open temp zip")
		return
	}
	defer f.Close()
	io.Copy(w, f)
}

func addFileToZip(zw *zip.Writer, filePath, name string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = name
	header.Method = zip.Store // audio is already compressed

	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// POST /api/fix
func (s *Server) handleFix(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "invalid JSON")
		return
	}
	dir := req.Dir
	if dir == "" {
		s.cfgMu.RLock()
		dir = s.cfg.OutputDir
		s.cfgMu.RUnlock()
	}
	if _, err := os.Stat(dir); err != nil {
		respondErr(w, 400, "directory not found: "+dir)
		return
	}
	var messages []string
	err := beatport.FixMetadata(dir, func(msg string) {
		messages = append(messages, msg)
		s.hub.Broadcast(WSMessage{Type: "fix_progress", Payload: map[string]string{"message": msg}})
	})
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, 200, map[string]interface{}{"status": "done", "messages": messages})
}

// WS /api/ws
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.Register(conn)
	defer s.hub.Unregister(conn)
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// ── Job runner ────────────────────────────────────────────────────────────────

func (s *Server) runJob(job *Job, cfg *config.Config) {
	client := beatport.NewClient(cfg.Username, cfg.Password, credentialsDir())

	job.Status = "running"
	s.broadcastJob(job)

	link, err := beatport.ParseLink(job.URL)
	if err != nil {
		s.failJob(job, "Invalid URL: "+err.Error())
		return
	}

	if err := client.Authenticate(); err != nil {
		s.failJob(job, "Authentication failed: "+err.Error())
		return
	}

	var tracks []beatport.Track
	var collectionName string

	switch link.Type {
	case beatport.LinkTypeTrack:
		track, err := client.GetTrack(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch track: "+err.Error())
			return
		}
		tracks = []beatport.Track{*track}
		collectionName = beatport.ArtistNames(track.Artists) + " - " + track.FullTitle()

	case beatport.LinkTypeRelease:
		release, err := client.GetRelease(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch release: "+err.Error())
			return
		}
		collectionName = beatport.ArtistNames(release.Artists) + " - " + release.Name
		tracks, err = client.GetReleaseTracks(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch release tracks: "+err.Error())
			return
		}

	case beatport.LinkTypePlaylist:
		playlist, err := client.GetPlaylist(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch playlist: "+err.Error())
			return
		}
		collectionName = playlist.Name
		tracks, err = client.GetPlaylistTracks(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch playlist tracks: "+err.Error())
			return
		}

	case beatport.LinkTypeChart:
		chart, err := client.GetChart(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch chart: "+err.Error())
			return
		}
		collectionName = chart.Name
		tracks, err = client.GetChartTracks(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch chart tracks: "+err.Error())
			return
		}

	case beatport.LinkTypeArtist:
		artist, err := client.GetArtist(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch artist: "+err.Error())
			return
		}
		collectionName = artist.Name
		tracks, err = client.GetArtistTracks(link.ID)
		if err != nil {
			s.failJob(job, "Failed to fetch artist tracks: "+err.Error())
			return
		}

	default:
		s.failJob(job, "Unsupported URL type")
		return
	}

	if len(tracks) == 0 {
		s.failJob(job, "No tracks found")
		return
	}

	job.Name = collectionName
	job.Total = len(tracks)
	job.Tracks = make([]TrackSummary, len(tracks))
	for i, t := range tracks {
		job.Tracks[i] = TrackSummary{
			ID:     t.ID,
			Artist: beatport.ArtistNames(t.Artists),
			Title:  t.FullTitle(),
			Status: "queued",
		}
	}
	s.broadcastJob(job)

	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = defaultOutputDir()
	}
	if cfg.CreateSubdirs && collectionName != "" && link.Type != beatport.LinkTypeTrack {
		outputDir = filepath.Join(outputDir, beatport.SanitizePath(collectionName))
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		s.failJob(job, "Cannot create output directory "+outputDir+": "+err.Error())
		return
	}
	job.OutputDir = outputDir

	// Worker pool — 2 concurrent downloads to avoid rate limiting
	type result struct {
		index    int
		err      error
		filePath string
	}
	type workItem struct {
		index int
		track beatport.Track
	}

	workerCount := cfg.MaxWorkers
	if workerCount <= 0 {
		workerCount = 2
	}
	if workerCount > len(tracks) {
		workerCount = len(tracks)
	}

	workCh := make(chan workItem, len(tracks))
	resultCh := make(chan result, len(tracks))

	for i := 0; i < workerCount; i++ {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					resultCh <- result{index: -1, err: fmt.Errorf("worker panic: %v", r)}
				}
			}()
			for item := range workCh {
				// Mark as downloading — each worker writes its own index, no race
				job.Tracks[item.index].Status = "downloading"
				// Don't broadcastJob here — it can block workers via WebSocket backpressure

				filePath, err := s.downloadTrack(client, &item.track, cfg, outputDir, func(progress float64, msg string) {
					s.hub.Broadcast(WSMessage{
						Type: "track_progress",
						Payload: ProgressPayload{
							JobID:       job.ID,
							TrackID:     item.track.ID,
							TrackTitle:  item.track.FullTitle(),
							TrackArtist: beatport.ArtistNames(item.track.Artists),
							Status:      "downloading",
							Progress:    progress,
							Message:     msg,
						},
					})
				})
				resultCh <- result{item.index, err, filePath}
			}
		}()
	}

	for i, t := range tracks {
		workCh <- workItem{i, t}
	}
	close(workCh)

	for range tracks {
		res := <-resultCh
		if res.index < 0 {
			// Worker panic — don't attribute to a specific track
			job.Failed++
			s.broadcastJob(job)
			continue
		}
		if res.err != nil {
			job.Failed++
			if res.index >= 0 && res.index < len(job.Tracks) {
				job.Tracks[res.index].Status = "error"
				job.Tracks[res.index].Message = res.err.Error()
			}
		} else {
			job.Completed++
			if res.index >= 0 && res.index < len(job.Tracks) {
				job.Tracks[res.index].Status = "done"
			}
			if res.filePath != "" {
				job.filesMu.Lock()
				job.Files = append(job.Files, res.filePath)
				job.filesMu.Unlock()
			}
		}
		s.broadcastJob(job)
	}

	job.Status = "done"
	if job.Failed > 0 && job.Completed == 0 {
		job.Status = "error"
	}
	s.broadcastJob(job)
}

// downloadTrack downloads one track, trying quality fallbacks if needed.
func (s *Server) downloadTrack(client *beatport.Client, track *beatport.Track, cfg *config.Config, outputDir string, progressFn func(float64, string)) (string, error) {
	// Build quality fallback chain based on requested quality
	qualities := qualityFallbackChain(cfg.Quality)

	var downloadURL string
	var chosenQuality string

	for _, q := range qualities {
		dl, err := client.GetTrackDownload(track.ID, q)
		if err != nil {
			if isAccessError(err) {
				continue // try next quality
			}
			return "", fmt.Errorf("download URL fetch failed: %w", err)
		}
		downloadURL = dl.Location
		chosenQuality = q
		break
	}

	if downloadURL == "" {
		return "", fmt.Errorf("track not available at any quality (tried: %s)", strings.Join(qualities, ", "))
	}

	// Use the actual quality for the filename extension
	filename := track.Filename(chosenQuality)
	destPath := filepath.Join(outputDir, filename)

	if _, err := os.Stat(destPath); err == nil {
		return destPath, nil // already exists
	}

	progressFn(0, "Downloading...")
	tmpPath := destPath + ".part"

	if err := client.DownloadFile(downloadURL, tmpPath, func(downloaded, total int64) {
		if total > 0 {
			progressFn(float64(downloaded)/float64(total)*90, "Downloading...")
		}
	}); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("download failed: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("file rename failed: %w", err)
	}

	progressFn(90, "Writing metadata...")

	meta := beatport.BuildMetadata(track)

	// Cover art: prefer release image (album artwork) over track image (waveform)
	if cfg.EmbedCover || cfg.SaveCover {
		imageURL := track.Release.Image.DynamicURI
		if imageURL == "" {
			imageURL = track.Release.Image.URI
		}
		if imageURL == "" {
			imageURL = track.Image.DynamicURI
		}
		if imageURL != "" {
			// Per-track temp cover to avoid races between workers
			coverPath := filepath.Join(outputDir, fmt.Sprintf(".cover_%d.jpg", track.ID))
			if err := client.DownloadCover(imageURL, coverPath); err == nil {
				if cfg.EmbedCover {
					meta.CoverPath = coverPath
				}
				if cfg.SaveCover {
					// Copy to the shared cover.jpg (last writer wins, all are same release art)
					sharedCover := filepath.Join(outputDir, "cover.jpg")
					copyFile(coverPath, sharedCover)
				}
				// Always clean up the per-track temp cover after embedding
				defer os.Remove(coverPath)
			}
		}
	}

	if err := beatport.WriteMetadata(destPath, meta); err != nil {
		// Non-fatal — file is still there
		progressFn(100, "Done (metadata: "+err.Error()+")")
		return destPath, nil
	}

	progressFn(100, "Done")
	return destPath, nil
}

// qualityFallbackChain returns the quality to try in order.
func qualityFallbackChain(requested string) []string {
	switch requested {
	case "lossless":
		return []string{"lossless", "high", "medium"}
	case "high":
		return []string{"high", "medium"}
	case "medium", "medium-hls":
		return []string{"medium"}
	default:
		return []string{"lossless", "high", "medium"}
	}
}

// isAccessError returns true if the error is a 403/404 indicating subscription limits.
func isAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "403") ||
		strings.Contains(msg, "404") ||
		strings.Contains(msg, "No Track matches")
}

func (s *Server) failJob(job *Job, msg string) {
	job.Status = "error"
	if len(job.Tracks) == 0 {
		job.Tracks = []TrackSummary{{Status: "error", Message: msg}}
	}
	s.broadcastJob(job)
}

func (s *Server) broadcastJob(job *Job) {
	job.filesMu.Lock()
	hasFiles := len(job.Files) > 0
	job.filesMu.Unlock()

	s.hub.Broadcast(WSMessage{
		Type: "job_update",
		Payload: JobPayload{
			JobID:     job.ID,
			URL:       job.URL,
			Name:      job.Name,
			Status:    job.Status,
			Total:     job.Total,
			Completed: job.Completed,
			Failed:    job.Failed,
			Tracks:    job.Tracks,
			HasFiles:  hasFiles,
		},
	})
}

func credentialsDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.Getenv("HOME")
		if dir == "" {
			dir = "/tmp"
		}
	}
	path := filepath.Join(dir, "beatportdl-ui")
	os.MkdirAll(path, 0700)
	return path
}

func defaultOutputDir() string {
	if _, err := os.Stat("/downloads"); err == nil {
		return "/downloads"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Music", "BeatportDL")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func sanitizeFilenameSimple(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	return strings.TrimSpace(r.Replace(s))
}
