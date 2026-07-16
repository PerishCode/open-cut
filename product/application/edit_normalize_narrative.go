package application

import "github.com/PerishCode/open-cut/product/domain"

func (normalizer *editNormalizer) insertNarrativeNode(operation EditOperationInput) error {
	allocation := normalizer.allocations[operation.CreateAs.String()]
	if allocation.Kind != domain.EntityNarrativeNode {
		return ErrEditInvalid
	}
	id, err := domain.ParseNarrativeNodeID(allocation.ID)
	if err != nil || normalizer.narrativeNodeExists(id.String()) ||
		normalizer.markTouched(domain.EntityNarrativeNode, id.String()) != nil {
		return ErrEditInvalid
	}
	parent, err := normalizer.requireNarrativeParent(*operation.ParentID)
	if err != nil {
		return err
	}
	after, err := normalizer.resolveNarrativeNodeReference(operation.After)
	if err != nil || (after != nil && normalizer.narrativeNodeParent(*after) != parent.ID) {
		return ErrEditInvalid
	}
	revision, _ := domain.NewRevision(1)
	var state domain.NarrativeNodeState
	switch operation.Type {
	case domain.EditInsertSection:
		parentID := parent.ID
		value := domain.NarrativeSectionState{
			ID: id, Revision: revision, DocumentID: parent.DocumentID, ParentID: &parentID,
			AfterNodeID: after, Title: *operation.Title, Language: *operation.Language,
		}
		state = domain.NarrativeNodeState{Kind: domain.NarrativeNodeSection, Section: &value}
	case domain.EditInsertAuthoredText:
		value := domain.AuthoredTextState{
			ID: id, Revision: revision, DocumentID: parent.DocumentID, ParentID: parent.ID,
			AfterNodeID: after, Purpose: *operation.AuthoredTextPurpose,
			Language: *operation.Language, Text: *operation.Text,
		}
		state = domain.NarrativeNodeState{Kind: domain.NarrativeNodeAuthoredText, AuthoredText: &value}
	case domain.EditInsertVisualIntent:
		value := domain.VisualIntentState{
			ID: id, Revision: revision, DocumentID: parent.DocumentID, ParentID: parent.ID,
			AfterNodeID: after, Purpose: *operation.VisualIntentPurpose,
			Language: *operation.Language, Description: *operation.Description,
		}
		state = domain.NarrativeNodeState{Kind: domain.NarrativeNodeVisualIntent, VisualIntent: &value}
	case domain.EditInsertNote:
		value := domain.NoteState{
			ID: id, Revision: revision, DocumentID: parent.DocumentID, ParentID: parent.ID,
			AfterNodeID: after, Language: *operation.Language, Text: *operation.Text,
		}
		state = domain.NarrativeNodeState{Kind: domain.NarrativeNodeNote, Note: &value}
	default:
		return ErrEditInvalid
	}
	inverse, err := narrativeNodeWithRevisionAndTombstone(state, mustNext(revision), true)
	if err != nil {
		return err
	}
	normalizer.setNarrativeNode(state)
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &state},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &inverse},
	)
	normalizer.changes = append(normalizer.changes,
		newEntityChange(domain.EntityNarrativeNode, id.String(), revision, false))
	normalizer.narrativeChanged = true
	normalizer.markNarrativeParentChanged(parent.ID)
	normalizer.sectionChildren[parent.ID.String()]++
	return nil
}

