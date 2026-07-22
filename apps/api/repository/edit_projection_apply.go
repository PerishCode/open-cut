package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func applyNormalizedOperation(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	operation domain.NormalizedEditOperation,
	changes map[string]domain.EntityRevisionChange,
) error {
	if normalizedOperationPayloadCount(operation) != 1 {
		return application.ErrEditInvalid
	}
	switch operation.Type {
	case domain.NormalizedRestoreProjectVersion:
		if operation.ProjectVersion == nil {
			return application.ErrEditInvalid
		}
		return applyProjectVersionRestore(ctx, tx, projectID, transactionID, *operation.ProjectVersion, changes)
	case domain.NormalizedPutNarrativeNode:
		if operation.NarrativeNode == nil || operation.NarrativeNode.ID().IsZero() {
			return application.ErrEditInvalid
		}
		change := changes[string(domain.EntityNarrativeNode)+"\x00"+operation.NarrativeNode.ID().String()]
		return applyNarrativeNode(ctx, tx, projectID, transactionID, *operation.NarrativeNode, change)
	case domain.NormalizedPutCaption:
		if operation.Caption == nil {
			return application.ErrEditInvalid
		}
		change := changes[string(domain.EntityCaption)+"\x00"+operation.Caption.ID.String()]
		return applyCaption(ctx, tx, projectID, transactionID, *operation.Caption, change)
	case domain.NormalizedPutAlignment:
		if operation.Alignment == nil {
			return application.ErrEditInvalid
		}
		change := changes[string(domain.EntityAlignment)+"\x00"+operation.Alignment.ID.String()]
		return applyAlignment(ctx, tx, projectID, transactionID, *operation.Alignment, change)
	case domain.NormalizedPutAsset:
		if operation.Asset == nil {
			return application.ErrEditInvalid
		}
		change := changes[string(domain.EntityAsset)+"\x00"+operation.Asset.ID.String()]
		return applyAsset(ctx, tx, projectID, transactionID, *operation.Asset, change)
	case domain.NormalizedPutClip:
		if operation.Clip == nil {
			return application.ErrEditInvalid
		}
		change := changes[string(domain.EntityClip)+"\x00"+operation.Clip.ID.String()]
		return applyClip(ctx, tx, projectID, transactionID, *operation.Clip, change)
	case domain.NormalizedPutLinkGroup:
		if operation.LinkGroup == nil {
			return application.ErrEditInvalid
		}
		change := changes[string(domain.EntityLinkGroup)+"\x00"+operation.LinkGroup.ID.String()]
		return applyLinkGroup(ctx, tx, projectID, transactionID, *operation.LinkGroup, change)
	case domain.NormalizedPutTranscriptCorrection:
		if operation.TranscriptCorrection == nil {
			return application.ErrEditInvalid
		}
		change := changes[string(domain.EntityTranscriptCorrection)+"\x00"+operation.TranscriptCorrection.ID.String()]
		return applyTranscriptCorrection(ctx, tx, projectID, transactionID, *operation.TranscriptCorrection, change)
	default:
		return application.ErrEditInvalid
	}
}

