package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
)

const (
	DefaultPeakBuckets = 1200
	peakSampleRate     = 8000
)

// Peaks is a downsampled waveform for canvas drawing (WaveSurfer-compatible min/max pairs).
type Peaks struct {
	DurationSec float64   `json:"duration_sec"`
	Peaks       []float64 `json:"peaks"` // interleaved [min, max, min, max, ...] in -1..1
}

// ComputePeaks downsamples audio via ffmpeg to mono f32le @ 8kHz and buckets min/max pairs.
func ComputePeaks(ctx context.Context, path string, buckets int) (Peaks, error) {
	if path == "" {
		return Peaks{}, fmt.Errorf("path required")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return Peaks{}, err
	}
	if fi.IsDir() || fi.Size() == 0 {
		return Peaks{}, fmt.Errorf("empty or invalid audio file: %s", path)
	}
	if buckets <= 0 {
		buckets = DefaultPeakBuckets
	}
	if _, err := lookPath("ffmpeg"); err != nil {
		return Peaks{}, fmt.Errorf("ffmpeg not found (required for waveforms)")
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
		return Peaks{}, err
	}
	var stderrBuf []byte
	cmd.Stderr = &collectWriter{buf: &stderrBuf}
	if err := cmd.Start(); err != nil {
		return Peaks{}, fmt.Errorf("ffmpeg start: %w", err)
	}

	samples, err := readF32LE(stdout)
	waitErr := cmd.Wait()
	if err != nil {
		return Peaks{}, err
	}
	if waitErr != nil && len(samples) == 0 {
		return Peaks{}, cmdError("ffmpeg peaks", stderrBuf, waitErr)
	}
	if len(samples) == 0 {
		return Peaks{}, fmt.Errorf("no PCM samples from ffmpeg")
	}

	peaks := bucketPeaks(samples, buckets)
	dur := float64(len(samples)) / float64(peakSampleRate)
	return Peaks{DurationSec: dur, Peaks: peaks}, nil
}

type collectWriter struct {
	buf *[]byte
}

func (w *collectWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

func readF32LE(r io.Reader) ([]float32, error) {
	const chunk = 8192 * 4
	buf := make([]byte, chunk)
	var samples []float32
	for {
		n, err := r.Read(buf)
		if n > 0 {
			usable := n - (n % 4)
			for i := 0; i < usable; i += 4 {
				bits := binary.LittleEndian.Uint32(buf[i : i+4])
				samples = append(samples, math.Float32frombits(bits))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return samples, err
		}
	}
	return samples, nil
}

// bucketPeaks turns samples into interleaved [min,max] pairs normalized to -1..1.
func bucketPeaks(samples []float32, buckets int) []float64 {
	if buckets <= 0 || len(samples) == 0 {
		return nil
	}
	out := make([]float64, 0, buckets*2)
	n := len(samples)
	var peakAbs float32
	mins := make([]float32, buckets)
	maxs := make([]float32, buckets)
	for i := 0; i < buckets; i++ {
		start := i * n / buckets
		end := (i + 1) * n / buckets
		if end <= start {
			end = start + 1
		}
		if end > n {
			end = n
		}
		mn := samples[start]
		mx := samples[start]
		for j := start; j < end; j++ {
			v := samples[j]
			if v < mn {
				mn = v
			}
			if v > mx {
				mx = v
			}
			av := v
			if av < 0 {
				av = -av
			}
			if av > peakAbs {
				peakAbs = av
			}
		}
		mins[i] = mn
		maxs[i] = mx
	}
	scale := float32(1)
	if peakAbs > 0 {
		scale = 1 / peakAbs
	}
	for i := 0; i < buckets; i++ {
		out = append(out, float64(mins[i]*scale), float64(maxs[i]*scale))
	}
	return out
}