func (normalizer *editNormalizer) updateNarrativeNode(operation EditOperationInput) error {
	current, exists := normalizer.narrativeNode(operation.NodeID.String())
	if !exists || narrativeNodeTombstoned(current) ||
		normalizer.markTouched(domain.EntityNarrativeNode, operation.NodeID.String()) != nil ||
		!operationMatchesNarrativeKind(operation.Type, current.Kind) {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityNarrativeNode, operation.NodeID.String(), narrativeNodeRevision(current)); err != nil {
		return err
	}
	delete(normalizer.parentChanges, operation.NodeID.String())
	next := cloneNarrativeNodeState(current)
	nextRevision := mustNext(narrativeNodeRevision(current))
	switch operation.Type {
	case domain.EditUpdateSection:
		next.Section.Revision = nextRevision
		next.Section.Title = *operation.Title
		next.Section.Language = *operation.Language
	case domain.EditUpdateAuthoredText:
		next.AuthoredText.Revision = nextRevision
		next.AuthoredText.Purpose = *operation.AuthoredTextPurpose
		next.AuthoredText.Language = *operation.Language
		next.AuthoredText.Text = *operation.Text
	case domain.EditUpdateVisualIntent:
		next.VisualIntent.Revision = nextRevision
		next.VisualIntent.Purpose = *operation.VisualIntentPurpose
		next.VisualIntent.Language = *operation.Language
		next.VisualIntent.Description = *operation.Description
	case domain.EditUpdateNote:
		next.Note.Revision = nextRevision
		next.Note.Language = *operation.Language
		next.Note.Text = *operation.Text
	default:
		return ErrEditInvalid
	}
	inverse, err := narrativeNodeWithRevisionAndTombstone(current, mustNext(nextRevision), false)
	if err != nil {
		return err
	}
	normalizer.setNarrativeNode(next)
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityNarrativeNode, operation.NodeID.String(), narrativeNodeRevision(current), nextRevision, false,
	))
	normalizer.narrativeChanged = true
	return nil
}

func (normalizer *editNormalizer) moveNarrativeNode(operation EditOperationInput) error {
	current, exists := normalizer.narrativeNode(operation.NodeID.String())
	if !exists || narrativeNodeTombstoned(current) || narrativeNodeParentPointer(current) == nil ||
		normalizer.markTouched(domain.EntityNarrativeNode, operation.NodeID.String()) != nil {
		return ErrEditInvalid
	}
	currentRevision := narrativeNodeRevision(current)
	if err := normalizer.require(domain.EntityNarrativeNode, operation.NodeID.String(), currentRevision); err != nil {
		return err
	}
	delete(normalizer.parentChanges, operation.NodeID.String())
	oldParent, err := normalizer.requireNarrativeParent(*narrativeNodeParentPointer(current))
	if err != nil {
		return err
	}
	newParent, err := normalizer.requireNarrativeParent(*operation.ParentID)
	if err != nil || oldParent.DocumentID != newParent.DocumentID {
		return ErrEditInvalid
	}
	after, err := normalizer.resolveNarrativeNodeReference(operation.After)
	if err != nil || (after != nil && (*after == *operation.NodeID || normalizer.narrativeNodeParent(*after) != newParent.ID)) {
		return ErrEditInvalid
	}
	if current.Kind == domain.NarrativeNodeSection && normalizer.sectionDescendsFrom(newParent.ID, *operation.NodeID) {
		return ErrEditInvalid
	}
	nextRevision := mustNext(currentRevision)
	next, err := narrativeNodeMoved(current, newParent.ID, after, nextRevision)
	if err != nil {
		return err
	}
	inverse, err := narrativeNodeMoved(current, oldParent.ID, narrativeNodeAfter(current), mustNext(nextRevision))
	if err != nil {
		return err
	}
	normalizer.setNarrativeNode(next)
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityNarrativeNode, operation.NodeID.String(), currentRevision, nextRevision, false,
	))
	normalizer.narrativeChanged = true
	normalizer.markNarrativeParentChanged(oldParent.ID)
	normalizer.markNarrativeParentChanged(newParent.ID)
	if oldParent.ID != newParent.ID {
		normalizer.sectionChildren[oldParent.ID.String()]--
		normalizer.sectionChildren[newParent.ID.String()]++
	}
	return nil
}

