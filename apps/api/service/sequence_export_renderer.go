package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type SequenceExportArtifactRootResolver interface {
	ResolveSequenceExportArtifactRoots(
		context.Context,
		application.WorkJobClaim,
		domain.Digest,
		[]domain.RenderPlanInput,
		time.Time,
	) (map[string]string, error)
}

type ExternalSequenceExportRenderer struct {
	resolver      SequenceExportArtifactRootResolver
	executable    string
	identity      application.RenderExecutorIdentity
	closure       renderengine.ExecutionClosure
	resourceRoots map[string]string
	tempRoot      string
	profile       lifecycle.Profile
	wallTime      time.Duration
}

func NewExternalSequenceExportRenderer(
	resolver SequenceExportArtifactRootResolver,
	executable string,
	identity application.RenderExecutorIdentity,
	closure renderengine.ExecutionClosure,
	resourceRoots map[string]string,
	tempRoot string,
	profile lifecycle.Profile,
) (*ExternalSequenceExportRenderer, error) {
	if resolver == nil || !cleanAbsolute(executable) || !cleanAbsolute(tempRoot) ||
		identity.Version == "" || len(identity.Version) > 1024 || validateRendererTarget(identity.Target) != nil ||
		closure.SHA256 == "" || len(closure.Tools) != 1 ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("sequence export renderer configuration is invalid")
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sequence export renderer executable is unavailable")
	}
	tool, exists := closure.Tools["ffmpeg"]
	if !exists || !cleanAbsolute(tool.Path) || tool.SHA256 == "" {
		return nil, fmt.Errorf("sequence export renderer tool closure is invalid")
	}
	toolInfo, err := os.Lstat(tool.Path)
	if err != nil || !toolInfo.Mode().IsRegular() || toolInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sequence export renderer tool closure is unavailable")
	}
	closure.Tools = map[string]renderengine.ExecutionToolPin{"ffmpeg": tool}
	tempRoot, err = preparePrivateRenderRoot(tempRoot)
	if err != nil {
		return nil, fmt.Errorf("create sequence export attempt root: %w", err)
	}
	resources := make(map[string]string, len(resourceRoots))
	for id, root := range resourceRoots {
		if id == "" || strings.TrimSpace(id) != id || !cleanAbsolute(root) {
			return nil, fmt.Errorf("sequence export renderer resource root is invalid")
		}
		resourceInfo, statErr := os.Lstat(root)
		if statErr != nil || !resourceInfo.IsDir() || resourceInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("sequence export renderer resource root is unavailable")
		}
		resources[id] = root
	}
	return &ExternalSequenceExportRenderer{
		resolver: resolver, executable: executable, identity: identity, closure: closure,
		resourceRoots: resources, tempRoot: tempRoot, profile: profile,
		wallTime: time.Duration(renderengine.MaximumWallTimeSeconds) * time.Second,
	}, nil
}

func (renderer *ExternalSequenceExportRenderer) Identity() application.RenderExecutorIdentity {
	return renderer.identity
}

