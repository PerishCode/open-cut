//go:build linux

package procident

import (
	"fmt"
	"os"
)

// Executable returns the kernel-reported executable path of the process. A
// deleted or replaced binary keeps its " (deleted)" suffix and therefore fails
// any identity comparison, which is the fail-closed behavior callers rely on.
func Executable(pid int) (string, error) {
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", fmt.Errorf("read process %d executable: %w", pid, err)
	}
	return path, nil
}
