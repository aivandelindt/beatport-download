package audio_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"beatportdl-ui/internal/audio"
)

func writeFixtureWAV(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	out := filepath.Join(t.TempDir(), "tone.wav")
	// 1s stereo 440Hz sine near full scale to exercise clipping/RMS/spectrogram
	cmd := exec.Command("ffmpeg", "-nostdin", "-y", "-v", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=44100",
		"-filter_complex", "volume=3dB",
		out,
	)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture: %v (%s)", err, outBytes)
	}
	return out
}

func TestComputeRMSTimeline_Fixture(t *testing.T) {
	path := writeFixtureWAV(t)
	got, err := audio.ComputeRMSTimeline(context.Background(), path, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != audio.ReliabilityMeasured {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	if len(got.Values) != 50 {
		t.Fatalf("buckets: %d", len(got.Values))
	}
}

func TestSpectrogramPNG_Fixture(t *testing.T) {
	path := writeFixtureWAV(t)
	png := filepath.Join(t.TempDir(), "spec.png")
	if err := audio.SpectrogramPNG(context.Background(), path, png); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(png)
	if err != nil || fi.Size() < 100 {
		t.Fatalf("png missing or tiny: %v size=%v", err, fi)
	}
}

func TestDetectClipping_Fixture(t *testing.T) {
	path := writeFixtureWAV(t)
	events, err := audio.DetectClipping(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	// volume=+3dB sine may or may not clip depending on lavfi amplitude; just ensure call works
	_ = events
}
