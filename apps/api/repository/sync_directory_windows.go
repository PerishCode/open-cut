//go:build windows

package repository

// Windows does not expose a portable directory fsync through os.File. Each
// artifact file is flushed before the same-volume directory rename; SQLite is
// committed only after that rename succeeds.
func syncDirectory(string) error { return nil }
