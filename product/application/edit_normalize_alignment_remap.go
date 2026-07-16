package application

import (
	"sort"

	"github.com/PerishCode/open-cut/product/domain"
)

func (normalizer *editNormalizer) remapAlignment(operation EditOperationInput) error {
	current, exists := normalizer.alignments[operation.AlignmentID.String()]
	if !exists || current.Status != domain.AlignmentExact || len(current.Targets) == 0 ||
		normalizer.markTouched(domain.EntityAlignment, current.ID.String()) != nil {
		return ErrEditInvalid
	}
	if _, nodeTouched := normalizer.touched[entityKey(domain.EntityNarrativeNode, current.NarrativeNodeID.String())]; nodeTouched {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityAlignment, current.ID.String(), current.Revision); err != nil {
		return err
	}
	targets, err := normalizer.normalizeAlignmentTargets(operation.AlignmentTargets, true)
	if err != nil || len(targets) == 0 || targets[0].Type != current.Targets[0].Type {
		return ErrEditInvalid
	}
	if !normalizer.preservesAlignmentSemantics(current.Targets, targets) {
		return ErrEditInvalid
	}
	next := current
	next.Revision = mustNext(current.Revision)
	next.Targets = cloneAlignmentTargets(targets)
	next.Status = domain.AlignmentExact
	inverse := current
	inverse.Revision = mustNext(next.Revision)
	inverse.Targets = cloneAlignmentTargets(current.Targets)
	normalizer.alignments[current.ID.String()] = next
	normalizer.alignmentEffects[current.ID.String()] = domain.AlignmentExact
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutAlignment, Alignment: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutAlignment, Alignment: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityAlignment, current.ID.String(), current.Revision, next.Revision, false,
	))
	return nil
}

func (normalizer *editNormalizer) preservesAlignmentSemantics(
	current []domain.AlignmentTarget,
	next []domain.AlignmentTarget,
) bool {
	switch current[0].Type {
	case domain.AlignmentTargetClip:
		oldCoverage, err := alignmentSourceCoverage(current, normalizer.input.State.Clips)
		if err != nil {
			return false
		}
		newCoverage, err := alignmentSourceCoverage(next, normalizer.clips)
		return err == nil && sameAlignmentCoverage(oldCoverage, newCoverage)
	case domain.AlignmentTargetCaption:
		return normalizer.preservesCaptionAlignment(current, next)
	default:
		return false
	}
}

func (normalizer *editNormalizer) preservesCaptionAlignment(
	current []domain.AlignmentTarget,
	next []domain.AlignmentTarget,
) bool {
	if len(current) != len(next) {
		return false
	}
	for index := range current {
		beforeTarget, afterTarget := current[index].Caption, next[index].Caption
		if beforeTarget == nil || afterTarget == nil ||
			beforeTarget.CaptionID != afterTarget.CaptionID ||
			!equalTimeRange(beforeTarget.LocalRange, afterTarget.LocalRange) {
			return false
		}
		before := normalizer.input.State.Captions[beforeTarget.CaptionID.String()]
		after := normalizer.captions[afterTarget.CaptionID.String()]
		if before.ID.IsZero() || after.ID.IsZero() || after.Tombstoned ||
			before.Text != after.Text || before.Language != after.Language ||
			beforeTarget.CaptionRevision != before.Revision || afterTarget.CaptionRevision != after.Revision ||
			!rangeWithin(afterTarget.LocalRange, after.Range.Duration) {
			return false
		}
	}
	return true
}

type alignmentCoverageSpan struct {
	start    domain.RationalTime
	duration domain.RationalTime
}

func alignmentSourceCoverage(
	targets []domain.AlignmentTarget,
	clips map[string]domain.ClipState,
) (map[string][]alignmentCoverageSpan, error) {
	result := make(map[string][]alignmentCoverageSpan)
	for _, target := range targets {
		if target.Type != domain.AlignmentTargetClip || target.Clip == nil {
			return nil, ErrEditInvalid
		}
		clip := clips[target.Clip.ClipID.String()]
		if clip.ID.IsZero() || !rangeWithin(target.Clip.LocalRange, clip.TimelineRange.Duration) {
			return nil, ErrEditInvalid
		}
		start, err := clip.SourceRange.Start.Add(target.Clip.LocalRange.Start)
		if err != nil {
			return nil, ErrEditInvalid
		}
		key := clip.AssetID.String() + "\x00" + clip.SourceStreamID.String()
		result[key] = append(result[key], alignmentCoverageSpan{
			start: start, duration: target.Clip.LocalRange.Duration,
		})
	}
	for key, spans := range result {
		sort.Slice(spans, func(left, right int) bool {
			comparison, err := spans[left].start.Compare(spans[right].start)
			return err == nil && comparison < 0
		})
		merged := make([]alignmentCoverageSpan, 0, len(spans))
		for _, current := range spans {
			if len(merged) == 0 {
				merged = append(merged, current)
				continue
			}
			previous := &merged[len(merged)-1]
			previousEnd, err := previous.start.Add(previous.duration)
			if err != nil {
				return nil, ErrEditInvalid
			}
			comparison, err := current.start.Compare(previousEnd)
			if err != nil || comparison < 0 {
				return nil, ErrEditInvalid
			}
			if comparison == 0 {
				currentEnd, err := current.start.Add(current.duration)
				if err != nil {
					return nil, ErrEditInvalid
				}
				previous.duration, err = currentEnd.Subtract(previous.start)
				if err != nil {
					return nil, ErrEditInvalid
				}
				continue
			}
			merged = append(merged, current)
		}
		result[key] = merged
	}
	return result, nil
}

func sameAlignmentCoverage(
	left, right map[string][]alignmentCoverageSpan,
) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftSpans := range left {
		rightSpans, exists := right[key]
		if !exists || len(leftSpans) != len(rightSpans) {
			return false
		}
		for index := range leftSpans {
			startComparison, startErr := leftSpans[index].start.Compare(rightSpans[index].start)
			durationComparison, durationErr := leftSpans[index].duration.Compare(rightSpans[index].duration)
			if startErr != nil || durationErr != nil || startComparison != 0 || durationComparison != 0 {
				return false
			}
		}
	}
	return true
}
