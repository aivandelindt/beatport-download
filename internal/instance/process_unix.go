//go:build unix

package instance

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func defaultAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func defaultTerm(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(syscall.SIGTERM)
}

func defaultCommand(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func defaultPortPIDs(port int) []int {
	out, err := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		return nil
	}
	var pids []int
	seen := map[int]bool{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		n, convErr := strconv.Atoi(strings.TrimSpace(string(line)))
		if convErr != nil || n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		pids = append(pids, n)
	}
	return pids
}
