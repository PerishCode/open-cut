package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var ErrTranscriptSelectionConflict = errors.New("transcript default selection changed")

type SelectTranscriptDefaultInput struct {
	ArtifactID                domain.ArtifactID `json:"artifactId"`
	ExpectedDefaultArtifactID domain.ArtifactID `json:"expectedDefaultArtifactId"`
}

type SelectTranscriptDefaultRecord struct {
	ProjectID                 domain.ProjectID
	AssetID                   domain.AssetID
	ArtifactID                domain.ArtifactID
	ExpectedDefaultArtifactID domain.ArtifactID
	Actor                     domain.ActorRef
	ActivityEventID           domain.ActivityEventID
	SelectedAt                time.Time
}

type TranscriptDefaultSelection struct {
	AssetID            domain.AssetID    `json:"assetId"`
	ArtifactID         domain.ArtifactID `json:"artifactId"`
	PreviousArtifactID domain.ArtifactID `json:"previousArtifactId"`
	SelectedAt         time.Time         `json:"selectedAt"`
	ActivityCursor     domain.Cursor     `json:"activityCursor" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Replayed           bool              `json:"replayed"`
}

type TranscriptSelectionRepository interface {
	SelectTranscriptDefault(context.Context, SelectTranscriptDefaultRecord) (TranscriptDefaultSelection, error)
}

func (media *Media) SelectTranscriptDefault(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	input SelectTranscriptDefaultInput,
) (TranscriptDefaultSelection, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return TranscriptDefaultSelection{}, err
	}
	if projectID.IsZero() || assetID.IsZero() || input.ArtifactID.IsZero() || input.ExpectedDefaultArtifactID.IsZero() {
		return TranscriptDefaultSelection{}, ErrTranscriptReadInvalid
	}
	now := media.clock.Now().UTC()
	value, err := media.identities.NewID(ctx, now)
	if err != nil {
		return TranscriptDefaultSelection{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	eventID, err := domain.ParseActivityEventID(value)
	if err != nil {
		return TranscriptDefaultSelection{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	return media.repository.SelectTranscriptDefault(ctx, SelectTranscriptDefaultRecord{
		ProjectID: projectID, AssetID: assetID, ArtifactID: input.ArtifactID,
		ExpectedDefaultArtifactID: input.ExpectedDefaultArtifactID,
		Actor:                     authority.Actor, ActivityEventID: eventID, SelectedAt: now,
	})
}
