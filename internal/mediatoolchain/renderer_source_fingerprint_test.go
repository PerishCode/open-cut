package mediatoolchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRendererSourceFingerprintDetectsChangeAndReuse(t *testing.T) {
	root := t.TempDir()
	write := func(relative, content string) {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/renderengine/oracle.go", "package renderengine\n")
	write("internal/renderhelper/runner.go", "package renderhelper\n")
	write("internal/rendernative/abi.h", "// abi\n")
	write("cmd/open-cut-render/main.go", "package main\n")

	first, err := RendererSourceFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := RendererSourceFingerprint(root)
	if err != nil || again != first {
		t.Fatalf("fingerprint not stable: %q vs %q err=%v", first, again, err)
	}

	artifact := filepath.Join(root, "apps", "api", "dist", "sidecar")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	if rendererSourceFingerprintMatches(root, artifact) {
		t.Fatal("missing fingerprint file must not match")
	}
	if err := writeRendererSourceFingerprint(root, artifact); err != nil {
		t.Fatal(err)
	}
	if !rendererSourceFingerprintMatches(root, artifact) {
		t.Fatal("freshly recorded fingerprint must match")
	}
	write("internal/renderengine/oracle.go", "package renderengine\n// clamp\n")
	if rendererSourceFingerprintMatches(root, artifact) {
		t.Fatal("renderer source change must invalidate the recorded fingerprint")
	}
}
