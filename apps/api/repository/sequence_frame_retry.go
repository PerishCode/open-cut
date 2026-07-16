package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) LoadSequenceFrameRetrySeed(
	ctx context.Context,
	record application.ReadSequenceFrameSetRecord,
) (application.SequenceFrameRetrySeed, error) {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.RunID.IsZero() ||
		record.TurnID.IsZero() || record.JobID.IsZero() || record.Actor.Validate() != nil ||
		record.Actor.Kind != domain.ActorAgent {
		return application.SequenceFrameRetrySeed{}, application.ErrSequenceFramesInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceFrameRetrySeed{}, err
	}
	defer tx.Rollback()
	if err := verifyActiveSequenceFrameTurn(ctx, tx, record, true); err != nil {
		return application.SequenceFrameRetrySeed{}, err
	}
	tailID, err := resolveSequenceFrameTailID(ctx, tx, record.ProjectID, record.SequenceID, record.JobID)
	if err != nil {
		return application.SequenceFrameRetrySeed{}, err
	}
	result, err := loadSequenceFrameSetResult(ctx, tx, record.ProjectID, record.SequenceID, tailID)
	if err != nil {
		return application.SequenceFrameRetrySeed{}, err
	}
	if result.Status != application.SequenceFrameSetFailed || result.Recovery != application.MediaRecoveryRetryJob {
		return application.SequenceFrameRetrySeed{}, application.ErrSequenceFramesRecovery
	}
	var parametersJSON string
	err = tx.QueryRowContext(ctx, `
SELECT job.parameters_json
FROM work_jobs job
WHERE job.id = ? AND job.kind = 'sequence-frame-set'
  AND NOT EXISTS (SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id)`, tailID.String()).Scan(&parametersJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequenceFrameRetrySeed{}, application.ErrSequenceFramesRecovery
	}
	if err != nil {
		return application.SequenceFrameRetrySeed{}, err
	}
	parameters, err := application.DecodeSequenceFrameSetParameters([]byte(parametersJSON))
	if err != nil || parameters.ProjectID != record.ProjectID || parameters.SequenceID != record.SequenceID ||
		parameters.SequenceRevision != result.SequenceRevision {
		return application.SequenceFrameRetrySeed{}, application.ErrSequenceFramesInvalid
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceFrameRetrySeed{}, err
	}
	return application.SequenceFrameRetrySeed{Result: result, Parameters: parameters}, nil
}

