package application

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
)

func validateEditProposeInput(input EditProposeInput) error {
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil {
		return editInvalidf("requestId is missing or malformed")
	}
	if validateEditIntent(input.Intent, false) != nil {
		return editInvalidf("intent is missing or exceeds its length bound")
	}
	if input.BaseProjectRevision.Value() < 1 {
		return editInvalidf("baseProjectRevision must be a positive revision")
	}
	if len(input.Operations) < 1 || len(input.Operations) > 512 {
		return editInvalidf("a proposal carries 1 to 512 operations, got %d", len(input.Operations))
	}
	if len(input.Preconditions) > 2048 {
		return editInvalidf("a proposal carries at most 2048 preconditions, got %d", len(input.Preconditions))
	}
	preconditions := make(map[string]struct{}, len(input.Preconditions))
	for _, precondition := range input.Preconditions {
		if precondition.Revision.Value() < 1 || validateEntityID(precondition.Kind, precondition.ID) != nil {
			return editInvalidf("precondition on %s %q is malformed", precondition.Kind, precondition.ID)
		}
		key := string(precondition.Kind) + "\x00" + precondition.ID
		if _, duplicate := preconditions[key]; duplicate {
			return editInvalidf("duplicate precondition on %s %q", precondition.Kind, precondition.ID)
		}
		preconditions[key] = struct{}{}
	}
	created := make(map[string]domain.EditEntityKind)
	afterGraph := make(map[string]string)
	for index, operation := range input.Operations {
		if err := validateEditOperation(operation); err != nil {
			return editInvalidf("operation %d (%s) is malformed", index, operation.Type)
		}
		if operation.CreateAs != nil {
			local := operation.CreateAs.String()
			if _, err := domain.ParseLocalID(local); err != nil {
				return ErrEditInvalid
			}
			if _, duplicate := created[local]; duplicate {
				return ErrEditInvalid
			}
			created[local] = createdKind(operation.Type)
			if createsNarrativeNode(operation.Type) && operation.After != nil && operation.After.Local != nil {
				afterGraph[local] = operation.After.Local.String()
			}
		}
		if operation.CreateLinkGroupAs != nil {
			local := operation.CreateLinkGroupAs.String()
			if _, err := domain.ParseLocalID(local); err != nil {
				return ErrEditInvalid
			}
			if _, duplicate := created[local]; duplicate {
				return ErrEditInvalid
			}
			created[local] = domain.EntityLinkGroup
		}
		for _, localValue := range []*domain.LocalID{operation.LeftLinkGroupAs, operation.RightLinkGroupAs} {
			if localValue == nil {
				continue
			}
			local := localValue.String()
			if _, err := domain.ParseLocalID(local); err != nil {
				return ErrEditInvalid
			}
			if _, duplicate := created[local]; duplicate {
				return ErrEditInvalid
			}
			created[local] = domain.EntityLinkGroup
		}
		for _, output := range operation.SplitOutputs {
			for _, localValue := range []domain.LocalID{output.LeftAs, output.RightAs} {
				local := localValue.String()
				if _, duplicate := created[local]; duplicate {
					return ErrEditInvalid
				}
				created[local] = domain.EntityClip
			}
		}
		for _, output := range operation.DerivedCaptions {
			for local, kind := range map[domain.LocalID]domain.EditEntityKind{
				output.CaptionAs: domain.EntityCaption, output.AlignmentAs: domain.EntityAlignment,
			} {
				value := local.String()
				if _, err := domain.ParseLocalID(value); err != nil {
					return ErrEditInvalid
				}
				if _, duplicate := created[value]; duplicate {
					return ErrEditInvalid
				}
				created[value] = kind
			}
		}
		for _, output := range operation.DerivedRoughCut {
			locals := map[domain.LocalID]domain.EditEntityKind{
				output.AlignmentAs: domain.EntityAlignment,
			}
			if output.Video != nil {
				locals[output.Video.ClipAs] = domain.EntityClip
			}
			if output.Audio != nil {
				locals[output.Audio.ClipAs] = domain.EntityClip
			}
			if output.LinkGroupAs != nil {
				locals[*output.LinkGroupAs] = domain.EntityLinkGroup
			}
			for local, kind := range locals {
				value := local.String()
				if _, err := domain.ParseLocalID(value); err != nil {
					return ErrEditInvalid
				}
				if _, duplicate := created[value]; duplicate {
					return ErrEditInvalid
				}
				created[value] = kind
			}
		}
	}
	if len(created) > 1024 {
		return ErrEditInvalid
	}
	for index, operation := range input.Operations {
		if err := validateEditReferences(operation, created); err != nil {
			return editInvalidf("operation %d (%s) references an unknown or mistyped local id", index, operation.Type)
		}
	}
	if hasLocalReferenceCycle(afterGraph) {
		return editInvalidf("operations form an `after` reference cycle")
	}
	return nil
}

