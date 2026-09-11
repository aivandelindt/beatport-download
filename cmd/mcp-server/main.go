package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"beatportdl-ui/internal/beatport"
	"beatportdl-ui/internal/config"
)

const (
	serverName    = "beatport-download-mcp"
	serverVersion = "1.0.0"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcErrorObject `json:"error,omitempty"`
}

type rpcErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type mcpToolResult struct {
	Content []mcpTextContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func main() {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	for {
		msg, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			_ = writeError(writer, nil, -32700, "failed to read message: "+err.Error())
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			_ = writeError(writer, nil, -32700, "invalid JSON: "+err.Error())
			continue
		}
		if req.Method == "" {
			_ = writeError(writer, req.ID, -32600, "missing method")
			continue
		}

		switch req.Method {
		case "initialize":
			handleInitialize(writer, req)
		case "notifications/initialized":
			// No-op by spec.
		case "tools/list":
			handleToolsList(writer, req)
		case "tools/call":
			handleToolsCall(writer, req)
		case "ping":
			_ = writeResult(writer, req.ID, map[string]interface{}{})
		default:
			_ = writeError(writer, req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func handleInitialize(writer *bufio.Writer, req rpcRequest) {
	protocolVersion := "2025-03-26"
	var params initializeParams
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &params) == nil && params.ProtocolVersion != "" {
		protocolVersion = params.ProtocolVersion
	}

	result := map[string]interface{}{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    serverName,
			"version": serverVersion,
		},
	}
	_ = writeResult(writer, req.ID, result)
}

func handleToolsList(writer *bufio.Writer, req rpcRequest) {
	result := map[string]interface{}{
		"tools": []mcpTool{
			{
				Name:        "beatport_test_auth",
				Description: "Validate configured Beatport credentials",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				Name:        "beatport_parse_url",
				Description: "Parse a Beatport or Beatsource URL into type + ID",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "Beatport or Beatsource URL",
						},
					},
					"required": []string{"url"},
				},
			},
			{
				Name:        "beatport_get_genres",
				Description: "List available Beatport genres",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				Name:        "beatport_search",
				Description: "Search Beatport catalog (tracks/artists/releases/labels/charts/all)",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Search text. Required unless genre_id is set.",
						},
						"search_type": map[string]interface{}{
							"type":        "string",
							"description": "all|tracks|artists|releases|labels|charts",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "Result page (default 1)",
						},
						"per_page": map[string]interface{}{
							"type":        "integer",
							"description": "Results per page (default 50, max 100)",
						},
						"genre_id": map[string]interface{}{
							"type":        "integer",
							"description": "Optional Beatport genre ID",
						},
						"include_artists": map[string]interface{}{
							"type":        "boolean",
							"description": "For tracks search, also return matching artists",
						},
						"top_tracks": map[string]interface{}{
							"type":        "boolean",
							"description": "For artists, include top tracks",
						},
					},
				},
			},
			{
				Name:        "beatport_download_url",
				Description: "Download audio files from a Beatport URL (track, release, playlist, chart, artist)",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "Beatport URL: track, release, playlist, chart, or artist",
						},
						"quality": map[string]interface{}{
							"type":        "string",
							"description": "lossless|high|medium",
						},
						"output_dir": map[string]interface{}{
							"type":        "string",
							"description": "Optional output directory override",
						},
						"create_subdirs": map[string]interface{}{
							"type":        "boolean",
							"description": "Use collection subdirectory for non-track URLs",
						},
						"save_cover": map[string]interface{}{
							"type":        "boolean",
							"description": "Save cover.jpg in output directory",
						},
						"embed_cover": map[string]interface{}{
							"type":        "boolean",
							"description": "Embed album art in audio metadata",
						},
					},
					"required": []string{"url"},
				},
			},
		},
	}
	_ = writeResult(writer, req.ID, result)
}

