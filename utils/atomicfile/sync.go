package atomicfile

// SyncDirectory flushes a directory entry so a rename or creation inside it
// survives a crash. It is a no-op on platforms without a portable directory
// fsync; there the file itself is flushed before the rename, which is the
// strongest guarantee available.
func SyncDirectory(path string) error {
	return syncParent(path)
}
