package application

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
)

type transcriptEvidenceToken struct {
	segmentOrdinal uint32
	token          domain.TranscriptToken
}

func ResolveTranscriptEvidenceText(
	artifact EditTranscriptArtifactState,
	segmentIDs []domain.TranscriptSegmentID,
	rangeValue domain.TimeRange,
	corrections []domain.TranscriptCorrectionState,
) (string, error) {
	_, tokens, err := transcriptEvidenceSpan(artifact, segmentIDs, rangeValue)
	if err != nil {
		return "", err
	}
	sort.Slice(corrections, func(left, right int) bool {
		comparison, _ := corrections[left].SourceRange.Start.Compare(corrections[right].SourceRange.Start)
		return comparison < 0
	})
	return effectiveTranscriptText(tokens, corrections)
}

func (normalizer *editNormalizer) addTranscriptCorrection(operation EditOperationInput) error {
	allocation := normalizer.allocations[operation.CreateAs.String()]
	if allocation.Kind != domain.EntityTranscriptCorrection {
		return ErrEditInvalid
	}
	id, _ := domain.ParseTranscriptCorrectionID(allocation.ID)
	if _, exists := normalizer.corrections[id.String()]; exists ||
		normalizer.markTouched(domain.EntityTranscriptCorrection, id.String()) != nil {
		return ErrEditInvalid
	}
	artifact, exists := normalizer.input.State.TranscriptArtifacts[operation.TranscriptArtifactID.String()]
	if !exists || artifact.AssetID != *operation.AssetID {
		return ErrEditInvalid
	}
	segments, _, err := transcriptEvidenceSpan(artifact, operation.TranscriptSegmentIDs, *operation.SourceRange)
	if err != nil || transcriptCorrectionOverlaps(
		normalizer.corrections, artifact.ID, *operation.Language, *operation.SourceRange, "",
	) {
		return ErrEditInvalid
	}
	revision, _ := domain.NewRevision(1)
	state := domain.TranscriptCorrectionState{
		ID: id, Revision: revision, AssetID: artifact.AssetID, ArtifactID: artifact.ID,
		SegmentIDs: transcriptSegmentIDs(segments), SourceRange: *operation.SourceRange,
		ReplacementText: *operation.Text, Language: *operation.Language,
	}
	normalizer.corrections[id.String()] = state
	inverse := state
	inverse.Revision = mustNext(revision)
	inverse.Tombstoned = true
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutTranscriptCorrection, TranscriptCorrection: &state},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutTranscriptCorrection, TranscriptCorrection: &inverse},
	)
	normalizer.changes = append(normalizer.changes, newEntityChange(
		domain.EntityTranscriptCorrection, id.String(), revision, false,
	))
	return nil
}

func (normalizer *editNormalizer) updateTranscriptCorrection(operation EditOperationInput, remove bool) error {
	current, exists := normalizer.corrections[operation.TranscriptCorrectionID.String()]
	if !exists || current.Tombstoned ||
		normalizer.markTouched(domain.EntityTranscriptCorrection, current.ID.String()) != nil {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityTranscriptCorrection, current.ID.String(), current.Revision); err != nil {
		return err
	}
	next := current
	next.Revision = mustNext(current.Revision)
	if remove {
		next.Tombstoned = true
	} else {
		next.ReplacementText = *operation.Text
		next.Language = *operation.Language
		if transcriptCorrectionOverlaps(
			normalizer.corrections, next.ArtifactID, next.Language, next.SourceRange, current.ID.String(),
		) {
			return ErrEditInvalid
		}
	}
	inverse := current
	inverse.Revision = mustNext(next.Revision)
	normalizer.corrections[current.ID.String()] = next
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutTranscriptCorrection, TranscriptCorrection: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutTranscriptCorrection, TranscriptCorrection: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityTranscriptCorrection, current.ID.String(), current.Revision, next.Revision, remove,
	))
	return nil
}

