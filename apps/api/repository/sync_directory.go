package repository

import "github.com/PerishCode/open-cut/utils/atomicfile"

// syncDirectory flushes a directory entry so a durable publication's rename
// survives a crash. The durable-write mechanics live in one package; this is
// the repository's name for them.
func syncDirectory(path string) error {
	return atomicfile.SyncDirectory(path)
}
