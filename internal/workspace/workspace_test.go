package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTopologyIsDerivedFromSidecarManifests(t *testing.T) {
	root := t.TempDir()
	for _, app := range []string{"electron", "web", "api", "plain"} {
		if err := os.MkdirAll(filepath.Join(root, "apps", app), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifests := map[string]string{
		"electron": `{"schema":1,"command":"$payload"}`,
		"web":      `{"schema":1,"command":"$node","args":["dist/sidecar/index.js"]}`,
		"api":      `{"schema":1,"command":"dist/sidecar/api-sidecar.exe","artifactChecks":[{"command":"dist/sidecar/api-sidecar.exe","args":["artifact","check"]}]}`,
	}
	for app, manifest := range manifests {
		path := filepath.Join(root, "apps", app, "sidecar", SidecarManifestFilename)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	artifact := filepath.Join(root, "apps", "api", "dist", "sidecar", "api-sidecar.exe")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("entry"), 0o755); err != nil {
		t.Fatal(err)
	}
	topology, err := DiscoverTopology(root, Config{Schema: 1, PayloadWorkspace: "electron"})
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.Sidecars) != 3 || topology.Sidecars[0].App != "api" || topology.Sidecars[1].App != "electron" || topology.Sidecars[2].App != "web" {
		t.Fatalf("unexpected topology: %+v", topology)
	}
	if topology.Sidecars[0].Command != "dist/sidecar/api-sidecar.exe" || topology.Sidecars[2].Command != SidecarCommandNode {
		t.Fatalf("unexpected declared commands: %+v", topology.Sidecars)
	}
	if len(topology.Sidecars[0].ArtifactChecks) != 1 || topology.Sidecars[0].ArtifactChecks[0].Args[1] != "check" {
		t.Fatalf("artifact checks were not preserved: %+v", topology.Sidecars[0].ArtifactChecks)
	}
}

func TestTopologyRejectsPayloadCommandEscape(t *testing.T) {
	root := t.TempDir()
	for _, app := range []string{"electron", "api"} {
		if err := os.MkdirAll(filepath.Join(root, "apps", app, "sidecar"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(root, "apps", "electron", "sidecar", SidecarManifestFilename),
		[]byte(`{"schema":1,"command":"$payload"}`), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "apps", "api", "sidecar", SidecarManifestFilename),
		[]byte(`{"schema":1,"command":"$payload"}`), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverTopology(root, Config{Schema: 1, PayloadWorkspace: "electron"}); err == nil {
		t.Fatal("non-payload app accepted the payload command")
	}
}

func TestTopologyRejectsArtifactCheckEscape(t *testing.T) {
	root := t.TempDir()
	for _, app := range []string{"electron", "api"} {
		if err := os.MkdirAll(filepath.Join(root, "apps", app, "sidecar"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(root, "apps", "electron", "sidecar", SidecarManifestFilename),
		[]byte(`{"schema":1,"command":"$payload"}`), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "apps", "api", "sidecar", SidecarManifestFilename),
		[]byte(`{"schema":1,"command":"dist/sidecar/api-sidecar.exe","artifactChecks":[{"command":"../escape"}]}`), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "apps", "api", "dist", "sidecar", "api-sidecar.exe")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("entry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverTopology(root, Config{Schema: 1, PayloadWorkspace: "electron"}); err == nil {
		t.Fatal("escaping artifact check command was accepted")
	}
}
