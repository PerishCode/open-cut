package application

import (
	"fmt"
	"strings"

	"github.com/PerishCode/open-cut/product/domain"
)

func validateNormalizationInput(input NormalizeEditInput) error {
	if input.ProposalID.IsZero() || input.ProjectID.IsZero() || input.SequenceID.IsZero() ||
		input.Actor.Validate() != nil || input.CreatedAt.IsZero() ||
		input.State.ProjectID != input.ProjectID || input.State.SequenceID != input.SequenceID ||
		input.State.ProjectRevision.Value() < 1 || input.State.DocumentRevision.Value() < 1 ||
		input.State.SequenceRevision.Value() < 1 {
		return ErrEditInvalid
	}
	switch input.Actor.Kind {
	case domain.ActorAgent:
		if input.RunID.IsZero() || input.TurnID.IsZero() {
			return ErrEditInvalid
		}
	case domain.ActorCreator:
		if !input.RunID.IsZero() || !input.TurnID.IsZero() {
			return ErrEditInvalid
		}
	default:
		return ErrEditInvalid
	}
	if err := validateEditProposeInput(input.Input); err != nil {
		return err
	}
	return validateAllocationMatchesOperations(input.Allocation, input.Input.Operations)
}

func (normalizer *editNormalizer) appendOperation(
	operation domain.NormalizedEditOperation,
	inverse domain.NormalizedEditOperation,
) {
	normalizer.operations = append(normalizer.operations, operation)
	normalizer.inverse = append(normalizer.inverse, inverse)
}

func (normalizer *editNormalizer) markTouched(kind domain.EditEntityKind, id string) error {
	key := entityKey(kind, id)
	if _, duplicate := normalizer.touched[key]; duplicate {
		return ErrEditInvalid
	}
	normalizer.touched[key] = struct{}{}
	return nil
}

func (normalizer *editNormalizer) require(
	kind domain.EditEntityKind,
	id string,
	revision domain.Revision,
) error {
	provided, exists := normalizer.conditions[entityKey(kind, id)]
	if !exists {
		return editValidationError("missing %s precondition for %s", kind, id)
	}
	if provided != revision {
		return fmt.Errorf("%w: %s %s expected %s, observed %s", ErrEditConflict, kind, id, provided, revision)
	}
	return nil
}

func (normalizer *editNormalizer) resolveNarrativeReference(
	reference *EditReference,
) (*domain.NarrativeNodeID, error) {
	return normalizer.resolveNarrativeNodeReference(reference)
}

func (normalizer *editNormalizer) resolveNodeReference(reference EditReference) (domain.NarrativeNodeID, error) {
	value, err := normalizer.resolveReference(reference, domain.EntityNarrativeNode)
	if err != nil {
		return domain.NarrativeNodeID{}, err
	}
	return domain.ParseNarrativeNodeID(value)
}

func (normalizer *editNormalizer) resolveCaptionReference(reference EditReference) (domain.CaptionID, error) {
	value, err := normalizer.resolveReference(reference, domain.EntityCaption)
	if err != nil {
		return domain.CaptionID{}, err
	}
	return domain.ParseCaptionID(value)
}

func (normalizer *editNormalizer) resolveClipReference(reference EditReference) (domain.ClipID, error) {
	value, err := normalizer.resolveReference(reference, domain.EntityClip)
	if err != nil {
		return domain.ClipID{}, err
	}
	return domain.ParseClipID(value)
}

func (normalizer *editNormalizer) resolveReference(
	reference EditReference,
	kind domain.EditEntityKind,
) (string, error) {
	if reference.ID != "" {
		if err := validateEntityID(kind, reference.ID); err != nil {
			return "", err
		}
		return reference.ID, nil
	}
	if reference.Local == nil {
		return "", ErrEditInvalid
	}
	allocation, exists := normalizer.allocations[reference.Local.String()]
	if !exists || allocation.Kind != kind {
		return "", ErrEditInvalid
	}
	return allocation.ID, nil
}

