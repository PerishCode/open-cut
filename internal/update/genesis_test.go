package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestBadSignatureNeverDownloadsBundle(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var bundleRequests atomic.Int64
	var metadata []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/metadata/root.json" {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Path == "/metadata/beta/"+target.Host().String()+"/latest.json" {
			writer.Write(metadata)
			return
		}
		bundleRequests.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	descriptor := release.Descriptor{
		Schema: release.ReleaseMetadataSchema, Channel: "beta", Version: "1.0.0-beta.1",
		Platform: target.Host().Platform, Arch: target.Host().Arch,
		Bundle: release.BundleDescriptor{
			Path: "releases/1.0.0-beta.1/" + target.Host().String() + "/bundle.tar.zst", Size: 10,
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	envelope, err := release.SignEnvelope(descriptor, "root", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Signed.Version = "1.1.0-beta.1"
	metadata, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: "beta", Namespace: "test",
		Roots: config.RootSet{
			BootstrapRoot: filepath.Join(root, "bootstrap"), StoreRoot: filepath.Join(root, "store"),
			CacheRoot: filepath.Join(root, "cache"), RuntimeRoot: filepath.Join(root, "runtime"), LogRoot: filepath.Join(root, "logs"),
		},
		UpdateOrigins: []string{server.URL}, ProtocolFloor: "bootstrap.v1",
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
	if _, err := (Installer{Now: func() time.Time { return now }}).InstallLatest(context.Background(), bootstrap, paths); err == nil {
		t.Fatal("tampered metadata installed")
	}
	if got := bundleRequests.Load(); got != 0 {
		t.Fatalf("bundle endpoint received %d requests", got)
	}
}

func TestSignedOlderReleaseNeverDownloadsBundle(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	var bundleRequests atomic.Int64
	var metadata []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata/root.json":
			http.NotFound(writer, request)
		case "/metadata/beta/" + target.Host().String() + "/latest.json":
			_, _ = writer.Write(metadata)
		default:
			bundleRequests.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	envelope, err := release.SignEnvelope(release.Descriptor{
		Schema: release.ReleaseMetadataSchema, Channel: "beta", Version: "1.0.0-beta.1",
		Platform: target.Host().Platform, Arch: target.Host().Arch,
		Bundle: release.BundleDescriptor{
			Path: "releases/1.0.0-beta.1/" + target.Host().String() + "/bundle.tar.zst", Size: 10,
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}, "root", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ = json.Marshal(envelope)
	root := t.TempDir()
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: "beta", Namespace: "rollback", ProtocolFloor: "bootstrap.v1",
		Roots: config.RootSet{
			BootstrapRoot: filepath.Join(root, "bootstrap"), StoreRoot: filepath.Join(root, "store"),
			CacheRoot: filepath.Join(root, "cache"), RuntimeRoot: filepath.Join(root, "runtime"), LogRoot: filepath.Join(root, "logs"),
		},
		UpdateOrigins:    []string{server.URL},
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
	if err := state.Save(paths.StateFile, bootstrap.Channel, state.Runtime{
		Schema: state.Schema, Active: "2.0.0-beta.1", LastGood: "2.0.0-beta.1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Installer{Now: func() time.Time { return now }}).InstallLatest(context.Background(), bootstrap, paths); err == nil {
		t.Fatal("signed older release was accepted")
	}
	if got := bundleRequests.Load(); got != 0 {
		t.Fatalf("bundle endpoint received %d requests", got)
	}
}
