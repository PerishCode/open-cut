package mediatoolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

func TestPinnedArchiveTypesAndSafeXZExtraction(t *testing.T) {
	for sourceURL, expected := range map[string]string{
		"https://example.invalid/source.tar.gz": ".tar.gz",
		"https://example.invalid/source.tar.xz": ".tar.xz",
		"https://example.invalid/source.zip":    ".zip",
	} {
		actual, err := sourceArchiveSuffix(sourceURL)
		if err != nil || actual != expected {
			t.Fatalf("url=%s actual=%s err=%v", sourceURL, actual, err)
		}
	}
	if _, err := sourceArchiveSuffix("https://example.invalid/source.tgz"); err == nil {
		t.Fatal("unsupported source archive suffix was accepted")
	}

	archive := filepath.Join(t.TempDir(), "fixture.tar.xz")
	writeXZTarFixture(t, archive, []tarFixtureEntry{
		{name: "fixture/", directory: true},
		{name: "fixture/configure", content: []byte("#!/bin/sh\n"), mode: 0o755},
		{name: "fixture/empty", content: []byte{}, mode: 0o644},
	})
	destination := t.TempDir()
	root, err := extractSource(archive, destination, "fixture", "configure")
	if err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "configure")); err != nil || string(content) != "#!/bin/sh\n" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	if info, err := os.Stat(filepath.Join(root, "empty")); err != nil || info.Size() != 0 {
		t.Fatalf("empty info=%v err=%v", info, err)
	}
}

func TestPinnedTarRejectsEscapingEntry(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "fixture.tar.xz")
	writeXZTarFixture(t, archive, []tarFixtureEntry{
		{name: "fixture/configure", content: []byte("ok"), mode: 0o755},
		{name: "../escape", content: []byte("no"), mode: 0o644},
	})
	if _, err := extractSource(archive, t.TempDir(), "fixture", "configure"); err == nil ||
		!strings.Contains(err.Error(), "escaping entry") {
		t.Fatalf("error=%v", err)
	}
}

func TestPinnedTarIgnoresOnlyExactDeclaredLink(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "fixture.tar.xz")
	writeXZTarFixture(t, archive, []tarFixtureEntry{
		{name: "fixture/configure", content: []byte("ok"), mode: 0o755},
		{name: "fixture/alias", typeflag: tar.TypeSymlink, linkname: "configure", mode: 0o777},
	})
	destination := t.TempDir()
	root, err := extractSourceIgnoringLinks(
		archive, destination, "fixture", "configure",
		[]archiveIgnoredLink{{Member: "fixture/alias", Target: "configure"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "alias")); !os.IsNotExist(err) {
		t.Fatalf("ignored link was materialized: %v", err)
	}
	if _, err := extractSourceIgnoringLinks(
		archive, t.TempDir(), "fixture", "configure",
		[]archiveIgnoredLink{{Member: "fixture/alias", Target: "different"}},
	); err == nil {
		t.Fatal("mismatched ignored link contract was accepted")
	}
}

func TestPinnedZipExtractsOnlyDeclaredMembers(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "fixture.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range map[string]string{
		"fonts/Regular.ttf": "font",
		"../ambient.txt":    "ambient",
	} {
		member, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := extractZipFiles(archive, destination, []archiveSelection{{
		Member: "fonts/Regular.ttf", Destination: "bundle/Regular.ttf",
	}}); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(destination, "bundle", "Regular.ttf")); err != nil || string(content) != "font" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "ambient.txt")); !os.IsNotExist(err) {
		t.Fatalf("ambient entry escaped selection: %v", err)
	}
	if err := extractZipFiles(archive, t.TempDir(), []archiveSelection{{
		Member: "fonts/Regular.ttf", Destination: "../escape.ttf",
	}}); err == nil {
		t.Fatal("escaping zip destination was accepted")
	}
}

type tarFixtureEntry struct {
	name      string
	content   []byte
	mode      int64
	directory bool
	typeflag  byte
	linkname  string
}

func writeXZTarFixture(t *testing.T, filename string, entries []tarFixtureEntry) {
	t.Helper()
	var compressed bytes.Buffer
	xzWriter, err := xz.NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	tarWriter := tar.NewWriter(xzWriter)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		if entry.directory {
			typeflag = tar.TypeDir
		}
		header := &tar.Header{
			Name: entry.name, Mode: entry.mode, Size: int64(len(entry.content)), Typeflag: typeflag,
			Linkname: entry.linkname,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if len(entry.content) > 0 {
			if _, err := tarWriter.Write(entry.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := xzWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
