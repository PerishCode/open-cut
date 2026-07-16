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

func (repository *SQLiteProjects) LoadSourcePreviewSelection(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	input application.SourcePreviewSelectionInput,
) (application.SourcePreviewSelectionSnapshot, error) {
	if projectID.IsZero() || assetID.IsZero() {
		return application.SourcePreviewSelectionSnapshot{}, application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SourcePreviewSelectionSnapshot{}, err
	}
	defer tx.Rollback()
	var revision uint64
	var fingerprintValue, availability, observed string
	err = tx.QueryRowContext(ctx, `
SELECT asset.revision, asset.accepted_fingerprint, media.availability, media.observed_fingerprint
FROM assets asset
JOIN asset_media_state media ON media.asset_id = asset.id
WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0
  AND asset.accepted_fingerprint IS NOT NULL AND media.observed_fingerprint IS NOT NULL`,
		assetID.String(), projectID.String(),
	).Scan(&revision, &fingerprintValue, &availability, &observed)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SourcePreviewSelectionSnapshot{}, application.ErrAssetNotFound
	}
	if err != nil {
		return application.SourcePreviewSelectionSnapshot{}, err
	}
	if revision != input.AssetRevision.Value() || fingerprintValue != input.Fingerprint.String() ||
		observed != fingerprintValue ||
		(availability != string(domain.AssetOnline) && availability != string(domain.AssetManagedState)) {
		return application.SourcePreviewSelectionSnapshot{}, application.ErrRenderInputRequired
	}
	revisionValue, err := domain.NewRevision(revision)
	if err != nil {
		return application.SourcePreviewSelectionSnapshot{}, application.ErrAssetInvalid
	}
	fingerprint, err := domain.ParseDigest(fingerprintValue)
	if err != nil {
		return application.SourcePreviewSelectionSnapshot{}, application.ErrAssetInvalid
	}
	result := application.SourcePreviewSelectionSnapshot{
		ProjectID: projectID, AssetID: assetID, AssetRevision: revisionValue, Fingerprint: fingerprint,
	}
	if input.VideoStreamID != nil {
		stream, loadErr := loadSourcePreviewStream(ctx, tx, assetID, fingerprint, *input.VideoStreamID, domain.MediaVideo)
		if loadErr != nil {
			return application.SourcePreviewSelectionSnapshot{}, loadErr
		}
		result.Video = &stream
	}
	if input.AudioStreamID != nil {
		stream, loadErr := loadSourcePreviewStream(ctx, tx, assetID, fingerprint, *input.AudioStreamID, domain.MediaAudio)
		if loadErr != nil {
			return application.SourcePreviewSelectionSnapshot{}, loadErr
		}
		result.Audio = &stream
	}
	if err := tx.Commit(); err != nil {
		return application.SourcePreviewSelectionSnapshot{}, err
	}
	return result, nil
}

func (repository *SQLiteProjects) ResolveSourcePreview(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	jobID domain.MediaJobID,
	parametersDigest domain.Digest,
) (application.SourcePreviewResolution, error) {
	if projectID.IsZero() || assetID.IsZero() || jobID.IsZero() || parametersDigest == "" {
		return application.SourcePreviewResolution{}, application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SourcePreviewResolution{}, err
	}
	defer tx.Rollback()
	var fingerprintValue string
	var assetRevision uint64
	err = tx.QueryRowContext(ctx, `
SELECT revision, accepted_fingerprint FROM assets
WHERE id = ? AND project_id = ? AND tombstoned = 0 AND accepted_fingerprint IS NOT NULL`,
		assetID.String(), projectID.String(),
	).Scan(&assetRevision, &fingerprintValue)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SourcePreviewResolution{}, application.ErrAssetNotFound
	}
	if err != nil {
		return application.SourcePreviewResolution{}, err
	}
	fingerprint, err := domain.ParseDigest(fingerprintValue)
	if err != nil {
		return application.SourcePreviewResolution{}, application.ErrAssetInvalid
	}
	revision, err := domain.NewRevision(assetRevision)
	if err != nil {
		return application.SourcePreviewResolution{}, application.ErrAssetInvalid
	}
	var jobValue, parametersJSON string
	var retryOfValue sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT job.id, job.parameters_json, job.retry_of_job_id
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.project_id = ? AND detail.asset_id = ? AND job.kind = 'proxy'
  AND job.parameters_digest = ?`,
		jobID.String(), projectID.String(), assetID.String(), parametersDigest.String(),
	).Scan(&jobValue, &parametersJSON, &retryOfValue)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SourcePreviewResolution{}, application.ErrAssetInvalid
	}
	if err != nil {
		return application.SourcePreviewResolution{}, err
	}
	parsedJobID, err := domain.ParseMediaJobID(jobValue)
	if err != nil {
		return application.SourcePreviewResolution{}, err
	}
	if parsedJobID != jobID {
		return application.SourcePreviewResolution{}, application.ErrAssetInvalid
	}
	parameters, err := application.DecodeInitialMediaJobParameters([]byte(parametersJSON))
	if err != nil || parameters.AssetID != assetID || parameters.Kind != domain.MediaJobProxy ||
		parameters.Profile != application.SourceProxyProfile || parameters.ProxySelection == nil ||
		parameters.ProxySelection.Policy != application.SourceProxySelectionExplicit {
		return application.SourcePreviewResolution{}, application.ErrAssetInvalid
	}
	job, err := loadMediaJobSummary(ctx, tx, parsedJobID)
	if err != nil {
		return application.SourcePreviewResolution{}, err
	}
	resolution := application.SourcePreviewResolution{
		ProjectID: projectID, AssetID: assetID, AssetRevision: revision, Fingerprint: fingerprint, Job: job,
	}
	if parameters.ProxySelection.VideoStreamID != nil {
		stream, loadErr := loadSourcePreviewStream(
			ctx, tx, assetID, fingerprint, *parameters.ProxySelection.VideoStreamID, domain.MediaVideo,
		)
		if loadErr != nil {
			return application.SourcePreviewResolution{}, loadErr
		}
		resolution.Video = &stream
	}
	if parameters.ProxySelection.AudioStreamID != nil {
		stream, loadErr := loadSourcePreviewStream(
			ctx, tx, assetID, fingerprint, *parameters.ProxySelection.AudioStreamID, domain.MediaAudio,
		)
		if loadErr != nil {
			return application.SourcePreviewResolution{}, loadErr
		}
		resolution.Audio = &stream
	}
	if retryOfValue.Valid {
		var rejectedArtifactValue, rejectedState string
		if err := tx.QueryRowContext(ctx, `
