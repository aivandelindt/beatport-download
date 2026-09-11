package instance

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"beatportdl-ui/internal/config"
)

// pidDir is injectable for tests.
var pidDir = config.Dir

var osGetpid = os.Getpid

// PidPath returns ~/.config/beatportdl-ui/server-<port>.pid
func PidPath(port int) string {
	return filepath.Join(pidDir(), fmt.Sprintf("server-%d.pid", port))
}

// WritePid records the current process as the owner of port.
func WritePid(port int) error {
	dir := pidDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create pid dir: %w", err)
	}
	body := strconv.Itoa(osGetpid()) + "\n"
	if err := os.WriteFile(PidPath(port), []byte(body), 0o600); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	return nil
}

// RemoveIfOwned deletes the pid file only if it still names this process.
func RemoveIfOwned(port int) error {
	if readPid(port) != osGetpid() {
		return nil
	}
	err := os.Remove(PidPath(port))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove pid file: %w", err)
	}
	return nil
}

func readPid(port int) int {
	data, err := os.ReadFile(PidPath(port))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
