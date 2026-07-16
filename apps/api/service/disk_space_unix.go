//go:build !windows

package service

import (
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

func availableFilesystemBytes(path string) (uint64, error) {
	var statistics unix.Statfs_t
	if err := unix.Statfs(path, &statistics); err != nil {
		return 0, err
	}
	blockSize := uint64(statistics.Bsize)
	availableBlocks := uint64(statistics.Bavail)
	if blockSize == 0 || availableBlocks > math.MaxUint64/blockSize {
		return 0, fmt.Errorf("filesystem availability overflows")
	}
	return availableBlocks * blockSize, nil
}
