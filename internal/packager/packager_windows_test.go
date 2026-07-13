//go:build windows

package packager

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRemoveExternalDeploySelfJunction(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "deploy")
	selfJunction := filepath.Join(destination, "node_modules", ".pnpm", "node_modules", "@open-cut", "api")
	workspacePackage := filepath.Join(root, "workspace", "apps", "api")
	if err := os.MkdirAll(filepath.Dir(selfJunction), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspacePackage, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", selfJunction, workspacePackage).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	if err := removeExternalDeploySelfLink(destination, "@open-cut/api"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(selfJunction); !os.IsNotExist(err) {
		t.Fatalf("external self junction still exists: %v", err)
	}
}

func TestRemoveExternalDeploySelfJunctionPreservesInternalTarget(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "deploy")
	selfJunction := filepath.Join(destination, "node_modules", ".pnpm", "node_modules", "@open-cut", "api")
	deployedPackage := filepath.Join(destination, "node_modules", ".pnpm", "api", "node_modules", "@open-cut", "api")
	if err := os.MkdirAll(filepath.Dir(selfJunction), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deployedPackage, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", selfJunction, deployedPackage).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	if err := removeExternalDeploySelfLink(destination, "@open-cut/api"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(selfJunction); err != nil || !info.IsDir() {
		t.Fatalf("internal self junction was removed: info=%v err=%v", info, err)
	}
}

func TestCopyTreeDereferencesWindowsJunction(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(source, "store", "package")
	junction := filepath.Join(source, "node_modules", "package")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(`{"name":"package"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(junction), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	junctionPath, err := canonicalPath(junction)
	if err != nil {
		t.Fatal(err)
	}
	targetPath, err := canonicalPath(target)
	if err != nil {
		t.Fatal(err)
	}
	if directoryKey(junctionPath) != directoryKey(targetPath) {
		t.Fatalf("junction canonical path=%q target=%q", junctionPath, targetPath)
	}

	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination, true, ""); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(destination, "node_modules", "package")
	if info, err := os.Lstat(copied); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("junction was not materialized: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(filepath.Join(copied, "package.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCopyTreeRejectsWindowsJunctionCycle(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(source, "loop")
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, source).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	if err := copyTree(source, filepath.Join(t.TempDir(), "destination"), true, ""); err == nil {
		t.Fatal("junction cycle was accepted")
	}
}

func TestCopyTreeDereferencesWindowsRepositoryDependencyJunction(t *testing.T) {
	repository := t.TempDir()
	source := filepath.Join(repository, ".tmp", "full-pack")
	dependency := filepath.Join(repository, "packages", "client", "node_modules", "ws")
	junction := filepath.Join(source, "resources", "node_modules", "ws")
	if err := os.MkdirAll(dependency, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "package.json"), []byte(`{"name":"ws"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(junction), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, dependency).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}

	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination, true, repository); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(destination, "resources", "node_modules", "ws")
	if info, err := os.Lstat(copied); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dependency junction was not materialized: info=%v err=%v", info, err)
	}
}

func TestCopyTreeRejectsWindowsRepositorySourceJunction(t *testing.T) {
	repository := t.TempDir()
	source := filepath.Join(repository, ".tmp", "full-pack")
	productSource := filepath.Join(repository, "packages", "client", "src")
	junction := filepath.Join(source, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(productSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, productSource).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	if err := copyTree(source, filepath.Join(t.TempDir(), "destination"), true, repository); err == nil {
		t.Fatal("repository source junction was accepted")
	}
}
