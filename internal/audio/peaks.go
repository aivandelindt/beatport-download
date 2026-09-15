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
	peakRGBSampleRate  = 16000
	// Pioneer/rekordbox-style 3-band RGB crossovers.
	rgbLowCutHz  = 250
	rgbHighCutHz = 4000
)

// Peaks is a downsampled waveform for canvas drawing (WaveSurfer-compatible min/max pairs).
// RGBLow/Mid/High are rekordbox-style band energies (0–1) aligned with peak buckets:
// red = bass, green = mids, blue = highs.
type Peaks struct {
	DurationSec float64   `json:"duration_sec"`
	Peaks       []float64 `json:"peaks"` // interleaved [min, max, min, max, ...] in -1..1
	RGBLow      []float64 `json:"rgb_low,omitempty"`
	RGBMid      []float64 `json:"rgb_mid,omitempty"`
	RGBHigh     []float64 `json:"rgb_high,omitempty"`
}

// ComputePeaks downsamples audio via ffmpeg and buckets min/max pairs, plus 3-band RGB energies.
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

	if p, err := computeRGBPeaks(ctx, path, buckets); err == nil && len(p.Peaks) > 0 {
		return p, nil
	}

	samples, stderrBuf, waitErr, err := decodeF32LE(ctx, path,
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", peakSampleRate),
		"-f", "f32le",
		"pipe:1",
	)
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

func computeRGBPeaks(ctx context.Context, path string, buckets int) (Peaks, error) {
	filter := fmt.Sprintf(
		"[0:a]aformat=channel_layouts=mono,aresample=%d,asplit=4[o][l][m][h];"+
			"[l]lowpass=f=%d:poles=2[low];"+
			"[m]highpass=f=%d:poles=2,lowpass=f=%d:poles=2[mid];"+
			"[h]highpass=f=%d:poles=2[high];"+
			"[o][low][mid][high]join=inputs=4:channel_layout=quad[out]",
		peakRGBSampleRate, rgbLowCutHz, rgbLowCutHz, rgbHighCutHz, rgbHighCutHz,
	)
	samples, stderrBuf, waitErr, err := decodeF32LE(ctx, path,
		"-filter_complex", filter,
		"-map", "[out]",
		"-f", "f32le",
		"pipe:1",
	)
	if err != nil {
		return Peaks{}, err
	}
	if waitErr != nil && len(samples) == 0 {
		return Peaks{}, cmdError("ffmpeg rgb peaks", stderrBuf, waitErr)
	}
	if len(samples) < 4 {
		return Peaks{}, fmt.Errorf("no RGB PCM samples from ffmpeg")
	}
	orig, low, mid, high := splitQuad(samples)
	if len(orig) == 0 {
		return Peaks{}, fmt.Errorf("empty RGB peak channels")
	}
	peaks, rgbL, rgbM, rgbH := bucketMinMaxAndRGB(orig, low, mid, high, buckets)
	dur := float64(len(orig)) / float64(peakRGBSampleRate)
	return Peaks{
		DurationSec: dur,
		Peaks:       peaks,
		RGBLow:      rgbL,
		RGBMid:      rgbM,
		RGBHigh:     rgbH,
	}, nil
}

func decodeF32LE(ctx context.Context, path string, extra ...string) (samples []float32, stderr []byte, waitErr, err error) {
	args := append([]string{"-nostdin", "-v", "error", "-i", path}, extra...)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	var stderrBuf []byte
	cmd.Stderr = &collectWriter{buf: &stderrBuf}
	if err := cmd.Start(); err != nil {
		return nil, stderrBuf, nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	samples, err = readF32LE(stdout)
	waitErr = cmd.Wait()
	return samples, stderrBuf, waitErr, err
}

func splitQuad(samples []float32) (orig, low, mid, high []float32) {
	n := len(samples) / 4
	orig = make([]float32, n)
	low = make([]float32, n)
	mid = make([]float32, n)
	high = make([]float32, n)
	for i := 0; i < n; i++ {
		base := i * 4
		orig[i] = samples[base]
		low[i] = samples[base+1]
		mid[i] = samples[base+2]
		high[i] = samples[base+3]
	}
	return orig, low, mid, high
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
	peaks, _, _, _ := bucketMinMaxAndRGB(samples, nil, nil, nil, buckets)
	return peaks
}

func bucketMinMaxAndRGB(orig, low, mid, high []float32, buckets int) (peaks, rgbL, rgbM, rgbH []float64) {
	if buckets <= 0 || len(orig) == 0 {
		return nil, nil, nil, nil
	}
	n := len(orig)
	haveRGB := len(low) == n && len(mid) == n && len(high) == n
	mins := make([]float32, buckets)
	maxs := make([]float32, buckets)
	var peakAbs float32
	var rgbPeak float32
	loB := make([]float32, buckets)
	miB := make([]float32, buckets)
	hiB := make([]float32, buckets)

	for i := 0; i < buckets; i++ {
		start := i * n / buckets
		end := (i + 1) * n / buckets
		if end <= start {
			end = start + 1
		}
		if end > n {
			end = n
		}
		mn := orig[start]
		mx := orig[start]
		var loMax, miMax, hiMax float32
		for j := start; j < end; j++ {
			v := orig[j]
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
			if haveRGB {
				loMax = maxAbs(loMax, low[j])
				miMax = maxAbs(miMax, mid[j])
				hiMax = maxAbs(hiMax, high[j])
			}
		}
		mins[i] = mn
		maxs[i] = mx
		if haveRGB {
			loB[i] = loMax
			miB[i] = miMax
			hiB[i] = hiMax
			if loMax > rgbPeak {
				rgbPeak = loMax
			}
			if miMax > rgbPeak {
				rgbPeak = miMax
			}
			if hiMax > rgbPeak {
				rgbPeak = hiMax
			}
		}
	}
	scale := float32(1)
	if peakAbs > 0 {
		scale = 1 / peakAbs
	}
	peaks = make([]float64, 0, buckets*2)
	for i := 0; i < buckets; i++ {
		peaks = append(peaks, float64(mins[i]*scale), float64(maxs[i]*scale))
	}
	if !haveRGB {
		return peaks, nil, nil, nil
	}
	rgbScale := float32(1)
	if rgbPeak > 0 {
		rgbScale = 1 / rgbPeak
	}
	rgbL = make([]float64, buckets)
	rgbM = make([]float64, buckets)
	rgbH = make([]float64, buckets)
	for i := 0; i < buckets; i++ {
		rgbL[i] = float64(loB[i] * rgbScale)
		rgbM[i] = float64(miB[i] * rgbScale)
		rgbH[i] = float64(hiB[i] * rgbScale)
	}
	return peaks, rgbL, rgbM, rgbH
}

func maxAbs(cur, v float32) float32 {
	if v < 0 {
		v = -v
	}
	if v > cur {
		return v
	}
	return cur
}
