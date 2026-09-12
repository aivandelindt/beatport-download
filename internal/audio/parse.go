package audio

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	reSampleRate = regexp.MustCompile(`(?i)Sample rate:\s*(\d+)\s*Hz`)
	reSamples    = regexp.MustCompile(`(?i)Samples:\s*(\d+)`)
	reDuration   = regexp.MustCompile(`(?i)Duration:\s*([\d.]+)\s*(?:seconds?|sec)\b`)
	reFilePath   = regexp.MustCompile(`(?m)^(?:File:|Spectral Analysis:|Harmonic Analysis:|Rhythm Analysis:)\s*(.+)$`)
	reFullFile   = regexp.MustCompile(`(?m)^File:\s*(.+)$`)

	reCentroid  = regexp.MustCompile(`(?i)Centroid[^:]*:\s*(?:avg\s+)?([\d.]+)\s*Hz`)
	reBandwidth = regexp.MustCompile(`(?i)Bandwidth[^:]*:\s*(?:avg\s+)?([\d.]+)\s*Hz`)
	reRolloff   = regexp.MustCompile(`(?i)Rolloff[^:]*:\s*(?:avg\s+)?([\d.]+)\s*Hz`)
	reFlatness  = regexp.MustCompile(`(?i)Flatness[^:]*:\s*(?:avg\s+)?([\d.]+)`)
	reRMS       = regexp.MustCompile(`(?i)RMS Energy[^:]*:\s*(?:avg\s+)?([\d.]+)`)
	reZCR       = regexp.MustCompile(`(?i)Zero Crossing Rate:\s*(?:avg\s+)?([\d.]+)`)
	reMFCCList  = regexp.MustCompile(`(?i)MFCCs?\s*\([^)]*\):\s*(?:\[([^\]]+)\]|(.+))`)
	reMFCCNum   = regexp.MustCompile(`-?[\d.]+`)

	reBandLine = regexp.MustCompile(`(?i)^\s*([A-Za-z_ -]+?)\s*(?:\([^)]*\))?:\s*([\d.]+)`)

	rePeakDBFS      = regexp.MustCompile(`(?i)Peak:\s*([-\d.]+)\s*dBFS`)
	reCrest         = regexp.MustCompile(`(?i)Crest factor:\s*([\d.]+)\s*dB`)
	reLoudnessRange = regexp.MustCompile(`(?i)Loudness range:\s*([\d.]+)\s*dB`)
	reIntegrated    = regexp.MustCompile(`(?i)Integrated:\s*([-\d.]+)\s*LUFS`)
	reTruePeak      = regexp.MustCompile(`(?i)True peak:\s*([-\d.]+)\s*dBTP`)
	reLRA           = regexp.MustCompile(`(?i)Loudness range:\s*([\d.]+)\s*LU\b`)

	rePhaseCorr = regexp.MustCompile(`(?i)Phase correlation:\s*([-\d.]+)\s*avg,\s*([-\d.]+)\s*min`)
	reStereoW   = regexp.MustCompile(`(?i)Stereo width:\s*([\d.]+)\s*avg`)
	reBalance   = regexp.MustCompile(`(?i)Balance:\s*([-\d.]+)`)
	reMonoCompat = regexp.MustCompile(`(?i)Mono compatibility:\s*([\d.]+)\s*avg`)

	reKey = regexp.MustCompile(`(?i)Estimated [Kk]ey:\s*(\S+)\s+(\w+)\s*\(confidence:\s*([\d.]+)\)`)
	rePitchClass = regexp.MustCompile(`(?m)^\s*\d+\.\s+(\S+)\s+([\d.]+)`)

	reTempo = regexp.MustCompile(`(?i)(?:Estimated )?Tempo:\s*([\d.]+)\s*BPM\s*\(confidence:\s*([\d.]+)\)`)
	reBeats = regexp.MustCompile(`(?i)(?:Detected )?[Bb]eats(?: detected)?:\s*(\d+)`)
	reMeanMedian = regexp.MustCompile(`(?i)Mean tempo:\s*([\d.]+)\s*BPM\s*\|\s*Median:\s*([\d.]+)\s*BPM`)
	reStability = regexp.MustCompile(`(?i)Stability:\s*([\d.]+)`)
	reIBIStd = regexp.MustCompile(`(?i)IBI std(?:ard)?\s*dev(?:iation)?:\s*([\d.]+)\s*sec`)
	// MCP: "First 20 beats: …"  CLI: "First beats: …"
	reBeatTimes = regexp.MustCompile(`(?i)First(?:\s+\d+)?\s+beats:\s*(.+)`)

	reQuietLoud = regexp.MustCompile(`(?i)Quiet sections:\s*([-\d.]+)\s*dBFS\s*\|\s*Loud sections:\s*([-\d.]+)\s*dBFS`)

	rePercRatio = regexp.MustCompile(`(?i)Percussive ratio:\s*([\d.]+)`)
	reOnsetDens = regexp.MustCompile(`(?i)Onset density:\s*([\d.]+)`)
	reAttack    = regexp.MustCompile(`(?i)Peak attack sharp:\s*([\d.]+)`)

	reSection = regexp.MustCompile(`(?m)^\s*(?:(\d+):)?(\d+(?:\.\d+)?)s\s+([^(]+)\(confidence:\s*([\d.]+)\)`)

	reMaskCrowding = regexp.MustCompile(`(?i)^\s*([A-Za-z_ -]+?)\s*\([^)]*\):\s*([\d.]+)\s+(\S+)`)
	reMaskHP       = regexp.MustCompile(`(?i)^\s*([A-Za-z_ -]+?)\s*:\s*([\d.]+)\s+(\S+)`)
	reMaskBleed    = regexp.MustCompile(`(?i)^\s*(\S+↔\S+|\S+<->\S+|\S+↔\S+):\s*([\d.]+)\s+(\S+)`)
)

