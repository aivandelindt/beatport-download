package audio

import (
	"context"
	"fmt"
	"math"
	"os/exec"
)

// VU-style digital meter constants (IEC 60268-17 *inspired*, not compliant).
// τ ≈ 65.1 ms → 99% rise in ~300 ms on a linear-amplitude one-pole.
const (
	VUTauSec       = 0.0651
	VUZeroDBFS     = -18.0 // display: 0 VU = −18 dBFS (EBU-style offset)
	VUSampleRate   = peakSampleRate
	VULabel        = "VU-style"
)

// VUTimeline is a downsampled VU-style envelope for the Library strip.
type VUTimeline struct {
	DurationSec float64   `json:"duration_sec"`
	TimesSec    []float64 `json:"times_sec"`
	Values      []float64 `json:"values"` // VU units (0 = ZeroVUDBFS)
	ZeroVUDBFS  float64   `json:"zero_vu_dbfs"`
	TauSec      float64   `json:"tau_sec"`
	Units       string    `json:"units"`
	Method      string    `json:"method"`
	Label       string    `json:"label"`
	Status      string    `json:"status"` // estimated | not_performed
	Error       string    `json:"error,omitempty"`
}

// VUStyleStep advances a linear-amplitude one-pole toward |sample|.
// Returns updated state (linear amplitude).
func VUStyleStep(state, sample, dt, tau float64) float64 {
	if tau <= 0 || dt <= 0 {
		return math.Abs(sample)
	}
	alpha := 1 - math.Exp(-dt/tau)
	target := math.Abs(sample)
	return state + alpha*(target-state)
}

// LinearAmpToVU converts linear amplitude to VU with 0 VU = zeroVUDBFS.
func LinearAmpToVU(amp, zeroVUDBFS float64) float64 {
	if amp <= 1e-12 {
		return -80 // floor
	}
	dbfs := 20 * math.Log10(amp)
	return dbfs - zeroVUDBFS
}

// ComputeVUStyleFromSamples runs the VU-style envelope over mono PCM and
// downsamples to buckets. Used by ComputeVUStyleTimeline and unit tests.
func ComputeVUStyleFromSamples(samples []float64, sampleRate, buckets int) VUTimeline {
	out := VUTimeline{
		ZeroVUDBFS: VUZeroDBFS,
		TauSec:     VUTauSec,
		Units:      "VU",
		Method:     "linear-amplitude one-pole τ≈65.1ms, 0 VU = −18 dBFS (not IEC 60268-17)",
		Label:      VULabel,
		Status:     ReliabilityNotPerf,
	}
	if buckets <= 0 {
		buckets = timelineBuckets
	}
	if sampleRate <= 0 || len(samples) == 0 {
		out.Error = "no samples"
		return out
	}
	dt := 1.0 / float64(sampleRate)
	dur := float64(len(samples)) / float64(sampleRate)
	state := 0.0
	envelope := make([]float64, len(samples))
	for i, s := range samples {
		state = VUStyleStep(state, s, dt, VUTauSec)
		envelope[i] = state
	}
	times := make([]float64, buckets)
	vals := make([]float64, buckets)
	n := len(envelope)
	for i := 0; i < buckets; i++ {
		start := i * n / buckets
		end := (i + 1) * n / buckets
		if end <= start {
			end = start + 1
		}
		if end > n {
			end = n
		}
		var sum float64
		for j := start; j < end; j++ {
			sum += envelope[j]
		}
		avg := sum / float64(end-start)
		times[i] = (float64(start) + float64(end-start)/2) / float64(sampleRate)
		vals[i] = LinearAmpToVU(avg, VUZeroDBFS)
	}
	out.DurationSec = dur
	out.TimesSec = times
	out.Values = vals
	out.Status = ReliabilityEstimated
	return out
}

// ComputeVUStyleTimeline decodes mono @ 8 kHz via ffmpeg and applies VU-style ballistics.
func ComputeVUStyleTimeline(ctx context.Context, path string, buckets int) (VUTimeline, error) {
	out := VUTimeline{
		ZeroVUDBFS: VUZeroDBFS,
		TauSec:     VUTauSec,
		Units:      "VU",
		Method:     "linear-amplitude one-pole τ≈65.1ms, 0 VU = −18 dBFS (not IEC 60268-17)",
		Label:      VULabel,
		Status:     ReliabilityNotPerf,
	}
	if buckets <= 0 {
		buckets = timelineBuckets
	}
	if _, err := lookPath("ffmpeg"); err != nil {
		out.Error = "ffmpeg not found"
		return out, nil
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin", "-v", "error",
		"-i", path,
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", VUSampleRate),
		"-f", "f32le",
		"pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	var stderrBuf []byte
	cmd.Stderr = &collectWriter{buf: &stderrBuf}
	if err := cmd.Start(); err != nil {
		out.Error = err.Error()
		return out, nil
	}
	f32, err := readF32LE(stdout)
	waitErr := cmd.Wait()
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	if waitErr != nil && len(f32) == 0 {
		out.Error = cmdError("ffmpeg vu", stderrBuf, waitErr).Error()
		return out, nil
	}
	if len(f32) == 0 {
		out.Error = "no samples"
		return out, nil
	}
	samples := make([]float64, len(f32))
	for i, v := range f32 {
		samples[i] = float64(v)
	}
	return ComputeVUStyleFromSamples(samples, VUSampleRate, buckets), nil
}
