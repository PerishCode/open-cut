package repository

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func reconcileSequenceExportJobs(
	ctx context.Context,
	tx *sql.Tx,
	executorVersion string,
	now time.Time,
) error {
	at := formatInstant(now.UTC())
	if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'failed', terminal_error_code = 'input-job-failed', updated_at = ?
WHERE kind = 'sequence-export' AND state IN ('blocked', 'queued')
  AND EXISTS (
    SELECT 1 FROM sequence_export_job_inputs input
    JOIN work_jobs producer ON producer.id = input.producer_job_id
    WHERE input.job_id = work_jobs.id AND producer.state IN ('failed', 'cancelled')
  )`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'failed', terminal_error_code = 'input-artifact-unavailable', updated_at = ?
WHERE kind = 'sequence-export' AND state IN ('blocked', 'queued')
  AND EXISTS (
    SELECT 1 FROM sequence_export_job_inputs input
    JOIN work_jobs producer ON producer.id = input.producer_job_id
    JOIN media_job_details detail ON detail.job_id = producer.id
    LEFT JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
    WHERE input.job_id = work_jobs.id AND producer.state = 'succeeded'
      AND (artifact.id IS NULL OR artifact.kind != 'render-input' OR artifact.state != 'ready')
  )`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM work_job_prerequisites
WHERE job_id IN (
  SELECT id FROM work_jobs
  WHERE kind = 'sequence-export' AND state IN ('blocked', 'queued')
)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT DISTINCT input.job_id, 'artifact-ready', 'job', input.producer_job_id, ?
FROM sequence_export_job_inputs input
JOIN work_jobs export ON export.id = input.job_id
JOIN work_jobs producer ON producer.id = input.producer_job_id
JOIN media_job_details detail ON detail.job_id = producer.id
LEFT JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE export.state IN ('blocked', 'queued')
  AND NOT (producer.state = 'succeeded' AND artifact.kind = 'render-input' AND artifact.state = 'ready')`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT job.id, 'executor-required', 'capability', 'work-executor/sequence-export', ?
FROM work_jobs job JOIN sequence_export_job_details detail ON detail.job_id = job.id
WHERE job.state IN ('blocked', 'queued')
  AND (? = '' OR detail.renderer_version != ?)`, at, executorVersion, executorVersion); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = CASE WHEN EXISTS (
      SELECT 1 FROM work_job_prerequisites prerequisite
      WHERE prerequisite.job_id = work_jobs.id
    ) THEN 'blocked' ELSE 'queued' END,
    terminal_error_code = NULL, updated_at = ?
WHERE kind = 'sequence-export' AND state IN ('blocked', 'queued')`, at)
	return err
}

func (repository *SQLiteProjects) claimSequenceExportJob(
	ctx context.Context,
	input application.ClaimWorkJobInput,
	jobID domain.WorkJobID,
	executorVersion string,
) (application.WorkJobClaim, error) {
	if jobID.IsZero() || executorVersion == "" {
		return application.WorkJobClaim{}, application.ErrNoWork
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.WorkJobClaim{}, err
	}
	defer tx.Rollback()
	var projectValue, sequenceValue, parametersDigestValue, parametersJSON string
	var resolverVersion, compilerVersion, rendererVersion, rendererTarget, preset string
	var sequenceRevision, generation uint64
	err = tx.QueryRowContext(ctx, `
SELECT job.project_id, detail.sequence_id, detail.sequence_revision, detail.preset,
       detail.resolver_version, detail.compiler_version, detail.renderer_version,
       detail.renderer_target, job.parameters_digest, job.parameters_json,
       COALESCE((SELECT MAX(generation) FROM work_job_attempts WHERE job_id = job.id), 0) + 1