// Parse converts analyzer formatted text into structured Analysis for the given kind.
func Parse(kind, text string) (Analysis, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Analysis{}, fmt.Errorf("empty analysis text")
	}

	out := Analysis{
		Kind:    kind,
		RawText: text,
	}

	switch kind {
	case KindAudioInfo:
		info := parseAudioInfo(text)
		if info.SampleRate == 0 && info.DurationSec == 0 {
			return Analysis{}, fmt.Errorf("failed to parse audio_info")
		}
		out.AudioInfo = &info
		out.Path = info.Path
	case KindSpectralFeatures:
		spec := parseSpectral(text)
		out.SpectralFeatures = &spec
		out.Path = firstPath(text)
		out.AudioInfo = ptrAudioInfoFromHeader(text)
	case KindHarmonicAnalysis:
		harm := parseHarmonic(text)
		out.HarmonicAnalysis = &harm
		out.Path = firstPath(text)
	case KindRhythmAnalysis:
		rhythm := parseRhythm(text)
		out.RhythmAnalysis = &rhythm
		out.Path = firstPath(text)
	case KindFullAnalysis:
		fillFull(&out, text)
	default:
		return Analysis{}, fmt.Errorf("unknown analysis kind: %s", kind)
	}

	return out, nil
}

func fillFull(out *Analysis, text string) {
	out.Path = firstPath(text)
	info := parseAudioInfo(text)
	if info.Path == "" {
		info.Path = out.Path
	}
	out.AudioInfo = &info
	spec := parseSpectral(text)
	out.SpectralFeatures = &spec
	harm := parseHarmonic(text)
	out.HarmonicAnalysis = &harm
	rhythm := parseRhythm(text)
	out.RhythmAnalysis = &rhythm
	if p := parsePercussive(text); p != nil {
		out.Percussive = p
	}
	if m := parseMasking(text); m != nil {
		out.Masking = m
	}
	out.Sections = parseSections(text)
}

func ptrAudioInfoFromHeader(text string) *AudioInfo {
	info := parseAudioInfo(text)
	if info.SampleRate == 0 && info.DurationSec == 0 && info.Samples == 0 {
		return nil
	}
	return &info
}

