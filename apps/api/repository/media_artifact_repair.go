package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) RejectSourceProxyArtifact(
	ctx context.Context,
	record application.RejectSourceProxyArtifactRecord,
) (domain.MediaJobSummary, error) {
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	liveLease, err := repository.hasLiveRenderMaterialLease(ctx, record.ArtifactID, record.RejectedAt)
	if err != nil {
		return domain.MediaJobSummary{}, err
	}
	if liveLease {
		return domain.MediaJobSummary{}, application.ErrAssetInvalid
	}

	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "media")
	canonicalRoot := filepath.Join(artifactRoot, record.ArtifactID.String())
	evictionRoot := filepath.Join(repository.dataDir, "work", "media-evictions")
	eventRoot := filepath.Join(evictionRoot, record.EventID.String())
	quarantineRoot := filepath.Join(eventRoot, record.ArtifactID.String())
	quarantined := false
	if _, err := os.Lstat(canonicalRoot); err == nil {
		if err := os.MkdirAll(eventRoot, 0o700); err != nil {
			return domain.MediaJobSummary{}, err
		}
		if err := os.Rename(canonicalRoot, quarantineRoot); err != nil {
			return domain.MediaJobSummary{}, err
		}
		quarantined = true
		if err := syncDirectory(artifactRoot); err != nil {
			return domain.MediaJobSummary{}, repository.restoreRejectedArtifact(canonicalRoot, eventRoot, quarantineRoot, err)
		}
		if err := syncDirectory(eventRoot); err != nil {
			return domain.MediaJobSummary{}, repository.restoreRejectedArtifact(canonicalRoot, eventRoot, quarantineRoot, err)
		}
	} else if !os.IsNotExist(err) {
		return domain.MediaJobSummary{}, err
	}

	restoreOnError := func(cause error) (domain.MediaJobSummary, error) {
		if !quarantined {
			return domain.MediaJobSummary{}, cause
		}
		return domain.MediaJobSummary{}, repository.restoreRejectedArtifact(
			canonicalRoot, eventRoot, quarantineRoot, cause,
		)
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return restoreOnError(err)
	}
	defer tx.Rollback()
	var jobState, jobKind, resultArtifact, artifactState, artifactKind string
	err = tx.QueryRowContext(ctx, `
SELECT job.state, job.kind, detail.result_artifact_id, artifact.state, artifact.kind
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE job.id = ? AND job.project_id = ? AND detail.asset_id = ?
  AND artifact.id = ? AND artifact.project_id = job.project_id AND artifact.asset_id = detail.asset_id`,
		record.JobID.String(), record.ProjectID.String(), record.AssetID.String(), record.ArtifactID.String(),
	).Scan(&jobState, &jobKind, &resultArtifact, &artifactState, &artifactKind)
	if errors.Is(err, sql.ErrNoRows) {
		return restoreOnError(application.ErrAssetInvalid)
	}
	if err != nil {
		return restoreOnError(err)
	}
	if jobState != string(domain.MediaJobSucceeded) ||
		jobKind != string(domain.MediaJobProxy) || resultArtifact != record.ArtifactID.String() ||
		artifactState != string(domain.ArtifactReady) || artifactKind != string(domain.ArtifactProxy) {
		return restoreOnError(application.ErrAssetInvalid)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE media_artifacts SET state = 'evicted'
WHERE id = ? AND state = 'ready' AND NOT EXISTS (
  SELECT 1
  FROM render_material_leases lease
  JOIN work_job_attempts attempt ON attempt.id = lease.attempt_id
  JOIN work_jobs job ON job.id = attempt.job_id
  WHERE lease.artifact_id = media_artifacts.id
    AND attempt.state IN ('leased', 'running', 'publishing')
    AND attempt.lease_expires_at > ? AND job.state = 'running'
)`, record.ArtifactID.String(), formatInstant(record.RejectedAt.UTC()))
	if err != nil {
		return restoreOnError(err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return restoreOnError(application.ErrAssetInvalid)
	}
	at := formatInstant(record.RejectedAt.UTC())
	result, err = tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, installation_id, kind, state, pool, priority_class, logical_key,
  parameters_digest, parameters_json, producer_version, progress_basis_points,
  cancellation_requested, retry_of_job_id, created_at, updated_at,
  terminal_error_code
)
SELECT ?, scope_kind, project_id, installation_id, kind, 'blocked', pool, priority_class, logical_key,
       parameters_digest, parameters_json, producer_version, 0,
       0, id, ?, ?, NULL
FROM work_jobs
WHERE id = ? AND state = 'succeeded'`,
		record.RetryJobID.String(), at, at, record.JobID.String(),
	)
	if err != nil {
		return restoreOnError(err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return restoreOnError(application.ErrAssetInvalid)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO media_job_details (job_id, asset_id)
SELECT ?, asset_id FROM media_job_details
WHERE job_id = ? AND result_artifact_id = ?`,
		record.RetryJobID.String(), record.JobID.String(), record.ArtifactID.String(),
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
VALUES (?, 'executor-required', 'capability', 'media-executor/proxy', ?)`,
		record.RetryJobID.String(), at,
	); err != nil {
		return restoreOnError(err)
	}
	if err := appendProxyArtifactRejectedActivity(ctx, tx, record); err != nil {
		return restoreOnError(err)
	}
	retry, err := loadMediaJobSummary(ctx, tx, record.RetryJobID)
	if err != nil {
		return restoreOnError(err)
	}
	if err := tx.Commit(); err != nil {
		// A failed commit can be ambiguous. Leave recognized quarantine work for
		// startup reconciliation instead of risking restoring committed bytes.
		return domain.MediaJobSummary{}, err
	}
	if quarantined {
		_ = os.RemoveAll(eventRoot)
		_ = syncDirectory(evictionRoot)
	}
	return retry, nil
}