FROM work_jobs job JOIN sequence_export_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.kind = 'sequence-export' AND job.state = 'queued'
  AND job.cancellation_requested = 0 AND detail.renderer_version = ?
  AND NOT EXISTS (
    SELECT 1 FROM sequence_export_job_inputs input
    JOIN work_jobs producer ON producer.id = input.producer_job_id
    JOIN media_job_details media_detail ON media_detail.job_id = producer.id
    LEFT JOIN media_artifacts artifact ON artifact.id = media_detail.result_artifact_id
    WHERE input.job_id = job.id AND NOT (
      producer.state = 'succeeded' AND artifact.kind = 'render-input' AND artifact.state = 'ready'
    )
  )`, jobID.String(), executorVersion).Scan(
		&projectValue, &sequenceValue, &sequenceRevision, &preset, &resolverVersion,
		&compilerVersion, &rendererVersion, &rendererTarget, &parametersDigestValue,
		&parametersJSON, &generation,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.WorkJobClaim{}, application.ErrNoWork
	}
	if err != nil {
		return application.WorkJobClaim{}, err
	}
	projectID, projectErr := domain.ParseProjectID(projectValue)
	sequenceID, sequenceErr := domain.ParseSequenceID(sequenceValue)
	revision, revisionErr := domain.NewRevision(sequenceRevision)
	digest, digestErr := domain.ParseDigest(parametersDigestValue)
	parameters, parametersErr := application.DecodeSequenceExportJobParameters([]byte(parametersJSON))
	canonical, canonicalDigest, normalized, canonicalErr := application.CanonicalSequenceExportJobParameters(parameters)
	if projectErr != nil || sequenceErr != nil || revisionErr != nil || digestErr != nil ||
		parametersErr != nil || canonicalErr != nil || digest != canonicalDigest ||
		!bytes.Equal(canonical, []byte(parametersJSON)) || normalized.ProjectID != projectID ||
		normalized.SequenceID != sequenceID || normalized.SequenceRevision != revision ||
		normalized.Preset != preset || normalized.ResolverVersion != resolverVersion ||
		normalized.CompilerVersion != compilerVersion || normalized.RendererVersion != rendererVersion ||
		normalized.RendererTarget != rendererTarget {
		return application.WorkJobClaim{}, application.ErrSequenceExportInvalid
	}
	if err := validateClaimedSequenceExportPins(ctx, tx, jobID, normalized); err != nil {
		return application.WorkJobClaim{}, err
	}
	now := formatInstant(input.Now.UTC())
	expires := formatInstant(input.Now.UTC().Add(input.LeaseDuration))
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_attempts (
  id, job_id, generation, state, lease_owner, lease_expires_at, heartbeat_at,
  started_at, executor_version, temporary_output_identity
) VALUES (?, ?, ?, 'running', ?, ?, ?, ?, ?, ?)`,
		input.AttemptID.String(), jobID.String(), generation, input.LeaseOwner,
		expires, now, now, executorVersion, input.AttemptID.String(),
	); err != nil {
		return application.WorkJobClaim{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE work_jobs SET state = 'running', updated_at = ?
WHERE id = ? AND state = 'queued' AND cancellation_requested = 0`, now, jobID.String())
	if err != nil {
		return application.WorkJobClaim{}, err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.WorkJobClaim{}, application.ErrNoWork
	}
	if err := tx.Commit(); err != nil {
		return application.WorkJobClaim{}, err
	}
	exportClaim := application.SequenceExportJobClaim{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: revision,
		Parameters: normalized, ParametersDigest: digest, ParametersJSON: append([]byte(nil), canonical...),
	}
	return application.WorkJobClaim{
		JobID: jobID, AttemptID: input.AttemptID, Kind: domain.WorkJobSequenceExport,
		ExecutorVersion: executorVersion, Generation: generation, LeaseOwner: input.LeaseOwner,
		LeaseExpiresAt: input.Now.UTC().Add(input.LeaseDuration), SequenceExport: &exportClaim,
	}, nil
}

func validateClaimedSequenceExportPins(
	ctx context.Context,
	tx *sql.Tx,
	jobID domain.WorkJobID,
	parameters application.SequenceExportJobParameters,
) error {
	inputRows, err := tx.QueryContext(ctx, `
SELECT clip_id, source_stream_id, producer_job_id
FROM sequence_export_job_inputs WHERE job_id = ? ORDER BY ordinal`, jobID.String())
	if err != nil {
		return err
	}
	storedInputs := make([]application.SequenceRenderInputRequirement, 0, len(parameters.Inputs))
	for inputRows.Next() {
		var clipValue, streamValue, producerValue string
		if err := inputRows.Scan(&clipValue, &streamValue, &producerValue); err != nil {
			inputRows.Close()
			return err
		}
		clipID, clipErr := domain.ParseClipID(clipValue)
		streamID, streamErr := domain.ParseSourceStreamID(streamValue)
		producerID, producerErr := domain.ParseWorkJobID(producerValue)
		if clipErr != nil || streamErr != nil || producerErr != nil {
			inputRows.Close()
			return application.ErrSequenceExportInvalid
		}
		storedInputs = append(storedInputs, application.SequenceRenderInputRequirement{
			ClipID: clipID, SourceStreamID: streamID, ProducerJobID: producerID,
		})
	}
	if err := inputRows.Err(); err != nil {
		inputRows.Close()
		return err
	}
	if err := inputRows.Close(); err != nil {
		return err
	}
	resourceRows, err := tx.QueryContext(ctx, `
SELECT resource_kind, resource_id, resource_version, resource_digest
FROM sequence_export_job_resources WHERE job_id = ? ORDER BY ordinal`, jobID.String())
	if err != nil {
		return err
	}
	storedResources := make([]application.SequenceRenderResourcePin, 0, len(parameters.Resources))
	for resourceRows.Next() {
		var kind, id, version, digestValue string
		if err := resourceRows.Scan(&kind, &id, &version, &digestValue); err != nil {
			resourceRows.Close()
			return err
		}
		digest, parseErr := domain.ParseDigest(digestValue)
		if parseErr != nil {
			resourceRows.Close()
			return application.ErrSequenceExportInvalid
		}
		storedResources = append(storedResources, application.SequenceRenderResourcePin{
			Kind: kind, ID: id, Version: version, SHA256: digest,
		})
	}
	if err := resourceRows.Err(); err != nil {
		resourceRows.Close()
		return err
	}
	if err := resourceRows.Close(); err != nil {
		return err
	}
	if !sequencePreviewInputsEqual(storedInputs, parameters.Inputs) ||
		!sequencePreviewResourcesEqual(storedResources, parameters.Resources) {
		return application.ErrSequenceExportInvalid
	}
	return nil
}
