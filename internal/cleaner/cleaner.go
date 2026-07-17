package cleaner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

type Scope string

const (
	ScopeTemp  Scope = "temp"
	ScopeBuild Scope = "build"
	ScopeQuick Scope = "quick"
	ScopeAll   Scope = "all"
)

// mediaToolchainDirectory holds downloaded source archives whose loss forces a
// multi-minute native recompile; quick keeps it while removing everything else.
const mediaToolchainDirectory = "media-toolchain"

type Item struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Bytes  int64  `json:"bytes"`
}

type Report struct {
	Schema int    `json:"schema"`
	Root   string `json:"root"`
	Scope  Scope  `json:"scope"`
	DryRun bool   `json:"dryRun"`
	Items  []Item `json:"items"`
}

type candidate struct {
	path      string
	protected bool
}

func Clean(repositoryRoot string, scope Scope, dryRun bool) (Report, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return Report{}, err
	}
	root = filepath.Clean(root)
	if err := validateRepository(root); err != nil {
		return Report{}, err
	}
	if scope != ScopeTemp && scope != ScopeBuild && scope != ScopeQuick && scope != ScopeAll {
		return Report{}, fmt.Errorf("clean scope must be temp, build, quick, or all")
	}
	candidates := make([]candidate, 0)
	if scope == ScopeQuick {
		quick, err := quickCandidates(root)
		if err != nil {
			return Report{}, err
		}
		candidates = append(candidates, quick...)
	}
	if scope == ScopeTemp || scope == ScopeAll {
		candidates = append(candidates, candidate{path: filepath.Join(root, ".tmp")})
	}
	if scope == ScopeBuild || scope == ScopeAll {
		for _, layer := range []string{"apps", "packages"} {
			entries, readErr := os.ReadDir(filepath.Join(root, layer))
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			if readErr != nil {
				return Report{}, readErr
			}
			for _, entry := range entries {
				if entry.IsDir() {
					candidates = append(candidates, candidate{path: filepath.Join(root, layer, entry.Name(), "dist")})
				}
			}
		}
	}

	report := Report{Schema: 1, Root: root, Scope: scope, DryRun: dryRun, Items: make([]Item, 0, len(candidates))}
	for _, selected := range candidates {
		if err := requireContained(root, selected.path); err != nil {
			return Report{}, err
		}
		_, statErr := os.Lstat(selected.path)
		if errors.Is(statErr, os.ErrNotExist) {
			report.Items = append(report.Items, Item{Path: selected.path, Status: "missing"})
			continue
		}
		if statErr != nil {
			return Report{}, statErr
		}
		bytes := measureBytes(selected.path)
		if selected.protected {
			report.Items = append(report.Items, Item{Path: selected.path, Status: "protected", Bytes: bytes})
			continue
		}
		held, err := holdsLiveCell(selected.path)
		if err != nil {
			return Report{}, err
		}
		if held {
			report.Items = append(report.Items, Item{Path: selected.path, Status: "in-use", Bytes: bytes})
			continue
		}
		if dryRun {
			report.Items = append(report.Items, Item{Path: selected.path, Status: "would-remove", Bytes: bytes})
			continue
		}
		if err := os.RemoveAll(selected.path); err != nil {
			return Report{}, fmt.Errorf("remove generated path %s: %w", selected.path, err)
		}
		report.Items = append(report.Items, Item{Path: selected.path, Status: "removed", Bytes: bytes})
	}
	return report, nil
}

// quickCandidates enumerates each child of .tmp as an independent removal
// unit, expanding .tmp/oc-control one level so its cell roots and caches can
// be dispositioned separately.
func quickCandidates(root string) ([]candidate, error) {
	temp := filepath.Join(root, ".tmp")
	entries, err := os.ReadDir(temp)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(temp, entry.Name())
		if entry.Name() != "oc-control" || !entry.IsDir() {
			candidates = append(candidates, candidate{path: path})
			continue
		}
		children, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			childPath := filepath.Join(path, child.Name())
			candidates = append(candidates, candidate{
				path:      childPath,
				protected: child.IsDir() && child.Name() == mediaToolchainDirectory,
			})
		}
	}
	return candidates, nil
}

// holdsLiveCell reports whether any broker.lock below the candidate is held by
// a running cell. Cell layouts keep the lock at runtime/<channel>/<namespace>/
// broker.lock; the three patterns cover a cell root, an oc-control container,
// and the whole .tmp directory.
func holdsLiveCell(path string) (bool, error) {
	patterns := []string{
		filepath.Join(path, "runtime", "*", "*", "broker.lock"),
		filepath.Join(path, "*", "runtime", "*", "*", "broker.lock"),
		filepath.Join(path, "oc-control", "*", "runtime", "*", "*", "broker.lock"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return false, err
		}
		for _, match := range matches {
			lock := flock.New(match)
			acquired, err := lock.TryLock()
			if err != nil {
				return false, fmt.Errorf("probe cell lock %s: %w", match, err)
			}
			if !acquired {
				return true, nil
			}
			if err := lock.Unlock(); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

// measureBytes sums regular-file sizes best-effort so the report doubles as a
// disk usage probe; unreadable entries are skipped rather than failing clean.
func measureBytes(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func validateRepository(root string) error {
	for _, marker := range []string{"go.mod", "pnpm-workspace.yaml"} {
		info, err := os.Lstat(filepath.Join(root, marker))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not an open-cut workspace: missing %s", root, marker)
		}
	}
	return nil
}

func requireContained(root, target string) error {
	relative, err := filepath.Rel(root, filepath.Clean(target))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("refusing to clean path outside repository: %s", target)
	}
	return nil
}
