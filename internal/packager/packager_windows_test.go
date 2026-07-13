//go:build windows

package packager

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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

	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination, true); err != nil {
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
