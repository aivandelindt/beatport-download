package instance

import (
	"errors"
	"strings"
)

// IsAddrInUse reports whether err is a TCP bind conflict.
func IsAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errAddrInUse) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "address already in use")
}
