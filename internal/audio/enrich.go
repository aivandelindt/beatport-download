package audio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// EnrichAnalysis fills Camelot, tempo alternatives, estimated beat grid,
// true-peak/masking findings, and meta (file hash). Does not run ffmpeg.
func EnrichAnalysis(ctx context.Context, a *Analysis, filePath string) error {
	if a == nil {
		return fmt.Errorf("nil analysis")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filePath
	if path == "" {
		path = a.Path
	}
	meta := a.Meta
	if meta == nil {
		meta = &AnalysisMeta{}
		a.Meta = meta
	}
	meta.EnrichVersion = EnrichVersion
	meta.AnalyzerKind = a.Kind
	meta.AnalyzerSource = a.Source
	if path != "" {
		if hash, err := FileSHA256(ctx, path); err == nil {
			meta.FileSHA256 = hash
		}
	}

	if a.HarmonicAnalysis != nil {
		code := CamelotFromKeyMode(a.HarmonicAnalysis.Key, a.HarmonicAnalysis.Mode)
		if code != "" {
			a.HarmonicAnalysis.Camelot = code
			a.HarmonicAnalysis.CamelotReliability = ReliabilityEstimated
		}
	}

	var duration float64
	if a.AudioInfo != nil {
		duration = a.AudioInfo.DurationSec
	}

	if a.RhythmAnalysis != nil {
		r := a.RhythmAnalysis
		if r.BeatsDetected > 0 && len(r.BeatTimesSec) == r.BeatsDetected {
			r.BeatsComplete = true
		}
		bpm := r.MedianTempoBPM
		if bpm <= 0 {
			bpm = r.TempoBPM
		}
		if bpm > 0 {
			r.TempoHalfBPM, r.TempoDoubleBPM = TempoAlternatives(bpm)
			// When we have the full measured list, skip redundant extrapolated grid.
			if !r.BeatsComplete {
				r.BeatGridEstimated = EstimateBeatGrid(r.BeatTimesSec, bpm, duration)
			} else {
				r.BeatGridEstimated = nil
			}
		}
	}

	a.Issues = appendFindingsFromScalars(a)
	return nil
}

// FileSHA256 hashes file contents. Cancelled via ctx between read chunks.
func FileSHA256(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func appendFindingsFromScalars(a *Analysis) []Finding {
	var out []Finding
	if a.SpectralFeatures != nil {
		s := a.SpectralFeatures
		if s.TruePeakDBTP > -1.0 {
			out = append(out, Finding{
				Category:        "clipping_or_overload_risk",
				ChannelOrStem:   "mix",
				Measurement:     fmt.Sprintf("%.2f", s.TruePeakDBTP),
				Units:           "dBTP",
				Method:          "audio-analyzer-rs true_peak",
				Reliability:     ReliabilityMeasured,
				SuggestedAction: "True peak exceeds -1 dBTP; review for inter-sample peaks. Turning down does not repair clipped peaks.",
			})
		}
		if s.PeakDBFS >= -0.1 {
			out = append(out, Finding{
				Category:        "clipping_or_overload_risk",
				ChannelOrStem:   "mix",
				Measurement:     fmt.Sprintf("%.2f", s.PeakDBFS),
				Units:           "dBFS",
				Method:          "audio-analyzer-rs peak_dbfs",
				Reliability:     ReliabilityMeasured,
				SuggestedAction: "Sample peak near full scale; inspect for clipping. Do not treat gain reduction as declipping.",
			})
		}
		if s.Stereo != nil && s.Stereo.PhaseCorrMin < -0.5 {
			out = append(out, Finding{
				Category:        "possible_phase_mono_compatibility",
				ChannelOrStem:   "mix",
				Measurement:     fmt.Sprintf("%.3f", s.Stereo.PhaseCorrMin),
				Units:           "correlation",
				Method:          "audio-analyzer-rs stereo phase_corr_min",
				Reliability:     ReliabilityMeasured,
				SuggestedAction: "Low phase correlation; check mono compatibility before club/radio play.",
			})
		}
	}
	if a.Masking != nil {
		for _, b := range a.Masking.BandCrowding {
			if !strings.EqualFold(b.Label, "CROWDED") {
				continue
			}
			out = append(out, Finding{
				Category:        "frequency_masking",
				ChannelOrStem:   "mix",
				Measurement:     fmt.Sprintf("%s=%.3f", b.Band, b.Score),
				Units:           "crowding_score",
				Method:          "audio-analyzer-rs frequency masking",
				Reliability:     ReliabilityEstimated,
				SuggestedAction: "Band marked CROWDED (high energy + low contrast); review for muddiness — overlap alone is not proof of an audible problem.",
			})
		}
		for _, b := range a.Masking.HPCollision {
			if strings.EqualFold(b.Label, "COLLISION") || b.Score > 0.5 {
				out = append(out, Finding{
					Category:        "possible_harmonic_clash",
					ChannelOrStem:   "mix",
					Measurement:     fmt.Sprintf("%s=%.3f", b.Band, b.Score),
					Units:           "hp_collision",
					Method:          "audio-analyzer-rs H/P collision",
					Reliability:     ReliabilityEstimated,
					SuggestedAction: "Harmonic and percussive energy compete in this band; check kick/bass or transient clarity.",
				})
			}
		}
		for _, b := range a.Masking.CrossBleed {
			if strings.EqualFold(b.Label, "HIGH") || b.Score > 0.85 {
				out = append(out, Finding{
					Category:        "frequency_masking",
					ChannelOrStem:   "mix",
					Measurement:     fmt.Sprintf("%s=%.3f", b.Pair, b.Score),
					Units:           "cross_band_corr",
					Method:          "audio-analyzer-rs cross-band bleed",
					Reliability:     ReliabilityEstimated,
					SuggestedAction: "High cross-band correlation; possible bleed or shared masking between adjacent bands.",
				})
			}
		}
	}
	return out
}

// EnsureCamelot fills Camelot on older payloads that lack it.
func EnsureCamelot(a *Analysis) {
	if a == nil || a.HarmonicAnalysis == nil {
		return
	}
	if a.HarmonicAnalysis.Camelot != "" {
		return
	}
	code := CamelotFromKeyMode(a.HarmonicAnalysis.Key, a.HarmonicAnalysis.Mode)
	if code == "" {
		return
	}
	a.HarmonicAnalysis.Camelot = code
	a.HarmonicAnalysis.CamelotReliability = ReliabilityEstimated
}
