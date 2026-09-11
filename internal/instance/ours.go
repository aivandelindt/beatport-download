package instance

import "strings"

// IsOursCommand reports whether a process command line belongs to this app.
func IsOursCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	return strings.Contains(lower, "beatportdl-ui") ||
		strings.Contains(lower, "beatport-download")
}
