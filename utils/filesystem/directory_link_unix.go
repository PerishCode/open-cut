//go:build !windows

package filesystem

import "os"

func CreateDirectoryLink(target, link string) error {
	return os.Symlink(target, link)
}
