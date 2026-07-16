package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) RecoverWorkJobs(
	ctx context.Context,
	executors []application.WorkExecutorRegistration,
	resources []application.ProductResourceRegistration,
	now time.Time,
) error {
	media, err := mediaExecutorRegistrations(executors)
	if err != nil || application.ValidateProductResourceRegistrations(resources) != nil || now.IsZero() {
		return application.ErrSequencePreviewInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recoverExpiredWorkAttempts(ctx, tx, now.UTC()); err != nil {
		return err
	}
	if err := reconcileMediaPrerequisites(ctx, tx, media, resources, now.UTC()); err != nil {
		return err
	}
	if err := reconcileSequencePreviewJobs(
		ctx, tx, sequencePreviewExecutorVersion(executors), now.UTC(),
	); err != nil {
		return err
	}
	if err := reconcileSequenceExportJobs(
		ctx, tx, sequenceExportExecutorVersion(executors), now.UTC(),
	); err != nil {
		return err
	}
	if err := reconcileSequenceFrameJobs(
		ctx, tx, sequenceFrameExecutorVersion(executors), now.UTC(),
	); err != nil {
		return err
	}
	if err := reconcileProductResourceJobs(
		ctx, tx, productResourceExecutorVersion(executors), resources, now.UTC(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (repository *SQLiteProjects) ClaimWorkJob(
	ctx context.Context,
	input application.ClaimWorkJobInput,
) (application.WorkJobClaim, error) {
	if input.AttemptID.IsZero() || input.LeaseOwner == "" || len(input.LeaseOwner) > 128 ||
		input.Now.IsZero() || input.LeaseDuration < 3*time.Second || input.LeaseDuration > 10*time.Minute {
		return application.WorkJobClaim{}, application.ErrSequencePreviewInvalid
	}
	media, err := mediaExecutorRegistrations(input.Executors)
	if err != nil {
		return application.WorkJobClaim{}, application.ErrSequencePreviewInvalid
	}
	if application.ValidateProductResourceRegistrations(input.Resources) != nil {
		return application.WorkJobClaim{}, application.ErrProductResourceInvalid
	}
	if err := repository.RecoverWorkJobs(ctx, input.Executors, input.Resources, input.Now.UTC()); err != nil {
		return application.WorkJobClaim{}, err
	}
	jobID, kind, err := repository.nextClaimableWorkJob(ctx, input.Executors)
	if errors.Is(err, sql.ErrNoRows) {
		return application.WorkJobClaim{}, application.ErrNoWork
	}
	if err != nil {
		return application.WorkJobClaim{}, err
	}
	if kind == domain.WorkJobSequencePreview {
		return repository.claimSequencePreviewJob(
			ctx, input, jobID, sequencePreviewExecutorVersion(input.Executors),
		)
	}
	if kind == domain.WorkJobSequenceFrames {
		return repository.claimSequenceFrameJob(
			ctx, input, jobID, sequenceFrameExecutorVersion(input.Executors),
		)
	}
	if kind == domain.WorkJobSequenceExport {
		return repository.claimSequenceExportJob(
			ctx, input, jobID, sequenceExportExecutorVersion(input.Executors),
		)
	}
	if kind == application.WorkJobResourceAcquire {
		return repository.claimProductResourceJob(
			ctx, input, jobID, productResourceExecutorVersion(input.Executors),
		)
	}
	claim, err := repository.ClaimMediaJob(ctx, application.ClaimMediaJobInput{
		AttemptID: input.AttemptID, Executors: media, Resources: input.Resources,
		OnlyJobID: &jobID, LeaseOwner: input.LeaseOwner,
		Now: input.Now, LeaseDuration: input.LeaseDuration,
	})
	if errors.Is(err, application.ErrNoMediaWork) {
		return application.WorkJobClaim{}, application.ErrNoWork
	}
	if err != nil {
		return application.WorkJobClaim{}, err
	}
	return application.WorkJobClaim{
		JobID: claim.JobID, AttemptID: claim.AttemptID, Kind: domain.WorkJobKind(claim.Kind),
		ExecutorVersion: claim.ExecutorVersion, ExecutorTarget: claim.ExecutorTarget,
		Generation: claim.Generation,
		LeaseOwner: claim.LeaseOwner, LeaseExpiresAt: claim.LeaseExpiresAt, Media: &claim,
	}, nil
}

func (repository *SQLiteProjects) RenewWorkJobLease(
	ctx context.Context,
	claim application.WorkJobClaim,
	now time.Time,
	duration time.Duration,
) error {
	if claim.JobID.IsZero() || claim.AttemptID.IsZero() || claim.LeaseOwner == "" ||
		now.IsZero() || duration < 3*time.Second || duration > 10*time.Minute {
		return application.ErrWorkLeaseLost
	}
	result, err := repository.db.ExecContext(ctx, `
UPDATE work_job_attempts
SET heartbeat_at = ?, lease_expires_at = ?
WHERE id = ? AND job_id = ? AND generation = ? AND lease_owner = ?
  AND state = 'running' AND lease_expires_at > ?`,
		formatInstant(now.UTC()), formatInstant(now.UTC().Add(duration)), claim.AttemptID.String(),
		claim.JobID.String(), claim.Generation, claim.LeaseOwner, formatInstant(now.UTC()),
	)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	return nil
}

func mediaExecutorRegistrations(
	executors []application.WorkExecutorRegistration,
) ([]application.MediaExecutorRegistration, error) {
	if err := validateWorkExecutorRegistrations(executors); err != nil {
		return nil, application.ErrSequencePreviewInvalid
	}
	result := make([]application.MediaExecutorRegistration, 0, len(executors))
	for _, executor := range executors {
		switch executor.Kind {
		case domain.MediaJobIdentify, domain.MediaJobProbe, domain.MediaJobFrameSet,
			domain.MediaJobProxy, domain.MediaJobRenderInput, domain.MediaJobTranscript:
			result = append(result, application.MediaExecutorRegistration{
				Kind: domain.MediaJobKind(executor.Kind), Version: executor.Version, Target: executor.Target,
			})
		case domain.WorkJobSequencePreview, domain.WorkJobSequenceFrames, domain.WorkJobSequenceExport:
		case application.WorkJobResourceAcquire:
		}
	}
	return result, nil
}

func validateWorkExecutorRegistrations(executors []application.WorkExecutorRegistration) error {
	if len(executors) == 0 || len(executors) > 16 {
		return application.ErrSequencePreviewInvalid
	}
	seen := make(map[domain.WorkJobKind]struct{}, len(executors))
	for _, executor := range executors {
		maximumVersionLength := 256
		switch executor.Kind {
		case domain.MediaJobIdentify, domain.MediaJobProbe, domain.MediaJobFrameSet, domain.MediaJobProxy:
			if executor.Target != "" {
				return application.ErrSequencePreviewInvalid
			}
		case domain.MediaJobTranscript, domain.MediaJobRenderInput:
			maximumVersionLength = 1024
			if executor.Target == "" || len(executor.Target) > 128 {
				return application.ErrSequencePreviewInvalid
			}
		case domain.WorkJobSequencePreview:
			maximumVersionLength = 1024
			if executor.Target != "" {
				return application.ErrSequencePreviewInvalid
			}
		case domain.WorkJobSequenceFrames:
			maximumVersionLength = 1024
			if executor.Target != "" {
				return application.ErrSequencePreviewInvalid
			}
		case domain.WorkJobSequenceExport:
			maximumVersionLength = 1024
			if executor.Target != "" {
				return application.ErrSequencePreviewInvalid
			}
		case application.WorkJobResourceAcquire:
			if executor.Target != "" {
				return application.ErrSequencePreviewInvalid
			}
		default:
			return application.ErrSequencePreviewInvalid
		}
		if executor.Version == "" || len(executor.Version) > maximumVersionLength {
			return application.ErrSequencePreviewInvalid
		}
		if _, duplicate := seen[executor.Kind]; duplicate {
			return application.ErrSequencePreviewInvalid
		}
		seen[executor.Kind] = struct{}{}
	}
	return nil
}

func sequencePreviewExecutorVersion(executors []application.WorkExecutorRegistration) string {
	for _, executor := range executors {
		if executor.Kind == domain.WorkJobSequencePreview {
			return executor.Version
		}
	}
	return ""
}

func sequenceFrameExecutorVersion(executors []application.WorkExecutorRegistration) string {
	for _, executor := range executors {
		if executor.Kind == domain.WorkJobSequenceFrames {
			return executor.Version
		}
	}
	return ""
}

func sequenceExportExecutorVersion(executors []application.WorkExecutorRegistration) string {
	for _, executor := range executors {
		if executor.Kind == domain.WorkJobSequenceExport {
			return executor.Version
		}
	}
	return ""
}

func productResourceExecutorVersion(executors []application.WorkExecutorRegistration) string {
	for _, executor := range executors {
		if executor.Kind == application.WorkJobResourceAcquire {
			return executor.Version
		}
	}
	return ""
}

func reconcileSequencePreviewJobs(
	ctx context.Context,
	tx *sql.Tx,
	executorVersion string,
	now time.Time,
) error {
	at := formatInstant(now.UTC())
	if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'failed', terminal_error_code = 'input-job-failed', updated_at = ?
WHERE kind = 'sequence-preview' AND state IN ('blocked', 'queued')
  AND EXISTS (
    SELECT 1
    FROM sequence_preview_job_inputs input
    JOIN work_jobs producer ON producer.id = input.producer_job_id
    WHERE input.job_id = work_jobs.id AND producer.state IN ('failed', 'cancelled')
  )`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'failed', terminal_error_code = 'input-artifact-unavailable', updated_at = ?
WHERE kind = 'sequence-preview' AND state IN ('blocked', 'queued')
  AND EXISTS (
    SELECT 1
    FROM sequence_preview_job_inputs input
    JOIN work_jobs producer ON producer.id = input.producer_job_id
    JOIN media_job_details detail ON detail.job_id = producer.id
    LEFT JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
    WHERE input.job_id = work_jobs.id AND producer.state = 'succeeded'
      AND (artifact.id IS NULL OR artifact.state != 'ready')
  )`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM work_job_prerequisites
WHERE job_id IN (
  SELECT id FROM work_jobs
  WHERE kind = 'sequence-preview' AND state IN ('blocked', 'queued')
)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT DISTINCT input.job_id, 'artifact-ready', 'job', input.producer_job_id, ?
FROM sequence_preview_job_inputs input
JOIN work_jobs preview ON preview.id = input.job_id
JOIN work_jobs producer ON producer.id = input.producer_job_id
JOIN media_job_details detail ON detail.job_id = producer.id
LEFT JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE preview.state IN ('blocked', 'queued')
  AND NOT (producer.state = 'succeeded' AND artifact.state = 'ready')`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT job.id, 'executor-required', 'capability', 'work-executor/sequence-preview', ?
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE job.state IN ('blocked', 'queued')
  AND (? = '' OR detail.renderer_version != ?)`, at, executorVersion, executorVersion); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = CASE
      WHEN EXISTS (
        SELECT 1 FROM work_job_prerequisites prerequisite
        WHERE prerequisite.job_id = work_jobs.id
      ) THEN 'blocked'
      ELSE 'queued'
    END,
    terminal_error_code = NULL,
    updated_at = ?
WHERE kind = 'sequence-preview' AND state IN ('blocked', 'queued')`, at)
	return err
}

func (repository *SQLiteProjects) nextClaimableWorkJob(
	ctx context.Context,
	executors []application.WorkExecutorRegistration,
) (domain.WorkJobID, domain.WorkJobKind, error) {
	if err := validateWorkExecutorRegistrations(executors); err != nil {
		return domain.WorkJobID{}, "", err
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(executors)), ",")
	arguments := make([]any, 0, len(executors))
	for _, executor := range executors {
		arguments = append(arguments, string(executor.Kind))
	}
	var idValue, kindValue string
	err := repository.db.QueryRowContext(ctx, `
SELECT id, kind
FROM work_jobs
WHERE state = 'queued' AND cancellation_requested = 0
  AND kind IN (`+placeholders+`)
ORDER BY CASE priority_class WHEN 'interactive' THEN 0 WHEN 'foreground' THEN 1 ELSE 2 END,
         created_at, id
LIMIT 1`, arguments...).Scan(&idValue, &kindValue)
	if err != nil {
		return domain.WorkJobID{}, "", err
	}
	jobID, err := domain.ParseWorkJobID(idValue)
	if err != nil {
		return domain.WorkJobID{}, "", application.ErrSequencePreviewInvalid
	}
	return jobID, domain.WorkJobKind(kindValue), nil
}

func (repository *SQLiteProjects) claimSequencePreviewJob(
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
	var (
		projectValue, sequenceValue, parametersDigestValue, parametersJSON string
		resolverVersion, compilerVersion, rendererVersion                  string
		rendererTarget, outputProfile                                      string
		sequenceRevision, generation                                       uint64
	)
	err = tx.QueryRowContext(ctx, `
SELECT job.project_id, detail.sequence_id, detail.sequence_revision,
       detail.resolver_version, detail.compiler_version, detail.renderer_version,
       detail.renderer_target, detail.output_profile,
       job.parameters_digest, job.parameters_json,
       COALESCE((SELECT MAX(generation) FROM work_job_attempts WHERE job_id = job.id), 0) + 1
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.kind = 'sequence-preview' AND job.state = 'queued'
  AND job.cancellation_requested = 0 AND detail.renderer_version = ?
  AND NOT EXISTS (
    SELECT 1 FROM sequence_preview_job_inputs input
    JOIN work_jobs producer ON producer.id = input.producer_job_id
    JOIN media_job_details media_detail ON media_detail.job_id = producer.id
    LEFT JOIN media_artifacts artifact ON artifact.id = media_detail.result_artifact_id
    WHERE input.job_id = job.id
      AND NOT (producer.state = 'succeeded' AND artifact.state = 'ready')
  )`, jobID.String(), executorVersion).Scan(
		&projectValue, &sequenceValue, &sequenceRevision, &resolverVersion, &compilerVersion,
		&rendererVersion, &rendererTarget, &outputProfile, &parametersDigestValue,
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
	parameters, parametersErr := application.DecodeSequencePreviewJobParameters([]byte(parametersJSON))
	canonical, canonicalDigest, normalized, canonicalErr := application.CanonicalSequencePreviewJobParameters(parameters)
	if projectErr != nil || sequenceErr != nil || revisionErr != nil || digestErr != nil ||
		parametersErr != nil || canonicalErr != nil || digest != canonicalDigest ||
		!bytes.Equal(canonical, []byte(parametersJSON)) || normalized.ProjectID != projectID ||
		normalized.SequenceID != sequenceID || normalized.SequenceRevision != revision ||
		normalized.ResolverVersion != resolverVersion || normalized.CompilerVersion != compilerVersion ||
		normalized.RendererVersion != rendererVersion || normalized.RendererTarget != rendererTarget ||
		normalized.OutputProfile != outputProfile ||
		!json.Valid([]byte(parametersJSON)) {
		return application.WorkJobClaim{}, application.ErrSequencePreviewInvalid
	}
	if err := validateClaimedSequencePreviewPins(ctx, tx, jobID, normalized); err != nil {
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
	sequenceClaim := application.SequencePreviewJobClaim{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: revision,
		Parameters: normalized, ParametersDigest: digest, ParametersJSON: append([]byte(nil), canonical...),
	}
	return application.WorkJobClaim{
		JobID: jobID, AttemptID: input.AttemptID, Kind: domain.WorkJobSequencePreview,
		ExecutorVersion: executorVersion, Generation: generation, LeaseOwner: input.LeaseOwner,
		LeaseExpiresAt: input.Now.UTC().Add(input.LeaseDuration), SequencePreview: &sequenceClaim,
	}, nil
}

func validateClaimedSequencePreviewPins(
	ctx context.Context,
	tx *sql.Tx,
	jobID domain.WorkJobID,
	parameters application.SequencePreviewJobParameters,
) error {
	inputRows, err := tx.QueryContext(ctx, `
SELECT clip_id, source_stream_id, producer_job_id
FROM sequence_preview_job_inputs WHERE job_id = ? ORDER BY ordinal`, jobID.String())
	if err != nil {
		return err
	}
	storedInputs := make([]application.SequencePreviewInputRequirement, 0, len(parameters.Inputs))
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
			return application.ErrSequencePreviewInvalid
		}
		storedInputs = append(storedInputs, application.SequencePreviewInputRequirement{
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
FROM sequence_preview_job_resources WHERE job_id = ? ORDER BY ordinal`, jobID.String())
	if err != nil {
		return err
	}
	storedResources := make([]application.SequencePreviewResourcePin, 0, len(parameters.Resources))
	for resourceRows.Next() {
		var kind, id, version, digestValue string
		if err := resourceRows.Scan(&kind, &id, &version, &digestValue); err != nil {
			resourceRows.Close()
			return err
		}
		digest, parseErr := domain.ParseDigest(digestValue)
		if parseErr != nil {
			resourceRows.Close()
			return application.ErrSequencePreviewInvalid
		}
		storedResources = append(storedResources, application.SequencePreviewResourcePin{
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
		return application.ErrSequencePreviewInvalid
	}
	return nil
}

func sequencePreviewInputsEqual(
	left []application.SequencePreviewInputRequirement,
	right []application.SequencePreviewInputRequirement,
) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sequencePreviewResourcesEqual(
	left []application.SequencePreviewResourcePin,
	right []application.SequencePreviewResourcePin,
) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
