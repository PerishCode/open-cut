package lifecycle

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentSignerKeepsPrivateKeysBehindRestrictedSocket(t *testing.T) {
	identity, err := EnsureDevelopmentInstallationIdentity(
		filepath.Join(t.TempDir(), "identity"), []string{"first-party-ui", "product-cli"},
	)
	if err != nil {
		t.Fatal(err)
	}
	socketRoot, err := os.MkdirTemp("", "oc-signer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketRoot)
	socket := filepath.Join(socketRoot, "signer.sock")
	signer, err := StartDevelopmentSigner(socket, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	info, err := os.Stat(socket)
	if err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket info=%+v err=%v", info, err)
	}
	payload := []byte("single-use API challenge")
	encoded, _ := json.Marshal(SignerRequest{
		Schema: SignerRequestSchema, Role: "first-party-ui",
		Payload: base64.RawURLEncoding.EncodeToString(payload),
	})
	client := &http.Client{Transport: &http.Transport{DialContext: func(
		_ context.Context, _, _ string,
	) (net.Conn, error) {
		return net.Dial("unix", socket)
	}}}
	response, err := client.Post("http://lifecycle"+SignerPath, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var signed SignerResponse
	if err := json.NewDecoder(response.Body).Decode(&signed); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || signed.Role != "first-party-ui" {
		t.Fatalf("status=%d response=%+v", response.StatusCode, signed)
	}
	signature, err := base64.RawURLEncoding.DecodeString(signed.Signature)
	if err != nil {
		t.Fatal(err)
	}
	var publicKey ed25519.PublicKey
	for _, key := range identity.Assertion().Keys {
		if key.Role == "first-party-ui" {
			publicKey, _ = base64.StdEncoding.DecodeString(key.PublicKey)
		}
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		t.Fatal("development signer returned an invalid signature")
	}
	t.Setenv(SignerSocketEnvironment, socket)
	requested, err := RequestSignature(context.Background(), "first-party-ui", payload)
	if err != nil {
		t.Fatal(err)
	}
	requestedSignature, err := base64.RawURLEncoding.DecodeString(requested.Signature)
	if err != nil || !ed25519.Verify(publicKey, payload, requestedSignature) {
		t.Fatal("shared signer client returned an invalid signature")
	}
}

func TestDevelopmentSignerRejectsUnknownRoleAndNonSocketCollision(t *testing.T) {
	root := t.TempDir()
	identity, err := EnsureDevelopmentInstallationIdentity(filepath.Join(root, "identity"), []string{"first-party-ui"})
	if err != nil {
		t.Fatal(err)
	}
	collision := filepath.Join(root, "collision")
	if err := os.WriteFile(collision, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := StartDevelopmentSigner(collision, identity); err == nil {
		t.Fatal("non-socket collision was removed")
	}
	if contents, _ := os.ReadFile(collision); !bytes.Equal(contents, []byte("preserve")) {
		t.Fatal("non-socket collision changed")
	}
}
