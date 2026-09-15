package audio_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"beatportdl-ui/internal/audio"
)

func TestEnrichAnalysis_CamelotAndFindings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "dummy.bin")
	if err := os.WriteFile(path, []byte("hello-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := audio.Analysis{
		Kind:   audio.KindFullAnalysis,
		Source: "mcp",
		Path:   path,
		AudioInfo: &audio.AudioInfo{DurationSec: 10},
		HarmonicAnalysis: &audio.HarmonicAnalysis{Key: "E", Mode: "minor", Confidence: 0.5},
		RhythmAnalysis: &audio.RhythmAnalysis{
			TempoBPM:       120,
			MedianTempoBPM: 120,
			BeatTimesSec:   []float64{0.5, 1.0},
		},
		SpectralFeatures: &audio.SpectralFeatures{TruePeakDBTP: 0.2, PeakDBFS: -0.05},
		Masking: &audio.MaskingAnalysis{
			BandCrowding: []audio.BandCrowding{{Band: "low_mid", Score: 0.61, Label: "CROWDED"}},
		},
	}
	if err := audio.EnrichAnalysis(context.Background(), &a, path); err != nil {
		t.Fatal(err)
	}
	if a.HarmonicAnalysis.Camelot != "9A" {
		t.Fatalf("camelot: %q", a.HarmonicAnalysis.Camelot)
	}
	if a.RhythmAnalysis.TempoHalfBPM != 60 || a.RhythmAnalysis.TempoDoubleBPM != 240 {
		t.Fatalf("tempo alt: %+v", a.RhythmAnalysis)
	}
	if len(a.RhythmAnalysis.BeatGridEstimated) == 0 {
		t.Fatal("expected estimated beat grid")
	}
	if a.Meta == nil || a.Meta.FileSHA256 == "" {
		t.Fatalf("meta: %+v", a.Meta)
	}
	if len(a.Issues) < 2 {
		t.Fatalf("issues: %+v", a.Issues)
	}
}
