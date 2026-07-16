package application

import "github.com/PerishCode/open-cut/product/domain"

func (normalizer *editNormalizer) addClip(operation EditOperationInput) error {
	allocation := normalizer.allocations[operation.CreateAs.String()]
	if allocation.Kind != domain.EntityClip {
		return ErrEditInvalid
	}
	clipID, err := domain.ParseClipID(allocation.ID)
	if err != nil {
		return ErrEditInvalid
	}
	if _, exists := normalizer.input.State.Clips[clipID.String()]; exists ||
		normalizer.markTouched(domain.EntityClip, clipID.String()) != nil {
		return ErrEditInvalid
	}
	track := normalizer.input.State.Tracks[operation.TrackID.String()]
	if track.ID.IsZero() || track.SequenceID != normalizer.input.SequenceID ||
		(track.Type != domain.TrackVideo && track.Type != domain.TrackAudio) {
		return ErrEditInvalid
	}
	stream := normalizer.input.State.SourceStreams[operation.SourceStreamID.String()]
	if stream.ID.IsZero() || stream.AssetID != *operation.AssetID ||
		(track.Type == domain.TrackVideo && stream.Descriptor.MediaType != domain.MediaVideo) ||
		(track.Type == domain.TrackAudio && stream.Descriptor.MediaType != domain.MediaAudio) {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityTrack, track.ID.String(), track.Revision); err != nil {
		return err
	}
	if err := normalizer.require(domain.EntityAsset, stream.AssetID.String(), stream.AssetRevision); err != nil {
		return err
	}
	equalDuration, err := operation.SourceRange.Duration.Compare(operation.TimelineRange.Duration)
	if err != nil || equalDuration != 0 {
		return ErrEditInvalid
	}
	if !sourceRangeWithin(*operation.SourceRange, stream.Descriptor) {
		return ErrEditInvalid
	}
	linkGroupID, err := normalizer.resolveClipLinkGroup(operation)
	if err != nil {
		return err
	}
	revision, _ := domain.NewRevision(1)
	state := domain.ClipState{
		ID: clipID, Revision: revision, SequenceID: normalizer.input.SequenceID,
		TrackID: track.ID, AssetID: stream.AssetID, SourceStreamID: stream.ID,
		SourceRange: *operation.SourceRange, TimelineRange: *operation.TimelineRange,
		Enabled: *operation.Enabled, LinkGroupID: linkGroupID,
	}
	normalizer.clips[clipID.String()] = state
	if linkGroupID != nil {
		normalizer.linkGroupClips[linkGroupID.String()] = append(
			normalizer.linkGroupClips[linkGroupID.String()], clipID,
		)
	}
	inverse := state
	inverse.Revision = mustNext(revision)
	inverse.Tombstoned = true
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutClip, Clip: &state},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutClip, Clip: &inverse},
	)
	normalizer.changes = append(normalizer.changes, newEntityChange(domain.EntityClip, clipID.String(), revision, false))
	normalizer.sequenceChanged = true
	normalizer.trackChanges[track.ID.String()] = track.Revision
	return nil
}

func (normalizer *editNormalizer) resolveClipLinkGroup(
	operation EditOperationInput,
) (*domain.LinkGroupID, error) {
	if operation.CreateLinkGroupAs != nil {
		allocation := normalizer.allocations[operation.CreateLinkGroupAs.String()]
		if allocation.Kind != domain.EntityLinkGroup {
			return nil, ErrEditInvalid
		}
		id, err := domain.ParseLinkGroupID(allocation.ID)
		if err != nil {
			return nil, ErrEditInvalid
		}
		if _, exists := normalizer.input.State.LinkGroups[id.String()]; exists ||
			normalizer.markTouched(domain.EntityLinkGroup, id.String()) != nil {
			return nil, ErrEditInvalid
		}
		revision, _ := domain.NewRevision(1)
		state := domain.LinkGroupState{ID: id, Revision: revision, SequenceID: normalizer.input.SequenceID}
		normalizer.linkGroups[id.String()] = state
		normalizer.linkGroupClips[id.String()] = nil
		inverse := state
		inverse.Revision = mustNext(revision)
		inverse.Tombstoned = true
		normalizer.appendOperation(
			domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &state},
			domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &inverse},
		)
		normalizer.changes = append(
			normalizer.changes,
			newEntityChange(domain.EntityLinkGroup, id.String(), revision, false),
		)
		return &id, nil
	}
	if operation.LinkGroup == nil {
		return nil, nil
	}
	value, err := normalizer.resolveReference(*operation.LinkGroup, domain.EntityLinkGroup)
	if err != nil {
		return nil, err
	}
	id, err := domain.ParseLinkGroupID(value)
	if err != nil {
		return nil, ErrEditInvalid
	}
	group, exists := normalizer.linkGroups[id.String()]
	if !exists || group.Tombstoned || group.SequenceID != normalizer.input.SequenceID {
		return nil, ErrEditInvalid
	}
	key := entityKey(domain.EntityLinkGroup, id.String())
	if _, touched := normalizer.touched[key]; !touched {
		if operation.LinkGroup.ID == "" {
			return nil, ErrEditInvalid
		}
		if err := normalizer.require(domain.EntityLinkGroup, id.String(), group.Revision); err != nil {
			return nil, err
		}
		normalizer.touched[key] = struct{}{}
		next := group
		next.Revision = mustNext(group.Revision)
		inverse := group
		inverse.Revision = mustNext(next.Revision)
		normalizer.linkGroups[id.String()] = next
		normalizer.appendOperation(
			domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &next},
			domain.NormalizedEditOperation{Type: domain.NormalizedPutLinkGroup, LinkGroup: &inverse},
		)
		normalizer.changes = append(normalizer.changes, existingEntityChange(
			domain.EntityLinkGroup, id.String(), group.Revision, next.Revision, false,
		))
	}
	return &id, nil
}

func (normalizer *editNormalizer) validateClipOverlaps() error {
	for key := range normalizer.touched {
		kind, id := splitEntityKey(key)
		if kind != domain.EntityClip {
			continue
		}
		clip := normalizer.clips[id]
		if clip.Tombstoned {
			continue
		}
		for otherID, other := range normalizer.clips {
			if otherID == id || other.Tombstoned || other.TrackID != clip.TrackID {
				continue
			}
			if rangesOverlap(clip.TimelineRange, other.TimelineRange) {
				return ErrEditInvalid
			}
		}
	}
	return nil
}
