package sourcefingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

var sourceRoots = []string{"cmd", "internal", "lifecycle", "protocol", "sidecar", "utils"}

var rootFiles = []string{"go.mod", "go.sum"}

// Calculate identifies every repository source that can change oc-control or
// one of the policy helpers it invokes. Paths are included in the digest so a
// rename invalidates the checkout-pinned development binary.
func Calculate(repositoryRoot string) (string, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	files := append([]string{}, rootFiles...)
	for _, sourceRoot := range sourceRoots {
		absolute := filepath.Join(root, sourceRoot)
		if err := filepath.WalkDir(absolute, func(filename string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			relative, err := filepath.Rel(root, filename)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relative))
			return nil
		}); err != nil {
			return "", fmt.Errorf("walk control source %s: %w", sourceRoot, err)
		}
	}
	sort.Strings(files)

	digest := sha256.New()
	for _, relative := range files {
		filename := filepath.Join(root, filepath.FromSlash(relative))
		file, err := os.Open(filename)
		if err != nil {
			return "", fmt.Errorf("open control source %s: %w", relative, err)
		}
		if _, err := fmt.Fprintf(digest, "%d:%s\x00", len(relative), relative); err != nil {
			file.Close()
			return "", err
		}
		if _, err := io.Copy(digest, file); err != nil {
			file.Close()
			return "", fmt.Errorf("hash control source %s: %w", relative, err)
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("close control source %s: %w", relative, err)
		}
		if _, err := digest.Write([]byte{0}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
