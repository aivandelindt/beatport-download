//go:build windows

package instance

import "syscall"

// WSAEADDRINUSE (10048). Named constant lives in golang.org/x/sys/windows.
var errAddrInUse error = syscall.Errno(10048)
