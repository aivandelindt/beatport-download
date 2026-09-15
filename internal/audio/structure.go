package audio

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// LabelSections turns novelty boundaries + an RMS envelope into labeled
// arrangement regions. This is a documented heuristic, not MIR ground truth:
//
//  1. Split the track at novelty boundaries (plus 0 and duration).
//  2. For each interval, compute mean RMS vs the track median.
//  3. Prefer labels from boundary reason strings (energy/spectral/harmonic/texture)
//     combined with relative loudness — never "loudest = chorus".
//  4. Ends default toward outro; starts toward intro when quieter than median.
//  5. Weak evidence → "unknown" with reliability "low".
func LabelSections(boundaries []SectionBoundary, durationSec float64, rmsTimes, rmsValues []float64) []LabeledSection {
	if durationSec <= 0 {
		return nil
	}
	cuts := []float64{0}
	type cutMeta struct {
		reasons string
		conf    float64
	}
	metaAt := map[float64]cutMeta{}
	for _, b := range boundaries {
		t := b.TimeSec
		if t <= 0 || t >= durationSec {
			continue
		}
		cuts = append(cuts, t)
		metaAt[t] = cutMeta{reasons: b.Reasons, conf: b.Confidence}
	}
	cuts = append(cuts, durationSec)
	sort.Float64s(cuts)
	cuts = uniqueFloats(cuts)

	median := percentile(rmsValues, 0.5)
	if median <= 0 {
		median = 1e-9
	}

	var out []LabeledSection
	for i := 0; i+1 < len(cuts); i++ {
		start, end := cuts[i], cuts[i+1]
		mean := meanInRange(rmsTimes, rmsValues, start, end)
		rel := mean / median
		reasons := ""
		conf := 0.0
		if m, ok := metaAt[start]; ok {
			reasons = m.reasons
			conf = m.conf
		} else if i == 0 && len(boundaries) > 0 {
			// First section: use first boundary reasons as upcoming change hint.
			reasons = boundaries[0].Reasons
			conf = boundaries[0].Confidence * 0.5
		}
		label, evidence, reliability := classifySection(i, len(cuts)-1, rel, reasons, conf)
		out = append(out, LabeledSection{
			StartTime:   start,
			EndTime:     end,
			Label:       label,
			Evidence:    evidence,
			Reliability: reliability,
		})
	}
	return out
}

func classifySection(idx, nSections int, relLoud float64, reasons string, conf float64) (label, evidence, reliability string) {
	r := strings.ToLower(reasons)
	hasEnergy := strings.Contains(r, "energy")
	hasHarmonic := strings.Contains(r, "harmonic")
	hasSpectral := strings.Contains(r, "spectral")
	hasTexture := strings.Contains(r, "texture")
	isFirst := idx == 0
	isLast := idx == nSections-1

	evidence = fmt.Sprintf("rms_vs_median=%.2f reasons=%q boundary_conf=%.2f", relLoud, reasons, conf)

	// Weak signal → unknown
	if conf < 0.35 && !isFirst && !isLast && math.Abs(relLoud-1) < 0.15 {
		return "unknown", evidence, ReliabilityLow
	}

	switch {
	case isFirst && relLoud < 0.85:
		return "intro", evidence + "; quieter opening", ReliabilityEstimated
	case isLast && relLoud < 1.15:
		return "outro", evidence + "; trailing region", ReliabilityEstimated
	case relLoud > 1.25 && hasEnergy && (hasSpectral || hasTexture):
		// Loud + multi-feature change — drop candidate, not auto-chorus
		return "drop", evidence + "; loud multi-feature boundary", ReliabilityEstimated
	case relLoud > 1.15 && hasEnergy && hasHarmonic:
		return "chorus", evidence + "; loud energy+harmonic change", ReliabilityEstimated
	case relLoud < 0.75 && (hasEnergy || hasTexture):
		return "breakdown", evidence + "; quieter energy/texture change", ReliabilityEstimated
	case relLoud > 1.05 && hasEnergy && !hasHarmonic:
		return "build", evidence + "; rising energy without harmonic shift", ReliabilityEstimated
	case hasHarmonic || hasSpectral:
		return "verse", evidence + "; harmonic/spectral change at moderate level", ReliabilityEstimated
	case isFirst:
		return "intro", evidence + "; first region default", ReliabilityLow
	case isLast:
		return "outro", evidence + "; last region default", ReliabilityLow
	default:
		return "unknown", evidence, ReliabilityLow
	}
}

func uniqueFloats(in []float64) []float64 {
	if len(in) == 0 {
		return in
	}
	out := []float64{in[0]}
	for i := 1; i < len(in); i++ {
		if in[i]-out[len(out)-1] > 1e-6 {
			out = append(out, in[i])
		}
	}
	return out
}

func meanInRange(times, values []float64, start, end float64) float64 {
	if len(times) == 0 || len(values) == 0 || len(times) != len(values) {
		return 1
	}
	var sum float64
	var n int
	for i, t := range times {
		if t < start || t >= end {
			continue
		}
		sum += values[i]
		n++
	}
	if n == 0 {
		// fallback: nearest samples
		for i, t := range times {
			if t >= start && t <= end {
				sum += values[i]
				n++
			}
		}
	}
	if n == 0 {
		return 1
	}
	return sum / float64(n)
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	if p <= 0 {
		return cp[0]
	}
	if p >= 1 {
		return cp[len(cp)-1]
	}
	idx := int(math.Round(p * float64(len(cp)-1)))
	return cp[idx]
}
