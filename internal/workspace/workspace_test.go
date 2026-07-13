package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTopologyIsDerivedFromSidecarEntries(t *testing.T) {
	root := t.TempDir()
	for _, app := range []string{"electron", "web", "api", "plain"} {
		if err := os.MkdirAll(filepath.Join(root, "apps", app), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, app := range []string{"web", "api"} {
		for _, entry := range []string{"sidecar/index.ts", "dist/sidecar/index.js"} {
			path := filepath.Join(root, "apps", app, entry)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("entry"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	topology, err := DiscoverTopology(root, Config{Schema: 1, PayloadWorkspace: "electron"})
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.Sidecars) != 2 || topology.Sidecars[0].App != "api" || topology.Sidecars[1].App != "web" {
		t.Fatalf("unexpected topology: %+v", topology)
	}
	if topology.Sidecars[0].Entry != "sidecars/api/dist/sidecar/index.js" {
		t.Fatalf("unexpected derived entry: %s", topology.Sidecars[0].Entry)
	}
}
