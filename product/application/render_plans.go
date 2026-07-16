package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrRenderSequenceNotFound = errors.New("render sequence not found")
	ErrRenderSequenceConflict = errors.New("render sequence revision conflict")
)

type PublishedRenderPlan struct {
	Plan      domain.RenderPlan
	CreatedAt time.Time
	Replayed  bool
}

type RenderPlanPublication struct {
	Compiled  CompiledRenderPlan
	CreatedAt time.Time
}

func DecodeCanonicalRenderPlan(data []byte) (domain.RenderPlanPayload, domain.Digest, error) {
	var envelope struct {
		Domain  string                   `json:"domain"`
		Payload domain.RenderPlanPayload `json:"payload"`
		Schema  string                   `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/render-plan" || envelope.Schema != domain.RenderPlanSchema ||
		ValidateRenderPlanPayload(envelope.Payload) != nil {
		return domain.RenderPlanPayload{}, "", ErrRenderPlanInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/render-plan", domain.RenderPlanSchema, envelope.Payload,
	)
	if err != nil || !bytes.Equal(canonical, data) {
		return domain.RenderPlanPayload{}, "", ErrRenderPlanInvalid
	}
	return envelope.Payload, digest, nil
}

type RenderPlanRepository interface {
	LoadSequenceRenderSnapshot(
		context.Context,
		domain.ProjectID,
		domain.SequenceID,
		domain.Revision,
	) (CompileRenderPlanInput, error)
	PublishRenderPlan(context.Context, RenderPlanPublication) (PublishedRenderPlan, error)
}

type RenderPlans struct {
	repository RenderPlanRepository
	clock      Clock
}

func NewRenderPlans(repository RenderPlanRepository, clock Clock) (*RenderPlans, error) {
	if repository == nil || clock == nil {
		return nil, fmt.Errorf("render plan dependencies are required")
	}
	return &RenderPlans{repository: repository, clock: clock}, nil
}

func (plans *RenderPlans) CompileSequencePreview(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	expectedSequenceRevision domain.Revision,
) (PublishedRenderPlan, error) {
	if projectID.IsZero() || sequenceID.IsZero() || expectedSequenceRevision.Value() == 0 {
		return PublishedRenderPlan{}, ErrRenderPlanInvalid
	}
	snapshot, err := plans.repository.LoadSequenceRenderSnapshot(
		ctx, projectID, sequenceID, expectedSequenceRevision,
	)
	if err != nil {
		return PublishedRenderPlan{}, err
	}
	compiled, err := CompileSequencePreviewPlan(snapshot)
	if err != nil {
		return PublishedRenderPlan{}, err
	}
	return plans.repository.PublishRenderPlan(ctx, RenderPlanPublication{
		Compiled: compiled, CreatedAt: plans.clock.Now().UTC(),
	})
}
