package application

import (
	"context"

	"github.com/PerishCode/open-cut/product/domain"
)

type CreatorClipPlacementLaneInput struct {
	TrackID        domain.TrackID        `json:"trackId"`
	TrackRevision  domain.Revision       `json:"trackRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
}

type CreatorClipPlacementPreviewInput struct {
	AssetID             domain.AssetID                 `json:"assetId"`
	AssetRevision       domain.Revision                `json:"assetRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	AcceptedFingerprint domain.Digest                  `json:"acceptedFingerprint" format:"sha256-digest"`
	SourceRange         domain.TimeRange               `json:"sourceRange"`
	TimelineStart       domain.RationalTime            `json:"timelineStart"`
	LocalPrefix         domain.LocalID                 `json:"localPrefix" pattern:"^[a-z][a-z0-9_-]{0,47}$"`
	Video               *CreatorClipPlacementLaneInput `json:"video,omitempty"`
	Audio               *CreatorClipPlacementLaneInput `json:"audio,omitempty"`
}

type CreatorClipPlacementPreviewQuery struct {
	ProjectID  domain.ProjectID
	SequenceID domain.SequenceID
	Actor      domain.ActorRef
	Input      CreatorClipPlacementPreviewInput
}

type CreatorClipPlacementLane struct {
	Type           domain.TrackType      `json:"type" enum:"video,audio"`
	TrackID        domain.TrackID        `json:"trackId"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
}

type CreatorClipPlacementPreview struct {
	BaseProjectRevision domain.Revision             `json:"baseProjectRevision"`
	Preconditions       []domain.EntityPrecondition `json:"preconditions" maxItems:"4" nullable:"false"`
	Operations          []EditOperationInput        `json:"operations" minItems:"1" maxItems:"2" nullable:"false"`
	AssetID             domain.AssetID              `json:"assetId"`
	AssetRevision       domain.Revision             `json:"assetRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	AcceptedFingerprint domain.Digest               `json:"acceptedFingerprint" format:"sha256-digest"`
	SourceRange         domain.TimeRange            `json:"sourceRange"`
	TimelineRange       domain.TimeRange            `json:"timelineRange"`
	Lanes               []CreatorClipPlacementLane  `json:"lanes" minItems:"1" maxItems:"2" nullable:"false"`
	Linked              bool                        `json:"linked"`
	OutputDigest        domain.Digest               `json:"outputDigest" format:"sha256-digest"`
	ActivityCursor      domain.Cursor               `json:"activityCursor"`
}

func (reads *EditReads) ClipPlacementForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input CreatorClipPlacementPreviewInput,
) (CreatorClipPlacementPreview, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return CreatorClipPlacementPreview{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || !validCreatorClipPlacement(input) {
		return CreatorClipPlacementPreview{}, ErrEditInvalid
	}
	return reads.repository.ReadCreatorClipPlacementPreview(ctx, CreatorClipPlacementPreviewQuery{
		ProjectID: projectID, SequenceID: sequenceID, Actor: authority.Actor, Input: input,
	})
}

func validCreatorClipPlacement(input CreatorClipPlacementPreviewInput) bool {
	if input.AssetID.IsZero() || input.AssetRevision.Value() < 1 ||
		input.SourceRange.Start.Validate() != nil || input.SourceRange.Duration.Validate() != nil ||
		!input.SourceRange.Duration.IsPositive() || input.TimelineStart.Validate() != nil ||
		input.TimelineStart.IsNegative() || len(input.LocalPrefix.String()) > 48 ||
		(input.Video == nil && input.Audio == nil) {
		return false
	}
	if _, err := input.SourceRange.End(); err != nil {
		return false
	}
	if _, err := domain.ParseDigest(input.AcceptedFingerprint.String()); err != nil {
		return false
	}
	if _, err := domain.ParseLocalID(input.LocalPrefix.String()); err != nil {
		return false
	}
	validLane := func(lane *CreatorClipPlacementLaneInput) bool {
		return lane == nil || (!lane.TrackID.IsZero() && lane.TrackRevision.Value() > 0 &&
			!lane.SourceStreamID.IsZero())
	}
	if !validLane(input.Video) || !validLane(input.Audio) {
		return false
	}
	return input.Video == nil || input.Audio == nil ||
		(input.Video.TrackID != input.Audio.TrackID && input.Video.SourceStreamID != input.Audio.SourceStreamID)
}
