package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func reconcileSequenceFrameJobs(
	ctx context.Context,
	tx *sql.Tx,
	executorVersion string,
	now time.Time,
) error {
	at := formatInstant(now.UTC())
	if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'failed', terminal_error_code = 'input-job-failed', updated_at = ?
WHERE kind = 'sequence-frame-set' AND state IN ('blocked', 'queued')
  AND EXISTS (
    SELECT 1 FROM sequence_frame_set_job_details detail
    JOIN work_jobs preview ON preview.id = detail.preview_job_id
    WHERE detail.job_id = work_jobs.id AND preview.state IN ('failed', 'cancelled')
  )`, at); err != nil {
		return err
	}
	if err := bindReadySequenceFrameInputs(ctx, tx, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM work_job_prerequisites
WHERE job_id IN (
  SELECT id FROM work_jobs
  WHERE kind = 'sequence-frame-set' AND state IN ('blocked', 'queued')
)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT detail.job_id, 'artifact-ready', 'job', detail.preview_job_id, ?
FROM sequence_frame_set_job_details detail
JOIN work_jobs frame ON frame.id = detail.job_id
WHERE frame.state IN ('blocked', 'queued') AND detail.preview_artifact_id IS NULL`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT job.id, 'executor-required', 'capability', 'work-executor/sequence-frame-set', ?
FROM work_jobs job
WHERE job.kind = 'sequence-frame-set' AND job.state IN ('blocked', 'queued')
  AND (? = '' OR job.producer_version != ?)`, at, executorVersion, executorVersion); err != nil {
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
WHERE kind = 'sequence-frame-set' AND state IN ('blocked', 'queued')`, at)
	return err
}

