package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/PerishCode/open-cut/internal/config"
)

func TestTrustRootRotationRequiresCurrentThresholdAndExactIncrement(t *testing.T) {
	oldPublicA, oldPrivateA, _ := ed25519.GenerateKey(rand.Reader)
	oldPublicB, oldPrivateB, _ := ed25519.GenerateKey(rand.Reader)
	newPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	current := TrustRoot{
		Schema: TrustRootSchema, Version: 4, Threshold: 2,
		Keys: []config.TrustKey{
			{ID: "old-a", PublicKey: base64.StdEncoding.EncodeToString(oldPublicA)},
			{ID: "old-b", PublicKey: base64.StdEncoding.EncodeToString(oldPublicB)},
		},
	}
	candidate := TrustRoot{
		Schema: TrustRootSchema, Version: 5, Threshold: 1,
		Keys: []config.TrustKey{{ID: "new", PublicKey: base64.StdEncoding.EncodeToString(newPublic)}},
	}
	envelope, err := SignTrustRoot(candidate, "old-a", oldPrivateA)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyTrustRoot(envelope, current); err == nil {
		t.Fatal("one signature satisfied a threshold of two")
	}
	envelope, err = AddTrustRootSignature(envelope, "old-b", oldPrivateB)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyTrustRoot(envelope, current); err != nil {
		t.Fatal(err)
	}
	envelope.Signed.Version = 7
	if err := VerifyTrustRoot(envelope, current); err == nil {
		t.Fatal("trust root version gap accepted")
	}
}
