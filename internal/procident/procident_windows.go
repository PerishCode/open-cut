//go:build windows

package procident

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// Executable returns the kernel-reported executable path of the process.
func Executable(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", fmt.Errorf("read process %d executable: %w", pid, err)
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

// Alive reports whether the pid currently names a running process.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	// STILL_ACTIVE (STATUS_PENDING): x/sys/windows does not export it.
	const stillActive = 259
	return code == stillActive
}

// Terminate stops the process; Windows offers no cross-process graceful
// signal, so Terminate and Kill share the forced path. A process that is
// already gone is not an error.
func Terminate(pid int) error {
	return Kill(pid)
}

// Kill forcibly stops the process.
func Kill(pid int) error {
	if pid <= 0 {
		return nil
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return fmt.Errorf("open process %d for termination: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.TerminateProcess(handle, 1); err != nil && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return fmt.Errorf("terminate process %d: %w", pid, err)
	}
	return nil
}
