package audio

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	clipThreshold     = 0.9995
	clipMinRunSamples = 48 // ~1 ms at 48 kHz; ignore transient single-sample peaks
	timelineBuckets   = 400
)

var (
	reEburM     = regexp.MustCompile(`(?i)\bt:\s*([\d.]+)\s+.*?M:\s*([-\d.]+)`)
	reFFmpegVer = regexp.MustCompile(`(?i)ffmpeg version\s+(\S+)`)

	vizMu sync.Mutex // serialize heavy inspect work per process
)

// ClipEvent is a run of near-full-scale samples.
type ClipEvent struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Samples   int     `json:"samples"`
	PeakAbs   float64 `json:"peak_abs"`
}

// TimelineSeries is a downsampled envelope for UI strips.
type TimelineSeries struct {
	DurationSec float64   `json:"duration_sec"`
	TimesSec    []float64 `json:"times_sec"`
	Values      []float64 `json:"values"`
	Units       string    `json:"units"`
	Method      string    `json:"method"`
	Status      string    `json:"status"` // measured | not_performed
	Error       string    `json:"error,omitempty"`
}

// EnergyTimeline is a custom 0–100 within-track RMS percentile score.
type EnergyTimeline struct {
	DurationSec float64   `json:"duration_sec"`
	TimesSec    []float64 `json:"times_sec"`
	Energy0to100 []float64 `json:"energy_0_100"`
	Label       string    `json:"label"`
	Method      string    `json:"method"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
}

// DetectClipping streams native-rate mono f32le via ffmpeg and flags full-scale runs.
func DetectClipping(ctx context.Context, path string) ([]ClipEvent, error) {
	if path == "" {
		return nil, fmt.Errorf("path required")
	}
	if _, err := lookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin", "-v", "error",
		"-i", path,
		"-ac", "1",
		"-f", "f32le",
		"pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderrBuf []byte
	cmd.Stderr = &collectWriter{buf: &stderrBuf}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}

	// Probe sample rate via ffprobe-less path: ask ffmpeg for rate in a separate call is heavy;
	// decode at native rate and estimate from duration after. Use astats-free approach:
	// sample rate from a short ffprobe-like ffmpeg print.
	rate, rateErr := probeSampleRate(ctx, path)
	if rateErr != nil || rate <= 0 {
		rate = 44100
	}

	buf := make([]byte, 8192*4)
	var (
		events    []ClipEvent
		runStart  = -1
		runPeak   float64
		runLen    int
		sampleIdx int64
	)
	flush := func(endIdx int64) {
		if runLen >= clipMinRunSamples && runStart >= 0 {
			events = append(events, ClipEvent{
				StartTime: float64(runStart) / float64(rate),
				EndTime:   float64(endIdx) / float64(rate),
				Samples:   runLen,
				PeakAbs:   runPeak,
			})
		}
		runStart = -1
		runLen = 0
		runPeak = 0
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = cmd.Process.Kill()
			return nil, err
		}
		n, readErr := stdout.Read(buf)
		usable := n - (n % 4)
		for i := 0; i < usable; i += 4 {
			bits := binary.LittleEndian.Uint32(buf[i : i+4])
			v := float64(math.Float32frombits(bits))
			abs := v
			if abs < 0 {
				abs = -abs
			}
			if abs >= clipThreshold {
				if runStart < 0 {
					runStart = int(sampleIdx)
					runPeak = abs
					runLen = 1
				} else {
					runLen++
					if abs > runPeak {
						runPeak = abs
					}
				}
			} else if runStart >= 0 {
				flush(sampleIdx)
			}
			sampleIdx++
		}
		if readErr != nil {
			break
		}
	}
	if runStart >= 0 {
		flush(sampleIdx)
	}
	waitErr := cmd.Wait()
	if waitErr != nil && len(events) == 0 && sampleIdx == 0 {
		return nil, cmdError("ffmpeg clipping", stderrBuf, waitErr)
	}
	return events, nil
}

func probeSampleRate(ctx context.Context, path string) (int, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-i", path, "-f", "null", "-")
	var stderrBuf []byte
	cmd.Stderr = &collectWriter{buf: &stderrBuf}
	_ = cmd.Run()
	re := regexp.MustCompile(`(?i)(\d+)\s*Hz`)
	m := re.FindStringSubmatch(string(stderrBuf))
	if len(m) != 2 {
		return 0, fmt.Errorf("sample rate not found")
	}
	return strconv.Atoi(m[1])
}

// ComputeRMSTimeline downsamples mono PCM to bucketed RMS values.
func ComputeRMSTimeline(ctx context.Context, path string, buckets int) (TimelineSeries, error) {
	out := TimelineSeries{
		Units:  "rms_linear",
		Method: "ffmpeg f32le mono @8kHz bucketed RMS",
		Status: ReliabilityNotPerf,
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
		"-ar", fmt.Sprintf("%d", peakSampleRate),
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
	samples, err := readF32LE(stdout)
	waitErr := cmd.Wait()
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	if waitErr != nil && len(samples) == 0 {
		out.Error = cmdError("ffmpeg rms", stderrBuf, waitErr).Error()
		return out, nil
	}
	if len(samples) == 0 {
		out.Error = "no samples"
		return out, nil
	}
	dur := float64(len(samples)) / float64(peakSampleRate)
	times := make([]float64, buckets)
	vals := make([]float64, buckets)
	n := len(samples)
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
			v := float64(samples[j])
			sum += v * v
		}
		rms := math.Sqrt(sum / float64(end-start))
		times[i] = (float64(start) + float64(end-start)/2) / float64(peakSampleRate)
		vals[i] = rms
	}
	out.DurationSec = dur
	out.TimesSec = times
	out.Values = vals
	out.Status = ReliabilityMeasured
	return out, nil
}

// ComputeEnergyTimeline maps RMS to a custom within-track 0–100 percentile score.
func ComputeEnergyTimeline(ctx context.Context, path string, buckets int) (EnergyTimeline, error) {
	rms, err := ComputeRMSTimeline(ctx, path, buckets)
	out := EnergyTimeline{
		Label:  "custom within-track RMS percentile (not a calibrated loudness meter)",
		Method: "percentile rank of bucketed RMS within this file",
		Status: ReliabilityNotPerf,
	}
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	if rms.Status != ReliabilityMeasured {
		out.Error = rms.Error
		if out.Error == "" {
			out.Error = "rms not measured"
		}
		return out, nil
	}
	sorted := append([]float64(nil), rms.Values...)
	// rank each value
	energy := make([]float64, len(rms.Values))
	for i, v := range rms.Values {
		rank := 0
		for _, o := range sorted {
			if o <= v {
				rank++
			}
		}
		energy[i] = 100 * float64(rank) / float64(len(sorted))
	}
	out.DurationSec = rms.DurationSec
	out.TimesSec = rms.TimesSec
	out.Energy0to100 = energy
	out.Status = ReliabilityMeasured
	return out, nil
}

// ComputeLUFSTimeline parses ffmpeg ebur128 momentary loudness frames.
func ComputeLUFSTimeline(ctx context.Context, path string) (TimelineSeries, error) {
	out := TimelineSeries{
		Units:  "LUFS",
		Method: "ffmpeg ebur128 momentary (M)",
		Status: ReliabilityNotPerf,
	}
	if _, err := lookPath("ffmpeg"); err != nil {
		out.Error = "ffmpeg not found"
		return out, nil
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin", "-hide_banner",
		"-i", path,
		"-af", "ebur128=peak=true",
		"-f", "null", "-",
	)
	var stderrBuf []byte
	cmd.Stderr = &collectWriter{buf: &stderrBuf}
	_ = cmd.Run()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	text := string(stderrBuf)
	var times, vals []float64
	for _, line := range strings.Split(text, "\n") {
		m := reEburM.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		t, err1 := strconv.ParseFloat(m[1], 64)
		v, err2 := strconv.ParseFloat(m[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		if math.IsInf(v, 0) || math.IsNaN(v) {
			continue
		}
		times = append(times, t)
		vals = append(vals, v)
	}
	if len(times) == 0 {
		out.Error = "no ebur128 momentary frames (filter missing or silent file)"
		return out, nil
	}
	out.TimesSec = times
	out.Values = vals
	out.DurationSec = times[len(times)-1]
	out.Status = ReliabilityMeasured
	return out, nil
}

// SpectrogramPNG writes a log-frequency spectrogram PNG via showspectrumpic.
func SpectrogramPNG(ctx context.Context, path, outPNG string) error {
	if _, err := lookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found")
	}
	if err := os.MkdirAll(filepath.Dir(outPNG), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin", "-y", "-v", "error",
		"-i", path,
		"-lavfi", "showspectrumpic=s=1280x512:mode=combined:color=intensity:scale=log",
		outPNG,
	)
	var stderrBuf []byte
	cmd.Stderr = &collectWriter{buf: &stderrBuf}
	if err := cmd.Run(); err != nil {
		return cmdError("ffmpeg spectrogram", stderrBuf, err)
	}
	return nil
}

// CachedSpectrogramPath returns a cache path under configDir/cache/audio-viz.
func CachedSpectrogramPath(configDir, audioPath string, size, mtime int64) string {
	sum := sha1Short(fmt.Sprintf("%s|%d|%d", audioPath, size, mtime))
	return filepath.Join(configDir, "cache", "audio-viz", sum+".png")
}

func sha1Short(s string) string {
	h := sha256.Sum256([]byte(s))
	return hexEncode(h[:12])
}

func hexEncode(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

// FFmpegVersion returns a short version string or empty.
func FFmpegVersion(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	m := reFFmpegVer.FindStringSubmatch(string(out))
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// WithVizLock serializes heavy ffmpeg inspect work.
func WithVizLock(fn func() error) error {
	vizMu.Lock()
	defer vizMu.Unlock()
	return fn()
}
