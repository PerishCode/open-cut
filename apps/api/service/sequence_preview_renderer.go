package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

type SequencePreviewArtifactRootResolver interface {
	ResolveSequencePreviewArtifactRoots(
		context.Context,
		application.WorkJobClaim,
		domain.Digest,
		[]domain.RenderPlanInput,
		time.Time,
	) (map[string]string, error)
}

type ExternalSequencePreviewRenderer struct {
	resolver      SequencePreviewArtifactRootResolver
	executable    string
	identity      application.SequencePreviewRendererIdentity
	closure       renderengine.ExecutionClosure
	resourceRoots map[string]string
	tempRoot      string
	profile       lifecycle.Profile
	wallTime      time.Duration
}

func NewExternalSequencePreviewRenderer(
	resolver SequencePreviewArtifactRootResolver,
	executable string,
	identity application.SequencePreviewRendererIdentity,
	closure renderengine.ExecutionClosure,
	resourceRoots map[string]string,
	tempRoot string,
	profile lifecycle.Profile,
) (*ExternalSequencePreviewRenderer, error) {
	if resolver == nil || !cleanAbsolute(executable) || !cleanAbsolute(tempRoot) ||
		identity.Version == "" || len(identity.Version) > 1024 || validateRendererTarget(identity.Target) != nil ||
		closure.SHA256 == "" || len(closure.Tools) != 1 ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("sequence preview renderer configuration is invalid")
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sequence preview renderer executable is unavailable")
	}
	tool, exists := closure.Tools["ffmpeg"]
	if !exists || !cleanAbsolute(tool.Path) || tool.SHA256 == "" {
		return nil, fmt.Errorf("sequence preview renderer tool closure is invalid")
	}
	toolInfo, err := os.Lstat(tool.Path)
	if err != nil || !toolInfo.Mode().IsRegular() || toolInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sequence preview renderer tool closure is unavailable")
	}
	closure.Tools = map[string]renderengine.ExecutionToolPin{"ffmpeg": tool}
	tempRoot, err = preparePrivateRenderRoot(tempRoot)
	if err != nil {
		return nil, fmt.Errorf("create sequence preview attempt root: %w", err)
	}
	resources := make(map[string]string, len(resourceRoots))
	for id, root := range resourceRoots {
		if id == "" || strings.TrimSpace(id) != id || !cleanAbsolute(root) {
			return nil, fmt.Errorf("sequence preview renderer resource root is invalid")
		}
		resourceInfo, statErr := os.Lstat(root)
		if statErr != nil || !resourceInfo.IsDir() || resourceInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("sequence preview renderer resource root is unavailable")
		}
		resources[id] = root
	}
	return &ExternalSequencePreviewRenderer{
		resolver: resolver, executable: executable, identity: identity, closure: closure, resourceRoots: resources,
		tempRoot: tempRoot, profile: profile,
		wallTime: time.Duration(renderengine.MaximumWallTimeSeconds) * time.Second,
	}, nil
}

func (renderer *ExternalSequencePreviewRenderer) Identity() application.SequencePreviewRendererIdentity {
	return renderer.identity
}

