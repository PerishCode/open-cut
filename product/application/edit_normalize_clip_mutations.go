package application

import (
	"sort"

	"github.com/PerishCode/open-cut/product/domain"
)

func (normalizer *editNormalizer) moveClip(operation EditOperationInput) error {
	members, seed, err := normalizer.clipMutationMembers(operation, false)
	if err != nil {
		return err
	}
	delta, err := operation.TimelineStart.Subtract(seed.TimelineRange.Start)
	if err != nil {
		return ErrEditInvalid
	}
	for _, memberID := range members {
		current := normalizer.clips[memberID.String()]
		next := current
		next.Revision = mustNext(current.Revision)
		next.TimelineRange.Start, err = current.TimelineRange.Start.Add(delta)
		if err != nil || next.TimelineRange.Start.IsNegative() {
			return ErrEditInvalid
		}
		if current.ID == seed.ID {
			next.TrackID = *operation.TrackID
			track := normalizer.input.State.Tracks[next.TrackID.String()]
			stream := normalizer.input.State.SourceStreams[current.SourceStreamID.String()]
			if track.ID.IsZero() || stream.ID.IsZero() || !trackAcceptsStream(track, stream) {
				return ErrEditInvalid
			}
			if err := normalizer.require(domain.EntityTrack, track.ID.String(), track.Revision); err != nil {
				return err
			}
			normalizer.trackChanges[track.ID.String()] = track.Revision
		}
		if err := normalizer.putExistingClip(current, next, true); err != nil {
			return err
		}
	}
	return nil
}

func (normalizer *editNormalizer) trimClip(operation EditOperationInput) error {
	members, seed, err := normalizer.clipMutationMembers(operation, false)
	if err != nil {
		return err
	}
	left, err := operation.TimelineRange.Start.Subtract(seed.TimelineRange.Start)
	if err != nil {
		return ErrEditInvalid
	}
	seedOldEnd, err := seed.TimelineRange.End()
	if err != nil {
		return ErrEditInvalid
	}
	seedNewEnd, err := operation.TimelineRange.End()
	if err != nil {
		return ErrEditInvalid
	}
	right, err := seedNewEnd.Subtract(seedOldEnd)
	if err != nil || !sameBoundaryDelta(seed.SourceRange, *operation.SourceRange, left, right) {
		return ErrEditInvalid
	}
	for _, memberID := range members {
		current := normalizer.clips[memberID.String()]
		nextSource, err := rangeWithBoundaryDeltas(current.SourceRange, left, right)
		if err != nil {
			return ErrEditInvalid
		}
		nextTimeline, err := rangeWithBoundaryDeltas(current.TimelineRange, left, right)
		if err != nil || nextTimeline.Start.IsNegative() {
			return ErrEditInvalid
		}
		stream := normalizer.input.State.SourceStreams[current.SourceStreamID.String()]
		if stream.ID.IsZero() || !sourceRangeWithin(nextSource, stream.Descriptor) {
			return ErrEditInvalid
		}
		next := current
		next.Revision = mustNext(current.Revision)
		next.SourceRange = nextSource
		next.TimelineRange = nextTimeline
		if err := normalizer.putExistingClip(current, next, true); err != nil {
			return err
		}
	}
	return nil
}

func (normalizer *editNormalizer) splitClip(operation EditOperationInput) error {
	members, seed, err := normalizer.clipMutationMembers(operation, true)
	if err != nil {
		return err
	}
	outputs := make(map[string]ClipSplitOutputInput, len(operation.SplitOutputs))
	for _, output := range operation.SplitOutputs {
		outputs[output.Clip.ID] = output
	}
	if len(outputs) != len(members) {
		return ErrEditInvalid
	}
	for _, memberID := range members {
		if _, exists := outputs[memberID.String()]; !exists {
			return ErrEditInvalid
		}
	}
	var leftGroupID, rightGroupID *domain.LinkGroupID
	if *operation.Scope == domain.ClipScopeLinked {
		left, err := normalizer.createLinkGroup(*operation.LeftLinkGroupAs)
		if err != nil {
			return err
		}
		right, err := normalizer.createLinkGroup(*operation.RightLinkGroupAs)
		if err != nil {
			return err
		}
		leftGroupID, rightGroupID = &left, &right
	}
	for _, memberID := range members {
		current := normalizer.clips[memberID.String()]
		leftState, rightState, err := normalizer.splitClipStates(
			current, *operation.SplitAt, outputs[memberID.String()], leftGroupID, rightGroupID,
		)
		if err != nil {
			return err
		}
		tombstone := current
		tombstone.Revision = mustNext(current.Revision)
		tombstone.Tombstoned = true
		if err := normalizer.putExistingClip(current, tombstone, true); err != nil {
			return err
		}
		if err := normalizer.putNewClip(leftState); err != nil {
			return err
		}
		if err := normalizer.putNewClip(rightState); err != nil {
			return err
		}
	}
	if *operation.Scope == domain.ClipScopeLinked {
		if seed.LinkGroupID == nil {
			return ErrEditInvalid
		}
		if err := normalizer.tombstoneLinkGroup(*seed.LinkGroupID); err != nil {
			return err
		}
	} else if seed.LinkGroupID != nil {
		if err := normalizer.finishSingleMembershipRemoval(*seed.LinkGroupID); err != nil {
			return err
		}
	}
	return nil
}

