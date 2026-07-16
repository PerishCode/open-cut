package mediatoolchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestRendererConformanceMatrixClosesEveryRequiredShape(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proxy := filepath.Join(root, "proxy.webm")
	if err := os.WriteFile(proxy, []byte("fixture-proxy"), 0o600); err != nil {
		t.Fatal(err)
	}
	fontRoot := filepath.Join(root, "fonts")
	if err := os.Mkdir(fontRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	font := ResourceRecord{
		ID: renderengine.CaptionFontBundleID, Kind: ResourceKindFontBundle,
		Version: renderengine.CaptionFontBundleVersion,
		Root:    "media/fonts/fixture", SHA256: "sha256:" + strings.Repeat("a", 64),
	}
	for _, purpose := range []domain.RenderPlanPurpose{
		domain.RenderPurposeSequencePreview, domain.RenderPurposeExport,
	} {
		fixtures, fixtureErr := rendererConformanceFixtures(root, proxy, fontRoot, font, purpose)
		if fixtureErr != nil {
			t.Fatal(fixtureErr)
		}
		if len(fixtures) != 4 {
			t.Fatalf("%s fixtures=%d", purpose, len(fixtures))
		}
		for _, fixture := range fixtures {
			if application.ValidateRenderPlanPayload(fixture.Plan.Plan.Payload) != nil {
				t.Fatalf("%s fixture %s has an invalid plan", purpose, fixture.ID)
			}
		}
		main := fixtures[0].Plan.Plan.Payload
		if len(main.Video) != 3 || len(main.Audio) != 3 || len(main.Captions) != 1 ||
			len(main.FontResources) != 1 || main.Output.VideoFrameCount.Value() != 26 ||
			main.Output.AudioSampleCount.Value() != 41_143 {
			t.Fatalf("%s main matrix is incomplete: %+v", purpose, main)
		}
		if len(fixtures[1].Plan.Plan.Payload.Audio) != 0 || len(fixtures[1].Plan.Plan.Payload.Video) != 1 ||
			len(fixtures[2].Plan.Plan.Payload.Audio) != 1 || len(fixtures[2].Plan.Plan.Payload.Video) != 0 ||
			len(fixtures[3].Plan.Plan.Payload.Captions) != 1 || len(fixtures[3].Plan.Plan.Payload.Inputs) != 0 {
			t.Fatalf("%s A/V shape matrix is incomplete", purpose)
		}
	}
}