func (renderer *ExternalSequencePreviewRenderer) Render(
	ctx context.Context,
	request application.SequencePreviewRenderRequest,
) (application.SequencePreviewRenderExecution, error) {
	claim := request.Claim
	plan := request.Plan
	if claim.Kind != domain.WorkJobSequencePreview || claim.SequencePreview == nil || claim.Media != nil ||
		request.ObservedAt.IsZero() ||
		claim.AttemptID.IsZero() || claim.JobID.IsZero() ||
		claim.SequencePreview.ProjectID != plan.Plan.Payload.ProjectID ||
		claim.SequencePreview.SequenceID != plan.Plan.Payload.SequenceID ||
		claim.SequencePreview.SequenceRevision != plan.Plan.Payload.SequenceRevision ||
		claim.SequencePreview.Parameters.RendererVersion != renderer.identity.Version ||
		claim.SequencePreview.Parameters.RendererTarget != renderer.identity.Target ||
		application.ValidateSequencePreviewRenderPlanPayload(plan.Plan.Payload) != nil {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			"renderer-input-invalid", application.ErrSequencePreviewInvalid,
		)
	}
	artifactRoots, err := renderer.resolver.ResolveSequencePreviewArtifactRoots(
		ctx, claim, plan.Plan.Digest, plan.Plan.Payload.Inputs, request.ObservedAt.UTC(),
	)
	if err != nil {
		var resourceLimit renderengine.ResourceLimitError
		if errors.As(err, &resourceLimit) {
			return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
				renderengine.ResultCodeResourceLimit, err,
			)
		}
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			"render-input-unavailable", err,
		)
	}
	resourceRoots := make(map[string]string, len(plan.Plan.Payload.FontResources))
	for _, resource := range plan.Plan.Payload.FontResources {
		root, exists := renderer.resourceRoots[resource.ResourceID]
		if !exists {
			return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
				"render-font-unavailable", application.ErrRenderFontRequired,
			)
		}
		resourceRoots[resource.ResourceID] = root
	}
	execution, executionBytes, err := renderengine.CompileExecutionManifest(
		plan.Plan, renderer.identity, renderer.closure,
		renderengine.MaterialPaths{ArtifactRoots: artifactRoots, Resources: resourceRoots},
	)
	if err != nil {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			"renderer-input-invalid", err,
		)
	}
	attemptRoot := filepath.Join(renderer.tempRoot, claim.AttemptID.String())
	if !pathWithin(renderer.tempRoot, attemptRoot) {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			"renderer-input-invalid", application.ErrSequencePreviewInvalid,
		)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	keepWorkspace := false
	defer func() {
		if !keepWorkspace {
			_ = os.RemoveAll(attemptRoot)
		}
	}()
	available, err := availableFilesystemBytes(attemptRoot)
	if err != nil || available < execution.Budget.ScratchAdmissionBytes {
		if err == nil {
			err = fmt.Errorf("render scratch requires %d bytes, only %d available",
				execution.Budget.ScratchAdmissionBytes, available)
		}
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	executionPath := filepath.Join(attemptRoot, renderengine.ExecutionFilename)
	if err := writePrivateRenderManifest(executionPath, executionBytes); err != nil {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	executionContext, cancel := context.WithTimeout(ctx, renderer.wallTime)
	defer cancel()
	stderr := &boundedBuffer{limit: 64 << 10}
	processErr := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: renderer.executable,
		Args:       []string{"--execution", executionPath}, Directory: attemptRoot,
		Env: executorEnvironment(), Stdin: nil, Stdout: io.Discard, Stderr: stderr,
		Profile: renderer.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	})
	if ctx.Err() != nil {
		return application.SequencePreviewRenderExecution{}, ctx.Err()
	}
	result, resultErr := readPrivateRenderResult(filepath.Join(attemptRoot, renderengine.ResultFilename))
	if processErr != nil {
		if executionContext.Err() == context.DeadlineExceeded {
			return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
				"renderer-timeout", executionContext.Err(),
			)
		}
		if !stderr.exceeded && resultErr == nil && result.Status == renderengine.ResultFailed &&
			result.Diagnostic != nil {
			// The closed result names the failure; the helper's stderr explains
			// it. Carry both so a real content failure stays diagnosable instead
			// of collapsing to a bare code.
			return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
				result.Diagnostic.Code, fmt.Errorf("private renderer rejected %s/%s: %s",
					result.Diagnostic.SubjectKind, result.Diagnostic.SubjectID, strings.TrimSpace(stderr.String())),
			)
		}
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			"renderer-process-failed", fmt.Errorf("private renderer failed: %s", strings.TrimSpace(stderr.String())),
		)
	}
	if stderr.exceeded || resultErr != nil || result.Status != renderengine.ResultSucceeded || result.Output == nil ||
		result.Evaluation == nil || !validPrivateRenderEvaluation(plan.Plan.Payload, *result.Evaluation) {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			"renderer-output-invalid", fmt.Errorf("private renderer returned no valid success result"),
		)
	}
	if err := os.Remove(executionPath); err != nil {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	if err := os.Remove(filepath.Join(attemptRoot, renderengine.ResultFilename)); err != nil {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	media, err := inspectSequencePreviewOutput(filepath.Join(attemptRoot, "preview.webm"))
	if err != nil {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			"renderer-output-invalid", err,
		)
	}
	if result.Output.RelativePath != media.Path || result.Output.ByteSize != media.ByteSize ||
		result.Output.SHA256 != media.SHA256 {
		return application.SequencePreviewRenderExecution{}, application.NewSequencePreviewExecutionError(
			"renderer-output-invalid", application.ErrSequencePreviewInvalid,
		)
	}
	workspace := &sequencePreviewWorkspace{root: attemptRoot}
	keepWorkspace = true
	return application.SequencePreviewRenderExecution{Media: media, Workspace: workspace}, nil
}

