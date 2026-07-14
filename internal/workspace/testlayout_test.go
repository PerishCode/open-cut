package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateTestLayoutRejectsSourceTests(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "apps", "web", "src", "server.test.ts")
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("export {};\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ValidateTestLayout(root)
	if err == nil || !strings.Contains(err.Error(), "apps/web/src/server.test.ts") {
		t.Fatalf("error=%v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "apps", "web", "tests"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filename, filepath.Join(root, "apps", "web", "tests", "server.test.ts")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTestLayout(root); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryTestLayout(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	if err := ValidateTestLayout(repositoryRoot); err != nil {
		t.Fatal(err)
	}
}
