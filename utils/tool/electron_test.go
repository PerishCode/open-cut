package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveElectronUsesPackagePathFile(t *testing.T) {
	repository := t.TempDir()
	packageRoot := filepath.Join(repository, "apps", "electron", "node_modules", "electron")
	binary := filepath.Join(packageRoot, "dist", "Electron.app", "Contents", "MacOS", "Electron")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "path.txt"), []byte("Electron.app/Contents/MacOS/Electron\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveElectron(repository, "electron")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != binary {
		t.Fatalf("resolved=%q want=%q", resolved, binary)
	}
}
