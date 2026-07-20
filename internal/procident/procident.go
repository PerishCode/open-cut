// Package procident reads kernel-reported process identity so callers can
// prove a stored pid still refers to the process they recorded before acting
// on it. A recycled pid whose new process runs the same executable path is the
// accepted residual risk; every check here fails closed on any other
// uncertainty.
package procident

import (
	"os"
	"path/filepath"
)

// SameExecutable reports whether two executable paths refer to the same file,
// tolerating lexical differences via a file-identity fallback.
func SameExecutable(actual, expected string) bool {
	if actual == "" || expected == "" {
		return false
	}
	if filepath.Clean(actual) == filepath.Clean(expected) {
		return true
	}
	actualInfo, actualErr := os.Stat(actual)
	expectedInfo, expectedErr := os.Stat(expected)
	return actualErr == nil && expectedErr == nil && os.SameFile(actualInfo, expectedInfo)
}
