package audio_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"beatportdl-ui/internal/audio"
)

func TestListAudioFiles_DirectoryNonRecursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.flac"), "x")
	mustWrite(t, filepath.Join(dir, "b.txt"), "x")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sub, "c.mp3"), "x")

	got, err := audio.ListAudioFiles(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "a.flac" {
		t.Fatalf("got %v", got)
	}
}

func TestListAudioFiles_FilePassthrough(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "track.m4a")
	mustWrite(t, path, "x")
	got, err := audio.ListAudioFiles(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != path {
		t.Fatalf("got %v", got)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
