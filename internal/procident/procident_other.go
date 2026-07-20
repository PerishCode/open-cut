//go:build !darwin

package procident

import (
	"fmt"

	"github.com/shirou/gopsutil/v4/process"
)

// Executable returns the kernel-reported executable path of the process.
// Callers needing recycled-pid proof must pair this with CreateTimeMs: path
// equality alone cannot distinguish a restarted binary.
func Executable(pid int) (string, error) {
	target := process.Process{Pid: int32(pid)}
	executable, err := target.Exe()
	if err != nil {
		return "", fmt.Errorf("read process %d executable: %w", pid, err)
	}
	return executable, nil
}
