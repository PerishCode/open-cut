//go:build windows

package atomicfile

// Windows does not support FlushFileBuffers on a directory handle. The file is
// fully flushed before the atomic rename, which is the strongest portable
// guarantee available here.
func syncParent(string) error { return nil }
