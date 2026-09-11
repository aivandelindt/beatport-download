package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var audioExts = map[string]bool{
	".flac": true,
	".m4a":  true,
	".mp3":  true,
	".wav":  true,
	".ogg":  true,
	".aac":  true,
}

// ListAudioFiles returns a single file if path is a file, or non-recursive
// directory entries with known audio extensions.
func ListAudioFiles(ctx context.Context, path string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("path required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(path))
		if !audioExts[ext] {
			return nil, fmt.Errorf("not an audio file: %s", path)
		}
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !audioExts[ext] {
			continue
		}
		out = append(out, filepath.Join(path, name))
	}
	return out, nil
}