func rangeWithin(local domain.TimeRange, duration domain.RationalTime) bool {
	if local.Start.Validate() != nil || local.Start.IsNegative() ||
		local.Duration.Validate() != nil || !local.Duration.IsPositive() || duration.Validate() != nil {
		return false
	}
	end, err := local.End()
	if err != nil {
		return false
	}
	comparison, err := end.Compare(duration)
	return err == nil && comparison <= 0
}

func sourceRangeWithin(source domain.TimeRange, descriptor domain.SourceStreamDescriptor) bool {
	if source.Start.Validate() != nil || source.Duration.Validate() != nil || !source.Duration.IsPositive() {
		return false
	}
	sourceEnd, err := source.End()
	if err != nil {
		return false
	}
	coverageStart, err := domain.NewRationalTime(0, 1)
	if err != nil {
		return false
	}
	if descriptor.StartTime != nil {
		if descriptor.StartTime.Validate() != nil {
			return false
		}
		coverageStart = *descriptor.StartTime
	}
	startsWithin, err := source.Start.Compare(coverageStart)
	if err != nil || startsWithin < 0 {
		return false
	}
	if descriptor.Duration == nil {
		return true
	}
	if descriptor.Duration.Validate() != nil || descriptor.Duration.IsNegative() {
		return false
	}
	coverageEnd, err := coverageStart.Add(*descriptor.Duration)
	if err != nil {
		return false
	}
	endsWithin, err := sourceEnd.Compare(coverageEnd)
	return err == nil && endsWithin <= 0
}

func rangesOverlap(left, right domain.TimeRange) bool {
	leftEnd, leftErr := left.End()
	rightEnd, rightErr := right.End()
	if leftErr != nil || rightErr != nil {
		return true
	}
	leftBeforeRightEnd, err := left.Start.Compare(rightEnd)
	if err != nil {
		return true
	}
	rightBeforeLeftEnd, err := right.Start.Compare(leftEnd)
	return err != nil || (leftBeforeRightEnd < 0 && rightBeforeLeftEnd < 0)
}

func mustNext(revision domain.Revision) domain.Revision {
	next, err := revision.Next()
	if err != nil {
		panic("validated revision overflow")
	}
	return next
}

func newEntityChange(
	kind domain.EditEntityKind,
	id string,
	after domain.Revision,
	tombstoned bool,
) domain.EntityRevisionChange {
	return domain.EntityRevisionChange{Kind: kind, ID: id, After: after, Tombstoned: tombstoned}
}

func existingEntityChange(
	kind domain.EditEntityKind,
	id string,
	before domain.Revision,
	after domain.Revision,
	tombstoned bool,
) domain.EntityRevisionChange {
	copyBefore := before
	return domain.EntityRevisionChange{
		Kind: kind, ID: id, Before: &copyBefore, After: after, Tombstoned: tombstoned,
	}
}

func entityKey(kind domain.EditEntityKind, id string) string {
	return string(kind) + "\x00" + id
}

func splitEntityKey(value string) (domain.EditEntityKind, string) {
	kind, id, _ := strings.Cut(value, "\x00")
	return domain.EditEntityKind(kind), id
}

