package productresource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultResourcesPinsOfficialImmutableWhisperModel(t *testing.T) {
	resources := DefaultResources()
	if len(resources) != 1 || resources[0].Name != TranscriptModelName ||
		resources[0].Version != TranscriptModelVersion || resources[0].Origin != TranscriptModelOrigin ||
		resources[0].ByteSize.Value() != TranscriptModelByteSize ||
		resources[0].SHA256.String() != TranscriptModelSHA256 {
		t.Fatalf("resources=%+v", resources)
	}
	root := t.TempDir()
	if err := Write(root, "fixture-v1", resources); err != nil {
		t.Fatal(err)
	}
	verified, err := Load(root)
	if err != nil || len(verified.Entries) != 1 || verified.Entries[0].Profile != TranscriptModelName {
		t.Fatalf("verified=%+v err=%v", verified, err)
	}
}

func TestLoadValidatesAndOrdersAuthenticatedEntries(t *testing.T) {
	root := t.TempDir()
	manifest := `{
  "schema": 1,
  "catalogId": "open-cut-product-resources",
  "version": "fixture-v1",
  "resources": [
    {
      "name": "whisper-small-multilingual-v1",
      "kind": "transcription-model",
      "version": "small-v1",
      "profile": "whisper-small-multilingual-v1",
      "origin": "https://resources.example.invalid/whisper-small.bin",
      "byteSize": "4",
      "sha256": "sha256:` + strings.Repeat("a", 64) + `",
      "retention": "offline"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(root, CatalogName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	verified, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.Entries) != 1 || verified.Entries[0].Name != "whisper-small-multilingual-v1" ||
		verified.Entries[0].Canonical == nil || verified.Entries[0].EntryDigest == "" {
		t.Fatalf("verified=%+v", verified)
	}
}

func TestLoadRejectsAmbientOrDuplicateCatalogEntries(t *testing.T) {
	root := t.TempDir()
	bad := `{"schema":1,"catalogId":"open-cut-product-resources","version":"fixture","resources":[{"name":"model","kind":"transcription-model","version":"v1","profile":"p","origin":"http://example.invalid/model","byteSize":"1","sha256":"sha256:` + strings.Repeat("a", 64) + `","retention":"offline"}]}`
	if err := os.WriteFile(filepath.Join(root, CatalogName), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("insecure resource origin was accepted")
	}
}
