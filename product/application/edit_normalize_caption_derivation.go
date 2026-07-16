package application

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/rivo/uniseg"
)

type CaptionDerivationCue struct {
	SourceRange         domain.TimeRange
	TimelineRange       domain.TimeRange
	EvidenceSourceRange domain.TimeRange
	Text                string
	SegmentIDs          []domain.TranscriptSegmentID
	CorrectionRevisions []domain.TranscriptCorrectionRevisionRef
}

type captionEvidenceUnit struct {
	rangeValue         domain.TimeRange
	text               string
	ensureBoundary     bool
	segmentIDs         []domain.TranscriptSegmentID
	correctionRevision *domain.TranscriptCorrectionRevisionRef
}

func DeriveCaptionCues(
	artifact EditTranscriptArtifactState,
	excerpt domain.SourceExcerptState,
	correctionState map[string]domain.TranscriptCorrectionState,
	clip domain.ClipState,
	policy domain.CaptionDerivationPolicy,
) ([]CaptionDerivationCue, error) {
	if policy.Validate() != nil || excerpt.Tombstoned || clip.Tombstoned || !clip.Enabled ||
		artifact.ID != excerpt.Evidence.ArtifactID || artifact.AssetID != excerpt.AssetID ||
		artifact.Fingerprint != excerpt.AcceptedFingerprint || artifact.SourceStreamID != excerpt.Evidence.SourceStreamID ||
		clip.AssetID != excerpt.AssetID || !transcriptRangeContains(clip.SourceRange, excerpt.SourceRange) {
		return nil, ErrEditInvalid
	}
	segments, tokens, err := transcriptEvidenceSpan(artifact, excerpt.Evidence.SegmentIDs, excerpt.SourceRange)
	if err != nil {
		return nil, err
	}
	corrections, err := captionExcerptCorrections(excerpt, correctionState)
	if err != nil {
		return nil, err
	}
	effective, err := effectiveTranscriptText(tokens, corrections)
	if err != nil || effective != excerpt.EffectiveText {
		return nil, ErrEditInvalid
	}
	units, err := captionEvidenceUnits(segments, tokens, corrections)
	if err != nil {
		return nil, err
	}
	groups, err := groupCaptionUnits(units, policy)
	if err != nil {
		return nil, err
	}
	excerptEnd, _ := excerpt.SourceRange.End()
	clipEnd, _ := clip.SourceRange.End()
	capEnd := excerptEnd
	if comparison, _ := clipEnd.Compare(capEnd); comparison < 0 {
		capEnd = clipEnd
	}
	result := make([]CaptionDerivationCue, 0, len(groups))
	for index, group := range groups {
		text, err := wrapCaptionUnits(group, policy)
		if err != nil {
			return nil, err
		}
		evidenceStart := group[0].rangeValue.Start
		evidenceEnd, _ := group[len(group)-1].rangeValue.End()
		if index+1 < len(groups) {
			capEnd = groups[index+1][0].rangeValue.Start
		} else {
			capEnd = excerptEnd
			if comparison, _ := clipEnd.Compare(capEnd); comparison < 0 {
				capEnd = clipEnd
			}
		}
		desiredEnd := evidenceEnd
		minimumEnd, err := evidenceStart.Add(policy.MinimumDuration)
		if err != nil {
			return nil, ErrEditInvalid
		}
		if comparison, _ := minimumEnd.Compare(desiredEnd); comparison > 0 {
			desiredEnd = minimumEnd
		}
		readingDuration, err := domain.NewRationalTime(int64(uniseg.GraphemeClusterCount(strings.ReplaceAll(text, "\n", ""))), int32(policy.MaximumReadingRate))
		if err != nil {
			return nil, ErrEditInvalid
		}
		readingEnd, err := evidenceStart.Add(readingDuration)
		if err != nil {
			return nil, ErrEditInvalid
		}
		if comparison, _ := readingEnd.Compare(desiredEnd); comparison > 0 {
			desiredEnd = readingEnd
		}
		if comparison, _ := desiredEnd.Compare(capEnd); comparison > 0 {
			return nil, ErrEditInvalid
		}
		duration, err := desiredEnd.Subtract(evidenceStart)
		if err != nil || !duration.IsPositive() {
			return nil, ErrEditInvalid
		}
		if comparison, _ := duration.Compare(policy.MaximumDuration); comparison > 0 {
			return nil, ErrEditInvalid
		}
		sourceRange, _ := domain.NewTimeRange(evidenceStart, duration)
		sourceOffset, err := evidenceStart.Subtract(clip.SourceRange.Start)
		if err != nil {
			return nil, ErrEditInvalid
		}
		timelineStart, err := clip.TimelineRange.Start.Add(sourceOffset)
		if err != nil {
			return nil, ErrEditInvalid
		}
		timelineRange, err := domain.NewTimeRange(timelineStart, duration)
		if err != nil || !transcriptRangeContains(clip.TimelineRange, timelineRange) {
			return nil, ErrEditInvalid
		}
		evidenceDuration, _ := evidenceEnd.Subtract(evidenceStart)
		evidenceRange, _ := domain.NewTimeRange(evidenceStart, evidenceDuration)
		segmentIDs, correctionRefs := captionGroupEvidence(group)
		result = append(result, CaptionDerivationCue{
			SourceRange: sourceRange, TimelineRange: timelineRange, EvidenceSourceRange: evidenceRange,
			Text: text, SegmentIDs: segmentIDs, CorrectionRevisions: correctionRefs,
		})
	}
	return result, nil
}

