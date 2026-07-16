package application

import (
	"errors"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequencePreviewArtifactManifestPinsVerifiedPlanFacts(t *testing.T) {
	compiled, err := CompileSequencePreviewPlan(renderPlanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := SequencePreviewFactsForPlan(compiled.Plan.Payload)
	if err != nil || facts.SemanticDuration != compiled.Plan.Payload.Duration ||
		facts.PresentationDuration != compiled.Plan.Payload.Duration ||
		facts.VideoFrameCount.Value() != 150 || facts.AudioSampleCount.Value() != 240_000 {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	mediaSize, _ := domain.NewUInt64(4096)
	manifest := SequencePreviewArtifactManifest{
		ProjectID: compiled.Plan.Payload.ProjectID, SequenceID: compiled.Plan.Payload.SequenceID,
		SequenceRevision: compiled.Plan.Payload.SequenceRevision, RenderPlanDigest: compiled.Plan.Digest,
		RendererVersion: SequencePreviewRendererV1 + "@fixture", RendererTarget: "mac-arm64",
		Profile: compiled.Plan.Payload.Output.Profile, Facts: facts,
		Media: SequencePreviewArtifactFile{
			Path: "preview.webm", MimeType: "video/webm", ByteSize: mediaSize, SHA256: renderDigest("9"),
		},
	}
	canonical, _, err := domain.CanonicalDigest(
		"open-cut/sequence-preview-artifact", domain.SequencePreviewArtifactSchema, manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSequencePreviewArtifactManifest(canonical)
	if err != nil || decoded != manifest {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
}

func TestSequencePreviewPresentationDurationUsesLongestPhysicalGrid(t *testing.T) {
	fixture := renderPlanFixture(t)
	duration, _ := domain.NewRationalTime(1, 100)
	for index := range fixture.Clips {
		fixture.Clips[index].SourceRange.Duration = duration
		fixture.Clips[index].TimelineRange.Duration = duration
	}
	compiled, err := CompileSequencePreviewPlan(fixture)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := SequencePreviewFactsForPlan(compiled.Plan.Payload)
	want, _ := domain.NewRationalTime(1, 30)
	if err != nil || facts.SemanticDuration != duration || facts.PresentationDuration != want {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
}

func TestSequencePreviewPlanRejectsOutputBeyondStableProfileBounds(t *testing.T) {
	fixture := renderPlanFixture(t)
	duration, _ := domain.NewRationalTime(MaximumSequencePreviewAudioSamples+1, 48_000)
	for index := range fixture.Clips {
		fixture.Clips[index].SourceRange.Duration = duration
		fixture.Clips[index].TimelineRange.Duration = duration
	}
	if _, err := CompileSequencePreviewPlan(fixture); !errors.Is(err, ErrRenderPlanInvalid) {
		t.Fatalf("oversized preview err=%v", err)
	}
}
