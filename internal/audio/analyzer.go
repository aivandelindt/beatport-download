package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveAnalyzerBins finds mcp-server and cli binaries.
func ResolveAnalyzerBins(mcpPath, cliPath string) (mcp, cli string) {
	mcp = resolveOne(mcpPath, []string{"mcp-server", "audio-analyzer-mcp"}, []string{
		"third_party/audio-analyzer-rs/target/release/mcp-server",
	})
	cli = resolveOne(cliPath, []string{"cli", "audio-analyzer"}, []string{
		"third_party/audio-analyzer-rs/target/release/cli",
	})
	return mcp, cli
}

func resolveOne(configured string, pathNames []string, repoRel []string) string {
	if configured != "" {
		if ok, _ := lookPathStat(configured); ok {
			return configured
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, name := range pathNames {
			p := filepath.Join(dir, name)
			if ok, _ := lookPathStat(p); ok {
				return p
			}
		}
	}
	for _, rel := range repoRel {
		if cwd, err := os.Getwd(); err == nil {
			p := filepath.Join(cwd, rel)
			if ok, _ := lookPathStat(p); ok {
				return p
			}
		}
		p := rel
		if ok, _ := lookPathStat(p); ok {
			return p
		}
	}
	for _, name := range pathNames {
		if p, err := lookPath(name); err == nil && p != "" {
			return p
		}
	}
	return ""
}

// Analyze runs the requested analysis via MCP and/or CLI.
func Analyze(ctx context.Context, req AnalyzeRequest) (Analysis, error) {
	if err := ctx.Err(); err != nil {
		return Analysis{}, err
	}
	kind := req.Kind
	if kind == "" {
		kind = KindFullAnalysis
	}
	if !ValidKind(kind) {
		return Analysis{}, fmt.Errorf("invalid kind: %s", kind)
	}
	backend := strings.ToLower(req.Backend)
	if backend == "" {
		backend = BackendAuto
	}
	mcpBin, cliBin := ResolveAnalyzerBins(req.MCPPath, req.CLIPath)

	switch backend {
	case BackendMCP:
		if mcpBin == "" {
			return Analysis{}, fmt.Errorf("mcp-server binary not found")
		}
		return analyzeMCP(ctx, mcpBin, req.Path, kind)
	case BackendCLI:
		if cliBin == "" {
			return Analysis{}, fmt.Errorf("cli binary not found")
		}
		return analyzeCLI(ctx, cliBin, req.Path, kind)
	case BackendAuto:
		if mcpBin != "" {
			a, err := analyzeMCP(ctx, mcpBin, req.Path, kind)
			if err == nil {
				return a, nil
			}
			if cliBin == "" {
				return Analysis{}, err
			}
		}
		if cliBin == "" {
			return Analysis{}, fmt.Errorf("no analyzer binary found (mcp or cli)")
		}
		return analyzeCLI(ctx, cliBin, req.Path, kind)
	default:
		return Analysis{}, fmt.Errorf("unknown backend: %s", backend)
	}
}

// ValidKind reports whether kind is a supported analysis tool name.
func ValidKind(kind string) bool {
	switch kind {
	case KindAudioInfo, KindSpectralFeatures, KindHarmonicAnalysis, KindRhythmAnalysis, KindFullAnalysis:
		return true
	default:
		return false
	}
}

func analyzeMCP(ctx context.Context, bin, path, kind string) (Analysis, error) {
	client, err := StartMCP(ctx, bin)
	if err != nil {
		return Analysis{}, err
	}
	defer client.Close()
	text, err := client.CallTool(ctx, kind, map[string]interface{}{"path": path})
	if err != nil {
		return Analysis{}, err
	}
	a, err := Parse(kind, text)
	if err != nil {
		return Analysis{}, err
	}
	a.Source = BackendMCP
	a.Path = path
	return a, nil
}

func analyzeCLI(ctx context.Context, bin, path, kind string) (Analysis, error) {
	text, err := RunCLI(ctx, bin, path)
	if err != nil {
		return Analysis{}, err
	}
	parseKind := kind
	if kind != KindFullAnalysis {
		// CLI always dumps everything; parse as full then trim to requested kind below.
		parseKind = KindFullAnalysis
	}
	a, err := Parse(parseKind, text)
	if err != nil {
		return Analysis{}, err
	}
	a.Source = BackendCLI
	a.Path = path
	a.Kind = kind
	if kind != KindFullAnalysis {
		a = TrimAnalysis(a, kind)
	}
	return a, nil
}

// TrimAnalysis returns a copy of full with only fields for the requested kind.
func TrimAnalysis(full Analysis, kind string) Analysis {
	out := Analysis{
		Kind:    kind,
		Source:  full.Source,
		Path:    full.Path,
		RawText: full.RawText,
		Meta:    full.Meta,
		Issues:  full.Issues,
	}
	switch kind {
	case KindAudioInfo:
		out.AudioInfo = full.AudioInfo
	case KindSpectralFeatures:
		out.SpectralFeatures = full.SpectralFeatures
		out.AudioInfo = full.AudioInfo
	case KindHarmonicAnalysis:
		out.HarmonicAnalysis = full.HarmonicAnalysis
	case KindRhythmAnalysis:
		out.RhythmAnalysis = full.RhythmAnalysis
	}
	return out
}

// AnalyzeWithClient reuses an open MCP client for a single file.
func AnalyzeWithClient(ctx context.Context, client *MCPClient, path, kind string) (Analysis, error) {
	if client == nil {
		return Analysis{}, fmt.Errorf("nil mcp client")
	}
	text, err := client.CallTool(ctx, kind, map[string]interface{}{"path": path})
	if err != nil {
		return Analysis{}, err
	}
	a, err := Parse(kind, text)
	if err != nil {
		return Analysis{}, err
	}
	a.Source = BackendMCP
	a.Path = path
	return a, nil
}