func cloneAuthoredTexts(source map[string]domain.AuthoredTextState) map[string]domain.AuthoredTextState {
	result := make(map[string]domain.AuthoredTextState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneNarrativeSections(source map[string]domain.NarrativeSectionState) map[string]domain.NarrativeSectionState {
	result := make(map[string]domain.NarrativeSectionState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneVisualIntents(source map[string]domain.VisualIntentState) map[string]domain.VisualIntentState {
	result := make(map[string]domain.VisualIntentState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneNotes(source map[string]domain.NoteState) map[string]domain.NoteState {
	result := make(map[string]domain.NoteState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneIntMap(source map[string]int) map[string]int {
	result := make(map[string]int, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneSourceExcerpts(source map[string]domain.SourceExcerptState) map[string]domain.SourceExcerptState {
	result := make(map[string]domain.SourceExcerptState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneTranscriptCorrections(source map[string]domain.TranscriptCorrectionState) map[string]domain.TranscriptCorrectionState {
	result := make(map[string]domain.TranscriptCorrectionState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneCaptions(source map[string]domain.CaptionState) map[string]domain.CaptionState {
	result := make(map[string]domain.CaptionState, len(source))
	for key, value := range source {
		if value.Provenance.Derivation != nil {
			derivation := *value.Provenance.Derivation
			derivation.SegmentIDs = append([]domain.TranscriptSegmentID(nil), derivation.SegmentIDs...)
			derivation.CorrectionRevisions = append(
				[]domain.TranscriptCorrectionRevisionRef(nil), derivation.CorrectionRevisions...,
			)
			value.Provenance.Derivation = &derivation
		}
		if value.ProvenanceStatus != nil {
			status := *value.ProvenanceStatus
			value.ProvenanceStatus = &status
		}
		result[key] = value
	}
	return result
}

func cloneClips(source map[string]domain.ClipState) map[string]domain.ClipState {
	result := make(map[string]domain.ClipState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneLinkGroups(source map[string]domain.LinkGroupState) map[string]domain.LinkGroupState {
	result := make(map[string]domain.LinkGroupState, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneLinkGroupClips(source map[string][]domain.ClipID) map[string][]domain.ClipID {
	result := make(map[string][]domain.ClipID, len(source))
	for key, value := range source {
		result[key] = append([]domain.ClipID(nil), value...)
	}
	return result
}

func cloneAlignments(source map[string]domain.AlignmentState) map[string]domain.AlignmentState {
	result := make(map[string]domain.AlignmentState, len(source))
	for key, value := range source {
		value.Targets = cloneAlignmentTargets(value.Targets)
		result[key] = value
	}
	return result
}

func cloneAlignmentTargets(source []domain.AlignmentTarget) []domain.AlignmentTarget {
	result := make([]domain.AlignmentTarget, len(source))
	for index, target := range source {
		result[index] = target
		if target.Caption != nil {
			value := *target.Caption
			result[index].Caption = &value
		}
		if target.Clip != nil {
			value := *target.Clip
			result[index].Clip = &value
		}
		if target.Timeline != nil {
			value := *target.Timeline
			result[index].Timeline = &value
		}
	}
	return result
}

func validateAllocationMatchesOperations(
	allocation []domain.LocalAllocation,
	operations []EditOperationInput,
) error {
	expected := make(map[string]domain.EditEntityKind)
	for _, operation := range operations {
		if operation.CreateAs != nil {
			expected[operation.CreateAs.String()] = createdKind(operation.Type)
		}
		if operation.CreateLinkGroupAs != nil {
			expected[operation.CreateLinkGroupAs.String()] = domain.EntityLinkGroup
		}
		for _, local := range []*domain.LocalID{operation.LeftLinkGroupAs, operation.RightLinkGroupAs} {
			if local != nil {
				expected[local.String()] = domain.EntityLinkGroup
			}
		}
		for _, output := range operation.SplitOutputs {
			expected[output.LeftAs.String()] = domain.EntityClip
			expected[output.RightAs.String()] = domain.EntityClip
		}
		for _, output := range operation.DerivedCaptions {
			expected[output.CaptionAs.String()] = domain.EntityCaption
			expected[output.AlignmentAs.String()] = domain.EntityAlignment
		}
		for _, output := range operation.DerivedRoughCut {
			expected[output.AlignmentAs.String()] = domain.EntityAlignment
			if output.Video != nil {
				expected[output.Video.ClipAs.String()] = domain.EntityClip
			}
			if output.Audio != nil {
				expected[output.Audio.ClipAs.String()] = domain.EntityClip
			}
			if output.LinkGroupAs != nil {
				expected[output.LinkGroupAs.String()] = domain.EntityLinkGroup
			}
		}
	}
	if len(expected) != len(allocation) {
		return ErrEditInvalid
	}
	for _, current := range allocation {
		if expected[current.Local.String()] != current.Kind || validateEntityID(current.Kind, current.ID) != nil {
			return ErrEditInvalid
		}
	}
	return nil
}
