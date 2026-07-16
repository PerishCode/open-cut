package application

import "github.com/PerishCode/open-cut/product/domain"

func noRoughCutFields(operation EditOperationInput) bool {
	return operation.RoughCutPolicy == nil && operation.RoughCutTimelineStart == nil &&
		operation.RoughCutLocalPrefix == nil && len(operation.RoughCutItems) == 0 &&
		len(operation.DerivedRoughCut) == 0 && operation.RoughCutOutputDigest == nil
}

func validateRoughCutOperation(operation EditOperationInput) error {
	baseClear := operation.CreateAs == nil && operation.NodeID == nil && operation.ParentID == nil &&
		operation.After == nil && operation.Title == nil && operation.Text == nil && operation.Description == nil &&
		operation.AuthoredTextPurpose == nil && operation.VisualIntentPurpose == nil &&
		operation.CaptionID == nil && operation.TrackID == nil &&
		operation.Range == nil && operation.Language == nil && operation.AlignmentID == nil &&
		operation.NarrativeNode == nil && len(operation.AlignmentTargets) == 0 && operation.AssetID == nil &&
		operation.SourceStreamID == nil && operation.SourceRange == nil && operation.TimelineRange == nil &&
		operation.Enabled == nil && operation.CreateLinkGroupAs == nil && operation.LeftLinkGroupAs == nil &&
		operation.RightLinkGroupAs == nil && operation.LinkGroup == nil && operation.Clip == nil &&
		len(operation.Clips) == 0 && operation.Scope == nil && operation.TimelineStart == nil &&
		operation.SplitAt == nil && len(operation.SplitOutputs) == 0 &&
		operation.TranscriptCorrectionID == nil && operation.TranscriptArtifactID == nil &&
		len(operation.TranscriptSegmentIDs) == 0 && len(operation.CorrectionRevisions) == 0 &&
		operation.AcceptedFingerprint == nil && operation.CaptionPolicy == nil && len(operation.DerivedCaptions) == 0
	if !baseClear || operation.RoughCutPolicy == nil || operation.RoughCutPolicy.Validate() != nil ||
		operation.RoughCutTimelineStart == nil || operation.RoughCutTimelineStart.Validate() != nil ||
		operation.RoughCutTimelineStart.IsNegative() || operation.RoughCutLocalPrefix == nil ||
		len(operation.RoughCutLocalPrefix.String()) > 40 || len(operation.RoughCutItems) == 0 ||
		len(operation.RoughCutItems) > 128 || len(operation.DerivedRoughCut) != len(operation.RoughCutItems) ||
		operation.RoughCutOutputDigest == nil {
		return ErrEditInvalid
	}
	if _, err := domain.ParseLocalID(operation.RoughCutLocalPrefix.String()); err != nil {
		return ErrEditInvalid
	}
	if _, err := domain.ParseDigest(operation.RoughCutOutputDigest.String()); err != nil {
		return ErrEditInvalid
	}
	for _, item := range operation.RoughCutItems {
		if item.SourceExcerptID.IsZero() || (item.Video == nil && item.Audio == nil) ||
			(item.Video != nil && !validRoughCutLaneInput(*item.Video)) ||
			(item.Audio != nil && !validRoughCutLaneInput(*item.Audio)) {
			return ErrEditInvalid
		}
	}
	for _, output := range operation.DerivedRoughCut {
		if output.SourceExcerptID.IsZero() || !validSourceTimeRange(output.SourceRange) ||
			!validTimelineTimeRange(output.TimelineRange) || (output.Video == nil && output.Audio == nil) ||
			(output.Video != nil && !validRoughCutLaneOutput(*output.Video)) ||
			(output.Audio != nil && !validRoughCutLaneOutput(*output.Audio)) {
			return ErrEditInvalid
		}
		if _, err := domain.ParseLocalID(output.AlignmentAs.String()); err != nil {
			return ErrEditInvalid
		}
		if (output.Video != nil && output.Audio != nil) != (output.LinkGroupAs != nil) {
			return ErrEditInvalid
		}
		if output.LinkGroupAs != nil {
			if _, err := domain.ParseLocalID(output.LinkGroupAs.String()); err != nil {
				return ErrEditInvalid
			}
		}
	}
	return nil
}

func validRoughCutLaneInput(value RoughCutLaneBindingInput) bool {
	return !value.TrackID.IsZero() && !value.SourceStreamID.IsZero()
}

func validRoughCutLaneOutput(value DerivedRoughCutLaneOutputInput) bool {
	if !validRoughCutLaneInput(RoughCutLaneBindingInput{
		TrackID: value.TrackID, SourceStreamID: value.SourceStreamID,
	}) {
		return false
	}
	_, err := domain.ParseLocalID(value.ClipAs.String())
	return err == nil
}

func validSourceTimeRange(value domain.TimeRange) bool {
	if value.Start.Validate() != nil || value.Duration.Validate() != nil || !value.Duration.IsPositive() {
		return false
	}
	_, err := value.End()
	return err == nil
}

func validTimelineTimeRange(value domain.TimeRange) bool {
	return !value.Start.IsNegative() && validSourceTimeRange(value)
}
