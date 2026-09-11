//go:build unix

package instance

import "syscall"

var errAddrInUse error = syscall.EADDRINUSE
