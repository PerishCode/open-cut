//go:build open_cut_renderer_native && cgo

package renderhelper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/internal/rendernative"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestPinnedNativeHelperProducesStableCaptionOnlyWebM(t *testing.T) {
	fontRoot := os.Getenv("OPEN_CUT_NATIVE_TEXT_FONT_ROOT")
	if fontRoot == "" || !rendernative.Available() {
		t.Skip("pinned native text fixture is unavailable")
	}
	stageRoot := filepath.Dir(filepath.Dir(filepath.Dir(fontRoot)))
	verified, err := mediatoolchain.Load(stageRoot, target.Host())
	if err != nil {
		t.Fatal(err)
	}
	font, exists := verified.Resources[renderengine.CaptionFontBundleID]
	if !exists {
		t.Fatal("pinned caption font resource is unavailable")
	}
	ffmpeg, exists := verified.Tools["ffmpeg"]
	if !exists {
		t.Fatal("pinned FFmpeg is unavailable")
	}
	plan := nativeHelperCaptionPlan(t, font)
	closure := renderengine.ExecutionClosure{
		SHA256: domain.Digest("sha256:" + strings.Repeat("c", 64)),
		Tools: map[string]renderengine.ExecutionToolPin{
			"ffmpeg": {Path: ffmpeg.Path, SHA256: domain.Digest(ffmpeg.SHA256)},
		},
	}

	var first domain.Digest
	for attempt := 0; attempt < 2; attempt++ {
		root := physicalTempDir(t)
		manifest, encoded, err := renderengine.CompileExecutionManifest(
			plan,
			application.SequencePreviewRendererIdentity{
				Version: "native-helper-smoke-v1", Target: target.Host().String(),
			},
			closure,
			renderengine.MaterialPaths{
				ArtifactRoots: map[string]string{}, Resources: map[string]string{font.ID: font.Root},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		executionPath := filepath.Join(root, renderengine.ExecutionFilename)
		if err := os.WriteFile(executionPath, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Run(context.Background(), executionPath); err != nil {
			t.Fatal(err)
		}
		resultBytes, err := os.ReadFile(filepath.Join(root, renderengine.ResultFilename))
		if err != nil {
			t.Fatal(err)
		}
		result, err := renderengine.DecodeResult(resultBytes)
		if err != nil || result.Status != renderengine.ResultSucceeded || result.Output == nil ||
			result.Output.ByteSize.Value() == 0 || result.Output.RelativePath != manifest.Output.RelativePath {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if attempt == 0 {
			first = result.Output.SHA256
		} else if result.Output.SHA256 != first {
			t.Fatalf("native helper output changed: %s != %s", first, result.Output.SHA256)
		}
	}
}

func nativeHelperCaptionPlan(
	t *testing.T,
	font mediatoolchain.Resource,
) application.PublishedRenderPlan {
	t.Helper()
	projectID, err := domain.ParseProjectID("00000000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	sequenceID, _ := domain.ParseSequenceID("00000000-0000-7000-8000-000000000002")
	trackID, _ := domain.ParseTrackID("00000000-0000-7000-8000-000000000003")
	captionID, _ := domain.ParseCaptionID("00000000-0000-7000-8000-000000000004")
	revision, _ := domain.NewRevision(1)
	zero, _ := domain.NewRationalTime(0, 1)
	one, _ := domain.NewRationalTime(1, 1)
	format := domain.DefaultSequenceFormat()
	format.CanvasWidth, format.CanvasHeight = 320, 180
	compiled, err := application.CompileSequencePreviewPlan(application.CompileRenderPlanInput{
		ProjectID: projectID, ObservedProjectRevision: revision,
		Sequence: domain.Sequence{
			ID: sequenceID, Revision: revision, Name: "native helper", Role: domain.SequenceRoleMain,
			Format: format,
			Tracks: []domain.Track{{
				ID: trackID, Revision: revision, Type: domain.TrackCaption,
				Label: "Captions", OrderKey: "a",
			}},
		},
		Captions: []domain.CaptionState{{
			ID: captionID, Revision: revision, SequenceID: sequenceID, TrackID: trackID,
			Range: domain.TimeRange{Start: zero, Duration: one}, Language: "zh-Hans",
			Text: "Open Cut · שלום · 中文",
		}},
		Assets: map[string]application.RenderAssetSnapshot{},
		FontResource: &domain.RenderFontResource{
			ResourceID: font.ID, Version: font.Version, SHA256: domain.Digest(font.SHA256),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return application.PublishedRenderPlan{Plan: compiled.Plan}
}
