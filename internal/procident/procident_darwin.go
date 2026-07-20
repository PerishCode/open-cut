//go:build darwin

package procident

import (
	"bytes"
	"fmt"

	"golang.org/x/sys/unix"
)

// Executable returns the kernel-reported executable path of the process.
func Executable(pid int) (string, error) {
	arguments, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	const argumentCountBytes = 4
	if len(arguments) <= argumentCountBytes {
		return "", fmt.Errorf("process %d has invalid arguments", pid)
	}
	pathBytes := arguments[argumentCountBytes:]
	end := bytes.IndexByte(pathBytes, 0)
	if end <= 0 {
		return "", fmt.Errorf("process %d has no executable path", pid)
	}
	return string(pathBytes[:end]), nil
}
