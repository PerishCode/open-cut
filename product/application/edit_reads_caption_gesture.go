package application

import (
	"context"

	"github.com/PerishCode/open-cut/product/domain"
)

type CreatorCaptionGestureKind string

const (
	CreatorCaptionCreate CreatorCaptionGestureKind = "create"
	CreatorCaptionUpdate CreatorCaptionGestureKind = "update"
	CreatorCaptionRemove CreatorCaptionGestureKind = "remove"
)

type CreatorCaptionAlignmentHandling string

const (
	CreatorCaptionPreserveAlignment CreatorCaptionAlignmentHandling = "preserve-if-provable"
	CreatorCaptionStaleAlignment    CreatorCaptionAlignmentHandling = "mark-stale"
	CreatorCaptionUnbindAlignment   CreatorCaptionAlignmentHandling = "unbind"
)

type CreatorCaptionGesturePreviewInput struct {
	Kind              CreatorCaptionGestureKind        `json:"kind" enum:"create,update,remove"`
	CaptionID         *domain.CaptionID                `json:"captionId,omitempty"`
	CaptionRevision   *domain.Revision                 `json:"captionRevision,omitempty" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	CaptionAs         *domain.LocalID                  `json:"captionAs,omitempty" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	TrackID           domain.TrackID                   `json:"trackId"`
	TrackRevision     domain.Revision                  `json:"trackRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Range             *domain.TimeRange                `json:"range,omitempty"`
	Language          *domain.CaptionLanguage          `json:"language,omitempty" maxLength:"64"`
	Text              *string                          `json:"text,omitempty" maxLength:"262144"`
	AlignmentHandling *CreatorCaptionAlignmentHandling `json:"alignmentHandling,omitempty" enum:"preserve-if-provable,mark-stale,unbind"`
}

type CreatorCaptionGesturePreviewQuery struct {
	ProjectID  domain.ProjectID
	SequenceID domain.SequenceID
	Actor      domain.ActorRef
	Input      CreatorCaptionGesturePreviewInput
}

type CreatorCaptionGestureSubject struct {
	CaptionID  *domain.CaptionID            `json:"captionId,omitempty"`
	CaptionAs  *domain.LocalID              `json:"captionAs,omitempty" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	TrackID    domain.TrackID               `json:"trackId"`
	Range      domain.TimeRange             `json:"range"`
	Language   domain.CaptionLanguage       `json:"language" maxLength:"64"`
	Text       string                       `json:"text" minLength:"1" maxLength:"262144"`
	Provenance domain.CaptionProvenanceKind `json:"provenance" enum:"manual,transcript-derivation"`
}

type CreatorCaptionAlignmentEffect struct {
	AlignmentID domain.AlignmentID              `json:"alignmentId"`
	Revision    domain.Revision                 `json:"revision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Handling    CreatorCaptionAlignmentHandling `json:"handling" enum:"preserve-if-provable,mark-stale,unbind"`
	TargetCount int                             `json:"targetCount" minimum:"1" maximum:"64"`
}

type CreatorCaptionGesturePreview struct {
	BaseProjectRevision domain.Revision                 `json:"baseProjectRevision"`
	Preconditions       []domain.EntityPrecondition     `json:"preconditions" maxItems:"2048" nullable:"false"`
	Operations          []EditOperationInput            `json:"operations" maxItems:"512" nullable:"false"`
	Kind                CreatorCaptionGestureKind       `json:"kind" enum:"create,update,remove"`
	Subject             CreatorCaptionGestureSubject    `json:"subject"`
	AlignmentEffects    []CreatorCaptionAlignmentEffect `json:"alignmentEffects" maxItems:"511" nullable:"false"`
	OutputDigest        domain.Digest                   `json:"outputDigest" format:"sha256-digest"`
	ActivityCursor      domain.Cursor                   `json:"activityCursor"`
}

func (reads *EditReads) CaptionGestureForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input CreatorCaptionGesturePreviewInput,
) (CreatorCaptionGesturePreview, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return CreatorCaptionGesturePreview{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || !validCreatorCaptionGesture(input) {
		return CreatorCaptionGesturePreview{}, ErrEditInvalid
	}
	return reads.repository.ReadCreatorCaptionGesturePreview(ctx, CreatorCaptionGesturePreviewQuery{
		ProjectID: projectID, SequenceID: sequenceID, Actor: authority.Actor, Input: input,
	})
}

func validCreatorCaptionGesture(input CreatorCaptionGesturePreviewInput) bool {
	if input.TrackID.IsZero() || input.TrackRevision.Value() < 1 {
		return false
	}
	validContent := func() bool {
		if input.Range == nil || input.Language == nil || input.Text == nil ||
			input.Range.Start.Validate() != nil || input.Range.Start.IsNegative() ||
			input.Range.Duration.Validate() != nil || !input.Range.Duration.IsPositive() ||
			input.Language.Validate() != nil || len(*input.Text) == 0 || len(*input.Text) > 262_144 {
			return false
		}
		_, err := input.Range.End()
		return err == nil
	}
	validExisting := input.CaptionID != nil && !input.CaptionID.IsZero() &&
		input.CaptionRevision != nil && input.CaptionRevision.Value() > 0 && input.CaptionAs == nil
	switch input.Kind {
	case CreatorCaptionCreate:
		return input.CaptionID == nil && input.CaptionRevision == nil && input.CaptionAs != nil &&
			input.CaptionAs.String() != "" && input.AlignmentHandling == nil && validContent()
	case CreatorCaptionUpdate:
		return validExisting && input.AlignmentHandling != nil && validCaptionAlignmentHandling(*input.AlignmentHandling) &&
			validContent()
	case CreatorCaptionRemove:
		return validExisting && input.Range == nil && input.Language == nil && input.Text == nil &&
			input.AlignmentHandling != nil &&
			(*input.AlignmentHandling == CreatorCaptionStaleAlignment ||
				*input.AlignmentHandling == CreatorCaptionUnbindAlignment)
	default:
		return false
	}
}

func validCaptionAlignmentHandling(value CreatorCaptionAlignmentHandling) bool {
	return value == CreatorCaptionPreserveAlignment || value == CreatorCaptionStaleAlignment ||
		value == CreatorCaptionUnbindAlignment
}
