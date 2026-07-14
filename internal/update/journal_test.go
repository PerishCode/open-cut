package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/atomicfile"
	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/internal/target"
)

func TestRecoverPromotedReleasePreparesCandidate(t *testing.T) {
	root := t.TempDir()
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: "beta", Namespace: "recovery", ProtocolFloor: "bootstrap.v1",
		Roots: config.RootSet{
			BootstrapRoot: filepath.Join(root, "bootstrap"), StoreRoot: filepath.Join(root, "store"),
			CacheRoot: filepath.Join(root, "cache"), RuntimeRoot: filepath.Join(root, "runtime"), LogRoot: filepath.Join(root, "logs"),
		},
		UpdateOrigins:    []string{"https://example.test"},
		InitialTrustRoot: config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{ID: "root", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}},
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	version := "2.0.0-beta.1"
	versionRoot := filepath.Join(paths.Versions, version)
	manifest := release.Manifest{
		Schema: release.ManifestSchema, Channel: bootstrap.Channel, Version: version,
		Platform: target.Host().Platform, Arch: target.Host().Arch,
		Launcher: release.Entry{Entry: "launcher/launcher"}, Payload: release.Entry{Entry: "payload/runtime-topology.json"},
		MinimumBootstrapProtocol: bootstrap.ProtocolFloor, PublishedAt: time.Now().UTC(),
	}
	if err := atomicfile.WriteJSON(filepath.Join(versionRoot, "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(versionRoot, "payload", "bin", "runtime")
	if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(command, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runtimetopology.Write(filepath.Join(versionRoot, "payload", "runtime-topology.json"), runtimetopology.Topology{
		Schema: 1, Processes: []runtimetopology.Process{{App: "app", Command: "bin/runtime", WorkingDirectory: "."}},
	}); err != nil {
		t.Fatal(err)
	}
	transaction := filepath.Join(paths.Incoming, "interrupted")
	if err := os.MkdirAll(transaction, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveJournal(paths.UpdateJournal, journal{
		TransactionID: "interrupted", Channel: bootstrap.Channel, Version: version,
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Phase: "promoted",
	}); err != nil {
		t.Fatal(err)
	}
	if err := (Installer{}).Recover(bootstrap, paths); err != nil {
		t.Fatal(err)
	}
	got, err := state.Load(paths.StateFile, bootstrap.Channel)
	if err != nil {
		t.Fatal(err)
	}
	if got.Candidate != version || got.Attempt != 1 {
		t.Fatalf("unexpected recovered state: %+v", got)
	}
	if _, err := os.Stat(paths.UpdateJournal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still exists: %v", err)
	}
}
