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

func (repository *SQLiteProjects) EnsureExplicitRenderInputJob(
	ctx context.Context,
	record application.EnsureExplicitRenderInputJobRecord,
) (domain.WorkJobID, error) {
	if err := validateExplicitRenderInputJobRecord(record); err != nil {
		return domain.WorkJobID{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.WorkJobID{}, err
	}
	defer tx.Rollback()
	var fingerprint, availability, descriptorJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT asset.accepted_fingerprint, media.availability, stream.descriptor_json
FROM assets asset
JOIN asset_media_state media ON media.asset_id = asset.id
JOIN source_streams stream ON stream.id = ? AND stream.asset_id = asset.id
  AND stream.fingerprint = asset.accepted_fingerprint
WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0`,
		record.SourceStream.ID.String(), record.AssetID.String(), record.ProjectID.String(),
	).Scan(&fingerprint, &availability, &descriptorJSON); errors.Is(err, sql.ErrNoRows) {
		return domain.WorkJobID{}, application.ErrRenderInputRequired
	} else if err != nil {
		return domain.WorkJobID{}, err
	}
	storedDescriptor, err := json.Marshal(record.SourceStream.Descriptor)
	if err != nil || fingerprint != record.Fingerprint.String() || string(storedDescriptor) != descriptorJSON ||
		(availability != string(domain.AssetOnline) && availability != string(domain.AssetManagedState)) {
		return domain.WorkJobID{}, application.ErrRenderInputRequired
	}
	if existing, found, err := findReusableExplicitRenderInputJob(ctx, tx, record); err != nil {
		return domain.WorkJobID{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return domain.WorkJobID{}, err
		}
		return existing, nil
	}
	var retryOf sql.NullString
	if err := tx.QueryRowContext(ctx, `
SELECT job.id
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id AND artifact.state = 'evicted'
WHERE job.logical_key = ? AND job.kind = 'render-input' AND job.state = 'succeeded'
  AND job.project_id = ? AND detail.asset_id = ?
ORDER BY job.created_at DESC, job.id DESC LIMIT 1`,
		record.LogicalKey, record.ProjectID.String(), record.AssetID.String(),
	).Scan(&retryOf); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.WorkJobID{}, err
	}
	at := formatInstant(record.CreatedAt.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, kind, state, pool, priority_class, logical_key,
  parameters_digest, parameters_json, producer_version, retry_of_job_id, created_at, updated_at
) VALUES (?, 'project', ?, 'render-input', 'blocked', 'cpu', 'foreground', ?, ?, ?, ?, ?, ?, ?)`,
		record.JobID.String(), record.ProjectID.String(), record.LogicalKey, record.Digest.String(),
		string(record.Canonical), application.InitialMediaProducer, nullableString(retryOf), at, at,
	); err != nil {
		return domain.WorkJobID{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO media_job_details (job_id, asset_id) VALUES (?, ?)`,
		record.JobID.String(), record.AssetID.String()); err != nil {
		return domain.WorkJobID{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'media-executor/render-input', ?)`,
		record.JobID.String(), at); err != nil {
		return domain.WorkJobID{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
VALUES (?, 'project', ?, ?)`, record.JobID.String(), record.ProjectID.String(), at); err != nil {
		return domain.WorkJobID{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.WorkJobID{}, err
	}
	return record.JobID, nil
}

func findReusableExplicitRenderInputJob(
	ctx context.Context,
	tx *sql.Tx,
	record application.EnsureExplicitRenderInputJobRecord,
) (domain.WorkJobID, bool, error) {
	var idValue, digest, parameters string
	err := tx.QueryRowContext(ctx, `
SELECT job.id, job.parameters_digest, job.parameters_json
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
WHERE job.logical_key = ? AND job.kind = 'render-input' AND job.project_id = ? AND detail.asset_id = ?
  AND (
    job.state IN ('blocked', 'queued', 'running') OR
    (job.state = 'succeeded' AND EXISTS (
      SELECT 1 FROM media_artifacts artifact
      WHERE artifact.id = detail.result_artifact_id AND artifact.state = 'ready'
    ))
  )
ORDER BY job.created_at DESC, job.id DESC LIMIT 1`,
		record.LogicalKey, record.ProjectID.String(), record.AssetID.String(),
	).Scan(&idValue, &digest, &parameters)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkJobID{}, false, nil
	}
	if err != nil {
		return domain.WorkJobID{}, false, err
	}
	if digest != record.Digest.String() || parameters != string(record.Canonical) {
		return domain.WorkJobID{}, false, application.ErrRenderPlanInvalid
	}
	id, err := domain.ParseWorkJobID(idValue)
	return id, err == nil, err
}

func validateExplicitRenderInputJobRecord(record application.EnsureExplicitRenderInputJobRecord) error {
	if record.JobID.IsZero() || record.ProjectID.IsZero() || record.AssetID.IsZero() ||
		record.SourceStream.ID.IsZero() || record.SourceStream.Descriptor.Validate() != nil ||
		record.Parameters.Validate() != nil || record.Parameters.AssetID != record.AssetID ||
		record.Parameters.Kind != domain.MediaJobRenderInput || record.Parameters.RenderInputSelection == nil ||
		record.Parameters.RenderInputSelection.Policy != application.SourceProxySelectionExplicit ||
		record.CreatedAt.IsZero() || record.LogicalKey == "" || len(record.LogicalKey) > 1024 {
		return application.ErrRenderPlanInvalid
	}
	selection := record.Parameters.RenderInputSelection
	if (record.SourceStream.Descriptor.MediaType == domain.MediaVideo &&
		(selection.VideoStreamID == nil || *selection.VideoStreamID != record.SourceStream.ID ||
			selection.AudioStreamID != nil)) ||
		(record.SourceStream.Descriptor.MediaType == domain.MediaAudio &&
			(selection.AudioStreamID == nil || *selection.AudioStreamID != record.SourceStream.ID ||
				selection.VideoStreamID != nil)) ||
		(record.SourceStream.Descriptor.MediaType != domain.MediaVideo &&
			record.SourceStream.Descriptor.MediaType != domain.MediaAudio) {
		return application.ErrRenderPlanInvalid
	}
	canonical, digest, err := application.CanonicalInitialMediaJobParameters(record.Parameters)
	if err != nil || !bytes.Equal(canonical, record.Canonical) || digest != record.Digest {
		return application.ErrRenderPlanInvalid
	}
	if _, err := domain.ParseDigest(record.Fingerprint.String()); err != nil {
		return application.ErrRenderPlanInvalid
	}
	return nil
}
