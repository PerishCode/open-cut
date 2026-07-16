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

func (repository *SQLiteProjects) LoadSequencePreviewRetrySeed(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
) (application.SequencePreviewRetrySeed, error) {
	if projectID.IsZero() || sequenceID.IsZero() || sequenceRevision.Value() == 0 || jobID.IsZero() {
		return application.SequencePreviewRetrySeed{}, application.ErrSequencePreviewInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequencePreviewRetrySeed{}, err
	}
	defer tx.Rollback()
	var parametersJSON, intentSchema, intentDigestValue, intentJSON string
	err = tx.QueryRowContext(ctx, `
SELECT job.parameters_json, detail.render_intent_schema,
       detail.render_intent_digest, detail.render_intent_json
FROM work_jobs job
JOIN projects project ON project.id = job.project_id
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.kind = 'sequence-preview' AND job.project_id = ?
  AND detail.sequence_id = ? AND detail.sequence_revision = ?
  AND job.state IN ('failed', 'cancelled') AND project.status = 'active'
  AND NOT EXISTS (SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id)`,
		jobID.String(), projectID.String(), sequenceID.String(), sequenceRevision.Value(),
	).Scan(&parametersJSON, &intentSchema, &intentDigestValue, &intentJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequencePreviewRetrySeed{}, application.ErrSequencePreviewRecovery
	}
	if err != nil {
		return application.SequencePreviewRetrySeed{}, err
	}
	parameters, err := application.DecodeSequencePreviewJobParameters([]byte(parametersJSON))
	if err != nil || parameters.ProjectID != projectID || parameters.SequenceID != sequenceID ||
		parameters.SequenceRevision != sequenceRevision {
		return application.SequencePreviewRetrySeed{}, application.ErrSequencePreviewInvalid
	}
	intent, intentDigest, err := application.DecodeSequencePreviewRenderIntent(
		[]byte(intentJSON), parameters.Inputs,
	)
	if err != nil || intentSchema != application.SequencePreviewRenderIntentSchema ||
		intentDigest.String() != intentDigestValue || intent.ProjectID != projectID ||
		intent.SequenceID != sequenceID || intent.SequenceRevision != sequenceRevision {
		return application.SequencePreviewRetrySeed{}, application.ErrSequencePreviewInvalid
	}
	job, err := loadSequencePreviewJobProjection(ctx, tx, jobID)
	if err != nil {
		return application.SequencePreviewRetrySeed{}, err
	}
	if application.SequencePreviewRecoveryAction(job) != application.MediaRecoveryRetryJob {
		return application.SequencePreviewRetrySeed{}, application.ErrSequencePreviewRecovery
	}
	if err := tx.Commit(); err != nil {
		return application.SequencePreviewRetrySeed{}, err
	}
	return application.SequencePreviewRetrySeed{Job: job, Parameters: parameters, RenderIntent: intent}, nil
}

func (repository *SQLiteProjects) RetrySequencePreviewJob(
	ctx context.Context,
	record application.RetrySequencePreviewJobRecord,
) (application.SequencePreviewJobProjection, error) {
	if record.PredecessorJobID.IsZero() || record.PredecessorJobID == record.Job.JobID ||
		record.EventID.IsZero() || !validSequencePreviewRetryActor(record.Actor) ||
		validateSequencePreviewJobRecord(record.Job) != nil {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	defer tx.Rollback()
	var state, sourceParametersJSON, sourceIntentSchema, sourceIntentDigest, sourceIntentJSON string
	var sourceProject, sourceSequence string
	var sourceRevision uint64
	var sourcePlan sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT predecessor.state, predecessor.parameters_json, predecessor.project_id,
       detail.sequence_id, detail.sequence_revision, detail.render_intent_schema,
       detail.render_intent_digest, detail.render_intent_json, detail.render_plan_digest
FROM work_jobs predecessor
JOIN projects project ON project.id = predecessor.project_id
JOIN sequence_preview_job_details detail ON detail.job_id = predecessor.id
WHERE predecessor.id = ? AND predecessor.kind = 'sequence-preview'
  AND predecessor.state IN ('failed', 'cancelled') AND project.status = 'active'
  AND NOT EXISTS (SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = predecessor.id)`,
		record.PredecessorJobID.String(),
	).Scan(
		&state, &sourceParametersJSON, &sourceProject, &sourceSequence, &sourceRevision,
		&sourceIntentSchema, &sourceIntentDigest, &sourceIntentJSON, &sourcePlan,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewRecovery
	}
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if sourceProject != record.Job.Parameters.ProjectID.String() ||
		sourceSequence != record.Job.Parameters.SequenceID.String() ||
		sourceRevision != record.Job.Parameters.SequenceRevision.Value() {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
	}
	predecessor, err := loadSequencePreviewJobProjection(ctx, tx, record.PredecessorJobID)
	if err != nil || string(predecessor.State) != state ||
		application.SequencePreviewRecoveryAction(predecessor) != application.MediaRecoveryRetryJob {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewRecovery
	}
	sourceParameters, err := application.DecodeSequencePreviewJobParameters([]byte(sourceParametersJSON))
	if err != nil {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
	}
	sourceParameters.RendererVersion = record.Job.Parameters.RendererVersion
	sourceParameters.RendererTarget = record.Job.Parameters.RendererTarget
	expectedParameters, expectedDigest, expectedNormalized, err :=
		application.CanonicalSequencePreviewJobParameters(sourceParameters)
	if err != nil || expectedNormalized.Validate() != nil || expectedDigest != record.Job.Digest ||
		!bytes.Equal(expectedParameters, record.Job.Canonical) {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
	}
	if sourceIntentSchema != application.SequencePreviewRenderIntentSchema ||
		sourceIntentDigest != record.Job.IntentDigest.String() ||
		sourceIntentJSON != string(record.Job.IntentCanonical) {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
	}
	at := formatInstant(record.Job.CreatedAt.UTC())
	result, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, installation_id, kind, state, pool, priority_class,
  logical_key, parameters_digest, parameters_json, producer_version,
  progress_basis_points, cancellation_requested, retry_of_job_id, created_at,
  updated_at, terminal_error_code
)
SELECT ?, scope_kind, project_id, installation_id, kind, 'blocked', pool, priority_class,
       ?, ?, ?, ?, 0, 0, id, ?, ?, NULL
FROM work_jobs WHERE id = ? AND state IN ('failed', 'cancelled')`,
		record.Job.JobID.String(), record.Job.LogicalKey, record.Job.Digest.String(),
		string(record.Job.Canonical), record.Job.Parameters.RendererVersion,
		at, at, record.PredecessorJobID.String(),
	)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewRecovery
	}
	result, err = tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_details (
  job_id, sequence_id, sequence_revision, resolver_version, compiler_version,
  renderer_version, renderer_target, output_profile,
  render_intent_schema, render_intent_digest, render_intent_json, render_plan_digest
)
SELECT ?, sequence_id, sequence_revision, resolver_version, compiler_version,
       ?, ?, output_profile, ?, ?, ?, render_plan_digest
