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

func TestRemoveExternalDeploySelfLinkPreservesMaterializedDirectory(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "deploy")
	selfReference := filepath.Join(destination, "node_modules", ".pnpm", "node_modules", "@open-cut", "api")
	if err := os.MkdirAll(selfReference, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selfReference, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeExternalDeploySelfLink(destination, "@open-cut/api"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(selfReference); err != nil || !info.IsDir() {
		t.Fatalf("materialized self-reference was not preserved: %v", err)
	}
}

func TestRemoveExternalDeploySelfLinkRejectsUnsafePackageName(t *testing.T) {
	if err := removeExternalDeploySelfLink(t.TempDir(), "../../escape"); err == nil {
		t.Fatal("unsafe package name accepted")
	}
}

func TestCopyTreeDereferencesInternalDirectoryLink(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	packageRoot := filepath.Join(source, "node_modules", ".pnpm", "ws", "node_modules", "ws")
	link := filepath.Join(source, "node_modules", "ws")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte(`{"name":"ws"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(link), packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination, true); err != nil {
		t.Fatal(err)
	}
	copiedLink := filepath.Join(destination, "node_modules", "ws")
	if info, err := os.Lstat(copiedLink); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("link was not materialized as a directory: info=%v err=%v", info, err)
	}
	if contents, err := os.ReadFile(filepath.Join(copiedLink, "package.json")); err != nil || string(contents) != `{"name":"ws"}` {
		t.Fatalf("materialized package mismatch: contents=%q err=%v", contents, err)
	}
}

func TestCopyTreePreservesInternalDirectoryLink(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(source, "Versions", "A")
	link := filepath.Join(source, "Versions", "Current")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "runtime"), []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("A", link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination, false); err != nil {
		t.Fatal(err)
	}
	copiedLink := filepath.Join(destination, "Versions", "Current")
	if info, err := os.Lstat(copiedLink); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was not preserved: info=%v err=%v", info, err)
	}
	if target, err := os.Readlink(copiedLink); err != nil || target != "A" {
		t.Fatalf("preserved link target=%q err=%v", target, err)
	}
}

func TestCopyTreeDereferenceRejectsExternalLink(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	external := filepath.Join(parent, "external")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(source, "external")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := copyTree(source, filepath.Join(parent, "destination"), true); err == nil {
		t.Fatal("external link was accepted")
	}
}