func normalizedOperationPayloadCount(operation domain.NormalizedEditOperation) int {
	count := 0
	for _, present := range []bool{
		operation.NarrativeNode != nil,
		operation.TranscriptCorrection != nil, operation.Caption != nil,
		operation.Alignment != nil, operation.Asset != nil, operation.Clip != nil,
		operation.LinkGroup != nil,
		operation.ProjectVersion != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

func applyAsset(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	state domain.AssetState,
	change domain.EntityRevisionChange,
) error {
	if change.Before == nil || state.ProjectID != projectID || state.DisplayName == "" ||
		(state.ImportMode != domain.AssetReferenced && state.ImportMode != domain.AssetManaged) {
		return application.ErrEditInvalid
	}
	var fingerprint any
	if state.AcceptedFingerprint != nil {
		fingerprint = state.AcceptedFingerprint.String()
	}
	result, err := tx.ExecContext(ctx, `
UPDATE assets SET revision = ?, source_grant_id = ?, display_name = ?, import_mode = ?,
  accepted_fingerprint = ?, tombstoned = ?, last_transaction_id = ?
WHERE id = ? AND project_id = ? AND revision = ?`,
		state.Revision.Value(), state.SourceGrantID.String(), state.DisplayName, state.ImportMode,
		fingerprint, state.Tombstoned, transactionID.String(), state.ID.String(), projectID.String(),
		change.Before.Value(),
	)
	return requireOneEditRow(result, err)
}

func applyCaption(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	state domain.CaptionState,
	change domain.EntityRevisionChange,
) error {
	startKey, endKey, err := captionOrderKeys(state.Range)
	if err != nil {
		return application.ErrEditInvalid
	}
	if change.Before == nil {
		_, err := tx.ExecContext(ctx, `
INSERT INTO captions (
  id, project_id, sequence_id, track_id, revision, start_value, start_scale,
  duration_value, duration_scale, start_order_key, end_order_key, language, text,
  tombstoned, last_transaction_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			state.ID.String(), projectID.String(), state.SequenceID.String(), state.TrackID.String(),
			state.Revision.Value(), state.Range.Start.Value.Value(), state.Range.Start.Scale,
			state.Range.Duration.Value.Value(), state.Range.Duration.Scale, startKey, endKey,
			state.Language.String(), state.Text, state.Tombstoned, transactionID.String())
		if err != nil {
			return err
		}
		return insertCaptionProvenance(ctx, tx, state)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE captions SET revision = ?, start_value = ?, start_scale = ?, duration_value = ?,
  duration_scale = ?, start_order_key = ?, end_order_key = ?, language = ?, text = ?, tombstoned = ?,
  last_transaction_id = ?
WHERE id = ? AND project_id = ? AND revision = ?`,
		state.Revision.Value(), state.Range.Start.Value.Value(), state.Range.Start.Scale,
		state.Range.Duration.Value.Value(), state.Range.Duration.Scale, startKey, endKey,
		state.Language.String(), state.Text, state.Tombstoned, transactionID.String(),
		state.ID.String(), projectID.String(), change.Before.Value())
	return requireOneEditRow(result, err)
}

func applyAlignment(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	state domain.AlignmentState,
	change domain.EntityRevisionChange,
) error {
	if change.Before == nil {
		_, err := tx.ExecContext(ctx, `
INSERT INTO alignments (
  id, project_id, narrative_node_id, narrative_node_revision, sequence_id,
  revision, status, last_transaction_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			state.ID.String(), projectID.String(), state.NarrativeNodeID.String(), state.NarrativeNodeRevision.Value(),
			state.SequenceID.String(), state.Revision.Value(), state.Status, transactionID.String())
		if err != nil {
			return err
		}
		return insertAlignmentTargets(ctx, tx, state)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE alignments SET narrative_node_id = ?, narrative_node_revision = ?,
  sequence_id = ?, revision = ?, status = ?, last_transaction_id = ?
WHERE id = ? AND project_id = ? AND revision = ?`,
		state.NarrativeNodeID.String(), state.NarrativeNodeRevision.Value(), state.SequenceID.String(),
		state.Revision.Value(), state.Status, transactionID.String(),
		state.ID.String(), projectID.String(), change.Before.Value())
	if err := requireOneEditRow(result, err); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alignment_targets WHERE alignment_id = ?`, state.ID.String()); err != nil {
		return err
	}
	return insertAlignmentTargets(ctx, tx, state)
}

func insertAlignmentTargets(ctx context.Context, tx *sql.Tx, state domain.AlignmentState) error {
	if len(state.Targets) == 0 || len(state.Targets) > 64 {
		return application.ErrEditInvalid
	}
	for ordinal, target := range state.Targets {
		var captionID, clipID any
		var entityRevision, localStart, localStartScale, localDuration, localDurationScale any
		var timelineStart, timelineStartScale, timelineDuration, timelineDurationScale, sequenceRevision any
		switch target.Type {
		case domain.AlignmentTargetCaption:
			if target.Caption == nil || target.Clip != nil || target.Timeline != nil {
				return application.ErrEditInvalid
			}
			captionID, entityRevision = target.Caption.CaptionID.String(), target.Caption.CaptionRevision.Value()
			localStart, localStartScale = target.Caption.LocalRange.Start.Value.Value(), target.Caption.LocalRange.Start.Scale
			localDuration = target.Caption.LocalRange.Duration.Value.Value()
			localDurationScale = target.Caption.LocalRange.Duration.Scale
		case domain.AlignmentTargetClip:
			if target.Clip == nil || target.Caption != nil || target.Timeline != nil {
				return application.ErrEditInvalid
			}
			clipID, entityRevision = target.Clip.ClipID.String(), target.Clip.ClipRevision.Value()
			localStart, localStartScale = target.Clip.LocalRange.Start.Value.Value(), target.Clip.LocalRange.Start.Scale
			localDuration = target.Clip.LocalRange.Duration.Value.Value()
			localDurationScale = target.Clip.LocalRange.Duration.Scale
		case domain.AlignmentTargetTimeline:
			if target.Timeline == nil || target.Caption != nil || target.Clip != nil {
				return application.ErrEditInvalid
			}
			timelineStart = target.Timeline.Range.Start.Value.Value()
			timelineStartScale = target.Timeline.Range.Start.Scale
			timelineDuration = target.Timeline.Range.Duration.Value.Value()
			timelineDurationScale = target.Timeline.Range.Duration.Scale
			sequenceRevision = target.Timeline.SequenceRevision.Value()
		default:
			return application.ErrEditInvalid
		}
		if ordinal > 0 && state.Targets[0].Type != target.Type {
			return application.ErrEditInvalid
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO alignment_targets (
  alignment_id, ordinal, kind, caption_id, clip_id, entity_revision,
  local_start_value, local_start_scale, local_duration_value, local_duration_scale,
  timeline_start_value, timeline_start_scale, timeline_duration_value,
  timeline_duration_scale, sequence_revision
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			state.ID.String(), ordinal, target.Type, captionID, clipID, entityRevision,
			localStart, localStartScale, localDuration, localDurationScale,
			timelineStart, timelineStartScale, timelineDuration, timelineDurationScale, sequenceRevision)
		if err != nil {
			return err
		}
	}
	return nil
}

func applyAggregateRevision(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	change domain.EntityRevisionChange,
) error {
	if change.Before == nil {
		return application.ErrEditInvalid
	}
	var statement string
	switch change.Kind {
	case domain.EntityNarrativeDocument:
		statement = `UPDATE narrative_documents SET revision = ? WHERE id = ? AND project_id = ? AND revision = ?`
	case domain.EntityNarrativeNode:
		statement = `UPDATE narrative_nodes SET revision = ? WHERE id = ? AND project_id = ? AND revision = ?`
	case domain.EntitySequence:
		statement = `UPDATE sequences SET revision = ? WHERE id = ? AND project_id = ? AND revision = ?`
	case domain.EntityTrack:
		statement = `UPDATE tracks SET revision = ? WHERE id = ? AND project_id = ? AND revision = ?`
	default:
		return application.ErrEditInvalid
	}
	result, err := tx.ExecContext(ctx, statement, change.After.Value(), change.ID, projectID.String(), change.Before.Value())
	return requireOneEditRow(result, err)
}

func captionOrderKeys(value domain.TimeRange) (string, string, error) {
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

func requireOneEditRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return application.ErrEditConflict
	}
	return nil
}
