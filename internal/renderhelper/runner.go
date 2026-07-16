package renderhelper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/internal/rendernative"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

// Run executes the private sequence-preview protocol. It writes exactly one
// result document beside a valid canonical execution path. A returned error
// means the process must exit non-zero, including when a closed failure result
// was written successfully.
func Run(ctx context.Context, executionPath string) error {
	attemptRoot, err := validateInvocation(executionPath)
	if err != nil {
		return err
	}
	resultPath := filepath.Join(attemptRoot, renderengine.ResultFilename)
	if _, err := os.Lstat(resultPath); !os.IsNotExist(err) {
		return fmt.Errorf("private render result already exists")
	}

	result, renderErr := execute(ctx, executionPath, attemptRoot)
	encoded, encodeErr := renderengine.EncodeResult(result)
	if encodeErr != nil {
		return fmt.Errorf("encode private render result: %w", encodeErr)
	}
	if err := atomicfile.Write(resultPath, encoded, 0o600); err != nil {
		return fmt.Errorf("publish private render result: %w", err)
	}
	return renderErr
}

func execute(
	ctx context.Context,
	executionPath, attemptRoot string,
) (renderengine.ResultDocument, error) {
	encoded, err := readBoundedRegular(executionPath, renderengine.MaximumExecutionBytes)
	if err != nil {
		return failedResult(failure(
			renderengine.ResultCodePlanInvalid, "plan", renderengine.ExecutionFilename, err,
		))
	}
	manifest, err := renderengine.DecodeExecutionManifest(encoded)
	if err != nil {
		return failedResult(failure(
			renderengine.ResultCodePlanInvalid, "plan", renderengine.ExecutionFilename, err,
		))
	}
	if err := verifyTool(manifest.Tools[0]); err != nil {
		return failedResult(failure(
			renderengine.ResultCodeInputUnavailable, "tool", manifest.Tools[0].ID, err,
		))
	}
	outputPath := filepath.Join(attemptRoot, manifest.Output.RelativePath)
	if _, err := os.Lstat(outputPath); !os.IsNotExist(err) {
		return failedResult(failure(
			renderengine.ResultCodeEncode, "output", manifest.Output.RelativePath,
			fmt.Errorf("private render output already exists"),
		))
	}

	video, err := videoProducer(manifest, attemptRoot)
	if err != nil {
		return failedResult(classify(err, renderengine.ResultCodePlanInvalid, "plan", "video-evaluator"))
	}
	audio, err := renderengine.NewAudioStreamProducer(manifest, attemptRoot, lifecycle.ProfilePackaged)
	if err != nil {
		return failedResult(classify(err, renderengine.ResultCodePlanInvalid, "plan", "audio-evaluator"))
	}
	video, videoObservation, err := observeStream(video)
	if err != nil {
		return failedResult(failure(renderengine.ResultCodeInternal, "plan", "video-observation", err))
	}
	audio, audioObservation, err := observeStream(audio)
	if err != nil {
		return failedResult(failure(renderengine.ResultCodeInternal, "plan", "audio-observation", err))
	}
	executionContext, cancel := context.WithTimeout(
		ctx, time.Duration(manifest.Budget.WallTimeSeconds)*time.Second,
	)
	defer cancel()
	err = renderengine.RunRawAVPipeline(
		executionContext, manifest, attemptRoot, lifecycle.ProfilePackaged,
		renderengine.RawAVProducers{Video: video, Audio: audio},
	)
	if err != nil {
		return failedResult(classify(err, renderengine.ResultCodeEncode, "output", manifest.Output.RelativePath))
	}
	digest, size, err := digestOutput(outputPath, manifest.Budget.OutputByteLimit)
	if err != nil {
		return failedResult(failure(
			renderengine.ResultCodeEncode, "output", manifest.Output.RelativePath, err,
		))
	}
	byteSize, err := domain.NewUInt64(size)
	if err != nil {
		return failedResult(failure(
			renderengine.ResultCodeResourceLimit, "output", manifest.Output.RelativePath, err,
		))
	}
	videoResult, err := videoObservation.result()
	if err != nil {
		return failedResult(failure(renderengine.ResultCodeInternal, "plan", "video-observation", err))
	}
	audioResult, err := audioObservation.result()
	if err != nil {
		return failedResult(failure(renderengine.ResultCodeInternal, "plan", "audio-observation", err))
	}
	return renderengine.ResultDocument{
		Schema: renderengine.ResultSchema, Status: renderengine.ResultSucceeded,
		Evaluation: &renderengine.ResultEvaluation{Video: videoResult, Audio: audioResult},
		Output: &renderengine.ResultOutput{
			RelativePath: manifest.Output.RelativePath, ByteSize: byteSize, SHA256: digest,
		},
	}, nil
}

