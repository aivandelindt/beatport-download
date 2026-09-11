package audio

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// runCmd is injectable for tests.
var runCmd = defaultRunCmd

func defaultRunCmd(ctx context.Context, name string, args []string, env []string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// lookPath is injectable for tests.
var lookPath = exec.LookPath

// lookPathStat checks a concrete path is an existing regular file.
var lookPathStat = func(path string) (bool, error) {
	fi, err := osStat(path)
	if err != nil {
		return false, err
	}
	return !fi.IsDir(), nil
}

type fileInfo interface {
	IsDir() bool
}

var osStat = func(path string) (fileInfo, error) {
	return defaultOsStat(path)
}

func cmdError(name string, stderr []byte, err error) error {
	if len(stderr) > 0 {
		return fmt.Errorf("%s: %w: %s", name, err, truncate(string(stderr), 500))
	}
	return fmt.Errorf("%s: %w", name, err)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
