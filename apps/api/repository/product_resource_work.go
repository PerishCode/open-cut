package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func reconcileProductResourceJobs(
	ctx context.Context,
	tx *sql.Tx,
	executorVersion string,
	resources []application.ProductResourceRegistration,
	now time.Time,
) error {
	if application.ValidateProductResourceRegistrations(resources) != nil {
		return application.ErrProductResourceInvalid
	}
	active := make(map[string]application.ProductResourceRegistration, len(resources))
	for _, resource := range resources {
		active[resource.Name] = resource
	}
	rows, err := tx.QueryContext(ctx, `
SELECT job.id, job.producer_version, detail.catalog_entry_id,
       detail.resource_profile, detail.entry_digest
FROM work_jobs job
JOIN resource_job_details detail ON detail.job_id = job.id
WHERE job.kind = 'resource-acquire' AND job.state IN ('blocked', 'queued')`)
	if err != nil {
		return err
	}
	type candidate struct{ id, producer, name, profile, digest string }
	candidates := make([]candidate, 0)
	for rows.Next() {
		var current candidate
		if err := rows.Scan(&current.id, &current.producer, &current.name, &current.profile, &current.digest); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, current)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	at := formatInstant(now.UTC())
	for _, current := range candidates {
		registration, exists := active[current.name]
		if !exists || registration.Profile != current.profile ||
			registration.EntryDigest.String() != current.digest {
			if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'failed', terminal_error_code = 'resource-catalog-entry-unavailable', updated_at = ?
WHERE id = ? AND state IN ('blocked', 'queued')`, at, current.id); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM work_job_prerequisites WHERE job_id = ?`, current.id); err != nil {
			return err
		}
		if executorVersion == "" || current.producer != executorVersion {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (
  job_id, kind, reference_kind, reference_id, created_at
) VALUES (?, 'executor-required', 'capability', 'work-executor/resource-acquire', ?)`, current.id, at); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = CASE
      WHEN EXISTS (SELECT 1 FROM work_job_prerequisites WHERE job_id = ?) THEN 'blocked'
      ELSE 'queued'
    END,
    terminal_error_code = NULL, updated_at = ?
WHERE id = ? AND state IN ('blocked', 'queued')`, current.id, at, current.id); err != nil {
			return err
		}
	}
	return nil
}