func (repository *SQLiteProjects) hasLiveRenderMaterialLease(
	ctx context.Context,
	artifactID domain.ArtifactID,
	at time.Time,
) (bool, error) {
	if artifactID.IsZero() || at.IsZero() {
		return false, application.ErrAssetInvalid
	}
	var live int
	err := repository.db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM render_material_leases lease
  JOIN work_job_attempts attempt ON attempt.id = lease.attempt_id
  JOIN work_jobs job ON job.id = attempt.job_id
  WHERE lease.artifact_id = ?
    AND attempt.state IN ('leased', 'running', 'publishing')
    AND attempt.lease_expires_at > ? AND job.state = 'running'
)`, artifactID.String(), formatInstant(at.UTC())).Scan(&live)
	return live == 1, err
}

func (repository *SQLiteProjects) restoreRejectedArtifact(
	canonicalRoot string,
	eventRoot string,
	quarantineRoot string,
	cause error,
) error {
	if _, err := os.Lstat(canonicalRoot); !os.IsNotExist(err) {
		return fmt.Errorf("reject source proxy artifact: %w; restore destination is not empty", cause)
	}
	if err := os.Rename(quarantineRoot, canonicalRoot); err != nil {
		return fmt.Errorf("reject source proxy artifact: %w; restore: %v", cause, err)
	}
	_ = os.Remove(eventRoot)
	_ = syncDirectory(filepath.Dir(canonicalRoot))
	return cause
}

func appendProxyArtifactRejectedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.RejectSourceProxyArtifactRecord,
) error {
	var projectRevisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, record.ProjectID.String()).Scan(&projectRevisionValue); err != nil {
		return err
	}
	projectRevision, err := domain.NewRevision(projectRevisionValue)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef  `json:"changedEntityRefs"`
		ArtifactID        domain.ArtifactID               `json:"artifactId"`
		RejectedJobID     domain.MediaJobID               `json:"rejectedJobId"`
		RetryJobID        domain.MediaJobID               `json:"retryJobId"`
		FailureCode       application.MediaDiagnosticCode `json:"failureCode"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{{
			Kind: "asset-media-state", ID: record.AssetID.String(), Revision: projectRevision,
		}},
		ArtifactID: record.ArtifactID, RejectedJobID: record.JobID,
		RetryJobID: record.RetryJobID, FailureCode: record.Code,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.ProjectID.String(), EventID: record.EventID.String(),
		Kind: "media.artifact-rejected", OccurredAt: formatInstant(record.RejectedAt.UTC()),
		ActorKind: nil, ActorID: nil, ProjectID: record.ProjectID.String(),
		ProjectRevision: int64(projectRevision.Value()), OutcomeKind: "media-job",
		OutcomeID: record.RetryJobID.String(), SummaryCode: "media-artifact-rejected", Payload: payload,
	})
	return err
}