func handleToolsCall(writer *bufio.Writer, req rpcRequest) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		_ = writeError(writer, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	if params.Arguments == nil {
		params.Arguments = map[string]interface{}{}
	}

	ctx := context.Background()
	switch params.Name {
	case "beatport_test_auth":
		resp, err := testAuthHandler(ctx)
		_ = writeResult(writer, req.ID, toolResultFrom(resp, err))
	case "beatport_parse_url":
		var args ParseURLRequest
		if err := bindArgs(params.Arguments, &args); err != nil {
			_ = writeResult(writer, req.ID, toolErrorResult("invalid arguments: "+err.Error()))
			return
		}
		resp, err := parseURLHandler(ctx, args)
		_ = writeResult(writer, req.ID, toolResultFrom(resp, err))
	case "beatport_get_genres":
		resp, err := genresHandler(ctx)
		_ = writeResult(writer, req.ID, toolResultFrom(resp, err))
	case "beatport_search":
		var args SearchRequest
		if err := bindArgs(params.Arguments, &args); err != nil {
			_ = writeResult(writer, req.ID, toolErrorResult("invalid arguments: "+err.Error()))
			return
		}
		resp, err := searchHandler(ctx, args)
		_ = writeResult(writer, req.ID, toolResultFrom(resp, err))
	case "beatport_download_url":
		var args DownloadRequest
		if err := bindArgs(params.Arguments, &args); err != nil {
			_ = writeResult(writer, req.ID, toolErrorResult("invalid arguments: "+err.Error()))
			return
		}
		resp, err := downloadHandler(ctx, args)
		_ = writeResult(writer, req.ID, toolResultFrom(resp, err))
	default:
		_ = writeResult(writer, req.ID, toolErrorResult("unknown tool: "+params.Name))
	}
}

func toolResultFrom(v interface{}, err error) mcpToolResult {
	if err != nil {
		return toolErrorResult(err.Error())
	}
	return mcpToolResult{
		Content: []mcpTextContent{{Type: "text", Text: toJSONText(v)}},
	}
}

func toolErrorResult(msg string) mcpToolResult {
	return mcpToolResult{
		Content: []mcpTextContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}

func toJSONText(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func bindArgs(args map[string]interface{}, out interface{}) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := 0

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			value = strings.TrimSpace(strings.TrimPrefix(value, "content-length:"))
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid content length: %w", err)
			}
			contentLength = n
		}
	}

	if contentLength <= 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func writeResult(writer *bufio.Writer, id json.RawMessage, result interface{}) error {
	return writeMessage(writer, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeError(writer *bufio.Writer, id json.RawMessage, code int, message string) error {
	return writeMessage(writer, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcErrorObject{
			Code:    code,
			Message: message,
		},
	})
}

func writeMessage(writer *bufio.Writer, msg rpcResponse) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	if _, err := writer.Write(body); err != nil {
		return err
	}
	return writer.Flush()
}

type TestAuthResponse struct {
	Authenticated bool   `json:"authenticated"`
	Message       string `json:"message"`
}

func testAuthHandler(ctx context.Context) (TestAuthResponse, error) {
	client, _, err := authenticatedClient()
	if err != nil {
		return TestAuthResponse{Authenticated: false, Message: err.Error()}, nil
	}
	if err := client.Authenticate(); err != nil {
		return TestAuthResponse{Authenticated: false, Message: err.Error()}, nil
	}
	return TestAuthResponse{Authenticated: true, Message: "authenticated"}, nil
}

type ParseURLRequest struct {
	URL string `json:"url"`
}

type ParseURLResponse struct {
	Platform string `json:"platform"`
	LinkType string `json:"link_type"`
	ID       int    `json:"id"`
}

func parseURLHandler(ctx context.Context, args ParseURLRequest) (ParseURLResponse, error) {
	if strings.TrimSpace(args.URL) == "" {
		return ParseURLResponse{}, fmt.Errorf("url is required")
	}
	link, err := beatport.ParseLink(args.URL)
	if err != nil {
		return ParseURLResponse{}, err
	}
	return ParseURLResponse{
		Platform: link.Platform,
		LinkType: link.Type.String(),
		ID:       link.ID,
	}, nil
}

type GenresResponse struct {
	Genres []GenreItem `json:"genres"`
}

type GenreItem struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func genresHandler(ctx context.Context) (GenresResponse, error) {
	client, _, err := authenticatedClient()
	if err != nil {
		return GenresResponse{}, err
	}
	genres, err := client.GetGenres(ctx)
	if err != nil {
		return GenresResponse{}, err
	}
	items := make([]GenreItem, 0, len(genres))
	for _, g := range genres {
		items = append(items, GenreItem{ID: g.ID, Name: g.Name, Slug: g.Slug})
	}
	return GenresResponse{Genres: items}, nil
}

