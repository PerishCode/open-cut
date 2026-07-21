package toolchainclosure

// Directory hashing is shared because both toolchains use it for the same
// purpose: deciding whether previously built material still corresponds to the
// logic that produced it. It over-approximates by design — an unrelated edit in
// a hashed directory invalidates too — which is the right trade only where a
// rebuild is cheap.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HashDirectories digests several directories as one identity, in the order
// given, so build logic split across packages still yields a single key.
func HashDirectories(roots ...string) (string, error) {
	digest := sha256.New()
	for _, root := range roots {
		value, err := hashDirectory(root)
		if err != nil {
			return "", err
		}
		_, _ = digest.Write([]byte(value))
		_, _ = digest.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

// hashDirectory digests every regular file under a directory by relative path
// and content.
func hashDirectory(root string) (string, error) {
	entries := make([]string, 0, 128)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		// Tests never produce an artifact, so a test edit must not discard
		// material that took minutes to build. Everything else is included:
		// narrowing further would mean maintaining a list of which sources the
		// build really reads, and that list goes stale the first time a stage
		// learns to read one more file.
		if strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		digest := sha256.New()
		if _, err := io.Copy(digest, file); err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(relativePath)+"\x00"+hex.EncodeToString(digest.Sum(nil)))
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("directory %s has no files", root)
	}
	sort.Strings(entries)
	overall := sha256.New()
	overall.Write([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(overall.Sum(nil)), nil
}
