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

func (repository *SQLiteProjects) ReadSequenceFrameRate(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
) (domain.RationalTime, error) {
	if projectID.IsZero() || sequenceID.IsZero() || sequenceRevision.Value() == 0 {
		return domain.RationalTime{}, application.ErrSequenceFramesInvalid
	}
	var value int64
	var scale int32
	err := repository.db.QueryRowContext(ctx, `
SELECT frame_rate_value, frame_rate_scale
FROM sequences WHERE id = ? AND project_id = ? AND revision = ?`,
		sequenceID.String(), projectID.String(), sequenceRevision.Value(),
	).Scan(&value, &scale)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RationalTime{}, application.ErrEditConflict
	}
	if err != nil {
		return domain.RationalTime{}, err
	}
	frameRate, err := domain.NewRationalTime(value, scale)
	if err != nil || !frameRate.IsPositive() {
		return domain.RationalTime{}, application.ErrSequenceFramesInvalid
	}
	return frameRate, nil
}

func (repository *SQLiteProjects) RequestSequenceFrameSet(
	ctx context.Context,
	record application.RequestSequenceFrameSetRecord,
) (application.SequenceFrameSetResult, error) {
	if err := validateSequenceFrameSetRequest(record); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	defer tx.Rollback()
	if err := verifyActiveSequenceFrameTurn(ctx, tx, application.ReadSequenceFrameSetRecord{
		ProjectID: record.ProjectID, SequenceID: record.SequenceID, RunID: record.RunID,
		TurnID: record.TurnID, Actor: record.Actor, JobID: record.JobID,
	}, false); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if err := validateSequenceFrameRequestPins(ctx, tx, record); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	jobID := record.JobID
	var existing, storedDigest, storedJSON string
	err = tx.QueryRowContext(ctx, `
SELECT id, parameters_digest, parameters_json
FROM work_jobs
WHERE logical_key = ? AND kind = 'sequence-frame-set'
ORDER BY created_at DESC, id DESC LIMIT 1`, record.LogicalKey).Scan(&existing, &storedDigest, &storedJSON)
	created := false
	if err == nil {
		if storedDigest != record.ParametersDigest.String() || storedJSON != string(record.ParametersJSON) {
			return application.SequenceFrameSetResult{}, application.ErrSequenceFramesInvalid
		}
		jobID, err = domain.ParseWorkJobID(existing)
		if err != nil {
			return application.SequenceFrameSetResult{}, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		created = true
		at := formatInstant(record.RequestedAt.UTC())
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, kind, state, pool, priority_class, logical_key,
  parameters_digest, parameters_json, producer_version, created_at, updated_at
) VALUES (?, 'project', ?, 'sequence-frame-set', 'blocked', 'interactive-cpu', 'interactive', ?, ?, ?, ?, ?, ?)`,
			record.JobID.String(), record.ProjectID.String(), record.LogicalKey,
			record.ParametersDigest.String(), string(record.ParametersJSON),
			record.Parameters.ExecutorVersion, at, at,
		); err != nil {
			return application.SequenceFrameSetResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_frame_set_job_details (
  job_id, sequence_id, sequence_revision, preview_job_id,
  frame_rate_value, frame_rate_scale, grid_policy, profile
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, record.JobID.String(), record.SequenceID.String(),
			record.Parameters.SequenceRevision.Value(), record.Parameters.PreviewJobID.String(),
			record.Parameters.FrameRate.Value.Value(), record.Parameters.FrameRate.Scale,
			record.Parameters.GridPolicy, record.Parameters.Profile,
		); err != nil {
			return application.SequenceFrameSetResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'artifact-ready', 'job', ?, ?)`,
			record.JobID.String(), record.Parameters.PreviewJobID.String(), at,
		); err != nil {
			return application.SequenceFrameSetResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'work-executor/sequence-frame-set', ?)`,
			record.JobID.String(), at,
		); err != nil {
			return application.SequenceFrameSetResult{}, err
		}
	} else {
		return application.SequenceFrameSetResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
VALUES (?, 'run', ?, ?)`, jobID.String(), record.RunID.String(), formatInstant(record.RequestedAt.UTC())); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if created {
		if err := appendSequenceFrameRequestedActivity(ctx, tx, record); err != nil {
			return application.SequenceFrameSetResult{}, err
		}
	}
	result, err := loadSequenceFrameSetResult(ctx, tx, record.ProjectID, record.SequenceID, jobID)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	return result, nil
}

