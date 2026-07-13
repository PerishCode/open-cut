package cleaner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanKnownGeneratedPathsOnly(t *testing.T) {
	root := t.TempDir()
	write := func(relative string) {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod")
	write("pnpm-workspace.yaml")
	write(".tmp/session/state")
	write("apps/web/dist/index.js")
	write("apps/web/src/index.ts")
	write("packages/client/dist/index.js")
	report, err := Clean(root, ScopeAll, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 3 {
		t.Fatalf("cleaned %d targets, want 3", len(report.Items))
	}
	for _, removed := range []string{".tmp", "apps/web/dist", "packages/client/dist"} {
		if _, err := os.Stat(filepath.Join(root, removed)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", removed)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "apps/web/src/index.ts")); err != nil {
		t.Fatalf("business source was removed: %v", err)
	}
}

func TestCleanDryRunAndWorkspaceGuard(t *testing.T) {
	root := t.TempDir()
	if _, err := Clean(root, ScopeTemp, false); err == nil {
		t.Fatal("non-workspace clean succeeded")
	}
	for _, marker := range []string{"go.mod", "pnpm-workspace.yaml"} {
		if err := os.WriteFile(filepath.Join(root, marker), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, ".tmp", "keep")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Clean(root, ScopeTemp, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run removed path: %v", err)
	}
}
