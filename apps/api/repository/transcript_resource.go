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

func (repository *SQLiteProjects) ReadBoundTranscriptResource(
	ctx context.Context,
	claim application.MediaJobClaim,
	now time.Time,
) (domain.ProductResource, error) {
	if now.IsZero() || claim.Kind != domain.MediaJobTranscript || claim.TranscriptBinding == nil ||
		claim.TranscriptBinding.Validate() != nil || claim.TranscriptNoAudio {
		return domain.ProductResource{}, application.ErrProductResourceInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.ProductResource{}, err
	}
	defer tx.Rollback()
	if err := verifyMediaAttempt(ctx, tx, claim, now.UTC(), domain.MediaJobTranscript); err != nil {
		return domain.ProductResource{}, err
	}
	loaded, err := loadTranscriptBinding(ctx, tx, claim.JobID, claim.AssetID, *claim.AcceptedFingerprint)
	if err != nil || loaded != *claim.TranscriptBinding {
		if err != nil {
			return domain.ProductResource{}, err
		}
		return domain.ProductResource{}, application.ErrMediaLeaseLost
	}
	var idValue, installationID, name, kind, version, profile string
	var entryDigestValue, contentDigestValue, retention, createdAt string
	var byteSize uint64
	if err := tx.QueryRowContext(ctx, `
SELECT id, installation_id, catalog_entry_id, kind, version, profile,
       entry_digest, content_digest, byte_size, retention, created_at
FROM product_resources WHERE id = ? AND state = 'ready'`,
		claim.TranscriptBinding.ModelResourceID.String(),
	).Scan(
		&idValue, &installationID, &name, &kind, &version, &profile,
		&entryDigestValue, &contentDigestValue, &byteSize, &retention, &createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ProductResource{}, application.ErrProductResourceInvalid
		}
		return domain.ProductResource{}, err
	}
	id, idErr := domain.ParseResourceID(idValue)
	entryDigest, entryErr := domain.ParseDigest(entryDigestValue)
	contentDigest, contentErr := domain.ParseDigest(contentDigestValue)
	size, sizeErr := domain.NewUInt64(byteSize)
	created, timeErr := time.Parse(time.RFC3339Nano, createdAt)
	resource := domain.ProductResource{
		ID: id, InstallationID: installationID, Name: name, Kind: domain.ProductResourceKind(kind),
		Version: version, Profile: profile, EntryDigest: entryDigest, ContentDigest: contentDigest,
		ByteSize: size, Retention: domain.ProductResourceRetention(retention), CreatedAt: created.UTC(),
	}
	if idErr != nil || entryErr != nil || contentErr != nil || sizeErr != nil || timeErr != nil ||
		resource.Validate() != nil || resource.ID != claim.TranscriptBinding.ModelResourceID ||
		resource.Name != claim.TranscriptBinding.ModelName ||
		resource.Version != claim.TranscriptBinding.ModelVersion ||
		resource.EntryDigest != claim.TranscriptBinding.ModelEntryDigest ||
		resource.ContentDigest != claim.TranscriptBinding.ModelContentDigest {
		return domain.ProductResource{}, application.ErrProductResourceInvalid
	}
	if err := tx.Commit(); err != nil {
		return domain.ProductResource{}, err
	}
	return resource, nil
}

func (repository *SQLiteProjects) ReblockMediaTranscriptResource(
	ctx context.Context,
	input application.ReblockMediaTranscriptResource,
) error {
	if input.ResourceID.IsZero() || input.EventID.IsZero() || input.ResourceEventID.IsZero() ||
		input.EventID == input.ResourceEventID || input.ReblockedAt.IsZero() ||
		input.Claim.Kind != domain.MediaJobTranscript || input.Claim.TranscriptBinding == nil ||
		input.Claim.TranscriptBinding.ModelResourceID != input.ResourceID {
		return application.ErrProductResourceInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyMediaAttempt(
		ctx, tx, input.Claim, input.ReblockedAt.UTC(), domain.MediaJobTranscript,
	); err != nil {
		return err
	}
	loaded, err := loadTranscriptBinding(
		ctx, tx, input.Claim.JobID, input.Claim.AssetID, *input.Claim.AcceptedFingerprint,
	)
	if err != nil || loaded != *input.Claim.TranscriptBinding {
		if err != nil {
			return err
		}
		return application.ErrMediaLeaseLost
	}
	var installationID, name string
	if err := tx.QueryRowContext(ctx, `
SELECT installation_id, catalog_entry_id
FROM product_resources WHERE id = ? AND state = 'ready'`, input.ResourceID.String()).Scan(
		&installationID, &name,
	); err != nil {
		return err
	}
	at := formatInstant(input.ReblockedAt.UTC())
	result, err := tx.ExecContext(ctx, `
UPDATE product_resources SET state = 'invalid' WHERE id = ? AND state = 'ready'`, input.ResourceID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrProductResourceInvalid
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'abandoned', heartbeat_at = ?, ended_at = ?, diagnostics_json = '{"code":"resource-integrity-invalid"}'
WHERE state IN ('leased', 'running', 'publishing')
  AND job_id IN (
    SELECT job_id FROM transcript_job_bindings WHERE model_resource_id = ?
  )`, at, at, input.ResourceID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'blocked', progress_basis_points = 0, updated_at = ?, terminal_error_code = NULL
WHERE kind = 'transcript' AND state IN ('blocked', 'queued', 'running')
  AND id IN (
    SELECT job_id FROM transcript_job_bindings WHERE model_resource_id = ?
  )`, at, input.ResourceID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO work_job_prerequisites (
  job_id, kind, reference_kind, reference_id, created_at
)
SELECT job.id, 'model-required', 'resource', ?, ?
FROM work_jobs job
JOIN transcript_job_bindings binding ON binding.job_id = job.id
WHERE binding.model_resource_id = ? AND job.state = 'blocked'`,
		name, at, input.ResourceID.String()); err != nil {
		return err
	}
	failureCode := "resource-integrity-invalid"
	if err := appendMediaJobActivity(
		ctx, tx, input.Claim, input.EventID, input.ReblockedAt,
		"media.transcript-reblocked", "media-transcript-resource-invalid", &failureCode,
	); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ResourceID        domain.ResourceID              `json:"resourceId"`
		JobID             domain.MediaJobID              `json:"jobId"`
		FailureCode       string                         `json:"failureCode"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{}, ResourceID: input.ResourceID,
		JobID: input.Claim.JobID, FailureCode: failureCode,
	})
	if err != nil {
		return err
	}
	if _, err := appendActivity(ctx, tx, activityRecord{
		ScopeKind: "installation", ScopeID: installationID, EventID: input.ResourceEventID.String(),
		Kind: "resource.invalidated", OccurredAt: at,
		ActorKind: nil, ActorID: nil, ProjectID: nil, ProjectRevision: nil,
		OutcomeKind: "product-resource", OutcomeID: input.ResourceID.String(),
		SummaryCode: "product-resource-integrity-invalid", Payload: payload,
	}); err != nil {
		return err
	}
	return tx.Commit()
}
