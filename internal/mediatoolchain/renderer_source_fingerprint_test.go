package mediatoolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fingerprintRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// The fingerprint's whole purpose is to notice that something compiled into
// open-cut-render changed. A hand-kept list of renderer source trees could not
// see past the renderer's own packages, so a change to a package it imports -
// domain value types, the process lifecycle it spawns FFmpeg with - left the
// recorded fingerprint matching and a stale helper in place. The input set now
// comes from the compiler's own view of the closure, and these are the
// dependencies that view must include.
func TestRendererFingerprintCoversItsTransitiveDependencies(t *testing.T) {
	root := fingerprintRepositoryRoot(t)
	entries, err := rendererSourceFingerprintInputs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"file\x00internal/renderengine/",
		"file\x00internal/renderhelper/",
		"file\x00internal/rendernative/",
		"file\x00cmd/open-cut-render/",
		"file\x00product/domain/",
		"file\x00product/rendercontract/",
		"file\x00lifecycle/",
		"file\x00internal/mediaclosure/",
		"file\x00utils/target/",
		"module\x00",
	} {
		found := false
		for _, entry := range entries {
			if strings.HasPrefix(entry, required) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("renderer fingerprint input set omits %q", strings.ReplaceAll(required, "\x00", " "))
		}
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry, "file\x00product/application/") {
			t.Fatalf(
				"renderer fingerprint includes application orchestration: %q",
				strings.ReplaceAll(entry, "\x00", " "),
			)
		}
	}
	for index := 1; index < len(entries); index++ {
		if entries[index] == entries[index-1] {
			t.Fatalf("renderer fingerprint contains duplicate input %q", entries[index])
		}
	}
}

func TestRendererFingerprintPinsTheNativeCGOClosure(t *testing.T) {
	root := fingerprintRepositoryRoot(t)
	ctx := context.Background()
	t.Setenv("CGO_ENABLED", "0")
	disabled, err := RendererSourceFingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CGO_ENABLED", "1")
	enabled, err := RendererSourceFingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if disabled != enabled {
		t.Fatalf("ambient CGO_ENABLED changed renderer identity: %q vs %q", disabled, enabled)
	}
	entries, err := rendererSourceFingerprintInputs(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	native, stub := false, false
	for _, entry := range entries {
		native = native || strings.HasPrefix(
			entry, "file\x00internal/rendernative/backend_native.go\x00",
		)
		stub = stub || strings.HasPrefix(
			entry, "file\x00internal/rendernative/backend_stub.go\x00",
		)
	}
	if !native || stub {
		t.Fatalf("renderer identity did not resolve the native CGO implementation: native=%t stub=%t", native, stub)
	}
}

func TestRendererFingerprintIsStableAndRoundTrips(t *testing.T) {
	root := fingerprintRepositoryRoot(t)
	ctx := context.Background()
	first, err := RendererSourceFingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := RendererSourceFingerprint(ctx, root)
	if err != nil || again != first {
		t.Fatalf("fingerprint is not stable: %q vs %q err=%v", first, again, err)
	}

	artifact := t.TempDir()
	if rendererSourceFingerprintMatches(ctx, root, artifact) {
		t.Fatal("a missing fingerprint file must not match")
	}
	if err := writeRendererSourceFingerprint(ctx, root, artifact); err != nil {
		t.Fatal(err)
	}
	if !rendererSourceFingerprintMatches(ctx, root, artifact) {
		t.Fatal("a freshly recorded fingerprint must match")
	}
	recorded := filepath.Join(artifact, rendererSourceFingerprintName)
	if err := os.WriteFile(recorded, []byte("sha256:0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rendererSourceFingerprintMatches(ctx, root, artifact) {
		t.Fatal("a different recorded fingerprint must not match")
	}
}
