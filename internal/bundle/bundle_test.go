package bundle

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestPackExtractRoundTrip(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "manifest.json"), []byte(`{"schema":1}`), 0o644)
	writeTestFile(t, filepath.Join(source, "launcher", "launcher"), []byte("launcher"), 0o755)
	writeTestFile(t, filepath.Join(source, "payload", "app.txt"), []byte("payload"), 0o644)
	bundlePath := filepath.Join(t.TempDir(), "release-bundle.tar.zst")
	if err := Pack(source, bundlePath); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tree")
	if err := Extract(bundlePath, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "payload", "app.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("payload = %q", data)
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "bad.tar.zst")
	file, err := os.Create(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(file)
	if err != nil {
		t.Fatal(err)
	}
	tarWriter := tar.NewWriter(encoder)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Extract(bundlePath, filepath.Join(t.TempDir(), "tree")); err == nil {
		t.Fatal("traversing archive extracted successfully")
	}
}

func TestPackExtractPreservesSafeSymlink(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "manifest.json"), []byte(`{"schema":1}`), 0o644)
	writeTestFile(t, filepath.Join(source, "launcher", "launcher"), []byte("launcher"), 0o755)
	writeTestFile(t, filepath.Join(source, "payload", "target"), []byte("payload"), 0o644)
	if err := os.Symlink("target", filepath.Join(source, "payload", "current")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	bundlePath := filepath.Join(t.TempDir(), "release-bundle.tar.zst")
	if err := Pack(source, bundlePath); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tree")
	if err := Extract(bundlePath, destination); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(destination, "payload", "current"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "target" {
		t.Fatalf("symlink target = %q", target)
	}
}

func writeTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