func (repository *SQLiteProjects) claimProductResourceJob(
	ctx context.Context,
	input application.ClaimWorkJobInput,
	jobID domain.WorkJobID,
	executorVersion string,
) (application.WorkJobClaim, error) {
	if jobID.IsZero() || executorVersion == "" ||
		application.ValidateProductResourceRegistrations(input.Resources) != nil {
		return application.WorkJobClaim{}, application.ErrNoWork
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.WorkJobClaim{}, err
	}
	defer tx.Rollback()
	var installationID, name, kind, version, profile, entryDigest string
	var entryJSON, origin, contentDigest, producerVersion string
	var byteSize, generation uint64
	err = tx.QueryRowContext(ctx, `
SELECT job.installation_id, detail.catalog_entry_id, detail.resource_kind,
       detail.resource_version, detail.resource_profile, detail.entry_digest,
       detail.entry_json, detail.origin, detail.expected_byte_size,
       detail.expected_content_digest, job.producer_version,
       COALESCE((SELECT MAX(generation) FROM work_job_attempts WHERE job_id = job.id), 0) + 1
FROM work_jobs job
JOIN resource_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.kind = 'resource-acquire' AND job.state = 'queued'
  AND job.cancellation_requested = 0 AND job.producer_version = ?`,
		jobID.String(), executorVersion,
	).Scan(
		&installationID, &name, &kind, &version, &profile, &entryDigest,
		&entryJSON, &origin, &byteSize, &contentDigest, &producerVersion, &generation,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.WorkJobClaim{}, application.ErrNoWork
	}
	if err != nil {
		return application.WorkJobClaim{}, err
	}
	size, sizeErr := domain.NewUInt64(byteSize)
	digest, digestErr := domain.ParseDigest(contentDigest)
	entry, entryErr := application.NewProductResourceCatalogEntry(
		name, domain.ProductResourceKind(kind), version, profile, origin, size, digest,
		domain.ProductResourceRetentionOffline,
	)
	if sizeErr != nil || digestErr != nil || entryErr != nil || entry.EntryDigest.String() != entryDigest ||
		string(entry.Canonical) != entryJSON || producerVersion != executorVersion ||
		!activeProductResourceEntry(input.Resources, entry) {
		return application.WorkJobClaim{}, application.ErrProductResourceInvalid
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
	return application.WorkJobClaim{
		JobID: jobID, AttemptID: input.AttemptID, Kind: application.WorkJobResourceAcquire,
		ExecutorVersion: executorVersion, Generation: generation, LeaseOwner: input.LeaseOwner,
		LeaseExpiresAt: input.Now.UTC().Add(input.LeaseDuration),
		Resource:       &application.ProductResourceJobClaim{InstallationID: installationID, Entry: entry},
	}, nil
}

func activeProductResourceEntry(
	registrations []application.ProductResourceRegistration,
	entry application.ProductResourceCatalogEntry,
) bool {
	for _, registration := range registrations {
		if registration.Name == entry.Name && registration.Profile == entry.Profile &&
			registration.EntryDigest == entry.EntryDigest {
			return true
		}
	}
	return false
}

func (repository *SQLiteProjects) CompleteProductResource(
	ctx context.Context,
	input application.CompleteProductResourceInput,
) error {
	if err := validateProductResourceCompletion(input); err != nil {
		return err
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	resourceID := input.ResourceID
	rematerializing := false
	var existingID, existingState string
	err := repository.db.QueryRowContext(ctx, `
SELECT id, state FROM product_resources
WHERE installation_id = ? AND catalog_entry_id = ? AND entry_digest = ?`,
		input.Claim.Resource.InstallationID, input.Claim.Resource.Entry.Name,
		input.Claim.Resource.Entry.EntryDigest.String(),
	).Scan(&existingID, &existingState)
	if err == nil {
		if existingState != "invalid" {
			return application.ErrProductResourceInvalid
		}
		resourceID, err = domain.ParseResourceID(existingID)
		if err != nil {
			return err
		}
		rematerializing = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	finalRoot, err := repository.publishProductResourceFile(input, resourceID)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(finalRoot)
		}
	}()
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyProductResourceAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC()); err != nil {
		return err
	}
	entry := input.Claim.Resource.Entry
	at := formatInstant(input.CompletedAt.UTC())
	if rematerializing {
		result, err := tx.ExecContext(ctx, `
UPDATE product_resources
SET state = 'ready', producer_job_id = ?, created_at = ?
WHERE id = ? AND state = 'invalid' AND installation_id = ?
  AND catalog_entry_id = ? AND entry_digest = ? AND content_digest = ? AND byte_size = ?`,
			input.Claim.JobID.String(), at, resourceID.String(), input.Claim.Resource.InstallationID,
			entry.Name, entry.EntryDigest.String(), entry.SHA256.String(), entry.ByteSize.Value(),
		)
		if err != nil {
			return err
		}
		if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
			return application.ErrProductResourceLeaseLost
		}
	} else if _, err := tx.ExecContext(ctx, `
INSERT INTO product_resources (
  id, installation_id, catalog_entry_id, kind, version, profile, entry_digest,
  content_digest, state, byte_size, byte_reference, retention, producer_job_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?, ?, ?, ?)`,
		resourceID.String(), input.Claim.Resource.InstallationID, entry.Name, entry.Kind,
		entry.Version, entry.Profile, entry.EntryDigest.String(), entry.SHA256.String(),
		entry.ByteSize.Value(), "resource:product/"+resourceID.String(), entry.Retention,
		input.Claim.JobID.String(), at,
	); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE resource_job_details SET result_resource_id = ?
