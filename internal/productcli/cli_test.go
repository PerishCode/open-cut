package productcli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestResolveAndInspectActiveCellThroughObserverCapability(t *testing.T) {
	root := t.TempDir()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: "beta", Namespace: "cli", ProtocolFloor: "bootstrap.v1",
		DataDir: filepath.Join(root, "data", "beta", "cli"),
		Roots: config.RootSet{
			BootstrapRoot: filepath.Join(root, "bootstrap"), StoreRoot: filepath.Join(root, "store"),
			CacheRoot: filepath.Join(root, "cache"), RuntimeRoot: filepath.Join(root, "runtime"),
			LogRoot: filepath.Join(root, "logs"),
		},
		InitialTrustRoot: config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{
			ID: "test", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
		}}},
	}
	bootstrapPath := filepath.Join(bootstrap.Roots.BootstrapRoot, "bootstrap.json")
	if err := atomicfile.WriteJSON(bootstrapPath, bootstrap, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	active := "1.0.0-beta.1"
	candidate := "2.0.0-beta.1"
	if err := state.Save(paths.StateFile, identity.Channel, state.Runtime{
		Schema: state.Schema, Generation: 9, Active: active, LastGood: active, Candidate: candidate, Attempt: 2,
	}); err != nil {
		t.Fatal(err)
	}
	versionRoot := filepath.Join(paths.Versions, active)
	manifest := release.Manifest{
		Schema: release.ManifestSchema, Channel: identity.Channel, Version: active,
		Platform: target.Host().Platform, Arch: target.Host().Arch,
		Launcher: release.Entry{Entry: "launcher/launcher"}, Payload: release.Entry{Entry: "payload/runtime-topology.json"},
		MinimumBootstrapProtocol: bootstrap.ProtocolFloor, PublishedAt: time.Now().UTC(),
	}
	if err := atomicfile.WriteJSON(filepath.Join(versionRoot, "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	cliEntry, _ := release.CLIEntry(target.Host())
	cliPath := filepath.Join(versionRoot, filepath.FromSlash(cliEntry))
	if err := os.MkdirAll(filepath.Dir(cliPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cliPath, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveActiveCLI(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Active != active || resolution.CLIExecutable != cliPath {
		t.Fatalf("resolution = %#v", resolution)
	}

	cellBroker, err := broker.Start(broker.Options{
		Identity: identity, Paths: paths, Generation: 9, OwnerTokenTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(context.Background(), []string{"status", "--bootstrap", bootstrapPath}, Options{
		Stdout: &stdout, Stderr: &stderr,
	}); code != 0 {
		t.Fatalf("Run() = %d, stderr=%s", code, stderr.String())
	}
	var result Status
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Active != active || result.Cell.Channel != identity.Channel || result.Cell.Namespace != identity.Namespace {
		t.Fatalf("status = %#v", result)
	}
}
