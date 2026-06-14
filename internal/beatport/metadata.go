package beatport

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type TrackMetadata struct {
	Artist      string
	Title       string
	Album       string
	AlbumArtist string
	Label       string
	Genre       string
	BPM         string
	Key         string
	TrackNumber string
	TotalTracks string
	ISRC        string
	Year        string
	CatalogNumber string
	CoverPath   string
}

func BuildMetadata(track *Track) TrackMetadata {
	meta := TrackMetadata{
		Artist:      ArtistNames(track.Artists),
		Title:       track.FullTitle(),
		Album:       track.Release.Name,
		AlbumArtist: ArtistNames(track.Release.Artists),
		Label:       track.Release.Label.Name,
		Genre:       track.Genre.Name,
		ISRC:        track.ISRC,
	}

	if track.BPM > 0 {
		meta.BPM = fmt.Sprintf("%d", track.BPM)
	}
	if track.Key != nil {
		meta.Key = track.Key.Name
	}
	if track.Number > 0 {
		meta.TrackNumber = fmt.Sprintf("%d", track.Number)
	}
	if track.Release.TrackCount > 0 {
		meta.TotalTracks = fmt.Sprintf("%d", track.Release.TrackCount)
	}
	if track.Release.CatalogNumber != "" {
		meta.CatalogNumber = track.Release.CatalogNumber
	}

	// Year from publish date
	if track.PublishDate != "" && len(track.PublishDate) >= 4 {
		meta.Year = track.PublishDate[:4]
	} else if track.NewReleaseDate != "" && len(track.NewReleaseDate) >= 4 {
		meta.Year = track.NewReleaseDate[:4]
	}

	return meta
}

// WriteMetadata uses ffmpeg to embed metadata and cover art into an audio file.
// Writes to a temp file, then replaces the original.
func WriteMetadata(filePath string, meta TrackMetadata) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH")
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	tmpPath := filePath + ".tmp" + ext

	args := []string{"-i", filePath}

	if meta.CoverPath != "" {
		if _, err := os.Stat(meta.CoverPath); err == nil {
			args = append(args, "-i", meta.CoverPath)
		}
	}

	args = append(args, "-y", "-codec", "copy")

	// Map streams
	if meta.CoverPath != "" {
		if _, err := os.Stat(meta.CoverPath); err == nil {
			args = append(args, "-map", "0:a", "-map", "1")
			if ext == ".m4a" || ext == ".aac" {
				args = append(args, "-c:v", "mjpeg")
				args = append(args, "-disposition:v:0", "attached_pic")
			} else if ext == ".flac" {
				args = append(args, "-c:v", "copy")
				args = append(args, "-metadata:s:v", "title=Album cover")
				args = append(args, "-metadata:s:v", "comment=Cover (front)")
			}
		}
	}

	// Metadata fields
	metaMap := map[string]string{
		"artist":         meta.Artist,
		"title":          meta.Title,
		"album":          meta.Album,
		"album_artist":   meta.AlbumArtist,
		"label":          meta.Label,
		"genre":          meta.Genre,
		"bpm":            meta.BPM,
		"key":            meta.Key,
		"track":          meta.TrackNumber,
		"isrc":           meta.ISRC,
		"date":           meta.Year,
		"catalog_number": meta.CatalogNumber,
	}

	for k, v := range metaMap {
		if v != "" {
			args = append(args, "-metadata", fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Handle track/total separately
	if meta.TrackNumber != "" && meta.TotalTracks != "" {
		// Override track with n/N format for id3
		if ext == ".mp3" {
			trackStr := meta.TrackNumber + "/" + meta.TotalTracks
			args = append(args, "-metadata", "track="+trackStr)
		}
	}

	args = append(args, tmpPath)

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to replace file: %w", err)
	}

	return nil
}

// FixMetadata strips track number prefixes from filenames and rewrites artist/title tags.
// Mirrors the logic in fix.sh and fix.bat.
func FixMetadata(dir string, progressFn func(msg string)) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".m4a" && ext != ".flac" {
			continue
		}

		originalPath := filepath.Join(dir, name)
		newName := name

		// Strip leading "01. ", "02. ", etc.
		if len(newName) > 4 {
			prefix := newName[:4]
			if len(prefix) == 4 && prefix[0] >= '0' && prefix[0] <= '9' &&
				prefix[1] >= '0' && prefix[1] <= '9' && prefix[2] == '.' && prefix[3] == ' ' {
				newName = strings.TrimSpace(newName[4:])
				if progressFn != nil {
					progressFn(fmt.Sprintf("[Stripped] %s → %s", name, newName))
				}
			}
		}

		// Rename if changed
		newPath := filepath.Join(dir, newName)
		if newPath != originalPath {
			if err := os.Rename(originalPath, newPath); err != nil {
				if progressFn != nil {
					progressFn(fmt.Sprintf("[ERROR] Rename failed: %s", err))
				}
				continue
			}
			originalPath = newPath
			if progressFn != nil {
				progressFn(fmt.Sprintf("[Renamed] → %s", newName))
			}
		}

		// Split at " - " to get artist and title
		noExt := strings.TrimSuffix(newName, ext)
		idx := strings.Index(noExt, " - ")
		if idx < 0 {
			if progressFn != nil {
				progressFn(fmt.Sprintf("[Skip] No ' - ' separator found: %s", newName))
			}
			continue
		}
		artist := strings.TrimSpace(noExt[:idx])
		title := strings.TrimSpace(noExt[idx+3:])

		if progressFn != nil {
			progressFn(fmt.Sprintf("[Tagging] %s | artist=%q title=%q", newName, artist, title))
		}

		tmpPath := originalPath + ".tmp" + ext
		cmd := exec.Command("ffmpeg", "-i", originalPath, "-y", "-codec", "copy",
			"-metadata", "artist="+artist,
			"-metadata", "title="+title,
			tmpPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			os.Remove(tmpPath)
			if progressFn != nil {
				progressFn(fmt.Sprintf("[ERROR] ffmpeg failed for %s: %s", newName, string(output)))
			}
			continue
		}

		if err := os.Rename(tmpPath, originalPath); err != nil {
			os.Remove(tmpPath)
			if progressFn != nil {
				progressFn(fmt.Sprintf("[ERROR] Replace failed: %s", err))
			}
			continue
		}

		if progressFn != nil {
			progressFn(fmt.Sprintf("[Done] %s", newName))
		}
	}

	return nil
}