type SearchRequest struct {
	Query          string `json:"query,omitempty"`
	SearchType     string `json:"search_type,omitempty"`
	Page           int    `json:"page,omitempty"`
	PerPage        int    `json:"per_page,omitempty"`
	GenreID        int    `json:"genre_id,omitempty"`
	IncludeArtists bool   `json:"include_artists,omitempty"`
	TopTracks      bool   `json:"top_tracks,omitempty"`
}

type SearchResponse struct {
	Query      string        `json:"query,omitempty"`
	SearchType string        `json:"search_type"`
	Tracks     []TrackItem   `json:"tracks,omitempty"`
	Artists    []ArtistItem  `json:"artists,omitempty"`
	Releases   []ReleaseItem `json:"releases,omitempty"`
	Labels     []LabelItem   `json:"labels,omitempty"`
	Charts     []ChartItem   `json:"charts,omitempty"`
}

type TrackItem struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Artists  string `json:"artists"`
	Genre    string `json:"genre,omitempty"`
	Label    string `json:"label,omitempty"`
	BPM      int    `json:"bpm,omitempty"`
	Key      string `json:"key,omitempty"`
	Camelot  string `json:"camelot,omitempty"`
	Length   string `json:"length,omitempty"`
	Released string `json:"released,omitempty"`
	ImageURI string `json:"image_uri,omitempty"`
	URL      string `json:"url"`
}

type ArtistItem struct {
	ID        int         `json:"id"`
	Name      string      `json:"name"`
	ImageURI  string      `json:"image_uri,omitempty"`
	URL       string      `json:"url"`
	TopTracks []TrackItem `json:"top_tracks,omitempty"`
}

type ReleaseItem struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Artists    string `json:"artists,omitempty"`
	Label      string `json:"label,omitempty"`
	TrackCount int    `json:"track_count,omitempty"`
	Released   string `json:"released,omitempty"`
	ImageURI   string `json:"image_uri,omitempty"`
	URL        string `json:"url"`
}

type LabelItem struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	ImageURI string `json:"image_uri,omitempty"`
	URL      string `json:"url"`
}

type ChartItem struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Curator   string `json:"curator,omitempty"`
	Genre     string `json:"genre,omitempty"`
	Published string `json:"published,omitempty"`
	ImageURI  string `json:"image_uri,omitempty"`
	URL       string `json:"url"`
}

