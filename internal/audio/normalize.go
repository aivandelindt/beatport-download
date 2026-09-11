package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type NormalizeRequest struct {
	Input      string
	Output     string // empty → sidecar: name_normalized + ext
	TargetLUFS float64
	TruePeak   float64
	LRA        float64
	Overwrite  bool
}

type NormalizeResult struct {
	Input       string  `json:"input"`
	Output      string  `json:"output"`
	MeasuredI   float64 `json:"measured_i"`
	MeasuredTP  float64 `json:"measured_tp"`
	MeasuredLRA float64 `json:"measured_lra"`
}

type loudnormMeasure struct {
	InputI       float64
	InputTP      float64
	InputLRA     float64
	InputThresh  float64
	TargetOffset float64
}

type loudnormMeasureRaw struct {
	InputI       flexFloat `json:"input_i"`
	InputTP      flexFloat `json:"input_tp"`
	InputLRA     flexFloat `json:"input_lra"`
	InputThresh  flexFloat `json:"input_thresh"`
	TargetOffset flexFloat `json:"target_offset"`
}

type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexFloat(v)
	return nil
}

var reLoudnormJSON = regexp.MustCompile(`(?s)\{[^{}]*"input_i"[^{}]*\}`)

// SidecarNormalizedPath returns path with _normalized before the extension.
func SidecarNormalizedPath(input string) string {
	ext := filepath.Ext(input)
	base := strings.TrimSuffix(input, ext)
	return base + "_normalized" + ext
}

// ParseLoudnormJSON extracts the measurement object from ffmpeg stderr.
func ParseLoudnormJSON(stderr string) (loudnormMeasure, error) {
	m := reLoudnormJSON.FindString(stderr)
	if m == "" {
		return loudnormMeasure{}, fmt.Errorf("loudnorm JSON not found in ffmpeg output")
	}
	var raw loudnormMeasureRaw
	if err := json.Unmarshal([]byte(m), &raw); err != nil {
		return loudnormMeasure{}, fmt.Errorf("decode loudnorm JSON: %w", err)
	}
	return loudnormMeasure{
		InputI:       float64(raw.InputI),
		InputTP:      float64(raw.InputTP),
		InputLRA:     float64(raw.InputLRA),
		InputThresh:  float64(raw.InputThresh),
		TargetOffset: float64(raw.TargetOffset),
	}, nil
}

// Normalize runs two-pass ffmpeg loudnorm.
func Normalize(ctx context.Context, req NormalizeRequest) (NormalizeResult, error) {
	if req.Input == "" {
		return NormalizeResult{}, fmt.Errorf("input required")
	}
	if _, err := lookPath("ffmpeg"); err != nil {
		return NormalizeResult{}, fmt.Errorf("ffmpeg not found in PATH")
	}
	targetI := req.TargetLUFS
	if targetI == 0 {
		targetI = -14
	}
	tp := req.TruePeak
	if tp == 0 {
		tp = -1.5
	}
	lra := req.LRA
	if lra == 0 {
		lra = 11
	}

	outPath := req.Output
	if outPath == "" {
		outPath = SidecarNormalizedPath(req.Input)
	}
	if !req.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return NormalizeResult{}, fmt.Errorf("output exists (overwrite disabled): %s", outPath)
		}
	}

	filter1 := fmt.Sprintf("loudnorm=I=%.1f:TP=%.1f:LRA=%.1f:print_format=json", targetI, tp, lra)
	_, stderr1, err := runCmd(ctx, "ffmpeg", []string{
		"-nostdin", "-hide_banner", "-i", req.Input,
		"-af", filter1, "-f", "null", "-",
	}, nil)
	if err != nil {
		// ffmpeg still prints JSON on stderr even when exit is non-zero sometimes
		meas, parseErr := ParseLoudnormJSON(string(stderr1))
		if parseErr != nil {
			return NormalizeResult{}, cmdError("ffmpeg loudnorm pass1", stderr1, err)
		}
		_ = meas
	}
	meas, err := ParseLoudnormJSON(string(stderr1))
	if err != nil {
		return NormalizeResult{}, err
	}

	writePath := outPath
	var tmpPath string
	if req.Overwrite && outPath == req.Input {
		tmpPath = outPath + ".tmp-normalize"
		writePath = tmpPath
	}

	codecArgs := normalizeCodecArgs(req.Input)
	filter2 := fmt.Sprintf(
		"loudnorm=I=%.1f:TP=%.1f:LRA=%.1f:measured_I=%.2f:measured_TP=%.2f:measured_LRA=%.2f:measured_thresh=%.2f:offset=%.2f:linear=true",
		targetI, tp, lra,
		meas.InputI, meas.InputTP, meas.InputLRA, meas.InputThresh, meas.TargetOffset,
	)
	args := []string{"-nostdin", "-hide_banner", "-y", "-i", req.Input, "-af", filter2, "-map_metadata", "0"}
	args = append(args, codecArgs...)
	args = append(args, writePath)

	_, stderr2, err := runCmd(ctx, "ffmpeg", args, nil)
	if err != nil {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
		return NormalizeResult{}, cmdError("ffmpeg loudnorm pass2", stderr2, err)
	}
	if tmpPath != "" {
		if err := os.Rename(tmpPath, outPath); err != nil {
			_ = os.Remove(tmpPath)
			return NormalizeResult{}, err
		}
	}

	return NormalizeResult{
		Input:       req.Input,
		Output:      outPath,
		MeasuredI:   meas.InputI,
		MeasuredTP:  meas.InputTP,
		MeasuredLRA: meas.InputLRA,
	}, nil
}

func normalizeCodecArgs(input string) []string {
	switch strings.ToLower(filepath.Ext(input)) {
	case ".flac":
		return []string{"-c:a", "flac"}
	case ".m4a", ".aac":
		return []string{"-c:a", "aac", "-b:a", "256k"}
	case ".mp3":
		return []string{"-c:a", "libmp3lame", "-q:a", "2"}
	case ".wav":
		return []string{"-c:a", "pcm_s16le"}
	case ".ogg":
		return []string{"-c:a", "libvorbis", "-q:a", "6"}
	default:
		return []string{"-c:a", "flac"}
	}
}
