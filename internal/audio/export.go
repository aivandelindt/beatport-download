package audio

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ResearchBundle is the on-demand research payload for the Library inspector.
type ResearchBundle struct {
	Path            string           `json:"path"`
	Camelot         string           `json:"camelot,omitempty"`
	TempoBPM        float64          `json:"tempo_bpm,omitempty"`
	TempoHalfBPM    float64          `json:"tempo_half_bpm,omitempty"`
	TempoDoubleBPM  float64          `json:"tempo_double_bpm,omitempty"`
	BeatTimesMeasured []float64      `json:"beat_times_measured,omitempty"`
	BeatGridEstimated []float64      `json:"beat_grid_estimated,omitempty"`
	LabeledSections []LabeledSection `json:"labeled_sections,omitempty"`
	Issues          []Finding        `json:"issues,omitempty"`
	RMS             TimelineSeries   `json:"rms"`
	LUFS            TimelineSeries   `json:"lufs"`
	Energy          EnergyTimeline   `json:"energy"`
	Clipping        []ClipEvent      `json:"clipping,omitempty"`
	ClippingStatus  string           `json:"clipping_status"`
	ClippingError   string           `json:"clipping_error,omitempty"`
	SpectrogramURL  string           `json:"spectrogram_url,omitempty"`
	NotPerformed    []string         `json:"not_performed"`
}

// BuildResearch runs ffmpeg timelines/clipping and merges with stored analysis.
func BuildResearch(ctx context.Context, a Analysis, filePath string) (ResearchBundle, error) {
	EnsureCamelot(&a)
	b := ResearchBundle{
		Path:         filePath,
		NotPerformed: []string{"midi_transcription", "chord_timeline", "vu_meter", "waveform_editing"},
		ClippingStatus: ReliabilityNotPerf,
	}
	if a.HarmonicAnalysis != nil {
		b.Camelot = a.HarmonicAnalysis.Camelot
	}
	if a.RhythmAnalysis != nil {
		r := a.RhythmAnalysis
		b.TempoBPM = r.TempoBPM
		b.TempoHalfBPM = r.TempoHalfBPM
		b.TempoDoubleBPM = r.TempoDoubleBPM
		b.BeatTimesMeasured = r.BeatTimesSec
		b.BeatGridEstimated = r.BeatGridEstimated
		if len(b.BeatGridEstimated) == 0 {
			bpm := r.MedianTempoBPM
			if bpm <= 0 {
				bpm = r.TempoBPM
			}
			var dur float64
			if a.AudioInfo != nil {
				dur = a.AudioInfo.DurationSec
			}
			b.BeatGridEstimated = EstimateBeatGrid(r.BeatTimesSec, bpm, dur)
			b.TempoHalfBPM, b.TempoDoubleBPM = TempoAlternatives(bpm)
		}
	}
	b.Issues = append([]Finding(nil), a.Issues...)
	b.LabeledSections = a.LabeledSections

	var rms TimelineSeries
	var energy EnergyTimeline
	var lufs TimelineSeries
	var clips []ClipEvent
	var clipErr string

	err := WithVizLock(func() error {
		var e error
		rms, e = ComputeRMSTimeline(ctx, filePath, timelineBuckets)
		if e != nil {
			return e
		}
		energy, e = ComputeEnergyTimeline(ctx, filePath, timelineBuckets)
		if e != nil {
			return e
		}
		lufs, _ = ComputeLUFSTimeline(ctx, filePath)
		clips, e = DetectClipping(ctx, filePath)
		if e != nil {
			clipErr = e.Error()
			e = nil
		}
		return nil
	})
	if err != nil {
		return b, err
	}
	b.RMS = rms
	b.Energy = energy
	b.LUFS = lufs
	if clipErr != "" {
		b.ClippingError = clipErr
		b.ClippingStatus = ReliabilityNotPerf
	} else {
		b.Clipping = clips
		b.ClippingStatus = ReliabilityMeasured
		// Cap findings so a brickwalled master does not flood the UI.
		const maxClipFindings = 25
		for i, c := range clips {
			if i >= maxClipFindings {
				st := c.StartTime
				b.Issues = append(b.Issues, Finding{
					Category:        "clipping_or_overload_risk",
					StartTime:       &st,
					ChannelOrStem:   "mix",
					Measurement:     fmt.Sprintf("%d additional runs omitted", len(clips)-maxClipFindings),
					Units:           "full_scale_run",
					Method:          "ffmpeg native-rate f32le threshold 0.9995 min_run=48",
					Reliability:     ReliabilityMeasured,
					SuggestedAction: "Many near-full-scale runs; spot-check the loudest sections. Gain reduction does not restore clipped peaks.",
				})
				break
			}
			st, et := c.StartTime, c.EndTime
			b.Issues = append(b.Issues, Finding{
				Category:        "clipping_or_overload_risk",
				StartTime:       &st,
				EndTime:         &et,
				ChannelOrStem:   "mix",
				Measurement:     fmt.Sprintf("%d samples peak=%.4f", c.Samples, c.PeakAbs),
				Units:           "full_scale_run",
				Method:          "ffmpeg native-rate f32le threshold 0.9995 min_run=48",
				Reliability:     ReliabilityMeasured,
				SuggestedAction: "Near-full-scale sample run detected; review for audible clipping. Gain reduction does not restore clipped peaks.",
			})
		}
	}

	if len(b.LabeledSections) == 0 && len(a.Sections) > 0 && rms.Status == ReliabilityMeasured {
		var dur float64
		if a.AudioInfo != nil {
			dur = a.AudioInfo.DurationSec
		}
		if dur <= 0 {
			dur = rms.DurationSec
		}
		b.LabeledSections = LabelSections(a.Sections, dur, rms.TimesSec, rms.Values)
	}

	return b, nil
}

