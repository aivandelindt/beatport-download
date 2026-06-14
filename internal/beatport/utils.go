package beatport

import (
	"regexp"
	"strings"
)

var sanitizeRe = regexp.MustCompile(`[<>:"/\\|?*]`)

func sanitizeFilename(name string) string {
	name = sanitizeRe.ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

func SanitizePath(name string) string {
	return sanitizeFilename(name)
}