func (normalizer *editNormalizer) insertSourceExcerpt(operation EditOperationInput) error {
	allocation := normalizer.allocations[operation.CreateAs.String()]
	if allocation.Kind != domain.EntityNarrativeNode {
		return ErrEditInvalid
	}
	id, _ := domain.ParseNarrativeNodeID(allocation.ID)
	if _, exists := normalizer.sourceExcerpts[id.String()]; exists ||
		normalizer.narrativeNodeExists(id.String()) ||
		normalizer.markTouched(domain.EntityNarrativeNode, id.String()) != nil {
		return ErrEditInvalid
	}
	parent, err := normalizer.requireNarrativeParent(*operation.ParentID)
	if err != nil {
		return err
	}
	afterID, err := normalizer.resolveNarrativeNodeReference(operation.After)
	if err != nil {
		return err
	}
	if afterID != nil && normalizer.narrativeNodeParent(*afterID) != parent.ID {
		return ErrEditInvalid
	}
	artifact, exists := normalizer.input.State.TranscriptArtifacts[operation.TranscriptArtifactID.String()]
	if !exists || artifact.AssetID != *operation.AssetID || artifact.Fingerprint != *operation.AcceptedFingerprint {
		return ErrEditInvalid
	}
	segments, tokens, err := transcriptEvidenceSpan(artifact, operation.TranscriptSegmentIDs, *operation.SourceRange)
	if err != nil {
		return err
	}
	corrections, err := normalizer.sourceExcerptCorrections(
		artifact.ID, *operation.Language, *operation.SourceRange, operation.CorrectionRevisions,
	)
	if err != nil {
		return err
	}
	effectiveText, err := effectiveTranscriptText(tokens, corrections)
	if err != nil {
		return err
	}
	revision, _ := domain.NewRevision(1)
	state := domain.SourceExcerptState{
		ID: id, Revision: revision, DocumentID: parent.DocumentID, ParentID: parent.ID,
		AfterNodeID: afterID, AssetID: artifact.AssetID, AcceptedFingerprint: artifact.Fingerprint,
		SourceRange: *operation.SourceRange, Language: *operation.Language, EffectiveText: effectiveText,
		Evidence: domain.SourceExcerptTranscriptEvidence{
			ArtifactID: artifact.ID, SourceStreamID: artifact.SourceStreamID,
			SegmentIDs: transcriptSegmentIDs(segments), CorrectionRevisions: correctionRevisionRefs(corrections),
		},
	}
	normalizer.sourceExcerpts[id.String()] = state
	inverse := state
	inverse.Revision = mustNext(revision)
	inverse.Tombstoned = true
	node := domain.NarrativeNodeState{Kind: domain.NarrativeNodeSourceExcerpt, SourceExcerpt: &state}
	inverseNode := domain.NarrativeNodeState{Kind: domain.NarrativeNodeSourceExcerpt, SourceExcerpt: &inverse}
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &node},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutNarrativeNode, NarrativeNode: &inverseNode},
	)
	normalizer.changes = append(normalizer.changes, newEntityChange(
		domain.EntityNarrativeNode, id.String(), revision, false,
	))
	normalizer.narrativeChanged = true
	normalizer.markNarrativeParentChanged(parent.ID)
	normalizer.sectionChildren[parent.ID.String()]++
	return nil
}