func videoProducer(
	manifest renderengine.ExecutionManifest,
	attemptRoot string,
) (renderengine.StreamProducer, error) {
	if len(manifest.Plan.Captions) == 0 {
		if len(manifest.Resources) != 0 {
			return nil, fmt.Errorf("caption-free execution contains an ambient font resource")
		}
		return renderengine.NewVideoStreamProducer(manifest, attemptRoot, lifecycle.ProfilePackaged)
	}
	if len(manifest.Resources) != 1 {
		return nil, fmt.Errorf("caption execution font closure is invalid")
	}
	resource := manifest.Resources[0]
	bundle, err := renderengine.VerifyCaptionFontResource(
		resource.Path, resource.ID, resource.Version, resource.SHA256,
	)
	if err != nil {
		return nil, failure(renderengine.ResultCodeFontUnavailable, "resource", resource.ID, err)
	}
	native, err := rendernative.New(resource.Path, bundle)
	if err != nil {
		return nil, failure(renderengine.ResultCodeFontUnavailable, "resource", resource.ID, err)
	}
	captions, err := renderengine.NewCaptionCoverageEvaluator(manifest, bundle, native)
	if err != nil {
		return nil, err
	}
	return renderengine.NewCaptionedVideoStreamProducer(
		manifest, attemptRoot, lifecycle.ProfilePackaged, captions,
	)
}

type renderFailure struct {
	diagnostic renderengine.ResultDiagnostic
	cause      error
}

func (current renderFailure) Error() string {
	if current.cause == nil {
		return current.diagnostic.Code
	}
	return current.diagnostic.Code + ": " + current.cause.Error()
}

func (current renderFailure) Unwrap() error { return current.cause }

func failure(code, subjectKind, subjectID string, cause error) error {
	return renderFailure{
		diagnostic: renderengine.ResultDiagnostic{
			Code: code, SubjectKind: subjectKind, SubjectID: subjectID,
		},
		cause: cause,
	}
}

func classify(err error, fallbackCode, fallbackKind, fallbackID string) error {
	var classified renderFailure
	if errors.As(err, &classified) {
		return classified
	}
	var limit renderengine.ResourceLimitError
	if errors.As(err, &limit) {
		return failure(renderengine.ResultCodeResourceLimit, "plan", limit.Subject, err)
	}
	var missing renderengine.CaptionGlyphMissingError
	if errors.As(err, &missing) {
		return failure(renderengine.ResultCodeGlyphMissing, "caption", missing.CaptionID.String(), err)
	}
	var color renderengine.CaptionColorEmojiError
	if errors.As(err, &color) {
		return failure(renderengine.ResultCodeColorEmoji, "caption", color.CaptionID.String(), err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return failure(renderengine.ResultCodeResourceLimit, "plan", "wall-time", err)
	}
	return failure(fallbackCode, fallbackKind, fallbackID, err)
}

func failedResult(err error) (renderengine.ResultDocument, error) {
	classified := classify(err, renderengine.ResultCodeInternal, "plan", "renderer")
	var renderErr renderFailure
	if !errors.As(classified, &renderErr) {
		return renderengine.ResultDocument{}, classified
	}
	return renderengine.ResultDocument{
		Schema: renderengine.ResultSchema, Status: renderengine.ResultFailed,
		Diagnostic: &renderErr.diagnostic,
	}, classified
}

func validateInvocation(executionPath string) (string, error) {
	if executionPath == "" || !filepath.IsAbs(executionPath) || filepath.Clean(executionPath) != executionPath ||
		filepath.Base(executionPath) != renderengine.ExecutionFilename {
		return "", fmt.Errorf("private render execution path is invalid")
	}
	physical, err := filepath.EvalSymlinks(executionPath)
	if err != nil || filepath.Clean(physical) != executionPath {
		return "", fmt.Errorf("private render execution path is linked")
	}
	info, err := os.Lstat(executionPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("private render execution file is invalid")
	}
	root := filepath.Dir(executionPath)
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("private render attempt root is invalid")
	}
	return root, nil
}

func readBoundedRegular(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > int64(limit) {
		return nil, fmt.Errorf("private render file size is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(data) == 0 || len(data) > limit {
		return nil, fmt.Errorf("read private render file: %w", err)
	}
	return data, nil
}

func verifyTool(tool renderengine.ExecutionTool) error {
	digest, _, err := digestOutput(tool.Path, ^uint64(0))
	if err != nil || digest != tool.SHA256 {
		return fmt.Errorf("private render tool digest is invalid")
	}
	return nil
}

func digestOutput(path string, limit uint64) (domain.Digest, uint64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || uint64(info.Size()) > limit {
		return "", 0, fmt.Errorf("private render regular file is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() {
		return "", 0, fmt.Errorf("digest private render file: %w", err)
	}
	return domain.Digest("sha256:" + hex.EncodeToString(digest.Sum(nil))), uint64(written), nil
}
