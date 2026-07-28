// Package draftoverlay applies one uncommitted gesture draft to a pinned render
// plan. It exists so the draft tier and the commit tier share a single operation
// vocabulary: an overlay carries the same normalized payloads the commit tier
// journals, and applying one yields an ordinary render plan the private renderer
// composites with its ordinary semantics. There is no draft-only dialect and no
// second effect model. Like rendercontract it depends only on product/domain, so
// application orchestration cannot enter the renderer's compiled source closure.
package draftoverlay

import (
	"errors"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

var (
	ErrOverlayInvalid = errors.New("draft overlay input is invalid")
	ErrOverlayStale   = errors.New("draft overlay base revisions no longer match the pinned plan")
)

const MaximumOverlayOperations = 256

// ClipPlacementOperation is one candidate set-clip-placement, carrying the base
// Clip revision the gesture started from. The revision is what makes staleness
// decidable without consulting current projections.
type ClipPlacementOperation struct {
	ClipID       domain.ClipID
	ClipRevision domain.Revision
	Placement    domain.RenderPlacement
}

// Overlay is one gesture's bounded candidate operation set. It is renderer-local
// and never durable; nothing here is journalled until the gesture commits.
type Overlay struct {
	SequenceRevision domain.Revision
	Placements       []ClipPlacementOperation
}

func (overlay Overlay) IsEmpty() bool {
	return len(overlay.Placements) == 0
}

// Apply returns the pinned plan with the overlay's candidate operations applied.
//
// An empty overlay returns the plan unchanged, so an empty overlay reproduces the
// committed composition exactly. An overlay whose base revisions no longer match
// the pinned plan is stale as a whole and is never partially applied. The plan is
// treated as immutable: instructions are copied before substitution.
func Apply(
	plan domain.RenderPlanPayload,
	overlay Overlay,
) (domain.RenderPlanPayload, error) {
	if err := validateOverlay(overlay); err != nil {
		return domain.RenderPlanPayload{}, err
	}
	if overlay.IsEmpty() {
		return plan, nil
	}
	if overlay.SequenceRevision != plan.SequenceRevision {
		return domain.RenderPlanPayload{}, ErrOverlayStale
	}
	targets := make(map[domain.ClipID]int, len(overlay.Placements))
	for index, instruction := range plan.Video {
		targets[instruction.ClipID] = index
	}
	for _, operation := range overlay.Placements {
		index, present := targets[operation.ClipID]
		if !present || plan.Video[index].ClipRevision != operation.ClipRevision {
			return domain.RenderPlanPayload{}, ErrOverlayStale
		}
	}
	applied := plan
	applied.Video = make([]domain.RenderVideoInstruction, len(plan.Video))
	copy(applied.Video, plan.Video)
	for _, operation := range overlay.Placements {
		applied.Video[targets[operation.ClipID]].Placement = operation.Placement
	}
	return applied, nil
}

func validateOverlay(overlay Overlay) error {
	if len(overlay.Placements) > MaximumOverlayOperations {
		return ErrOverlayInvalid
	}
	seen := make(map[domain.ClipID]struct{}, len(overlay.Placements))
	for _, operation := range overlay.Placements {
		if operation.ClipID.IsZero() {
			return ErrOverlayInvalid
		}
		if _, duplicate := seen[operation.ClipID]; duplicate {
			return ErrOverlayInvalid
		}
		if rendercontract.ValidateRenderPlacement(operation.Placement) != nil {
			return ErrOverlayInvalid
		}
		seen[operation.ClipID] = struct{}{}
	}
	return nil
}