func (normalizer *editNormalizer) removeClip(operation EditOperationInput) error {
	members, seed, err := normalizer.clipMutationMembers(operation, true)
	if err != nil {
		return err
	}
	for _, memberID := range members {
		current := normalizer.clips[memberID.String()]
		next := current
		next.Revision = mustNext(current.Revision)
		next.Tombstoned = true
		if err := normalizer.putExistingClip(current, next, true); err != nil {
			return err
		}
	}
	if seed.LinkGroupID == nil {
		return nil
	}
	if *operation.Scope == domain.ClipScopeLinked {
		return normalizer.tombstoneLinkGroup(*seed.LinkGroupID)
	}
	return normalizer.finishSingleMembershipRemoval(*seed.LinkGroupID)
}

func (normalizer *editNormalizer) linkClips(operation EditOperationInput) error {
	groupID, err := normalizer.createLinkGroup(*operation.CreateLinkGroupAs)
	if err != nil {
		return err
	}
	members := make([]domain.ClipID, 0, len(operation.Clips))
	for _, reference := range operation.Clips {
		id, err := domain.ParseClipID(reference.ID)
		if err != nil {
			return ErrEditInvalid
		}
		current := normalizer.clips[id.String()]
		if current.ID.IsZero() || current.Tombstoned || current.SequenceID != normalizer.input.SequenceID || current.LinkGroupID != nil {
			return ErrEditInvalid
		}
		if err := normalizer.require(domain.EntityClip, id.String(), current.Revision); err != nil {
			return err
		}
		next := current
		next.Revision = mustNext(current.Revision)
		next.LinkGroupID = &groupID
		if err := normalizer.putExistingClip(current, next, false); err != nil {
			return err
		}
		members = append(members, id)
	}
	normalizer.linkGroupClips[groupID.String()] = members
	return nil
}

func (normalizer *editNormalizer) unlinkClips(operation EditOperationInput) error {
	groupID, err := domain.ParseLinkGroupID(operation.LinkGroup.ID)
	if err != nil {
		return ErrEditInvalid
	}
	group, exists := normalizer.linkGroups[groupID.String()]
	if !exists || group.Tombstoned {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityLinkGroup, groupID.String(), group.Revision); err != nil {
		return err
	}
	for _, memberID := range normalizer.linkGroupClips[groupID.String()] {
		current := normalizer.clips[memberID.String()]
		if current.Tombstoned || current.LinkGroupID == nil || *current.LinkGroupID != groupID {
			return ErrEditInvalid
		}
		if err := normalizer.require(domain.EntityClip, memberID.String(), current.Revision); err != nil {
			return err
		}
		next := current
		next.Revision = mustNext(current.Revision)
		next.LinkGroupID = nil
		if err := normalizer.putExistingClip(current, next, false); err != nil {
			return err
		}
	}
	normalizer.linkGroupClips[groupID.String()] = nil
	return normalizer.tombstoneLinkGroup(groupID)
}

