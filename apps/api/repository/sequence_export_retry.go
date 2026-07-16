package repository

import (
	"bytes"
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
)

func (repository *SQLiteProjects) LoadSequenceExportRetrySeed(
	ctx context.Context,
	record application.ReadSequenceExportRecord,
) (application.SequenceExportRetrySeed, error) {
	if err := validateSequenceExportReadRecord(record); err != nil {
		return application.SequenceExportRetrySeed{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceExportRetrySeed{}, err
	}
	defer tx.Rollback()
	if err := verifySequenceExportAccess(ctx, tx, record, true); err != nil {
		return application.SequenceExportRetrySeed{}, err
	}
	tail, err := resolveSequenceExportTailID(ctx, tx, record.ProjectID, record.JobID)
	if err != nil {
		return application.SequenceExportRetrySeed{}, err
	}
	result, err := loadSequenceExportResult(ctx, tx, record.ProjectID, tail)
	if err != nil {
		return application.SequenceExportRetrySeed{}, err
	}
	if result.Recovery != application.MediaRecoveryRetryJob {
		return application.SequenceExportRetrySeed{}, application.ErrSequenceExportRecovery
	}
	var parametersJSON, intentSchema, intentDigestValue, intentJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT job.parameters_json, detail.render_intent_schema,
       detail.render_intent_digest, detail.render_intent_json
FROM work_jobs job JOIN sequence_export_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.kind = 'sequence-export'`, tail.String()).Scan(
		&parametersJSON, &intentSchema, &intentDigestValue, &intentJSON,
	); err != nil {
		return application.SequenceExportRetrySeed{}, err
	}
	parameters, err := application.DecodeSequenceExportJobParameters([]byte(parametersJSON))
	if err != nil {
		return application.SequenceExportRetrySeed{}, application.ErrSequenceExportInvalid
	}
	intent, intentDigest, err := application.DecodeSequenceRenderIntent([]byte(intentJSON), parameters.Inputs)
	if err != nil || intentSchema != application.SequenceRenderIntentSchema ||
		intentDigest.String() != intentDigestValue {
		return application.SequenceExportRetrySeed{}, application.ErrSequenceExportInvalid
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportRetrySeed{}, err
	}
	return application.SequenceExportRetrySeed{
		Result: result, Parameters: parameters, RenderIntent: intent,
	}, nil
}

func (repository *SQLiteProjects) RetrySequenceExport(
	ctx context.Context,
	record application.RetrySequenceExportRecord,
) (application.SequenceExportResult, error) {
	if err := validateSequenceExportRetryRecord(record); err != nil {
		return application.SequenceExportResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	defer tx.Rollback()
	read := application.ReadSequenceExportRecord{
		ProjectID: record.Job.ProjectID, RunID: record.Job.RunID, TurnID: record.Job.TurnID,
		Actor: record.Job.Actor, Owner: record.Job.Owner, JobID: record.PredecessorJobID,
	}
	if err := verifySequenceExportAccess(ctx, tx, read, true); err != nil {
		return application.SequenceExportResult{}, err
	}
	tail, err := resolveSequenceExportTailID(ctx, tx, record.Job.ProjectID, record.PredecessorJobID)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if tail != record.PredecessorJobID {
		result, loadErr := loadSequenceExportResult(ctx, tx, record.Job.ProjectID, tail)
		if loadErr != nil {
			return application.SequenceExportResult{}, loadErr
		}
		if err := tx.Commit(); err != nil {
			return application.SequenceExportResult{}, err
		}
		return result, nil
	}
	predecessor, err := loadSequenceExportResult(ctx, tx, record.Job.ProjectID, tail)
	if err != nil || predecessor.Recovery != application.MediaRecoveryRetryJob {
		return application.SequenceExportResult{}, application.ErrSequenceExportRecovery
	}
	if err := verifySequenceExportRetryPins(ctx, tx, record); err != nil {
		return application.SequenceExportResult{}, err
	}
	at := formatInstant(record.Job.RequestedAt.UTC())
	result, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, installation_id, kind, state, pool, priority_class,
  logical_key, parameters_digest, parameters_json, producer_version,
  progress_basis_points, cancellation_requested, retry_of_job_id, created_at,
  updated_at, terminal_error_code
)
SELECT ?, scope_kind, project_id, installation_id, kind, 'blocked', pool, priority_class,
       ?, ?, ?, ?, 0, 0, id, ?, ?, NULL
FROM work_jobs WHERE id = ? AND kind = 'sequence-export'`,
		record.Job.JobID.String(), record.Job.LogicalKey, record.Job.ParametersDigest.String(),
		string(record.Job.ParametersJSON), record.Job.Parameters.RendererVersion,
		at, at, record.PredecessorJobID.String(),
	)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.SequenceExportResult{}, application.ErrSequenceExportRecovery
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_job_details (
  job_id, sequence_id, sequence_revision, preset, resolver_version, compiler_version,
  renderer_version, renderer_target, render_intent_schema, render_intent_digest,
  render_intent_json, render_plan_digest
)
SELECT ?, sequence_id, sequence_revision, preset, resolver_version, compiler_version,
       ?, ?, ?, ?, ?, render_plan_digest
FROM sequence_export_job_details WHERE job_id = ?`,
		record.Job.JobID.String(), record.Job.Parameters.RendererVersion,
		record.Job.Parameters.RendererTarget, application.SequenceRenderIntentSchema,
		record.Job.IntentDigest.String(), string(record.Job.IntentJSON), record.PredecessorJobID.String(),
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	for ordinal, input := range record.Job.Parameters.Inputs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_job_inputs (job_id, ordinal, clip_id, source_stream_id, producer_job_id)
VALUES (?, ?, ?, ?, ?)`, record.Job.JobID.String(), ordinal, input.ClipID.String(),
			input.SourceStreamID.String(), input.ProducerJobID.String()); err != nil {
			return application.SequenceExportResult{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_job_resources (
  job_id, ordinal, resource_kind, resource_id, resource_version, resource_digest
)
SELECT ?, ordinal, resource_kind, resource_id, resource_version, resource_digest
FROM sequence_export_job_resources WHERE job_id = ?`,
		record.Job.JobID.String(), record.PredecessorJobID.String(),
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT DISTINCT ?, 'artifact-ready', 'job', producer_job_id, ?
FROM sequence_export_job_inputs WHERE job_id = ?`,
		record.Job.JobID.String(), at, record.Job.JobID.String(),
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'work-executor/sequence-export', ?)`,
		record.Job.JobID.String(), at,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	ownerResult, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
SELECT ?, owner_kind, owner_id, ? FROM work_job_owners WHERE job_id = ?`,
		record.Job.JobID.String(), at, predecessor.Job.RootJobID.String())
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if changed, rowsErr := ownerResult.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.SequenceExportResult{}, application.ErrSequenceExportInvalid
	}
	if err := appendSequenceExportActivity(
		ctx, tx, record.Job.ProjectID, record.Job.RunID, record.Job.TurnID, record.Job.Actor,
		record.Job.JobID, record.Job.ActivityEventID, "sequence.export-retried",
		"sequence-export-retried", record.Job.RequestedAt,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	retry, err := loadSequenceExportResult(ctx, tx, record.Job.ProjectID, record.Job.JobID)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportResult{}, err
	}
	return retry, nil
}

