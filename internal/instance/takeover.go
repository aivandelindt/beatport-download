package instance

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

var (
	processAlive             = defaultAlive
	processCommand           = defaultCommand
	signalTerm               = defaultTerm
	portPIDs                 = defaultPortPIDs
	stderr         io.Writer = os.Stderr
	takeoverWait             = 15 * time.Second
	waitPoll                 = 50 * time.Millisecond
)

// Takeover asks a previous BeatportDL-UI process on port to exit.
// Foreign processes are not signalled.
func Takeover(ctx context.Context, port int) error {
	candidates, err := takeoverCandidates(port)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return fmt.Errorf("port %d already in use", port)
	}

	for _, pid := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := signalTerm(pid); err != nil {
			if !processAlive(pid) {
				continue
			}
			return fmt.Errorf("signal previous instance pid %d: %w", pid, err)
		}
		fmt.Fprintf(stderr, "Stopping previous instance (pid %d)…\n", pid)
		if err := waitDead(ctx, pid, takeoverWait); err != nil {
			return err
		}
	}
	return nil
}

func takeoverCandidates(port int) ([]int, error) {
	filePID := readPid(port)
	lsofPIDs := portPIDs(port)

	seen := map[int]bool{}
	var candidates []int

	add := func(pid int, trusted bool) error {
		if pid <= 0 || pid == osGetpid() || seen[pid] {
			return nil
		}
		seen[pid] = true
		if !processAlive(pid) {
			return nil
		}
		cmd := processCommand(pid)
		if cmd != "" && !IsOursCommand(cmd) {
			return fmt.Errorf("port %d in use by pid %d (%s)", port, pid, cmd)
		}
		if cmd == "" && !trusted {
			return fmt.Errorf("port %d in use by pid %d", port, pid)
		}
		candidates = append(candidates, pid)
		return nil
	}

	if err := add(filePID, true); err != nil {
		return nil, err
	}
	for _, pid := range lsofPIDs {
		if err := add(pid, false); err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

func waitDead(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("previous instance pid %d did not exit", pid)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitPoll):
		}
	}
}
