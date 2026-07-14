package publisher_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/publisher"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/verifier"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestCreateIsIdempotentAndVerifiable(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	for _, entry := range []string{"launcher/launcher", "payload/bin/runtime"} {
		path := filepath.Join(tree, filepath.FromSlash(entry))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(entry), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtimetopology.Write(filepath.Join(tree, "payload", "runtime-topology.json"), runtimetopology.Topology{
		Schema: 1, Processes: []runtimetopology.Process{{App: "app", Command: "bin/runtime", WorkingDirectory: "."}},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	manifest := release.Manifest{
		Schema: release.ManifestSchema, Channel: "beta", Version: "0.1.0-beta.1",
		Platform: target.Mac, Arch: target.ARM64,
		Launcher: release.Entry{Entry: "launcher/launcher"}, Payload: release.Entry{Entry: "payload/runtime-topology.json"},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: now,
	}
	if err := atomicfile.WriteJSON(filepath.Join(tree, "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(root, "bundle.tar.zst")
	if err := bundle.Pack(tree, bundlePath); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "key.json")
	if _, err := publisher.GenerateKey(keyPath, "test"); err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(root, "origin")
	first, err := publisher.Create(bundlePath, origin, keyPath, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	firstMetadata, err := os.ReadFile(first.Release)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publisher.Create(bundlePath, origin, keyPath, time.Hour, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	secondMetadata, err := os.ReadFile(second.Release)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstMetadata) != string(secondMetadata) {
		t.Fatal("idempotent publication rewrote immutable release metadata")
	}
	if _, err := verifier.VerifyOrigin(origin, "beta", target.Target{Platform: target.Mac, Arch: target.ARM64}, keyPath, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}