func validateSequenceExportRetryRecord(record application.RetrySequenceExportRecord) error {
	job := record.Job
	if record.PredecessorJobID.IsZero() || job.JobID.IsZero() || record.PredecessorJobID == job.JobID ||
		job.RequestID != "" || job.RequestDigest != "" || len(job.RequestCanonical) != 0 ||
		job.ProjectID.IsZero() || job.SequenceID.IsZero() ||
		job.Owner.Validate(job.Actor, job.RunID, job.TurnID) != nil || job.ActivityEventID.IsZero() ||
		job.RequestedAt.IsZero() || job.LogicalKey == "" || len(job.LogicalKey) > 1024 {
		return application.ErrSequenceExportInvalid
	}
	canonical, digest, normalized, err := application.CanonicalSequenceExportJobParameters(job.Parameters)
	if err != nil || normalized.ProjectID != job.ProjectID || normalized.SequenceID != job.SequenceID ||
		digest != job.ParametersDigest || !bytes.Equal(canonical, job.ParametersJSON) {
		return application.ErrSequenceExportInvalid
	}
	intent, intentJSON, intentDigest, err := application.CanonicalSequenceRenderIntent(
		job.RenderIntent, normalized.Inputs,
	)
	if err != nil || intent.ProjectID != job.ProjectID || intent.SequenceID != job.SequenceID ||
		intentDigest != job.IntentDigest || !bytes.Equal(intentJSON, job.IntentJSON) {
		return application.ErrSequenceExportInvalid
	}
	return nil
}

func verifySequenceExportRetryPins(
	ctx context.Context,
	tx *sql.Tx,
	record application.RetrySequenceExportRecord,
) error {
	var parametersJSON, intentDigest, intentJSON, projectValue, sequenceValue string
	var revision uint64
	if err := tx.QueryRowContext(ctx, `
SELECT job.parameters_json, job.project_id, detail.sequence_id, detail.sequence_revision,
       detail.render_intent_digest, detail.render_intent_json
FROM work_jobs job JOIN sequence_export_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND NOT EXISTS (
  SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id
)`, record.PredecessorJobID.String()).Scan(
		&parametersJSON, &projectValue, &sequenceValue, &revision, &intentDigest, &intentJSON,
	); err != nil {
		return application.ErrSequenceExportRecovery
	}
	source, err := application.DecodeSequenceExportJobParameters([]byte(parametersJSON))
	if err != nil {
		return application.ErrSequenceExportInvalid
	}
	if len(source.Inputs) != len(record.Job.Parameters.Inputs) {
		return application.ErrSequenceExportInvalid
	}
	for index := range source.Inputs {
		if source.Inputs[index].ClipID != record.Job.Parameters.Inputs[index].ClipID ||
			source.Inputs[index].SourceStreamID != record.Job.Parameters.Inputs[index].SourceStreamID {
			return application.ErrSequenceExportInvalid
		}
		source.Inputs[index].ProducerJobID = record.Job.Parameters.Inputs[index].ProducerJobID
	}
	source.RendererVersion = record.Job.Parameters.RendererVersion
	source.RendererTarget = record.Job.Parameters.RendererTarget
	canonical, digest, normalized, err := application.CanonicalSequenceExportJobParameters(source)
	if err != nil || normalized.ProjectID.String() != projectValue || normalized.SequenceID.String() != sequenceValue ||
		normalized.SequenceRevision.Value() != revision || digest != record.Job.ParametersDigest ||
		!bytes.Equal(canonical, record.Job.ParametersJSON) || intentDigest != record.Job.IntentDigest.String() ||
		intentJSON != string(record.Job.IntentJSON) {
		return application.ErrSequenceExportInvalid
	}
	return nil
}