func (normalizer *editNormalizer) removeNarrativeNode(operation EditOperationInput) error {
	current, exists := normalizer.narrativeNode(operation.NodeID.String())
	parentID := narrativeNodeParentPointer(current)
	if !exists || narrativeNodeTombstoned(current) || parentID == nil ||
		(current.Kind == domain.NarrativeNodeSection && normalizer.sectionChildren[operation.NodeID.String()] != 0) ||
		normalizer.markTouched(domain.EntityNarrativeNode, operation.NodeID.String()) != nil {
		return ErrEditInvalid
	}
	currentRevision := narrativeNodeRevision(current)
	if err := normalizer.require(domain.EntityNarrativeNode, operation.NodeID.String(), currentRevision); err != nil {
		return err
	}
	delete(normalizer.parentChanges, operation.NodeID.String())
	parent, err := normalizer.requireNarrativeParent(*parentID)
	if err != nil {
		return err
	}
	nextRevision := mustNext(currentRevision)
	next, err := narrativeNodeWithRevisionAndTombstone(current, nextRevision, true)
	if err != nil {
		return err
	}
	inverse, err := narrativeNodeWithRevisionAndTombstone(current, mustNext(nextRevision), false)
	if err != nil {
		return err
	}
	normalizer.setNarrativeNode(next)
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityNarrativeNode, operation.NodeID.String(), currentRevision, nextRevision, true,
	))
	normalizer.narrativeChanged = true
	normalizer.markNarrativeParentChanged(parent.ID)
	normalizer.sectionChildren[parent.ID.String()]--
	return nil
}

func (normalizer *editNormalizer) requireNarrativeParent(id domain.NarrativeNodeID) (domain.NarrativeSectionState, error) {
	parent, exists := normalizer.sections[id.String()]
	if !exists || parent.ID.IsZero() || parent.Tombstoned || parent.DocumentID != normalizer.input.State.DocumentID {
		return domain.NarrativeSectionState{}, ErrEditInvalid
	}
	base := normalizer.input.State.Sections[id.String()]
	if base.ID.IsZero() {
		return domain.NarrativeSectionState{}, ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityNarrativeNode, id.String(), base.Revision); err != nil {
		return domain.NarrativeSectionState{}, err
	}
	return parent, nil
}

func (normalizer *editNormalizer) markNarrativeParentChanged(id domain.NarrativeNodeID) {
	if _, directlyTouched := normalizer.touched[entityKey(domain.EntityNarrativeNode, id.String())]; directlyTouched {
		delete(normalizer.parentChanges, id.String())
		return
	}
	base := normalizer.input.State.Sections[id.String()]
	if !base.ID.IsZero() {
		normalizer.parentChanges[id.String()] = base.Revision
	}
}

func (normalizer *editNormalizer) sectionDescendsFrom(candidate, ancestor domain.NarrativeNodeID) bool {
	seen := make(map[string]struct{})
	current := candidate
	for !current.IsZero() {
		if current == ancestor {
			return true
		}
		if _, duplicate := seen[current.String()]; duplicate {
			return true
		}
		seen[current.String()] = struct{}{}
		section, exists := normalizer.sections[current.String()]
		if !exists || section.ParentID == nil {
			return false
		}
		current = *section.ParentID
	}
	return false
}

func (normalizer *editNormalizer) narrativeNode(id string) (domain.NarrativeNodeState, bool) {
	if value, exists := normalizer.sections[id]; exists {
		copyValue := value
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeSection, Section: &copyValue}, true
	}
	if value, exists := normalizer.texts[id]; exists {
		copyValue := value
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeAuthoredText, AuthoredText: &copyValue}, true
	}
	if value, exists := normalizer.sourceExcerpts[id]; exists {
		copyValue := value
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeSourceExcerpt, SourceExcerpt: &copyValue}, true
	}
	if value, exists := normalizer.visualIntents[id]; exists {
		copyValue := value
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeVisualIntent, VisualIntent: &copyValue}, true
	}
	if value, exists := normalizer.notes[id]; exists {
		copyValue := value
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeNote, Note: &copyValue}, true
	}
	return domain.NarrativeNodeState{}, false
}

func (normalizer *editNormalizer) narrativeNodeExists(id string) bool {
	_, exists := normalizer.narrativeNode(id)
	return exists
}

