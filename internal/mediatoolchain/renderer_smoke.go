package mediatoolchain

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

type RendererSmokeInput struct {
	FFmpegPath   string
	FFmpegSHA256 string
	FontRoot     string
	Font         ResourceRecord
}

type RendererSmokeObservation struct {
	OutputSHA256 string
	OutputBytes  uint64
}

func runRendererHelperSmoke(
	ctx context.Context,
	executable, workRoot string,
	buildTarget target.Target,
	input RendererSmokeInput,
) (RendererSmokeObservation, error) {
	if !cleanAbsolute(executable) || !cleanAbsolute(workRoot) || buildTarget != target.Host() ||
		!cleanAbsolute(input.FFmpegPath) || !cleanAbsolute(input.FontRoot) ||
		input.FFmpegSHA256 == "" || input.Font.ID != renderengine.CaptionFontBundleID ||
		input.Font.Version != renderengine.CaptionFontBundleVersion || input.Font.SHA256 == "" {
		return RendererSmokeObservation{}, fmt.Errorf("renderer smoke input is invalid")
	}
	plan, err := rendererSmokePlan(input.Font)
	if err != nil {
		return RendererSmokeObservation{}, err
	}
	attemptRoot, err := os.MkdirTemp(workRoot, "renderer-smoke-")
	if err != nil {
		return RendererSmokeObservation{}, err
	}
	defer os.RemoveAll(attemptRoot)
	attemptRoot, err = filepath.EvalSymlinks(attemptRoot)
	if err != nil {
		return RendererSmokeObservation{}, err
	}
	manifest, encoded, err := renderengine.CompileExecutionManifest(
		plan,
		application.SequencePreviewRendererIdentity{
			Version: application.SequencePreviewRendererV1 + "@relink-smoke", Target: buildTarget.String(),
		},
		renderengine.ExecutionClosure{
			SHA256: domain.Digest("sha256:" + strings.Repeat("c", 64)),
			Tools: map[string]renderengine.ExecutionToolPin{
				"ffmpeg": {Path: input.FFmpegPath, SHA256: domain.Digest(input.FFmpegSHA256)},
			},
		},
		renderengine.MaterialPaths{
			ArtifactRoots: map[string]string{}, Resources: map[string]string{input.Font.ID: input.FontRoot},
		},
	)
	if err != nil {
		return RendererSmokeObservation{}, err
	}
	executionPath := filepath.Join(attemptRoot, renderengine.ExecutionFilename)
	if err := atomicfile.Write(executionPath, encoded, 0o600); err != nil {
		return RendererSmokeObservation{}, err
	}
	executionContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var diagnostic rendererBoundedBuffer
	diagnostic.limit = 64 << 10
	if err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executable, Args: []string{"--execution", executionPath}, Directory: attemptRoot,
		Env: []string{"LANG=C"}, Stdin: nil, Stdout: io.Discard, Stderr: &diagnostic,
		Profile: lifecycle.ProfilePackaged, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	}); err != nil {
		detail := strings.TrimSpace(diagnostic.String())
		if diagnostic.exceeded {
			detail += " [diagnostic truncated]"
		}
		if detail == "" {
			detail = "no diagnostic"
		}
		return RendererSmokeObservation{}, fmt.Errorf("run relinked renderer smoke: %w: %s", err, detail)
	}
	resultBytes, err := os.ReadFile(filepath.Join(attemptRoot, renderengine.ResultFilename))
	if err != nil {
		return RendererSmokeObservation{}, err
	}
	result, err := renderengine.DecodeResult(resultBytes)
	if err != nil || result.Status != renderengine.ResultSucceeded || result.Output == nil ||
		result.Output.RelativePath != manifest.Output.RelativePath {
		return RendererSmokeObservation{}, fmt.Errorf("relinked renderer smoke result is invalid")
	}
	outputDigest, outputSize, err := digestFile(filepath.Join(attemptRoot, manifest.Output.RelativePath))
	if err != nil || result.Output.SHA256.String() != outputDigest || result.Output.ByteSize.Value() != outputSize {
		return RendererSmokeObservation{}, fmt.Errorf("relinked renderer smoke output is invalid")
	}
	return RendererSmokeObservation{OutputSHA256: outputDigest, OutputBytes: outputSize}, nil
}

func rendererSmokePlan(font ResourceRecord) (application.PublishedRenderPlan, error) {
	projectID, err := domain.ParseProjectID("00000000-0000-7000-8000-000000000001")
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	sequenceID, err := domain.ParseSequenceID("00000000-0000-7000-8000-000000000002")
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	trackID, err := domain.ParseTrackID("00000000-0000-7000-8000-000000000003")
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	captionID, err := domain.ParseCaptionID("00000000-0000-7000-8000-000000000004")
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	revision, err := domain.NewRevision(1)
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	zero, err := domain.NewRationalTime(0, 1)
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	one, err := domain.NewRationalTime(1, 1)
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	format := domain.DefaultSequenceFormat()
	format.CanvasWidth, format.CanvasHeight = 320, 180
	compiled, err := application.CompileSequencePreviewPlan(application.CompileRenderPlanInput{
		ProjectID: projectID, ObservedProjectRevision: revision,
		Sequence: domain.Sequence{
			ID: sequenceID, Revision: revision, Name: "relink smoke", Role: domain.SequenceRoleMain,
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
		return application.PublishedRenderPlan{}, err
	}
	return application.PublishedRenderPlan{Plan: compiled.Plan}, nil
}
