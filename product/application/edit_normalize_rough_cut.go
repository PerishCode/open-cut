package application

import (
	"fmt"

	"github.com/PerishCode/open-cut/product/domain"
)

func BuildRoughCutOperation(
	state EditNormalizationState,
	timelineStart domain.RationalTime,
	localPrefix domain.LocalID,
	items []RoughCutDerivationItemInput,
) (EditOperationInput, error) {
	if timelineStart.Validate() != nil || timelineStart.IsNegative() || len(items) == 0 || len(items) > 128 ||
		len(localPrefix.String()) > 40 {
		return EditOperationInput{}, ErrEditInvalid
	}
	if _, err := domain.ParseLocalID(localPrefix.String()); err != nil {
		return EditOperationInput{}, ErrEditInvalid
	}
	policy := domain.PaperEditRoughCutPolicyV1()
	outputs := make([]DerivedRoughCutOutputInput, 0, len(items))
	cursor := timelineStart
	for index, item := range items {
		excerpt := state.SourceExcerpts[item.SourceExcerptID.String()]
		if excerpt.ID.IsZero() || excerpt.Tombstoned ||
			state.SourceExcerptEvidence[excerpt.ID.String()] != domain.SourceExcerptEvidenceExact ||
			(item.Video == nil && item.Audio == nil) {
			return EditOperationInput{}, ErrEditInvalid
		}
		if item.Video != nil && validateRoughCutLane(state, excerpt, *item.Video, domain.TrackVideo) != nil {
			return EditOperationInput{}, ErrEditInvalid
		}
		if item.Audio != nil && validateRoughCutLane(state, excerpt, *item.Audio, domain.TrackAudio) != nil {
			return EditOperationInput{}, ErrEditInvalid
		}
		timelineRange, err := domain.NewTimeRange(cursor, excerpt.SourceRange.Duration)
		if err != nil {
			return EditOperationInput{}, ErrEditInvalid
		}
		ordinal := index + 1
		alignmentAs, err := roughCutLocal(localPrefix, "alignment", ordinal)
		if err != nil {
			return EditOperationInput{}, err
		}
		output := DerivedRoughCutOutputInput{
			SourceExcerptID: excerpt.ID, SourceRange: excerpt.SourceRange,
			TimelineRange: timelineRange, AlignmentAs: alignmentAs,
		}
		if item.Video != nil {
			clipAs, localErr := roughCutLocal(localPrefix, "video", ordinal)
			if localErr != nil {
				return EditOperationInput{}, localErr
			}
			output.Video = &DerivedRoughCutLaneOutputInput{
				ClipAs: clipAs, TrackID: item.Video.TrackID, SourceStreamID: item.Video.SourceStreamID,
			}
		}
		if item.Audio != nil {
			clipAs, localErr := roughCutLocal(localPrefix, "audio", ordinal)
			if localErr != nil {
				return EditOperationInput{}, localErr
			}
			output.Audio = &DerivedRoughCutLaneOutputInput{
				ClipAs: clipAs, TrackID: item.Audio.TrackID, SourceStreamID: item.Audio.SourceStreamID,
			}
		}
		if output.Video != nil && output.Audio != nil {
			groupAs, localErr := roughCutLocal(localPrefix, "group", ordinal)
			if localErr != nil {
				return EditOperationInput{}, localErr
			}
			output.LinkGroupAs = &groupAs
		}
		outputs = append(outputs, output)
		cursor, err = timelineRange.End()
		if err != nil {
			return EditOperationInput{}, ErrEditInvalid
		}
	}
	operation := EditOperationInput{
		Type: domain.EditDeriveRoughCut, RoughCutPolicy: &policy,
		RoughCutTimelineStart: &timelineStart, RoughCutLocalPrefix: &localPrefix,
		RoughCutItems: cloneRoughCutItems(items), DerivedRoughCut: outputs,
	}
	digest, err := roughCutOperationDigest(operation)
	if err != nil {
		return EditOperationInput{}, err
	}
	operation.RoughCutOutputDigest = &digest
	return operation, nil
}

func validateRoughCutLane(
	state EditNormalizationState,
	excerpt domain.SourceExcerptState,
	lane RoughCutLaneBindingInput,
	expectedTrack domain.TrackType,
) error {
	track := state.Tracks[lane.TrackID.String()]
	stream := state.SourceStreams[lane.SourceStreamID.String()]
	if track.ID.IsZero() || track.SequenceID != state.SequenceID || track.Type != expectedTrack ||
		stream.ID.IsZero() || stream.AssetID != excerpt.AssetID || !trackAcceptsStream(track, stream) {
		return ErrEditInvalid
	}
	if !sourceRangeWithin(excerpt.SourceRange, stream.Descriptor) {
		return ErrEditInvalid
	}
	return nil
}

