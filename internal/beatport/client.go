package beatport

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	beatportClientID  = "ryZ8LuyQVPqbK2mBX2Hwt4qSMtnWuTYSqBPO92yQ"
	beatportAPIBase   = "https://api.beatport.com/v4"
	beatsourceAPIBase = "https://api.beatsource.com/v4"
)

// downloadClient is used for all file downloads (audio, cover art).
// Uses a long timeout to handle large files, but won't hang forever.
var downloadClient = &http.Client{
	Timeout: 15 * time.Minute,
}

type Client struct {
	httpClient  *http.Client
	credentials *Credentials
	username    string
	password    string
	apiBase     string
	configDir   string
}

func NewClient(username, password, configDir string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		username:  username,
		password:  password,
		apiBase:   beatportAPIBase,
		configDir: configDir,
	}
}

func (c *Client) loginID() string {
	h := fnv.New64a()
	h.Write([]byte(c.username + ":" + c.password))
	return fmt.Sprintf("%x", h.Sum64())
}

func (c *Client) credentialsPath() string {
	return filepath.Join(c.configDir, "beatportdl-credentials.json")
}

func (c *Client) loadCachedCredentials() bool {
	data, err := os.ReadFile(c.credentialsPath())
	if err != nil {
		return false
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return false
	}
	if creds.LoginID != c.loginID() {
		return false
	}
	c.credentials = &creds
	return true
}

func (c *Client) saveCredentials() error {
	data, err := json.MarshalIndent(c.credentials, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.credentialsPath(), data, 0600)
}

func (c *Client) Authenticate() error {
	if c.loadCachedCredentials() {
		if time.Until(c.credentials.ExpiresAt) > 5*time.Minute {
			return nil
		}
		if err := c.refreshToken(); err == nil {
			return nil
		}
	}
	return c.fullAuth()
}

