package audio

import "strings"

// camelotWheel maps (pitch class, mode) → Camelot code.
// A = minor, B = major. Matches Mixed In Key convention.
var camelotWheel = map[string]string{
	"C|major":  "8B",
	"C|minor":  "5A",
	"C#|major": "3B",
	"C#|minor": "12A",
	"DB|major": "3B",
	"DB|minor": "12A",
	"D|major":  "10B",
	"D|minor":  "7A",
	"D#|major": "5B",
	"D#|minor": "2A",
	"EB|major": "5B",
	"EB|minor": "2A",
	"E|major":  "12B",
	"E|minor":  "9A",
	"F|major":  "7B",
	"F|minor":  "4A",
	"F#|major": "2B",
	"F#|minor": "11A",
	"GB|major": "2B",
	"GB|minor": "11A",
	"G|major":  "9B",
	"G|minor":  "6A",
	"G#|major": "4B",
	"G#|minor": "1A",
	"AB|major": "4B",
	"AB|minor": "1A",
	"A|major":  "11B",
	"A|minor":  "8A",
	"A#|major": "6B",
	"A#|minor": "3A",
	"BB|major": "6B",
	"BB|minor": "3A",
	"B|major":  "1B",
	"B|minor":  "10A",
}

// CamelotFromKeyMode returns a Camelot code for major/minor key estimates.
// Empty string when key or mode is missing or not major/minor.
func CamelotFromKeyMode(key, mode string) string {
	key = normalizePitchClass(key)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if key == "" || (mode != "major" && mode != "minor") {
		return ""
	}
	return camelotWheel[key+"|"+mode]
}

func normalizePitchClass(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "♯", "#")
	key = strings.ReplaceAll(key, "♭", "b")
	if key == "" {
		return ""
	}
	lower := strings.ToLower(key)
	letter := strings.ToUpper(string(lower[0]))
	if len(lower) == 1 {
		return letter
	}
	switch lower[1] {
	case '#':
		return letter + "#"
	case 'b':
		return letter + "B"
	}
	return letter
}

// TempoAlternatives returns half-time and double-time BPM interpretations.
func TempoAlternatives(bpm float64) (half, double float64) {
	if bpm <= 0 {
		return 0, 0
	}
	return bpm / 2, bpm * 2
}

// EstimateBeatGrid fills beat times from measured first beats plus a steady
// grid using median (or main) BPM for the rest of the duration.
func EstimateBeatGrid(measured []float64, bpm, durationSec float64) []float64 {
	if bpm <= 0 || durationSec <= 0 {
		return nil
	}
	interval := 60.0 / bpm
	if interval <= 0 {
		return nil
	}
	var start float64
	if len(measured) > 0 {
		start = measured[0]
	}
	var out []float64
	for t := start; t < durationSec+interval*0.25; t += interval {
		if t < 0 {
			continue
		}
		if t > durationSec {
			break
		}
		// Skip times already covered by measured first beats (within 40ms).
		if coveredByMeasured(measured, t, 0.04) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func coveredByMeasured(measured []float64, t, tol float64) bool {
	for _, m := range measured {
		d := t - m
		if d < 0 {
			d = -d
		}
		if d <= tol {
			return true
		}
	}
	return false
}
