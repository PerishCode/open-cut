package lifecycle

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentInstallationIdentityPersistsAndSignsByRole(t *testing.T) {
	root := filepath.Join(t.TempDir(), "installation")
	roles := []string{"product-cli", "first-party-ui"}
	first, err := EnsureDevelopmentInstallationIdentity(root, roles)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureDevelopmentInstallationIdentity(root, roles)
	if err != nil {
		t.Fatal(err)
	}
	if first.Assertion().InstallationID != second.Assertion().InstallationID || len(first.Assertion().Keys) != 2 {
		t.Fatalf("first=%+v second=%+v", first.Assertion(), second.Assertion())
	}
	message := []byte("single-use API challenge")
	signature, err := second.Sign("first-party-ui", message)
	if err != nil {
		t.Fatal(err)
	}
	var publicKey []byte
	for _, key := range second.Assertion().Keys {
		if key.Role == "first-party-ui" {
			publicKey, _ = base64.StdEncoding.DecodeString(key.PublicKey)
		}
	}
	if !ed25519.Verify(publicKey, message, signature) {
		t.Fatal("development installation signature did not verify")
	}
	info, err := os.Stat(filepath.Join(root, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity mode = %o", info.Mode().Perm())
	}
	if _, err := EnsureDevelopmentInstallationIdentity(root, []string{"first-party-ui"}); err == nil {
		t.Fatal("changed role set was silently accepted")
	}
}