SELECT artifact.id, artifact.state
FROM work_jobs rejected
JOIN media_job_details detail ON detail.job_id = rejected.id
JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE rejected.id = ? AND rejected.project_id = ? AND detail.asset_id = ?
  AND rejected.kind = 'proxy' AND rejected.state = 'succeeded'`,
			retryOfValue.String, projectID.String(), assetID.String(),
		).Scan(&rejectedArtifactValue, &rejectedState); err != nil {
			return application.SourcePreviewResolution{}, application.ErrAssetInvalid
		}
		rejectedArtifactID, parseErr := domain.ParseArtifactID(rejectedArtifactValue)
		if parseErr != nil {
			return application.SourcePreviewResolution{}, parseErr
		}
		if job.State == domain.MediaJobSucceeded {
			if rejectedState != string(domain.ArtifactReady) {
				return application.SourcePreviewResolution{}, application.ErrAssetInvalid
			}
		} else {
			if rejectedState != string(domain.ArtifactEvicted) {
				return application.SourcePreviewResolution{}, application.ErrAssetInvalid
			}
			resolution.RejectedArtifactID = &rejectedArtifactID
		}
	}
	if job.ResultArtifactID != nil {
		artifact, loadErr := loadMediaArtifactSummary(ctx, tx, *job.ResultArtifactID)
		if loadErr != nil {
			return application.SourcePreviewResolution{}, loadErr
		}
		resolution.Artifact = &artifact
	}
	if err := tx.Commit(); err != nil {
		return application.SourcePreviewResolution{}, err
	}
	return resolution, nil
}

func loadSourcePreviewStream(
	ctx context.Context,
	tx *sql.Tx,
	assetID domain.AssetID,
	fingerprint domain.Digest,
	streamID domain.SourceStreamID,
	expected domain.MediaType,
) (domain.SourceStream, error) {
	var descriptorJSON string
	err := tx.QueryRowContext(ctx, `
SELECT descriptor_json FROM source_streams
WHERE id = ? AND asset_id = ? AND fingerprint = ? AND media_type = ?`,
		streamID.String(), assetID.String(), fingerprint.String(), string(expected),
	).Scan(&descriptorJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SourceStream{}, application.ErrRenderInputRequired
	}
	if err != nil {
		return domain.SourceStream{}, err
	}
	var descriptor domain.SourceStreamDescriptor
	if json.Unmarshal([]byte(descriptorJSON), &descriptor) != nil || descriptor.Validate() != nil ||
		descriptor.MediaType != expected {
		return domain.SourceStream{}, application.ErrAssetInvalid
	}
	return domain.SourceStream{ID: streamID, Descriptor: descriptor}, nil
}

func loadMediaArtifactSummary(
	ctx context.Context,
	tx *sql.Tx,
	artifactID domain.ArtifactID,
) (domain.ArtifactSummary, error) {
	var idValue, kind, producer, fingerprint, state, contentDigest, createdAt string
	var byteSize uint64
	err := tx.QueryRowContext(ctx, `
SELECT id, kind, producer_version, input_fingerprint, state, byte_size, content_digest, created_at
FROM media_artifacts WHERE id = ?`, artifactID.String()).Scan(
		&idValue, &kind, &producer, &fingerprint, &state, &byteSize, &contentDigest, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ArtifactSummary{}, application.ErrAssetInvalid
	}
	if err != nil {
		return domain.ArtifactSummary{}, err
	}
	id, err := domain.ParseArtifactID(idValue)
	if err != nil {
		return domain.ArtifactSummary{}, err
	}
	parsedFingerprint, err := domain.ParseDigest(fingerprint)
	if err != nil {
		return domain.ArtifactSummary{}, err
	}
	parsedDigest, err := domain.ParseDigest(contentDigest)
	if err != nil {
		return domain.ArtifactSummary{}, err
	}
	size, err := domain.NewUInt64(byteSize)
	if err != nil {
		return domain.ArtifactSummary{}, err
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.ArtifactSummary{}, err
	}
	return domain.ArtifactSummary{
		ID: id, Kind: domain.ArtifactKind(kind), ProducerVersion: producer,
		InputFingerprint: parsedFingerprint, State: domain.ArtifactState(state),
		ByteSize: size, ContentDigest: parsedDigest, CreatedAt: created.UTC(),
	}, nil
}
