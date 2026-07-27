package mediatoolchain

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// TestRendererSourceClosureStaysOffProductAndControlPlanes locks the renderer
// build identity to render-relevant code: application logic, command schemas,
// app roots, and the control plane must never ride into the closure again,
// because every member file's digest invalidates the built renderer and its
// native qualification on change. product/domain is a known current member
// (time/digest/ID primitives and render types travel with it); shrinking that
// is tracked separately and is deliberately not forbidden here.
func TestRendererSourceClosureStaysOffProductAndControlPlanes(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	packages, _, err := rendererSourceGraph(
		context.Background(), repositoryRoot, "", "", io.Discard, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"/product/application",
		"/product/command",
		"/apps/",
		"/packages/",
		"/internal/cell",
		"/internal/controlcli",
		"/internal/devsession",
		"/internal/devsuite",
		"/internal/businessacceptance",
		"/internal/mediatoolchain",
		"/sidecar/protocol",
	}
	// The lifecycle root carries installer, signer, data-directory, and
	// cell-broker identity; renderer code may only use the process leaf.
	forbiddenExact := []string{"github.com/PerishCode/open-cut/lifecycle"}
	seen := make([]string, 0, len(packages))
	for _, current := range packages {
		if current.Module == nil || !current.Module.Main {
			continue
		}
		seen = append(seen, current.ImportPath)
		for _, fragment := range forbidden {
			if strings.Contains(current.ImportPath, fragment) {
				t.Errorf("renderer closure gained %s (matches %q)", current.ImportPath, fragment)
			}
		}
		for _, exact := range forbiddenExact {
			if current.ImportPath == exact {
				t.Errorf("renderer closure gained %s", current.ImportPath)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("renderer closure resolved no first-party packages")
	}
}
