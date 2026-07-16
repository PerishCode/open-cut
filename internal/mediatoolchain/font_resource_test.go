package mediatoolchain

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

func TestVerifyCaptionFontResourceClosesExactTree(t *testing.T) {
	root, digest := fixtureCaptionFontResource(t)
	bundle, err := renderengine.VerifyCaptionFontResource(
		root, renderengine.CaptionFontBundleID, renderengine.CaptionFontBundleVersion,
		domain.Digest(digest),
	)
	if err != nil || bundle.Validate() != nil {
		t.Fatalf("bundle=%+v err=%v", bundle, err)
	}

	if err := os.WriteFile(filepath.Join(root, "ambient.txt"), []byte("ambient"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := renderengine.VerifyCaptionFontResource(
		root, renderengine.CaptionFontBundleID, renderengine.CaptionFontBundleVersion,
		domain.Digest(digest),
	); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("extra resource file was accepted: %v", err)
	}
}

func TestVerifyCaptionFontResourceRejectsExecutionDigestMismatch(t *testing.T) {
	root, _ := fixtureCaptionFontResource(t)
	wrong := domain.Digest("sha256:" + strings.Repeat("a", 64))
	if _, err := renderengine.VerifyCaptionFontResource(
		root, renderengine.CaptionFontBundleID, renderengine.CaptionFontBundleVersion, wrong,
	); err == nil || !strings.Contains(err.Error(), "closure digest") {
		t.Fatalf("mismatched execution digest was accepted: %v", err)
	}
}

func fixtureCaptionFontResource(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := make([]renderengine.CaptionFontFile, 0, len(captionFontSelections()))
	for _, selection := range captionFontSelections() {
		filename := filepath.Join(root, selection.Destination)
		if err := os.WriteFile(filename, []byte("fixture:"+selection.Destination), 0o600); err != nil {
			t.Fatal(err)
		}
		digest, size, err := digestFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, renderengine.CaptionFontFile{
			Path: selection.Destination, SHA256: domain.Digest(digest), ByteSize: size,
		})
	}
	bundle, err := renderengine.NewPinnedCaptionFontBundle(files)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, renderengine.CaptionFontBundleFilename)
	if err := atomicfile.WriteJSON(manifestPath, bundle, 0o600); err != nil {
		t.Fatal(err)
	}
	record := ResourceRecord{
		ID: bundle.ID, Kind: ResourceKindFontBundle, Version: bundle.Version,
		Root: captionFontResourceRoot, Files: make([]ResourceFileRecord, 0, len(files)+1),
	}
	for _, file := range files {
		record.Files = append(record.Files, ResourceFileRecord{
			Path: file.Path, SHA256: file.SHA256.String(), ByteSize: file.ByteSize,
		})
	}
	manifestDigest, manifestSize, err := digestFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	record.Files = append(record.Files, ResourceFileRecord{
		Path: renderengine.CaptionFontBundleFilename, SHA256: manifestDigest, ByteSize: manifestSize,
	})
	slices.SortFunc(record.Files, func(left, right ResourceFileRecord) int {
		return strings.Compare(left.Path, right.Path)
	})
	digest, err := resourceClosureDigest(record)
	if err != nil {
		t.Fatal(err)
	}
	return root, digest
}