// WriteExportZip writes research artifacts into a zip archive.
func WriteExportZip(ctx context.Context, w io.Writer, a Analysis, research ResearchBundle, spectrogramPNG string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	add := func(name string, data []byte) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}

	EnsureCamelot(&a)
	analysisJSON, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if err := add("analysis.json", analysisJSON); err != nil {
		return err
	}

	report := buildReportMarkdown(a, research)
	if err := add("report.md", []byte(report)); err != nil {
		return err
	}

	if buf, err := sectionsCSV(research.LabeledSections, a.Sections); err == nil {
		_ = add("sections.csv", buf)
	}
	if buf, err := beatsCSV(research); err == nil {
		_ = add("beats.csv", buf)
	}
	if buf, err := issuesCSV(research.Issues); err == nil {
		_ = add("issues.csv", buf)
	}

	if spectrogramPNG != "" {
		if data, err := os.ReadFile(spectrogramPNG); err == nil {
			_ = add("plots/spectrogram.png", data)
		}
	}

	logObj := map[string]interface{}{
		"exported_at":    time.Now().UTC().Format(time.RFC3339),
		"enrich_version": EnrichVersion,
		"path":           research.Path,
		"not_performed":  research.NotPerformed,
	}
	if a.Meta != nil {
		logObj["meta"] = a.Meta
	}
	logJSON, _ := json.MarshalIndent(logObj, "", "  ")
	_ = add("processing_log.json", logJSON)

	researchJSON, _ := json.MarshalIndent(research, "", "  ")
	_ = add("research.json", researchJSON)

	_ = ctx
	return nil
}