func roughCutLocal(prefix domain.LocalID, kind string, ordinal int) (domain.LocalID, error) {
	value, err := domain.ParseLocalID(fmt.Sprintf("%s_%s_%03d", prefix, kind, ordinal))
	if err != nil {
		return "", ErrEditInvalid
	}
	return value, nil
}

func cloneRoughCutItems(source []RoughCutDerivationItemInput) []RoughCutDerivationItemInput {
	result := make([]RoughCutDerivationItemInput, len(source))
	for index, item := range source {
		result[index] = item
		if item.Video != nil {
			lane := *item.Video
			result[index].Video = &lane
		}
		if item.Audio != nil {
			lane := *item.Audio
			result[index].Audio = &lane
		}
	}
	return result
}

func roughCutOperationDigest(operation EditOperationInput) (domain.Digest, error) {
	_, digest, err := domain.CanonicalDigest("open-cut/rough-cut-derivation", domain.RoughCutDerivationSchema, struct {
		Policy        *domain.RoughCutDerivationPolicy `json:"policy"`
		TimelineStart *domain.RationalTime             `json:"timelineStart"`
		LocalPrefix   *domain.LocalID                  `json:"localPrefix"`
		Items         []RoughCutDerivationItemInput    `json:"items"`
		Outputs       []DerivedRoughCutOutputInput     `json:"outputs"`
	}{
		Policy: operation.RoughCutPolicy, TimelineStart: operation.RoughCutTimelineStart,
		LocalPrefix: operation.RoughCutLocalPrefix, Items: operation.RoughCutItems,
		Outputs: operation.DerivedRoughCut,
	})
	return digest, err
}

func (normalizer *editNormalizer) deriveRoughCut(operation EditOperationInput) error {
	expected, err := BuildRoughCutOperation(
		normalizer.input.State, *operation.RoughCutTimelineStart,
		*operation.RoughCutLocalPrefix, operation.RoughCutItems,
	)
	if err != nil {
		return err
	}
	suppliedDigest, err := roughCutOperationDigest(operation)
	if err != nil || operation.RoughCutOutputDigest == nil || expected.RoughCutOutputDigest == nil ||
		suppliedDigest != *operation.RoughCutOutputDigest || suppliedDigest != *expected.RoughCutOutputDigest {
		return ErrEditInvalid
	}
	zero, _ := domain.NewRationalTime(0, 1)
	for _, output := range expected.DerivedRoughCut {
		excerpt := normalizer.sourceExcerpts[output.SourceExcerptID.String()]
		var groupReference *EditReference
		if output.LinkGroupAs != nil {
			groupReference = &EditReference{Local: output.LinkGroupAs}
		}
		lanes := []*DerivedRoughCutLaneOutputInput{output.Video, output.Audio}
		for laneIndex, lane := range lanes {
			if lane == nil {
				continue
			}
			enabled := true
			clipOperation := EditOperationInput{
				Type: domain.EditAddClip, CreateAs: &lane.ClipAs, TrackID: &lane.TrackID,
				AssetID: &excerpt.AssetID, SourceStreamID: &lane.SourceStreamID,
				SourceRange: &output.SourceRange, TimelineRange: &output.TimelineRange, Enabled: &enabled,
			}
			if groupReference != nil {
				if laneIndex == 0 {
					clipOperation.CreateLinkGroupAs = output.LinkGroupAs
				} else {
					clipOperation.LinkGroup = groupReference
				}
			}
			if err := normalizer.addClip(clipOperation); err != nil {
				return err
			}
		}
		targets := make([]AlignmentTargetInput, 0, 2)
		for _, lane := range lanes {
			if lane == nil {
				continue
			}
			localRange, rangeErr := domain.NewTimeRange(zero, output.TimelineRange.Duration)
			if rangeErr != nil {
				return ErrEditInvalid
			}
			targets = append(targets, AlignmentTargetInput{
				Type: domain.AlignmentTargetClip, Clip: &EditReference{Local: &lane.ClipAs}, LocalRange: &localRange,
			})
		}
		if err := normalizer.bindAlignment(EditOperationInput{
			Type: domain.EditBindAlignment, CreateAs: &output.AlignmentAs,
			NarrativeNode: &EditReference{ID: output.SourceExcerptID.String()}, AlignmentTargets: targets,
		}); err != nil {
			return err
		}
	}
	return nil
}
