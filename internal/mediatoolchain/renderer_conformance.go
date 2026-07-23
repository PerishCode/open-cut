package mediatoolchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

type RendererConformanceInput struct {
	HelperPath    string
	HelperSHA256  string
	FFmpegPath    string
	FFmpegSHA256  string
	FFprobePath   string
	FFprobeSHA256 string
	FontRoot      string
	Font          ResourceRecord
}

func rendererCapabilityRecord(
	capabilityID string,
	notices []NoticeRecord,
	font ResourceRecord,
) CapabilityRecord {
	profile, exists := capabilityConformanceProfile(capabilityID)
	if !exists || (capabilityID != CapabilitySequencePreviewRendererV1 &&
		capabilityID != CapabilitySequenceExportRendererV1) {
		return CapabilityRecord{}
	}
	noticeIDs := make([]string, 0, len(notices)+1)
	for _, notice := range notices {
		noticeIDs = append(noticeIDs, notice.ID)
	}
	evidenceID := conformanceEvidenceNoticeID(capabilityID)
	noticeIDs = append(noticeIDs, evidenceID)
	slices.Sort(noticeIDs)
	return CapabilityRecord{
		ID: capabilityID, EntryToolID: "open-cut-render",
		ToolIDs: []string{"ffmpeg", "ffprobe", "open-cut-render"}, ResourceIDs: []string{font.ID},
		NoticeIDs: noticeIDs, ConformanceProfile: profile,
		ConformanceSuiteSHA256:      conformanceSuiteDigest(capabilityID),
		ConformanceEvidenceNoticeID: evidenceID,
	}
}

func stageRendererConformanceEvidence(
	ctx context.Context,
	buildTarget target.Target,
	stageRoot string,
	tools []ToolRecord,
	font ResourceRecord,
	capability CapabilityRecord,
) (NoticeRecord, error) {
	toolByID := make(map[string]ToolRecord, len(tools))
	for _, tool := range tools {
		toolByID[tool.ID] = tool
	}
	input, err := rendererConformanceInputFromRecords(stageRoot, toolByID, font)
	if err != nil {
		return NoticeRecord{}, err
	}
	observations, err := qualifyRendererCapability(ctx, buildTarget, capability.ID, input)
	if err != nil {
		return NoticeRecord{}, err
	}
	evidence, err := buildConformanceEvidence(
		buildTarget, capability, toolByID,
		map[string]ResourceRecord{font.ID: font}, observations,
	)
	if err != nil {
		return NoticeRecord{}, err
	}
	return writeConformanceEvidence(stageRoot, evidence)
}

func rendererConformanceInputFromRecords(
	root string,
	tools map[string]ToolRecord,
	font ResourceRecord,
) (RendererConformanceInput, error) {
	helper, helperOK := tools["open-cut-render"]
	ffmpeg, ffmpegOK := tools["ffmpeg"]
	ffprobe, ffprobeOK := tools["ffprobe"]
	if !helperOK || !ffmpegOK || !ffprobeOK {
		return RendererConformanceInput{}, fmt.Errorf("renderer conformance tool closure is unavailable")
	}
	return RendererConformanceInput{
		HelperPath: filepath.Join(root, filepath.FromSlash(helper.Path)), HelperSHA256: helper.SHA256,
		FFmpegPath: filepath.Join(root, filepath.FromSlash(ffmpeg.Path)), FFmpegSHA256: ffmpeg.SHA256,
		FFprobePath: filepath.Join(root, filepath.FromSlash(ffprobe.Path)), FFprobeSHA256: ffprobe.SHA256,
		FontRoot: filepath.Join(root, filepath.FromSlash(font.Root)), Font: font,
	}, nil
}

func rendererConformanceInputFromVerified(
	verified Verified,
) (RendererConformanceInput, error) {
	helper, helperOK := verified.Tools["open-cut-render"]
	ffmpeg, ffmpegOK := verified.Tools["ffmpeg"]
	ffprobe, ffprobeOK := verified.Tools["ffprobe"]
	font, fontOK := verified.Resources[renderengine.CaptionFontBundleID]
	fontRecord := resourceRecord(verified.Manifest.Resources, renderengine.CaptionFontBundleID)
	if !helperOK || !ffmpegOK || !ffprobeOK || !fontOK || fontRecord.ID == "" {
		return RendererConformanceInput{}, fmt.Errorf("verified renderer conformance closure is unavailable")
	}
	return RendererConformanceInput{
		HelperPath: helper.Path, HelperSHA256: helper.SHA256,
		FFmpegPath: ffmpeg.Path, FFmpegSHA256: ffmpeg.SHA256,
		FFprobePath: ffprobe.Path, FFprobeSHA256: ffprobe.SHA256,
		FontRoot: font.Root, Font: fontRecord,
	}, nil
}

