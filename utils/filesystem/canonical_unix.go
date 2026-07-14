//go:build !windows

package filesystem

import "path/filepath"

func Canonical(name string) (string, error) {
	return filepath.EvalSymlinks(name)
}
