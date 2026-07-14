package devsession

import (
	"path/filepath"
	"testing"
)

func TestResolveBaseDirUsesRepositorySubcommandAndCell(t *testing.T) {
	repository := t.TempDir()
	resolved, err := ResolveBaseDir(repository, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(repository, ".tmp", "oc-control", "dev", "dev", "default"); resolved != want {
		t.Fatalf("ResolveBaseDir() = %q, want %q", resolved, want)
	}
	if _, err := ResolveBaseDir(repository, filepath.Join(repository, "custom")); err == nil {
		t.Fatal("base directory without the cell suffix was accepted")
	}
}