func (normalizer *editNormalizer) clipMutationMembers(
	operation EditOperationInput,
	membershipChange bool,
) ([]domain.ClipID, domain.ClipState, error) {
	seedID, err := domain.ParseClipID(operation.Clip.ID)
	if err != nil {
		return nil, domain.ClipState{}, ErrEditInvalid
	}
	seed := normalizer.clips[seedID.String()]
	if seed.ID.IsZero() || seed.Tombstoned || seed.SequenceID != normalizer.input.SequenceID {
		return nil, domain.ClipState{}, ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityClip, seed.ID.String(), seed.Revision); err != nil {
		return nil, domain.ClipState{}, err
	}
	if *operation.Scope == domain.ClipScopeSingle {
		if membershipChange && seed.LinkGroupID != nil {
			group := normalizer.linkGroups[seed.LinkGroupID.String()]
			if group.ID.IsZero() || group.Tombstoned {
				return nil, domain.ClipState{}, ErrEditInvalid
			}
			if err := normalizer.require(domain.EntityLinkGroup, group.ID.String(), group.Revision); err != nil {
				return nil, domain.ClipState{}, err
			}
		}
		return []domain.ClipID{seed.ID}, seed, nil
	}
	if seed.LinkGroupID == nil {
		return nil, domain.ClipState{}, ErrEditInvalid
	}
	group := normalizer.linkGroups[seed.LinkGroupID.String()]
	members := append([]domain.ClipID(nil), normalizer.linkGroupClips[seed.LinkGroupID.String()]...)
	if group.ID.IsZero() || group.Tombstoned || len(members) < 2 || len(members) > 64 {
		return nil, domain.ClipState{}, ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityLinkGroup, group.ID.String(), group.Revision); err != nil {
		return nil, domain.ClipState{}, err
	}
	found := false
	for _, memberID := range members {
		member := normalizer.clips[memberID.String()]
		if member.ID == seed.ID {
			found = true
		}
		if member.ID.IsZero() || member.Tombstoned || member.LinkGroupID == nil || *member.LinkGroupID != group.ID {
			return nil, domain.ClipState{}, ErrEditInvalid
		}
		if err := normalizer.require(domain.EntityClip, member.ID.String(), member.Revision); err != nil {
			return nil, domain.ClipState{}, err
		}
	}
	if !found {
		return nil, domain.ClipState{}, ErrEditInvalid
	}
	sort.Slice(members, func(left, right int) bool { return members[left].String() < members[right].String() })
	return members, seed, nil
}

func (normalizer *editNormalizer) splitClipStates(
	current domain.ClipState,
	splitAt domain.RationalTime,
	output ClipSplitOutputInput,
	leftGroupID, rightGroupID *domain.LinkGroupID,
) (domain.ClipState, domain.ClipState, error) {
	offset, err := splitAt.Subtract(current.TimelineRange.Start)
	if err != nil || !offset.IsPositive() {
		return domain.ClipState{}, domain.ClipState{}, ErrEditInvalid
	}
	comparison, err := offset.Compare(current.TimelineRange.Duration)
	if err != nil || comparison >= 0 {
		return domain.ClipState{}, domain.ClipState{}, ErrEditInvalid
	}
	rightDuration, err := current.TimelineRange.Duration.Subtract(offset)
	if err != nil || !rightDuration.IsPositive() {
		return domain.ClipState{}, domain.ClipState{}, ErrEditInvalid
	}
	sourceSplit, err := current.SourceRange.Start.Add(offset)
	if err != nil {
		return domain.ClipState{}, domain.ClipState{}, ErrEditInvalid
	}
	leftRange, err := domain.NewTimeRange(current.TimelineRange.Start, offset)
	if err != nil {
		return domain.ClipState{}, domain.ClipState{}, ErrEditInvalid
	}
	rightRange, err := domain.NewTimeRange(splitAt, rightDuration)
	if err != nil {
		return domain.ClipState{}, domain.ClipState{}, ErrEditInvalid
	}
	leftSource, err := domain.NewTimeRange(current.SourceRange.Start, offset)
	if err != nil {
		return domain.ClipState{}, domain.ClipState{}, ErrEditInvalid
	}
	rightSource, err := domain.NewTimeRange(sourceSplit, rightDuration)
	if err != nil {
		return domain.ClipState{}, domain.ClipState{}, ErrEditInvalid
	}
	leftID, err := normalizer.allocatedClipID(output.LeftAs)
	if err != nil {
		return domain.ClipState{}, domain.ClipState{}, err
	}
	rightID, err := normalizer.allocatedClipID(output.RightAs)
	if err != nil {
		return domain.ClipState{}, domain.ClipState{}, err
	}
	revision, _ := domain.NewRevision(1)
	left := current
	left.ID, left.Revision, left.SourceRange, left.TimelineRange = leftID, revision, leftSource, leftRange
	left.LinkGroupID, left.Tombstoned = leftGroupID, false
	right := current
	right.ID, right.Revision, right.SourceRange, right.TimelineRange = rightID, revision, rightSource, rightRange
	right.LinkGroupID, right.Tombstoned = rightGroupID, false
	return left, right, nil
}