func (normalizer *editNormalizer) setNarrativeNode(state domain.NarrativeNodeState) {
	switch state.Kind {
	case domain.NarrativeNodeSection:
		normalizer.sections[state.Section.ID.String()] = *state.Section
	case domain.NarrativeNodeAuthoredText:
		normalizer.texts[state.AuthoredText.ID.String()] = *state.AuthoredText
	case domain.NarrativeNodeSourceExcerpt:
		normalizer.sourceExcerpts[state.SourceExcerpt.ID.String()] = *state.SourceExcerpt
	case domain.NarrativeNodeVisualIntent:
		normalizer.visualIntents[state.VisualIntent.ID.String()] = *state.VisualIntent
	case domain.NarrativeNodeNote:
		normalizer.notes[state.Note.ID.String()] = *state.Note
	}
}

func (normalizer *editNormalizer) narrativeNodeParent(id domain.NarrativeNodeID) domain.NarrativeNodeID {
	state, exists := normalizer.narrativeNode(id.String())
	if !exists || narrativeNodeParentPointer(state) == nil {
		return domain.NarrativeNodeID{}
	}
	return *narrativeNodeParentPointer(state)
}

func (normalizer *editNormalizer) narrativeNodeRevision(id string) (domain.Revision, bool) {
	state, exists := normalizer.narrativeNode(id)
	if !exists || narrativeNodeTombstoned(state) || state.Kind == domain.NarrativeNodeSection {
		return 0, false
	}
	return narrativeNodeRevision(state), true
}

func (normalizer *editNormalizer) resolveNarrativeNodeReference(reference *EditReference) (*domain.NarrativeNodeID, error) {
	if reference == nil {
		return nil, nil
	}
	id, err := normalizer.resolveNodeReference(*reference)
	state, exists := normalizer.narrativeNode(id.String())
	if err != nil || !exists || narrativeNodeTombstoned(state) {
		return nil, ErrEditInvalid
	}
	return &id, nil
}

func operationMatchesNarrativeKind(operation domain.EditOperationType, kind domain.NarrativeNodeKind) bool {
	return (operation == domain.EditUpdateSection && kind == domain.NarrativeNodeSection) ||
		(operation == domain.EditUpdateAuthoredText && kind == domain.NarrativeNodeAuthoredText) ||
		(operation == domain.EditUpdateVisualIntent && kind == domain.NarrativeNodeVisualIntent) ||
		(operation == domain.EditUpdateNote && kind == domain.NarrativeNodeNote)
}

func narrativeNodeRevision(state domain.NarrativeNodeState) domain.Revision {
	switch state.Kind {
	case domain.NarrativeNodeSection:
		return state.Section.Revision
	case domain.NarrativeNodeAuthoredText:
		return state.AuthoredText.Revision
	case domain.NarrativeNodeSourceExcerpt:
		return state.SourceExcerpt.Revision
	case domain.NarrativeNodeVisualIntent:
		return state.VisualIntent.Revision
	case domain.NarrativeNodeNote:
		return state.Note.Revision
	default:
		return 0
	}
}

func narrativeNodeParentPointer(state domain.NarrativeNodeState) *domain.NarrativeNodeID {
	switch state.Kind {
	case domain.NarrativeNodeSection:
		return state.Section.ParentID
	case domain.NarrativeNodeAuthoredText:
		return &state.AuthoredText.ParentID
	case domain.NarrativeNodeSourceExcerpt:
		return &state.SourceExcerpt.ParentID
	case domain.NarrativeNodeVisualIntent:
		return &state.VisualIntent.ParentID
	case domain.NarrativeNodeNote:
		return &state.Note.ParentID
	default:
		return nil
	}
}

func narrativeNodeAfter(state domain.NarrativeNodeState) *domain.NarrativeNodeID {
	switch state.Kind {
	case domain.NarrativeNodeSection:
		return state.Section.AfterNodeID
	case domain.NarrativeNodeAuthoredText:
		return state.AuthoredText.AfterNodeID
	case domain.NarrativeNodeSourceExcerpt:
		return state.SourceExcerpt.AfterNodeID
	case domain.NarrativeNodeVisualIntent:
		return state.VisualIntent.AfterNodeID
	case domain.NarrativeNodeNote:
		return state.Note.AfterNodeID
	default:
		return nil
	}
}

