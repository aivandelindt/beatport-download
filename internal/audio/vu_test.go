package audio_test

import (
	"math"
	"testing"

	"beatportdl-ui/internal/audio"
)

func TestVUStyleStep_99PercentAt300ms(t *testing.T) {
	t.Parallel()
	const target = 1.0
	const dt = 0.001 // 1 ms
	state := 0.0
	for i := 0; i < 300; i++ {
		state = audio.VUStyleStep(state, target, dt, audio.VUTauSec)
	}
	ratio := state / target
	// IEC criterion: 99% ± 10% band → accept >= 0.98
	if ratio < 0.98 || ratio > 1.01 {
		t.Fatalf("300ms rise ratio want ~0.99, got %.4f", ratio)
	}
}

func TestLinearAmpToVU_Calibration(t *testing.T) {
	t.Parallel()
	// amp for −18 dBFS: 10^(-18/20)
	amp := math.Pow(10, -18.0/20.0)
	vu := audio.LinearAmpToVU(amp, audio.VUZeroDBFS)
	if math.Abs(vu) > 0.05 {
		t.Fatalf("0 VU at −18 dBFS: got %.3f", vu)
	}
}

func TestComputeVUStyleFromSamples_Buckets(t *testing.T) {
	t.Parallel()
	sr := 8000
	samples := make([]float64, sr) // 1s of −18 dBFS sine-ish constant amp
	amp := math.Pow(10, -18.0/20.0)
	for i := range samples {
		samples[i] = amp
	}
	got := audio.ComputeVUStyleFromSamples(samples, sr, 50)
	if got.Status != audio.ReliabilityEstimated {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	if len(got.Values) != 50 {
		t.Fatalf("buckets: %d", len(got.Values))
	}
	if got.Label != audio.VULabel {
		t.Fatalf("label: %q", got.Label)
	}
	// After ballistics settle, last buckets should be near 0 VU
	last := got.Values[len(got.Values)-1]
	if math.Abs(last) > 0.5 {
		t.Fatalf("settled VU want ~0, got %.3f", last)
	}
}
