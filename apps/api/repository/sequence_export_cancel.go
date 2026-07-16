package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) CancelSequenceExport(
	ctx context.Context,
	record application.CancelSequenceExportRecord,
) (application.SequenceExportResult, error) {
	if err := validateSequenceExportCancelRecord(record); err != nil {
		return application.SequenceExportResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	defer tx.Rollback()
	if err := verifySequenceExportAccess(ctx, tx, record.ReadSequenceExportRecord, true); err != nil {
		return application.SequenceExportResult{}, err
	}
	replayRecord := application.ReplaySequenceExportRequestRecord{
		ReadSequenceExportRecord: record.ReadSequenceExportRecord, Command: "cancel",
		RequestID: record.RequestID, RequestDigest: record.RequestDigest,
		RequestCanonical: record.RequestCanonical,
	}
	if replay, found, err := replaySequenceExportRequestTx(ctx, tx, replayRecord); err != nil {
		return application.SequenceExportResult{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return application.SequenceExportResult{}, err
		}
		replay.Replayed = true
		return replay, nil
	}
	tail, err := resolveSequenceExportTailID(ctx, tx, record.ProjectID, record.JobID)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	var state string
	if err := tx.QueryRowContext(ctx, `
SELECT state FROM work_jobs WHERE id = ? AND kind = 'sequence-export'`, tail.String()).Scan(&state); err != nil {
		return application.SequenceExportResult{}, err
	}
	at := formatInstant(record.CancelledAt.UTC())
	kind, summary := "sequence.export-cancelled", "sequence-export-cancelled"
	if state == string(domain.MediaJobSucceeded) {
		kind, summary = "sequence.export-cancel-observed", "sequence-export-success-won"
	} else if state != string(domain.MediaJobFailed) && state != string(domain.MediaJobCancelled) {
		if _, err := tx.ExecContext(ctx, `
DELETE FROM render_material_leases WHERE attempt_id IN (
  SELECT id FROM work_job_attempts
  WHERE job_id = ? AND state IN ('leased', 'running', 'publishing')
)`, tail.String()); err != nil {
			return application.SequenceExportResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'abandoned', heartbeat_at = ?, ended_at = ?, diagnostics_json = '{"code":"cancelled"}'
WHERE job_id = ? AND state IN ('leased', 'running', 'publishing')`, at, at, tail.String()); err != nil {
			return application.SequenceExportResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs SET state = 'cancelled', cancellation_requested = 1,
    updated_at = ?, terminal_error_code = NULL
WHERE id = ? AND state IN ('blocked', 'queued', 'running')`, at, tail.String()); err != nil {
			return application.SequenceExportResult{}, err
		}
	}
	if err := appendSequenceExportActivity(
		ctx, tx, record.ProjectID, record.RunID, record.TurnID, record.Actor,
		tail, record.ActivityEventID, kind, summary, record.CancelledAt,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_requests (
  actor_id, request_id, command, input_digest, input_json, project_id,
  owner_kind, owner_id, run_id, turn_id, job_id, activity_event_id, created_at
) VALUES (?, ?, 'cancel', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Actor.IDString(), record.RequestID.String(), record.RequestDigest.String(),
		string(record.RequestCanonical), record.ProjectID.String(), string(record.Owner.Kind), record.Owner.ID,
		sequenceExportOptionalID(record.RunID.String()), sequenceExportOptionalID(record.TurnID.String()),
		tail.String(), record.ActivityEventID.String(), at,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	result, err := loadSequenceExportResult(ctx, tx, record.ProjectID, tail)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportResult{}, err
	}
	return result, nil
}

func validateSequenceExportCancelRecord(record application.CancelSequenceExportRecord) error {
	if err := validateSequenceExportReadRecord(record.ReadSequenceExportRecord); err != nil ||
		record.ActivityEventID.IsZero() || record.CancelledAt.IsZero() ||
		record.RequestDigest == "" || !json.Valid(record.RequestCanonical) {
		return application.ErrSequenceExportInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrSequenceExportInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-cancel", application.SequenceExportCancelSchema, struct {
			JobID domain.WorkJobID `json:"jobId"`
		}{record.JobID},
	)
	if err != nil || digest != record.RequestDigest || !bytes.Equal(canonical, record.RequestCanonical) {
		return application.ErrSequenceExportInvalid
	}
	return nil
}
