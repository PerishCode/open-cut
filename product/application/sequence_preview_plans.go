package application

import (
	"context"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

type BindSequencePreviewRenderPlan struct {
	Claim     WorkJobClaim
	Compiled  CompiledRenderPlan
	CreatedAt time.Time
}

type SequencePreviewPlanRepository interface {
	LoadBoundSequencePreviewRenderPlan(
		context.Context,
		WorkJobClaim,
		time.Time,
	) (PublishedRenderPlan, bool, error)
	LoadSequencePreviewRenderSnapshot(
		context.Context,
		WorkJobClaim,
		time.Time,
	) (CompileRenderPlanInput, error)
	BindSequencePreviewRenderPlan(
		context.Context,
		BindSequencePreviewRenderPlan,
	) (PublishedRenderPlan, error)
}

type SequencePreviewAttemptPlans struct {
	repository SequencePreviewPlanRepository
	clock      Clock
}

func NewSequencePreviewAttemptPlans(
	repository SequencePreviewPlanRepository,
	clock Clock,
) (*SequencePreviewAttemptPlans, error) {
	if repository == nil || clock == nil {
		return nil, fmt.Errorf("sequence preview attempt plan dependencies are required")
	}
	return &SequencePreviewAttemptPlans{repository: repository, clock: clock}, nil
}

func (plans *SequencePreviewAttemptPlans) Bind(
	ctx context.Context,
	claim WorkJobClaim,
) (PublishedRenderPlan, error) {
	if claim.JobID.IsZero() || claim.AttemptID.IsZero() || claim.Kind != domain.WorkJobSequencePreview ||
		claim.SequencePreview == nil || claim.Media != nil ||
		claim.SequencePreview.ProjectID.IsZero() || claim.SequencePreview.SequenceID.IsZero() ||
		claim.SequencePreview.SequenceRevision.Value() == 0 {
		return PublishedRenderPlan{}, ErrSequencePreviewInvalid
	}
	now := plans.clock.Now().UTC()
	if now.IsZero() {
		return PublishedRenderPlan{}, ErrSequencePreviewInvalid
	}
	bound, exists, err := plans.repository.LoadBoundSequencePreviewRenderPlan(ctx, claim, now)
	if err != nil {
		return PublishedRenderPlan{}, err
	}
	if exists {
		return bound, nil
	}
	snapshot, err := plans.repository.LoadSequencePreviewRenderSnapshot(ctx, claim, now)
	if err != nil {
		return PublishedRenderPlan{}, err
	}
	compiled, err := CompileSequencePreviewPlan(snapshot)
	if err != nil {
		return PublishedRenderPlan{}, err
	}
	return plans.repository.BindSequencePreviewRenderPlan(ctx, BindSequencePreviewRenderPlan{
		Claim: claim, Compiled: compiled, CreatedAt: now,
	})
}
