package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var sequenceExportFailureCode = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

type SequenceExportRenderRequest struct {
	Claim      WorkJobClaim
	Plan       PublishedRenderPlan
	ObservedAt time.Time
}

type SequenceExportRenderer interface {
	Identity() RenderExecutorIdentity
	Render(context.Context, SequenceExportRenderRequest) (SequenceExportRenderExecution, error)
}

type SequenceExportVerificationRequest struct {
	Claim     WorkJobClaim
	Plan      PublishedRenderPlan
	Media     SequenceExportArtifactFile
	Workspace PreparedMediaWorkspace
}

type SequenceExportArtifactVerifier interface {
	Verify(context.Context, SequenceExportVerificationRequest) (domain.RenderedMediaFacts, error)
}

type FailSequenceExport struct {
	Claim    WorkJobClaim
	Code     string
	EventID  domain.ActivityEventID
	FailedAt time.Time
}

type SequenceExportWorkRepository interface {
	SequenceExportPlanRepository
	CompleteSequenceExport(context.Context, CompleteSequenceExport) error
	FailSequenceExport(context.Context, FailSequenceExport) error
}

type SequenceExportExecutionError struct {
	Code  string
	Cause error
}

func (failure SequenceExportExecutionError) Error() string {
	if failure.Cause == nil {
		return failure.Code
	}
	return failure.Code + ": " + failure.Cause.Error()
}

func (failure SequenceExportExecutionError) Unwrap() error { return failure.Cause }

func NewSequenceExportExecutionError(code string, cause error) error {
	if !sequenceExportFailureCode.MatchString(code) {
		return fmt.Errorf("invalid sequence export execution failure")
	}
	return SequenceExportExecutionError{Code: code, Cause: cause}
}

type SequenceExportWorkExecutor struct {
	repository SequenceExportWorkRepository
	plans      *SequenceExportAttemptPlans
	renderer   SequenceExportRenderer
	verifier   SequenceExportArtifactVerifier
	identity   RenderExecutorIdentity
	identities IdentityGenerator
	clock      Clock
}

func NewSequenceExportWorkExecutor(
	repository SequenceExportWorkRepository,
	renderer SequenceExportRenderer,
	verifier SequenceExportArtifactVerifier,
	identities IdentityGenerator,
	clock Clock,
) (*SequenceExportWorkExecutor, error) {
	if repository == nil || renderer == nil || verifier == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("sequence export executor dependencies are invalid")
	}
	identity := renderer.Identity()
	if identity.Version == "" || len(identity.Version) > 1024 || !validPreviewTarget(identity.Target) {
		return nil, fmt.Errorf("sequence export renderer identity is invalid")
	}
	plans, err := NewSequenceExportAttemptPlans(repository, clock)
	if err != nil {
		return nil, err
	}
	return &SequenceExportWorkExecutor{
		repository: repository, plans: plans, renderer: renderer, verifier: verifier,
		identity: identity, identities: identities, clock: clock,
	}, nil
}

func (executor *SequenceExportWorkExecutor) Registration() WorkExecutorRegistration {
	return WorkExecutorRegistration{Kind: domain.WorkJobSequenceExport, Version: executor.identity.Version}
}

func (executor *SequenceExportWorkExecutor) Execute(ctx context.Context, claim WorkJobClaim) error {
	if claim.Kind != domain.WorkJobSequenceExport || claim.SequenceExport == nil ||
		claim.ExecutorVersion != executor.identity.Version ||
		claim.SequenceExport.Parameters.RendererVersion != executor.identity.Version ||
		claim.SequenceExport.Parameters.RendererTarget != executor.identity.Target {
		return ErrWorkLeaseLost
	}
	plan, err := executor.plans.Bind(ctx, claim)
	if err != nil {
		return executor.failKnown(ctx, claim, err)
	}
	execution, renderErr := executor.renderer.Render(ctx, SequenceExportRenderRequest{
		Claim: claim, Plan: plan, ObservedAt: executor.clock.Now().UTC(),
	})
	if execution.Workspace != nil {
		defer execution.Workspace.Release()
	}
	if renderErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return executor.fail(ctx, claim, classifySequenceExportExecutionError(renderErr))
	}
	verifiedFacts, verifyErr := executor.verifier.Verify(ctx, SequenceExportVerificationRequest{
		Claim: claim, Plan: plan, Media: execution.Media, Workspace: execution.Workspace,
	})
	if verifyErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return executor.fail(ctx, claim, SequenceExportExecutionError{
			Code: "renderer-output-invalid", Cause: verifyErr,
		})
	}
	publication, err := executor.materialize(ctx, claim, plan, execution, verifiedFacts)
	if err != nil {
		return executor.fail(ctx, claim, SequenceExportExecutionError{
			Code: "renderer-output-invalid", Cause: err,
		})
	}
	return executor.repository.CompleteSequenceExport(ctx, publication)
}