func captionExcerptCorrections(
	excerpt domain.SourceExcerptState,
	state map[string]domain.TranscriptCorrectionState,
) ([]domain.TranscriptCorrectionState, error) {
	provided := make(map[string]domain.Revision, len(excerpt.Evidence.CorrectionRevisions))
	for _, reference := range excerpt.Evidence.CorrectionRevisions {
		if _, duplicate := provided[reference.ID.String()]; duplicate {
			return nil, ErrEditInvalid
		}
		provided[reference.ID.String()] = reference.Revision
	}
	result := make([]domain.TranscriptCorrectionState, 0, len(provided))
	for id, correction := range state {
		if correction.Tombstoned || correction.ArtifactID != excerpt.Evidence.ArtifactID ||
			correction.Language != excerpt.Language || !rangesOverlap(correction.SourceRange, excerpt.SourceRange) {
			continue
		}
		if !transcriptRangeContains(excerpt.SourceRange, correction.SourceRange) ||
			provided[id] != correction.Revision {
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

func captionEvidenceUnits(
	segments []EditTranscriptSegmentState,
	tokens []transcriptEvidenceToken,
	corrections []domain.TranscriptCorrectionState,
) ([]captionEvidenceUnit, error) {
	segmentByOrdinal := make(map[uint32]domain.TranscriptSegmentID, len(segments))
	for _, segment := range segments {
		segmentByOrdinal[segment.Ordinal] = segment.ID
	}
	result := make([]captionEvidenceUnit, 0, len(tokens))
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
				ref := domain.TranscriptCorrectionRevisionRef{ID: correction.ID, Revision: correction.Revision}
				result = append(result, captionEvidenceUnit{
					rangeValue: correction.SourceRange, text: correction.ReplacementText,
					ensureBoundary: index > 0, segmentIDs: append([]domain.TranscriptSegmentID(nil), correction.SegmentIDs...),
					correctionRevision: &ref,
				})
				end, _ := correction.SourceRange.End()
				skippedUntil = &end
				correctionIndex++
				previousSegment = current.segmentOrdinal
				continue
			}
			if comparison > 0 {
				return nil, ErrEditInvalid
			}
		}
		segmentID, exists := segmentByOrdinal[current.segmentOrdinal]
		if !exists {
			return nil, ErrEditInvalid
		}
		result = append(result, captionEvidenceUnit{
			rangeValue: current.token.SourceRange, text: current.token.Text,
			ensureBoundary: index > 0 && current.segmentOrdinal != previousSegment,
			segmentIDs:     []domain.TranscriptSegmentID{segmentID},
		})
		previousSegment = current.segmentOrdinal
	}
	if correctionIndex != len(corrections) || len(result) == 0 {
		return nil, ErrEditInvalid
	}
	return result, nil
}