func validateEditOperation(operation EditOperationInput) error {
	if operation.Type == domain.EditDeriveRoughCut {
		return validateRoughCutOperation(operation)
	}
	if !noRoughCutFields(operation) {
		return ErrEditInvalid
	}
	if isClipMutationOperation(operation.Type) {
		return validateClipMutationOperation(operation)
	}
	if !noNewClipMutationFields(operation) {
		return ErrEditInvalid
	}
	validText := func(value *string) bool {
		return value != nil && utf8.ValidString(*value) && len(*value) > 0 && len([]byte(*value)) <= domain.MaximumAuthoredTextBytes
	}
	validCorrectionText := func(value *string) bool {
		return validText(value) && strings.TrimSpace(*value) == *value
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
		if value == nil || value.Start.Validate() != nil ||
			value.Duration.Validate() != nil || !value.Duration.IsPositive() {
			return false
		}
		_, err := value.End()
		return err == nil
	}
	validLanguage := func(value *domain.CaptionLanguage) bool {
		return value != nil && value.Validate() == nil
	}
	validAuthoredPurpose := func(value *domain.AuthoredTextPurpose) bool {
		return value != nil && value.Validate() == nil
	}
	validVisualPurpose := func(value *domain.VisualIntentPurpose) bool {
		return value != nil && value.Validate() == nil
	}
	writingOperation := operation.Type == domain.EditInsertSection || operation.Type == domain.EditUpdateSection ||
		operation.Type == domain.EditInsertAuthoredText || operation.Type == domain.EditUpdateAuthoredText ||
		operation.Type == domain.EditInsertVisualIntent || operation.Type == domain.EditUpdateVisualIntent ||
		operation.Type == domain.EditInsertNote || operation.Type == domain.EditUpdateNote
	if !writingOperation && (operation.Title != nil || operation.Description != nil ||
		operation.AuthoredTextPurpose != nil || operation.VisualIntentPurpose != nil) {
		return ErrEditInvalid
	}
	noCreate := operation.CreateAs == nil
	noNode := operation.NodeID == nil && operation.ParentID == nil && operation.After == nil
	noCaption := operation.CaptionID == nil && operation.TrackID == nil && operation.Range == nil && operation.Language == nil
	noAlignment := operation.AlignmentID == nil && operation.NarrativeNode == nil && len(operation.AlignmentTargets) == 0
	noClip := operation.AssetID == nil && operation.SourceStreamID == nil && operation.SourceRange == nil &&
		operation.TimelineRange == nil && operation.Enabled == nil && operation.CreateLinkGroupAs == nil && operation.LinkGroup == nil
	noTranscript := operation.TranscriptCorrectionID == nil && operation.TranscriptArtifactID == nil &&
		len(operation.TranscriptSegmentIDs) == 0 && len(operation.CorrectionRevisions) == 0 &&
		operation.AcceptedFingerprint == nil && operation.Clip == nil && operation.CaptionPolicy == nil &&
		len(operation.DerivedCaptions) == 0
	switch operation.Type {
	case domain.EditInsertSection:
		if operation.CreateAs == nil || operation.ParentID == nil || operation.NodeID != nil ||
			!validText(operation.Title) || !validLanguage(operation.Language) || operation.Text != nil ||
			operation.Description != nil || operation.AuthoredTextPurpose != nil || operation.VisualIntentPurpose != nil ||
			!noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditUpdateSection:
		if !noCreate || operation.NodeID == nil || operation.ParentID != nil || operation.After != nil ||
			!validText(operation.Title) || !validLanguage(operation.Language) || operation.Text != nil ||
			operation.Description != nil || operation.AuthoredTextPurpose != nil || operation.VisualIntentPurpose != nil ||
			!noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditInsertAuthoredText:
		if operation.CreateAs == nil || operation.ParentID == nil || !validText(operation.Text) ||
			!validLanguage(operation.Language) || !validAuthoredPurpose(operation.AuthoredTextPurpose) ||
			operation.NodeID != nil || operation.Title != nil || operation.Description != nil ||
			operation.VisualIntentPurpose != nil || !noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditUpdateAuthoredText:
		if !noCreate || operation.NodeID == nil || !validText(operation.Text) || operation.ParentID != nil ||
			operation.After != nil || !validLanguage(operation.Language) ||
			!validAuthoredPurpose(operation.AuthoredTextPurpose) || operation.Title != nil ||
			operation.Description != nil || operation.VisualIntentPurpose != nil || !noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditInsertVisualIntent:
		if operation.CreateAs == nil || operation.ParentID == nil || operation.NodeID != nil ||
			!validText(operation.Description) || !validLanguage(operation.Language) ||
			!validVisualPurpose(operation.VisualIntentPurpose) || operation.Title != nil || operation.Text != nil ||
			operation.AuthoredTextPurpose != nil || !noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditUpdateVisualIntent:
		if !noCreate || operation.NodeID == nil || operation.ParentID != nil || operation.After != nil ||
			!validText(operation.Description) || !validLanguage(operation.Language) ||
			!validVisualPurpose(operation.VisualIntentPurpose) || operation.Title != nil || operation.Text != nil ||
			operation.AuthoredTextPurpose != nil || !noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditInsertNote:
		if operation.CreateAs == nil || operation.ParentID == nil || operation.NodeID != nil ||
			!validText(operation.Text) || !validLanguage(operation.Language) || operation.Title != nil ||
			operation.Description != nil || operation.AuthoredTextPurpose != nil || operation.VisualIntentPurpose != nil ||
			!noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditUpdateNote:
		if !noCreate || operation.NodeID == nil || operation.ParentID != nil || operation.After != nil ||
			!validText(operation.Text) || !validLanguage(operation.Language) || operation.Title != nil ||
			operation.Description != nil || operation.AuthoredTextPurpose != nil || operation.VisualIntentPurpose != nil ||
			!noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditMoveNarrativeNode:
		if !noCreate || operation.NodeID == nil || operation.ParentID == nil || operation.Title != nil ||
			operation.Text != nil || operation.Description != nil || operation.AuthoredTextPurpose != nil ||
			operation.VisualIntentPurpose != nil || operation.Language != nil || !noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditRemoveNarrativeNode:
		if !noCreate || operation.NodeID == nil || operation.ParentID != nil || operation.After != nil ||
			operation.Title != nil || operation.Text != nil || operation.Description != nil ||
			operation.AuthoredTextPurpose != nil || operation.VisualIntentPurpose != nil || operation.Language != nil ||
			!noNarrativeExternalFields(operation) {
			return ErrEditInvalid
		}
	case domain.EditAddCaption:
		if operation.CreateAs == nil || operation.TrackID == nil || !validRange(operation.Range) ||
			!validLanguage(operation.Language) || !validText(operation.Text) || operation.CaptionID != nil ||
			!noNode || !noAlignment || !noClip || !noTranscript {
			return ErrEditInvalid
		}
	case domain.EditUpdateCaption:
		if !noCreate || operation.CaptionID == nil || operation.TrackID != nil || !validRange(operation.Range) ||
			!validLanguage(operation.Language) || !validText(operation.Text) || !noNode || !noAlignment || !noClip || !noTranscript {
			return ErrEditInvalid
		}
	case domain.EditRemoveCaption:
		if !noCreate || operation.CaptionID == nil || operation.TrackID != nil || operation.Range != nil ||
			operation.Language != nil || operation.Text != nil || !noNode || !noAlignment || !noClip || !noTranscript {
			return ErrEditInvalid
		}
	case domain.EditBindAlignment:
		if operation.CreateAs == nil || operation.NarrativeNode == nil ||
			!validAlignmentTargetInputs(operation.AlignmentTargets, validRange) || !noNode || !noCaption ||
			operation.AlignmentID != nil || operation.Text != nil || !noClip || !noTranscript {
			return ErrEditInvalid
		}
	case domain.EditRemapAlignment:
		if !noCreate || operation.AlignmentID == nil ||
			!validAlignmentTargetInputs(operation.AlignmentTargets, validRange) || !noNode || !noCaption ||
			operation.NarrativeNode != nil || operation.Text != nil || !noClip || !noTranscript {
			return ErrEditInvalid
		}
	case domain.EditMarkAlignmentStale, domain.EditUnbindAlignment:
		if !noCreate || operation.AlignmentID == nil || !noNode || !noCaption ||
			operation.NarrativeNode != nil || len(operation.AlignmentTargets) != 0 || operation.Text != nil || !noClip || !noTranscript {
			return ErrEditInvalid
		}
	case domain.EditAddClip:
		if operation.CreateAs == nil || operation.TrackID == nil || operation.AssetID == nil ||
			operation.SourceStreamID == nil || !validSourceRange(operation.SourceRange) ||
			!validRange(operation.TimelineRange) || operation.Enabled == nil || operation.CaptionID != nil ||
			operation.Range != nil || operation.Language != nil || operation.Text != nil || !noNode || !noAlignment ||
			(operation.CreateLinkGroupAs != nil && operation.LinkGroup != nil) || !noTranscript {
			return ErrEditInvalid
		}
	case domain.EditAddTranscriptCorrection:
		if operation.CreateAs == nil || operation.TranscriptCorrectionID != nil ||
			operation.TranscriptArtifactID == nil || operation.AssetID == nil ||
			!validSourceRange(operation.SourceRange) || !validTranscriptSegmentIDs(operation.TranscriptSegmentIDs) ||
			!validLanguage(operation.Language) || !validCorrectionText(operation.Text) ||
			operation.AcceptedFingerprint != nil || len(operation.CorrectionRevisions) != 0 ||
			operation.NodeID != nil || operation.ParentID != nil || operation.After != nil ||
			operation.CaptionID != nil || operation.TrackID != nil || operation.Range != nil ||
			!noAlignment || operation.SourceStreamID != nil || operation.TimelineRange != nil ||
			operation.Enabled != nil || operation.CreateLinkGroupAs != nil || operation.LinkGroup != nil ||
			operation.Clip != nil || operation.CaptionPolicy != nil || len(operation.DerivedCaptions) != 0 {
			return ErrEditInvalid
		}
	case domain.EditUpdateTranscriptCorrection:
		if !noCreate || operation.TranscriptCorrectionID == nil || !validLanguage(operation.Language) ||
			!validCorrectionText(operation.Text) || operation.TranscriptArtifactID != nil ||
			len(operation.TranscriptSegmentIDs) != 0 || len(operation.CorrectionRevisions) != 0 ||
			operation.AcceptedFingerprint != nil || operation.Clip != nil || operation.CaptionPolicy != nil ||
			len(operation.DerivedCaptions) != 0 || !noNode || !noCaption || !noAlignment || !noClip {
			return ErrEditInvalid
		}
	case domain.EditRemoveTranscriptCorrection:
		if !noCreate || operation.TranscriptCorrectionID == nil || operation.TranscriptArtifactID != nil ||
			len(operation.TranscriptSegmentIDs) != 0 || len(operation.CorrectionRevisions) != 0 ||
			operation.AcceptedFingerprint != nil || operation.Clip != nil || operation.CaptionPolicy != nil ||
			len(operation.DerivedCaptions) != 0 || operation.Text != nil || operation.Language != nil ||
			!noNode || !noCaption || !noAlignment || !noClip {
			return ErrEditInvalid
		}
	case domain.EditInsertSourceExcerpt:
		if operation.CreateAs == nil || operation.NodeID != nil || operation.ParentID == nil ||
			operation.AssetID == nil || operation.AcceptedFingerprint == nil ||
			operation.TranscriptArtifactID == nil || !validTranscriptSegmentIDs(operation.TranscriptSegmentIDs) ||
			!validCorrectionRefs(operation.CorrectionRevisions) || !validSourceRange(operation.SourceRange) ||
			!validLanguage(operation.Language) || operation.Text != nil || operation.TranscriptCorrectionID != nil ||
			operation.CaptionID != nil || operation.TrackID != nil || operation.Range != nil || !noAlignment ||
			operation.SourceStreamID != nil || operation.TimelineRange != nil || operation.Enabled != nil ||
			operation.CreateLinkGroupAs != nil || operation.LinkGroup != nil || operation.Clip != nil ||
			operation.CaptionPolicy != nil || len(operation.DerivedCaptions) != 0 ||
			!validDigestPointer(operation.AcceptedFingerprint) {
			return ErrEditInvalid
		}
	case domain.EditDeriveCaptions:
		if !noCreate || operation.NarrativeNode == nil || operation.NarrativeNode.ID == "" ||
			operation.Clip == nil || operation.Clip.ID == "" || operation.TrackID == nil ||
			operation.CaptionPolicy == nil || operation.CaptionPolicy.Validate() != nil ||
			!validDerivedCaptionOutputs(operation.DerivedCaptions, validSourceRange, validRange, validText) ||
			operation.NodeID != nil || operation.ParentID != nil || operation.After != nil || operation.Text != nil ||
			operation.CaptionID != nil || operation.Range != nil || operation.Language != nil ||
			operation.AlignmentID != nil || len(operation.AlignmentTargets) != 0 ||
			operation.AssetID != nil || operation.SourceStreamID != nil || operation.SourceRange != nil ||
			operation.TimelineRange != nil || operation.Enabled != nil || operation.CreateLinkGroupAs != nil ||
			operation.LinkGroup != nil || operation.TranscriptCorrectionID != nil ||
			operation.TranscriptArtifactID != nil || len(operation.TranscriptSegmentIDs) != 0 ||
			len(operation.CorrectionRevisions) != 0 || operation.AcceptedFingerprint != nil {
			return ErrEditInvalid
		}
	default:
		return ErrEditInvalid
	}
	return nil
}