func (normalizer *editNormalizer) allocatedClipID(local domain.LocalID) (domain.ClipID, error) {
	allocation := normalizer.allocations[local.String()]
	if allocation.Kind != domain.EntityClip {
		return domain.ClipID{}, ErrEditInvalid
	}
	id, err := domain.ParseClipID(allocation.ID)
	if err != nil {
		return domain.ClipID{}, ErrEditInvalid
	}
	return id, nil
}

func (normalizer *editNormalizer) putExistingClip(current, next domain.ClipState, trackChanged bool) error {
	if normalizer.markTouched(domain.EntityClip, current.ID.String()) != nil || next.Revision != mustNext(current.Revision) {
		return ErrEditInvalid
	}
	inverse := current
	inverse.Revision = mustNext(next.Revision)
	normalizer.clips[current.ID.String()] = next
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutClip, Clip: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutClip, Clip: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityClip, current.ID.String(), current.Revision, next.Revision, next.Tombstoned,
	))
	normalizer.sequenceChanged = true
	if trackChanged {
		for _, trackID := range []domain.TrackID{current.TrackID, next.TrackID} {
			track := normalizer.input.State.Tracks[trackID.String()]
			if track.ID.IsZero() {
				return ErrEditInvalid
			}
			if err := normalizer.require(domain.EntityTrack, track.ID.String(), track.Revision); err != nil {
				return err
			}
			normalizer.trackChanges[track.ID.String()] = track.Revision
		}
	}
	return nil
}

func (normalizer *editNormalizer) putNewClip(state domain.ClipState) error {
	if _, exists := normalizer.clips[state.ID.String()]; exists || normalizer.markTouched(domain.EntityClip, state.ID.String()) != nil {
		return ErrEditInvalid
	}
	normalizer.clips[state.ID.String()] = state
	inverse := state
	inverse.Revision = mustNext(state.Revision)
	inverse.Tombstoned = true
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutClip, Clip: &state},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutClip, Clip: &inverse},
	)
	normalizer.changes = append(normalizer.changes, newEntityChange(domain.EntityClip, state.ID.String(), state.Revision, false))
	track := normalizer.input.State.Tracks[state.TrackID.String()]
	if track.ID.IsZero() {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityTrack, track.ID.String(), track.Revision); err != nil {
		return err
	}
	normalizer.trackChanges[track.ID.String()] = track.Revision
	normalizer.sequenceChanged = true
	return nil
}

func (normalizer *editNormalizer) createLinkGroup(local domain.LocalID) (domain.LinkGroupID, error) {
	allocation := normalizer.allocations[local.String()]
	if allocation.Kind != domain.EntityLinkGroup {
		return domain.LinkGroupID{}, ErrEditInvalid
	}
	id, err := domain.ParseLinkGroupID(allocation.ID)
	if err != nil {
		return domain.LinkGroupID{}, ErrEditInvalid
	}
	if _, exists := normalizer.linkGroups[id.String()]; exists || normalizer.markTouched(domain.EntityLinkGroup, id.String()) != nil {
		return domain.LinkGroupID{}, ErrEditInvalid
	}
	revision, _ := domain.NewRevision(1)
	state := domain.LinkGroupState{ID: id, Revision: revision, SequenceID: normalizer.input.SequenceID}
	normalizer.linkGroups[id.String()] = state
	inverse := state
	inverse.Revision = mustNext(revision)
	inverse.Tombstoned = true
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &state},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &inverse},
	)
	normalizer.changes = append(normalizer.changes, newEntityChange(domain.EntityLinkGroup, id.String(), revision, false))
	normalizer.linkGroupClips[id.String()] = nil
	normalizer.sequenceChanged = true
	return id, nil
}

func (normalizer *editNormalizer) tombstoneLinkGroup(id domain.LinkGroupID) error {
	current := normalizer.linkGroups[id.String()]
	if current.ID.IsZero() || current.Tombstoned || normalizer.markTouched(domain.EntityLinkGroup, id.String()) != nil {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityLinkGroup, id.String(), current.Revision); err != nil {
		return err
	}
	next := current
	next.Revision = mustNext(current.Revision)
	next.Tombstoned = true
	inverse := current
	inverse.Revision = mustNext(next.Revision)
	normalizer.linkGroups[id.String()] = next
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityLinkGroup, id.String(), current.Revision, next.Revision, true,
	))
	normalizer.sequenceChanged = true
	return nil
}

