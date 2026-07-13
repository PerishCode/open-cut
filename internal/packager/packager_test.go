package packager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveExternalDeploySelfLink(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "deploy")
	selfLink := filepath.Join(destination, "node_modules", ".pnpm", "node_modules", "@open-cut", "api")
	if err := os.MkdirAll(filepath.Dir(selfLink), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceWorkspace := filepath.Join(filepath.Dir(destination), "workspace", "apps", "api")
	if err := os.MkdirAll(sourceWorkspace, 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(selfLink), sourceWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, selfLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := removeExternalDeploySelfLink(destination, "@open-cut/api"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(selfLink); !os.IsNotExist(err) {
		t.Fatalf("external self-link still exists: %v", err)
	}
}

func TestRemoveExternalDeploySelfLinkPreservesInternalLink(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "deploy")
	selfLink := filepath.Join(destination, "node_modules", ".pnpm", "node_modules", "@open-cut", "api")
	target := filepath.Join(destination, "node_modules", ".pnpm", "api@workspace", "node_modules", "@open-cut", "api")
	if err := os.MkdirAll(filepath.Dir(selfLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(selfLink), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, selfLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := removeExternalDeploySelfLink(destination, "@open-cut/api"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(selfLink); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("internal self-link was not preserved: %v", err)
	}
}

func TestRemoveExternalDeploySelfLinkRejectsUnsafePackageName(t *testing.T) {
	if err := removeExternalDeploySelfLink(t.TempDir(), "../../escape"); err == nil {
		t.Fatal("unsafe package name accepted")
	}
}
