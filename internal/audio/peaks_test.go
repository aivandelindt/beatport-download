package audio

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBucketPeaks_Synthetic(t *testing.T) {
	t.Parallel()
	// Ramp: first half negative, second half positive.
	samples := make([]float32, 100)
	for i := range samples {
		if i < 50 {
			samples[i] = -0.5
		} else {
			samples[i] = 0.8
		}
	}
	peaks := bucketPeaks(samples, 4)
	if len(peaks) != 8 {
		t.Fatalf("want 8 values, got %d", len(peaks))
	}
	// Normalized so peak abs 0.8 → 1.0
	const eps = 1e-5
	if math.Abs(peaks[0]-(-0.5/0.8)) > eps || math.Abs(peaks[1]-(-0.5/0.8)) > eps {
		t.Fatalf("bucket0: %v %v", peaks[0], peaks[1])
	}
	if math.Abs(peaks[6]-(1.0)) > eps || math.Abs(peaks[7]-(1.0)) > eps {
		t.Fatalf("bucket3: %v %v", peaks[6], peaks[7])
	}
}

func TestBucketPeaks_Empty(t *testing.T) {
	t.Parallel()
	if bucketPeaks(nil, 10) != nil {
		t.Fatal("expected nil")
	}
	if bucketPeaks([]float32{1}, 0) != nil {
		t.Fatal("expected nil for zero buckets")
	}
}

func TestComputePeaks_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := ComputePeaks(context.Background(), filepath.Join(t.TempDir(), "nope.wav"), 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestComputePeaks_TinyWav(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	wav := filepath.Join(dir, "tone.wav")
	// Generate 0.25s sine via ffmpeg
	cmd := exec.Command("ffmpeg", "-nostdin", "-y", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=0.25",
		"-ac", "1", wav)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate wav: %v: %s", err, out)
	}
	fi, err := os.Stat(wav)
	if err != nil || fi.Size() == 0 {
		t.Fatal("wav missing")
	}
	p, err := ComputePeaks(context.Background(), wav, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Peaks) != 100 {
		t.Fatalf("want 100 peak values, got %d", len(p.Peaks))
	}
	if p.DurationSec < 0.2 || p.DurationSec > 0.35 {
		t.Fatalf("duration %v", p.DurationSec)
	}
	var maxAbs float64
	for _, v := range p.Peaks {
		if a := math.Abs(v); a > maxAbs {
			maxAbs = a
		}
	}
	if maxAbs < 0.5 {
		t.Fatalf("expected normalized peaks near 1, maxAbs=%v", maxAbs)
	}
}