func readPrivateRenderResult(path string) (renderengine.ResultDocument, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > renderengine.MaximumResultBytes {
		return renderengine.ResultDocument{}, application.ErrSequencePreviewInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return renderengine.ResultDocument{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, renderengine.MaximumResultBytes+1))
	if err != nil || len(data) == 0 || len(data) > renderengine.MaximumResultBytes {
		return renderengine.ResultDocument{}, application.ErrSequencePreviewInvalid
	}
	return renderengine.DecodeResult(data)
}

func validPrivateRenderEvaluation(
	plan domain.RenderPlanPayload,
	evaluation renderengine.ResultEvaluation,
) bool {
	if evaluation.Validate() != nil || plan.Output.CanvasWidth == 0 || plan.Output.CanvasHeight == 0 ||
		plan.Output.VideoFrameCount.Value() == 0 || plan.Output.AudioSampleCount.Value() == 0 {
		return false
	}
	pixels := uint64(plan.Output.CanvasWidth) * uint64(plan.Output.CanvasHeight)
	if pixels%2 != 0 {
		return false
	}
	frameBytes := pixels + pixels/2
	frames := plan.Output.VideoFrameCount.Value()
	samples := plan.Output.AudioSampleCount.Value()
	if frames > math.MaxUint64/frameBytes || samples > math.MaxUint64/4 {
		return false
	}
	return evaluation.Video.ByteSize.Value() == frameBytes*frames &&
		evaluation.Audio.ByteSize.Value() == samples*4
}

func writePrivateRenderManifest(path string, value []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(value)
	if writeErr == nil && written != len(value) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func preparePrivateRenderRoot(root string) (string, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	physical = filepath.Clean(physical)
	info, err := os.Lstat(physical)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !cleanAbsolute(physical) {
		return "", fmt.Errorf("private render root is invalid")
	}
	return physical, nil
}

func inspectSequencePreviewOutput(path string) (application.SequencePreviewArtifactFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return application.SequencePreviewArtifactFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		uint64(info.Size()) > application.MaximumSequencePreviewArtifactSize {
		return application.SequencePreviewArtifactFile{}, application.ErrSequencePreviewInvalid
	}
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() {
		return application.SequencePreviewArtifactFile{}, application.ErrSequencePreviewInvalid
	}
	size, err := domain.NewUInt64(uint64(written))
	if err != nil {
		return application.SequencePreviewArtifactFile{}, err
	}
	return application.SequencePreviewArtifactFile{
		Path: "preview.webm", MimeType: "video/webm", ByteSize: size,
		SHA256: domain.Digest("sha256:" + hex.EncodeToString(digest.Sum(nil))),
	}, nil
}

func validateRendererTarget(value string) error {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return fmt.Errorf("renderer target is invalid")
	}
	_, err := target.New(parts[0], parts[1])
	return err
}

type sequencePreviewWorkspace struct {
	root     string
	mu       sync.Mutex
	released bool
}

func (workspace *sequencePreviewWorkspace) Open(relativePath string) (io.ReadCloser, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released || relativePath != "preview.webm" {
		return nil, fmt.Errorf("sequence preview workspace file is unavailable")
	}
	path := filepath.Join(workspace.root, relativePath)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sequence preview workspace file is invalid")
	}
	return os.Open(path)
}

func (workspace *sequencePreviewWorkspace) Release() error {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released {
		return nil
	}
	workspace.released = true
	return os.RemoveAll(workspace.root)
}

var _ application.SequencePreviewRenderer = (*ExternalSequencePreviewRenderer)(nil)
var _ application.PreparedMediaWorkspace = (*sequencePreviewWorkspace)(nil)
