package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func applyTranscriptCorrection(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	state domain.TranscriptCorrectionState,
	change domain.EntityRevisionChange,
) error {
	startKey, endKey, err := sourceOrderKeys(state.SourceRange)
	if err != nil || len(state.SegmentIDs) == 0 || len(state.SegmentIDs) > 256 {
		return application.ErrEditInvalid
	}
	if change.Before == nil {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO transcript_corrections (
  id, project_id, asset_id, artifact_id, revision,
  source_start_value, source_start_scale, source_duration_value,
  source_duration_scale, source_start_order_key, source_end_order_key,
  replacement_text, language, tombstoned, last_transaction_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			state.ID.String(), projectID.String(), state.AssetID.String(), state.ArtifactID.String(),
			state.Revision.Value(), state.SourceRange.Start.Value.Value(), state.SourceRange.Start.Scale,
			state.SourceRange.Duration.Value.Value(), state.SourceRange.Duration.Scale,
			startKey, endKey, state.ReplacementText, state.Language.String(), state.Tombstoned,
			transactionID.String()); err != nil {
			return err
		}
		for ordinal, segmentID := range state.SegmentIDs {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO transcript_correction_segments (correction_id, ordinal, segment_id)
VALUES (?, ?, ?)`, state.ID.String(), ordinal, segmentID.String()); err != nil {
				return err
			}
		}
		return nil
	}
	result, err := tx.ExecContext(ctx, `
UPDATE transcript_corrections
SET revision = ?, replacement_text = ?, language = ?, tombstoned = ?,
    last_transaction_id = ?
WHERE id = ? AND project_id = ? AND revision = ?`,
		state.Revision.Value(), state.ReplacementText, state.Language.String(), state.Tombstoned,
		transactionID.String(), state.ID.String(), projectID.String(), change.Before.Value())
	return requireOneEditRow(result, err)
}

func sourceOrderKeys(value domain.TimeRange) (string, string, error) {
	start, err := domain.RationalOrderKey(value.Start)
	if err != nil {
		return "", "", err
	}
	endValue, err := value.End()
	if err != nil {
		return "", "", err
	}
	end, err := domain.RationalOrderKey(endValue)
	return start, end, err
}
