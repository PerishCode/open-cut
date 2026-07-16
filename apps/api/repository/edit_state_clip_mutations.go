package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func loadClipMutationInput(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	operation application.EditOperationInput,
	mutated map[string]struct{},
) error {
	seedID, err := domain.ParseClipID(operation.Clip.ID)
	if err != nil {
		return application.ErrEditInvalid
	}
	if err := ensureClipState(ctx, tx, state, seedID); err != nil {
		return err
	}
	seed := state.Clips[seedID.String()]
	if err := ensureClipDependencies(ctx, tx, state, seed); err != nil {
		return err
	}
	members := []domain.ClipID{seedID}
	membershipChange := operation.Type == domain.EditSplitClip || operation.Type == domain.EditRemoveClip
	if operation.Scope != nil && (*operation.Scope == domain.ClipScopeLinked || membershipChange) && seed.LinkGroupID != nil {
		if err := loadLinkGroupMembers(ctx, tx, state, *seed.LinkGroupID); err != nil {
			return err
		}
		if *operation.Scope == domain.ClipScopeLinked {
			members = append([]domain.ClipID(nil), state.LinkGroupClips[seed.LinkGroupID.String()]...)
		}
		if membershipChange {
			for _, memberID := range state.LinkGroupClips[seed.LinkGroupID.String()] {
				mutated[memberID.String()] = struct{}{}
			}
		}
	}
	for _, memberID := range members {
		if err := ensureClipState(ctx, tx, state, memberID); err != nil {
			return err
		}
		current := state.Clips[memberID.String()]
		if err := ensureClipDependencies(ctx, tx, state, current); err != nil {
			return err
		}
		mutated[memberID.String()] = struct{}{}
	}
	switch operation.Type {
	case domain.EditMoveClip:
		if err := ensureTrackState(ctx, tx, state, *operation.TrackID); err != nil {
			return err
		}
		delta, err := operation.TimelineStart.Subtract(seed.TimelineRange.Start)
		if err != nil {
			return application.ErrEditInvalid
		}
		for _, memberID := range members {
			current := state.Clips[memberID.String()]
			candidate := current.TimelineRange
			candidate.Start, err = candidate.Start.Add(delta)
			if err != nil {
				return application.ErrEditInvalid
			}
			trackID := current.TrackID
			if current.ID == seed.ID {
				trackID = *operation.TrackID
			}
			if err := loadClipOverlaps(ctx, tx, state, state.SequenceID, trackID, candidate); err != nil {
				return err
			}
		}
	case domain.EditTrimClip:
		left, right, err := trimBoundaryDeltas(seed.TimelineRange, *operation.TimelineRange)
		if err != nil {
			return err
		}
		for _, memberID := range members {
			current := state.Clips[memberID.String()]
			candidate, err := rangeWithStoredBoundaryDeltas(current.TimelineRange, left, right)
			if err != nil {
				return err
			}
			if err := loadClipOverlaps(ctx, tx, state, state.SequenceID, current.TrackID, candidate); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadLinkClipsInput(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	operation application.EditOperationInput,
	mutated map[string]struct{},
) error {
	for _, reference := range operation.Clips {
		id, err := domain.ParseClipID(reference.ID)
		if err != nil {
			return application.ErrEditInvalid
		}
		if err := ensureClipState(ctx, tx, state, id); err != nil {
			return err
		}
		if err := ensureClipDependencies(ctx, tx, state, state.Clips[id.String()]); err != nil {
			return err
		}
		mutated[id.String()] = struct{}{}
	}
	return nil
}

func loadUnlinkClipsInput(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	operation application.EditOperationInput,
	mutated map[string]struct{},
) error {
	id, err := domain.ParseLinkGroupID(operation.LinkGroup.ID)
	if err != nil {
		return application.ErrEditInvalid
	}
	if err := loadLinkGroupMembers(ctx, tx, state, id); err != nil {
		return err
	}
	for _, memberID := range state.LinkGroupClips[id.String()] {
		if err := ensureClipDependencies(ctx, tx, state, state.Clips[memberID.String()]); err != nil {
			return err
		}
		mutated[memberID.String()] = struct{}{}
	}
	return nil
}

func loadAlignmentRemapInput(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	operation application.EditOperationInput,
) error {
	if err := ensureAlignmentState(ctx, tx, state, *operation.AlignmentID); err != nil {
		return err
	}
	current := state.Alignments[operation.AlignmentID.String()]
	for _, target := range current.Targets {
		if target.Clip != nil {
			if err := ensureClipState(ctx, tx, state, target.Clip.ClipID); err != nil {
				return err
			}
		}
		if target.Caption != nil {
			if err := ensureCaptionState(ctx, tx, state, target.Caption.CaptionID); err != nil {
				return err
			}
		}
	}
	for _, target := range operation.AlignmentTargets {
		if target.Clip != nil && target.Clip.ID != "" {
			id, err := domain.ParseClipID(target.Clip.ID)
			if err != nil {
				return application.ErrEditInvalid
			}
			if err := ensureClipState(ctx, tx, state, id); err != nil {
				return err
			}
		}
		if target.Caption != nil && target.Caption.ID != "" {
			id, err := domain.ParseCaptionID(target.Caption.ID)
			if err != nil {
				return application.ErrEditInvalid
			}
			if err := ensureCaptionState(ctx, tx, state, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureClipDependencies(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	clip domain.ClipState,
) error {
	if err := ensureTrackState(ctx, tx, state, clip.TrackID); err != nil {
		return err
	}
	return ensureSourceStreamState(ctx, tx, state, clip.AssetID, clip.SourceStreamID)
}

func trimBoundaryDeltas(current, next domain.TimeRange) (domain.RationalTime, domain.RationalTime, error) {
	left, err := next.Start.Subtract(current.Start)
	if err != nil {
		return domain.RationalTime{}, domain.RationalTime{}, application.ErrEditInvalid
	}
	currentEnd, err := current.End()
	if err != nil {
		return domain.RationalTime{}, domain.RationalTime{}, application.ErrEditInvalid
	}
	nextEnd, err := next.End()
	if err != nil {
		return domain.RationalTime{}, domain.RationalTime{}, application.ErrEditInvalid
	}
	right, err := nextEnd.Subtract(currentEnd)
	if err != nil {
		return domain.RationalTime{}, domain.RationalTime{}, application.ErrEditInvalid
	}
	return left, right, nil
}

func rangeWithStoredBoundaryDeltas(
	current domain.TimeRange,
	left, right domain.RationalTime,
) (domain.TimeRange, error) {
	start, err := current.Start.Add(left)
	if err != nil {
		return domain.TimeRange{}, application.ErrEditInvalid
	}
	end, err := current.End()
	if err != nil {
		return domain.TimeRange{}, application.ErrEditInvalid
	}
	end, err = end.Add(right)
	if err != nil {
		return domain.TimeRange{}, application.ErrEditInvalid
	}
	duration, err := end.Subtract(start)
	if err != nil {
		return domain.TimeRange{}, application.ErrEditInvalid
	}
	result, err := domain.NewTimeRange(start, duration)
	if err != nil {
		return domain.TimeRange{}, application.ErrEditInvalid
	}
	return result, nil
}
