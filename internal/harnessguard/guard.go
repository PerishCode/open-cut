package harnessguard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ResultSchema     = 1
	MaxResourceBytes = 50 * 1024
	MaxFileLines     = 800
)

type Violation struct {
	Rule   string `json:"rule"`
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

type Result struct {
	Schema     int         `json:"schema"`
	Passed     bool        `json:"passed"`
	Repository string      `json:"repository"`
	DurationMS int64       `json:"durationMs"`
	Violations []Violation `json:"violations"`
}

var resourceExtensions = map[string]bool{
	".7z": true, ".avif": true, ".bin": true, ".br": true, ".bz2": true, ".dmg": true,
	".eot": true, ".gif": true, ".gz": true, ".ico": true, ".jpeg": true, ".jpg": true,
	".mov": true, ".mp3": true, ".mp4": true, ".otf": true, ".pdf": true, ".png": true,
	".svg": true, ".tar": true, ".tgz": true, ".ttf": true, ".wasm": true, ".wav": true,
	".webm": true, ".webp": true, ".woff": true, ".woff2": true, ".zip": true, ".zst": true,
}

var htmlStylePattern = regexp.MustCompile(`(?i)<style\b|\bstyle\s*=|\brel\s*=\s*["']?stylesheet\b|\.css(?:["'?]|$)`)

func Run(_ context.Context, repositoryRoot string) Result {
	started := time.Now()
	root, err := filepath.Abs(repositoryRoot)
	result := Result{Schema: ResultSchema, Repository: root, Violations: []Violation{}}
	if err != nil {
		result.Violations = append(result.Violations, Violation{Rule: "repository", Path: repositoryRoot, Detail: err.Error()})
		return finish(result, started)
	}
	for _, marker := range []string{".git", "go.mod", "package.json", "pnpm-workspace.yaml"} {
		if _, statErr := os.Stat(filepath.Join(root, marker)); statErr != nil {
			result.Violations = append(result.Violations, Violation{Rule: "repository", Path: marker, Detail: "required repository marker is unavailable"})
		}
	}
	result.Violations = append(result.Violations, inspectTree(root)...)
	result.Violations = append(result.Violations, inspectLayout(root)...)
	result.Violations = append(result.Violations, inspectAPILayers(root)...)
	result.Violations = append(result.Violations, inspectTypeScript(root)...)
	return finish(result, started)
}

func inspectTree(root string) []Violation {
	var violations []Violation
	_ = filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			violations = append(violations, violation(root, filename, "walk", walkErr.Error()))
			return nil
		}
		if entry.IsDir() {
			if filename != root && ignoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		relative := slashRelative(root, filename)
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		data, readErr := os.ReadFile(filename)
		if readErr != nil {
			violations = append(violations, Violation{Rule: "file-read", Path: relative, Detail: readErr.Error()})
			return nil
		}
		text := utf8.Valid(data) && !bytes.ContainsRune(data, '\x00')
		if info.Size() > MaxResourceBytes && (resourceExtensions[extension] || !text) {
			violations = append(violations, Violation{
				Rule: "resource-size", Path: relative,
				Detail: fmt.Sprintf("resource is %d bytes; maximum is %d bytes", info.Size(), MaxResourceBytes),
			})
		}
		if text && entry.Name() != "pnpm-lock.yaml" {
			lines := bytes.Count(data, []byte{'\n'})
			if len(data) > 0 && data[len(data)-1] != '\n' {
				lines++
			}
			if lines > MaxFileLines {
				violations = append(violations, Violation{
					Rule: "line-count", Path: relative,
					Detail: fmt.Sprintf("file has %d lines; maximum is %d", lines, MaxFileLines),
				})
			}
		}
		if isStyleFile(extension) && !strings.HasPrefix(relative, "packages/components/") {
			violations = append(violations, Violation{
				Rule: "style-boundary", Path: relative, Detail: "style files are owned only by packages/components",
			})
		}
		if isTestFile(entry.Name()) && testOutsideSiblingDirectory(relative) {
			violations = append(violations, Violation{
				Rule: "test-layout", Path: relative, Detail: "app and package tests must live under the sibling tests directory",
			})
		}
		return nil
	})
	return violations
}