func (repository *SQLiteProjects) ReadSequenceFrameSet(
	ctx context.Context,
	record application.ReadSequenceFrameSetRecord,
) (application.SequenceFrameSetResult, error) {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.RunID.IsZero() ||
		record.TurnID.IsZero() || record.JobID.IsZero() || record.Actor.Validate() != nil ||
		record.Actor.Kind != domain.ActorAgent {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	defer tx.Rollback()
	if err := verifyActiveSequenceFrameTurn(ctx, tx, record, true); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	tailID, err := resolveSequenceFrameTailID(ctx, tx, record.ProjectID, record.SequenceID, record.JobID)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	result, err := loadSequenceFrameSetResult(ctx, tx, record.ProjectID, record.SequenceID, tailID)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	return result, nil
}

func resolveSequenceFrameTailID(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	jobID domain.WorkJobID,
) (domain.WorkJobID, error) {
	var value string
	err := tx.QueryRowContext(ctx, `
WITH RECURSIVE chain(id) AS (
  SELECT job.id
  FROM work_jobs job
  JOIN sequence_frame_set_job_details detail ON detail.job_id = job.id
  WHERE job.id = ? AND job.kind = 'sequence-frame-set'
    AND job.project_id = ? AND detail.sequence_id = ?
  UNION
  SELECT retry.id
  FROM work_jobs retry
  JOIN chain predecessor ON retry.retry_of_job_id = predecessor.id
  JOIN sequence_frame_set_job_details detail ON detail.job_id = retry.id
  WHERE retry.kind = 'sequence-frame-set' AND retry.project_id = ?
    AND detail.sequence_id = ?
)
SELECT job.id
FROM chain
JOIN work_jobs job ON job.id = chain.id
WHERE NOT EXISTS (
  SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id
)
LIMIT 1`, jobID.String(), projectID.String(), sequenceID.String(), projectID.String(), sequenceID.String()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkJobID{}, application.ErrSequenceFramesNotFound
	}
	if err != nil {
		return domain.WorkJobID{}, err
	}
	result, err := domain.ParseWorkJobID(value)
	if err != nil {
		return domain.WorkJobID{}, application.ErrSequenceFramesInvalid
	}
	return result, nil
}

func validateSequenceFrameSetRequest(record application.RequestSequenceFrameSetRecord) error {
	if record.JobID.IsZero() || record.ProjectID.IsZero() || record.SequenceID.IsZero() ||
		record.RunID.IsZero() || record.TurnID.IsZero() || record.Actor.Validate() != nil ||
		record.Actor.Kind != domain.ActorAgent || record.Parameters.Validate() != nil ||
		record.Parameters.ProjectID != record.ProjectID || record.Parameters.SequenceID != record.SequenceID ||
		record.ParametersDigest == "" || record.LogicalKey == "" || len(record.LogicalKey) > 1024 ||
		record.ActivityEventID.IsZero() || record.RequestedAt.IsZero() || !json.Valid(record.ParametersJSON) {
		return application.ErrSequenceFramesInvalid
	}
	canonical, digest, err := application.CanonicalSequenceFrameSetParameters(record.Parameters)
	if err != nil || !bytes.Equal(canonical, record.ParametersJSON) || digest != record.ParametersDigest {
		return application.ErrSequenceFramesInvalid
	}
	return nil
}

func validateSequenceFrameRequestPins(
	ctx context.Context,
	tx *sql.Tx,
	record application.RequestSequenceFrameSetRecord,
) error {
	var sequenceRevision uint64
	var frameRateValue int64
	var frameRateScale int32
	if err := tx.QueryRowContext(ctx, `
SELECT revision, frame_rate_value, frame_rate_scale
FROM sequences WHERE id = ? AND project_id = ?`, record.SequenceID.String(), record.ProjectID.String()).Scan(
		&sequenceRevision, &frameRateValue, &frameRateScale,
	); errors.Is(err, sql.ErrNoRows) {
		return application.ErrSequenceFramesNotFound
	} else if err != nil {
		return err
	}
	if sequenceRevision != record.Parameters.SequenceRevision.Value() ||
		frameRateValue != record.Parameters.FrameRate.Value.Value() || frameRateScale != record.Parameters.FrameRate.Scale {
		return application.ErrEditConflict
	}
	var previewProject, previewSequence string
	var previewRevision uint64
	if err := tx.QueryRowContext(ctx, `
SELECT job.project_id, detail.sequence_id, detail.sequence_revision
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.kind = 'sequence-preview'`, record.Parameters.PreviewJobID.String()).Scan(
		&previewProject, &previewSequence, &previewRevision,
	); errors.Is(err, sql.ErrNoRows) {
		return application.ErrSequenceFramesInvalid
	} else if err != nil {
		return err
	}
	if previewProject != record.ProjectID.String() || previewSequence != record.SequenceID.String() ||
		previewRevision != record.Parameters.SequenceRevision.Value() {
		return application.ErrSequenceFramesInvalid
	}
	return nil
}

func verifyActiveSequenceFrameTurn(
	ctx context.Context,
	tx *sql.Tx,
	record application.ReadSequenceFrameSetRecord,
	requireOwnership bool,
) error {
	var valid int
	query := `
SELECT 1
FROM agent_runs run
JOIN agent_turns turn ON turn.id = run.current_turn_id AND turn.run_id = run.id
WHERE run.id = ? AND run.project_id = ? AND run.actor_id = ?
  AND run.status IN ('active', 'waiting')
  AND turn.id = ? AND turn.status = 'active'`
	args := []any{record.RunID.String(), record.ProjectID.String(), record.Actor.IDString(), record.TurnID.String()}
	if requireOwnership {
		query += ` AND EXISTS (
  SELECT 1 FROM work_job_owners owner
  JOIN work_jobs job ON job.id = owner.job_id
  JOIN sequence_frame_set_job_details detail ON detail.job_id = job.id
  WHERE owner.job_id = ? AND owner.owner_kind = 'run' AND owner.owner_id = run.id
    AND job.project_id = run.project_id AND detail.sequence_id = ?
)`
		args = append(args, record.JobID.String(), record.SequenceID.String())
	}
	err := tx.QueryRowContext(ctx, query, args...).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrRunStaleTurn
	}
	return err
}