WHERE job_id = ? AND result_resource_id IS NULL`, resourceID.String(), input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.ErrProductResourceLeaseLost
	}
	if err := completeProductResourceAttempt(ctx, tx, input.Claim, at); err != nil {
		return err
	}
	if err := satisfyProductResourcePrerequisites(ctx, tx, input.Claim.Resource, at); err != nil {
		return err
	}
	if err := appendProductResourceWorkActivity(
		ctx, tx, input.Claim, input.EventID, input.CompletedAt,
		"resource.acquired", "product-resource-acquired", nil,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func validateProductResourceCompletion(input application.CompleteProductResourceInput) error {
	if input.Claim.Resource == nil || input.Claim.Kind != application.WorkJobResourceAcquire ||
		input.ResourceID.IsZero() || input.EventID.IsZero() || input.CompletedAt.IsZero() ||
		input.Download.Workspace == nil || input.Download.ByteSize != input.Claim.Resource.Entry.ByteSize ||
		input.Download.SHA256 != input.Claim.Resource.Entry.SHA256 {
		return application.ErrProductResourceInvalid
	}
	if _, err := domain.ParseRequestID(input.Claim.Resource.InstallationID); err != nil {
		return application.ErrProductResourceInvalid
	}
	return nil
}

func (repository *SQLiteProjects) publishProductResourceFile(
	input application.CompleteProductResourceInput,
	resourceID domain.ResourceID,
) (string, error) {
	workRoot := filepath.Join(repository.dataDir, "work", "product-resource-publication")
	resourceRoot := filepath.Join(repository.dataDir, "resources", "product")
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(resourceRoot, 0o700); err != nil {
		return "", err
	}
	stageRoot := filepath.Join(workRoot, input.Claim.AttemptID.String()+"-"+resourceID.String())
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		return "", err
	}
	defer os.RemoveAll(stageRoot)
	output, err := os.OpenFile(filepath.Join(stageRoot, "content.bin"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	inputFile, err := input.Download.Workspace.Open()
	if err != nil {
		output.Close()
		return "", err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(inputFile, int64(input.Download.ByteSize.Value())+1))
	inputCloseErr := inputFile.Close()
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || inputCloseErr != nil || syncErr != nil || closeErr != nil ||
		written != int64(input.Download.ByteSize.Value()) ||
		"sha256:"+hex.EncodeToString(hash.Sum(nil)) != input.Download.SHA256.String() {
		return "", errors.Join(copyErr, inputCloseErr, syncErr, closeErr, application.ErrProductResourceInvalid)
	}
	if err := syncDirectory(stageRoot); err != nil {
		return "", err
	}
	finalRoot := filepath.Join(resourceRoot, resourceID.String())
	if _, err := os.Lstat(finalRoot); !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("product resource destination already exists")
	}
	if err := os.Rename(stageRoot, finalRoot); err != nil {
		return "", err
	}
	if err := syncDirectory(resourceRoot); err != nil {
		return "", err
	}
	return finalRoot, nil
}

func verifyProductResourceAttempt(
	ctx context.Context,
	tx *sql.Tx,
	claim application.WorkJobClaim,
	now time.Time,
) error {
	if claim.Resource == nil || claim.JobID.IsZero() || claim.AttemptID.IsZero() ||
		claim.Kind != application.WorkJobResourceAcquire {
		return application.ErrProductResourceLeaseLost
	}
	var jobState, producer, installationID, attemptState, owner, expires string
	var generation uint64
	err := tx.QueryRowContext(ctx, `
SELECT job.state, job.producer_version, job.installation_id,
       attempt.state, attempt.lease_owner, attempt.generation, attempt.lease_expires_at
FROM work_jobs job
JOIN work_job_attempts attempt ON attempt.job_id = job.id AND attempt.id = ?
JOIN resource_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.kind = 'resource-acquire'
  AND detail.catalog_entry_id = ? AND detail.entry_digest = ?`,
		claim.AttemptID.String(), claim.JobID.String(), claim.Resource.Entry.Name,
		claim.Resource.Entry.EntryDigest.String(),
	).Scan(&jobState, &producer, &installationID, &attemptState, &owner, &generation, &expires)
	if err != nil {
		return err
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || jobState != "running" || attemptState != "running" ||
		producer != claim.ExecutorVersion || installationID != claim.Resource.InstallationID ||
		owner != claim.LeaseOwner || generation != claim.Generation || !expiry.After(now.UTC()) {
		return application.ErrProductResourceLeaseLost
	}
	return nil
}

func completeProductResourceAttempt(
	ctx context.Context,
	tx *sql.Tx,
	claim application.WorkJobClaim,
	at string,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'succeeded', progress_basis_points = 10000,
    updated_at = ?, terminal_error_code = NULL
WHERE id = ? AND state = 'running'`, at, claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.ErrProductResourceLeaseLost
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'succeeded', heartbeat_at = ?, ended_at = ?, diagnostics_json = '{}'
WHERE id = ? AND state = 'running'`, at, at, claim.AttemptID.String())
	if err != nil {
		return err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.ErrProductResourceLeaseLost
	}
	return nil
}

func satisfyProductResourcePrerequisites(
	ctx context.Context,
	tx *sql.Tx,
	resource *application.ProductResourceJobClaim,
	at string,
) error {
	if _, err := tx.ExecContext(ctx, `