FROM sequence_preview_job_details WHERE job_id = ?`,
		record.Job.JobID.String(), record.Job.Parameters.RendererVersion,
		record.Job.Parameters.RendererTarget, application.SequencePreviewRenderIntentSchema,
		record.Job.IntentDigest.String(), string(record.Job.IntentCanonical), record.PredecessorJobID.String(),
	)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
	}
	if err := cloneSequencePreviewRetryInputs(ctx, tx, record.PredecessorJobID, record.Job.JobID); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if err := cloneSequencePreviewRetryResources(ctx, tx, record.PredecessorJobID, record.Job.JobID); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
SELECT ?, owner_kind, owner_id, ? FROM work_job_owners WHERE job_id = ?`,
		record.Job.JobID.String(), at, record.PredecessorJobID.String(),
	); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT DISTINCT ?, 'artifact-ready', 'job', input.producer_job_id, ?
FROM sequence_preview_job_inputs input
JOIN work_jobs producer ON producer.id = input.producer_job_id
JOIN media_job_details detail ON detail.job_id = producer.id
LEFT JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE input.job_id = ?
  AND NOT (producer.state = 'succeeded' AND artifact.state = 'ready')`,
		record.Job.JobID.String(), at, record.Job.JobID.String(),
	); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'work-executor/sequence-preview', ?)`,
		record.Job.JobID.String(), at,
	); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if err := appendSequencePreviewRetriedActivity(ctx, tx, record); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	retry, err := loadSequencePreviewJobProjection(ctx, tx, record.Job.JobID)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	return retry, nil
}

func validSequencePreviewRetryActor(actor domain.ActorRef) bool {
	return actor.Validate() == nil && (actor.Kind == domain.ActorCreator || actor.Kind == domain.ActorAgent)
}

func cloneSequencePreviewRetryInputs(
	ctx context.Context,
	tx *sql.Tx,
	predecessorID domain.WorkJobID,
	retryID domain.WorkJobID,
) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_inputs (job_id, ordinal, clip_id, source_stream_id, producer_job_id)
SELECT ?, ordinal, clip_id, source_stream_id, producer_job_id
FROM sequence_preview_job_inputs WHERE job_id = ?`, retryID.String(), predecessorID.String())
	return err
}

func cloneSequencePreviewRetryResources(
	ctx context.Context,
	tx *sql.Tx,
	predecessorID domain.WorkJobID,
	retryID domain.WorkJobID,
) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_resources (
  job_id, ordinal, resource_kind, resource_id, resource_version, resource_digest
)
SELECT ?, ordinal, resource_kind, resource_id, resource_version, resource_digest
FROM sequence_preview_job_resources WHERE job_id = ?`, retryID.String(), predecessorID.String())
	return err
}

func appendSequencePreviewRetriedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.RetrySequencePreviewJobRecord,
) error {
	var projectRevisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`,
		record.Job.Parameters.ProjectID.String(),
	).Scan(&projectRevisionValue); err != nil {
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
		SequenceID: record.Job.Parameters.SequenceID, SequenceRevision: record.Job.Parameters.SequenceRevision,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.Job.Parameters.ProjectID.String(),
		EventID: record.EventID.String(), Kind: "sequence.preview-retried",
		OccurredAt: formatInstant(record.Job.CreatedAt.UTC()),
		ActorKind:  string(record.Actor.Kind), ActorID: record.Actor.IDString(),
		ProjectID: record.Job.Parameters.ProjectID.String(), ProjectRevision: int64(projectRevisionValue),
		OutcomeKind: "sequence-preview-job", OutcomeID: record.Job.JobID.String(),
		SummaryCode: "sequence-preview-retried", Payload: payload,
	})
	return err
}
