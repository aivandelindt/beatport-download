package audio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const loudnormFixture = `
[Parsed_loudnorm_0 @ 0x0] 
{
	"input_i" : "-18.45",
	"input_tp" : "-2.10",
	"input_lra" : "7.20",
	"input_thresh" : "-28.50",
	"output_i" : "-14.00",
	"output_tp" : "-1.50",
	"output_lra" : "7.20",
	"output_thresh" : "-24.10",
	"normalization_type" : "dynamic",
	"target_offset" : "0.50"
}
`

func TestParseLoudnormJSON(t *testing.T) {
	t.Parallel()
	m, err := ParseLoudnormJSON(loudnormFixture)
	if err != nil {
		t.Fatal(err)
	}
	if m.InputI != -18.45 {
		t.Fatalf("input_i: %v", m.InputI)
	}
	if m.InputTP != -2.10 {
		t.Fatalf("input_tp: %v", m.InputTP)
	}
}

func TestSidecarNormalizedPath(t *testing.T) {
	t.Parallel()
	got := SidecarNormalizedPath("/music/foo.flac")
	if got != "/music/foo_normalized.flac" {
		t.Fatalf("got %s", got)
	}
}

func TestNormalize_OverwriteDisabledWhenExists(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "foo.flac")
	out := SidecarNormalizedPath(in)
	if err := os.WriteFile(in, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Normalize(context.Background(), NormalizeRequest{
		Input:     in,
		Overwrite: false,
	})
	if err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("expected overwrite error, got %v", err)
	}
}

func TestNormalize_TwoPassWithFakeFFmpeg(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "foo.flac")
	if err := os.WriteFile(in, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	origCmd := runCmd
	origLook := lookPath
	t.Cleanup(func() {
		runCmd = origCmd
		lookPath = origLook
	})
	lookPath = func(file string) (string, error) {
		if file == "ffmpeg" {
			return "/usr/bin/ffmpeg", nil
		}
		return "", os.ErrNotExist
	}
	pass := 0
	runCmd = func(ctx context.Context, name string, args []string, env []string) ([]byte, []byte, error) {
		pass++
		if pass == 1 {
			return nil, []byte(loudnormFixture), nil
		}
		// pass 2: write empty output file
		for i, a := range args {
			if a == "-y" && i+1 < len(args) {
				// last arg is output
			}
		}
		out := args[len(args)-1]
		_ = os.WriteFile(out, []byte("normalized"), 0644)
		return nil, nil, nil
	}

	res, err := Normalize(context.Background(), NormalizeRequest{
		Input:      in,
		TargetLUFS: -14,
		TruePeak:   -1.5,
		LRA:        11,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.MeasuredI != -18.45 {
		t.Fatalf("measured: %+v", res)
	}
	if _, err := os.Stat(res.Output); err != nil {
		t.Fatal(err)
	}
}
