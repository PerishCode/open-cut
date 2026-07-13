package packager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/internal/target"
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

func TestLocateLinuxPackSelectsSluggedProductExecutable(t *testing.T) {
	output := t.TempDir()
	root := filepath.Join(output, "linux-unpacked")
	for _, name := range []string{"open-cut", "libEGL.so", "libffmpeg.so"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	packRoot, entry, err := locateElectronPack(output, "Open Cut", target.Target{Platform: target.Linux, Arch: target.X64})
	if err != nil {
		t.Fatal(err)
	}
	if packRoot != root || entry != "open-cut" {
		t.Fatalf("root=%q entry=%q", packRoot, entry)
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
