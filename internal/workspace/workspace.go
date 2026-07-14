package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/PerishCode/open-cut/internal/atomicfile"
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
	return config, nil
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

func WriteTopology(path string, topology Topology) error {
	if topology.Schema != TopologySchema || len(topology.Sidecars) == 0 {
		return fmt.Errorf("invalid payload topology")
	}
	return atomicfile.WriteJSON(path, topology, 0o600)
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