func inspectLayout(root string) []Violation {
	var violations []Violation
	webSource := filepath.Join(root, "apps", "web", "src")
	entries, err := os.ReadDir(webSource)
	if err != nil {
		return []Violation{{Rule: "web-layout", Path: "apps/web/src", Detail: "Web source root is unavailable"}}
	}
	allowedFiles := map[string]bool{"main.tsx": true, "vite-env.d.ts": true}
	allowedDirectories := map[string]bool{"components": true, "lib": true, "views": true}
	for _, entry := range entries {
		allowed := entry.IsDir() && allowedDirectories[entry.Name()] || !entry.IsDir() && allowedFiles[entry.Name()]
		if !allowed {
			violations = append(violations, Violation{
				Rule: "web-layout", Path: filepath.ToSlash(filepath.Join("apps/web/src", entry.Name())),
				Detail: "Web source allows only lib, views, components, main.tsx, and vite-env.d.ts",
			})
		}
	}
	for _, name := range []string{"components", "views"} {
		if info, statErr := os.Stat(filepath.Join(webSource, name)); statErr != nil || !info.IsDir() {
			violations = append(violations, Violation{Rule: "web-layout", Path: "apps/web/src/" + name, Detail: "required Web directory is unavailable"})
		}
	}
	index, indexErr := os.ReadFile(filepath.Join(root, "apps", "web", "index.html"))
	if indexErr != nil {
		violations = append(violations, Violation{Rule: "style-boundary", Path: "apps/web/index.html", Detail: "Web HTML entry is unavailable"})
	} else {
		if htmlStylePattern.Match(index) {
			violations = append(violations, Violation{Rule: "style-boundary", Path: "apps/web/index.html", Detail: "Web HTML contains style syntax"})
		}
	}

	apiRoot := filepath.Join(root, "apps", "api")
	apiDirectories := map[string]bool{
		"controller": true, "service": true, "repository": true, "model": true, "sidecar": true, "tests": true,
	}
	for name := range apiDirectories {
		if info, statErr := os.Stat(filepath.Join(apiRoot, name)); statErr != nil || !info.IsDir() {
			violations = append(violations, Violation{Rule: "api-layout", Path: "apps/api/" + name, Detail: "required API layer is unavailable"})
		}
	}
	if entries, readErr := os.ReadDir(apiRoot); readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "dist" && entry.Name() != "node_modules" && !apiDirectories[entry.Name()] {
				violations = append(violations, Violation{
					Rule: "api-layout", Path: "apps/api/" + entry.Name(), Detail: "API source directories must be declared architecture layers",
				})
			}
		}
	}
	if _, statErr := os.Stat(filepath.Join(apiRoot, "src")); !errors.Is(statErr, os.ErrNotExist) {
		violations = append(violations, Violation{Rule: "api-layout", Path: "apps/api/src", Detail: "Go API must not recreate a generic src layer"})
	}
	return violations
}

func inspectAPILayers(root string) []Violation {
	apiRoot := filepath.Join(root, "apps", "api")
	allowed := map[string]map[string]bool{
		"model": {}, "repository": {"model": true}, "service": {"model": true, "repository": true},
		"controller": {"model": true, "service": true},
		"sidecar":    {"controller": true, "model": true, "repository": true, "service": true},
		"tests":      {"controller": true, "model": true, "repository": true, "service": true},
	}
	var violations []Violation
	for layer, dependencies := range allowed {
		layerRoot := filepath.Join(apiRoot, layer)
		_ = filepath.WalkDir(layerRoot, func(filename string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
			if err != nil {
				violations = append(violations, violation(root, filename, "api-layer", err.Error()))
				return nil
			}
			for _, imported := range parsed.Imports {
				path := strings.Trim(imported.Path.Value, "\"")
				const prefix = "github.com/PerishCode/open-cut/apps/api/"
				if !strings.HasPrefix(path, prefix) {
					continue
				}
				dependency := strings.Split(strings.TrimPrefix(path, prefix), "/")[0]
				if dependency != "" && !dependencies[dependency] {
					violations = append(violations, Violation{
						Rule: "api-layer", Path: slashRelative(root, filename),
						Detail: fmt.Sprintf("%s layer may not import %s layer", layer, dependency),
					})
				}
			}
			return nil
		})
	}
	return violations
}

func finish(result Result, started time.Time) Result {
	sort.Slice(result.Violations, func(i, j int) bool {
		left := result.Violations[i]
		right := result.Violations[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.Detail < right.Detail
	})
	result.Passed = len(result.Violations) == 0
	result.DurationMS = time.Since(started).Milliseconds()
	return result
}

func ignoredDirectory(name string) bool {
	switch name {
	case ".git", ".task", ".tmp", "coverage", "dist", "node_modules":
		return true
	default:
		return false
	}
}

func isStyleFile(extension string) bool {
	switch extension {
	case ".css", ".less", ".pcss", ".sass", ".scss", ".sss", ".styl", ".stylus":
		return true
	default:
		return false
	}
}

func isTestFile(name string) bool {
	if strings.HasSuffix(name, "_test.go") {
		return true
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	switch extension {
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
		return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".spec")
	default:
		return false
	}
}

func testOutsideSiblingDirectory(relative string) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	return len(parts) >= 3 && (parts[0] == "apps" || parts[0] == "packages") && (len(parts) < 4 || parts[2] != "tests")
}

func violation(root, filename, rule, detail string) Violation {
	return Violation{Rule: rule, Path: slashRelative(root, filename), Detail: detail}
}

func slashRelative(root, filename string) string {
	relative, err := filepath.Rel(root, filename)
	if err != nil {
		return filepath.ToSlash(filename)
	}
	return filepath.ToSlash(relative)
}
