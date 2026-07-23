package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

var sequencePreviewFailureCode = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

type RenderExecutorIdentity = rendercontract.ExecutorIdentity

type SequencePreviewRendererIdentity = RenderExecutorIdentity

type SequencePreviewRenderRequest struct {
	Claim      WorkJobClaim
	Plan       PublishedRenderPlan
	ObservedAt time.Time
}

type SequencePreviewRenderer interface {
	Identity() SequencePreviewRendererIdentity
	Render(context.Context, SequencePreviewRenderRequest) (SequencePreviewRenderExecution, error)
}

type SequencePreviewVerificationRequest struct {
	Claim     WorkJobClaim
	Plan      PublishedRenderPlan
	Media     SequencePreviewArtifactFile
	Workspace PreparedMediaWorkspace
}

type SequencePreviewArtifactVerifier interface {
	Verify(context.Context, SequencePreviewVerificationRequest) (domain.SequencePreviewMediaFacts, error)
}

type FailSequencePreview struct {
	Claim    WorkJobClaim
	Code     string
	Detail   string
	EventID  domain.ActivityEventID
	FailedAt time.Time
}

type SequencePreviewWorkRepository interface {
	SequencePreviewPlanRepository
	CompleteSequencePreview(context.Context, CompleteSequencePreview) error
	FailSequencePreview(context.Context, FailSequencePreview) error
}

type SequencePreviewExecutionError struct {
	Code  string
	Cause error
}

func (failure SequencePreviewExecutionError) Error() string {
	if failure.Cause == nil {
		return failure.Code
	}
	return failure.Code + ": " + failure.Cause.Error()
}

func (failure SequencePreviewExecutionError) Unwrap() error { return failure.Cause }

func NewSequencePreviewExecutionError(code string, cause error) error {
	if !sequencePreviewFailureCode.MatchString(code) {
		return fmt.Errorf("invalid sequence preview execution failure")
	}
	return SequencePreviewExecutionError{Code: code, Cause: cause}
}

type SequencePreviewWorkExecutor struct {
	repository SequencePreviewWorkRepository
	plans      *SequencePreviewAttemptPlans
	renderer   SequencePreviewRenderer
	verifier   SequencePreviewArtifactVerifier
	identity   SequencePreviewRendererIdentity
	identities IdentityGenerator
	clock      Clock
}

func NewSequencePreviewWorkExecutor(
	repository SequencePreviewWorkRepository,
	renderer SequencePreviewRenderer,
	verifier SequencePreviewArtifactVerifier,
	identities IdentityGenerator,
	clock Clock,
) (*SequencePreviewWorkExecutor, error) {
	if repository == nil || renderer == nil || verifier == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("sequence preview executor dependencies are invalid")
	}
	identity := renderer.Identity()
	if identity.Version == "" || len(identity.Version) > 1024 || !validPreviewTarget(identity.Target) {
		return nil, fmt.Errorf("sequence preview renderer identity is invalid")
	}
	plans, err := NewSequencePreviewAttemptPlans(repository, clock)
	if err != nil {
		return nil, err
	}
	return &SequencePreviewWorkExecutor{
		repository: repository, plans: plans, renderer: renderer, verifier: verifier, identity: identity,
		identities: identities, clock: clock,
	}, nil
}

func (executor *SequencePreviewWorkExecutor) Registration() WorkExecutorRegistration {
	return WorkExecutorRegistration{Kind: domain.WorkJobSequencePreview, Version: executor.identity.Version}
}

func (executor *SequencePreviewWorkExecutor) Execute(ctx context.Context, claim WorkJobClaim) error {
	if claim.Kind != domain.WorkJobSequencePreview || claim.SequencePreview == nil || claim.Media != nil ||
		claim.ExecutorVersion != executor.identity.Version ||
		claim.SequencePreview.Parameters.RendererVersion != executor.identity.Version ||
		claim.SequencePreview.Parameters.RendererTarget != executor.identity.Target {
		return ErrWorkLeaseLost
	}
	plan, err := executor.plans.Bind(ctx, claim)
	if err != nil {
		return executor.failKnown(ctx, claim, err)
	}
	execution, renderErr := executor.renderer.Render(ctx, SequencePreviewRenderRequest{
		Claim: claim, Plan: plan, ObservedAt: executor.clock.Now().UTC(),
	})
	if execution.Workspace != nil {
		defer execution.Workspace.Release()
	}
	if renderErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return executor.fail(ctx, claim, classifySequencePreviewExecutionError(renderErr))
	}
	verifiedFacts, verifyErr := executor.verifier.Verify(ctx, SequencePreviewVerificationRequest{
		Claim: claim, Plan: plan, Media: execution.Media, Workspace: execution.Workspace,
	})
	if verifyErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return executor.fail(ctx, claim, SequencePreviewExecutionError{
			Code: "renderer-output-invalid", Cause: verifyErr,
		})
	}
	publication, err := executor.materialize(ctx, claim, plan, execution, verifiedFacts)
	if err != nil {
		return executor.fail(ctx, claim, SequencePreviewExecutionError{
			Code: "renderer-output-invalid", Cause: err,
		})
	}
	if err := executor.repository.CompleteSequencePreview(ctx, publication); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrSequencePreviewInvalid) {
			return executor.fail(ctx, claim, SequencePreviewExecutionError{
				Code: "renderer-output-invalid", Cause: err,
			})
		}
		return err
	}
	return nil
}

