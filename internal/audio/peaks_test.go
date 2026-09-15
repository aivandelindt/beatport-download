package audio

import (
	"context"
	"fmt"
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
	if len(p.RGBLow) != 50 || len(p.RGBMid) != 50 || len(p.RGBHigh) != 50 {
		t.Fatalf("rgb lengths %d %d %d", len(p.RGBLow), len(p.RGBMid), len(p.RGBHigh))
	}
}

func TestBucketMinMaxAndRGB_LowDominates(t *testing.T) {
	t.Parallel()
	n := 40
	orig := make([]float32, n)
	low := make([]float32, n)
	mid := make([]float32, n)
	high := make([]float32, n)
	for i := 0; i < n; i++ {
		orig[i] = 0.4
		low[i] = 0.9
		mid[i] = 0.1
		high[i] = 0.05
	}
	_, rgbL, rgbM, rgbH := bucketMinMaxAndRGB(orig, low, mid, high, 4)
	if len(rgbL) != 4 {
		t.Fatalf("len %d", len(rgbL))
	}
	if rgbL[0] <= rgbM[0] || rgbL[0] <= rgbH[0] {
		t.Fatalf("low should dominate: L=%v M=%v H=%v", rgbL[0], rgbM[0], rgbH[0])
	}
	if math.Abs(rgbL[0]-1) > 1e-6 {
		t.Fatalf("low should normalize to 1, got %v", rgbL[0])
	}
}

func TestComputePeaks_RGBBandTones(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	bass := filepath.Join(dir, "bass.wav")
	air := filepath.Join(dir, "air.wav")
	for _, spec := range []struct {
		path string
		freq int
	}{{bass, 100}, {air, 6000}} {
		cmd := exec.Command("ffmpeg", "-nostdin", "-y", "-f", "lavfi",
			"-i", fmt.Sprintf("sine=frequency=%d:duration=0.4", spec.freq),
			"-ac", "1", spec.path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate %s: %v: %s", spec.path, err, out)
		}
	}
	lowP, err := ComputePeaks(context.Background(), bass, 20)
	if err != nil {
		t.Fatal(err)
	}
	highP, err := ComputePeaks(context.Background(), air, 20)
	if err != nil {
		t.Fatal(err)
	}
	if mean(lowP.RGBLow) <= mean(lowP.RGBHigh) {
		t.Fatalf("100Hz should be red/bass-dominant: L=%v H=%v", mean(lowP.RGBLow), mean(lowP.RGBHigh))
	}
	if mean(highP.RGBHigh) <= mean(highP.RGBLow) {
		t.Fatalf("6kHz should be blue/high-dominant: L=%v H=%v", mean(highP.RGBLow), mean(highP.RGBHigh))
	}
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}
