package audio_test

import (
	"os"
	"path/filepath"
	"testing"

	"beatportdl-ui/internal/audio"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParse_AudioInfo(t *testing.T) {
	t.Parallel()
	raw := readTestdata(t, "audio_info.txt")
	got, err := audio.Parse(audio.KindAudioInfo, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.AudioInfo == nil {
		t.Fatal("AudioInfo is nil")
	}
	if got.AudioInfo.SampleRate != 48000 {
		t.Fatalf("sample rate: got %d", got.AudioInfo.SampleRate)
	}
	if got.AudioInfo.DurationSec < 60 || got.AudioInfo.DurationSec > 61 {
		t.Fatalf("duration: got %v", got.AudioInfo.DurationSec)
	}
	if got.AudioInfo.Samples != 2909952 {
		t.Fatalf("samples: got %d", got.AudioInfo.Samples)
	}
}

func TestParse_SpectralFeatures(t *testing.T) {
	t.Parallel()
	raw := readTestdata(t, "spectral_features.txt")
	got, err := audio.Parse(audio.KindSpectralFeatures, raw)
	if err != nil {
		t.Fatal(err)
	}
	s := got.SpectralFeatures
	if s == nil {
		t.Fatal("SpectralFeatures is nil")
	}
	if s.CentroidHz != 2812 {
		t.Fatalf("centroid: got %v", s.CentroidHz)
	}
	if s.LUFSIntegrated != -12.1 {
		t.Fatalf("lufs: got %v", s.LUFSIntegrated)
	}
	if s.Stereo == nil || s.Stereo.PhaseCorrAvg != 0.257 {
		t.Fatalf("stereo: %+v", s.Stereo)
	}
	if len(s.MFCCs) < 10 {
		t.Fatalf("mfccs: got %v", s.MFCCs)
	}
	if s.BandEnergy["bass"] == 0 {
		t.Fatalf("band energy: %+v", s.BandEnergy)
	}
}

func TestParse_HarmonicAnalysis(t *testing.T) {
	t.Parallel()
	raw := readTestdata(t, "harmonic_analysis.txt")
	got, err := audio.Parse(audio.KindHarmonicAnalysis, raw)
	if err != nil {
		t.Fatal(err)
	}
	h := got.HarmonicAnalysis
	if h == nil {
		t.Fatal("HarmonicAnalysis is nil")
	}
	if h.Key != "E" || h.Mode != "minor" {
		t.Fatalf("key: %s %s", h.Key, h.Mode)
	}
	if h.Confidence < 0.5 || h.Confidence > 0.6 {
		t.Fatalf("confidence: %v", h.Confidence)
	}
	if len(h.PitchClasses) < 3 {
		t.Fatalf("pitch classes: %+v", h.PitchClasses)
	}
}

func TestParse_RhythmAnalysis(t *testing.T) {
	t.Parallel()
	raw := readTestdata(t, "rhythm_analysis.txt")
	got, err := audio.Parse(audio.KindRhythmAnalysis, raw)
	if err != nil {
		t.Fatal(err)
	}
	r := got.RhythmAnalysis
	if r == nil {
		t.Fatal("RhythmAnalysis is nil")
	}
	if r.TempoBPM != 84.0 {
		t.Fatalf("tempo: %v", r.TempoBPM)
	}
	if r.BeatsDetected != 69 {
		t.Fatalf("beats: %d", r.BeatsDetected)
	}
	if r.Stability < 0.9 {
		t.Fatalf("stability: %v", r.Stability)
	}
	if len(r.BeatTimesSec) < 3 {
		t.Fatalf("beat times: %v", r.BeatTimesSec)
	}
}

func TestParse_FullAnalysis(t *testing.T) {
	t.Parallel()
	raw := readTestdata(t, "full_analysis.txt")
	got, err := audio.Parse(audio.KindFullAnalysis, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.AudioInfo == nil || got.AudioInfo.SampleRate != 48000 {
		t.Fatalf("audio info: %+v", got.AudioInfo)
	}
	if got.SpectralFeatures == nil || got.SpectralFeatures.CentroidHz != 2812 {
		t.Fatalf("spectral: %+v", got.SpectralFeatures)
	}
	if got.HarmonicAnalysis == nil || got.HarmonicAnalysis.Key != "E" {
		t.Fatalf("harmonic: %+v", got.HarmonicAnalysis)
	}
	if got.RhythmAnalysis == nil || got.RhythmAnalysis.TempoBPM != 84.0 {
		t.Fatalf("rhythm: %+v", got.RhythmAnalysis)
	}
	if got.Percussive == nil || got.Percussive.PercussiveRatio != 0.277 {
		t.Fatalf("percussive: %+v", got.Percussive)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("sections: %+v", got.Sections)
	}
	if got.Sections[0].TimeSec < 18 || got.Sections[0].TimeSec > 19 {
		t.Fatalf("section0 time: %v", got.Sections[0].TimeSec)
	}
}

func TestParse_CLIFullDump_ExtractsAllSections(t *testing.T) {
	t.Parallel()
	raw := readTestdata(t, "cli_full.txt")
	got, err := audio.Parse(audio.KindFullAnalysis, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.AudioInfo == nil || got.AudioInfo.SampleRate != 48000 {
		t.Fatalf("audio info: %+v", got.AudioInfo)
	}
	if got.SpectralFeatures == nil || got.SpectralFeatures.CentroidHz != 2812 {
		t.Fatalf("spectral: %+v", got.SpectralFeatures)
	}
	if got.HarmonicAnalysis == nil || got.HarmonicAnalysis.Key != "E" {
		t.Fatalf("harmonic: %+v", got.HarmonicAnalysis)
	}
	if got.RhythmAnalysis == nil || got.RhythmAnalysis.TempoBPM != 84.0 {
		t.Fatalf("rhythm: %+v", got.RhythmAnalysis)
	}
	if got.Percussive == nil {
		t.Fatal("percussive nil")
	}
	if len(got.Sections) < 1 {
		t.Fatalf("sections: %+v", got.Sections)
	}
}

func TestParse_EmptyInput(t *testing.T) {
	t.Parallel()
	_, err := audio.Parse(audio.KindAudioInfo, "")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}
