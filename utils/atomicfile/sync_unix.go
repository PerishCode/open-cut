//go:build !windows

package atomicfile

import "os"

func syncParent(directory string) error {
	parent, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
