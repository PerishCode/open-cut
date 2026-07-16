package renderengine

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestExecutionManifestClosesPlanAndMaterialPaths(t *testing.T) {
	plan := captionRenderPlan(t)
	fontRoot := t.TempDir()
	identity := application.SequencePreviewRendererIdentity{
		Version: "fixture-renderer-v1", Target: target.Host().String(),
	}
	manifest, encoded, err := CompileExecutionManifest(plan, identity, executionClosure(t), MaterialPaths{
		ArtifactRoots: map[string]string{},
		Resources:     map[string]string{"font:noto-caption-bundle": fontRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	physicalFontRoot, err := filepath.EvalSymlinks(fontRoot)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Resources[0].Kind != "font-bundle" || manifest.Resources[0].Path != physicalFontRoot ||
		manifest.Output.RelativePath != "preview.webm" || manifest.Result.RelativePath != ResultFilename ||
		manifest.Budget.PeakCaptionLayers != 1 || manifest.Budget.OutputByteLimit != MaximumOutputBytes {
		t.Fatalf("manifest=%+v", manifest)
	}
	decoded, err := DecodeExecutionManifest(encoded)
	if err != nil || !reflect.DeepEqual(decoded, manifest) {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	unknown := bytes.Replace(encoded, []byte(`{"schema":5`), []byte(`{"unknown":true,"schema":5`), 1)
	if _, err := DecodeExecutionManifest(unknown); err == nil {
		t.Fatal("execution manifest accepted an unknown field")
	}

	mutated := manifest
	mutated.Plan.Output.Evaluation.BlendPolicy = "ambient"
	_, mutated.PlanDigest, err = domain.CanonicalDigest(
		"open-cut/render-plan", domain.RenderPlanSchema, mutated.Plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := mutated.Validate(); err == nil {
		t.Fatal("execution manifest accepted a digest-valid but unsupported plan")
	}
	mutated = manifest
	mutated.Budget.PixelSampleWork++
	if err := mutated.Validate(); err == nil {
		t.Fatal("execution manifest accepted a caller-mutated resource budget")
	}
}

func TestExecutionManifestClosesExportPurposeAndOutput(t *testing.T) {
	plan := captionRenderPlanFor(t, domain.DefaultSequenceFormat(), true)
	fontRoot := t.TempDir()
	manifest, encoded, err := CompileExecutionManifest(
		plan,
		application.SequencePreviewRendererIdentity{
			Version: "fixture-export-renderer-v1", Target: target.Host().String(),
		},
		executionClosure(t),
		MaterialPaths{
			ArtifactRoots: map[string]string{},
			Resources:     map[string]string{"font:noto-caption-bundle": fontRoot},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Plan.Purpose != domain.RenderPurposeExport ||
		manifest.Output.RelativePath != "export.webm" || manifest.Plan.Output.Video.CRF != 24 {
		t.Fatalf("export execution manifest=%+v", manifest)
	}
	if decoded, err := DecodeExecutionManifest(encoded); err != nil || !reflect.DeepEqual(decoded, manifest) {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
}

func TestExecutionManifestRejectsMissingExtraAndNonDirectoryMaterial(t *testing.T) {
	plan := captionRenderPlan(t)
	identity := application.SequencePreviewRendererIdentity{
		Version: "fixture-renderer-v1", Target: target.Host().String(),
	}
	closure := executionClosure(t)
	if _, _, err := CompileExecutionManifest(plan, identity, closure, MaterialPaths{
		ArtifactRoots: map[string]string{}, Resources: map[string]string{},
	}); err == nil {
		t.Fatal("missing resource root was accepted")
	}
	fontRoot := t.TempDir()
	if _, _, err := CompileExecutionManifest(plan, identity, closure, MaterialPaths{
		ArtifactRoots: map[string]string{"extra": fontRoot},
		Resources:     map[string]string{"font:noto-caption-bundle": fontRoot},
	}); err == nil {
		t.Fatal("extra material root was accepted")
	}
	regular := filepath.Join(t.TempDir(), "font.bin")
	if err := os.WriteFile(regular, []byte("font"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cleanAbsoluteDirectory(regular) {
		t.Fatal("regular file was accepted as a material root")
	}
	if runtime.GOOS != "windows" {
		linked := filepath.Join(t.TempDir(), "font-link")
		if err := os.Symlink(fontRoot, linked); err != nil {
			t.Fatal(err)
		}
		if cleanAbsoluteDirectory(linked) {
			t.Fatal("symlinked material root was accepted")
		}
	}
}

func executionClosure(t *testing.T) ExecutionClosure {
	t.Helper()
	root := t.TempDir()
	tool := filepath.Join(root, target.Host().ExecutableName("ffmpeg"))
	if err := os.WriteFile(tool, []byte("fixture-ffmpeg"), 0o700); err != nil {
		t.Fatal(err)
	}
	return ExecutionClosure{
		SHA256: domain.Digest("sha256:" + strings.Repeat("c", 64)),
		Tools: map[string]ExecutionToolPin{
			"ffmpeg": {Path: tool, SHA256: domain.Digest("sha256:" + strings.Repeat("d", 64))},
		},
	}
}

func captionRenderPlan(t *testing.T) application.PublishedRenderPlan {
	return captionRenderPlanWithFormat(t, domain.DefaultSequenceFormat())
}

func captionRenderPlanWithFormat(
	t *testing.T,
	format domain.SequenceFormat,
) application.PublishedRenderPlan {
	return captionRenderPlanFor(t, format, false)
}

func captionRenderPlanFor(
	t *testing.T,
	format domain.SequenceFormat,
	export bool,
) application.PublishedRenderPlan {
	t.Helper()
	projectID := mustRenderID(t, domain.ParseProjectID, "00000000-0000-7000-8000-000000000001")
	sequenceID := mustRenderID(t, domain.ParseSequenceID, "00000000-0000-7000-8000-000000000002")
	trackID := mustRenderID(t, domain.ParseTrackID, "00000000-0000-7000-8000-000000000003")
	captionID := mustRenderID(t, domain.ParseCaptionID, "00000000-0000-7000-8000-000000000004")
	revision, _ := domain.NewRevision(1)
	zero, _ := domain.NewRationalTime(0, 1)
	one, _ := domain.NewRationalTime(1, 1)
	input := application.CompileRenderPlanInput{
		ProjectID: projectID, ObservedProjectRevision: revision,
		Sequence: domain.Sequence{
			ID: sequenceID, Revision: revision, Name: "main", Role: domain.SequenceRoleMain,
			Format: format,
			Tracks: []domain.Track{{
				ID: trackID, Revision: revision, Type: domain.TrackCaption, Label: "Captions", OrderKey: "a",
			}},
		},
		Captions: []domain.CaptionState{{
			ID: captionID, Revision: revision, SequenceID: sequenceID, TrackID: trackID,
			Range: domain.TimeRange{Start: zero, Duration: one}, Language: "en", Text: "Pinned text",
		}},
		Assets: map[string]application.RenderAssetSnapshot{},
		FontResource: &domain.RenderFontResource{
			ResourceID: "font:noto-caption-bundle", Version: "fixture-v1",
			SHA256: domain.Digest("sha256:" + strings.Repeat("f", 64)),
		},
	}
	var compiled application.CompiledRenderPlan
	var err error
	if export {
		compiled, err = application.CompileSequenceExportPlan(input)
	} else {
		compiled, err = application.CompileSequencePreviewPlan(input)
	}
	if err != nil {
		t.Fatal(err)
	}
	return application.PublishedRenderPlan{Plan: compiled.Plan}
}

func mustRenderID[T any](t *testing.T, parse func(string) (T, error), value string) T {
	t.Helper()
	result, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
