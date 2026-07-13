//go:build windows

package packager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// canonicalPath asks the Windows kernel for the final DOS path behind a file
// handle. filepath.EvalSymlinks does not reliably dereference directory
// junctions, while pnpm uses junctions throughout its Windows virtual store.
func canonicalPath(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buffer := make([]uint16, 32_768)
	length, err := windows.GetFinalPathNameByHandle(windows.Handle(file.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	if length == 0 || length >= uint32(len(buffer)) {
		return "", fmt.Errorf("final Windows path for %s exceeds supported length", name)
	}
	resolved := windows.UTF16ToString(buffer[:length])
	switch {
	case strings.HasPrefix(resolved, `\\?\UNC\`):
		resolved = `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`)
	case strings.HasPrefix(resolved, `\\?\`):
		resolved = strings.TrimPrefix(resolved, `\\?\`)
	}
	return filepath.Clean(resolved), nil
}
