package application

import "github.com/PerishCode/open-cut/product/domain"

func isClipMutationOperation(value domain.EditOperationType) bool {
	switch value {
	case domain.EditMoveClip, domain.EditTrimClip, domain.EditSplitClip, domain.EditRemoveClip,
		domain.EditLinkClips, domain.EditUnlinkClips:
		return true
	default:
		return false
	}
}

func noNewClipMutationFields(operation EditOperationInput) bool {
	return operation.LeftLinkGroupAs == nil && operation.RightLinkGroupAs == nil &&
		len(operation.Clips) == 0 && operation.Scope == nil && operation.TimelineStart == nil &&
		operation.SplitAt == nil && len(operation.SplitOutputs) == 0
}

func validateClipMutationOperation(operation EditOperationInput) error {
	validScope := operation.Scope != nil &&
		(*operation.Scope == domain.ClipScopeLinked || *operation.Scope == domain.ClipScopeSingle)
	validTime := func(value *domain.RationalTime) bool {
		return value != nil && value.Validate() == nil && !value.IsNegative()
	}
	validRange := func(value *domain.TimeRange) bool {
		if value == nil || value.Start.Validate() != nil || value.Start.IsNegative() ||
			value.Duration.Validate() != nil || !value.Duration.IsPositive() {
			return false
		}
		_, err := value.End()
		return err == nil
	}
	validSourceRange := func(value *domain.TimeRange) bool {
		if value == nil || value.Start.Validate() != nil || value.Duration.Validate() != nil ||
			!value.Duration.IsPositive() {
			return false
		}
		_, err := value.End()
		return err == nil
	}
	baseClear := operation.CreateAs == nil && operation.NodeID == nil && operation.ParentID == nil &&
		operation.After == nil && operation.Title == nil && operation.Text == nil && operation.Description == nil &&
		operation.AuthoredTextPurpose == nil && operation.VisualIntentPurpose == nil &&
		operation.CaptionID == nil && operation.Range == nil &&
		operation.Language == nil && operation.AlignmentID == nil && operation.NarrativeNode == nil &&
		len(operation.AlignmentTargets) == 0 && operation.AssetID == nil && operation.SourceStreamID == nil &&
		operation.Enabled == nil && operation.TranscriptCorrectionID == nil && operation.TranscriptArtifactID == nil &&
		len(operation.TranscriptSegmentIDs) == 0 && len(operation.CorrectionRevisions) == 0 &&
		operation.AcceptedFingerprint == nil && operation.CaptionPolicy == nil && len(operation.DerivedCaptions) == 0 &&
		noRoughCutFields(operation)
	if !baseClear {
		return ErrEditInvalid
	}
	switch operation.Type {
	case domain.EditMoveClip:
		if !validExistingReference(operation.Clip) || !validScope || operation.TrackID == nil ||
			!validTime(operation.TimelineStart) || operation.SourceRange != nil || operation.TimelineRange != nil ||
			operation.CreateLinkGroupAs != nil || operation.LeftLinkGroupAs != nil || operation.RightLinkGroupAs != nil ||
			operation.LinkGroup != nil || len(operation.Clips) != 0 || operation.SplitAt != nil ||
			len(operation.SplitOutputs) != 0 {
			return ErrEditInvalid
		}
	case domain.EditTrimClip:
		if !validExistingReference(operation.Clip) || !validScope || !validSourceRange(operation.SourceRange) ||
			!validRange(operation.TimelineRange) || operation.TrackID != nil || operation.TimelineStart != nil ||
			operation.CreateLinkGroupAs != nil || operation.LeftLinkGroupAs != nil || operation.RightLinkGroupAs != nil ||
			operation.LinkGroup != nil || len(operation.Clips) != 0 || operation.SplitAt != nil ||
			len(operation.SplitOutputs) != 0 {
			return ErrEditInvalid
		}
	case domain.EditSplitClip:
		if !validExistingReference(operation.Clip) || !validScope || !validTime(operation.SplitAt) ||
			operation.TrackID != nil || operation.SourceRange != nil || operation.TimelineRange != nil ||
			operation.TimelineStart != nil || operation.CreateLinkGroupAs != nil || operation.LinkGroup != nil ||
			len(operation.Clips) != 0 || !validSplitOutputs(operation.SplitOutputs) {
			return ErrEditInvalid
		}
		if *operation.Scope == domain.ClipScopeSingle {
			if len(operation.SplitOutputs) != 1 || operation.LeftLinkGroupAs != nil || operation.RightLinkGroupAs != nil {
				return ErrEditInvalid
			}
		} else if len(operation.SplitOutputs) < 2 || operation.LeftLinkGroupAs == nil || operation.RightLinkGroupAs == nil ||
			operation.LeftLinkGroupAs.String() == operation.RightLinkGroupAs.String() {
			return ErrEditInvalid
		}
	case domain.EditRemoveClip:
		if !validExistingReference(operation.Clip) || !validScope || operation.TrackID != nil ||
			operation.SourceRange != nil || operation.TimelineRange != nil || operation.TimelineStart != nil ||
			operation.SplitAt != nil || len(operation.SplitOutputs) != 0 || operation.CreateLinkGroupAs != nil ||
			operation.LeftLinkGroupAs != nil || operation.RightLinkGroupAs != nil || operation.LinkGroup != nil ||
			len(operation.Clips) != 0 {
			return ErrEditInvalid
		}
	case domain.EditLinkClips:
		if operation.Clip != nil || operation.Scope != nil || operation.TrackID != nil || operation.SourceRange != nil ||
			operation.TimelineRange != nil || operation.TimelineStart != nil || operation.SplitAt != nil ||
			len(operation.SplitOutputs) != 0 || operation.CreateLinkGroupAs == nil ||
			operation.LeftLinkGroupAs != nil || operation.RightLinkGroupAs != nil || operation.LinkGroup != nil ||
			!validExistingReferences(operation.Clips, 2, 64) {
			return ErrEditInvalid
		}
	case domain.EditUnlinkClips:
		if operation.Clip != nil || operation.Scope != nil || operation.TrackID != nil || operation.SourceRange != nil ||
			operation.TimelineRange != nil || operation.TimelineStart != nil || operation.SplitAt != nil ||
			len(operation.SplitOutputs) != 0 || operation.CreateLinkGroupAs != nil ||
			operation.LeftLinkGroupAs != nil || operation.RightLinkGroupAs != nil ||
			!validExistingReference(operation.LinkGroup) || len(operation.Clips) != 0 {
			return ErrEditInvalid
		}
	default:
		return ErrEditInvalid
	}
	return nil
}

func validExistingReference(value *EditReference) bool {
	return value != nil && value.ID != "" && value.Local == nil
}

func validExistingReferences(values []EditReference, minimum, maximum int) bool {
	if len(values) < minimum || len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for index := range values {
		if !validExistingReference(&values[index]) {
			return false
		}
		if _, duplicate := seen[values[index].ID]; duplicate {
			return false
		}
		seen[values[index].ID] = struct{}{}
	}
	return true
}

func validSplitOutputs(values []ClipSplitOutputInput) bool {
	if len(values) == 0 || len(values) > 64 {
		return false
	}
	clips := make(map[string]struct{}, len(values))
	locals := make(map[string]struct{}, len(values)*2)
	for index := range values {
		value := &values[index]
		if !validExistingReference(&value.Clip) {
			return false
		}
		if _, duplicate := clips[value.Clip.ID]; duplicate {
			return false
		}
		clips[value.Clip.ID] = struct{}{}
		for _, local := range []domain.LocalID{value.LeftAs, value.RightAs} {
			if _, err := domain.ParseLocalID(local.String()); err != nil {
				return false
			}
			if _, duplicate := locals[local.String()]; duplicate {
				return false
			}
			locals[local.String()] = struct{}{}
		}
	}
	return true
}
