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

func reconcileTranscriptBindings(
	ctx context.Context,
	tx *sql.Tx,
	executors []application.MediaExecutorRegistration,
	resources []application.ProductResourceRegistration,
	now time.Time,
) error {
	var executor *application.MediaExecutorRegistration
	for index := range executors {
		if executors[index].Kind == domain.MediaJobTranscript {
			executor = &executors[index]
			break
		}
	}
	var registration *application.ProductResourceRegistration
	for index := range resources {
		if resources[index].Name == application.TranscriptProfile &&
			resources[index].Profile == application.TranscriptProfile {
			registration = &resources[index]
			break
		}
	}
	if executor == nil || registration == nil {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT job.id, detail.asset_id, asset.accepted_fingerprint, grant.installation_id
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
JOIN assets asset ON asset.id = detail.asset_id
JOIN asset_media_state media ON media.asset_id = asset.id
JOIN source_grants grant ON grant.id = asset.source_grant_id
WHERE job.kind = 'transcript' AND job.state IN ('blocked', 'queued')
  AND asset.accepted_fingerprint IS NOT NULL AND media.facts_json IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM transcript_job_bindings binding WHERE binding.job_id = job.id)
ORDER BY job.created_at, job.id`)
	if err != nil {
		return err
	}
	type candidate struct {
		jobID, assetID, fingerprint, installationID string
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var current candidate
		if err := rows.Scan(&current.jobID, &current.assetID, &current.fingerprint, &current.installationID); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, current)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, current := range candidates {
		jobID, err := domain.ParseMediaJobID(current.jobID)
		if err != nil {
			return application.ErrAssetInvalid
		}
		assetID, err := domain.ParseAssetID(current.assetID)
		if err != nil {
			return application.ErrAssetInvalid
		}
		fingerprint, err := domain.ParseDigest(current.fingerprint)
		if err != nil {
			return application.ErrAssetInvalid
		}
		streams, err := loadMediaClaimStreams(ctx, tx, assetID, fingerprint)
		if err != nil {
			return err
		}
		stream, found, err := application.SelectDefaultTranscriptAudioStream(streams)
		if err != nil {
			return application.ErrAssetInvalid
		}
		if !found {
			continue
		}
		var resourceValue, modelVersion, contentDigestValue string
		err = tx.QueryRowContext(ctx, `
SELECT id, version, content_digest
FROM product_resources
WHERE installation_id = ? AND catalog_entry_id = ? AND profile = ?
  AND entry_digest = ? AND state = 'ready'`,
			current.installationID, registration.Name, registration.Profile,
			registration.EntryDigest.String(),
		).Scan(&resourceValue, &modelVersion, &contentDigestValue)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		resourceID, err := domain.ParseResourceID(resourceValue)
		if err != nil {
			return application.ErrProductResourceInvalid
		}
		contentDigest, err := domain.ParseDigest(contentDigestValue)
		if err != nil {
			return application.ErrProductResourceInvalid
		}
		_, descriptorDigest, err := domain.CanonicalDigest(
			"open-cut/source-stream-descriptor", domain.MediaFactsSchema, stream.Descriptor,
		)
		if err != nil {
			return application.ErrAssetInvalid
		}
		binding := domain.TranscriptBinding{
			Schema: domain.TranscriptBindingSchema, AssetID: assetID, Fingerprint: fingerprint,
			SourceStreamID: stream.ID, SourceDescriptorDigest: descriptorDigest,
			SelectionPolicy:     domain.TranscriptSelectionDefaultV1,
			NormalizationPolicy: domain.TranscriptNormalizationV1,
			LanguagePolicy:      domain.TranscriptLanguageAutoOriginal,
			EngineVersion:       executor.Version, EngineTarget: executor.Target,
			ModelResourceID: resourceID, ModelName: registration.Name, ModelVersion: modelVersion,
			ModelEntryDigest: registration.EntryDigest, ModelContentDigest: contentDigest,
		}
		canonical, digest, err := application.CanonicalTranscriptBinding(binding)
		if err != nil {
			return application.ErrAssetInvalid
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO transcript_job_bindings (
  job_id, schema_version, binding_digest, source_stream_id, source_descriptor_digest,
  selection_policy, normalization_policy, language_policy, engine_version, engine_target,
  model_resource_id, model_name, model_version, model_entry_digest, model_content_digest,
  canonical_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			jobID.String(), binding.Schema, digest.String(), binding.SourceStreamID.String(),
			binding.SourceDescriptorDigest.String(), binding.SelectionPolicy, binding.NormalizationPolicy,
			binding.LanguagePolicy, binding.EngineVersion, binding.EngineTarget,
			binding.ModelResourceID.String(), binding.ModelName, binding.ModelVersion,
			binding.ModelEntryDigest.String(), binding.ModelContentDigest.String(), string(canonical),
			formatInstant(now.UTC()),
		); err != nil {
			return err
		}
	}
	return nil
}

func loadTranscriptBinding(
	ctx context.Context,
	tx *sql.Tx,
	jobID domain.MediaJobID,
	assetID domain.AssetID,
	fingerprint domain.Digest,
) (domain.TranscriptBinding, error) {
	var canonicalJSON, bindingDigestValue string
	var streamAsset, streamFingerprint, streamDescriptorDigest string
	var resourceID, resourceName, resourceVersion, resourceEntryDigest, resourceContentDigest, resourceState string
	err := tx.QueryRowContext(ctx, `
SELECT binding.canonical_json, binding.binding_digest,
       stream.asset_id, stream.fingerprint, stream.descriptor_digest,
       resource.id, resource.catalog_entry_id, resource.version,
       resource.entry_digest, resource.content_digest, resource.state
FROM transcript_job_bindings binding
JOIN source_streams stream ON stream.id = binding.source_stream_id
JOIN product_resources resource ON resource.id = binding.model_resource_id
WHERE binding.job_id = ?`, jobID.String()).Scan(
		&canonicalJSON, &bindingDigestValue,
		&streamAsset, &streamFingerprint, &streamDescriptorDigest,
		&resourceID, &resourceName, &resourceVersion,
		&resourceEntryDigest, &resourceContentDigest, &resourceState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TranscriptBinding{}, application.ErrAssetInvalid
	}
	if err != nil {
		return domain.TranscriptBinding{}, err
	}
	var envelope struct {
		Domain  string                   `json:"domain"`
		Payload domain.TranscriptBinding `json:"payload"`
		Schema  string                   `json:"schema"`
	}
	if err := json.Unmarshal([]byte(canonicalJSON), &envelope); err != nil ||
		envelope.Domain != "open-cut/transcript-binding" || envelope.Schema != domain.TranscriptBindingSchema ||
		envelope.Payload.Validate() != nil {
		return domain.TranscriptBinding{}, application.ErrAssetInvalid
	}
	binding := envelope.Payload
	canonical, digest, err := application.CanonicalTranscriptBinding(binding)
	if err != nil || string(canonical) != canonicalJSON || digest.String() != bindingDigestValue ||
		binding.AssetID != assetID || binding.Fingerprint != fingerprint ||
		streamAsset != assetID.String() || streamFingerprint != fingerprint.String() ||
		streamDescriptorDigest != binding.SourceDescriptorDigest.String() ||
		resourceID != binding.ModelResourceID.String() || resourceName != binding.ModelName ||
		resourceVersion != binding.ModelVersion || resourceEntryDigest != binding.ModelEntryDigest.String() ||
		resourceContentDigest != binding.ModelContentDigest.String() || resourceState != "ready" {
		return domain.TranscriptBinding{}, application.ErrAssetInvalid
	}
	return binding, nil
}
