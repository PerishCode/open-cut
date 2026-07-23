package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequencePreviewExecutorTerminalizesInvalidCompletionPublication(t *testing.T) {
	compiled, err := CompileSequencePreviewPlan(renderPlanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	version := SequencePreviewRendererV1 + "@publication-fixture"
	media := []byte("deterministic-preview")
	size, _ := domain.NewUInt64(uint64(len(media)))
	workspace := &completionFailureWorkspace{media: media}
	repository := &completionFailurePreviewRepository{
		plan: PublishedRenderPlan{Plan: compiled.Plan, CreatedAt: now, Replayed: true},
	}
	executor, err := NewSequencePreviewWorkExecutor(
		repository,
		completionFailurePreviewRenderer{
			version: version,
			target:  "mac-arm64",
			execution: SequencePreviewRenderExecution{
				Media: SequencePreviewArtifactFile{
					Path:     "preview.webm",
					MimeType: "video/webm",
					ByteSize: size,
					SHA256:   completionFailureDigest(media),
				},
				Workspace: workspace,
			},
		},
		completionFailurePreviewVerifier{},
		UUIDv7IdentityGenerator{},
		ClockFunc(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim := WorkJobClaim{
		JobID:     mustRenderID(t, "00000000-0000-7000-8000-000000000101", domain.ParseWorkJobID),
		AttemptID: mustRenderID(t, "00000000-0000-7000-8000-000000000102", domain.ParseJobAttemptID),
		Kind:      domain.WorkJobSequencePreview, ExecutorVersion: version,
		SequencePreview: &SequencePreviewJobClaim{
			ProjectID:        compiled.Plan.Payload.ProjectID,
			SequenceID:       compiled.Plan.Payload.SequenceID,
			SequenceRevision: compiled.Plan.Payload.SequenceRevision,
			Parameters: SequencePreviewJobParameters{
				RendererVersion: version,
				RendererTarget:  "mac-arm64",
				OutputProfile:   domain.SequencePreviewProfileV1,
			},
		},
	}
	if err := executor.Execute(context.Background(), claim); err != nil {
		t.Fatalf("invalid completion publication escaped its WorkJob: %v", err)
	}
	if repository.completions != 1 || repository.failures != 1 ||
		repository.failure.Code != "renderer-output-invalid" ||
		!strings.Contains(repository.failure.Detail, ErrSequencePreviewInvalid.Error()) ||
		!workspace.released {
		t.Fatalf(
			"completions=%d failures=%d failure=%+v released=%v",
			repository.completions, repository.failures, repository.failure, workspace.released,
		)
	}
}

type completionFailurePreviewRepository struct {
	plan        PublishedRenderPlan
	completions int
	failures    int
	failure     FailSequencePreview
}

func (repository *completionFailurePreviewRepository) LoadBoundSequencePreviewRenderPlan(
	context.Context,
	WorkJobClaim,
	time.Time,
) (PublishedRenderPlan, bool, error) {
	return repository.plan, true, nil
}

func (*completionFailurePreviewRepository) LoadSequencePreviewRenderSnapshot(
	context.Context,
	WorkJobClaim,
	time.Time,
) (CompileRenderPlanInput, error) {
	return CompileRenderPlanInput{}, ErrSequencePreviewInvalid
}

func (*completionFailurePreviewRepository) BindSequencePreviewRenderPlan(
	context.Context,
	BindSequencePreviewRenderPlan,
) (PublishedRenderPlan, error) {
	return PublishedRenderPlan{}, ErrSequencePreviewInvalid
}

func (repository *completionFailurePreviewRepository) CompleteSequencePreview(
	context.Context,
	CompleteSequencePreview,
) error {
	repository.completions++
	return ErrSequencePreviewInvalid
}

func (repository *completionFailurePreviewRepository) FailSequencePreview(
	_ context.Context,
	input FailSequencePreview,
) error {
	repository.failures++
	repository.failure = input
	return nil
}

type completionFailurePreviewRenderer struct {
	version   string
	target    string
	execution SequencePreviewRenderExecution
}

func (renderer completionFailurePreviewRenderer) Identity() SequencePreviewRendererIdentity {
	return SequencePreviewRendererIdentity{Version: renderer.version, Target: renderer.target}
}

func (renderer completionFailurePreviewRenderer) Render(
	context.Context,
	SequencePreviewRenderRequest,
) (SequencePreviewRenderExecution, error) {
	return renderer.execution, nil
}

type completionFailurePreviewVerifier struct{}

func (completionFailurePreviewVerifier) Verify(
	_ context.Context,
	request SequencePreviewVerificationRequest,
) (domain.SequencePreviewMediaFacts, error) {
	return SequencePreviewFactsForPlan(request.Plan.Plan.Payload)
}

type completionFailureWorkspace struct {
	media    []byte
	released bool
}

func (workspace *completionFailureWorkspace) Open(relativePath string) (io.ReadCloser, error) {
	if relativePath != "preview.webm" {
		return nil, ErrSequencePreviewInvalid
	}
	return io.NopCloser(bytes.NewReader(workspace.media)), nil
}

func (workspace *completionFailureWorkspace) Release() error {
	workspace.released = true
	return nil
}

func completionFailureDigest(value []byte) domain.Digest {
	sum := sha256.Sum256(value)
	return domain.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
