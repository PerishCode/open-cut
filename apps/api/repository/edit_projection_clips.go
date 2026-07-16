package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func applyLinkGroup(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	state domain.LinkGroupState,
	change domain.EntityRevisionChange,
) error {
	if change.Before == nil {
		_, err := tx.ExecContext(ctx, `
INSERT INTO clip_link_groups (
  id, project_id, sequence_id, revision, tombstoned, last_transaction_id
) VALUES (?, ?, ?, ?, ?, ?)`,
			state.ID.String(), projectID.String(), state.SequenceID.String(), state.Revision.Value(),
			state.Tombstoned, transactionID.String(),
		)
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE clip_link_groups
SET revision = ?, tombstoned = ?, last_transaction_id = ?
WHERE id = ? AND project_id = ? AND revision = ?`,
		state.Revision.Value(), state.Tombstoned, transactionID.String(), state.ID.String(),
		projectID.String(), change.Before.Value(),
	)
	return requireOneEditRow(result, err)
}

func applyClip(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	state domain.ClipState,
	change domain.EntityRevisionChange,
) error {
	startKey, endKey, err := clipOrderKeys(state.TimelineRange)
	if err != nil || state.TimelineRange.Start.IsNegative() ||
		!state.SourceRange.Duration.IsPositive() || !state.TimelineRange.Duration.IsPositive() {
		return application.ErrEditInvalid
	}
	equalDuration, err := state.SourceRange.Duration.Compare(state.TimelineRange.Duration)
	if err != nil || equalDuration != 0 {
		return application.ErrEditInvalid
	}
	var linkGroup any
	if state.LinkGroupID != nil {
		linkGroup = state.LinkGroupID.String()
	}
	if change.Before == nil {
		_, err := tx.ExecContext(ctx, `
INSERT INTO clips (
  id, project_id, sequence_id, track_id, asset_id, source_stream_id, revision,
  source_start_value, source_start_scale, source_duration_value, source_duration_scale,
  timeline_start_value, timeline_start_scale, timeline_duration_value, timeline_duration_scale,
  timeline_start_order_key, timeline_end_order_key, enabled, link_group_id, tombstoned,
  last_transaction_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			state.ID.String(), projectID.String(), state.SequenceID.String(), state.TrackID.String(),
			state.AssetID.String(), state.SourceStreamID.String(), state.Revision.Value(),
			state.SourceRange.Start.Value.Value(), state.SourceRange.Start.Scale,
			state.SourceRange.Duration.Value.Value(), state.SourceRange.Duration.Scale,
			state.TimelineRange.Start.Value.Value(), state.TimelineRange.Start.Scale,
			state.TimelineRange.Duration.Value.Value(), state.TimelineRange.Duration.Scale,
			startKey, endKey, state.Enabled, linkGroup, state.Tombstoned, transactionID.String(),
		)
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE clips SET sequence_id = ?, track_id = ?, asset_id = ?, source_stream_id = ?, revision = ?,
  source_start_value = ?, source_start_scale = ?, source_duration_value = ?, source_duration_scale = ?,
  timeline_start_value = ?, timeline_start_scale = ?, timeline_duration_value = ?, timeline_duration_scale = ?,
  timeline_start_order_key = ?, timeline_end_order_key = ?, enabled = ?, link_group_id = ?,
  tombstoned = ?, last_transaction_id = ?
WHERE id = ? AND project_id = ? AND revision = ?`,
		state.SequenceID.String(), state.TrackID.String(), state.AssetID.String(), state.SourceStreamID.String(),
		state.Revision.Value(), state.SourceRange.Start.Value.Value(), state.SourceRange.Start.Scale,
		state.SourceRange.Duration.Value.Value(), state.SourceRange.Duration.Scale,
		state.TimelineRange.Start.Value.Value(), state.TimelineRange.Start.Scale,
		state.TimelineRange.Duration.Value.Value(), state.TimelineRange.Duration.Scale,
		startKey, endKey, state.Enabled, linkGroup, state.Tombstoned, transactionID.String(),
		state.ID.String(), projectID.String(), change.Before.Value(),
	)
	return requireOneEditRow(result, err)
}

func clipOrderKeys(value domain.TimeRange) (string, string, error) {
	start, err := domain.RationalOrderKey(value.Start)
	if err != nil {
		return "", "", err
	}
	end, err := value.End()
	if err != nil {
		return "", "", err
	}
	endKey, err := domain.RationalOrderKey(end)
	return start, endKey, err
}