func bindReadySequenceFrameInputs(ctx context.Context, tx *sql.Tx, at string) error {
	rows, err := tx.QueryContext(ctx, `
SELECT frame.id, frame.parameters_json,
       detail.sequence_id, detail.sequence_revision, detail.preview_job_id,
       artifact.id, artifact.content_digest, artifact.render_plan_digest, artifact.facts_json
FROM work_jobs frame
JOIN sequence_frame_set_job_details detail ON detail.job_id = frame.id
JOIN work_jobs preview ON preview.id = detail.preview_job_id
JOIN sequence_preview_job_details preview_detail ON preview_detail.job_id = preview.id
JOIN sequence_preview_artifacts artifact ON artifact.id = preview_detail.result_artifact_id
WHERE frame.kind = 'sequence-frame-set' AND frame.state IN ('blocked', 'queued')
  AND detail.preview_artifact_id IS NULL
  AND preview.state = 'succeeded' AND artifact.state = 'ready'
ORDER BY frame.created_at, frame.id`)
	if err != nil {
		return err
	}
	type candidate struct {
		jobID, parametersJSON, sequenceID, previewJobID   string
		artifactID, artifactDigest, planDigest, factsJSON string
		sequenceRevision                                  uint64
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var value candidate
		if err := rows.Scan(
			&value.jobID, &value.parametersJSON, &value.sequenceID, &value.sequenceRevision,
			&value.previewJobID, &value.artifactID, &value.artifactDigest, &value.planDigest, &value.factsJSON,
		); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, value)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, candidate := range candidates {
		parameters, parametersErr := application.DecodeSequenceFrameSetParameters([]byte(candidate.parametersJSON))
		var facts domain.SequencePreviewMediaFacts
		factsErr := json.Unmarshal([]byte(candidate.factsJSON), &facts)
		valid := parametersErr == nil && factsErr == nil && application.ValidateSequencePreviewFacts(facts) == nil &&
			parameters.SequenceID.String() == candidate.sequenceID &&
			parameters.SequenceRevision.Value() == candidate.sequenceRevision &&
			parameters.PreviewJobID.String() == candidate.previewJobID
		if valid {
			comparison, compareErr := parameters.FrameRate.Compare(facts.FrameRate)
			valid = compareErr == nil && comparison == 0
		}
		outOfRange := false
		if valid {
			for _, sample := range parameters.Samples {
				comparison, compareErr := sample.RequestedTime.Compare(facts.PresentationDuration)
				if compareErr != nil || comparison >= 0 || sample.FrameIndex.Value() >= facts.VideoFrameCount.Value() {
					outOfRange = true
					break
				}
			}
		}
		if !valid || outOfRange {
			code := "input-artifact-invalid"
			if valid && outOfRange {
				code = "sequence-time-out-of-range"
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs SET state = 'failed', terminal_error_code = ?, updated_at = ?
WHERE id = ? AND state IN ('blocked', 'queued')`, code, at, candidate.jobID); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE sequence_frame_set_job_details
SET preview_artifact_id = ?, preview_artifact_digest = ?, render_plan_digest = ?
WHERE job_id = ? AND preview_artifact_id IS NULL`, candidate.artifactID,
			candidate.artifactDigest, candidate.planDigest, candidate.jobID); err != nil {
			return err
		}
	}
	return nil
}

func (repository *SQLiteProjects) claimSequenceFrameJob(
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
	var previewArtifactValue, previewDigestValue, planDigestValue string
	var sequenceRevision, generation uint64
	err = tx.QueryRowContext(ctx, `
SELECT job.project_id, detail.sequence_id, detail.sequence_revision,
       job.parameters_digest, job.parameters_json,
       detail.preview_artifact_id, detail.preview_artifact_digest, detail.render_plan_digest,
       COALESCE((SELECT MAX(generation) FROM work_job_attempts WHERE job_id = job.id), 0) + 1
FROM work_jobs job
JOIN sequence_frame_set_job_details detail ON detail.job_id = job.id
JOIN sequence_preview_artifacts preview ON preview.id = detail.preview_artifact_id
WHERE job.id = ? AND job.kind = 'sequence-frame-set' AND job.state = 'queued'
  AND job.cancellation_requested = 0 AND job.producer_version = ?
  AND preview.state = 'ready' AND preview.content_digest = detail.preview_artifact_digest
  AND preview.render_plan_digest = detail.render_plan_digest`, jobID.String(), executorVersion).Scan(
		&projectValue, &sequenceValue, &sequenceRevision, &parametersDigestValue, &parametersJSON,
		&previewArtifactValue, &previewDigestValue, &planDigestValue, &generation,
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
	parameters, parametersErr := application.DecodeSequenceFrameSetParameters([]byte(parametersJSON))
	canonical, canonicalDigest, canonicalErr := application.CanonicalSequenceFrameSetParameters(parameters)
	previewArtifactID, artifactErr := domain.ParseArtifactID(previewArtifactValue)
	previewDigest, previewDigestErr := domain.ParseDigest(previewDigestValue)
	planDigest, planDigestErr := domain.ParseDigest(planDigestValue)
	if projectErr != nil || sequenceErr != nil || revisionErr != nil || digestErr != nil ||
		parametersErr != nil || canonicalErr != nil || artifactErr != nil || previewDigestErr != nil ||
		planDigestErr != nil || digest != canonicalDigest || !bytes.Equal(canonical, []byte(parametersJSON)) ||
		parameters.ProjectID != projectID || parameters.SequenceID != sequenceID ||
		parameters.SequenceRevision != revision || parameters.ExecutorVersion != executorVersion {
		return application.WorkJobClaim{}, application.ErrSequenceFramesInvalid
	}
	previewArtifact, err := loadSequencePreviewArtifactSummary(ctx, tx, previewArtifactID)
	if err != nil || previewArtifact.ContentDigest != previewDigest ||
		previewArtifact.RenderPlanDigest != planDigest || previewArtifact.ProjectID != projectID ||
		previewArtifact.SequenceID != sequenceID || previewArtifact.SequenceRevision != revision ||
		previewArtifact.State != domain.SequencePreviewArtifactReady {
		return application.WorkJobClaim{}, application.ErrSequenceFramesInvalid
	}
	now := formatInstant(input.Now.UTC())
	expires := formatInstant(input.Now.UTC().Add(input.LeaseDuration))
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_attempts (
  id, job_id, generation, state, lease_owner, lease_expires_at, heartbeat_at,
  started_at, executor_version, temporary_output_identity
) VALUES (?, ?, ?, 'running', ?, ?, ?, ?, ?, ?)`, input.AttemptID.String(), jobID.String(), generation,
		input.LeaseOwner, expires, now, now, executorVersion, input.AttemptID.String()); err != nil {
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
	frameClaim := application.SequenceFrameJobClaim{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: revision,
		Parameters: parameters, ParametersDigest: digest, ParametersJSON: append([]byte(nil), canonical...),
		PreviewArtifact: previewArtifact,
	}
	return application.WorkJobClaim{
		JobID: jobID, AttemptID: input.AttemptID, Kind: domain.WorkJobSequenceFrames,
		ExecutorVersion: executorVersion, Generation: generation, LeaseOwner: input.LeaseOwner,
		LeaseExpiresAt: input.Now.UTC().Add(input.LeaseDuration), SequenceFrames: &frameClaim,
	}, nil
}
