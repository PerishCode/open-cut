//go:build !windows

package packager

import "path/filepath"

func canonicalPath(name string) (string, error) {
	return filepath.EvalSymlinks(name)
}
