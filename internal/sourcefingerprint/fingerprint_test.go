package sourcefingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCalculateChangesWithControlSourceButIgnoresApplicationSource(t *testing.T) {
	root := t.TempDir()
	for _, directory := range sourceRoots {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, filename := range rootFiles {
		if err := os.WriteFile(filepath.Join(root, filename), []byte(filename+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	controlSource := filepath.Join(root, "internal", "policy.go")
	if err := os.WriteFile(controlSource, []byte("package internal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Calculate(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "apps", "web", "main.tsx"), []byte("export {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Calculate(root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("application source changed control fingerprint: %s != %s", first, second)
	}
	if err := os.WriteFile(controlSource, []byte("package internal\n// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := Calculate(root)
	if err != nil {
		t.Fatal(err)
	}
	if third == second {
		t.Fatal("control source change did not invalidate fingerprint")
	}
}
