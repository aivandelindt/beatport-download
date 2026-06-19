package beatport

import (
	"strings"
	"fmt"
	"time"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

type Credentials struct {
	LoginID      string    `json:"login_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Image struct {
	ID        int    `json:"id"`
	URI       string `json:"uri"`
	DynamicURI string `json:"dynamic_uri"`
}

type Artist struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Image Image  `json:"image,omitempty"`
}

type SearchType string

const (
	SearchTypeTracks   SearchType = "tracks"
	SearchTypeArtists  SearchType = "artists"
	SearchTypeReleases SearchType = "releases"
	SearchTypeLabels   SearchType = "labels"
	SearchTypeCharts   SearchType = "charts"
)

type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Key struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	CamelotNumber  int    `json:"camelot_number"`
	CamelotLetter  string `json:"camelot_letter"`
	ShortName      string `json:"short_name,omitempty"`
}

func (k *Key) CamelotCode() string {
	if k == nil || k.CamelotNumber <= 0 {
		return ""
	}
	letter := strings.ToUpper(k.CamelotLetter)
	if letter != "A" && letter != "B" {
		letter = "A"
	}
	return fmt.Sprintf("%d%s", k.CamelotNumber, letter)
}

type Label struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Image Image  `json:"image"`
}

type Release struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Artists     []Artist `json:"artists"`
	Label       Label    `json:"label"`
	Image       Image    `json:"image"`
	CatalogNumber string `json:"catalog_number"`
	NewReleaseDate string `json:"new_release_date"`
	TrackCount  int      `json:"track_count,omitempty"`
	IsAvailableForStreaming bool `json:"is_available_for_streaming"`
}

type Track struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	MixName     string   `json:"mix_name"`
	Artists     []Artist `json:"artists"`
	Remixers    []Artist `json:"remixers"`
	Release     Release  `json:"release"`
	Genre       Genre    `json:"genre"`
	SubGenre    *Genre   `json:"sub_genre"`
	Key         *Key     `json:"key"`
	BPM         int      `json:"bpm"`
	Length      string   `json:"length"`
	LengthMs    int      `json:"length_ms"`
	Number      int      `json:"number"`
	ISRC        string   `json:"isrc"`
	PublishDate string   `json:"publish_date"`
	NewReleaseDate string `json:"new_release_date"`
	IsAvailableForStreaming bool `json:"is_available_for_streaming"`
	Image       Image    `json:"image"`
}

type DownloadResponse struct {
	Location      string `json:"location"`
	StreamQuality string `json:"stream_quality"`
}

type StreamResponse struct {
	StreamURL     string `json:"stream_url"`
	SampleStartMs int    `json:"sample_start_ms"`
	SampleEndMs   int    `json:"sample_end_ms"`
}

type Playlist struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TrackCount  int    `json:"track_count"`
	IsPublished bool   `json:"is_published"`
}

type Chart struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description string   `json:"description"`
	Person      *Artist  `json:"person"`
	Genre       *Genre   `json:"genre"`
	PublishDate string   `json:"publish_date"`
}

type Paginated[T any] struct {
	Count    int    `json:"count"`
	Next     string `json:"next"`
	Previous string `json:"previous"`
	Results  []T    `json:"results"`
}

// PlaylistItem wraps a track as returned by the playlist/chart tracks endpoints.
// Beatport returns {id, position, track: {...}} not a flat Track.
type PlaylistItem struct {
	ID       int   `json:"id"`
	Position int   `json:"position"`
	Track    Track `json:"track"`
}

type SearchResults struct {
	Tracks   []Track   `json:"tracks"`
	Releases []Release `json:"releases"`
	Artists  []Artist  `json:"artists"`
	Labels   []Label   `json:"labels"`
	Charts   []Chart   `json:"charts"`
}

type LinkType int

const (
	LinkTypeTrack LinkType = iota
	LinkTypeRelease
	LinkTypePlaylist
	LinkTypeChart
	LinkTypeLabel
	LinkTypeArtist
)

type ParsedLink struct {
	Type     LinkType
	ID       int
	Platform string // "beatport" or "beatsource"
}

func ArtistNames(artists []Artist) string {
	if len(artists) == 0 {
		return ""
	}
	names := artists[0].Name
	for i := 1; i < len(artists); i++ {
		names += ", " + artists[i].Name
	}
	return names
}

func (t Track) FullTitle() string {
	title := t.Name
	if t.MixName != "" && t.MixName != "Original Mix" {
		// Don't double-append if the mix name is already embedded in the track name
		if !strings.Contains(strings.ToLower(title), strings.ToLower(t.MixName)) {
			title += " (" + t.MixName + ")"
		}
	}
	return title
}

func (t Track) Filename(quality string) string {
	ext := ".m4a"
	if quality == "lossless" {
		ext = ".flac"
	}
	artists := ArtistNames(t.Artists)
	return sanitizeFilename(artists + " - " + t.FullTitle() + ext)
}