func validateEditReferences(operation EditOperationInput, created map[string]domain.EditEntityKind) error {
	if operation.After != nil {
		if err := validateReference(*operation.After, domain.EntityNarrativeNode, created); err != nil {
			return err
		}
	}
	if operation.NarrativeNode != nil {
		if err := validateReference(*operation.NarrativeNode, domain.EntityNarrativeNode, created); err != nil {
			return err
		}
	}
	for _, target := range operation.AlignmentTargets {
		if target.Caption != nil {
			if err := validateReference(*target.Caption, domain.EntityCaption, created); err != nil {
				return err
			}
		}
		if target.Clip != nil {
			if err := validateReference(*target.Clip, domain.EntityClip, created); err != nil {
				return err
			}
		}
	}
	if operation.LinkGroup != nil {
		if err := validateReference(*operation.LinkGroup, domain.EntityLinkGroup, created); err != nil {
			return err
		}
	}
	if operation.Clip != nil {
		if err := validateReference(*operation.Clip, domain.EntityClip, created); err != nil {
			return err
		}
	}
	for index := range operation.Clips {
		if err := validateReference(operation.Clips[index], domain.EntityClip, created); err != nil {
			return err
		}
	}
	for index := range operation.SplitOutputs {
		if err := validateReference(operation.SplitOutputs[index].Clip, domain.EntityClip, created); err != nil {
			return err
		}
	}
	for _, correction := range operation.CorrectionRevisions {
		if err := validateReference(correction.Correction, domain.EntityTranscriptCorrection, created); err != nil {
			return err
		}
		if (correction.Correction.ID != "" && (correction.Revision == nil || correction.Revision.Value() < 1)) ||
			(correction.Correction.Local != nil && correction.Revision != nil) {
			return ErrEditInvalid
		}
	}
	return nil
}

