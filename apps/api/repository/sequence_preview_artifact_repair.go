package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) RejectSequencePreviewArtifact(
	ctx context.Context,
	record application.RejectSequencePreviewArtifactRecord,
) (application.SequencePreviewJobProjection, error) {
	if err := validateSequencePreviewRejection(record); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()

	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-preview")
	canonicalRoot := filepath.Join(artifactRoot, record.ArtifactID.String())
	evictionRoot := filepath.Join(repository.dataDir, "work", "sequence-preview-evictions")
	eventRoot := filepath.Join(evictionRoot, record.EventID.String())
	quarantineRoot := filepath.Join(eventRoot, record.ArtifactID.String())
	quarantined := false
	if _, err := os.Lstat(canonicalRoot); err == nil {
		if err := os.MkdirAll(eventRoot, 0o700); err != nil {
			return application.SequencePreviewJobProjection{}, err
		}
		if err := os.Rename(canonicalRoot, quarantineRoot); err != nil {
			return application.SequencePreviewJobProjection{}, err
		}
		quarantined = true
		if err := syncDirectory(artifactRoot); err != nil {
			return application.SequencePreviewJobProjection{}, repository.restoreRejectedSequencePreview(
				canonicalRoot, eventRoot, quarantineRoot, err,
			)
		}
		if err := syncDirectory(eventRoot); err != nil {
			return application.SequencePreviewJobProjection{}, repository.restoreRejectedSequencePreview(
				canonicalRoot, eventRoot, quarantineRoot, err,
			)
		}
	} else if !os.IsNotExist(err) {
		return application.SequencePreviewJobProjection{}, err
	}

	restoreOnError := func(cause error) (application.SequencePreviewJobProjection, error) {
		if !quarantined {
			return application.SequencePreviewJobProjection{}, cause
		}
		return application.SequencePreviewJobProjection{}, repository.restoreRejectedSequencePreview(
			canonicalRoot, eventRoot, quarantineRoot, cause,
		)
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return restoreOnError(err)
	}
	defer tx.Rollback()
	var jobState, jobKind, resultArtifact, artifactState string
	var projectValue, sequenceValue, planValue string
	var sequenceRevision uint64
	err = tx.QueryRowContext(ctx, `
SELECT job.state, job.kind, job.project_id, detail.sequence_id, detail.sequence_revision,
       detail.render_plan_digest, detail.result_artifact_id, artifact.state
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
JOIN sequence_preview_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE job.id = ? AND job.project_id = ? AND detail.sequence_id = ?
  AND detail.sequence_revision = ? AND artifact.id = ?
  AND artifact.project_id = job.project_id AND artifact.sequence_id = detail.sequence_id
  AND artifact.sequence_revision = detail.sequence_revision
  AND artifact.render_plan_digest = detail.render_plan_digest`,
		record.JobID.String(), record.ProjectID.String(), record.SequenceID.String(),
		record.SequenceRevision.Value(), record.ArtifactID.String(),
	).Scan(
		&jobState, &jobKind, &projectValue, &sequenceValue, &sequenceRevision,
		&planValue, &resultArtifact, &artifactState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return restoreOnError(application.ErrSequencePreviewInvalid)
	}
	if err != nil {
		return restoreOnError(err)
	}
	if jobState != string(domain.MediaJobSucceeded) || jobKind != string(domain.WorkJobSequencePreview) ||
		projectValue != record.ProjectID.String() || sequenceValue != record.SequenceID.String() ||
		sequenceRevision != record.SequenceRevision.Value() || planValue == "" ||
		resultArtifact != record.ArtifactID.String() ||
		artifactState != string(domain.SequencePreviewArtifactReady) {
		return restoreOnError(application.ErrSequencePreviewInvalid)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE sequence_preview_artifacts SET state = 'evicted'
WHERE id = ? AND state = 'ready'`, record.ArtifactID.String())
	if err != nil {
		return restoreOnError(err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return restoreOnError(application.ErrSequencePreviewInvalid)
	}
	at := formatInstant(record.RejectedAt.UTC())
	result, err = tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, installation_id, kind, state, pool, priority_class,
  logical_key, parameters_digest, parameters_json, producer_version,
  progress_basis_points, cancellation_requested, retry_of_job_id, created_at,
  updated_at, terminal_error_code
)
SELECT ?, scope_kind, project_id, installation_id, kind, 'blocked', pool, priority_class,
       logical_key, parameters_digest, parameters_json, producer_version,
       0, 0, id, ?, ?, NULL
FROM work_jobs WHERE id = ? AND state = 'succeeded'`,
		record.RetryJobID.String(), at, at, record.JobID.String(),
	)
	if err != nil {
		return restoreOnError(err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return restoreOnError(application.ErrSequencePreviewInvalid)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_details (
  job_id, sequence_id, sequence_revision, resolver_version, compiler_version,
  renderer_version, renderer_target, output_profile,
  render_intent_schema, render_intent_digest, render_intent_json, render_plan_digest
)
SELECT ?, sequence_id, sequence_revision, resolver_version, compiler_version,
       renderer_version, renderer_target, output_profile,
       render_intent_schema, render_intent_digest, render_intent_json, render_plan_digest
FROM sequence_preview_job_details
WHERE job_id = ? AND result_artifact_id = ? AND render_plan_digest IS NOT NULL`,
		record.RetryJobID.String(), record.JobID.String(), record.ArtifactID.String(),
	); err != nil {
		return restoreOnError(err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_inputs (job_id, ordinal, clip_id, source_stream_id, producer_job_id)
SELECT ?, ordinal, clip_id, source_stream_id, producer_job_id
FROM sequence_preview_job_inputs WHERE job_id = ?`,
		record.RetryJobID.String(), record.JobID.String(),
	); err != nil {
		return restoreOnError(err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_resources (
  job_id, ordinal, resource_kind, resource_id, resource_version, resource_digest
)
SELECT ?, ordinal, resource_kind, resource_id, resource_version, resource_digest
FROM sequence_preview_job_resources WHERE job_id = ?`,
		record.RetryJobID.String(), record.JobID.String(),
	); err != nil {
		return restoreOnError(err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
SELECT ?, owner_kind, owner_id, ? FROM work_job_owners WHERE job_id = ?`,
		record.RetryJobID.String(), at, record.JobID.String(),
	); err != nil {
		return restoreOnError(err)
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
		record.RetryJobID.String(), at, record.RetryJobID.String(),
	); err != nil {
		return restoreOnError(err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'work-executor/sequence-preview', ?)`,
		record.RetryJobID.String(), at,
	); err != nil {
		return restoreOnError(err)
	}
	if err := appendSequencePreviewArtifactRejectedActivity(ctx, tx, record); err != nil {
		return restoreOnError(err)
	}
	retry, err := loadSequencePreviewJobProjection(ctx, tx, record.RetryJobID)
	if err != nil {
		return restoreOnError(err)
	}
	if err := tx.Commit(); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if quarantined {
		_ = os.RemoveAll(eventRoot)
		_ = syncDirectory(evictionRoot)
	}
	return retry, nil
}

func validateSequencePreviewRejection(record application.RejectSequencePreviewArtifactRecord) error {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.SequenceRevision.Value() == 0 ||
		record.ArtifactID.IsZero() || record.JobID.IsZero() || record.RetryJobID.IsZero() ||
		record.JobID == record.RetryJobID || record.EventID.IsZero() || record.RejectedAt.IsZero() ||
		record.Code != application.MediaDiagnosticSequenceIntegrityRejected {
		return application.ErrSequencePreviewInvalid
	}
	return nil
}

func (repository *SQLiteProjects) restoreRejectedSequencePreview(
	canonicalRoot string,
	eventRoot string,
	quarantineRoot string,
	cause error,
) error {
	if _, err := os.Lstat(canonicalRoot); !os.IsNotExist(err) {
		return fmt.Errorf("reject sequence preview artifact: %w; restore destination is not empty", cause)
	}
	if err := os.Rename(quarantineRoot, canonicalRoot); err != nil {
		return fmt.Errorf("reject sequence preview artifact: %w; restore: %v", cause, err)
	}
	_ = os.Remove(eventRoot)
	_ = syncDirectory(filepath.Dir(canonicalRoot))
	return cause
}

func appendSequencePreviewArtifactRejectedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.RejectSequencePreviewArtifactRecord,
) error {
	var projectRevisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`,
		record.ProjectID.String(),
	).Scan(&projectRevisionValue); err != nil {
		return err
	}
	projectRevision, err := domain.NewRevision(projectRevisionValue)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef  `json:"changedEntityRefs"`
		ArtifactID        domain.ArtifactID               `json:"artifactId"`
		RejectedJobID     domain.WorkJobID                `json:"rejectedJobId"`
		RetryJobID        domain.WorkJobID                `json:"retryJobId"`
		SequenceID        domain.SequenceID               `json:"sequenceId"`
		SequenceRevision  domain.Revision                 `json:"sequenceRevision"`
		FailureCode       application.MediaDiagnosticCode `json:"failureCode"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{}, ArtifactID: record.ArtifactID,
		RejectedJobID: record.JobID, RetryJobID: record.RetryJobID,
		SequenceID: record.SequenceID, SequenceRevision: record.SequenceRevision,
		FailureCode: record.Code,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.ProjectID.String(), EventID: record.EventID.String(),
		Kind: "sequence.preview-artifact-rejected", OccurredAt: formatInstant(record.RejectedAt.UTC()),
		ProjectID: record.ProjectID.String(), ProjectRevision: int64(projectRevision.Value()),
		OutcomeKind: "sequence-preview-job", OutcomeID: record.RetryJobID.String(),
		SummaryCode: "sequence-preview-artifact-rejected", Payload: payload,
	})
	return err
}
