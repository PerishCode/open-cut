package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ListProductResources(
	ctx context.Context,
	installationID string,
	entries []application.ProductResourceCatalogEntry,
) ([]application.ProductResourceView, error) {
	if _, err := domain.ParseRequestID(installationID); err != nil ||
		application.ValidateProductResourceCatalog(entries) != nil {
		return nil, application.ErrProductResourceInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := make([]application.ProductResourceView, 0, len(entries))
	for _, entry := range entries {
		view, err := loadProductResourceView(ctx, tx, installationID, entry)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (repository *SQLiteProjects) RequestProductResource(
	ctx context.Context,
	record application.RequestProductResourceRecord,
) (application.RequestProductResourceOutcome, error) {
	if err := validateProductResourceRequest(record); err != nil {
		return application.RequestProductResourceOutcome{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.RequestProductResourceOutcome{}, err
	}
	defer tx.Rollback()
	var existingDigest, existingJob, existingEvent string
	err = tx.QueryRowContext(ctx, `
SELECT input_digest, job_id, activity_event_id
FROM product_resource_requests WHERE installation_id = ? AND request_id = ?`,
		record.InstallationID, record.RequestID.String(),
	).Scan(&existingDigest, &existingJob, &existingEvent)
	if err == nil {
		if existingDigest != record.RequestDigest.String() {
			return application.RequestProductResourceOutcome{}, application.ErrRequestIdentityReused
		}
		view, err := loadProductResourceView(ctx, tx, record.InstallationID, record.Entry)
		if err != nil {
			return application.RequestProductResourceOutcome{}, err
		}
		if view.JobID == nil || view.JobID.String() != existingJob {
			return application.RequestProductResourceOutcome{}, application.ErrProductResourceInvalid
		}
		cursor, err := activityCursorForEvent(ctx, tx, existingEvent)
		if err != nil {
			return application.RequestProductResourceOutcome{}, err
		}
		if err := tx.Commit(); err != nil {
			return application.RequestProductResourceOutcome{}, err
		}
		return application.RequestProductResourceOutcome{View: view, ActivityCursor: cursor, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return application.RequestProductResourceOutcome{}, err
	}
	jobID, err := findCurrentProductResourceJob(ctx, tx, record.InstallationID, record.Entry)
	if errors.Is(err, sql.ErrNoRows) {
		jobID = record.JobID
		if err := insertProductResourceJob(ctx, tx, record); err != nil {
			return application.RequestProductResourceOutcome{}, err
		}
	} else if err != nil {
		return application.RequestProductResourceOutcome{}, err
	}
	at := formatInstant(record.RequestedAt.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO product_resource_requests (
  installation_id, request_id, input_digest, input_json, job_id, activity_event_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		record.InstallationID, record.RequestID.String(), record.RequestDigest.String(),
		string(record.RequestCanonical), jobID.String(), record.ActivityEventID.String(), at,
	); err != nil {
		return application.RequestProductResourceOutcome{}, err
	}
	cursor, err := appendProductResourceRequestedActivity(ctx, tx, record, jobID)
	if err != nil {
		return application.RequestProductResourceOutcome{}, err
	}
	view, err := loadProductResourceView(ctx, tx, record.InstallationID, record.Entry)
	if err != nil {
		return application.RequestProductResourceOutcome{}, err
	}
	if view.JobID == nil || *view.JobID != jobID {
		return application.RequestProductResourceOutcome{}, application.ErrProductResourceInvalid
	}
	if err := tx.Commit(); err != nil {
		return application.RequestProductResourceOutcome{}, err
	}
	return application.RequestProductResourceOutcome{View: view, ActivityCursor: cursor}, nil
}

func validateProductResourceRequest(record application.RequestProductResourceRecord) error {
	if _, err := domain.ParseRequestID(record.InstallationID); err != nil {
		return application.ErrProductResourceInvalid
	}
	if record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorCreator ||
		record.JobID.IsZero() || record.ActivityEventID.IsZero() || record.RequestedAt.IsZero() ||
		len(record.RequestCanonical) == 0 {
		return application.ErrProductResourceInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrProductResourceInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/product-resource-acquire", application.ProductResourceAcquireSchema,
		struct {
			Name        string        `json:"name"`
			EntryDigest domain.Digest `json:"entryDigest"`
		}{Name: record.Entry.Name, EntryDigest: record.Entry.EntryDigest},
	)
	if err != nil || digest != record.RequestDigest || string(canonical) != string(record.RequestCanonical) {
		return application.ErrProductResourceInvalid
	}
	entryCanonical, entryDigest, err := application.CanonicalProductResourceCatalogEntry(record.Entry)
	if err != nil || entryDigest != record.Entry.EntryDigest ||
		string(entryCanonical) != string(record.Entry.Canonical) {
		return application.ErrProductResourceInvalid
	}
	return nil
}

func findCurrentProductResourceJob(
	ctx context.Context,
	tx *sql.Tx,
	installationID string,
	entry application.ProductResourceCatalogEntry,
) (domain.WorkJobID, error) {
	var value string
	err := tx.QueryRowContext(ctx, `
SELECT producer_job_id FROM product_resources
WHERE installation_id = ? AND catalog_entry_id = ? AND entry_digest = ? AND state = 'ready'`,
		installationID, entry.Name, entry.EntryDigest.String(),
	).Scan(&value)
	if err == nil {
		return domain.ParseWorkJobID(value)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.WorkJobID{}, err
	}
	err = tx.QueryRowContext(ctx, `
SELECT job.id
FROM work_jobs job
JOIN resource_job_details detail ON detail.job_id = job.id
WHERE job.installation_id = ? AND detail.catalog_entry_id = ? AND detail.entry_digest = ?
  AND job.state IN ('blocked', 'queued', 'running')
ORDER BY job.created_at DESC, job.id DESC LIMIT 1`,
		installationID, entry.Name, entry.EntryDigest.String(),
	).Scan(&value)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	return domain.ParseWorkJobID(value)
}

func insertProductResourceJob(
	ctx context.Context,
	tx *sql.Tx,
	record application.RequestProductResourceRecord,
) error {
	at := formatInstant(record.RequestedAt.UTC())
	logicalKey := "product-resource/" + record.InstallationID + "/" + record.Entry.EntryDigest.String()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, installation_id, kind, state, pool, priority_class,
  logical_key, parameters_digest, parameters_json, producer_version,
  progress_basis_points, cancellation_requested, created_at, updated_at
) VALUES (?, 'installation', NULL, ?, 'resource-acquire', 'queued', 'network', 'foreground',
          ?, ?, ?, ?, 0, 0, ?, ?)`,
		record.JobID.String(), record.InstallationID, logicalKey, record.Entry.EntryDigest.String(),
		string(record.Entry.Canonical), application.ProductResourceDownloaderV1, at, at,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO resource_job_details (
  job_id, catalog_entry_id, resource_kind, resource_version, resource_profile,
  entry_digest, entry_json, origin, expected_byte_size, expected_content_digest, retention
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.JobID.String(), record.Entry.Name, record.Entry.Kind, record.Entry.Version,
		record.Entry.Profile, record.Entry.EntryDigest.String(), string(record.Entry.Canonical),
		record.Entry.Origin, record.Entry.ByteSize.Value(), record.Entry.SHA256.String(), record.Entry.Retention,
	); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
VALUES (?, 'installation', ?, ?)`, record.JobID.String(), record.InstallationID, at)
	return err
}

func loadProductResourceView(
	ctx context.Context,
	tx *sql.Tx,
	installationID string,
	entry application.ProductResourceCatalogEntry,
) (application.ProductResourceView, error) {
	view := application.ProductResourceView{
		Name: entry.Name, Kind: entry.Kind, Version: entry.Version, Profile: entry.Profile,
		ByteSize: entry.ByteSize, State: application.ProductResourceNotAcquired,
	}
	var resourceID, jobID, state, updatedAt string
	var progress uint16
	var failure sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT resource.id, job.id, job.state, job.progress_basis_points, job.terminal_error_code, job.updated_at
FROM product_resources resource
JOIN work_jobs job ON job.id = resource.producer_job_id
WHERE resource.installation_id = ? AND resource.catalog_entry_id = ? AND resource.entry_digest = ?`,
		installationID, entry.Name, entry.EntryDigest.String(),
	).Scan(&resourceID, &jobID, &state, &progress, &failure, &updatedAt)
	if err == nil {
		var resourceState string
		if stateErr := tx.QueryRowContext(ctx, `SELECT state FROM product_resources WHERE id = ?`, resourceID).Scan(&resourceState); stateErr != nil {
			return application.ProductResourceView{}, stateErr
		}
		if resourceState != "ready" {
			resourceID = ""
			state = "failed"
			failure = sql.NullString{String: "resource-integrity-invalid", Valid: true}
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		resourceID = ""
		err = tx.QueryRowContext(ctx, `
SELECT job.id, job.state, job.progress_basis_points, job.terminal_error_code, job.updated_at
FROM work_jobs job
JOIN resource_job_details detail ON detail.job_id = job.id
WHERE job.installation_id = ? AND detail.catalog_entry_id = ? AND detail.entry_digest = ?
ORDER BY job.created_at DESC, job.id DESC LIMIT 1`,
			installationID, entry.Name, entry.EntryDigest.String(),
		).Scan(&jobID, &state, &progress, &failure, &updatedAt)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return application.ProductResourceView{}, err
	}
	parsedJob, err := domain.ParseWorkJobID(jobID)
	if err != nil {
		return application.ProductResourceView{}, err
	}
	instant, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return application.ProductResourceView{}, err
	}
	view.JobID = &parsedJob
	view.ProgressBasisPoints = progress
	view.UpdatedAt = pointerTime(instant.UTC())
	view.FailureCode = failure.String
	switch state {
	case "blocked", "queued":
		view.State = application.ProductResourceQueued
	case "running":
		view.State = application.ProductResourceAcquiring
	case "failed":
		view.State = application.ProductResourceFailed
	case "cancelled":
		view.State = application.ProductResourceCancelled
	case "succeeded":
		if resourceID == "" {
			return application.ProductResourceView{}, application.ErrProductResourceInvalid
		}
		parsed, err := domain.ParseResourceID(resourceID)
		if err != nil {
			return application.ProductResourceView{}, err
		}
		view.State = application.ProductResourceReady
		view.ResourceID = &parsed
	default:
		return application.ProductResourceView{}, application.ErrProductResourceInvalid
	}
	return view, nil
}

func appendProductResourceRequestedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.RequestProductResourceRecord,
	jobID domain.WorkJobID,
) (domain.Cursor, error) {
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ResourceName      string                         `json:"resourceName"`
		JobID             domain.WorkJobID               `json:"jobId"`
	}{ChangedEntityRefs: []application.ChangedEntityRef{}, ResourceName: record.Entry.Name, JobID: jobID})
	if err != nil {
		return 0, err
	}
	return appendActivity(ctx, tx, activityRecord{
		ScopeKind: "installation", ScopeID: record.InstallationID,
		EventID: record.ActivityEventID.String(), Kind: "resource.acquisition-requested",
		OccurredAt: formatInstant(record.RequestedAt.UTC()),
		ActorKind:  string(record.Actor.Kind), ActorID: record.Actor.IDString(),
		OutcomeKind: "resource-job", OutcomeID: jobID.String(),
		SummaryCode: "resource-acquisition-requested", Payload: payload,
	})
}

func pointerTime(value time.Time) *time.Time { return &value }

var _ application.ProductResourceRepository = (*SQLiteProjects)(nil)
