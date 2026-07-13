package cleaner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Scope string

const (
	ScopeTemp  Scope = "temp"
	ScopeBuild Scope = "build"
	ScopeAll   Scope = "all"
)

type Item struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type Report struct {
	Schema int    `json:"schema"`
	Root   string `json:"root"`
	Scope  Scope  `json:"scope"`
	DryRun bool   `json:"dryRun"`
	Items  []Item `json:"items"`
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
	if scope != ScopeTemp && scope != ScopeBuild && scope != ScopeAll {
		return Report{}, fmt.Errorf("clean scope must be temp, build, or all")
	}
	targets := make([]string, 0)
	if scope == ScopeTemp || scope == ScopeAll {
		targets = append(targets, filepath.Join(root, ".tmp"))
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
					targets = append(targets, filepath.Join(root, layer, entry.Name(), "dist"))
				}
			}
		}
	}

	report := Report{Schema: 1, Root: root, Scope: scope, DryRun: dryRun, Items: make([]Item, 0, len(targets))}
	for _, target := range targets {
		if err := requireContained(root, target); err != nil {
			return Report{}, err
		}
		_, statErr := os.Lstat(target)
		if errors.Is(statErr, os.ErrNotExist) {
			report.Items = append(report.Items, Item{Path: target, Status: "missing"})
			continue
		}
		if statErr != nil {
			return Report{}, statErr
		}
		if dryRun {
			report.Items = append(report.Items, Item{Path: target, Status: "would-remove"})
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			return Report{}, fmt.Errorf("remove generated path %s: %w", target, err)
		}
		report.Items = append(report.Items, Item{Path: target, Status: "removed"})
	}
	return report, nil
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