func (repository *SQLiteProjects) RetrySequenceFrameSet(
	ctx context.Context,
	record application.RetrySequenceFrameSetRecord,
) (application.SequenceFrameSetResult, error) {
	if record.PredecessorJobID.IsZero() || record.PredecessorJobID == record.Job.JobID ||
		validateSequenceFrameSetRequest(record.Job) != nil {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	defer tx.Rollback()
	if err := verifyActiveSequenceFrameTurn(ctx, tx, application.ReadSequenceFrameSetRecord{
		ProjectID: record.Job.ProjectID, SequenceID: record.Job.SequenceID,
		RunID: record.Job.RunID, TurnID: record.Job.TurnID, Actor: record.Job.Actor,
		JobID: record.Job.JobID,
	}, false); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	tailID, err := resolveSequenceFrameTailID(
		ctx, tx, record.Job.ProjectID, record.Job.SequenceID, record.PredecessorJobID,
	)
	if err != nil || tailID != record.PredecessorJobID {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesRecovery
	}
	predecessor, err := loadSequenceFrameSetResult(
		ctx, tx, record.Job.ProjectID, record.Job.SequenceID, record.PredecessorJobID,
	)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if predecessor.Status != application.SequenceFrameSetFailed ||
		predecessor.Recovery != application.MediaRecoveryRetryJob {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesRecovery
	}
	var sourceParametersJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT parameters_json FROM work_jobs
WHERE id = ? AND kind = 'sequence-frame-set'`, record.PredecessorJobID.String()).Scan(&sourceParametersJSON); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	source, err := application.DecodeSequenceFrameSetParameters([]byte(sourceParametersJSON))
	if err != nil {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesInvalid
	}
	expected := source
	expected.PreviewJobID = record.Job.Parameters.PreviewJobID
	expected.ExecutorVersion = record.Job.Parameters.ExecutorVersion
	expectedCanonical, expectedDigest, err := application.CanonicalSequenceFrameSetParameters(expected)
	if err != nil || !bytes.Equal(expectedCanonical, record.Job.ParametersJSON) ||
		expectedDigest != record.Job.ParametersDigest {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesInvalid
	}
	if err := validateSequenceFrameRetryPreview(ctx, tx, record.Job.Parameters); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	at := formatInstant(record.Job.RequestedAt.UTC())
	insert, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, installation_id, kind, state, pool, priority_class,
  logical_key, parameters_digest, parameters_json, producer_version,
  progress_basis_points, cancellation_requested, retry_of_job_id, created_at,
  updated_at, terminal_error_code
)
SELECT ?, scope_kind, project_id, installation_id, kind, 'blocked', pool, priority_class,
       ?, ?, ?, ?, 0, 0, id, ?, ?, NULL
FROM work_jobs
WHERE id = ? AND kind = 'sequence-frame-set'
  AND NOT EXISTS (SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = work_jobs.id)`,
		record.Job.JobID.String(), record.Job.LogicalKey, record.Job.ParametersDigest.String(),
		string(record.Job.ParametersJSON), record.Job.Parameters.ExecutorVersion, at, at,
		record.PredecessorJobID.String(),
	)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if changed, rowsErr := insert.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesRecovery
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_frame_set_job_details (
  job_id, sequence_id, sequence_revision, preview_job_id,
  frame_rate_value, frame_rate_scale, grid_policy, profile
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, record.Job.JobID.String(), record.Job.SequenceID.String(),
		record.Job.Parameters.SequenceRevision.Value(), record.Job.Parameters.PreviewJobID.String(),
		record.Job.Parameters.FrameRate.Value.Value(), record.Job.Parameters.FrameRate.Scale,
		record.Job.Parameters.GridPolicy, record.Job.Parameters.Profile,
	); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
SELECT ?, owner_kind, owner_id, ? FROM work_job_owners WHERE job_id = ?`,
		record.Job.JobID.String(), at, record.PredecessorJobID.String(),
	); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
VALUES (?, 'run', ?, ?)`, record.Job.JobID.String(), record.Job.RunID.String(), at); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'artifact-ready', 'job', ?, ?)`, record.Job.JobID.String(),
		record.Job.Parameters.PreviewJobID.String(), at,
	); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'work-executor/sequence-frame-set', ?)`,
		record.Job.JobID.String(), at,
	); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if err := appendSequenceFrameRetriedActivity(ctx, tx, record); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	result, err := loadSequenceFrameSetResult(
		ctx, tx, record.Job.ProjectID, record.Job.SequenceID, record.Job.JobID,
	)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	return result, nil
}

func validateSequenceFrameRetryPreview(
	ctx context.Context,
	tx *sql.Tx,
	parameters application.SequenceFrameSetParameters,
) error {
	var projectValue, sequenceValue string
	var revision uint64
	err := tx.QueryRowContext(ctx, `
SELECT job.project_id, detail.sequence_id, detail.sequence_revision
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.kind = 'sequence-preview'`, parameters.PreviewJobID.String()).Scan(
		&projectValue, &sequenceValue, &revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrSequenceFramesInvalid
	}
	if err != nil {
		return err
	}
	if projectValue != parameters.ProjectID.String() || sequenceValue != parameters.SequenceID.String() ||
		revision != parameters.SequenceRevision.Value() {
		return application.ErrSequenceFramesInvalid
	}
	return nil
}

func appendSequenceFrameRetriedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.RetrySequenceFrameSetRecord,
) error {
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`,
		record.Job.ProjectID.String(),
	).Scan(&projectRevision); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		PredecessorJobID  domain.WorkJobID               `json:"predecessorJobId"`
		RetryJobID        domain.WorkJobID               `json:"retryJobId"`
		SequenceID        domain.SequenceID              `json:"sequenceId"`
		SequenceRevision  domain.Revision                `json:"sequenceRevision"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{},
		PredecessorJobID:  record.PredecessorJobID, RetryJobID: record.Job.JobID,
		SequenceID: record.Job.SequenceID, SequenceRevision: record.Job.Parameters.SequenceRevision,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.Job.ProjectID.String(),
		EventID: record.Job.ActivityEventID.String(), Kind: "sequence.frame-set-retried",
		OccurredAt: formatInstant(record.Job.RequestedAt.UTC()),
		ActorKind:  string(record.Job.Actor.Kind), ActorID: record.Job.Actor.IDString(),
		ProjectID: record.Job.ProjectID.String(), ProjectRevision: int64(projectRevision),
		OutcomeKind: "sequence-frame-job", OutcomeID: record.Job.JobID.String(),
		SummaryCode: "sequence-frame-set-retried", Payload: payload,
	})
	return err
}
