package lifecycle

import (
	"path/filepath"
	"testing"
)

func TestResolveProductDataDirUsesStableProductAndDirectCellSegments(t *testing.T) {
	base := filepath.Join(t.TempDir(), "User Data")
	resolved, err := ResolveProductDataDir(base, "open-cut", "beta", "default")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "open-cut", "beta", "default"); resolved != want {
		t.Fatalf("ResolveProductDataDir() = %q, want %q", resolved, want)
	}
	if _, err := ResolveProductDataDir("relative", "open-cut", "beta", "default"); err == nil {
		t.Fatal("relative product data base path was accepted")
	}
}