func buildReportMarkdown(a Analysis, r ResearchBundle) string {
	var b strings.Builder
	b.WriteString("# Audio research report\n\n")
	b.WriteString(fmt.Sprintf("- **Path:** `%s`\n", r.Path))
	if a.Meta != nil && a.Meta.FileSHA256 != "" {
		b.WriteString(fmt.Sprintf("- **SHA-256:** `%s`\n", a.Meta.FileSHA256))
	}
	b.WriteString(fmt.Sprintf("- **Kind:** %s\n", a.Kind))
	b.WriteString(fmt.Sprintf("- **Source:** %s\n\n", a.Source))

	b.WriteString("## Measured\n\n")
	if a.AudioInfo != nil {
		b.WriteString(fmt.Sprintf("- Duration: %.2fs, sample rate: %d\n", a.AudioInfo.DurationSec, a.AudioInfo.SampleRate))
	}
	if a.SpectralFeatures != nil {
		b.WriteString(fmt.Sprintf("- LUFS integrated: %.1f, true peak: %.2f dBTP\n", a.SpectralFeatures.LUFSIntegrated, a.SpectralFeatures.TruePeakDBTP))
	}
	if a.RhythmAnalysis != nil {
		b.WriteString(fmt.Sprintf("- Tempo: %.1f BPM (confidence %.3f), beats detected: %d\n", a.RhythmAnalysis.TempoBPM, a.RhythmAnalysis.Confidence, a.RhythmAnalysis.BeatsDetected))
		b.WriteString(fmt.Sprintf("- Measured beat times: %d (analyzer prints first N only)\n", len(a.RhythmAnalysis.BeatTimesSec)))
	}
	if r.ClippingStatus == ReliabilityMeasured {
		b.WriteString(fmt.Sprintf("- Clipping runs (ffmpeg): %d\n", len(r.Clipping)))
	}

	b.WriteString("\n## Estimated\n\n")
	if r.Camelot != "" {
		b.WriteString(fmt.Sprintf("- Camelot: %s\n", r.Camelot))
	}
	if r.TempoHalfBPM > 0 {
		b.WriteString(fmt.Sprintf("- Half/double tempo: %.1f / %.1f BPM\n", r.TempoHalfBPM, r.TempoDoubleBPM))
	}
	b.WriteString(fmt.Sprintf("- Estimated beat grid points: %d\n", len(r.BeatGridEstimated)))
	b.WriteString(fmt.Sprintf("- Labeled sections: %d (heuristic)\n", len(r.LabeledSections)))

	b.WriteString("\n## Not performed\n\n")
	for _, n := range r.NotPerformed {
		b.WriteString(fmt.Sprintf("- %s\n", n))
	}

	b.WriteString("\n## Issues\n\n")
	if len(r.Issues) == 0 {
		b.WriteString("- None flagged\n")
	} else {
		for _, iss := range r.Issues {
			b.WriteString(fmt.Sprintf("- **%s** (%s): %s %s — %s\n", iss.Category, iss.Reliability, iss.Measurement, iss.Units, iss.SuggestedAction))
		}
	}
	b.WriteString("\nOriginal audio was not modified.\n")
	return b.String()
}

func sectionsCSV(labeled []LabeledSection, boundaries []SectionBoundary) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"start_time", "end_time", "label", "evidence", "reliability", "boundary_reasons"})
	for i, s := range labeled {
		reasons := ""
		if i < len(boundaries) {
			reasons = boundaries[i].Reasons
		}
		_ = w.Write([]string{
			fmt.Sprintf("%.3f", s.StartTime),
			fmt.Sprintf("%.3f", s.EndTime),
			s.Label,
			s.Evidence,
			s.Reliability,
			reasons,
		})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func beatsCSV(r ResearchBundle) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"time_sec", "source"})
	for _, t := range r.BeatTimesMeasured {
		_ = w.Write([]string{fmt.Sprintf("%.4f", t), "measured"})
	}
	for _, t := range r.BeatGridEstimated {
		_ = w.Write([]string{fmt.Sprintf("%.4f", t), "estimated"})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func issuesCSV(issues []Finding) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"category", "start_time", "end_time", "channel_or_stem", "measurement", "units", "method", "reliability", "suggested_action"})
	for _, iss := range issues {
		st, et := "", ""
		if iss.StartTime != nil {
			st = strconv.FormatFloat(*iss.StartTime, 'f', 3, 64)
		}
		if iss.EndTime != nil {
			et = strconv.FormatFloat(*iss.EndTime, 'f', 3, 64)
		}
		_ = w.Write([]string{iss.Category, st, et, iss.ChannelOrStem, iss.Measurement, iss.Units, iss.Method, iss.Reliability, iss.SuggestedAction})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// EnsureSpectrogram returns a cached PNG path, generating if needed.
func EnsureSpectrogram(ctx context.Context, configDir, audioPath string, size, mtime int64) (string, error) {
	out := CachedSpectrogramPath(configDir, audioPath, size, mtime)
	if fi, err := os.Stat(out); err == nil && fi.Size() > 0 {
		return out, nil
	}
	var genErr error
	err := WithVizLock(func() error {
		genErr = SpectrogramPNG(ctx, audioPath, out)
		return genErr
	})
	if err != nil {
		return "", err
	}
	return out, genErr
}

// BasenameSafe returns a zip-friendly base name.
func BasenameSafe(path string) string {
	base := filepath.Base(path)
	base = strings.ReplaceAll(base, " ", "_")
	return base
}
