package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestRepositoryTypeScriptToolingContract(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))

	type packageManifest struct {
		Scripts         map[string]string `json:"scripts"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	loadManifest := func(path string) packageManifest {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var manifest packageManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return manifest
	}

	rootManifest := loadManifest(filepath.Join(root, "package.json"))
	wantRootScripts := map[string]string{
		"build":  "pnpm -r --if-present run build",
		"format": "pnpm -r --if-present run format",
		"lint":   "pnpm -r --if-present run lint",
		"test":   "pnpm -r --if-present run test",
	}
	if !reflect.DeepEqual(rootManifest.Scripts, wantRootScripts) {
		t.Fatalf("root scripts = %#v, want %#v", rootManifest.Scripts, wantRootScripts)
	}

	for _, kind := range []string{"apps", "packages"} {
		entries, err := os.ReadDir(filepath.Join(root, kind))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, kind, entry.Name(), "package.json")
			manifest := loadManifest(path)
			if manifest.DevDependencies["@biomejs/biome"] != "catalog:" {
				t.Fatalf("%s must source @biomejs/biome from the workspace catalog", path)
			}
			for _, script := range []string{"build", "format", "lint", "test"} {
				if manifest.Scripts[script] == "" {
					t.Fatalf("%s is missing the %s script", path, script)
				}
			}
		}
	}

	type biomeConfig struct {
		Files struct {
			Includes []string `json:"includes"`
		} `json:"files"`
	}
	data, err := os.ReadFile(filepath.Join(root, "biome.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config biomeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	wantGeneratedExclusion := "!**/sidecar-protocol/src/generated.ts"
	for _, include := range config.Files.Includes {
		if include == wantGeneratedExclusion {
			return
		}
	}
	t.Fatalf("biome.json must exclude generator-owned %s", wantGeneratedExclusion)
}
