package controlcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectDevInputFileRequiresNonEmptyRegularBytes(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "story.webm")
	if err := os.WriteFile(valid, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, size, err := inspectDevInputFile(valid)
	if err != nil || path != valid || size != 5 {
		t.Fatalf("path=%q size=%d err=%v", path, size, err)
	}

	empty := filepath.Join(root, "empty.webm")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectDevInputFile(empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty input error = %v", err)
	}
	if _, _, err := inspectDevInputFile(root); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory input error = %v", err)
	}
}
