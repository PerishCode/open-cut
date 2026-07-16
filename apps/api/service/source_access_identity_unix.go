//go:build !windows

package service

import (
	"fmt"
	"os"
	"syscall"
)

func sourceFileIdentity(_ *os.File, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrSourceSelectionUnreadable
	}
	return fmt.Sprintf("posix:%d:%d", stat.Dev, stat.Ino), nil
}
