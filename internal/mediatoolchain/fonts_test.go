package mediatoolchain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/internal/renderengine"
)

func TestCaptionFontSourceSelectionsAreClosed(t *testing.T) {
	sources := captionFontSourceRecords()
	selections := captionFontSelections()
	if len(sources) != 16 || len(selections) != 16 {
		t.Fatalf("sources=%d selections=%d", len(sources), len(selections))
	}
	wanted := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if !validDigest(source.SHA256) || source.License != "OFL-1.1" {
			t.Fatalf("source=%+v", source)
		}
		wanted[source.ID] = struct{}{}
	}
	for _, selection := range selections {
		if _, exists := wanted[selection.SourceID]; !exists {
			t.Fatalf("unknown source selection=%+v", selection)
		}
		delete(wanted, selection.SourceID)
	}
	if len(wanted) != 0 {
		t.Fatalf("unused sources=%v", wanted)
	}
}

func TestPinnedCaptionFontArchivesProduceClosedResource(t *testing.T) {
	assetRoot := os.Getenv("OPEN_CUT_RENDERER_ASSET_ROOT")
	if assetRoot == "" {
		t.Skip("OPEN_CUT_RENDERER_ASSET_ROOT is not configured")
	}
	archives := make(map[string]string, len(captionFontSourceRecords()))
	for _, source := range captionFontSourceRecords() {
		filename := filepath.Join(assetRoot, filepath.Base(source.URL))
		if digest, _, err := digestFile(filename); err != nil || digest != source.SHA256 {
			t.Fatalf("source=%s digest=%s err=%v", source.ID, digest, err)
		}
		archives[source.ID] = filename
	}
	stageRoot := t.TempDir()
	resource, err := stageCaptionFontBundle(archives, stageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if resource.ID != renderengine.CaptionFontBundleID ||
		resource.Version != renderengine.CaptionFontBundleVersion || len(resource.Files) != 17 {
		t.Fatalf("resource=%+v", resource)
	}
	manifestPath := filepath.Join(
		stageRoot, filepath.FromSlash(resource.Root), renderengine.CaptionFontBundleFilename,
	)
	encoded, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderengine.DecodeCaptionFontBundle(encoded); err != nil {
		t.Fatal(err)
	}
	notices, err := stageCaptionFontNotices(archives, stageRoot)
	if err != nil || len(notices) != 2 {
		t.Fatalf("notices=%+v err=%v", notices, err)
	}
}