func (executor *SequencePreviewWorkExecutor) materialize(
	ctx context.Context,
	claim WorkJobClaim,
	plan PublishedRenderPlan,
	execution SequencePreviewRenderExecution,
	verifiedFacts domain.SequencePreviewMediaFacts,
) (CompleteSequencePreview, error) {
	expectedFacts, err := SequencePreviewFactsForPlan(plan.Plan.Payload)
	if err != nil || verifiedFacts != expectedFacts || execution.Workspace == nil ||
		execution.Media.Path != "preview.webm" || execution.Media.MimeType != "video/webm" ||
		execution.Media.ByteSize.Value() == 0 ||
		execution.Media.ByteSize.Value() > MaximumSequencePreviewArtifactSize {
		return CompleteSequencePreview{}, ErrSequencePreviewInvalid
	}
	if _, err := domain.ParseDigest(execution.Media.SHA256.String()); err != nil {
		return CompleteSequencePreview{}, ErrSequencePreviewInvalid
	}
	now := executor.clock.Now().UTC()
	artifactID, err := executor.newArtifactID(ctx, now)
	if err != nil {
		return CompleteSequencePreview{}, err
	}
	eventID, err := executor.newActivityEventID(ctx, now)
	if err != nil {
		return CompleteSequencePreview{}, err
	}
	preview := claim.SequencePreview
	manifest := SequencePreviewArtifactManifest{
		ProjectID: preview.ProjectID, SequenceID: preview.SequenceID,
		SequenceRevision: preview.SequenceRevision, RenderPlanDigest: plan.Plan.Digest,
		RendererVersion: executor.identity.Version, RendererTarget: executor.identity.Target,
		Profile: plan.Plan.Payload.Output.Profile, Facts: expectedFacts, Media: execution.Media,
	}
	if err := manifest.Validate(); err != nil {
		return CompleteSequencePreview{}, err
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-preview-artifact", domain.SequencePreviewArtifactSchema, manifest,
	)
	if err != nil || len(canonical) > MaximumSequencePreviewManifestSize {
		return CompleteSequencePreview{}, ErrSequencePreviewInvalid
	}
	total := uint64(len(canonical)) + execution.Media.ByteSize.Value()
	if total > MaximumSequencePreviewArtifactSize {
		return CompleteSequencePreview{}, ErrSequencePreviewInvalid
	}
	byteSize, err := domain.NewUInt64(total)
	if err != nil {
		return CompleteSequencePreview{}, err
	}
	return CompleteSequencePreview{
		Claim: claim, ArtifactID: artifactID, Plan: plan, Manifest: manifest,
		ManifestCanonical: canonical, ContentDigest: digest, ByteSize: byteSize,
		Workspace: execution.Workspace, EventID: eventID, CompletedAt: now,
	}, nil
}

func (executor *SequencePreviewWorkExecutor) failKnown(
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
	case errors.Is(cause, ErrRenderPlanInvalid), errors.Is(cause, ErrSequencePreviewInvalid):
		code = "render-plan-invalid"
	default:
		return cause
	}
	return executor.fail(ctx, claim, SequencePreviewExecutionError{Code: code, Cause: cause})
}

func (executor *SequencePreviewWorkExecutor) fail(
	ctx context.Context,
	claim WorkJobClaim,
	failure SequencePreviewExecutionError,
) error {
	now := executor.clock.Now().UTC()
	eventID, err := executor.newActivityEventID(ctx, now)
	if err != nil {
		return err
	}
	detail := ""
	if failure.Cause != nil {
		detail = BoundedDiagnosticDetail(failure.Cause.Error())
	}
	if err := executor.repository.FailSequencePreview(ctx, FailSequencePreview{
		Claim: claim, Code: failure.Code, Detail: detail, EventID: eventID, FailedAt: now,
	}); err != nil {
		return fmt.Errorf("persist sequence preview failure %s: %w", failure.Code, err)
	}
	return nil
}

func (executor *SequencePreviewWorkExecutor) newArtifactID(
	ctx context.Context,
	at time.Time,
) (domain.ArtifactID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.ArtifactID{}, err
	}
	return domain.ParseArtifactID(value)
}

func (executor *SequencePreviewWorkExecutor) newActivityEventID(
	ctx context.Context,
	at time.Time,
) (domain.ActivityEventID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(value)
}

func classifySequencePreviewExecutionError(err error) SequencePreviewExecutionError {
	var failure SequencePreviewExecutionError
	if errors.As(err, &failure) && sequencePreviewFailureCode.MatchString(failure.Code) {
		return failure
	}
	return SequencePreviewExecutionError{Code: "renderer-failed", Cause: err}
}

func equalSequencePreviewManifestCanonical(
	manifest SequencePreviewArtifactManifest,
	canonical []byte,
	digest domain.Digest,
) bool {
	want, wantDigest, err := domain.CanonicalDigest(
		"open-cut/sequence-preview-artifact", domain.SequencePreviewArtifactSchema, manifest,
	)
	return err == nil && bytes.Equal(want, canonical) && wantDigest == digest
}