func searchHandler(ctx context.Context, args SearchRequest) (SearchResponse, error) {
	searchType := strings.TrimSpace(strings.ToLower(args.SearchType))
	if searchType == "" {
		searchType = "tracks"
	}
	validTypes := []string{"all", "tracks", "artists", "releases", "labels", "charts"}
	if !slices.Contains(validTypes, searchType) {
		return SearchResponse{}, fmt.Errorf("search_type must be one of: %s", strings.Join(validTypes, ", "))
	}

	query := strings.TrimSpace(args.Query)
	if query == "" && args.GenreID <= 0 {
		return SearchResponse{}, fmt.Errorf("query or genre_id is required")
	}

	page := args.Page
	if page < 1 {
		page = 1
	}
	perPage := args.PerPage
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 100 {
		perPage = 100
	}

	client, cfg, err := authenticatedClient()
	if err != nil {
		return SearchResponse{}, err
	}

	resp := SearchResponse{
		Query:      query,
		SearchType: searchType,
	}

	switch searchType {
	case "all":
		results, err := client.SearchCombined(ctx, query, page, perPage, args.GenreID)
		if err != nil {
			return SearchResponse{}, err
		}
		for _, t := range results.Tracks {
			resp.Tracks = append(resp.Tracks, trackToItem(t))
		}
		for _, a := range results.Artists {
			item := artistToItem(a)
			if args.TopTracks {
				top, topErr := client.GetArtistTopTracks(ctx, a.ID, 10)
				if topErr == nil {
					for _, tt := range top {
						item.TopTracks = append(item.TopTracks, trackToItem(tt))
					}
				}
			}
			resp.Artists = append(resp.Artists, item)
		}
		for _, r := range results.Releases {
			resp.Releases = append(resp.Releases, releaseToItem(r))
		}
		for _, l := range results.Labels {
			resp.Labels = append(resp.Labels, labelToItem(l))
		}
		for _, c := range results.Charts {
			resp.Charts = append(resp.Charts, chartToItem(c))
		}
	case "tracks":
		results, err := client.SearchTracks(ctx, query, page, perPage, args.GenreID)
		if err != nil {
			return SearchResponse{}, err
		}
		for _, t := range results.Results {
			resp.Tracks = append(resp.Tracks, trackToItem(t))
		}
		if args.IncludeArtists {
			artists, err := client.SearchArtists(ctx, query, page, min(perPage, cfg.SearchLimitArtists), args.GenreID)
			if err == nil {
				for _, a := range artists.Results {
					item := artistToItem(a)
					if args.TopTracks {
						top, topErr := client.GetArtistTopTracks(ctx, a.ID, 10)
						if topErr == nil {
							for _, tt := range top {
								item.TopTracks = append(item.TopTracks, trackToItem(tt))
							}
						}
					}
					resp.Artists = append(resp.Artists, item)
				}
			}
		}
	case "artists":
		results, err := client.SearchArtists(ctx, query, page, perPage, args.GenreID)
		if err != nil {
			return SearchResponse{}, err
		}
		for _, a := range results.Results {
			item := artistToItem(a)
			if args.TopTracks {
				top, topErr := client.GetArtistTopTracks(ctx, a.ID, 10)
				if topErr == nil {
					for _, tt := range top {
						item.TopTracks = append(item.TopTracks, trackToItem(tt))
					}
				}
			}
			resp.Artists = append(resp.Artists, item)
		}
	case "releases":
		results, err := client.SearchReleases(ctx, query, page, perPage, args.GenreID)
		if err != nil {
			return SearchResponse{}, err
		}
		for _, r := range results.Results {
			resp.Releases = append(resp.Releases, releaseToItem(r))
		}
	case "labels":
		results, err := client.SearchLabels(ctx, query, page, perPage, args.GenreID)
		if err != nil {
			return SearchResponse{}, err
		}
		for _, l := range results.Results {
			resp.Labels = append(resp.Labels, labelToItem(l))
		}
	case "charts":
		results, err := client.SearchCharts(ctx, query, page, perPage, args.GenreID)
		if err != nil {
			return SearchResponse{}, err
		}
		for _, c := range results.Results {
			resp.Charts = append(resp.Charts, chartToItem(c))
		}
	}

	return resp, nil
}

type DownloadRequest struct {
	URL           string `json:"url"`
	Quality       string `json:"quality,omitempty"`
	OutputDir     string `json:"output_dir,omitempty"`
	CreateSubdirs bool   `json:"create_subdirs,omitempty"`
	SaveCover     bool   `json:"save_cover,omitempty"`
	EmbedCover    bool   `json:"embed_cover,omitempty"`
}

type DownloadResponse struct {
	URL              string            `json:"url"`
	Collection       string            `json:"collection"`
	OutputDir        string            `json:"output_dir"`
	Total            int               `json:"total"`
	Downloaded       int               `json:"downloaded"`
	Failed           int               `json:"failed"`
	DownloadedTracks []DownloadedTrack `json:"downloaded_tracks,omitempty"`
	FailedTracks     []FailedTrack     `json:"failed_tracks,omitempty"`
}

type DownloadedTrack struct {
	TrackID  int    `json:"track_id"`
	Title    string `json:"title"`
	Artists  string `json:"artists"`
	Quality  string `json:"quality"`
	FilePath string `json:"file_path"`
	Warning  string `json:"warning,omitempty"`
}

type FailedTrack struct {
	TrackID int    `json:"track_id"`
	Title   string `json:"title"`
	Artists string `json:"artists"`
	Error   string `json:"error"`
}

