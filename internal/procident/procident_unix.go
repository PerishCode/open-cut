//go:build !windows

package procident

import (
	"errors"
	"syscall"
)

// Alive reports whether the pid currently names a running process. EPERM
// proves existence: the signal was routed to a live process we may not touch.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Terminate requests a graceful stop; a process that is already gone is not
// an error.
func Terminate(pid int) error {
	return signalIgnoringGone(pid, syscall.SIGTERM)
}

// Kill forcibly stops the process after Terminate ran out of patience.
func Kill(pid int) error {
	return signalIgnoringGone(pid, syscall.SIGKILL)
}

func signalIgnoringGone(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	err := syscall.Kill(pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
