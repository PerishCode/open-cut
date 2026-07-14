package devsession

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveElectronBinaryUsesPackagePathFile(t *testing.T) {
	root := t.TempDir()
	packageRoot := filepath.Join(root, "apps", "carrier", "node_modules", "electron")
	binary := filepath.Join(packageRoot, "dist", "runtime", "electron")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("electron"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "path.txt"), []byte("runtime/electron\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveElectronBinary(root, "carrier")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != binary {
		t.Fatalf("resolved %q, want %q", resolved, binary)
	}
}