func narrativeNodeTombstoned(state domain.NarrativeNodeState) bool {
	switch state.Kind {
	case domain.NarrativeNodeSection:
		return state.Section.Tombstoned
	case domain.NarrativeNodeAuthoredText:
		return state.AuthoredText.Tombstoned
	case domain.NarrativeNodeSourceExcerpt:
		return state.SourceExcerpt.Tombstoned
	case domain.NarrativeNodeVisualIntent:
		return state.VisualIntent.Tombstoned
	case domain.NarrativeNodeNote:
		return state.Note.Tombstoned
	default:
		return true
	}
}

func cloneNarrativeNodeState(state domain.NarrativeNodeState) domain.NarrativeNodeState {
	result := state
	if state.Section != nil {
		value := *state.Section
		result.Section = &value
	}
	if state.AuthoredText != nil {
		value := *state.AuthoredText
		result.AuthoredText = &value
	}
	if state.SourceExcerpt != nil {
		value := *state.SourceExcerpt
		value.Evidence.SegmentIDs = append([]domain.TranscriptSegmentID(nil), value.Evidence.SegmentIDs...)
		value.Evidence.CorrectionRevisions = append(
			[]domain.TranscriptCorrectionRevisionRef(nil), value.Evidence.CorrectionRevisions...,
		)
		result.SourceExcerpt = &value
	}
	if state.VisualIntent != nil {
		value := *state.VisualIntent
		result.VisualIntent = &value
	}
	if state.Note != nil {
		value := *state.Note
		result.Note = &value
	}
	return result
}

func narrativeNodeWithRevisionAndTombstone(
	state domain.NarrativeNodeState,
	revision domain.Revision,
	tombstoned bool,
) (domain.NarrativeNodeState, error) {
	result := cloneNarrativeNodeState(state)
	switch result.Kind {
	case domain.NarrativeNodeSection:
		result.Section.Revision, result.Section.Tombstoned = revision, tombstoned
	case domain.NarrativeNodeAuthoredText:
		result.AuthoredText.Revision, result.AuthoredText.Tombstoned = revision, tombstoned
	case domain.NarrativeNodeSourceExcerpt:
		result.SourceExcerpt.Revision, result.SourceExcerpt.Tombstoned = revision, tombstoned
	case domain.NarrativeNodeVisualIntent:
		result.VisualIntent.Revision, result.VisualIntent.Tombstoned = revision, tombstoned
	case domain.NarrativeNodeNote:
		result.Note.Revision, result.Note.Tombstoned = revision, tombstoned
	default:
		return domain.NarrativeNodeState{}, ErrEditInvalid
	}
	return result, nil
}

func narrativeNodeMoved(
	state domain.NarrativeNodeState,
	parent domain.NarrativeNodeID,
	after *domain.NarrativeNodeID,
	revision domain.Revision,
) (domain.NarrativeNodeState, error) {
	result, err := narrativeNodeWithRevisionAndTombstone(state, revision, false)
	if err != nil {
		return domain.NarrativeNodeState{}, err
	}
	switch result.Kind {
	case domain.NarrativeNodeSection:
		result.Section.ParentID, result.Section.AfterNodeID = &parent, after
	case domain.NarrativeNodeAuthoredText:
		result.AuthoredText.ParentID, result.AuthoredText.AfterNodeID = parent, after
	case domain.NarrativeNodeSourceExcerpt:
		result.SourceExcerpt.ParentID, result.SourceExcerpt.AfterNodeID = parent, after
	case domain.NarrativeNodeVisualIntent:
		result.VisualIntent.ParentID, result.VisualIntent.AfterNodeID = parent, after
	case domain.NarrativeNodeNote:
		result.Note.ParentID, result.Note.AfterNodeID = parent, after
	default:
		return domain.NarrativeNodeState{}, ErrEditInvalid
	}
	return result, nil
}
