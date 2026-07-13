package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/target"
)

func TestSignedReleaseEnvelope(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	descriptor := Descriptor{
		Schema: ReleaseMetadataSchema, Channel: "beta", Version: "1.2.3-beta.1",
		Platform: target.Host().Platform, Arch: target.Host().Arch,
		Bundle:                   BundleDescriptor{Path: "releases/1.2.3-beta.1/mac-arm64/bundle.tar.zst", Size: 1, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	envelope, err := SignEnvelope(descriptor, "root", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	trust := config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{ID: "root", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}}
	if err := VerifyEnvelope(envelope, trust, "beta", "bootstrap.v1", now); err != nil {
		t.Fatal(err)
	}
	envelope.Signed.Version = "1.2.4-beta.1"
	if err := VerifyEnvelope(envelope, trust, "beta", "bootstrap.v1", now); err == nil {
		t.Fatal("tampered envelope verified")
	}
}