func groupCaptionUnits(
	units []captionEvidenceUnit,
	policy domain.CaptionDerivationPolicy,
) ([][]captionEvidenceUnit, error) {
	groups := make([][]captionEvidenceUnit, 0)
	current := make([]captionEvidenceUnit, 0)
	for _, unit := range units {
		if uniseg.GraphemeClusterCount(strings.TrimSpace(unit.text)) > int(policy.MaximumLineGraphemes) {
			return nil, ErrEditInvalid
		}
		breakBefore := false
		if len(current) > 0 {
			previous := current[len(current)-1]
			previousEnd, _ := previous.rangeValue.End()
			gap, err := unit.rangeValue.Start.Subtract(previousEnd)
			if err != nil || gap.IsNegative() {
				return nil, ErrEditInvalid
			}
			if comparison, _ := gap.Compare(policy.MaximumGap); comparison > 0 {
				breakBefore = true
			}
			candidateEnd, _ := unit.rangeValue.End()
			candidateDuration, _ := candidateEnd.Subtract(current[0].rangeValue.Start)
			if comparison, _ := candidateDuration.Compare(policy.MaximumDuration); comparison > 0 {
				breakBefore = true
			}
			currentEnd, _ := previous.rangeValue.End()
			currentDuration, _ := currentEnd.Subtract(current[0].rangeValue.Start)
			if captionUnitEndsTerminal(previous) {
				if comparison, _ := currentDuration.Compare(policy.MinimumDuration); comparison >= 0 {
					breakBefore = true
				}
			}
			if !breakBefore {
				candidate := append(append([]captionEvidenceUnit(nil), current...), unit)
				if _, err := wrapCaptionUnits(candidate, policy); err != nil {
					breakBefore = true
				}
			}
		}
		if breakBefore {
			groups = append(groups, current)
			current = nil
		}
		current = append(current, unit)
		if _, err := wrapCaptionUnits(current, policy); err != nil {
			return nil, err
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	if len(groups) == 0 || len(groups) > 128 {
		return nil, ErrEditInvalid
	}
	return groups, nil
}

func wrapCaptionUnits(
	units []captionEvidenceUnit,
	policy domain.CaptionDerivationPolicy,
) (string, error) {
	lines := make([][]captionEvidenceUnit, 0, policy.MaximumLines)
	current := make([]captionEvidenceUnit, 0)
	for _, unit := range units {
		candidate := append(append([]captionEvidenceUnit(nil), current...), unit)
		text := joinCaptionUnits(candidate)
		if uniseg.GraphemeClusterCount(text) <= int(policy.MaximumLineGraphemes) {
			current = candidate
			continue
		}
		if len(current) == 0 {
			return "", ErrEditInvalid
		}
		lines = append(lines, current)
		current = []captionEvidenceUnit{unit}
		if len(lines) >= int(policy.MaximumLines) ||
			uniseg.GraphemeClusterCount(joinCaptionUnits(current)) > int(policy.MaximumLineGraphemes) {
			return "", ErrEditInvalid
		}
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}
	if len(lines) == 0 || len(lines) > int(policy.MaximumLines) {
		return "", ErrEditInvalid
	}
	textLines := make([]string, len(lines))
	for index, line := range lines {
		textLines[index] = joinCaptionUnits(line)
	}
	value := strings.Join(textLines, "\n")
	if value == "" || len([]byte(value)) > domain.MaximumAuthoredTextBytes {
		return "", ErrEditInvalid
	}
	return value, nil
}

func joinCaptionUnits(units []captionEvidenceUnit) string {
	var result strings.Builder
	for _, unit := range units {
		appendTranscriptText(&result, unit.text, unit.ensureBoundary)
	}
	return strings.TrimSpace(result.String())
}

func captionUnitEndsTerminal(unit captionEvidenceUnit) bool {
	value := strings.TrimSpace(unit.text)
	if value == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(value)
	return strings.ContainsRune(".!?。！？…", last)
}

func captionGroupEvidence(
	group []captionEvidenceUnit,
) ([]domain.TranscriptSegmentID, []domain.TranscriptCorrectionRevisionRef) {
	segments := make([]domain.TranscriptSegmentID, 0)
	corrections := make([]domain.TranscriptCorrectionRevisionRef, 0)
	seenSegments := make(map[string]struct{})
	seenCorrections := make(map[string]struct{})
	for _, unit := range group {
		for _, segmentID := range unit.segmentIDs {
			if _, exists := seenSegments[segmentID.String()]; !exists {
				seenSegments[segmentID.String()] = struct{}{}
				segments = append(segments, segmentID)
			}
		}
		if unit.correctionRevision != nil {
			if _, exists := seenCorrections[unit.correctionRevision.ID.String()]; !exists {
				seenCorrections[unit.correctionRevision.ID.String()] = struct{}{}
				corrections = append(corrections, *unit.correctionRevision)
			}
		}
	}
	return segments, corrections
}

func (normalizer *editNormalizer) deriveCaptions(operation EditOperationInput) error {
	nodeID, err := normalizer.resolveNodeReference(*operation.NarrativeNode)
	if err != nil {
		return err
	}
	excerpt, exists := normalizer.sourceExcerpts[nodeID.String()]
	if !exists || excerpt.Tombstoned {
		return ErrEditInvalid
	}
	if operation.NarrativeNode.ID != "" {
		if err := normalizer.require(domain.EntityNarrativeNode, nodeID.String(), excerpt.Revision); err != nil {
			return err
		}
	}
	clipID, err := normalizer.resolveClipReference(*operation.Clip)
	if err != nil {
		return err
	}
	clip, exists := normalizer.clips[clipID.String()]
	if !exists || clip.Tombstoned || clip.SequenceID != normalizer.input.SequenceID {
		return ErrEditInvalid
	}
	if operation.Clip.ID != "" {
		if err := normalizer.require(domain.EntityClip, clip.ID.String(), clip.Revision); err != nil {
			return err
		}
	}
	track := normalizer.input.State.Tracks[operation.TrackID.String()]
	if track.ID.IsZero() || track.SequenceID != normalizer.input.SequenceID || track.Type != domain.TrackCaption {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityTrack, track.ID.String(), track.Revision); err != nil {
		return err
	}
	artifact, exists := normalizer.input.State.TranscriptArtifacts[excerpt.Evidence.ArtifactID.String()]
	stream, streamExists := normalizer.input.State.SourceStreams[excerpt.Evidence.SourceStreamID.String()]
	if !exists || !streamExists || stream.AssetID != excerpt.AssetID || stream.AssetRevision.Value() < 1 {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityAsset, excerpt.AssetID.String(), stream.AssetRevision); err != nil {
		return err
	}
	for _, reference := range excerpt.Evidence.CorrectionRevisions {
		correction, exists := normalizer.corrections[reference.ID.String()]
		if !exists || correction.Revision != reference.Revision || correction.Tombstoned {
			return ErrEditInvalid
		}
		if err := normalizer.require(domain.EntityTranscriptCorrection, reference.ID.String(), reference.Revision); err != nil {
			return err
		}
	}
	cues, err := DeriveCaptionCues(artifact, excerpt, normalizer.corrections, clip, *operation.CaptionPolicy)
	if err != nil || len(cues) != len(operation.DerivedCaptions) {
		return ErrEditInvalid
	}
	for index, cue := range cues {
		output := operation.DerivedCaptions[index]
		if output.Text != cue.Text || !equalTimeRange(output.SourceRange, cue.SourceRange) ||
			!equalTimeRange(output.TimelineRange, cue.TimelineRange) {
			return ErrEditInvalid
		}
		captionAllocation := normalizer.allocations[output.CaptionAs.String()]
		alignmentAllocation := normalizer.allocations[output.AlignmentAs.String()]
		if captionAllocation.Kind != domain.EntityCaption || alignmentAllocation.Kind != domain.EntityAlignment {
			return ErrEditInvalid
		}
		captionID, captionErr := domain.ParseCaptionID(captionAllocation.ID)
		alignmentID, alignmentErr := domain.ParseAlignmentID(alignmentAllocation.ID)
		if captionErr != nil || alignmentErr != nil ||
			normalizer.markTouched(domain.EntityCaption, captionID.String()) != nil ||
			normalizer.markTouched(domain.EntityAlignment, alignmentID.String()) != nil {
			return ErrEditInvalid
		}
		revision, _ := domain.NewRevision(1)
		provenance := domain.CaptionProvenance{
			Kind: domain.CaptionProvenanceTranscriptDerivation,
			Derivation: &domain.CaptionDerivationProvenance{
				SourceExcerptID: excerpt.ID, SourceExcerptRevision: excerpt.Revision,
				AssetID: excerpt.AssetID, AcceptedFingerprint: excerpt.AcceptedFingerprint,
				ArtifactID: excerpt.Evidence.ArtifactID, SourceStreamID: excerpt.Evidence.SourceStreamID,
				SegmentIDs:          append([]domain.TranscriptSegmentID(nil), cue.SegmentIDs...),
				CorrectionRevisions: append([]domain.TranscriptCorrectionRevisionRef(nil), cue.CorrectionRevisions...),
				ClipID:              clip.ID, ClipRevision: clip.Revision, ClipSourceRange: clip.SourceRange,
				ClipTimelineRange: clip.TimelineRange, EvidenceSourceRange: cue.EvidenceSourceRange,
				Policy: *operation.CaptionPolicy, DerivedRange: cue.TimelineRange,
				DerivedLanguage: excerpt.Language, DerivedText: cue.Text,
			},
		}
		caption := domain.CaptionState{
			ID: captionID, Revision: revision, SequenceID: track.SequenceID, TrackID: track.ID,
			Range: cue.TimelineRange, Language: excerpt.Language, Text: cue.Text, Provenance: provenance,
		}
		alignment := domain.AlignmentState{
			ID: alignmentID, Revision: revision, NarrativeNodeID: excerpt.ID,
			NarrativeNodeRevision: excerpt.Revision, SequenceID: track.SequenceID,
			Targets: []domain.AlignmentTarget{{
				Type: domain.AlignmentTargetCaption,
				Caption: &domain.CaptionAlignmentTarget{
					CaptionID: captionID, CaptionRevision: revision,
					LocalRange: domain.TimeRange{Start: domain.RationalTime{Value: 0, Scale: 1}, Duration: cue.TimelineRange.Duration},
				},
			}},
			Status: domain.AlignmentExact,
		}
		normalizer.captions[captionID.String()] = caption
		normalizer.alignments[alignmentID.String()] = alignment
		captionInverse := caption
		captionInverse.Revision = mustNext(revision)
		captionInverse.Tombstoned = true
		alignmentInverse := alignment
		alignmentInverse.Revision = mustNext(revision)
		alignmentInverse.Targets = cloneAlignmentTargets(alignment.Targets)
		alignmentInverse.Status = domain.AlignmentUnbound
		normalizer.appendOperation(
			domain.NormalizedEditOperation{Type: domain.NormalizedPutCaption, Caption: &caption},
			domain.NormalizedEditOperation{Type: domain.NormalizedPutCaption, Caption: &captionInverse},
		)
		normalizer.appendOperation(
			domain.NormalizedEditOperation{Type: domain.NormalizedPutAlignment, Alignment: &alignment},
			domain.NormalizedEditOperation{Type: domain.NormalizedPutAlignment, Alignment: &alignmentInverse},
		)
		normalizer.changes = append(normalizer.changes,
			newEntityChange(domain.EntityCaption, captionID.String(), revision, false),
			newEntityChange(domain.EntityAlignment, alignmentID.String(), revision, false),
		)
	}
	normalizer.sequenceChanged = true
	normalizer.trackChanges[track.ID.String()] = track.Revision
	return nil
}

func equalTimeRange(left, right domain.TimeRange) bool {
	start, startErr := left.Start.Compare(right.Start)
	duration, durationErr := left.Duration.Compare(right.Duration)
	return startErr == nil && durationErr == nil && start == 0 && duration == 0
}
