package audio

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyze_CLIBackend(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cli_full.txt"))
	if err != nil {
		t.Fatal(err)
	}
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	runCmd = func(ctx context.Context, name string, args []string, env []string) ([]byte, []byte, error) {
		return raw, nil, nil
	}
	origStat := lookPathStat
	t.Cleanup(func() { lookPathStat = origStat })
	lookPathStat = func(path string) (bool, error) {
		if path == "/fake/cli" {
			return true, nil
		}
		return false, os.ErrNotExist
	}

	got, err := Analyze(context.Background(), AnalyzeRequest{
		Path:    "/music/jazz_trio.mp3",
		Kind:    KindFullAnalysis,
		Backend: BackendCLI,
		CLIPath: "/fake/cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != BackendCLI {
		t.Fatalf("source: %s", got.Source)
	}
	if got.RhythmAnalysis == nil || got.RhythmAnalysis.TempoBPM != 84.0 {
		t.Fatalf("rhythm: %+v", got.RhythmAnalysis)
	}
}

func TestAnalyze_AutoFallsBackToCLI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cli_full.txt"))
	if err != nil {
		t.Fatal(err)
	}
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	runCmd = func(ctx context.Context, name string, args []string, env []string) ([]byte, []byte, error) {
		return raw, nil, nil
	}
	origStat := lookPathStat
	t.Cleanup(func() { lookPathStat = origStat })
	lookPathStat = func(path string) (bool, error) {
		switch path {
		case "/fake/mcp":
			return true, nil
		case "/fake/cli":
			return true, nil
		default:
			return false, os.ErrNotExist
		}
	}
	// Force MCP start failure by pointing at a non-executable path that exists
	// according to lookPathStat but StartMCP will fail to exec. Instead, patch
	// via empty mcp that Analyze tries first — StartMCP("/fake/mcp") fails on exec.
	// So auto should fall back to CLI.
	got, err := Analyze(context.Background(), AnalyzeRequest{
		Path:    "/music/jazz_trio.mp3",
		Kind:    KindAudioInfo,
		Backend: BackendAuto,
		MCPPath: "/fake/mcp",
		CLIPath: "/fake/cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != BackendCLI {
		t.Fatalf("expected cli fallback, got %s", got.Source)
	}
	if got.Kind != KindAudioInfo {
		t.Fatalf("kind: %s", got.Kind)
	}
	if got.AudioInfo == nil || got.AudioInfo.SampleRate != 48000 {
		t.Fatalf("audio info: %+v", got.AudioInfo)
	}
}

func TestAnalyze_MissingBinary(t *testing.T) {
	origStat := lookPathStat
	origLook := lookPath
	t.Cleanup(func() {
		lookPathStat = origStat
		lookPath = origLook
	})
	lookPathStat = func(path string) (bool, error) { return false, os.ErrNotExist }
	lookPath = func(file string) (string, error) { return "", errors.New("not found") }

	_, err := Analyze(context.Background(), AnalyzeRequest{
		Path:    "/x.mp3",
		Kind:    KindFullAnalysis,
		Backend: BackendCLI,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
