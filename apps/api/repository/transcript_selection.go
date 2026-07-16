package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) SelectTranscriptDefault(
	ctx context.Context,
	record application.SelectTranscriptDefaultRecord,
) (application.TranscriptDefaultSelection, error) {
	if record.ProjectID.IsZero() || record.AssetID.IsZero() || record.ArtifactID.IsZero() ||
		record.ExpectedDefaultArtifactID.IsZero() || record.Actor.Kind != domain.ActorCreator ||
		record.ActivityEventID.IsZero() || record.SelectedAt.IsZero() {
		return application.TranscriptDefaultSelection{}, application.ErrTranscriptReadInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.TranscriptDefaultSelection{}, err
	}
	defer tx.Rollback()
	var currentID, selectedAt string
	var projectRevision uint64
	err = tx.QueryRowContext(ctx, `
SELECT selection.artifact_id, selection.selected_at, project.revision
FROM asset_transcript_selection selection
JOIN assets asset ON asset.id = selection.asset_id
JOIN projects project ON project.id = asset.project_id
WHERE selection.asset_id = ? AND asset.project_id = ?`,
		record.AssetID.String(), record.ProjectID.String(),
	).Scan(&currentID, &selectedAt, &projectRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return application.TranscriptDefaultSelection{}, application.ErrTranscriptNotFound
	}
	if err != nil {
		return application.TranscriptDefaultSelection{}, err
	}
	current, err := domain.ParseArtifactID(currentID)
	if err != nil {
		return application.TranscriptDefaultSelection{}, application.ErrTranscriptReadInvalid
	}
	if current == record.ArtifactID {
		at, parseErr := time.Parse(time.RFC3339Nano, selectedAt)
		if parseErr != nil {
			return application.TranscriptDefaultSelection{}, parseErr
		}
		cursor, cursorErr := loadActivityHead(ctx, tx, "project", record.ProjectID.String())
		if cursorErr != nil {
			return application.TranscriptDefaultSelection{}, cursorErr
		}
		if err := tx.Commit(); err != nil {
			return application.TranscriptDefaultSelection{}, err
		}
		return application.TranscriptDefaultSelection{
			AssetID: record.AssetID, ArtifactID: current, PreviousArtifactID: current,
			SelectedAt: at.UTC(), ActivityCursor: cursor, Replayed: true,
		}, nil
	}
	if current != record.ExpectedDefaultArtifactID {
		return application.TranscriptDefaultSelection{}, application.ErrTranscriptSelectionConflict
	}
	var desiredID string
	if err := tx.QueryRowContext(ctx, `
SELECT artifact.id
FROM media_artifacts artifact
JOIN transcript_artifacts transcript ON transcript.artifact_id = artifact.id
WHERE artifact.id = ? AND artifact.project_id = ? AND artifact.asset_id = ?
  AND artifact.kind = 'transcript' AND artifact.state = 'ready'`,
		record.ArtifactID.String(), record.ProjectID.String(), record.AssetID.String(),
	).Scan(&desiredID); errors.Is(err, sql.ErrNoRows) {
		return application.TranscriptDefaultSelection{}, application.ErrTranscriptNotFound
	} else if err != nil {
		return application.TranscriptDefaultSelection{}, err
	}
	at := formatInstant(record.SelectedAt.UTC())
	result, err := tx.ExecContext(ctx, `
UPDATE asset_transcript_selection SET artifact_id = ?, selected_at = ?
WHERE asset_id = ? AND artifact_id = ?`,
		record.ArtifactID.String(), at, record.AssetID.String(), current.String())
	if err != nil {
		return application.TranscriptDefaultSelection{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.TranscriptDefaultSelection{}, application.ErrTranscriptSelectionConflict
	}
	revision, err := domain.NewRevision(projectRevision)
	if err != nil {
		return application.TranscriptDefaultSelection{}, err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs  []application.ChangedEntityRef `json:"changedEntityRefs"`
		PreviousArtifactID domain.ArtifactID              `json:"previousArtifactId"`
		ArtifactID         domain.ArtifactID              `json:"artifactId"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{{
			Kind: "asset-media-state", ID: record.AssetID.String(), Revision: revision,
		}},
		PreviousArtifactID: current, ArtifactID: record.ArtifactID,
	})
	if err != nil {
		return application.TranscriptDefaultSelection{}, err
	}
	cursor, err := appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.ProjectID.String(), EventID: record.ActivityEventID.String(),
		Kind: "media.transcript-default-selected", OccurredAt: at,
		ActorKind: string(record.Actor.Kind), ActorID: record.Actor.IDString(),
		ProjectID: record.ProjectID.String(), ProjectRevision: int64(revision.Value()),
		OutcomeKind: "transcript-artifact", OutcomeID: record.ArtifactID.String(),
		SummaryCode: "media-transcript-default-selected", Payload: payload,
	})
	if err != nil {
		return application.TranscriptDefaultSelection{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.TranscriptDefaultSelection{}, err
	}
	return application.TranscriptDefaultSelection{
		AssetID: record.AssetID, ArtifactID: record.ArtifactID, PreviousArtifactID: current,
		SelectedAt: record.SelectedAt.UTC(), ActivityCursor: cursor,
	}, nil
}
