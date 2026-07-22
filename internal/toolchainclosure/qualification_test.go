package toolchainclosure

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQualificationReceiptBindsExactOwnerInput(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := struct {
		Target string `json:"target"`
		Digest string `json:"digest"`
	}{Target: "linux-x64", Digest: "sha256:fixture"}
	if err := WriteQualificationReceipt(root, "qualification.json", "fixture-v1", "fixture/qualification/v1", input); err != nil {
		t.Fatal(err)
	}
	if err := VerifyQualificationReceipt(root, "qualification.json", "fixture-v1", "fixture/qualification/v1", input); err != nil {
		t.Fatal(err)
	}
	changed := input
	changed.Target = "win-x64"
	if err := VerifyQualificationReceipt(root, "qualification.json", "fixture-v1", "fixture/qualification/v1", changed); err == nil {
		t.Fatal("receipt accepted a different input identity")
	}
	if err := VerifyQualificationReceipt(root, "qualification.json", "fixture-v2", "fixture/qualification/v1", input); err == nil {
		t.Fatal("receipt accepted a different qualification profile")
	}
}

func TestQualificationReceiptRejectsMalformedAndLinkedFiles(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "qualification.json")
	if err := os.WriteFile(filename, []byte(`{"schema":1,"profile":"fixture-v1","inputSha256":"nope","outcome":"succeeded"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyQualificationReceipt(root, "qualification.json", "fixture-v1", "fixture/qualification/v1", "input"); err == nil {
		t.Fatal("malformed receipt was accepted")
	}
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external.json")
	if err := os.WriteFile(external, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filename); err == nil {
		if err := VerifyQualificationReceipt(root, "qualification.json", "fixture-v1", "fixture/qualification/v1", "input"); err == nil {
			t.Fatal("linked receipt was accepted")
		}
	}
}
