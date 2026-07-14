//go:build windows

package filesystem

import (
	"path/filepath"
	"strings"
)

func IdentityKey(name string) string { return strings.ToLower(filepath.Clean(name)) }
