package audio_test

import (
	"testing"

	"beatportdl-ui/internal/audio"
)

func TestLabelSections_IntroDropOutro(t *testing.T) {
	t.Parallel()
	boundaries := []audio.SectionBoundary{
		{TimeSec: 10, Reasons: "energy+spectral+texture", Confidence: 0.9},
		{TimeSec: 40, Reasons: "energy+harmonic", Confidence: 0.8},
	}
	// Quiet-dominant track; loud middle so median stays low.
	n := 100
	times := make([]float64, n)
	vals := make([]float64, n)
	for i := 0; i < n; i++ {
		times[i] = float64(i)
		switch {
		case i < 10:
			vals[i] = 0.2
		case i < 40:
			vals[i] = 2.0
		default:
			vals[i] = 0.3
		}
	}
	got := audio.LabelSections(boundaries, float64(n), times, vals)
	if len(got) != 3 {
		t.Fatalf("sections: %+v", got)
	}
	if got[0].Label != "intro" {
		t.Fatalf("first label: %s (%s)", got[0].Label, got[0].Evidence)
	}
	if got[1].Label != "drop" && got[1].Label != "chorus" && got[1].Label != "build" {
		t.Fatalf("middle label: %s (%s)", got[1].Label, got[1].Evidence)
	}
	if got[2].Label != "outro" && got[2].Label != "breakdown" {
		t.Fatalf("last label: %s (%s)", got[2].Label, got[2].Evidence)
	}
}
