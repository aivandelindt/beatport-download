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
	Vocals string `json:"vocals,omitempty"`
	Drums  string `json:"drums,omitempty"`
	Bass   string `json:"bass,omitempty"`
	Other  string `json:"other,omitempty"`
}

// StemNames are the conventional stem-splitter stem roles.
var StemNames = []string{"vocals", "drums", "bass", "other"}

// mixBasename returns the mix filename without extension.
func mixBasename(mixPath string) string {
	return strings.TrimSuffix(filepath.Base(mixPath), filepath.Ext(mixPath))
}

// stemFileCandidates returns possible on-disk filenames for a stem role.
// stem-splitter writes <basename>_<stem>.wav; older/tests may use <stem>.wav.
func stemFileCandidates(mixPath, stem string) []string {
	stem = strings.ToLower(stem)
	switch stem {
	case "vocals", "drums", "bass", "other":
	default:
		return nil
	}
	dir := DefaultStemOutputDir(mixPath)
	return []string{
		filepath.Join(dir, mixBasename(mixPath)+"_"+stem+".wav"),
		filepath.Join(dir, stem+".wav"),
	}
}

// resolveStemFile returns the first existing candidate path for stem, or "".
func resolveStemFile(mixPath, stem string) string {
	for _, p := range stemFileCandidates(mixPath, stem) {
		fi, err := os.Stat(p)
		if err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// DiscoverStems looks for conventional stem WAVs under DefaultStemOutputDir(mixPath).
// ok is true when at least one stem file exists. Only present paths are set on found.
func DiscoverStems(mixPath string) (found StemResult, ok bool) {
	found.Vocals = resolveStemFile(mixPath, "vocals")
	found.Drums = resolveStemFile(mixPath, "drums")
	found.Bass = resolveStemFile(mixPath, "bass")
	found.Other = resolveStemFile(mixPath, "other")
	ok = found.Vocals != "" || found.Drums != "" || found.Bass != "" || found.Other != ""
	return found, ok
}

// StemPath returns the on-disk path for a named stem (prefers existing file).
// If none exist yet, returns the preferred stem-splitter naming path.
func StemPath(mixPath, stem string) string {
	if p := resolveStemFile(mixPath, stem); p != "" {
		return p
	}
	cands := stemFileCandidates(mixPath, stem)
	if len(cands) == 0 {
		return ""
	}
	return cands[0]
}

// AllowedInspectFile reports whether requested may be streamed/peaked for mixPath.
// True only for the cleaned mix path or a conventional stem WAV inside its stem dir.
func AllowedInspectFile(mixPath, requested string) bool {
	if mixPath == "" || requested == "" {
		return false
	}
	mixAbs, err := filepath.Abs(mixPath)
	if err != nil {
		return false
	}
	mixAbs = filepath.Clean(mixAbs)
	reqAbs, err := filepath.Abs(requested)
	if err != nil {
		return false
	}
	reqAbs = filepath.Clean(reqAbs)

	if reqAbs == mixAbs {
		ext := strings.ToLower(filepath.Ext(reqAbs))
		return audioExts[ext]
	}

	stemDir := filepath.Clean(DefaultStemOutputDir(mixAbs))
	if filepath.Dir(reqAbs) != stemDir {
		return false
	}
	base := filepath.Base(reqAbs)
	prefix := mixBasename(mixAbs) + "_"
	for _, stem := range StemNames {
		if base == stem+".wav" || base == prefix+stem+".wav" {
			return true
		}
	}
	return false
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