DELETE FROM work_job_prerequisites
WHERE kind = 'model-required' AND reference_kind = 'resource' AND reference_id = ?
  AND job_id IN (
    SELECT job.id
    FROM work_jobs job
    JOIN media_job_details detail ON detail.job_id = job.id
    JOIN assets asset ON asset.id = detail.asset_id
    JOIN source_grants grant ON grant.id = asset.source_grant_id
    WHERE grant.installation_id = ?
  )`, resource.Entry.Name, resource.InstallationID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = CASE
      WHEN EXISTS (SELECT 1 FROM work_job_prerequisites WHERE job_id = work_jobs.id) THEN 'blocked'
      ELSE 'queued'
    END,
    updated_at = ?
WHERE state IN ('blocked', 'queued')
  AND id IN (
    SELECT job.id
    FROM work_jobs job
    JOIN media_job_details detail ON detail.job_id = job.id
    JOIN assets asset ON asset.id = detail.asset_id
    JOIN source_grants grant ON grant.id = asset.source_grant_id
    WHERE grant.installation_id = ?
  )`, at, resource.InstallationID)
	return err
}

func (repository *SQLiteProjects) FailProductResource(
	ctx context.Context,
	input application.FailProductResourceInput,
) error {
	if input.Claim.Resource == nil || input.EventID.IsZero() || input.FailedAt.IsZero() ||
		input.Code == "" || len(input.Code) > 64 {
		return application.ErrProductResourceInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyProductResourceAttempt(ctx, tx, input.Claim, input.FailedAt.UTC()); err != nil {
		return err
	}
	at := formatInstant(input.FailedAt.UTC())
	diagnostics, _ := json.Marshal(struct {
		Code string `json:"code"`
	}{Code: input.Code})
	if _, err := tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'failed', heartbeat_at = ?, ended_at = ?, diagnostics_json = ?
WHERE id = ? AND state = 'running'`, at, at, string(diagnostics), input.Claim.AttemptID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs SET state = 'failed', updated_at = ?, terminal_error_code = ?
WHERE id = ? AND state = 'running'`, at, input.Code, input.Claim.JobID.String()); err != nil {
		return err
	}
	if err := appendProductResourceWorkActivity(
		ctx, tx, input.Claim, input.EventID, input.FailedAt,
		"resource.acquisition-failed", "product-resource-acquisition-failed", &input.Code,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func appendProductResourceWorkActivity(
	ctx context.Context,
	tx *sql.Tx,
	claim application.WorkJobClaim,
	eventID domain.ActivityEventID,
	at time.Time,
	kind, summary string,
	failureCode *string,
) error {
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ResourceName      string                         `json:"resourceName"`
		JobID             domain.WorkJobID               `json:"jobId"`
		AttemptID         domain.JobAttemptID            `json:"attemptId"`
		FailureCode       *string                        `json:"failureCode,omitempty"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{}, ResourceName: claim.Resource.Entry.Name,
		JobID: claim.JobID, AttemptID: claim.AttemptID, FailureCode: failureCode,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "installation", ScopeID: claim.Resource.InstallationID,
		EventID: eventID.String(), Kind: kind, OccurredAt: formatInstant(at.UTC()),
		OutcomeKind: "resource-job", OutcomeID: claim.JobID.String(),
		SummaryCode: summary, Payload: payload,
	})
	return err
}

var _ application.ProductResourceWorkRepository = (*SQLiteProjects)(nil)
