package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	ConfigSchema   = 1
	TopologySchema = 1
)

var appPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

type Config struct {
	Schema           int    `json:"schema"`
	PayloadWorkspace string `json:"payloadWorkspace"`
}

type Sidecar struct {
	App   string `json:"app"`
	Entry string `json:"entry"`
}

type Topology struct {
	Schema   int       `json:"schema"`
	Sidecars []Sidecar `json:"sidecars"`
}

type Package struct {
	Name            string            `json:"name"`
	ProductName     string            `json:"productName"`
	Main            string            `json:"main"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func Load(repositoryRoot string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(repositoryRoot, "oc-control.json"))
	if err != nil {
		return Config{}, fmt.Errorf("read oc-control.json: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode oc-control.json: %w", err)
	}
	if config.Schema != ConfigSchema || !appPattern.MatchString(config.PayloadWorkspace) {
		return Config{}, fmt.Errorf("oc-control.json requires schema 1 and a safe payloadWorkspace")
	}
	if info, err := os.Stat(filepath.Join(repositoryRoot, "apps", config.PayloadWorkspace)); err != nil || !info.IsDir() {
		return Config{}, fmt.Errorf("payload workspace apps/%s does not exist", config.PayloadWorkspace)
	}
	if err := ValidateTestLayout(repositoryRoot); err != nil {
		return Config{}, err
	}
	return config, nil
}

func ValidateTestLayout(repositoryRoot string) error {
	var misplaced []string
	for _, kind := range []string{"apps", "packages"} {
		workspaces, err := os.ReadDir(filepath.Join(repositoryRoot, kind))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s workspaces: %w", kind, err)
		}
		for _, candidate := range workspaces {
			if !candidate.IsDir() {
				continue
			}
			workspaceRoot := filepath.Join(repositoryRoot, kind, candidate.Name())
			err := filepath.WalkDir(workspaceRoot, func(filename string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() && filename != workspaceRoot {
					switch entry.Name() {
					case "dist", "node_modules":
						return filepath.SkipDir
					}
					return nil
				}
				if entry.IsDir() || !isTypeScriptTestFile(entry.Name()) {
					return nil
				}
				relative, err := filepath.Rel(workspaceRoot, filename)
				if err != nil {
					return err
				}
				parts := strings.Split(filepath.ToSlash(relative), "/")
				if len(parts) < 2 || parts[0] != "tests" {
					misplaced = append(misplaced, filepath.ToSlash(filepath.Join(kind, candidate.Name(), relative)))
				}
				return nil
			})
			if err != nil {
				return fmt.Errorf("validate %s/%s test layout: %w", kind, candidate.Name(), err)
			}
		}
	}
	if len(misplaced) > 0 {
		sort.Strings(misplaced)
		return fmt.Errorf("app and package tests must live under sibling tests directories: %v", misplaced)
	}
	return nil
}

func isTypeScriptTestFile(name string) bool {
	extension := filepath.Ext(name)
	switch extension {
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
	default:
		return false
	}
	base := strings.TrimSuffix(name, extension)
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".spec")
}

func DiscoverTopology(repositoryRoot string, config Config) (Topology, error) {
	appsRoot := filepath.Join(repositoryRoot, "apps")
	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		return Topology{}, err
	}
	sidecars := make([]Sidecar, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !appPattern.MatchString(entry.Name()) {
			continue
		}
		sourceEntry := filepath.Join(appsRoot, entry.Name(), "sidecar", "index.ts")
		if _, err := os.Stat(sourceEntry); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return Topology{}, err
		}
		compiledEntry := filepath.Join(appsRoot, entry.Name(), "dist", "sidecar", "index.js")
		if info, err := os.Stat(compiledEntry); err != nil || !info.Mode().IsRegular() {
			return Topology{}, fmt.Errorf("sidecar %s has source entry but no compiled dist/sidecar/index.js", entry.Name())
		}
		sidecars = append(sidecars, Sidecar{
			App: entry.Name(), Entry: filepath.ToSlash(filepath.Join("sidecars", entry.Name(), "dist", "sidecar", "index.js")),
		})
	}
	sort.Slice(sidecars, func(i, j int) bool { return sidecars[i].App < sidecars[j].App })
	if len(sidecars) == 0 {
		return Topology{}, fmt.Errorf("no app sidecar entries discovered")
	}
	return Topology{Schema: TopologySchema, Sidecars: sidecars}, nil
}

func LoadPackage(repositoryRoot, app string) (Package, error) {
	if !appPattern.MatchString(app) {
		return Package{}, fmt.Errorf("invalid app workspace %q", app)
	}
	data, err := os.ReadFile(filepath.Join(repositoryRoot, "apps", app, "package.json"))
	if err != nil {
		return Package{}, err
	}
	var manifest Package
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Package{}, err
	}
	if manifest.Name == "" {
		return Package{}, fmt.Errorf("apps/%s package name is required", app)
	}
	return manifest, nil
}