func verifyRendererConformanceEvidence(
	ctx context.Context,
	verified Verified,
	tools map[string]ToolRecord,
	resources map[string]ResourceRecord,
	capabilityID string,
) error {
	capability, exists := verified.Capabilities[capabilityID]
	if !exists {
		return nil
	}
	input, err := rendererConformanceInputFromVerified(verified)
	if err != nil {
		return err
	}
	observations, err := qualifyRendererCapability(ctx, verified.Manifest.Target, capabilityID, input)
	if err != nil {
		return fmt.Errorf("%s failed conformance: %w", capabilityID, err)
	}
	record := capabilityRecord(verified.Manifest.Capabilities, capabilityID)
	expected, err := buildConformanceEvidence(
		verified.Manifest.Target, record, tools, resources, observations,
	)
	if err != nil {
		return err
	}
	actual, err := readConformanceEvidence(filepath.Join(
		verified.Root, filepath.FromSlash(capability.ConformanceEvidence.Path),
	))
	if err != nil || !conformanceEvidenceEqual(actual, expected) {
		return fmt.Errorf("%s conformance evidence mismatch", capabilityID)
	}
	return nil
}

func qualifyRendererCapability(
	ctx context.Context,
	buildTarget target.Target,
	capabilityID string,
	input RendererConformanceInput,
) ([]ConformanceObservation, error) {
	if buildTarget != target.Host() || !cleanAbsolute(input.HelperPath) || !cleanAbsolute(input.FFmpegPath) ||
		!cleanAbsolute(input.FFprobePath) || !cleanAbsolute(input.FontRoot) ||
		!validDigest(input.HelperSHA256) || !validDigest(input.FFmpegSHA256) ||
		!validDigest(input.FFprobeSHA256) || input.Font.ID != renderengine.CaptionFontBundleID ||
		input.Font.Version != renderengine.CaptionFontBundleVersion || !validDigest(input.Font.SHA256) ||
		(capabilityID != CapabilitySequencePreviewRendererV1 &&
			capabilityID != CapabilitySequenceExportRendererV1) {
		return nil, fmt.Errorf("renderer conformance input is invalid")
	}
	root, err := os.MkdirTemp("", "open-cut-renderer-conformance-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	sourcePath := filepath.Join(root, "source.avi")
	if err := os.WriteFile(sourcePath, conformanceAVI(), 0o600); err != nil {
		return nil, err
	}
	mediaPath := filepath.Join(root, "source-proxy.webm")
	purpose := domain.RenderPurposeSequencePreview
	if capabilityID == CapabilitySequenceExportRendererV1 {
		mediaPath = filepath.Join(root, "render-input.mkv")
		purpose = domain.RenderPurposeExport
		if err := runConformanceRenderInputEncode(ctx, input.FFmpegPath, root, sourcePath, mediaPath); err != nil {
			return nil, fmt.Errorf("prepare renderer conformance render input: %w", err)
		}
	} else if err := runConformanceProxyEncode(ctx, input.FFmpegPath, root, sourcePath, mediaPath); err != nil {
		return nil, fmt.Errorf("prepare renderer conformance source proxy: %w", err)
	}
	fixtures, err := rendererConformanceFixtures(root, mediaPath, input.FontRoot, input.Font, purpose)
	if err != nil {
		return nil, err
	}
	closureDigest := rendererConformanceClosureDigest(
		capabilityID, input.HelperSHA256, input.FFmpegSHA256, input.FFprobeSHA256, input.Font,
	)
	observations := make([]ConformanceObservation, 0, len(fixtures)*7)
	for _, fixture := range fixtures {
		current, err := qualifyRendererFixture(ctx, root, buildTarget, input, closureDigest, fixture)
		if err != nil {
			return nil, fmt.Errorf("qualify renderer fixture %s: %w", fixture.ID, err)
		}
		observations = append(observations, current...)
	}
	return observations, nil
}

func qualifyRendererFixture(
	ctx context.Context,
	root string,
	buildTarget target.Target,
	input RendererConformanceInput,
	closureDigest domain.Digest,
	fixture rendererConformanceFixture,
) ([]ConformanceObservation, error) {
	videoPlan, err := rendererConformanceVideoPlan(fixture)
	if err != nil {
		return nil, err
	}
	audioPlan, err := rendererConformanceAudioPlan(fixture)
	if err != nil {
		return nil, err
	}
	var first renderengine.ResultDocument
	for attempt := 0; attempt < 2; attempt++ {
		result, err := runRendererConformanceFixture(
			ctx, root, buildTarget, input, closureDigest, fixture,
		)
		if err != nil {
			return nil, err
		}
		if attempt == 0 {
			first = result
		} else if !reflect.DeepEqual(result.Output, first.Output) ||
			!reflect.DeepEqual(result.Evaluation, first.Evaluation) {
			return nil, fmt.Errorf("renderer fixture output is not byte stable")
		}
	}
	if first.Output == nil || first.Evaluation == nil {
		return nil, fmt.Errorf("renderer fixture result is incomplete")
	}
	facts, err := application.RenderedMediaFactsForPlan(fixture.Plan.Plan.Payload)
	if err != nil {
		return nil, err
	}
	planObservation := struct {
		PlanDigest       domain.Digest       `json:"planDigest"`
		Duration         domain.RationalTime `json:"duration"`
		VideoFrameCount  domain.UInt64       `json:"videoFrameCount"`
		AudioSampleCount domain.UInt64       `json:"audioSampleCount"`
	}{
		fixture.Plan.Plan.Digest, fixture.Plan.Plan.Payload.Duration,
		fixture.Plan.Plan.Payload.Output.VideoFrameCount,
		fixture.Plan.Plan.Payload.Output.AudioSampleCount,
	}
	return []ConformanceObservation{
		{ID: fixture.ID + "-audio-plan", SHA256: digestConformanceJSON(audioPlan)},
		{ID: fixture.ID + "-media-facts", SHA256: digestConformanceJSON(facts)},
		{ID: fixture.ID + "-output", SHA256: first.Output.SHA256.String()},
		{ID: fixture.ID + "-plan", SHA256: digestConformanceJSON(planObservation)},
		{ID: fixture.ID + "-raw-audio", SHA256: first.Evaluation.Audio.SHA256.String()},
		{ID: fixture.ID + "-raw-video", SHA256: first.Evaluation.Video.SHA256.String()},
		{ID: fixture.ID + "-video-plan", SHA256: digestConformanceJSON(videoPlan)},
	}, nil
}

func rendererConformanceVideoPlan(fixture rendererConformanceFixture) (any, error) {
	manifest, err := rendererConformanceManifest(fixture)
	if err != nil {
		return nil, err
	}
	plan, err := renderengine.CompileVideoDecodePlan(manifest)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func rendererConformanceAudioPlan(fixture rendererConformanceFixture) (any, error) {
	manifest, err := rendererConformanceManifest(fixture)
	if err != nil {
		return nil, err
	}
	plan, err := renderengine.CompileAudioDecodePlan(manifest)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func rendererConformanceManifest(
	fixture rendererConformanceFixture,
) (renderengine.ExecutionManifest, error) {
	ffmpeg, err := os.Executable()
	if err != nil {
		return renderengine.ExecutionManifest{}, err
	}
	ffmpeg, err = filepath.EvalSymlinks(ffmpeg)
	if err != nil {
		return renderengine.ExecutionManifest{}, err
	}
	manifest, _, err := renderengine.CompileExecutionManifest(
		fixture.Plan.Plan,
		application.SequencePreviewRendererIdentity{Version: "conformance-plan-v1", Target: target.Host().String()},
		renderengine.ExecutionClosure{
			SHA256: domain.Digest("sha256:" + string(bytes.Repeat([]byte{'c'}, 64))),
			Tools: map[string]renderengine.ExecutionToolPin{
				"ffmpeg": {Path: ffmpeg, SHA256: domain.Digest("sha256:" + string(bytes.Repeat([]byte{'d'}, 64)))},
			},
		},
		fixture.Materials,
	)
	return manifest, err
}

func runRendererConformanceFixture(
	ctx context.Context,
	root string,
	buildTarget target.Target,
	input RendererConformanceInput,
	closureDigest domain.Digest,
	fixture rendererConformanceFixture,
) (renderengine.ResultDocument, error) {
	attemptRoot, err := os.MkdirTemp(root, fixture.ID+"-")
	if err != nil {
		return renderengine.ResultDocument{}, err
	}
	defer os.RemoveAll(attemptRoot)
	attemptRoot, err = filepath.EvalSymlinks(attemptRoot)
	if err != nil {
		return renderengine.ResultDocument{}, err
	}
	manifest, encoded, err := renderengine.CompileExecutionManifest(
		fixture.Plan.Plan,
		application.SequencePreviewRendererIdentity{
			Version: toolchainVersion + "@renderer-conformance", Target: buildTarget.String(),
		},
		renderengine.ExecutionClosure{
			SHA256: closureDigest,
			Tools: map[string]renderengine.ExecutionToolPin{
				"ffmpeg": {Path: input.FFmpegPath, SHA256: domain.Digest(input.FFmpegSHA256)},
			},
		},
		fixture.Materials,
	)
	if err != nil {
		return renderengine.ResultDocument{}, err
	}
	executionPath := filepath.Join(attemptRoot, renderengine.ExecutionFilename)
	if err := atomicfile.Write(executionPath, encoded, 0o600); err != nil {
		return renderengine.ResultDocument{}, err
	}
	executionContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var diagnostics limitedConformanceBuffer
	diagnostics.limit = 32 << 10
	err = lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: input.HelperPath, Args: []string{"--execution", executionPath}, Directory: attemptRoot,
		Env: conformanceEnvironment(), Stdin: nil, Stdout: io.Discard, Stderr: &diagnostics,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	})
	if err != nil || diagnostics.exceeded {
		return renderengine.ResultDocument{}, fmt.Errorf("renderer helper failed (%v): %s", err, diagnostics.String())
	}
	resultBytes, err := os.ReadFile(filepath.Join(attemptRoot, renderengine.ResultFilename))
	if err != nil {
		return renderengine.ResultDocument{}, err
	}
	result, err := renderengine.DecodeResult(resultBytes)
	if err != nil || result.Status != renderengine.ResultSucceeded || result.Output == nil || result.Evaluation == nil ||
		result.Output.RelativePath != manifest.Output.RelativePath {
		return renderengine.ResultDocument{}, fmt.Errorf("renderer helper returned an invalid success result")
	}
	outputPath := filepath.Join(attemptRoot, result.Output.RelativePath)
	digest, size, err := digestFile(outputPath)
	if err != nil || digest != result.Output.SHA256.String() || size != result.Output.ByteSize.Value() {
		return renderengine.ResultDocument{}, fmt.Errorf("renderer helper output digest changed")
	}
	if err := validateRendererConformanceEvaluation(manifest, *result.Evaluation); err != nil {
		return renderengine.ResultDocument{}, err
	}
	if err := verifyRendererConformanceMedia(ctx, input.FFprobePath, attemptRoot, outputPath, fixture.Plan); err != nil {
		return renderengine.ResultDocument{}, err
	}
	return result, nil
}

func validateRendererConformanceEvaluation(
	manifest renderengine.ExecutionManifest,
	evaluation renderengine.ResultEvaluation,
) error {
	videoBytes := uint64(manifest.Plan.Output.CanvasWidth) * uint64(manifest.Plan.Output.CanvasHeight) * 3 / 2
	videoBytes *= manifest.Plan.Output.VideoFrameCount.Value()
	audioBytes := manifest.Plan.Output.AudioSampleCount.Value() * 4
	if evaluation.Video.ByteSize.Value() != videoBytes || evaluation.Audio.ByteSize.Value() != audioBytes {
		return fmt.Errorf("renderer evaluation byte counts are invalid")
	}
	return nil
}

func decodeRendererConformanceProbe(data []byte) (renderengine.SequencePreviewProbeDocument, error) {
	var document renderengine.SequencePreviewProbeDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return renderengine.SequencePreviewProbeDocument{}, fmt.Errorf(
			"renderer conformance probe returned invalid JSON: %w", err,
		)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return renderengine.SequencePreviewProbeDocument{}, fmt.Errorf("renderer conformance probe returned trailing JSON")
	}
	return document, nil
}