func parseAudioInfo(text string) AudioInfo {
	info := AudioInfo{Path: firstPath(text)}
	if m := reSampleRate.FindStringSubmatch(text); len(m) == 2 {
		info.SampleRate, _ = strconv.Atoi(m[1])
	}
	if m := reSamples.FindStringSubmatch(text); len(m) == 2 {
		info.Samples, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := reDuration.FindStringSubmatch(text); len(m) == 2 {
		info.DurationSec, _ = strconv.ParseFloat(m[1], 64)
	}
	return info
}

func parseSpectral(text string) SpectralFeatures {
	s := SpectralFeatures{
		BandEnergy:       map[string]float64{},
		SpectralContrast: map[string]float64{},
	}
	if m := reCentroid.FindStringSubmatch(text); len(m) == 2 {
		s.CentroidHz, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reBandwidth.FindStringSubmatch(text); len(m) == 2 {
		s.BandwidthHz, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reRolloff.FindStringSubmatch(text); len(m) == 2 {
		s.RolloffHz, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reFlatness.FindStringSubmatch(text); len(m) == 2 {
		s.Flatness, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reRMS.FindStringSubmatch(text); len(m) == 2 {
		s.RMSEnergy, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reZCR.FindStringSubmatch(text); len(m) == 2 {
		s.ZeroCrossingRate, _ = strconv.ParseFloat(m[1], 64)
	}
	s.MFCCs = parseMFCCs(text)
	s.BandEnergy = parseBandMap(text, "Frequency Band Energy", "Spectral Contrast")
	s.SpectralContrast = parseBandMap(text, "Spectral Contrast", "── Dynamic Range")
	if len(s.SpectralContrast) == 0 {
		s.SpectralContrast = parseBandMap(text, "Spectral Contrast", "── Harmonic")
	}

	if m := rePeakDBFS.FindStringSubmatch(text); len(m) == 2 {
		s.PeakDBFS, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reCrest.FindStringSubmatch(text); len(m) == 2 {
		s.CrestFactorDB, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reLoudnessRange.FindStringSubmatch(text); len(m) == 2 {
		s.LoudnessRangeDB, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reIntegrated.FindStringSubmatch(text); len(m) == 2 {
		s.LUFSIntegrated, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reTruePeak.FindStringSubmatch(text); len(m) == 2 {
		s.TruePeakDBTP, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reLRA.FindStringSubmatch(text); len(m) == 2 {
		s.LRA, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reQuietLoud.FindStringSubmatch(text); len(m) == 3 {
		q, _ := strconv.ParseFloat(m[1], 64)
		l, _ := strconv.ParseFloat(m[2], 64)
		s.QuietRMSDBFS = &q
		s.LoudRMSDBFS = &l
	}
	if stereo := parseStereo(text); stereo != nil {
		s.Stereo = stereo
	}
	return s
}

func parseMFCCs(text string) []float64 {
	m := reMFCCList.FindStringSubmatch(text)
	if len(m) == 0 {
		return nil
	}
	blob := m[1]
	if blob == "" {
		blob = m[2]
	}
	nums := reMFCCNum.FindAllString(blob, -1)
	out := make([]float64, 0, len(nums))
	for _, n := range nums {
		// Skip MFCC index labels like "0" from "MFCC-0:" — prefer values after colon patterns
		if strings.HasPrefix(n, ".") {
			continue
		}
		v, err := strconv.ParseFloat(n, 64)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	// When format is "MFCC-0: -141.30, MFCC-1: 13.70" we get index+value pairs;
	// keep only odd-ish by taking values that look like MFCC coeffs (take every other starting at 1 if pairs).
	if looksLikeMFCCPairs(blob) {
		vals := make([]float64, 0, len(out)/2)
		for i := 0; i+1 < len(out); i += 2 {
			vals = append(vals, out[i+1])
		}
		return vals
	}
	return out
}

func looksLikeMFCCPairs(blob string) bool {
	return strings.Contains(strings.ToUpper(blob), "MFCC-")
}

func parseBandMap(text, startMarker, endMarker string) map[string]float64 {
	out := map[string]float64{}
	idx := strings.Index(text, startMarker)
	if idx < 0 {
		// Try case-insensitive / alternate headers
		lower := strings.ToLower(text)
		idx = strings.Index(lower, strings.ToLower(startMarker))
		if idx < 0 {
			return out
		}
	}
	section := text[idx:]
	if endMarker != "" {
		if e := strings.Index(section[len(startMarker):], endMarker); e >= 0 {
			section = section[:len(startMarker)+e]
		} else if e := strings.Index(section, "\n──"); e > 0 {
			// keep first dyn section for contrast end
			_ = e
		}
	}
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, startMarker) {
			continue
		}
		if strings.HasPrefix(line, "──") {
			break
		}
		m := reBandLine.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		name := normalizeBandName(m[1])
		if name == "" || name == "loudness" || name == "peak" {
			continue
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		out[name] = v
	}
	return out
}

func normalizeBandName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	switch {
	case strings.HasPrefix(s, "sub"):
		return "sub_bass"
	case s == "bass" || strings.HasPrefix(s, "bass"):
		return "bass"
	case strings.Contains(s, "low") && strings.Contains(s, "mid"):
		return "low_mid"
	case s == "mid" || strings.HasPrefix(s, "mid"):
		return "mid"
	case strings.Contains(s, "upper"):
		return "upper_mid"
	case strings.HasPrefix(s, "presence"):
		return "presence"
	case strings.HasPrefix(s, "brilliance"):
		return "brilliance"
	}
	return s
}

func parseStereo(text string) *StereoField {
	if !strings.Contains(text, "Stereo") && !strings.Contains(text, "Phase correlation") {
		return nil
	}
	sf := &StereoField{}
	found := false
	if m := rePhaseCorr.FindStringSubmatch(text); len(m) == 3 {
		sf.PhaseCorrAvg, _ = strconv.ParseFloat(m[1], 64)
		sf.PhaseCorrMin, _ = strconv.ParseFloat(m[2], 64)
		found = true
	}
	if m := reStereoW.FindStringSubmatch(text); len(m) == 2 {
		sf.StereoWidthAvg, _ = strconv.ParseFloat(m[1], 64)
		found = true
	}
	if m := reBalance.FindStringSubmatch(text); len(m) == 2 {
		sf.Balance, _ = strconv.ParseFloat(m[1], 64)
		found = true
	}
	if m := reMonoCompat.FindStringSubmatch(text); len(m) == 2 {
		sf.MonoCompatibility, _ = strconv.ParseFloat(m[1], 64)
		found = true
	}
	if !found {
		return nil
	}
	return sf
}

func parseHarmonic(text string) HarmonicAnalysis {
	h := HarmonicAnalysis{}
	if m := reKey.FindStringSubmatch(text); len(m) == 4 {
		h.Key = m[1]
		h.Mode = m[2]
		h.Confidence, _ = strconv.ParseFloat(m[3], 64)
	}
	for _, m := range rePitchClass.FindAllStringSubmatch(text, -1) {
		if len(m) != 3 {
			continue
		}
		v, _ := strconv.ParseFloat(m[2], 64)
		h.PitchClasses = append(h.PitchClasses, PitchClass{Name: m[1], Value: v})
	}
	return h
}

func parseRhythm(text string) RhythmAnalysis {
	r := RhythmAnalysis{}
	if m := reTempo.FindStringSubmatch(text); len(m) == 3 {
		r.TempoBPM, _ = strconv.ParseFloat(m[1], 64)
		r.Confidence, _ = strconv.ParseFloat(m[2], 64)
	}
	if m := reBeats.FindStringSubmatch(text); len(m) == 2 {
		r.BeatsDetected, _ = strconv.Atoi(m[1])
	}
	if m := reMeanMedian.FindStringSubmatch(text); len(m) == 3 {
		r.MeanTempoBPM, _ = strconv.ParseFloat(m[1], 64)
		r.MedianTempoBPM, _ = strconv.ParseFloat(m[2], 64)
	}
	if m := reStability.FindStringSubmatch(text); len(m) == 2 {
		r.Stability, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reIBIStd.FindStringSubmatch(text); len(m) == 2 {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			r.IBIStdSec = &v
		}
	}
	if m := reBeatTimes.FindStringSubmatch(text); len(m) == 2 {
		for _, part := range strings.Split(m[1], ",") {
			part = strings.TrimSpace(part)
			part = strings.TrimSuffix(part, "s")
			if part == "" {
				continue
			}
			v, err := strconv.ParseFloat(part, 64)
			if err != nil {
				continue
			}
			r.BeatTimesSec = append(r.BeatTimesSec, v)
		}
		if len(r.BeatTimesSec) > 0 {
			r.BeatTimesSource = ReliabilityMeasured
		}
	}
	return r
}

func parsePercussive(text string) *Percussive {
	if !strings.Contains(text, "Percussive") {
		return nil
	}
	p := &Percussive{}
	found := false
	if m := rePercRatio.FindStringSubmatch(text); len(m) == 2 {
		p.PercussiveRatio, _ = strconv.ParseFloat(m[1], 64)
		found = true
	}
	if m := reOnsetDens.FindStringSubmatch(text); len(m) == 2 {
		p.OnsetDensity, _ = strconv.ParseFloat(m[1], 64)
		found = true
	}
	if m := reAttack.FindStringSubmatch(text); len(m) == 2 {
		p.PeakAttackSharp, _ = strconv.ParseFloat(m[1], 64)
		found = true
	}
	if !found {
		return nil
	}
	return p
}

func parseSections(text string) []SectionBoundary {
	var out []SectionBoundary
	for _, m := range reSection.FindAllStringSubmatch(text, -1) {
		if len(m) != 5 {
			continue
		}
		sec := 0.0
		if m[1] != "" {
			mins, _ := strconv.ParseFloat(m[1], 64)
			sec = mins * 60
		}
		frac, _ := strconv.ParseFloat(m[2], 64)
		conf, _ := strconv.ParseFloat(m[4], 64)
		out = append(out, SectionBoundary{
			TimeSec:    sec + frac,
			Reasons:    strings.TrimSpace(m[3]),
			Confidence: conf,
		})
	}
	return out
}

func parseMasking(text string) *MaskingAnalysis {
	idx := strings.Index(text, "Frequency Masking")
	if idx < 0 {
		return nil
	}
	section := text[idx:]
	if e := strings.Index(section[1:], "\n──"); e >= 0 {
		section = section[:e+1]
	}
	m := &MaskingAnalysis{}
	mode := "" // crowding | hp | bleed
	for _, line := range strings.Split(section, "\n") {
		trim := strings.TrimSpace(line)
		lower := strings.ToLower(trim)
		switch {
		case strings.Contains(lower, "crowding"):
			mode = "crowding"
			continue
		case strings.Contains(lower, "h/p collision") || strings.Contains(lower, "harmonic + percussive"):
			mode = "hp"
			continue
		case strings.Contains(lower, "cross-band bleed") || strings.Contains(lower, "cross band bleed"):
			mode = "bleed"
			continue
		case trim == "" || strings.HasPrefix(trim, "──") || strings.Contains(lower, "frequency masking"):
			continue
		}
		switch mode {
		case "crowding":
			if mm := reMaskCrowding.FindStringSubmatch(trim); len(mm) == 4 {
				score, _ := strconv.ParseFloat(mm[2], 64)
				m.BandCrowding = append(m.BandCrowding, BandCrowding{
					Band:  normalizeBandName(mm[1]),
					Score: score,
					Label: mm[3],
				})
			}
		case "hp":
			if mm := reMaskHP.FindStringSubmatch(trim); len(mm) == 4 {
				score, _ := strconv.ParseFloat(mm[2], 64)
				m.HPCollision = append(m.HPCollision, HPCollision{
					Band:  normalizeBandName(mm[1]),
					Score: score,
					Label: mm[3],
				})
			}
		case "bleed":
			if mm := reMaskBleed.FindStringSubmatch(trim); len(mm) == 4 {
				score, _ := strconv.ParseFloat(mm[2], 64)
				m.CrossBleed = append(m.CrossBleed, CrossBandBleed{
					Pair:  mm[1],
					Score: score,
					Label: mm[3],
				})
			}
		}
	}
	if len(m.BandCrowding) == 0 && len(m.HPCollision) == 0 && len(m.CrossBleed) == 0 {
		return nil
	}
	return m
}

func firstPath(text string) string {
	if m := reFullFile.FindStringSubmatch(text); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := reFilePath.FindStringSubmatch(text); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