func (c *Client) fullAuth() error {
	// Step 1: Login to get session cookie
	loginBody := fmt.Sprintf(`{"username":"%s","password":"%s"}`, c.username, c.password)
	req, _ := http.NewRequest("POST", c.apiBase+"/auth/login/", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("login failed with status %d", resp.StatusCode)
	}

	// Step 2: Authorize to get code
	authURL := fmt.Sprintf("%s/auth/o/authorize/?client_id=%s&response_type=code", c.apiBase, beatportClientID)
	req, _ = http.NewRequest("GET", authURL, nil)
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("authorize request failed: %w", err)
	}
	resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		return fmt.Errorf("no redirect from authorize endpoint (status %d)", resp.StatusCode)
	}

	parsedURL, err := url.Parse(location)
	if err != nil {
		return fmt.Errorf("failed to parse redirect URL: %w", err)
	}
	code := parsedURL.Query().Get("code")
	if code == "" {
		return fmt.Errorf("no authorization code in redirect")
	}

	// Step 3: Exchange code for token
	tokenData := url.Values{
		"client_id":  {beatportClientID},
		"code":       {code},
		"grant_type": {"authorization_code"},
	}
	req, _ = http.NewRequest("POST", c.apiBase+"/auth/o/token/", strings.NewReader(tokenData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	c.credentials = &Credentials{
		LoginID:      c.loginID(),
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	return c.saveCredentials()
}

func (c *Client) refreshToken() error {
	tokenData := url.Values{
		"client_id":     {beatportClientID},
		"refresh_token": {c.credentials.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, _ := http.NewRequest("POST", c.apiBase+"/auth/o/token/", strings.NewReader(tokenData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("token refresh failed with status %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	c.credentials.AccessToken = tokenResp.AccessToken
	c.credentials.RefreshToken = tokenResp.RefreshToken
	c.credentials.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return c.saveCredentials()
}

func (c *Client) ensureToken() error {
	if c.credentials == nil {
		return c.fullAuth()
	}
	if time.Until(c.credentials.ExpiresAt) < 5*time.Minute {
		if err := c.refreshToken(); err != nil {
			return c.fullAuth()
		}
	}
	return nil
}

func (c *Client) apiRequest(method, path string, body io.Reader) (*http.Response, error) {
	return c.apiRequestContext(context.Background(), method, path, body)
}

func (c *Client) apiGet(path string, result interface{}) error {
	return c.apiGetContext(context.Background(), path, result)
}

func (c *Client) apiGetContext(ctx context.Context, path string, result interface{}) error {
	resp, err := c.apiRequestContext(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (c *Client) apiRequestContext(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if err := c.ensureToken(); err != nil {
		return nil, fmt.Errorf("auth failed: %w", err)
	}

	reqURL := c.apiBase + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.credentials.AccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 401 {
		resp.Body.Close()
		c.credentials = nil
		if err := c.fullAuth(); err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.credentials.AccessToken)
		return c.httpClient.Do(req)
	}

	return resp, nil
}

// Track methods

func (c *Client) GetTrack(id int) (*Track, error) {
	var track Track
	err := c.apiGet(fmt.Sprintf("/catalog/tracks/%d/", id), &track)
	return &track, err
}

func (c *Client) GetTrackDownload(id int, quality string) (*DownloadResponse, error) {
	var dl DownloadResponse
	err := c.apiGet(fmt.Sprintf("/catalog/tracks/%d/download/?quality=%s", id, quality), &dl)
	return &dl, err
}

func (c *Client) GetTrackStream(id int) (*StreamResponse, error) {
	var stream StreamResponse
	err := c.apiGet(fmt.Sprintf("/catalog/tracks/%d/stream/", id), &stream)
	return &stream, err
}

// Release methods

func (c *Client) GetRelease(id int) (*Release, error) {
	var release Release
	err := c.apiGet(fmt.Sprintf("/catalog/releases/%d/", id), &release)
	return &release, err
}

func (c *Client) GetReleaseTracks(id int) ([]Track, error) {
	return c.getAllTracks(fmt.Sprintf("/catalog/releases/%d/tracks/", id))
}

// Playlist methods

func (c *Client) GetPlaylist(id int) (*Playlist, error) {
	var playlist Playlist
	err := c.apiGet(fmt.Sprintf("/catalog/playlists/%d/", id), &playlist)
	return &playlist, err
}

func (c *Client) GetPlaylistTracks(id int) ([]Track, error) {
	return c.getAllPlaylistItems(fmt.Sprintf("/catalog/playlists/%d/tracks/", id))
}

// Chart methods

func (c *Client) GetChart(id int) (*Chart, error) {
	var chart Chart
	err := c.apiGet(fmt.Sprintf("/catalog/charts/%d/", id), &chart)
	return &chart, err
}

func (c *Client) GetChartTracks(id int) ([]Track, error) {
	return c.getAllPlaylistItems(fmt.Sprintf("/catalog/charts/%d/tracks/", id))
}

// Label methods

func (c *Client) GetLabel(id int) (*Label, error) {
	var label Label
	err := c.apiGet(fmt.Sprintf("/catalog/labels/%d/", id), &label)
	return &label, err
}

func (c *Client) GetLabelTracks(id int) ([]Track, error) {
	return c.getAllTracks(fmt.Sprintf("/catalog/labels/%d/releases/", id))
}

// Artist methods

func (c *Client) GetArtist(id int) (*Artist, error) {
	var artist Artist
	err := c.apiGet(fmt.Sprintf("/catalog/artists/%d/", id), &artist)
	return &artist, err
}

func (c *Client) GetArtistTopTracks(ctx context.Context, id int, perPage int) ([]Track, error) {
	if perPage < 1 {
		perPage = 10
	}
	if perPage > 100 {
		perPage = 100
	}

	var page Paginated[Track]
	err := c.apiGetContext(ctx, fmt.Sprintf("/catalog/artists/%d/tracks/?per_page=%d&page=1", id, perPage), &page)
	if err != nil {
		return nil, err
	}
	return page.Results, nil
}

func (c *Client) GetArtistTracks(id int) ([]Track, error) {
	return c.getAllTracks(fmt.Sprintf("/catalog/artists/%d/tracks/", id))
}

// Genre methods

func (c *Client) GetGenres(ctx context.Context) ([]Genre, error) {
	var all []Genre
	page := 1
	for {
		var paginated Paginated[Genre]
		err := c.apiGetContext(ctx, fmt.Sprintf("/catalog/genres/?page=%d&per_page=100", page), &paginated)
		if err != nil {
			return nil, err
		}
		all = append(all, paginated.Results...)
		if paginated.Next == "" || len(paginated.Results) == 0 {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) ListTracksByGenre(ctx context.Context, genreID, page, perPage int) (*Paginated[Track], error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 100 {
		perPage = 100
	}
	var result Paginated[Track]
	err := c.apiGetContext(ctx, fmt.Sprintf(
		"/catalog/tracks/?genre_id=%d&page=%d&per_page=%d&order_by=-publish_date",
		genreID, page, perPage,
	), &result)
	return &result, err
}

// Search

func (c *Client) Search(ctx context.Context, query string) (*SearchResults, error) {
	var results SearchResults
	err := c.apiGetContext(ctx, fmt.Sprintf("/catalog/search/?q=%s&order_by=-publish_date", url.QueryEscape(query)), &results)
	return &results, err
}

func (c *Client) SearchTyped(ctx context.Context, query string, searchType SearchType, page, perPage, genreID int) (*Paginated[json.RawMessage], error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 25
	}
	if perPage > 100 {
		perPage = 100
	}

	path := fmt.Sprintf(
		"/catalog/search/?q=%s&type=%s&page=%d&per_page=%d",
		url.QueryEscape(query), searchType, page, perPage,
	)
	if genreID > 0 {
		path += fmt.Sprintf("&genre_id=%d", genreID)
	}
	if searchType == SearchTypeTracks {
		path += "&order_by=-publish_date"
	}

	body, err := c.apiGetRawContext(ctx, path)
	if err != nil {
		return nil, err
	}
	return decodeTypedSearchPage(body, searchType)
}

func (c *Client) apiGetRawContext(ctx context.Context, path string) ([]byte, error) {
	resp, err := c.apiRequestContext(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func decodeTypedSearchPage(body []byte, searchType SearchType) (*Paginated[json.RawMessage], error) {
	var page struct {
		Count    int               `json:"count"`
		Next     string            `json:"next"`
		Previous string            `json:"previous"`
		Tracks   []json.RawMessage `json:"tracks"`
		Artists  []json.RawMessage `json:"artists"`
		Results  []json.RawMessage `json:"results"`
		Data     []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("decode search page: %w", err)
	}

	items := page.Results
	if len(items) == 0 {
		items = page.Data
	}
	if len(items) == 0 {
		switch searchType {
		case SearchTypeTracks:
			items = page.Tracks
		case SearchTypeArtists:
			items = page.Artists
		}
	}

	return &Paginated[json.RawMessage]{
		Count:    page.Count,
		Next:     page.Next,
		Previous: page.Previous,
		Results:  items,
	}, nil
}

func (c *Client) SearchTracks(ctx context.Context, query string, page, perPage, genreID int) (*Paginated[Track], error) {
	raw, err := c.SearchTyped(ctx, query, SearchTypeTracks, page, perPage, genreID)
	if err != nil {
		return nil, err
	}
	return decodeSearchItems[Track](raw)
}

func (c *Client) SearchArtists(ctx context.Context, query string, page, perPage, genreID int) (*Paginated[Artist], error) {
	raw, err := c.SearchTyped(ctx, query, SearchTypeArtists, page, perPage, genreID)
	if err != nil {
		return nil, err
	}
	return decodeSearchItems[Artist](raw)
}

func decodeSearchItems[T any](raw *Paginated[json.RawMessage]) (*Paginated[T], error) {
	out := &Paginated[T]{
		Count:    raw.Count,
		Next:     raw.Next,
		Previous: raw.Previous,
		Results:  make([]T, 0, len(raw.Results)),
	}
	for _, item := range raw.Results {
		var v T
		if err := json.Unmarshal(item, &v); err != nil {
			return nil, fmt.Errorf("decode search item: %w", err)
		}
		out.Results = append(out.Results, v)
	}
	return out, nil
}

// Helper to paginate playlist/chart items.
// Beatport wraps tracks as {track: {...}} in playlist/chart endpoints.
// On first page we detect which format the API uses; subsequent pages use the same format.
func (c *Client) getAllPlaylistItems(basePath string) ([]Track, error) {
	sep := "?"
	if strings.Contains(basePath, "?") {
		sep = "&"
	}

	// Probe page 1 with wrapped format
	var firstPage Paginated[PlaylistItem]
	if err := c.apiGet(fmt.Sprintf("%s%spage=1&per_page=100", basePath, sep), &firstPage); err != nil {
		return nil, err
	}

	// Count valid wrapped tracks
	var allTracks []Track
	wrappedCount := 0
	for _, item := range firstPage.Results {
		if item.Track.ID != 0 {
			allTracks = append(allTracks, item.Track)
			wrappedCount++
		}
	}

	// If zero wrapped tracks but results exist, fall back to flat format
	if wrappedCount == 0 && len(firstPage.Results) > 0 {
		return c.getAllTracks(basePath)
	}

	// Continue paginating with wrapped format
	if firstPage.Next == "" {
		return allTracks, nil
	}

	page := 2
	for {
		var paginated Paginated[PlaylistItem]
		if err := c.apiGet(fmt.Sprintf("%s%spage=%d&per_page=100", basePath, sep, page), &paginated); err != nil {
			// Return what we have rather than failing the whole job
			break
		}
		for _, item := range paginated.Results {
			if item.Track.ID != 0 {
				allTracks = append(allTracks, item.Track)
			}
		}
		if paginated.Next == "" || len(paginated.Results) == 0 {
			break
		}
		page++
	}
	return allTracks, nil
}

// Helper to paginate tracks

func (c *Client) getAllTracks(basePath string) ([]Track, error) {
	var allTracks []Track
	page := 1
	for {
		sep := "?"
		if strings.Contains(basePath, "?") {
			sep = "&"
		}
		var paginated Paginated[Track]
		err := c.apiGet(fmt.Sprintf("%s%spage=%d&per_page=100", basePath, sep, page), &paginated)
		if err != nil {
			return nil, err
		}
		allTracks = append(allTracks, paginated.Results...)
		if paginated.Next == "" || len(paginated.Results) == 0 {
			break
		}
		page++
	}
	return allTracks, nil
}

// Download a file from URL to disk with progress callback

func (c *Client) DownloadFile(downloadURL, destPath string, progressFn func(downloaded, total int64)) error {
	resp, err := downloadClient.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("cannot create file %s: %w", destPath, err)
	}

	total := resp.ContentLength
	var downloaded int64
	var lastPct int64 = -1

	buf := make([]byte, 32*1024)
	var writeErr error
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr = out.Write(buf[:n])
			if writeErr != nil {
				out.Close()
				os.Remove(destPath)
				return fmt.Errorf("write failed: %w", writeErr)
			}
			downloaded += int64(n)
			if progressFn != nil && total > 0 {
				pct := downloaded * 100 / total
				if pct != lastPct {
					lastPct = pct
					progressFn(downloaded, total)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			os.Remove(destPath)
			return fmt.Errorf("read failed after %d bytes: %w", downloaded, readErr)
		}
	}

	if err := out.Close(); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("file close failed: %w", err)
	}

	return nil
}

// Download cover art

func (c *Client) DownloadCover(imageURL, destPath string) error {
	if imageURL == "" {
		return nil
	}
	imageURL = strings.Replace(imageURL, "{w}x{h}", "1400x1400", 1)

	resp, err := downloadClient.Get(imageURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("cover download failed with status %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// HLS stream download (for medium-hls quality)

func (c *Client) DownloadHLSStream(streamURL, destPath string) error {
	// Fetch the master playlist
	resp, err := downloadClient.Get(streamURL)
	if err != nil {
		return fmt.Errorf("failed to fetch HLS playlist: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	content := string(body)
	baseURL := streamURL[:strings.LastIndex(streamURL, "/")+1]

	// Parse key and segments from m3u8
	var keyURL, ivHex string
	var segmentURLs []string

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			// Parse URI and IV
			if idx := strings.Index(line, "URI=\""); idx >= 0 {
				rest := line[idx+5:]
				endIdx := strings.Index(rest, "\"")
				keyURL = rest[:endIdx]
				if !strings.HasPrefix(keyURL, "http") {
					keyURL = baseURL + keyURL
				}
			}
			if idx := strings.Index(line, "IV=0x"); idx >= 0 {
				ivHex = line[idx+4:]
			}
		} else if !strings.HasPrefix(line, "#") && line != "" {
			segURL := line
			if !strings.HasPrefix(segURL, "http") {
				segURL = baseURL + segURL
			}
			segmentURLs = append(segmentURLs, segURL)
		}
	}

	if keyURL == "" || len(segmentURLs) == 0 {
		return fmt.Errorf("failed to parse HLS playlist")
	}

	// Fetch the AES key
	keyResp, err := downloadClient.Get(keyURL)
	if err != nil {
		return fmt.Errorf("failed to fetch HLS key: %w", err)
	}
	defer keyResp.Body.Close()
	keyBytes, err := io.ReadAll(keyResp.Body)
	if err != nil {
		return err
	}

	// Parse IV
	ivBytes, err := hex.DecodeString(strings.TrimPrefix(ivHex, "0x"))
	if err != nil {
		return fmt.Errorf("failed to parse IV: %w", err)
	}

	// Download and decrypt each segment
	dir := filepath.Dir(destPath)
	os.MkdirAll(dir, 0755)
	outFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	for _, segURL := range segmentURLs {
		segResp, err := downloadClient.Get(segURL)
		if err != nil {
			return fmt.Errorf("failed to download segment: %w", err)
		}
		segData, err := io.ReadAll(segResp.Body)
		segResp.Body.Close()
		if err != nil {
			return err
		}

		// Decrypt
		mode := cipher.NewCBCDecrypter(block, ivBytes)
		mode.CryptBlocks(segData, segData)

		// Remove PKCS7 padding
		if len(segData) > 0 {
			padLen := int(segData[len(segData)-1])
			if padLen <= aes.BlockSize && padLen <= len(segData) {
				segData = segData[:len(segData)-padLen]
			}
		}

		outFile.Write(segData)
	}

	return nil
}

// URL parsing

var linkPatterns = map[LinkType]*regexp.Regexp{
	LinkTypeTrack:    regexp.MustCompile(`/track/[^/]+/(\d+)`),
	LinkTypeRelease:  regexp.MustCompile(`/release/[^/]+/(\d+)`),
	LinkTypeChart:    regexp.MustCompile(`/chart/[^/]+/(\d+)`),
	LinkTypeLabel:    regexp.MustCompile(`/label/[^/]+/(\d+)`),
	LinkTypeArtist:   regexp.MustCompile(`/artist/[^/]+/(\d+)`),
}

// Broader patterns tried second — order matters, most specific first above
var altPatterns = map[LinkType]*regexp.Regexp{
	LinkTypeTrack:    regexp.MustCompile(`/tracks/(\d+)`),
	LinkTypeRelease:  regexp.MustCompile(`/releases/(\d+)`),
	// Matches: /playlists/123  /playlists/share/123  /library/playlists/123
	LinkTypePlaylist: regexp.MustCompile(`/playlists/(?:[^/\d][^/]*/)?(\d+)`),
	LinkTypeChart:    regexp.MustCompile(`/charts/(\d+)`),
	LinkTypeLabel:    regexp.MustCompile(`/labels/(\d+)`),
	LinkTypeArtist:   regexp.MustCompile(`/artists/(\d+)`),
}

func ParseLink(rawURL string) (*ParsedLink, error) {
	rawURL = strings.TrimSpace(rawURL)

	platform := "beatport"
	if strings.Contains(rawURL, "beatsource.com") {
		platform = "beatsource"
	}

	for linkType, pattern := range linkPatterns {
		matches := pattern.FindStringSubmatch(rawURL)
		if len(matches) >= 2 {
			id, _ := strconv.Atoi(matches[1])
			return &ParsedLink{Type: linkType, ID: id, Platform: platform}, nil
		}
	}

	for linkType, pattern := range altPatterns {
		matches := pattern.FindStringSubmatch(rawURL)
		if len(matches) >= 2 {
			id, _ := strconv.Atoi(matches[1])
			return &ParsedLink{Type: linkType, ID: id, Platform: platform}, nil
		}
	}

	return nil, fmt.Errorf("unsupported URL format: %s", rawURL)
}

func (l LinkType) String() string {
	switch l {
	case LinkTypeTrack:
		return "track"
	case LinkTypeRelease:
		return "release"
	case LinkTypePlaylist:
		return "playlist"
	case LinkTypeChart:
		return "chart"
	case LinkTypeLabel:
		return "label"
	case LinkTypeArtist:
		return "artist"
	default:
		return "unknown"
	}
}
