package audio

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultStemOutputDir(t *testing.T) {
	t.Parallel()
	got := DefaultStemOutputDir("/music/track.flac")
	want := filepath.Join("/music", "track_stems")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestStemEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		wantKey  string
		wantVal  string
	}{
		{"coreml", "STEMMER_EP_FORCE", "coreml"},
		{"xnnpack", "STEMMER_EP_FORCE", "xnnpack"},
		{"cpu", "STEMMER_FORCE_CPU", "1"},
	}
	for _, tc := range cases {
		env := StemEnv(tc.provider, []string{"PATH=/bin"})
		found := false
		for _, e := range env {
			if e == tc.wantKey+"="+tc.wantVal {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: env %v missing %s=%s", tc.provider, env, tc.wantKey, tc.wantVal)
		}
	}
	auto := StemEnv("auto", []string{"PATH=/bin"})
	for _, e := range auto {
		if strings.HasPrefix(e, "STEMMER_EP_FORCE=") || strings.HasPrefix(e, "STEMMER_FORCE_CPU=") {
			t.Fatalf("auto should not force provider: %v", auto)
		}
	}
}

func TestParseStemQuietOutput(t *testing.T) {
	t.Parallel()
	out := "/a/vocals.wav\n/a/drums.wav\n/a/bass.wav\n/a/other.wav\n"
	r, err := ParseStemQuietOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	if r.Vocals != "/a/vocals.wav" || r.Other != "/a/other.wav" {
		t.Fatalf("%+v", r)
	}
	_, err = ParseStemQuietOutput("one\ntwo\n")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultStemProvider(t *testing.T) {
	t.Parallel()
	p := DefaultStemProvider()
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if p != "coreml" {
			t.Fatalf("got %s", p)
		}
	} else if p != "auto" {
		t.Fatalf("got %s", p)
	}
}

func TestSplitStems_MissingBinary(t *testing.T) {
	origStat := lookPathStat
	origLook := lookPath
	t.Cleanup(func() {
		lookPathStat = origStat
		lookPath = origLook
	})
	lookPathStat = func(path string) (bool, error) { return false, os.ErrNotExist }
	lookPath = func(file string) (string, error) { return "", os.ErrNotExist }

	_, err := SplitStems(context.Background(), StemRequest{Input: "/x.mp3"})
	if err == nil || !strings.Contains(err.Error(), "stem-splitter") {
		t.Fatalf("got %v", err)
	}
}

func TestSplitStems_QuietParse(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(in, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	origCmd := runCmd
	origStat := lookPathStat
	t.Cleanup(func() {
		runCmd = origCmd
		lookPathStat = origStat
	})
	lookPathStat = func(path string) (bool, error) {
		if path == "/fake/stem-splitter" {
			return true, nil
		}
		return false, os.ErrNotExist
	}
	runCmd = func(ctx context.Context, name string, args []string, env []string) ([]byte, []byte, error) {
		outDir := ""
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				outDir = args[i+1]
			}
		}
		stdout := strings.Join([]string{
			filepath.Join(outDir, "vocals.wav"),
			filepath.Join(outDir, "drums.wav"),
			filepath.Join(outDir, "bass.wav"),
			filepath.Join(outDir, "other.wav"),
		}, "\n")
		return []byte(stdout), nil, nil
	}
	res, err := SplitStems(context.Background(), StemRequest{
		Input:   in,
		BinPath: "/fake/stem-splitter",
		Provider: "cpu",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Vocals, "vocals.wav") {
		t.Fatalf("%+v", res)
	}
}
