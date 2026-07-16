package repository

import (
	"context"
	"database/sql"
	"sort"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func finishCreatorTimelinePreview(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	result application.CreatorTimelineGesturePreviewResult,
) (application.CreatorTimelineGesturePreviewResult, error) {
	cursor, err := loadActivityHead(ctx, tx, "project", projectID.String())
	if err != nil {
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	switch result.Status {
	case application.CreatorTimelinePreviewReady:
		if result.Ready == nil || result.Blocked != nil {
			return application.CreatorTimelineGesturePreviewResult{}, application.ErrEditInvalid
		}
		result.Ready.ActivityCursor = cursor
	case application.CreatorTimelinePreviewBlocked:
		if result.Blocked == nil || result.Ready != nil {
			return application.CreatorTimelineGesturePreviewResult{}, application.ErrEditInvalid
		}
		result.Blocked.ActivityCursor = cursor
	default:
		return application.CreatorTimelineGesturePreviewResult{}, application.ErrEditInvalid
	}
	if err := tx.Commit(); err != nil {
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	return result, nil
}

func creatorTimelineBlocked(
	state application.EditNormalizationState,
	seed domain.ClipState,
	input application.CreatorTimelineGesturePreviewInput,
	reason application.CreatorTimelineBlockedReason,
	clips []domain.ClipID,
	alignments []domain.AlignmentID,
	recoveries []application.CreatorTimelineBlockedRecovery,
) application.CreatorTimelineGesturePreviewResult {
	clipIDs := append([]domain.ClipID(nil), clips...)
	alignmentIDs := append([]domain.AlignmentID(nil), alignments...)
	recoveryValues := append([]application.CreatorTimelineBlockedRecovery(nil), recoveries...)
	sort.Slice(clipIDs, func(left, right int) bool { return clipIDs[left].String() < clipIDs[right].String() })
	sort.Slice(alignmentIDs, func(left, right int) bool {
		return alignmentIDs[left].String() < alignmentIDs[right].String()
	})
	blocked := application.CreatorTimelineGestureBlocked{
		BaseProjectRevision: state.ProjectRevision,
		Kind:                input.Kind, Scope: input.Scope, SeedClipID: seed.ID, Reason: reason,
		SubjectClipIDs: clipIDs, SubjectAlignmentIDs: alignmentIDs, Recoveries: recoveryValues,
	}
	return application.CreatorTimelineGesturePreviewResult{
		Status: application.CreatorTimelinePreviewBlocked, Blocked: &blocked,
	}
}

func creatorTimelineGestureIsNoChange(
	seed domain.ClipState,
	input application.CreatorTimelineGesturePreviewInput,
) bool {
	switch input.Kind {
	case application.CreatorTimelineMove:
		return input.TrackID != nil && *input.TrackID == seed.TrackID &&
			timesEqual(*input.TimelineStart, seed.TimelineRange.Start)
	case application.CreatorTimelineTrim:
		return rangesEqual(*input.SourceRange, seed.SourceRange) &&
			rangesEqual(*input.TimelineRange, seed.TimelineRange)
	default:
		return false
	}
}

func classifyCreatorTimelineBlocked(
	state application.EditNormalizationState,
	seed domain.ClipState,
	members []domain.ClipID,
	alignments []domain.AlignmentID,
	input application.CreatorTimelineGesturePreviewInput,
) (application.CreatorTimelineGesturePreviewResult, bool) {
	candidates, reason := creatorTimelineCandidates(state, seed, members, input)
	if reason != "" {
		recoveries := []application.CreatorTimelineBlockedRecovery{application.CreatorTimelineChangeTarget}
		if reason == application.CreatorTimelineTrackIncompatible {
			recoveries = []application.CreatorTimelineBlockedRecovery{application.CreatorTimelineChooseTrack}
		}
		return creatorTimelineBlocked(state, seed, input, reason, members, nil, recoveries), true
	}
	collisions, overflow := creatorTimelineCollisions(state, members, candidates)
	if overflow {
		return creatorTimelineBlocked(
			state, seed, input, application.CreatorTimelineClosureLimit, members, nil,
			[]application.CreatorTimelineBlockedRecovery{application.CreatorTimelineReduceScope},
		), true
	}
	if len(collisions) > 0 {
		return creatorTimelineBlocked(
			state, seed, input, application.CreatorTimelineTrackCollision, collisions, nil,
			[]application.CreatorTimelineBlockedRecovery{application.CreatorTimelineChangeTarget},
		), true
	}
	if input.AlignmentHandling == application.CreatorTimelinePreserveAlignment && len(alignments) > 0 {
		return creatorTimelineBlocked(
			state, seed, input, application.CreatorTimelinePreserveUnprovable, members, alignments,
			[]application.CreatorTimelineBlockedRecovery{
				application.CreatorTimelineMarkStale, application.CreatorTimelineUnbind,
			},
		), true
	}
	return application.CreatorTimelineGesturePreviewResult{}, false
}

type creatorTimelineCandidate struct {
	sourceClipID domain.ClipID
	trackID      domain.TrackID
	rangeValue   domain.TimeRange
}

func creatorTimelineCandidates(
	state application.EditNormalizationState,
	seed domain.ClipState,
	members []domain.ClipID,
	input application.CreatorTimelineGesturePreviewInput,
) ([]creatorTimelineCandidate, application.CreatorTimelineBlockedReason) {
	switch input.Kind {
	case application.CreatorTimelineMove:
		track := state.Tracks[input.TrackID.String()]
		stream := state.SourceStreams[seed.SourceStreamID.String()]
		if track.ID.IsZero() || track.SequenceID != state.SequenceID || stream.ID.IsZero() ||
			!timelineTrackAcceptsStream(track.Type, stream.Descriptor.MediaType) {
			return nil, application.CreatorTimelineTrackIncompatible
		}
		delta, err := input.TimelineStart.Subtract(seed.TimelineRange.Start)
		if err != nil {
			return nil, application.CreatorTimelineRangeInvalid
		}
		result := make([]creatorTimelineCandidate, 0, len(members))
		for _, memberID := range members {
			member := state.Clips[memberID.String()]
			next := member.TimelineRange
			next.Start, err = next.Start.Add(delta)
			if err != nil || next.Start.IsNegative() {
				return nil, application.CreatorTimelineRangeInvalid
			}
			trackID := member.TrackID
			if member.ID == seed.ID {
				trackID = track.ID
			}
			result = append(result, creatorTimelineCandidate{member.ID, trackID, next})
		}
		return result, ""
	case application.CreatorTimelineTrim:
		left, right, ok := timelineBoundaryDeltas(seed.TimelineRange, *input.TimelineRange)
		if !ok || !sameTimelineBoundaryDeltas(seed.SourceRange, *input.SourceRange, left, right) {
			return nil, application.CreatorTimelineRangeInvalid
		}
		result := make([]creatorTimelineCandidate, 0, len(members))
		for _, memberID := range members {
			member := state.Clips[memberID.String()]
			timelineRange, timelineOK := timelineRangeWithDeltas(member.TimelineRange, left, right)
			sourceRange, sourceOK := timelineRangeWithDeltas(member.SourceRange, left, right)
			stream := state.SourceStreams[member.SourceStreamID.String()]
			if !timelineOK || timelineRange.Start.IsNegative() || !sourceOK || stream.ID.IsZero() ||
				!timelineSourceRangeWithin(sourceRange, stream.Descriptor) {
				return nil, application.CreatorTimelineRangeInvalid
			}
			result = append(result, creatorTimelineCandidate{member.ID, member.TrackID, timelineRange})
		}
		return result, ""
	case application.CreatorTimelineSplit:
		result := make([]creatorTimelineCandidate, 0, len(members)*2)
		for _, memberID := range members {
			member := state.Clips[memberID.String()]
			offset, err := input.SplitAt.Subtract(member.TimelineRange.Start)
			if err != nil || !offset.IsPositive() {
				return nil, application.CreatorTimelineRangeInvalid
			}
			comparison, err := offset.Compare(member.TimelineRange.Duration)
			if err != nil || comparison >= 0 {
				return nil, application.CreatorTimelineRangeInvalid
			}
			rightDuration, err := member.TimelineRange.Duration.Subtract(offset)
			if err != nil || !rightDuration.IsPositive() {
				return nil, application.CreatorTimelineRangeInvalid
			}
			left, leftErr := domain.NewTimeRange(member.TimelineRange.Start, offset)
			right, rightErr := domain.NewTimeRange(*input.SplitAt, rightDuration)
			if leftErr != nil || rightErr != nil {
				return nil, application.CreatorTimelineRangeInvalid
			}
			result = append(result,
				creatorTimelineCandidate{member.ID, member.TrackID, left},
				creatorTimelineCandidate{member.ID, member.TrackID, right},
			)
		}
		return result, ""
	case application.CreatorTimelineRemove:
		return nil, ""
	default:
		return nil, application.CreatorTimelineRangeInvalid
	}
}

func creatorTimelineCollisions(
	state application.EditNormalizationState,
	members []domain.ClipID,
	candidates []creatorTimelineCandidate,
) ([]domain.ClipID, bool) {
	mutated := make(map[string]struct{}, len(members))
	for _, id := range members {
		mutated[id.String()] = struct{}{}
	}
	conflicts := make(map[string]domain.ClipID)
	for _, candidate := range candidates {
		for id, current := range state.Clips {
			if _, replaced := mutated[id]; replaced || current.Tombstoned || current.TrackID != candidate.trackID {
				continue
			}
			if timelineRangesOverlap(candidate.rangeValue, current.TimelineRange) {
				conflicts[current.ID.String()] = current.ID
				conflicts[candidate.sourceClipID.String()] = candidate.sourceClipID
			}
		}
	}
	for left := 0; left < len(candidates); left++ {
		for right := left + 1; right < len(candidates); right++ {
			if candidates[left].sourceClipID == candidates[right].sourceClipID ||
				candidates[left].trackID != candidates[right].trackID {
				continue
			}
			if timelineRangesOverlap(candidates[left].rangeValue, candidates[right].rangeValue) {
				conflicts[candidates[left].sourceClipID.String()] = candidates[left].sourceClipID
				conflicts[candidates[right].sourceClipID.String()] = candidates[right].sourceClipID
			}
		}
	}
	if len(conflicts) > 64 {
		return nil, true
	}
	result := make([]domain.ClipID, 0, len(conflicts))
	for _, id := range conflicts {
		result = append(result, id)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].String() < result[right].String() })
	return result, false
}

func creatorTimelineClipEffects(
	state application.EditNormalizationState,
	proposal domain.EditProposal,
	touched []domain.ClipID,
	splitOutputs map[string]application.ClipSplitOutputInput,
	allocation []domain.LocalAllocation,
) ([]application.CreatorTimelineClipEffect, error) {
	normalized := make(map[string]domain.ClipState)
	for _, operation := range proposal.Operations {
		if operation.Type == domain.NormalizedPutClip && operation.Clip != nil {
			normalized[operation.Clip.ID.String()] = *operation.Clip
		}
	}
	allocated := make(map[string]string, len(allocation))
	for _, item := range allocation {
		allocated[item.Local.String()] = item.ID
	}
	result := make([]application.CreatorTimelineClipEffect, 0, len(touched))
	for _, clipID := range touched {
		before := state.Clips[clipID.String()]
		if before.ID.IsZero() || before.Tombstoned {
			return nil, application.ErrEditInvalid
		}
		effect := application.CreatorTimelineClipEffect{
			ClipID: before.ID, Before: creatorTimelinePlacement(before),
		}
		if output, split := splitOutputs[clipID.String()]; split {
			left, leftOK := normalized[allocated[output.LeftAs.String()]]
			right, rightOK := normalized[allocated[output.RightAs.String()]]
			original, originalOK := normalized[clipID.String()]
			if !leftOK || !rightOK || !originalOK || !original.Tombstoned || left.Tombstoned || right.Tombstoned {
				return nil, application.ErrEditInvalid
			}
			leftPlacement, rightPlacement := creatorTimelinePlacement(left), creatorTimelinePlacement(right)
			effect.Outcome, effect.Left, effect.Right = application.CreatorTimelineClipSplit, &leftPlacement, &rightPlacement
			result = append(result, effect)
			continue
		}
		after, ok := normalized[clipID.String()]
		if !ok {
			return nil, application.ErrEditInvalid
		}
		if after.Tombstoned {
			effect.Outcome = application.CreatorTimelineClipRemoved
		} else {
			placement := creatorTimelinePlacement(after)
			effect.Outcome, effect.After = application.CreatorTimelineClipUpdated, &placement
		}
		result = append(result, effect)
	}
	return result, nil
}

func creatorTimelinePlacement(clip domain.ClipState) application.CreatorTimelineClipPlacement {
	return application.CreatorTimelineClipPlacement{
		Revision: clip.Revision, TrackID: clip.TrackID,
		SourceRange: clip.SourceRange, TimelineRange: clip.TimelineRange, Linked: clip.LinkGroupID != nil,
	}
}

func timelineTrackAcceptsStream(track domain.TrackType, media domain.MediaType) bool {
	return (track == domain.TrackVideo && media == domain.MediaVideo) ||
		(track == domain.TrackAudio && media == domain.MediaAudio)
}

func timelineBoundaryDeltas(
	current domain.TimeRange,
	next domain.TimeRange,
) (domain.RationalTime, domain.RationalTime, bool) {
	left, err := next.Start.Subtract(current.Start)
	if err != nil {
		return domain.RationalTime{}, domain.RationalTime{}, false
	}
	currentEnd, err := current.End()
	if err != nil {
		return domain.RationalTime{}, domain.RationalTime{}, false
	}
	nextEnd, err := next.End()
	if err != nil {
		return domain.RationalTime{}, domain.RationalTime{}, false
	}
	right, err := nextEnd.Subtract(currentEnd)
	return left, right, err == nil
}

func sameTimelineBoundaryDeltas(
	current domain.TimeRange,
	next domain.TimeRange,
	left domain.RationalTime,
	right domain.RationalTime,
) bool {
	sourceLeft, sourceRight, ok := timelineBoundaryDeltas(current, next)
	return ok && timesEqual(sourceLeft, left) && timesEqual(sourceRight, right)
}

func timelineRangeWithDeltas(
	current domain.TimeRange,
	left domain.RationalTime,
	right domain.RationalTime,
) (domain.TimeRange, bool) {
	start, err := current.Start.Add(left)
	if err != nil {
		return domain.TimeRange{}, false
	}
	end, err := current.End()
	if err != nil {
		return domain.TimeRange{}, false
	}
	end, err = end.Add(right)
	if err != nil {
		return domain.TimeRange{}, false
	}
	duration, err := end.Subtract(start)
	if err != nil || !duration.IsPositive() {
		return domain.TimeRange{}, false
	}
	result, err := domain.NewTimeRange(start, duration)
	return result, err == nil
}

func timelineSourceRangeWithin(source domain.TimeRange, descriptor domain.SourceStreamDescriptor) bool {
	coverageStart, _ := domain.NewRationalTime(0, 1)
	if descriptor.StartTime != nil {
		coverageStart = *descriptor.StartTime
	}
	startComparison, err := source.Start.Compare(coverageStart)
	if err != nil || startComparison < 0 {
		return false
	}
	if descriptor.Duration == nil {
		return true
	}
	coverageEnd, err := coverageStart.Add(*descriptor.Duration)
	if err != nil {
		return false
	}
	sourceEnd, err := source.End()
	if err != nil {
		return false
	}
	endComparison, err := sourceEnd.Compare(coverageEnd)
	return err == nil && endComparison <= 0
}

func timelineRangesOverlap(left domain.TimeRange, right domain.TimeRange) bool {
	leftEnd, leftErr := left.End()
	rightEnd, rightErr := right.End()
	if leftErr != nil || rightErr != nil {
		return true
	}
	leftComparison, leftErr := left.Start.Compare(rightEnd)
	rightComparison, rightErr := right.Start.Compare(leftEnd)
	return leftErr != nil || rightErr != nil || (leftComparison < 0 && rightComparison < 0)
}

func rangesEqual(left domain.TimeRange, right domain.TimeRange) bool {
	return timesEqual(left.Start, right.Start) && timesEqual(left.Duration, right.Duration)
}

func timesEqual(left domain.RationalTime, right domain.RationalTime) bool {
	comparison, err := left.Compare(right)
	return err == nil && comparison == 0
}