func (renderer *ExternalSequenceExportRenderer) Render(
	ctx context.Context,
	request application.SequenceExportRenderRequest,
) (application.SequenceExportRenderExecution, error) {
	claim, plan := request.Claim, request.Plan
	if claim.Kind != domain.WorkJobSequenceExport || claim.SequenceExport == nil || request.ObservedAt.IsZero() ||
		claim.AttemptID.IsZero() || claim.JobID.IsZero() ||
		claim.SequenceExport.ProjectID != plan.Plan.Payload.ProjectID ||
		claim.SequenceExport.SequenceID != plan.Plan.Payload.SequenceID ||
		claim.SequenceExport.SequenceRevision != plan.Plan.Payload.SequenceRevision ||
		claim.SequenceExport.Parameters.RendererVersion != renderer.identity.Version ||
		claim.SequenceExport.Parameters.RendererTarget != renderer.identity.Target ||
		application.ValidateSequenceExportRenderPlanPayload(plan.Plan.Payload) != nil {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			"renderer-input-invalid", application.ErrSequenceExportInvalid,
		)
	}
	artifactRoots, err := renderer.resolver.ResolveSequenceExportArtifactRoots(
		ctx, claim, plan.Plan.Digest, plan.Plan.Payload.Inputs, request.ObservedAt.UTC(),
	)
	if err != nil {
		var resourceLimit renderengine.ResourceLimitError
		if errors.As(err, &resourceLimit) {
			return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
				renderengine.ResultCodeResourceLimit, err,
			)
		}
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			"render-input-unavailable", err,
		)
	}
	resourceRoots := make(map[string]string, len(plan.Plan.Payload.FontResources))
	for _, resource := range plan.Plan.Payload.FontResources {
		root, exists := renderer.resourceRoots[resource.ResourceID]
		if !exists {
			return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
				"render-font-unavailable", application.ErrRenderFontRequired,
			)
		}
		resourceRoots[resource.ResourceID] = root
	}
	execution, executionBytes, err := renderengine.CompileExecutionManifest(
		plan, renderer.identity, renderer.closure,
		renderengine.MaterialPaths{ArtifactRoots: artifactRoots, Resources: resourceRoots},
	)
	if err != nil {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			"renderer-input-invalid", err,
		)
	}
	attemptRoot := filepath.Join(renderer.tempRoot, claim.AttemptID.String())
	if !pathWithin(renderer.tempRoot, attemptRoot) {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			"renderer-input-invalid", application.ErrSequenceExportInvalid,
		)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
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
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	executionPath := filepath.Join(attemptRoot, renderengine.ExecutionFilename)
	if err := writePrivateRenderManifest(executionPath, executionBytes); err != nil {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	executionContext, cancel := context.WithTimeout(ctx, renderer.wallTime)
	defer cancel()
	stderr := &boundedBuffer{limit: 64 << 10}
	processErr := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: renderer.executable, Args: []string{"--execution", executionPath}, Directory: attemptRoot,
		Env: executorEnvironment(), Stdin: nil, Stdout: io.Discard, Stderr: stderr,
		Profile: renderer.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	})
	if ctx.Err() != nil {
		return application.SequenceExportRenderExecution{}, ctx.Err()
	}
	result, resultErr := readPrivateRenderResult(filepath.Join(attemptRoot, renderengine.ResultFilename))
	if processErr != nil {
		if executionContext.Err() == context.DeadlineExceeded {
			return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
				"renderer-timeout", executionContext.Err(),
			)
		}
		if !stderr.exceeded && resultErr == nil && result.Status == renderengine.ResultFailed &&
			result.Diagnostic != nil {
			return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
				result.Diagnostic.Code, fmt.Errorf("private renderer rejected %s/%s",
					result.Diagnostic.SubjectKind, result.Diagnostic.SubjectID),
			)
		}
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			"renderer-process-failed", fmt.Errorf("private renderer failed: %s", strings.TrimSpace(stderr.String())),
		)
	}
	if stderr.exceeded || resultErr != nil || result.Status != renderengine.ResultSucceeded || result.Output == nil ||
		result.Evaluation == nil || !validPrivateRenderEvaluation(plan.Plan.Payload, *result.Evaluation) {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			"renderer-output-invalid", fmt.Errorf("private renderer returned no valid success result"),
		)
	}
	if err := os.Remove(executionPath); err != nil {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	if err := os.Remove(filepath.Join(attemptRoot, renderengine.ResultFilename)); err != nil {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			renderengine.ResultCodeStorage, err,
		)
	}
	media, err := inspectSequenceExportOutput(filepath.Join(attemptRoot, "export.webm"))
	if err != nil {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			"renderer-output-invalid", err,
		)
	}
	if result.Output.RelativePath != media.Path || result.Output.ByteSize != media.ByteSize ||
		result.Output.SHA256 != media.SHA256 {
		return application.SequenceExportRenderExecution{}, application.NewSequenceExportExecutionError(
			"renderer-output-invalid", application.ErrSequenceExportInvalid,
		)
	}
	workspace := &sequenceExportWorkspace{root: attemptRoot}
	keepWorkspace = true
	return application.SequenceExportRenderExecution{Media: media, Workspace: workspace}, nil
}

func inspectSequenceExportOutput(path string) (application.SequenceExportArtifactFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return application.SequenceExportArtifactFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		uint64(info.Size()) > application.MaximumSequenceExportArtifactSize {
		return application.SequenceExportArtifactFile{}, application.ErrSequenceExportInvalid
	}
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() {
		return application.SequenceExportArtifactFile{}, application.ErrSequenceExportInvalid
	}
	size, err := domain.NewUInt64(uint64(written))
	if err != nil {
		return application.SequenceExportArtifactFile{}, err
	}
	return application.SequenceExportArtifactFile{
		Path: "export.webm", MimeType: "video/webm", ByteSize: size,
		SHA256: domain.Digest("sha256:" + hex.EncodeToString(digest.Sum(nil))),
	}, nil
}

type sequenceExportWorkspace struct {
	root     string
	mu       sync.Mutex
	released bool
}

func (workspace *sequenceExportWorkspace) Open(relativePath string) (io.ReadCloser, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released || relativePath != "export.webm" {
		return nil, fmt.Errorf("sequence export workspace file is unavailable")
	}
	path := filepath.Join(workspace.root, relativePath)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sequence export workspace file is invalid")
	}
	return os.Open(path)
}

func (workspace *sequenceExportWorkspace) Release() error {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released {
		return nil
	}
	workspace.released = true
	return os.RemoveAll(workspace.root)
}

var _ application.SequenceExportRenderer = (*ExternalSequenceExportRenderer)(nil)
var _ application.PreparedMediaWorkspace = (*sequenceExportWorkspace)(nil)