func transcriptEvidenceSpan(
	artifact EditTranscriptArtifactState,
	ids []domain.TranscriptSegmentID,
	rangeValue domain.TimeRange,
) ([]EditTranscriptSegmentState, []transcriptEvidenceToken, error) {
	if len(ids) == 0 || len(ids) > 256 || rangeValue.Start.Validate() != nil || rangeValue.Duration.Validate() != nil ||
		!rangeValue.Duration.IsPositive() {
		return nil, nil, ErrEditInvalid
	}
	segments := make([]EditTranscriptSegmentState, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for index, id := range ids {
		segment, exists := artifact.Segments[id.String()]
		if !exists || id.IsZero() {
			return nil, nil, ErrEditInvalid
		}
		if _, duplicate := seen[id.String()]; duplicate {
			return nil, nil, ErrEditInvalid
		}
		seen[id.String()] = struct{}{}
		if index > 0 && segment.Ordinal != segments[index-1].Ordinal+1 {
			return nil, nil, ErrEditInvalid
		}
		segments = append(segments, segment)
	}
	end, err := rangeValue.End()
	if err != nil {
		return nil, nil, ErrEditInvalid
	}
	tokens := make([]transcriptEvidenceToken, 0)
	segmentTokenCount := make(map[uint32]int, len(segments))
	for _, segment := range segments {
		for _, token := range segment.Tokens {
			tokenEnd, tokenErr := token.SourceRange.End()
			startsBeforeEnd, firstErr := token.SourceRange.Start.Compare(end)
			endsAfterStart, secondErr := tokenEnd.Compare(rangeValue.Start)
			if tokenErr != nil || firstErr != nil || secondErr != nil {
				return nil, nil, ErrEditInvalid
			}
			if startsBeforeEnd >= 0 || endsAfterStart <= 0 {
				continue
			}
			startInside, _ := token.SourceRange.Start.Compare(rangeValue.Start)
			endInside, _ := tokenEnd.Compare(end)
			if startInside < 0 || endInside > 0 {
				return nil, nil, ErrEditInvalid
			}
			tokens = append(tokens, transcriptEvidenceToken{segmentOrdinal: segment.Ordinal, token: token})
			segmentTokenCount[segment.Ordinal]++
		}
	}
	if len(tokens) == 0 {
		return nil, nil, ErrEditInvalid
	}
	firstComparison, _ := tokens[0].token.SourceRange.Start.Compare(rangeValue.Start)
	lastEnd, _ := tokens[len(tokens)-1].token.SourceRange.End()
	lastComparison, _ := lastEnd.Compare(end)
	if firstComparison != 0 || lastComparison != 0 {
		return nil, nil, ErrEditInvalid
	}
	for _, segment := range segments {
		if segmentTokenCount[segment.Ordinal] == 0 {
			return nil, nil, ErrEditInvalid
		}
	}
	return segments, tokens, nil
}

func (normalizer *editNormalizer) sourceExcerptCorrections(
	artifactID domain.ArtifactID,
	language domain.CaptionLanguage,
	rangeValue domain.TimeRange,
	references []TranscriptCorrectionReferenceInput,
) ([]domain.TranscriptCorrectionState, error) {
	provided := make(map[string]domain.Revision, len(references))
	for _, reference := range references {
		value, err := normalizer.resolveReference(reference.Correction, domain.EntityTranscriptCorrection)
		if err != nil {
			return nil, ErrEditInvalid
		}
		correction, exists := normalizer.corrections[value]
		if !exists || correction.Tombstoned {
			return nil, ErrEditInvalid
		}
		revision := correction.Revision
		if reference.Correction.ID != "" {
			if reference.Revision == nil || *reference.Revision != revision {
				return nil, ErrEditConflict
			}
			if err := normalizer.require(domain.EntityTranscriptCorrection, value, revision); err != nil {
				return nil, err
			}
		} else if reference.Revision != nil {
			return nil, ErrEditInvalid
		}
		if _, duplicate := provided[value]; duplicate {
			return nil, ErrEditInvalid
		}
		provided[value] = revision
	}
	result := make([]domain.TranscriptCorrectionState, 0, len(references))
	for id, correction := range normalizer.corrections {
		if correction.Tombstoned || correction.ArtifactID != artifactID || correction.Language != language ||
			!rangesOverlap(correction.SourceRange, rangeValue) {
			continue
		}
		if !transcriptRangeContains(rangeValue, correction.SourceRange) || provided[id] != correction.Revision {
			return nil, ErrEditInvalid
		}
		result = append(result, correction)
		delete(provided, id)
	}
	if len(provided) != 0 {
		return nil, ErrEditInvalid
	}
	sort.Slice(result, func(left, right int) bool {
		comparison, _ := result[left].SourceRange.Start.Compare(result[right].SourceRange.Start)
		return comparison < 0
	})
	for index := 1; index < len(result); index++ {
		if rangesOverlap(result[index-1].SourceRange, result[index].SourceRange) {
			return nil, ErrEditInvalid
		}
	}
	return result, nil
}

func effectiveTranscriptText(
	tokens []transcriptEvidenceToken,
	corrections []domain.TranscriptCorrectionState,
) (string, error) {
	var result strings.Builder
	correctionIndex := 0
	var skippedUntil *domain.RationalTime
	var previousSegment uint32
	for index, current := range tokens {
		if skippedUntil != nil {
			comparison, _ := current.token.SourceRange.Start.Compare(*skippedUntil)
			if comparison < 0 {
				continue
			}
			skippedUntil = nil
		}
		if correctionIndex < len(corrections) {
			correction := corrections[correctionIndex]
			comparison, _ := current.token.SourceRange.Start.Compare(correction.SourceRange.Start)
			if comparison == 0 {
				appendTranscriptText(&result, correction.ReplacementText, index > 0)
				end, _ := correction.SourceRange.End()
				skippedUntil = &end
				correctionIndex++
				previousSegment = current.segmentOrdinal
				continue
			}
			if comparison > 0 {
				return "", ErrEditInvalid
			}
		}
		appendTranscriptText(&result, current.token.Text, index > 0 && current.segmentOrdinal != previousSegment)
		previousSegment = current.segmentOrdinal
	}
	if correctionIndex != len(corrections) {
		return "", ErrEditInvalid
	}
	value := strings.TrimSpace(result.String())
	if value == "" || len([]byte(value)) > domain.MaximumAuthoredTextBytes {
		return "", ErrEditInvalid
	}
	return value, nil
}

func appendTranscriptText(result *strings.Builder, value string, ensureBoundary bool) {
	if result.Len() > 0 && ensureBoundary && needsTranscriptSpace(result.String(), value) {
		result.WriteByte(' ')
	}
	result.WriteString(value)
}

func needsTranscriptSpace(existing, next string) bool {
	if existing == "" || next == "" {
		return false
	}
	left, _ := utf8.DecodeLastRuneInString(existing)
	right, _ := utf8.DecodeRuneInString(next)
	return !unicode.IsSpace(left) && !unicode.IsSpace(right) && !unicode.IsPunct(right)
}

func transcriptRangeContains(parent, child domain.TimeRange) bool {
	parentEnd, parentErr := parent.End()
	childEnd, childErr := child.End()
	startComparison, startErr := child.Start.Compare(parent.Start)
	endComparison, endErr := childEnd.Compare(parentEnd)
	return parentErr == nil && childErr == nil && startErr == nil && endErr == nil &&
		startComparison >= 0 && endComparison <= 0
}

func transcriptCorrectionOverlaps(
	corrections map[string]domain.TranscriptCorrectionState,
	artifactID domain.ArtifactID,
	language domain.CaptionLanguage,
	rangeValue domain.TimeRange,
	exclude string,
) bool {
	for id, correction := range corrections {
		if id != exclude && !correction.Tombstoned && correction.ArtifactID == artifactID &&
			correction.Language == language && rangesOverlap(correction.SourceRange, rangeValue) {
			return true
		}
	}
	return false
}

func transcriptSegmentIDs(segments []EditTranscriptSegmentState) []domain.TranscriptSegmentID {
	result := make([]domain.TranscriptSegmentID, len(segments))
	for index, segment := range segments {
		result[index] = segment.ID
	}
	return result
}

func correctionRevisionRefs(corrections []domain.TranscriptCorrectionState) []domain.TranscriptCorrectionRevisionRef {
	result := make([]domain.TranscriptCorrectionRevisionRef, len(corrections))
	for index, correction := range corrections {
		result[index] = domain.TranscriptCorrectionRevisionRef{ID: correction.ID, Revision: correction.Revision}
	}
	return result
}