func loadSequenceFrameSetResult(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	jobID domain.WorkJobID,
) (application.SequenceFrameSetResult, error) {
	var state, parametersJSON, previewJobValue, createdValue, updatedValue string
	var terminalError, artifactValue sql.NullString
	var sequenceRevision uint64
	var progress uint16
	err := tx.QueryRowContext(ctx, `
SELECT job.state, job.progress_basis_points, job.parameters_json,
       job.terminal_error_code, job.created_at, job.updated_at,
       detail.sequence_revision, detail.preview_job_id, detail.result_artifact_id
FROM work_jobs job
JOIN sequence_frame_set_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.project_id = ? AND detail.sequence_id = ?
  AND job.kind = 'sequence-frame-set'`, jobID.String(), projectID.String(), sequenceID.String()).Scan(
		&state, &progress, &parametersJSON, &terminalError, &createdValue, &updatedValue,
		&sequenceRevision, &previewJobValue, &artifactValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesNotFound
	}
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	parameters, err := application.DecodeSequenceFrameSetParameters([]byte(parametersJSON))
	if err != nil || parameters.ProjectID != projectID || parameters.SequenceID != sequenceID ||
		parameters.SequenceRevision.Value() != sequenceRevision || parameters.PreviewJobID.String() != previewJobValue {
		return application.SequenceFrameSetResult{}, application.ErrSequenceFramesInvalid
	}
	revision, _ := domain.NewRevision(sequenceRevision)
	previewJobID, err := domain.ParseWorkJobID(previewJobValue)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdValue)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedValue)
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	job := application.SequenceFrameJob{
		ID: jobID, Kind: domain.WorkJobSequenceFrames, State: domain.WorkJobState(state),
		ProgressBasisPoints: progress, PreviewJobID: previewJobID,
		CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
	if terminalError.Valid {
		job.TerminalErrorCode = &terminalError.String
	}
	result := application.SequenceFrameSetResult{
		Status: application.SequenceFrameSetAccepted, ProjectID: projectID, SequenceID: sequenceID,
		SequenceRevision: revision, Profile: parameters.Profile, Job: job,
		Recovery:  application.MediaRecoveryNone,
		Samples:   append([]application.SequenceFrameCoordinate(nil), parameters.Samples...),
		Resources: []application.SequenceFrameResourceLease{},
	}
	if artifactValue.Valid {
		artifactID, err := domain.ParseArtifactID(artifactValue.String)
		if err != nil {
			return application.SequenceFrameSetResult{}, err
		}
		result.ArtifactID = &artifactID
		result.Job.ResultArtifactID = &artifactID
	}
	switch job.State {
	case domain.MediaJobSucceeded:
		if result.ArtifactID == nil {
			return application.SequenceFrameSetResult{}, application.ErrSequenceFramesInvalid
		}
		var artifactState string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM sequence_frame_set_artifacts WHERE id = ?`,
			result.ArtifactID.String()).Scan(&artifactState); err != nil {
			return application.SequenceFrameSetResult{}, err
		}
		if artifactState == "ready" {
			result.Status = application.SequenceFrameSetReady
		} else {
			result.Status = application.SequenceFrameSetFailed
			code := "frame-artifact-unavailable"
			result.Job.TerminalErrorCode = &code
			result.Recovery = application.MediaRecoveryRetryJob
		}
	case domain.MediaJobFailed, domain.MediaJobCancelled:
		result.Status = application.SequenceFrameSetFailed
		result.Recovery = application.SequenceFrameRecoveryAction(result.Job)
		if result.Job.TerminalErrorCode != nil &&
			(*result.Job.TerminalErrorCode == "input-job-failed" ||
				*result.Job.TerminalErrorCode == "input-artifact-unavailable") {
			previewTail, resolveErr := resolveSequencePreviewTailID(
				ctx, tx, projectID, sequenceID, revision, parameters.PreviewJobID,
			)
			if resolveErr != nil {
				return application.SequenceFrameSetResult{}, resolveErr
			}
			preview, loadErr := loadSequencePreviewJobProjection(ctx, tx, previewTail)
			if loadErr != nil {
				return application.SequenceFrameSetResult{}, loadErr
			}
			switch preview.State {
			case domain.MediaJobBlocked, domain.MediaJobQueued, domain.MediaJobRunning, domain.MediaJobSucceeded:
				if preview.ID != parameters.PreviewJobID {
					result.Recovery = application.MediaRecoveryRetryJob
				}
			case domain.MediaJobFailed, domain.MediaJobCancelled:
				result.Recovery = application.SequencePreviewRecoveryAction(preview)
			}
		}
	}
	cursor, err := loadActivityHead(ctx, tx, "project", projectID.String())
	if err != nil {
		return application.SequenceFrameSetResult{}, err
	}
	result.ActivityCursor = cursor
	return result, nil
}

func resolveSequencePreviewTailID(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
) (domain.WorkJobID, error) {
	var value string
	err := tx.QueryRowContext(ctx, `
WITH RECURSIVE chain(id) AS (
  SELECT job.id
  FROM work_jobs job
  JOIN sequence_preview_job_details detail ON detail.job_id = job.id
  WHERE job.id = ? AND job.kind = 'sequence-preview' AND job.project_id = ?
    AND detail.sequence_id = ? AND detail.sequence_revision = ?
  UNION
  SELECT retry.id
  FROM work_jobs retry
  JOIN chain predecessor ON retry.retry_of_job_id = predecessor.id
  JOIN sequence_preview_job_details detail ON detail.job_id = retry.id
  WHERE retry.kind = 'sequence-preview' AND retry.project_id = ?
    AND detail.sequence_id = ? AND detail.sequence_revision = ?
)
SELECT job.id
FROM chain
JOIN work_jobs job ON job.id = chain.id
WHERE NOT EXISTS (
  SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id
)
LIMIT 1`, jobID.String(), projectID.String(), sequenceID.String(), sequenceRevision.Value(),
		projectID.String(), sequenceID.String(), sequenceRevision.Value()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkJobID{}, application.ErrSequencePreviewNotFound
	}
	if err != nil {
		return domain.WorkJobID{}, err
	}
	result, err := domain.ParseWorkJobID(value)
	if err != nil {
		return domain.WorkJobID{}, application.ErrSequencePreviewInvalid
	}
	return result, nil
}

func appendSequenceFrameRequestedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.RequestSequenceFrameSetRecord,
) error {
	var revisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, record.ProjectID.String()).Scan(&revisionValue); err != nil {
		return err
	}
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		JobID             domain.WorkJobID               `json:"jobId"`
		PreviewJobID      domain.WorkJobID               `json:"previewJobId"`
		RunID             domain.RunID                   `json:"runId"`
		TurnID            domain.TurnID                  `json:"turnId"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{},
		JobID:             record.JobID, PreviewJobID: record.Parameters.PreviewJobID,
		RunID: record.RunID, TurnID: record.TurnID,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.ProjectID.String(), EventID: record.ActivityEventID.String(),
		Kind: "sequence.frame-set-requested", OccurredAt: formatInstant(record.RequestedAt.UTC()),
		ActorKind: string(record.Actor.Kind), ActorID: record.Actor.IDString(),
		ProjectID: record.ProjectID.String(), ProjectRevision: int64(revision.Value()),
		OutcomeKind: "work-job", OutcomeID: record.JobID.String(),
		SummaryCode: "sequence-frame-set-requested", Payload: payload,
	})
	return err
}
