//go:build windows

package instance

import "os"

var shutdownSignals = []os.Signal{os.Interrupt}
