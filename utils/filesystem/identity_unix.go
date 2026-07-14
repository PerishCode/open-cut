//go:build !windows

package filesystem

import "path/filepath"

func IdentityKey(name string) string { return filepath.Clean(name) }
