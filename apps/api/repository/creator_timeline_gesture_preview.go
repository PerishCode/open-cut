package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const creatorTimelineGestureSchema = "v1"

func (repository *SQLiteProjects) ReadCreatorTimelineGesturePreview(
	ctx context.Context,
	query application.CreatorTimelineGesturePreviewQuery,
) (application.CreatorTimelineGesturePreviewResult, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	defer tx.Rollback()
	operation, err := creatorTimelineBaseOperation(query.Input)
	if err != nil {
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	initialConditions := []domain.EntityPrecondition{{
		Kind: domain.EntityClip, ID: query.Input.ClipID.String(), Revision: query.Input.ClipRevision,
	}}
	if query.Input.TrackID != nil && query.Input.TrackRevision != nil {
		initialConditions = append(initialConditions, domain.EntityPrecondition{
			Kind: domain.EntityTrack, ID: query.Input.TrackID.String(), Revision: *query.Input.TrackRevision,
		})
	}
	requestID, _ := domain.ParseRequestID("ui:timeline-gesture-preview")
	state, err := loadEditNormalizationState(ctx, tx, application.ProposeEditRecord{
		ProjectID: query.ProjectID, SequenceID: query.SequenceID, Actor: query.Actor,
		Input: application.EditProposeInput{
			RequestID: requestID, Intent: "Preview Creator Timeline gesture", BaseProjectRevision: 1,
			Preconditions: initialConditions, Operations: []application.EditOperationInput{operation},
		},
	})
	if err != nil {
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	seed := state.Clips[query.Input.ClipID.String()]
	if seed.ID.IsZero() || seed.Tombstoned || seed.Revision != query.Input.ClipRevision {
		return application.CreatorTimelineGesturePreviewResult{}, application.ErrEditConflict
	}
	mutationMembers, touchedClips, err := creatorTimelineMutationClips(state, seed, query.Input)
	if err != nil {
		if query.Input.Scope == domain.ClipScopeLinked && seed.LinkGroupID == nil {
			return finishCreatorTimelinePreview(ctx, tx, query.ProjectID, creatorTimelineBlocked(
				state, seed, query.Input, application.CreatorTimelineScopeUnavailable,
				[]domain.ClipID{seed.ID}, nil, []application.CreatorTimelineBlockedRecovery{
					application.CreatorTimelineChooseSingle,
				},
			))
		}
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	if creatorTimelineGestureIsNoChange(seed, query.Input) {
		return finishCreatorTimelinePreview(ctx, tx, query.ProjectID, creatorTimelineBlocked(
			state, seed, query.Input, application.CreatorTimelineNoChange, mutationMembers, nil, nil,
		))
	}
	for _, clipID := range touchedClips {
		if err := ensureClipDependencies(ctx, tx, &state, state.Clips[clipID.String()]); err != nil {
			return application.CreatorTimelineGesturePreviewResult{}, err
		}
	}
	createdLocals := make([]domain.LocalID, 0)
	splitOutputs := make(map[string]application.ClipSplitOutputInput)
	if query.Input.Kind == application.CreatorTimelineSplit {
		operation, splitOutputs, createdLocals, err = creatorTimelineSplitOperation(operation, mutationMembers, query.Input)
		if err != nil {
			return application.CreatorTimelineGesturePreviewResult{}, err
		}
	}
	alignmentIDs := creatorTimelineAlignmentIDs(state, touchedClips)
	if len(alignmentIDs) > 511 {
		return finishCreatorTimelinePreview(ctx, tx, query.ProjectID, creatorTimelineBlocked(
			state, seed, query.Input, application.CreatorTimelineClosureLimit,
			touchedClips, alignmentIDs[:min(len(alignmentIDs), 512)],
			[]application.CreatorTimelineBlockedRecovery{application.CreatorTimelineReduceScope},
		))
	}
	for _, alignmentID := range alignmentIDs {
		alignment := state.Alignments[alignmentID.String()]
		for _, target := range alignment.Targets {
			if target.Clip != nil {
				if err := ensureClipState(ctx, tx, &state, target.Clip.ClipID); err != nil {
					return application.CreatorTimelineGesturePreviewResult{}, err
				}
				if err := ensureClipDependencies(ctx, tx, &state, state.Clips[target.Clip.ClipID.String()]); err != nil {
					return application.CreatorTimelineGesturePreviewResult{}, err
				}
			}
		}
	}
	alignmentOperations, alignmentEffects, err := creatorTimelineAlignmentOperations(
		state, seed, mutationMembers, alignmentIDs, splitOutputs, query.Input,
	)
	if err != nil {
		if query.Input.AlignmentHandling == application.CreatorTimelinePreserveAlignment {
			return finishCreatorTimelinePreview(ctx, tx, query.ProjectID, creatorTimelineBlocked(
				state, seed, query.Input, application.CreatorTimelinePreserveUnprovable,
				touchedClips, alignmentIDs, []application.CreatorTimelineBlockedRecovery{
					application.CreatorTimelineMarkStale, application.CreatorTimelineUnbind,
				},
			))
		}
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	operations := append([]application.EditOperationInput{operation}, alignmentOperations...)
	preconditions, err := creatorTimelinePreconditions(state, query.Input, touchedClips, alignmentIDs)
	if err != nil {
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	input := application.EditProposeInput{
		RequestID: requestID, Intent: "Preview Creator Timeline gesture",
		BaseProjectRevision: state.ProjectRevision, Preconditions: preconditions, Operations: operations,
	}
	allocation := creatorTimelineFakeAllocation(operation)
	proposal, err := validateCreatorTimelinePlan(query, state, input, allocation)
	if err != nil {
		if blocked, ok := classifyCreatorTimelineBlocked(state, seed, mutationMembers, alignmentIDs, query.Input); ok {
			return finishCreatorTimelinePreview(ctx, tx, query.ProjectID, blocked)
		}
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	clipEffects, err := creatorTimelineClipEffects(state, proposal, touchedClips, splitOutputs, allocation)
	if err != nil {
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	_, outputDigest, err := domain.CanonicalDigest("open-cut/creator-timeline-gesture", creatorTimelineGestureSchema, struct {
		BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
		Preconditions       []domain.EntityPrecondition      `json:"preconditions"`
		Operations          []application.EditOperationInput `json:"operations"`
	}{state.ProjectRevision, preconditions, operations})
	if err != nil {
		return application.CreatorTimelineGesturePreviewResult{}, err
	}
	ready := application.CreatorTimelineGesturePreview{
		BaseProjectRevision: state.ProjectRevision, Preconditions: preconditions, Operations: operations,
		Kind: query.Input.Kind, Scope: query.Input.Scope, SeedClipID: seed.ID,
		AffectedClipIDs: touchedClips, CreatedClipLocals: createdLocals,
		ClipEffects: clipEffects, AlignmentEffects: alignmentEffects, OutputDigest: outputDigest,
	}
	return finishCreatorTimelinePreview(ctx, tx, query.ProjectID, application.CreatorTimelineGesturePreviewResult{
		Status: application.CreatorTimelinePreviewReady, Ready: &ready,
	})
}

func creatorTimelineBaseOperation(
	input application.CreatorTimelineGesturePreviewInput,
) (application.EditOperationInput, error) {
	scope := input.Scope
	operation := application.EditOperationInput{
		Clip: &application.EditReference{ID: input.ClipID.String()}, Scope: &scope,
	}
	switch input.Kind {
	case application.CreatorTimelineMove:
		operation.Type, operation.TrackID, operation.TimelineStart = domain.EditMoveClip, input.TrackID, input.TimelineStart
	case application.CreatorTimelineTrim:
		operation.Type, operation.SourceRange, operation.TimelineRange =
			domain.EditTrimClip, input.SourceRange, input.TimelineRange
	case application.CreatorTimelineSplit:
		operation.Type, operation.SplitAt = domain.EditSplitClip, input.SplitAt
	case application.CreatorTimelineRemove:
		operation.Type = domain.EditRemoveClip
	default:
		return application.EditOperationInput{}, application.ErrEditInvalid
	}
	return operation, nil
}

func creatorTimelineMutationClips(
	state application.EditNormalizationState,
	seed domain.ClipState,
	input application.CreatorTimelineGesturePreviewInput,
) ([]domain.ClipID, []domain.ClipID, error) {
	members := []domain.ClipID{seed.ID}
	if input.Scope == domain.ClipScopeLinked {
		if seed.LinkGroupID == nil {
			return nil, nil, application.ErrEditInvalid
		}
		members = append([]domain.ClipID(nil), state.LinkGroupClips[seed.LinkGroupID.String()]...)
		if len(members) < 2 || len(members) > 64 {
			return nil, nil, application.ErrEditInvalid
		}
	}
	sort.Slice(members, func(left, right int) bool { return members[left].String() < members[right].String() })
	touched := append([]domain.ClipID(nil), members...)
	if input.Scope == domain.ClipScopeSingle &&
		(input.Kind == application.CreatorTimelineSplit || input.Kind == application.CreatorTimelineRemove) &&
		seed.LinkGroupID != nil {
		groupMembers := state.LinkGroupClips[seed.LinkGroupID.String()]
		if len(groupMembers) == 2 {
			for _, memberID := range groupMembers {
				if memberID != seed.ID {
					touched = append(touched, memberID)
				}
			}
		}
	}
	sort.Slice(touched, func(left, right int) bool { return touched[left].String() < touched[right].String() })
	return members, touched, nil
}

func creatorTimelineSplitOperation(
	operation application.EditOperationInput,
	members []domain.ClipID,
	input application.CreatorTimelineGesturePreviewInput,
) (application.EditOperationInput, map[string]application.ClipSplitOutputInput, []domain.LocalID, error) {
	if input.LocalPrefix == nil {
		return operation, nil, nil, application.ErrEditInvalid
	}
	outputs := make(map[string]application.ClipSplitOutputInput, len(members))
	created := make([]domain.LocalID, 0, len(members)*2)
	for index, memberID := range members {
		left, err := domain.ParseLocalID(fmt.Sprintf("%s_clip_%03d_left", input.LocalPrefix.String(), index+1))
		if err != nil {
			return operation, nil, nil, application.ErrEditInvalid
		}
		right, err := domain.ParseLocalID(fmt.Sprintf("%s_clip_%03d_right", input.LocalPrefix.String(), index+1))
		if err != nil {
			return operation, nil, nil, application.ErrEditInvalid
		}
		output := application.ClipSplitOutputInput{
			Clip: application.EditReference{ID: memberID.String()}, LeftAs: left, RightAs: right,
		}
		operation.SplitOutputs = append(operation.SplitOutputs, output)
		outputs[memberID.String()] = output
		created = append(created, left, right)
	}
	if input.Scope == domain.ClipScopeLinked {
		leftGroup, leftErr := domain.ParseLocalID(input.LocalPrefix.String() + "_group_left")
		rightGroup, rightErr := domain.ParseLocalID(input.LocalPrefix.String() + "_group_right")
		if leftErr != nil || rightErr != nil {
			return operation, nil, nil, application.ErrEditInvalid
		}
		operation.LeftLinkGroupAs, operation.RightLinkGroupAs = &leftGroup, &rightGroup
	}
	return operation, outputs, created, nil
}

func creatorTimelineAlignmentIDs(
	state application.EditNormalizationState,
	clips []domain.ClipID,
) []domain.AlignmentID {
	seen := make(map[string]domain.AlignmentID)
	for _, clipID := range clips {
		for _, alignmentID := range state.ClipAlignments[clipID.String()] {
			seen[alignmentID.String()] = alignmentID
		}
	}
	result := make([]domain.AlignmentID, 0, len(seen))
	for _, alignmentID := range seen {
		result = append(result, alignmentID)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].String() < result[right].String() })
	return result
}

func creatorTimelineAlignmentOperations(
	state application.EditNormalizationState,
	seed domain.ClipState,
	mutationMembers []domain.ClipID,
	alignmentIDs []domain.AlignmentID,
	splitOutputs map[string]application.ClipSplitOutputInput,
	input application.CreatorTimelineGesturePreviewInput,
) ([]application.EditOperationInput, []application.CreatorTimelineAlignmentEffect, error) {
	memberSet := make(map[string]struct{}, len(mutationMembers))
	for _, memberID := range mutationMembers {
		memberSet[memberID.String()] = struct{}{}
	}
	operations := make([]application.EditOperationInput, 0, len(alignmentIDs))
	effects := make([]application.CreatorTimelineAlignmentEffect, 0, len(alignmentIDs))
	for _, alignmentID := range alignmentIDs {
		alignment := state.Alignments[alignmentID.String()]
		operation := application.EditOperationInput{AlignmentID: &alignmentID}
		targetCount := len(alignment.Targets)
		switch input.AlignmentHandling {
		case application.CreatorTimelineStaleAlignment:
			operation.Type = domain.EditMarkAlignmentStale
		case application.CreatorTimelineUnbindAlignment:
			operation.Type = domain.EditUnbindAlignment
		case application.CreatorTimelinePreserveAlignment:
			operation.Type = domain.EditRemapAlignment
			targets, err := creatorTimelinePreservedTargets(
				state, seed, memberSet, alignment, splitOutputs, input,
			)
			if err != nil {
				return nil, nil, err
			}
			operation.AlignmentTargets = targets
			targetCount = len(targets)
		default:
			return nil, nil, application.ErrEditInvalid
		}
		operations = append(operations, operation)
		effects = append(effects, application.CreatorTimelineAlignmentEffect{
			AlignmentID: alignment.ID, Revision: alignment.Revision,
			Handling: input.AlignmentHandling, TargetCount: targetCount,
		})
	}
	return operations, effects, nil
}

func creatorTimelinePreservedTargets(
	state application.EditNormalizationState,
	seed domain.ClipState,
	mutationMembers map[string]struct{},
	alignment domain.AlignmentState,
	splitOutputs map[string]application.ClipSplitOutputInput,
	input application.CreatorTimelineGesturePreviewInput,
) ([]application.AlignmentTargetInput, error) {
	if input.Kind == application.CreatorTimelineRemove {
		return nil, application.ErrEditInvalid
	}
	var trimLeft domain.RationalTime
	if input.Kind == application.CreatorTimelineTrim {
		var err error
		trimLeft, err = input.TimelineRange.Start.Subtract(seed.TimelineRange.Start)
		if err != nil {
			return nil, application.ErrEditInvalid
		}
	}
	result := make([]application.AlignmentTargetInput, 0, len(alignment.Targets)*2)
	for _, target := range alignment.Targets {
		if target.Type != domain.AlignmentTargetClip || target.Clip == nil {
			return nil, application.ErrEditInvalid
		}
		clip := state.Clips[target.Clip.ClipID.String()]
		if clip.ID.IsZero() {
			return nil, application.ErrEditInvalid
		}
		if output, split := splitOutputs[clip.ID.String()]; split {
			fragments, err := creatorTimelineSplitTargets(clip, target.Clip.LocalRange, output, *input.SplitAt)
			if err != nil {
				return nil, err
			}
			result = append(result, fragments...)
			continue
		}
		localRange := target.Clip.LocalRange
		if input.Kind == application.CreatorTimelineTrim {
			if _, mutated := mutationMembers[clip.ID.String()]; mutated {
				start, err := localRange.Start.Subtract(trimLeft)
				if err != nil || start.IsNegative() {
					return nil, application.ErrEditInvalid
				}
				localRange.Start = start
			}
		}
		result = append(result, application.AlignmentTargetInput{
			Type: domain.AlignmentTargetClip, Clip: &application.EditReference{ID: clip.ID.String()},
			LocalRange: &localRange,
		})
	}
	return result, nil
}

func creatorTimelineSplitTargets(
	clip domain.ClipState,
	localRange domain.TimeRange,
	output application.ClipSplitOutputInput,
	splitAt domain.RationalTime,
) ([]application.AlignmentTargetInput, error) {
	offset, err := splitAt.Subtract(clip.TimelineRange.Start)
	if err != nil || !offset.IsPositive() {
		return nil, application.ErrEditInvalid
	}
	end, err := localRange.End()
	if err != nil {
		return nil, application.ErrEditInvalid
	}
	result := make([]application.AlignmentTargetInput, 0, 2)
	startBeforeSplit, _ := localRange.Start.Compare(offset)
	endAfterSplit, _ := end.Compare(offset)
	if startBeforeSplit < 0 {
		leftEnd := end
		if endAfterSplit > 0 {
			leftEnd = offset
		}
		duration, err := leftEnd.Subtract(localRange.Start)
		if err != nil || !duration.IsPositive() {
			return nil, application.ErrEditInvalid
		}
		leftRange, err := domain.NewTimeRange(localRange.Start, duration)
		if err != nil {
			return nil, application.ErrEditInvalid
		}
		leftLocal := output.LeftAs
		result = append(result, application.AlignmentTargetInput{
			Type: domain.AlignmentTargetClip, Clip: &application.EditReference{Local: &leftLocal}, LocalRange: &leftRange,
		})
	}
	if endAfterSplit > 0 {
		rightStart := localRange.Start
		if startBeforeSplit < 0 {
			rightStart = offset
		}
		duration, err := end.Subtract(rightStart)
		if err != nil || !duration.IsPositive() {
			return nil, application.ErrEditInvalid
		}
		localStart, err := rightStart.Subtract(offset)
		if err != nil || localStart.IsNegative() {
			return nil, application.ErrEditInvalid
		}
		rightRange, err := domain.NewTimeRange(localStart, duration)
		if err != nil {
			return nil, application.ErrEditInvalid
		}
		rightLocal := output.RightAs
		result = append(result, application.AlignmentTargetInput{
			Type: domain.AlignmentTargetClip, Clip: &application.EditReference{Local: &rightLocal}, LocalRange: &rightRange,
		})
	}
	if len(result) == 0 {
		return nil, application.ErrEditInvalid
	}
	return result, nil
}

func creatorTimelinePreconditions(
	state application.EditNormalizationState,
	input application.CreatorTimelineGesturePreviewInput,
	touchedClips []domain.ClipID,
	alignmentIDs []domain.AlignmentID,
) ([]domain.EntityPrecondition, error) {
	conditions := map[string]domain.EntityPrecondition{
		preconditionKey(domain.EntitySequence, state.SequenceID.String()): {
			Kind: domain.EntitySequence, ID: state.SequenceID.String(), Revision: state.SequenceRevision,
		},
	}
	addClip := func(clip domain.ClipState) error {
		if clip.ID.IsZero() || clip.Tombstoned {
			return application.ErrEditInvalid
		}
		conditions[preconditionKey(domain.EntityClip, clip.ID.String())] = domain.EntityPrecondition{
			Kind: domain.EntityClip, ID: clip.ID.String(), Revision: clip.Revision,
		}
		track := state.Tracks[clip.TrackID.String()]
		if track.ID.IsZero() {
			return application.ErrEditInvalid
		}
		conditions[preconditionKey(domain.EntityTrack, track.ID.String())] = domain.EntityPrecondition{
			Kind: domain.EntityTrack, ID: track.ID.String(), Revision: track.Revision,
		}
		return nil
	}
	for _, clipID := range touchedClips {
		if err := addClip(state.Clips[clipID.String()]); err != nil {
			return nil, err
		}
	}
	for _, alignmentID := range alignmentIDs {
		alignment := state.Alignments[alignmentID.String()]
		conditions[preconditionKey(domain.EntityAlignment, alignment.ID.String())] = domain.EntityPrecondition{
			Kind: domain.EntityAlignment, ID: alignment.ID.String(), Revision: alignment.Revision,
		}
		if input.AlignmentHandling == application.CreatorTimelinePreserveAlignment {
			for _, target := range alignment.Targets {
				if target.Clip != nil {
					if err := addClip(state.Clips[target.Clip.ClipID.String()]); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	seed := state.Clips[input.ClipID.String()]
	if seed.LinkGroupID != nil &&
		(input.Scope == domain.ClipScopeLinked || input.Kind == application.CreatorTimelineSplit ||
			input.Kind == application.CreatorTimelineRemove) {
		group := state.LinkGroups[seed.LinkGroupID.String()]
		conditions[preconditionKey(domain.EntityLinkGroup, group.ID.String())] = domain.EntityPrecondition{
			Kind: domain.EntityLinkGroup, ID: group.ID.String(), Revision: group.Revision,
		}
	}
	if input.TrackID != nil {
		track := state.Tracks[input.TrackID.String()]
		conditions[preconditionKey(domain.EntityTrack, track.ID.String())] = domain.EntityPrecondition{
			Kind: domain.EntityTrack, ID: track.ID.String(), Revision: track.Revision,
		}
	}
	if len(conditions) > 2048 {
		return nil, application.ErrEditInvalid
	}
	result := make([]domain.EntityPrecondition, 0, len(conditions))
	for _, condition := range conditions {
		result = append(result, condition)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		return result[left].ID < result[right].ID
	})
	return result, nil
}

func creatorTimelineFakeAllocation(operation application.EditOperationInput) []domain.LocalAllocation {
	result := make([]domain.LocalAllocation, 0, len(operation.SplitOutputs)*2+2)
	add := func(local domain.LocalID, kind domain.EditEntityKind) {
		result = append(result, domain.LocalAllocation{
			Local: local, Kind: kind,
			ID: fmt.Sprintf("018f0000-0000-7000-8000-%012x", len(result)+1),
		})
	}
	for _, output := range operation.SplitOutputs {
		add(output.LeftAs, domain.EntityClip)
		add(output.RightAs, domain.EntityClip)
	}
	if operation.LeftLinkGroupAs != nil {
		add(*operation.LeftLinkGroupAs, domain.EntityLinkGroup)
	}
	if operation.RightLinkGroupAs != nil {
		add(*operation.RightLinkGroupAs, domain.EntityLinkGroup)
	}
	return result
}

func validateCreatorTimelinePlan(
	query application.CreatorTimelineGesturePreviewQuery,
	state application.EditNormalizationState,
	input application.EditProposeInput,
	allocation []domain.LocalAllocation,
) (domain.EditProposal, error) {
	proposalID, _ := domain.ParseProposalID("018f0000-0000-7000-8000-000000000100")
	proposal, _, err := application.NormalizeEditProposal(application.NormalizeEditInput{
		ProposalID: proposalID, ProjectID: query.ProjectID, SequenceID: query.SequenceID,
		Actor: query.Actor, Allocation: allocation, Input: input, CreatedAt: time.Unix(0, 0).UTC(), State: state,
	})
	return proposal, err
}