func downloadHandler(ctx context.Context, args DownloadRequest) (DownloadResponse, error) {
	rawURL := strings.TrimSpace(args.URL)
	if rawURL == "" {
		return DownloadResponse{}, fmt.Errorf("url is required")
	}

	client, cfg, err := authenticatedClient()
	if err != nil {
		return DownloadResponse{}, err
	}

	link, err := beatport.ParseLink(rawURL)
	if err != nil {
		return DownloadResponse{}, fmt.Errorf("invalid URL: %w", err)
	}

	tracks, collection, err := resolveTracksFromLink(ctx, client, link)
	if err != nil {
		return DownloadResponse{}, err
	}
	if len(tracks) == 0 {
		return DownloadResponse{}, fmt.Errorf("no tracks found")
	}

	runCfg := *cfg
	if args.Quality != "" {
		runCfg.Quality = args.Quality
	}
	if args.OutputDir != "" {
		runCfg.OutputDir = args.OutputDir
	}
	if args.CreateSubdirs {
		runCfg.CreateSubdirs = true
	}
	if args.SaveCover {
		runCfg.SaveCover = true
	}
	if args.EmbedCover {
		runCfg.EmbedCover = true
	}

	outputDir := runCfg.OutputDir
	if outputDir == "" {
		outputDir = defaultOutputDir()
	}
	if runCfg.CreateSubdirs && collection != "" && link.Type != beatport.LinkTypeTrack {
		outputDir = filepath.Join(outputDir, beatport.SanitizePath(collection))
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return DownloadResponse{}, fmt.Errorf("failed to create output directory: %w", err)
	}

	result := DownloadResponse{
		URL:        rawURL,
		Collection: collection,
		OutputDir:  outputDir,
		Total:      len(tracks),
	}

	for _, track := range tracks {
		filePath, quality, warning, err := downloadTrack(client, &track, &runCfg, outputDir)
		if err != nil {
			result.Failed++
			result.FailedTracks = append(result.FailedTracks, FailedTrack{
				TrackID: track.ID,
				Title:   track.FullTitle(),
				Artists: beatport.ArtistNames(track.Artists),
				Error:   err.Error(),
			})
			continue
		}

		result.Downloaded++
		result.DownloadedTracks = append(result.DownloadedTracks, DownloadedTrack{
			TrackID:  track.ID,
			Title:    track.FullTitle(),
			Artists:  beatport.ArtistNames(track.Artists),
			Quality:  quality,
			FilePath: filePath,
			Warning:  warning,
		})
	}

	return result, nil
}

func resolveTracksFromLink(ctx context.Context, client *beatport.Client, link *beatport.ParsedLink) ([]beatport.Track, string, error) {
	switch link.Type {
	case beatport.LinkTypeTrack:
		track, err := client.GetTrack(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load track: %w", err)
		}
		collection := beatport.ArtistNames(track.Artists) + " - " + track.FullTitle()
		return []beatport.Track{*track}, collection, nil
	case beatport.LinkTypeRelease:
		release, err := client.GetRelease(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load release: %w", err)
		}
		tracks, err := client.GetReleaseTracks(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load release tracks: %w", err)
		}
		collection := beatport.ArtistNames(release.Artists) + " - " + release.Name
		return tracks, collection, nil
	case beatport.LinkTypePlaylist:
		playlist, err := client.GetPlaylist(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load playlist: %w", err)
		}
		tracks, err := client.GetPlaylistTracks(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load playlist tracks: %w", err)
		}
		return tracks, playlist.Name, nil
	case beatport.LinkTypeChart:
		chart, err := client.GetChart(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load chart: %w", err)
		}
		tracks, err := client.GetChartTracks(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load chart tracks: %w", err)
		}
		return tracks, chart.Name, nil
	case beatport.LinkTypeArtist:
		artist, err := client.GetArtist(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load artist: %w", err)
		}
		tracks, err := client.GetArtistTracks(link.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load artist tracks: %w", err)
		}
		return tracks, artist.Name, nil
	default:
		return nil, "", fmt.Errorf("unsupported URL type: %s", link.Type.String())
	}
}

func downloadTrack(client *beatport.Client, track *beatport.Track, cfg *config.Config, outputDir string) (string, string, string, error) {
	qualities := qualityFallbackChain(cfg.Quality)
	var downloadURL, chosenQuality string

	for _, q := range qualities {
		dl, err := client.GetTrackDownload(track.ID, q)
		if err != nil {
			if isAccessError(err) {
				continue
			}
			return "", "", "", fmt.Errorf("failed to get download URL: %w", err)
		}
		downloadURL = dl.Location
		chosenQuality = q
		break
	}

	if downloadURL == "" {
		return "", "", "", fmt.Errorf("track not available at any quality (%s)", strings.Join(qualities, ", "))
	}

	destPath := filepath.Join(outputDir, track.Filename(chosenQuality))
	if _, err := os.Stat(destPath); err == nil {
		return destPath, chosenQuality, "file already existed; skipped download", nil
	}

	tmpPath := destPath + ".part"
	if err := client.DownloadFile(downloadURL, tmpPath, nil); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", "", fmt.Errorf("download failed: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", "", fmt.Errorf("failed to finalize downloaded file: %w", err)
	}

	metadataWarning := ""
	meta := beatport.BuildMetadata(track)
	if cfg.EmbedCover || cfg.SaveCover {
		imageURL := track.Release.Image.DynamicURI
		if imageURL == "" {
			imageURL = track.Release.Image.URI
		}
		if imageURL == "" {
			imageURL = track.Image.DynamicURI
		}
		if imageURL != "" {
			coverPath := filepath.Join(outputDir, fmt.Sprintf(".cover_%d.jpg", track.ID))
			if err := client.DownloadCover(imageURL, coverPath); err == nil {
				if cfg.EmbedCover {
					meta.CoverPath = coverPath
				}
				if cfg.SaveCover {
					_ = copyFile(coverPath, filepath.Join(outputDir, "cover.jpg"))
				}
				defer os.Remove(coverPath)
			}
		}
	}
	if err := beatport.WriteMetadata(destPath, meta); err != nil {
		metadataWarning = "downloaded, but metadata embedding failed: " + err.Error()
	}

	return destPath, chosenQuality, metadataWarning, nil
}

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

func isAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "403") || strings.Contains(msg, "404") || strings.Contains(msg, "No Track matches")
}

func authenticatedClient() (*beatport.Client, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}
	if strings.TrimSpace(cfg.Username) == "" || strings.TrimSpace(cfg.Password) == "" {
		return nil, nil, fmt.Errorf("missing credentials in %s", config.ConfigPath())
	}

	client := beatport.NewClient(cfg.Username, cfg.Password, credentialsDir())
	if err := client.Authenticate(); err != nil {
		return nil, nil, fmt.Errorf("authentication failed: %w", err)
	}
	return client, cfg, nil
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
	_ = os.MkdirAll(path, 0700)
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

	_, err = out.ReadFrom(in)
	return err
}

func trackToItem(t beatport.Track) TrackItem {
	item := TrackItem{
		ID:       t.ID,
		Title:    t.FullTitle(),
		Artists:  beatport.ArtistNames(t.Artists),
		Genre:    t.Genre.Name,
		BPM:      t.BPM,
		Length:   t.Length,
		Released: t.NewReleaseDate,
		URL:      fmt.Sprintf("https://www.beatport.com/track/%s/%d", t.Slug, t.ID),
	}
	if t.Key != nil {
		item.Key = t.Key.Name
		item.Camelot = t.Key.CamelotCode()
	}
	if t.Release.Label.Name != "" {
		item.Label = t.Release.Label.Name
	}
	if t.Image.URI != "" {
		item.ImageURI = t.Image.URI
	}
	return item
}

func artistToItem(a beatport.Artist) ArtistItem {
	item := ArtistItem{
		ID:   a.ID,
		Name: a.Name,
		URL:  fmt.Sprintf("https://www.beatport.com/artist/%s/%d", a.Slug, a.ID),
	}
	if a.Image.URI != "" {
		item.ImageURI = a.Image.URI
	}
	return item
}

func releaseToItem(r beatport.Release) ReleaseItem {
	item := ReleaseItem{
		ID:         r.ID,
		Title:      r.Name,
		Artists:    beatport.ArtistNames(r.Artists),
		Label:      r.Label.Name,
		TrackCount: r.TrackCount,
		Released:   r.NewReleaseDate,
		URL:        fmt.Sprintf("https://www.beatport.com/release/%s/%d", r.Slug, r.ID),
	}
	if r.Image.URI != "" {
		item.ImageURI = r.Image.URI
	}
	return item
}

func labelToItem(l beatport.Label) LabelItem {
	item := LabelItem{
		ID:   l.ID,
		Name: l.Name,
		URL:  fmt.Sprintf("https://www.beatport.com/label/%s/%d", l.Slug, l.ID),
	}
	if l.Image.URI != "" {
		item.ImageURI = l.Image.URI
	}
	return item
}

func chartToItem(c beatport.Chart) ChartItem {
	item := ChartItem{
		ID:        c.ID,
		Name:      c.Name,
		Published: c.PublishDate,
		URL:       fmt.Sprintf("https://www.beatport.com/chart/%s/%d", c.Slug, c.ID),
	}
	if c.Person != nil {
		item.Curator = c.Person.Name
		if c.Person.Image.URI != "" {
			item.ImageURI = c.Person.Image.URI
		}
	}
	if c.Genre != nil {
		item.Genre = c.Genre.Name
	}
	return item
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