func (executor *SequenceExportWorkExecutor) materialize(
	ctx context.Context,
	claim WorkJobClaim,
	plan PublishedRenderPlan,
	execution SequenceExportRenderExecution,
	verifiedFacts domain.RenderedMediaFacts,
) (CompleteSequenceExport, error) {
	expectedFacts, err := SequenceExportFactsForPlan(plan.Plan.Payload)
	if err != nil || verifiedFacts != expectedFacts || execution.Workspace == nil ||
		execution.Media.Path != "export.webm" || execution.Media.MimeType != "video/webm" ||
		execution.Media.ByteSize.Value() == 0 ||
		execution.Media.ByteSize.Value() > MaximumSequenceExportArtifactSize {
		return CompleteSequenceExport{}, ErrSequenceExportInvalid
	}
	if _, err := domain.ParseDigest(execution.Media.SHA256.String()); err != nil {
		return CompleteSequenceExport{}, ErrSequenceExportInvalid
	}
	now := executor.clock.Now().UTC()
	artifactID, err := executor.newArtifactID(ctx, now)
	if err != nil {
		return CompleteSequenceExport{}, err
	}
	eventID, err := executor.newActivityEventID(ctx, now)
	if err != nil {
		return CompleteSequenceExport{}, err
	}
	export := claim.SequenceExport
	manifest := SequenceExportArtifactManifest{
		ProducerJobID: claim.JobID, ProjectID: export.ProjectID, SequenceID: export.SequenceID,
		SequenceRevision: export.SequenceRevision, RenderPlanDigest: plan.Plan.Digest,
		RendererVersion: executor.identity.Version, RendererTarget: executor.identity.Target,
		Profile: plan.Plan.Payload.Output.Profile, Facts: expectedFacts, Media: execution.Media,
	}
	if err := manifest.Validate(); err != nil {
		return CompleteSequenceExport{}, err
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-artifact", domain.SequenceExportArtifactSchema, manifest,
	)
	if err != nil || len(canonical) > MaximumSequenceExportManifestSize {
		return CompleteSequenceExport{}, ErrSequenceExportInvalid
	}
	total := uint64(len(canonical)) + execution.Media.ByteSize.Value()
	if total > MaximumSequenceExportArtifactSize {
		return CompleteSequenceExport{}, ErrSequenceExportInvalid
	}
	byteSize, err := domain.NewUInt64(total)
	if err != nil {
		return CompleteSequenceExport{}, err
	}
	return CompleteSequenceExport{
		Claim: claim, ArtifactID: artifactID, Plan: plan, Manifest: manifest,
		ManifestCanonical: canonical, ContentDigest: digest, ByteSize: byteSize,
		Workspace: execution.Workspace, EventID: eventID, CompletedAt: now,
	}, nil
}

func (executor *SequenceExportWorkExecutor) failKnown(
	ctx context.Context,
	claim WorkJobClaim,
	cause error,
) error {
	if ctx.Err() != nil || errors.Is(cause, ErrWorkLeaseLost) {
		return cause
	}
	code := ""
	switch {
	case errors.Is(cause, ErrRenderSequenceConflict):
		code = "sequence-revision-conflict"
	case errors.Is(cause, ErrRenderInputRequired):
		code = "render-input-unavailable"
	case errors.Is(cause, ErrRenderFontRequired):
		code = "render-font-unavailable"
	case errors.Is(cause, ErrRenderPlanInvalid), errors.Is(cause, ErrSequenceExportInvalid):
		code = "render-plan-invalid"
	default:
		return cause
	}
	return executor.fail(ctx, claim, SequenceExportExecutionError{Code: code, Cause: cause})
}

func (executor *SequenceExportWorkExecutor) fail(
	ctx context.Context,
	claim WorkJobClaim,
	failure SequenceExportExecutionError,
) error {
	now := executor.clock.Now().UTC()
	eventID, err := executor.newActivityEventID(ctx, now)
	if err != nil {
		return err
	}
	return executor.repository.FailSequenceExport(ctx, FailSequenceExport{
		Claim: claim, Code: failure.Code, EventID: eventID, FailedAt: now,
	})
}

func (executor *SequenceExportWorkExecutor) newArtifactID(
	ctx context.Context,
	at time.Time,
) (domain.ArtifactID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.ArtifactID{}, err
	}
	return domain.ParseArtifactID(value)
}

func (executor *SequenceExportWorkExecutor) newActivityEventID(
	ctx context.Context,
	at time.Time,
) (domain.ActivityEventID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(value)
}

func classifySequenceExportExecutionError(err error) SequenceExportExecutionError {
	var failure SequenceExportExecutionError
	if errors.As(err, &failure) && sequenceExportFailureCode.MatchString(failure.Code) {
		return failure
	}
	return SequenceExportExecutionError{Code: "renderer-failed", Cause: err}
}
