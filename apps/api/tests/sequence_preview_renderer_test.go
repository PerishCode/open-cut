package tests

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestExternalSequencePreviewRendererUsesPrivateManifestAndAttemptWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture helper is a POSIX script")
	}
	plan := serviceCaptionRenderPlan(t)
	root := t.TempDir()
	helper := filepath.Join(root, "open-cut-render")
	preview := []byte("fixture-preview-webm")
	digest := sha256.Sum256(preview)
	videoEvaluationBytes := uint64(plan.Plan.Payload.Output.CanvasWidth) *
		uint64(plan.Plan.Payload.Output.CanvasHeight) * 3 / 2 *
		plan.Plan.Payload.Output.VideoFrameCount.Value()
	audioEvaluationBytes := plan.Plan.Payload.Output.AudioSampleCount.Value() * 4
	result := fmt.Sprintf(
		`{"schema":%d,"status":"success","evaluation":{"video":{"byteSize":"%d","sha256":"sha256:%s"},"audio":{"byteSize":"%d","sha256":"sha256:%s"}},"output":{"relativePath":"preview.webm","byteSize":"%d","sha256":"sha256:%x"}}`,
		renderengine.ResultSchema,
		videoEvaluationBytes, strings.Repeat("c", 64),
		audioEvaluationBytes, strings.Repeat("d", 64), len(preview), digest,
	)
	script := []byte("#!/bin/sh\n" +
		"test \"$1\" = \"--execution\" || exit 2\n" +
		"test -f \"$2\" || exit 3\n" +
		"printf 'fixture-preview-webm' > preview.webm\n" +
		"printf '" + result + "' > result.json\n")
	if err := os.WriteFile(helper, script, 0o700); err != nil {
		t.Fatal(err)
	}
	fontRoot := filepath.Join(root, "font-bundle")
	if err := os.Mkdir(fontRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	identity := application.SequencePreviewRendererIdentity{
		Version: "fixture-open-cut-render-v1", Target: target.Host().String(),
	}
	resolver := &fixtureSequencePreviewRootResolver{}
	attemptRoot := filepath.Join(root, "attempts")
	ffmpeg := filepath.Join(root, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("fixture-ffmpeg"), 0o700); err != nil {
		t.Fatal(err)
	}
	renderer, err := service.NewExternalSequencePreviewRenderer(
		resolver, helper, identity, renderengine.ExecutionClosure{
			SHA256: domain.Digest("sha256:" + strings.Repeat("a", 64)),
			Tools: map[string]renderengine.ExecutionToolPin{
				"ffmpeg": {Path: ffmpeg, SHA256: domain.Digest("sha256:" + strings.Repeat("b", 64))},
			},
		},
		map[string]string{"font:noto-caption-bundle": fontRoot},
		attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	jobID := mustServiceRenderID(t, domain.ParseWorkJobID, "00000000-0000-7000-8000-000000000005")
	attemptID := mustServiceRenderID(t, domain.ParseJobAttemptID, "00000000-0000-7000-8000-000000000006")
	claim := application.WorkJobClaim{
		JobID: jobID, AttemptID: attemptID, Kind: domain.WorkJobSequencePreview,
		ExecutorVersion: identity.Version,
		SequencePreview: &application.SequencePreviewJobClaim{
			ProjectID: plan.Plan.Payload.ProjectID, SequenceID: plan.Plan.Payload.SequenceID,
			SequenceRevision: plan.Plan.Payload.SequenceRevision,
			Parameters: application.SequencePreviewJobParameters{
				RendererVersion: identity.Version, RendererTarget: identity.Target,
			},
		},
	}
	execution, err := renderer.Render(
		context.Background(), application.SequencePreviewRenderRequest{
			Claim: claim, Plan: plan, ObservedAt: time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || execution.Media.Path != "preview.webm" ||
		execution.Media.ByteSize.Value() != uint64(len("fixture-preview-webm")) {
		t.Fatalf("resolver calls=%d execution=%+v", resolver.calls, execution)
	}
	reader, err := execution.Workspace.Open("preview.webm")
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil || string(bytes) != "fixture-preview-webm" {
		t.Fatalf("bytes=%q err=%v close=%v", bytes, err, closeErr)
	}
	physicalAttempt := filepath.Join(attemptRoot, attemptID.String())
	if _, err := os.Stat(filepath.Join(physicalAttempt, "execution.json")); !os.IsNotExist(err) {
		t.Fatalf("private execution manifest survived helper completion: %v", err)
	}
	if _, err := os.Stat(filepath.Join(physicalAttempt, renderengine.ResultFilename)); !os.IsNotExist(err) {
		t.Fatalf("private render result survived helper completion: %v", err)
	}
	if err := execution.Workspace.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(physicalAttempt); !os.IsNotExist(err) {
		t.Fatalf("attempt workspace survived release: %v", err)
	}
}

func TestExternalSequencePreviewRendererPreservesTypedHelperFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture helper is a POSIX script")
	}
	plan := serviceCaptionRenderPlan(t)
	root := t.TempDir()
	helper := filepath.Join(root, "open-cut-render")
	diagnostic := fmt.Sprintf(`{"schema":%d,"status":"failed","diagnostic":{"code":"render-glyph-missing","subjectKind":"caption","subjectId":"00000000-0000-7000-8000-000000000004"}}`, renderengine.ResultSchema)
	script := []byte("#!/bin/sh\n" +
		"printf '" + diagnostic + "' > result.json\n" +
		"exit 9\n")
	if err := os.WriteFile(helper, script, 0o700); err != nil {
		t.Fatal(err)
	}
	fontRoot := filepath.Join(root, "font-bundle")
	if err := os.Mkdir(fontRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(root, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("fixture-ffmpeg"), 0o700); err != nil {
		t.Fatal(err)
	}
	identity := application.SequencePreviewRendererIdentity{
		Version: "fixture-open-cut-render-v1", Target: target.Host().String(),
	}
	attemptRoot := filepath.Join(root, "attempts")
	renderer, err := service.NewExternalSequencePreviewRenderer(
		&fixtureSequencePreviewRootResolver{}, helper, identity,
		renderengine.ExecutionClosure{
			SHA256: domain.Digest("sha256:" + strings.Repeat("a", 64)),
			Tools: map[string]renderengine.ExecutionToolPin{
				"ffmpeg": {Path: ffmpeg, SHA256: domain.Digest("sha256:" + strings.Repeat("b", 64))},
			},
		},
		map[string]string{"font:noto-caption-bundle": fontRoot}, attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	jobID := mustServiceRenderID(t, domain.ParseWorkJobID, "00000000-0000-7000-8000-000000000015")
	attemptID := mustServiceRenderID(t, domain.ParseJobAttemptID, "00000000-0000-7000-8000-000000000016")
	claim := application.WorkJobClaim{
		JobID: jobID, AttemptID: attemptID, Kind: domain.WorkJobSequencePreview,
		ExecutorVersion: identity.Version,
		SequencePreview: &application.SequencePreviewJobClaim{
			ProjectID: plan.Plan.Payload.ProjectID, SequenceID: plan.Plan.Payload.SequenceID,
			SequenceRevision: plan.Plan.Payload.SequenceRevision,
			Parameters: application.SequencePreviewJobParameters{
				RendererVersion: identity.Version, RendererTarget: identity.Target,
			},
		},
	}
	_, err = renderer.Render(context.Background(), application.SequencePreviewRenderRequest{
		Claim: claim, Plan: plan, ObservedAt: time.Now().UTC(),
	})
	var failure application.SequencePreviewExecutionError
	if !errors.As(err, &failure) || failure.Code != renderengine.ResultCodeGlyphMissing {
		t.Fatalf("typed helper failure = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(attemptRoot, attemptID.String())); !os.IsNotExist(statErr) {
		t.Fatalf("failed attempt workspace survived: %v", statErr)
	}
}

type fixtureSequencePreviewRootResolver struct{ calls int }

func (resolver *fixtureSequencePreviewRootResolver) ResolveSequencePreviewArtifactRoots(
	_ context.Context,
	_ application.WorkJobClaim,
	_ domain.Digest,
	inputs []domain.RenderPlanInput,
	_ time.Time,
) (map[string]string, error) {
	resolver.calls++
	if len(inputs) != 0 {
		return nil, application.ErrRenderPlanInvalid
	}
	return map[string]string{}, nil
}

func serviceCaptionRenderPlan(t *testing.T) application.PublishedRenderPlan {
	t.Helper()
	projectID := mustServiceRenderID(t, domain.ParseProjectID, "00000000-0000-7000-8000-000000000001")
	sequenceID := mustServiceRenderID(t, domain.ParseSequenceID, "00000000-0000-7000-8000-000000000002")
	trackID := mustServiceRenderID(t, domain.ParseTrackID, "00000000-0000-7000-8000-000000000003")
	captionID := mustServiceRenderID(t, domain.ParseCaptionID, "00000000-0000-7000-8000-000000000004")
	revision, _ := domain.NewRevision(1)
	zero, _ := domain.NewRationalTime(0, 1)
	one, _ := domain.NewRationalTime(1, 1)
	compiled, err := application.CompileSequencePreviewPlan(application.CompileRenderPlanInput{
		ProjectID: projectID, ObservedProjectRevision: revision,
		Sequence: domain.Sequence{
			ID: sequenceID, Revision: revision, Name: "main", Role: domain.SequenceRoleMain,
			Format: domain.DefaultSequenceFormat(),
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
	})
	if err != nil {
		t.Fatal(err)
	}
	return application.PublishedRenderPlan{Plan: compiled.Plan}
}

func mustServiceRenderID[T any](t *testing.T, parse func(string) (T, error), value string) T {
	t.Helper()
	result, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
