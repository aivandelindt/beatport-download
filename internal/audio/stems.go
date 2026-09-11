package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type StemRequest struct {
	Input     string
	OutputDir string // default: <input_dir>/<basename>_stems
	BinPath   string
	Provider  string // auto|coreml|xnnpack|cpu
}

type StemResult struct {
	Vocals string `json:"vocals"`
	Drums  string `json:"drums"`
	Bass   string `json:"bass"`
	Other  string `json:"other"`
}

// DefaultStemProvider returns the arch-appropriate default provider.
func DefaultStemProvider() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "coreml"
	}
	return "auto"
}

// DefaultStemOutputDir returns <dir>/<basename>_stems.
func DefaultStemOutputDir(input string) string {
	dir := filepath.Dir(input)
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	return filepath.Join(dir, base+"_stems")
}

// ResolveStemSplitterBin finds the stem-splitter binary.
func ResolveStemSplitterBin(configured string) string {
	if configured != "" {
		if ok, _ := lookPathStat(configured); ok {
			return configured
		}
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "stem-splitter")
		if ok, _ := lookPathStat(p); ok {
			return p
		}
	}
	candidates := []string{
		"dist/tools/bin/stem-splitter",
		filepath.Join("dist", "tools", "bin", "stem-splitter"),
	}
	if cwd, err := os.Getwd(); err == nil {
		for _, rel := range candidates {
			p := filepath.Join(cwd, rel)
			if ok, _ := lookPathStat(p); ok {
				return p
			}
		}
	}
	if p, err := lookPath("stem-splitter"); err == nil {
		return p
	}
	return ""
}

// StemEnv returns process env for the given provider (full env slice for cmd.Env).
func StemEnv(provider string, base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	env := append([]string{}, base...)
	switch strings.ToLower(provider) {
	case "coreml":
		env = append(env, "STEMMER_EP_FORCE=coreml")
	case "xnnpack":
		env = append(env, "STEMMER_EP_FORCE=xnnpack")
	case "cpu":
		env = append(env, "STEMMER_FORCE_CPU=1")
	case "auto", "":
		// library auto-selects
	}
	return env
}

// ParseStemQuietOutput parses four stem paths from quiet-mode stdout.
func ParseStemQuietOutput(stdout string) (StemResult, error) {
	var lines []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) < 4 {
		return StemResult{}, fmt.Errorf("expected 4 stem paths, got %d", len(lines))
	}
	return StemResult{
		Vocals: lines[0],
		Drums:  lines[1],
		Bass:   lines[2],
		Other:  lines[3],
	}, nil
}

// SplitStems runs stem-splitter split --quiet.
func SplitStems(ctx context.Context, req StemRequest) (StemResult, error) {
	bin := ResolveStemSplitterBin(req.BinPath)
	if bin == "" {
		return StemResult{}, fmt.Errorf("stem-splitter binary not found (run: make stem-splitter)")
	}
	outDir := req.OutputDir
	if outDir == "" {
		outDir = DefaultStemOutputDir(req.Input)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return StemResult{}, err
	}
	provider := req.Provider
	if provider == "" || provider == "auto" {
		// When UI sends auto, apply arch default for Apple Silicon CoreML preference.
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			provider = "coreml"
		} else {
			provider = "auto"
		}
	}
	env := StemEnv(provider, nil)
	args := []string{"split", "--input", req.Input, "--output", outDir, "--quiet"}
	stdout, stderr, err := runCmd(ctx, bin, args, env)
	if err != nil {
		return StemResult{}, cmdError("stem-splitter", stderr, err)
	}
	result, err := ParseStemQuietOutput(string(stdout))
	if err != nil {
		return StemResult{}, fmt.Errorf("%w (stderr: %s)", err, truncate(string(stderr), 300))
	}
	return result, nil
}