func (normalizer *editNormalizer) finishSingleMembershipRemoval(id domain.LinkGroupID) error {
	members := normalizer.linkGroupClips[id.String()]
	survivors := make([]domain.ClipID, 0, len(members))
	for _, memberID := range members {
		clip := normalizer.clips[memberID.String()]
		if !clip.Tombstoned && clip.LinkGroupID != nil && *clip.LinkGroupID == id {
			survivors = append(survivors, memberID)
		}
	}
	if len(survivors) >= 2 {
		return normalizer.advanceLinkGroup(id, survivors)
	}
	for _, memberID := range survivors {
		current := normalizer.clips[memberID.String()]
		if err := normalizer.require(domain.EntityClip, current.ID.String(), current.Revision); err != nil {
			return err
		}
		next := current
		next.Revision = mustNext(current.Revision)
		next.LinkGroupID = nil
		if err := normalizer.putExistingClip(current, next, false); err != nil {
			return err
		}
	}
	normalizer.linkGroupClips[id.String()] = nil
	return normalizer.tombstoneLinkGroup(id)
}

func (normalizer *editNormalizer) advanceLinkGroup(id domain.LinkGroupID, members []domain.ClipID) error {
	current := normalizer.linkGroups[id.String()]
	if current.ID.IsZero() || current.Tombstoned || normalizer.markTouched(domain.EntityLinkGroup, id.String()) != nil {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityLinkGroup, id.String(), current.Revision); err != nil {
		return err
	}
	next := current
	next.Revision = mustNext(current.Revision)
	inverse := current
	inverse.Revision = mustNext(next.Revision)
	normalizer.linkGroups[id.String()] = next
	normalizer.linkGroupClips[id.String()] = append([]domain.ClipID(nil), members...)
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityLinkGroup, id.String(), current.Revision, next.Revision, false,
	))
	normalizer.sequenceChanged = true
	return nil
}

func (normalizer *editNormalizer) validateLinkGroups() error {
	for key := range normalizer.touched {
		kind, id := splitEntityKey(key)
		if kind != domain.EntityLinkGroup {
			continue
		}
		group := normalizer.linkGroups[id]
		live := 0
		for _, clip := range normalizer.clips {
			if !clip.Tombstoned && clip.LinkGroupID != nil && clip.LinkGroupID.String() == id {
				live++
			}
		}
		if (group.Tombstoned && live != 0) || (!group.Tombstoned && (live < 2 || live > 64)) {
			return ErrEditInvalid
		}
	}
	return nil
}

func trackAcceptsStream(track EditTrackState, stream EditSourceStreamState) bool {
	return (track.Type == domain.TrackVideo && stream.Descriptor.MediaType == domain.MediaVideo) ||
		(track.Type == domain.TrackAudio && stream.Descriptor.MediaType == domain.MediaAudio)
}

func rangeWithBoundaryDeltas(
	current domain.TimeRange,
	left, right domain.RationalTime,
) (domain.TimeRange, error) {
	start, err := current.Start.Add(left)
	if err != nil || start.IsNegative() {
		return domain.TimeRange{}, ErrEditInvalid
	}
	end, err := current.End()
	if err != nil {
		return domain.TimeRange{}, ErrEditInvalid
	}
	end, err = end.Add(right)
	if err != nil {
		return domain.TimeRange{}, ErrEditInvalid
	}
	duration, err := end.Subtract(start)
	if err != nil || !duration.IsPositive() {
		return domain.TimeRange{}, ErrEditInvalid
	}
	result, err := domain.NewTimeRange(start, duration)
	if err != nil {
		return domain.TimeRange{}, ErrEditInvalid
	}
	return result, nil
}

func sameBoundaryDelta(
	current, next domain.TimeRange,
	left, right domain.RationalTime,
) bool {
	currentEnd, err := current.End()
	if err != nil {
		return false
	}
	nextEnd, err := next.End()
	if err != nil {
		return false
	}
	actualLeft, err := next.Start.Subtract(current.Start)
	if err != nil {
		return false
	}
	actualRight, err := nextEnd.Subtract(currentEnd)
	if err != nil {
		return false
	}
	leftComparison, leftErr := actualLeft.Compare(left)
	rightComparison, rightErr := actualRight.Compare(right)
	return leftErr == nil && rightErr == nil && leftComparison == 0 && rightComparison == 0
}
