package application

import (
	"context"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

type BindSequenceExportRenderPlan struct {
	Claim     WorkJobClaim
	Compiled  CompiledRenderPlan
	CreatedAt time.Time
}

type SequenceExportPlanRepository interface {
	LoadBoundSequenceExportRenderPlan(context.Context, WorkJobClaim, time.Time) (PublishedRenderPlan, bool, error)
	LoadSequenceExportRenderSnapshot(context.Context, WorkJobClaim, time.Time) (CompileRenderPlanInput, error)
	BindSequenceExportRenderPlan(context.Context, BindSequenceExportRenderPlan) (PublishedRenderPlan, error)
}

type SequenceExportAttemptPlans struct {
	repository SequenceExportPlanRepository
	clock      Clock
}

func NewSequenceExportAttemptPlans(
	repository SequenceExportPlanRepository,
	clock Clock,
) (*SequenceExportAttemptPlans, error) {
	if repository == nil || clock == nil {
		return nil, fmt.Errorf("sequence export attempt plan dependencies are required")
	}
	return &SequenceExportAttemptPlans{repository: repository, clock: clock}, nil
}

func (plans *SequenceExportAttemptPlans) Bind(
	ctx context.Context,
	claim WorkJobClaim,
) (PublishedRenderPlan, error) {
	if claim.JobID.IsZero() || claim.AttemptID.IsZero() || claim.Kind != domain.WorkJobSequenceExport ||
		claim.SequenceExport == nil || claim.SequenceExport.ProjectID.IsZero() ||
		claim.SequenceExport.SequenceID.IsZero() || claim.SequenceExport.SequenceRevision.Value() == 0 {
		return PublishedRenderPlan{}, ErrSequenceExportInvalid
	}
	now := plans.clock.Now().UTC()
	if now.IsZero() {
		return PublishedRenderPlan{}, ErrSequenceExportInvalid
	}
	bound, exists, err := plans.repository.LoadBoundSequenceExportRenderPlan(ctx, claim, now)
	if err != nil {
		return PublishedRenderPlan{}, err
	}
	if exists {
		return bound, nil
	}
	snapshot, err := plans.repository.LoadSequenceExportRenderSnapshot(ctx, claim, now)
	if err != nil {
		return PublishedRenderPlan{}, err
	}
	compiled, err := CompileSequenceExportPlan(snapshot)
	if err != nil {
		return PublishedRenderPlan{}, err
	}
	return plans.repository.BindSequenceExportRenderPlan(ctx, BindSequenceExportRenderPlan{
		Claim: claim, Compiled: compiled, CreatedAt: now,
	})
}