func validAlignmentTargetInputs(
	values []AlignmentTargetInput,
	validRange func(*domain.TimeRange) bool,
) bool {
	if len(values) == 0 || len(values) > 64 {
		return false
	}
	targetType := values[0].Type
	for _, value := range values {
		if value.Type != targetType {
			return false
		}
		switch value.Type {
		case domain.AlignmentTargetCaption:
			if value.Caption == nil || value.Clip != nil || !validRange(value.LocalRange) ||
				value.TimelineRange != nil || value.SequenceRevision != nil {
				return false
			}
		case domain.AlignmentTargetClip:
			if value.Clip == nil || value.Caption != nil || !validRange(value.LocalRange) ||
				value.TimelineRange != nil || value.SequenceRevision != nil {
				return false
			}
		case domain.AlignmentTargetTimeline:
			if value.Caption != nil || value.Clip != nil || value.LocalRange != nil ||
				!validRange(value.TimelineRange) || value.SequenceRevision == nil || value.SequenceRevision.Value() < 1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validDerivedCaptionOutputs(
	values []DerivedCaptionOutputInput,
	validSourceRange func(*domain.TimeRange) bool,
	validTimelineRange func(*domain.TimeRange) bool,
	validText func(*string) bool,
) bool {
	if len(values) == 0 || len(values) > 128 {
		return false
	}
	seen := make(map[string]struct{}, len(values)*2)
	for index := range values {
		value := &values[index]
		text := value.Text
		if _, err := domain.ParseLocalID(value.CaptionAs.String()); err != nil {
			return false
		}
		if _, err := domain.ParseLocalID(value.AlignmentAs.String()); err != nil {
			return false
		}
		if _, duplicate := seen[value.CaptionAs.String()]; duplicate {
			return false
		}
		seen[value.CaptionAs.String()] = struct{}{}
		if _, duplicate := seen[value.AlignmentAs.String()]; duplicate {
			return false
		}
		seen[value.AlignmentAs.String()] = struct{}{}
		if !validSourceRange(&value.SourceRange) || !validTimelineRange(&value.TimelineRange) ||
			!validText(&text) || strings.TrimSpace(text) != text {
			return false
		}
	}
	return true
}

func validateReference(reference EditReference, expected domain.EditEntityKind, created map[string]domain.EditEntityKind) error {
	if (reference.ID == "") == (reference.Local == nil) {
		return ErrEditInvalid
	}
	if reference.ID != "" {
		return validateEntityID(expected, reference.ID)
	}
	local := reference.Local.String()
	if _, err := domain.ParseLocalID(local); err != nil || created[local] != expected {
		return ErrEditInvalid
	}
	return nil
}

func validateEntityID(kind domain.EditEntityKind, value string) error {
	var err error
	switch kind {
	case domain.EntityNarrativeDocument:
		_, err = domain.ParseNarrativeDocumentID(value)
	case domain.EntityNarrativeNode:
		_, err = domain.ParseNarrativeNodeID(value)
	case domain.EntitySequence:
		_, err = domain.ParseSequenceID(value)
	case domain.EntityTrack:
		_, err = domain.ParseTrackID(value)
	case domain.EntityCaption:
		_, err = domain.ParseCaptionID(value)
	case domain.EntityAlignment:
		_, err = domain.ParseAlignmentID(value)
	case domain.EntityClip:
		_, err = domain.ParseClipID(value)
	case domain.EntityLinkGroup:
		_, err = domain.ParseLinkGroupID(value)
	case domain.EntityAsset:
		_, err = domain.ParseAssetID(value)
	case domain.EntityTranscriptCorrection:
		_, err = domain.ParseTranscriptCorrectionID(value)
	default:
		return ErrEditInvalid
	}
	if err != nil {
		return ErrEditInvalid
	}
	return nil
}

func createdKind(operation domain.EditOperationType) domain.EditEntityKind {
	switch operation {
	case domain.EditInsertSection, domain.EditInsertAuthoredText, domain.EditInsertVisualIntent,
		domain.EditInsertNote, domain.EditInsertSourceExcerpt:
		return domain.EntityNarrativeNode
	case domain.EditAddCaption:
		return domain.EntityCaption
	case domain.EditBindAlignment:
		return domain.EntityAlignment
	case domain.EditAddClip:
		return domain.EntityClip
	case domain.EditAddTranscriptCorrection:
		return domain.EntityTranscriptCorrection
	default:
		return ""
	}
}

func createsNarrativeNode(operation domain.EditOperationType) bool {
	return createdKind(operation) == domain.EntityNarrativeNode
}

func noNarrativeExternalFields(operation EditOperationInput) bool {
	return operation.CaptionID == nil && operation.TrackID == nil && operation.Range == nil &&
		operation.AlignmentID == nil && operation.NarrativeNode == nil && len(operation.AlignmentTargets) == 0 &&
		operation.AssetID == nil && operation.SourceStreamID == nil && operation.SourceRange == nil &&
		operation.TimelineRange == nil && operation.Enabled == nil && operation.CreateLinkGroupAs == nil &&
		operation.LeftLinkGroupAs == nil && operation.RightLinkGroupAs == nil && operation.LinkGroup == nil &&
		operation.Clip == nil && len(operation.Clips) == 0 && operation.Scope == nil &&
		operation.TimelineStart == nil && operation.SplitAt == nil && len(operation.SplitOutputs) == 0 &&
		operation.TranscriptCorrectionID == nil && operation.TranscriptArtifactID == nil &&
		len(operation.TranscriptSegmentIDs) == 0 && len(operation.CorrectionRevisions) == 0 &&
		operation.AcceptedFingerprint == nil && operation.CaptionPolicy == nil &&
		len(operation.DerivedCaptions) == 0 && noRoughCutFields(operation)
}

func validTranscriptSegmentIDs(values []domain.TranscriptSegmentID) bool {
	if len(values) == 0 || len(values) > 256 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value.IsZero() {
			return false
		}
		if _, duplicate := seen[value.String()]; duplicate {
			return false
		}
		seen[value.String()] = struct{}{}
	}
	return true
}

func validCorrectionRefs(values []TranscriptCorrectionReferenceInput) bool {
	if len(values) > 256 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if (value.Correction.ID == "") == (value.Correction.Local == nil) {
			return false
		}
		key := value.Correction.ID
		if value.Correction.Local != nil {
			key = "local:" + value.Correction.Local.String()
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validDigestPointer(value *domain.Digest) bool {
	if value == nil {
		return false
	}
	_, err := domain.ParseDigest(value.String())
	return err == nil
}

func validateEditIntent(value string, optional bool) error {
	if value == "" && optional {
		return nil
	}
	if !utf8.ValidString(value) || len(value) < 1 || len([]byte(value)) > MaximumEditIntentBytes {
		return ErrEditInvalid
	}
	return nil
}

func hasLocalReferenceCycle(edges map[string]string) bool {
	const visiting = 1
	const visited = 2
	state := make(map[string]int, len(edges))
	var visit func(string) bool
	visit = func(node string) bool {
		if state[node] == visiting {
			return true
		}
		if state[node] == visited {
			return false
		}
		state[node] = visiting
		if next := edges[node]; next != "" && visit(next) {
			return true
		}
		state[node] = visited
		return false
	}
	for node := range edges {
		if visit(node) {
			return true
		}
	}
	return false
}

func editValidationError(format string, values ...any) error {
	return fmt.Errorf("%w: %s", ErrEditInvalid, fmt.Sprintf(format, values...))
}
