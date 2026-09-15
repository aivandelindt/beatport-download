package audio_test

import (
	"testing"

	"beatportdl-ui/internal/audio"
)

func TestCamelotFromKeyMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key, mode, want string
	}{
		{"E", "minor", "9A"},
		{"C", "major", "8B"},
		{"A", "minor", "8A"},
		{"F#", "major", "2B"},
		{"Db", "minor", "12A"},
		{"E", "dorian", ""},
		{"", "minor", ""},
		{"E", "", ""},
	}
	for _, tc := range cases {
		got := audio.CamelotFromKeyMode(tc.key, tc.mode)
		if got != tc.want {
			t.Fatalf("%s %s: got %q want %q", tc.key, tc.mode, got, tc.want)
		}
	}
}

func TestTempoAlternatives(t *testing.T) {
	t.Parallel()
	h, d := audio.TempoAlternatives(128)
	if h != 64 || d != 256 {
		t.Fatalf("got half=%v double=%v", h, d)
	}
	h, d = audio.TempoAlternatives(0)
	if h != 0 || d != 0 {
		t.Fatalf("zero bpm: %v %v", h, d)
	}
}

func TestEstimateBeatGrid(t *testing.T) {
	t.Parallel()
	measured := []float64{0.5, 1.0}
	grid := audio.EstimateBeatGrid(measured, 120, 3.0)
	// 120 BPM → 0.5s interval; measured 0.5 and 1.0 skipped; expect ~1.5, 2.0, 2.5, 3.0
	if len(grid) < 3 {
		t.Fatalf("grid too short: %v", grid)
	}
	for _, tsec := range measured {
		for _, g := range grid {
			d := g - tsec
			if d < 0 {
				d = -d
			}
			if d < 0.04 {
				t.Fatalf("grid overlaps measured: grid=%v measured=%v", grid, measured)
			}
		}
	}
}
