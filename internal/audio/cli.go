package audio

import (
	"context"
	"fmt"
)

// RunCLI executes the audio-analyzer CLI for a single file (always full dump).
func RunCLI(ctx context.Context, binPath, audioPath string) (string, error) {
	if binPath == "" {
		return "", fmt.Errorf("cli path empty")
	}
	stdout, stderr, err := runCmd(ctx, binPath, []string{audioPath}, nil)
	if err != nil {
		return "", cmdError("audio-analyzer-cli", stderr, err)
	}
	return string(stdout), nil
}
