//go:build windows

package instance

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func defaultAlive(pid int) bool {
	cmd := defaultCommand(pid)
	if cmd == "" {
		return false
	}
	lower := strings.ToLower(cmd)
	if strings.Contains(lower, "no tasks") || strings.Contains(lower, "no matching") {
		return false
	}
	return strings.Contains(cmd, strconv.Itoa(pid))
}

func defaultTerm(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func defaultCommand(pid int) string {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func defaultPortPIDs(port int) []int {
	out, err := exec.Command("netstat", "-ano").Output()
	if err != nil {
		return nil
	}
	needle := ":" + strconv.Itoa(port)
	seen := map[int]bool{}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.Contains(strings.ToUpper(fields[0]), "TCP") {
			continue
		}
		if !strings.Contains(fields[1], needle) {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		n, convErr := strconv.Atoi(fields[len(fields)-1])
		if convErr != nil || n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		pids = append(pids, n)
	}
	return pids
}
