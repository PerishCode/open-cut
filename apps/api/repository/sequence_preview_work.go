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

func (repository *SQLiteProjects) LoadSequencePreviewPreparation(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	expectedSequenceRevision domain.Revision,
) (application.SequencePreviewPreparationSnapshot, error) {
	if projectID.IsZero() || sequenceID.IsZero() || expectedSequenceRevision.Value() == 0 {
		return application.SequencePreviewPreparationSnapshot{}, application.ErrSequencePreviewInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequencePreviewPreparationSnapshot{}, err
	}
	defer tx.Rollback()
	project, err := loadProjectProjection(ctx, tx, projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequencePreviewPreparationSnapshot{}, application.ErrRenderSequenceNotFound
	}
	if err != nil {
		return application.SequencePreviewPreparationSnapshot{}, err
	}
	if project.Status == domain.ProjectTombstoned || len(project.Sequences) != 1 ||
		project.Sequences[0].ID != sequenceID {
		return application.SequencePreviewPreparationSnapshot{}, application.ErrRenderSequenceNotFound
	}
	sequence := project.Sequences[0]
	if sequence.Revision != expectedSequenceRevision {
		return application.SequencePreviewPreparationSnapshot{}, application.ErrRenderSequenceConflict
	}
	clips, err := loadRenderClips(ctx, tx, projectID, sequenceID)
	if err != nil {
		return application.SequencePreviewPreparationSnapshot{}, err
	}
	captions, err := loadRenderCaptions(ctx, tx, projectID, sequenceID)
	if err != nil {
		return application.SequencePreviewPreparationSnapshot{}, err
	}
	assets, candidates, err := loadRenderAssetsAndCandidates(ctx, tx, projectID, clips)
	if err != nil {
		return application.SequencePreviewPreparationSnapshot{}, err
	}
	streams, err := loadSequencePreviewStreams(ctx, tx, clips, assets)
	if err != nil {
		return application.SequencePreviewPreparationSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequencePreviewPreparationSnapshot{}, err
	}
	loaded, err := repository.loadRenderCandidateManifests(candidates)
	if err != nil {
		return application.SequencePreviewPreparationSnapshot{}, err
	}
	publicCandidates := make(map[string][]application.SequencePreviewProxyCandidate, len(loaded))
	for assetID, records := range loaded {
		for _, candidate := range records {
			publicCandidates[assetID] = append(publicCandidates[assetID], application.SequencePreviewProxyCandidate{
				ProducerJobID: candidate.producerJobID,
				Artifact:      candidate.summary,
				Manifest:      candidate.manifest,
			})
		}
	}
	return application.SequencePreviewPreparationSnapshot{
		ProjectID: projectID, ObservedProjectRevision: project.Revision,
		Sequence: sequence, Clips: clips, Captions: captions, Assets: assets,
		Streams: streams, Candidates: publicCandidates,
	}, nil
}

func loadSequencePreviewStreams(
	ctx context.Context,
	tx *sql.Tx,
	clips []domain.ClipState,
	assets map[string]application.RenderAssetSnapshot,
) (map[string]domain.SourceStream, error) {
	result := make(map[string]domain.SourceStream)
	for _, clip := range clips {
		if !clip.Enabled || clip.Tombstoned {
			continue
		}
		if _, exists := result[clip.SourceStreamID.String()]; exists {
			continue
		}
		asset, exists := assets[clip.AssetID.String()]
		if !exists {
			return nil, application.ErrRenderInputRequired
		}
		var descriptorJSON string
		if err := tx.QueryRowContext(ctx, `
SELECT descriptor_json FROM source_streams
WHERE id = ? AND asset_id = ? AND fingerprint = ?`,
			clip.SourceStreamID.String(), clip.AssetID.String(), asset.AcceptedFingerprint.String(),
		).Scan(&descriptorJSON); errors.Is(err, sql.ErrNoRows) {
			return nil, application.ErrRenderInputRequired
		} else if err != nil {
			return nil, err
		}
		var descriptor domain.SourceStreamDescriptor
		if err := json.Unmarshal([]byte(descriptorJSON), &descriptor); err != nil || descriptor.Validate() != nil {
			return nil, application.ErrRenderPlanInvalid
		}
		result[clip.SourceStreamID.String()] = domain.SourceStream{
			ID: clip.SourceStreamID, Descriptor: descriptor,
		}
	}
	return result, nil
}

func (repository *SQLiteProjects) EnsureExplicitSourceProxyJob(
	ctx context.Context,
	record application.EnsureExplicitSourceProxyJobRecord,
) (domain.WorkJobID, error) {
	if err := validateExplicitSourceProxyJobRecord(record); err != nil {
		return domain.WorkJobID{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.WorkJobID{}, err
	}
	defer tx.Rollback()
	var fingerprint, availability string
	for _, sourceStream := range record.SourceStreams {
		var descriptorJSON string
		if err := tx.QueryRowContext(ctx, `
SELECT asset.accepted_fingerprint, media.availability, stream.descriptor_json
FROM assets asset
JOIN asset_media_state media ON media.asset_id = asset.id
JOIN source_streams stream ON stream.id = ? AND stream.asset_id = asset.id
  AND stream.fingerprint = asset.accepted_fingerprint
	WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0`,
			sourceStream.ID.String(), record.AssetID.String(), record.ProjectID.String(),
		).Scan(&fingerprint, &availability, &descriptorJSON); errors.Is(err, sql.ErrNoRows) {
			return domain.WorkJobID{}, application.ErrRenderInputRequired
		} else if err != nil {
			return domain.WorkJobID{}, err
		}
		storedDescriptor, err := json.Marshal(sourceStream.Descriptor)
		if err != nil || fingerprint != record.Fingerprint.String() || string(storedDescriptor) != descriptorJSON ||
			(availability != string(domain.AssetOnline) && availability != string(domain.AssetManagedState)) {
			return domain.WorkJobID{}, application.ErrRenderInputRequired
		}
	}
	if existing, found, err := findReusableExplicitProxyJob(ctx, tx, record); err != nil {
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
WHERE job.logical_key = ? AND job.kind = 'proxy' AND job.state = 'succeeded'
  AND job.project_id = ? AND detail.asset_id = ?
  AND NOT EXISTS (
    SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id
  )
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
) VALUES (?, 'project', ?, 'proxy', 'blocked', 'cpu', 'foreground', ?, ?, ?, ?, ?, ?, ?)`,
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
VALUES (?, 'executor-required', 'capability', 'media-executor/proxy', ?)`,
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

func findReusableExplicitProxyJob(
	ctx context.Context,
	tx *sql.Tx,
	record application.EnsureExplicitSourceProxyJobRecord,
) (domain.WorkJobID, bool, error) {
	var idValue, digest, parameters string
	err := tx.QueryRowContext(ctx, `
SELECT job.id, job.parameters_digest, job.parameters_json
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
WHERE job.logical_key = ? AND job.kind = 'proxy' AND job.project_id = ? AND detail.asset_id = ?
  AND NOT EXISTS (
    SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id
  )
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
		return domain.WorkJobID{}, false, application.ErrSequencePreviewInvalid
	}
	id, err := domain.ParseWorkJobID(idValue)
	return id, err == nil, err
}

func validateExplicitSourceProxyJobRecord(record application.EnsureExplicitSourceProxyJobRecord) error {
	if record.JobID.IsZero() || record.ProjectID.IsZero() || record.AssetID.IsZero() ||
		len(record.SourceStreams) == 0 || len(record.SourceStreams) > 2 ||
		record.Parameters.Validate() != nil || record.Parameters.AssetID != record.AssetID ||
		record.Parameters.Kind != domain.MediaJobProxy || record.Parameters.ProxySelection == nil ||
		record.Parameters.ProxySelection.Policy != application.SourceProxySelectionExplicit ||
		record.CreatedAt.IsZero() || record.LogicalKey == "" || len(record.LogicalKey) > 1024 {
		return application.ErrSequencePreviewInvalid
	}
	seen := make(map[string]struct{}, len(record.SourceStreams))
	for _, stream := range record.SourceStreams {
		if stream.ID.IsZero() || stream.Descriptor.Validate() != nil {
			return application.ErrSequencePreviewInvalid
		}
		if _, duplicate := seen[stream.ID.String()]; duplicate {
			return application.ErrSequencePreviewInvalid
		}
		seen[stream.ID.String()] = struct{}{}
	}
	video, audio, err := application.SelectSourceProxyStreams(record.SourceStreams, *record.Parameters.ProxySelection)
	if err != nil || (video == nil && audio == nil) || len(record.SourceStreams) != boolCount(video != nil)+boolCount(audio != nil) {
		return application.ErrSequencePreviewInvalid
	}
	canonical, digest, err := application.CanonicalInitialMediaJobParameters(record.Parameters)
	if err != nil || !bytes.Equal(canonical, record.Canonical) || digest != record.Digest {
		return application.ErrSequencePreviewInvalid
	}
	if _, err := domain.ParseDigest(record.Fingerprint.String()); err != nil {
		return application.ErrSequencePreviewInvalid
	}
	return nil
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (repository *SQLiteProjects) EnsureSequencePreviewJob(
	ctx context.Context,
	record application.EnsureSequencePreviewJobRecord,
) (application.SequencePreviewJobProjection, error) {
	if err := validateSequencePreviewJobRecord(record); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	defer tx.Rollback()
	var revision uint64
	if err := tx.QueryRowContext(ctx, `
SELECT sequence.revision
FROM sequences sequence
JOIN projects project ON project.id = sequence.project_id
WHERE sequence.id = ? AND sequence.project_id = ?
  AND project.status != 'tombstoned'`,
		record.Parameters.SequenceID.String(), record.Parameters.ProjectID.String(),
	).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		return application.SequencePreviewJobProjection{}, application.ErrRenderSequenceNotFound
	} else if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if revision != record.Parameters.SequenceRevision.Value() {
		return application.SequencePreviewJobProjection{}, application.ErrRenderSequenceConflict
	}
	if existing, found, err := findSequencePreviewJob(ctx, tx, record); err != nil {
		return application.SequencePreviewJobProjection{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return application.SequencePreviewJobProjection{}, err
		}
		return existing, nil
	}
	if err := validateSequencePreviewProducers(ctx, tx, record.Parameters); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if err := validateSequencePreviewIntentAssets(ctx, tx, record.RenderIntent); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	at := formatInstant(record.CreatedAt.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, kind, state, pool, priority_class, logical_key,
  parameters_digest, parameters_json, producer_version, created_at, updated_at
) VALUES (?, 'project', ?, 'sequence-preview', 'blocked', 'cpu', 'foreground', ?, ?, ?, ?, ?, ?)`,
		record.JobID.String(), record.Parameters.ProjectID.String(), record.LogicalKey,
		record.Digest.String(), string(record.Canonical), record.Parameters.RendererVersion, at, at,
	); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_details (
  job_id, sequence_id, sequence_revision, resolver_version, compiler_version,
  renderer_version, renderer_target, output_profile,
  render_intent_schema, render_intent_digest, render_intent_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.JobID.String(), record.Parameters.SequenceID.String(), record.Parameters.SequenceRevision.Value(),
		record.Parameters.ResolverVersion, record.Parameters.CompilerVersion,
		record.Parameters.RendererVersion, record.Parameters.RendererTarget, record.Parameters.OutputProfile,
		application.SequencePreviewRenderIntentSchema, record.IntentDigest.String(), string(record.IntentCanonical),
	); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	producerPrerequisites := make(map[string]struct{})
	for ordinal, input := range record.Parameters.Inputs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_inputs (
  job_id, ordinal, clip_id, source_stream_id, producer_job_id
) VALUES (?, ?, ?, ?, ?)`, record.JobID.String(), ordinal, input.ClipID.String(),
			input.SourceStreamID.String(), input.ProducerJobID.String()); err != nil {
			return application.SequencePreviewJobProjection{}, err
		}
		if _, exists := producerPrerequisites[input.ProducerJobID.String()]; !exists {
			producerPrerequisites[input.ProducerJobID.String()] = struct{}{}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'artifact-ready', 'job', ?, ?)`, record.JobID.String(), input.ProducerJobID.String(), at); err != nil {
				return application.SequencePreviewJobProjection{}, err
			}
		}
	}
	for ordinal, resource := range record.Parameters.Resources {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_job_resources (
  job_id, ordinal, resource_kind, resource_id, resource_version, resource_digest
) VALUES (?, ?, ?, ?, ?, ?)`, record.JobID.String(), ordinal, resource.Kind,
			resource.ID, resource.Version, resource.SHA256.String()); err != nil {
			return application.SequencePreviewJobProjection{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'work-executor/sequence-preview', ?)`,
		record.JobID.String(), at); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
VALUES (?, 'project', ?, ?)`, record.JobID.String(), record.Parameters.ProjectID.String(), at); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	result, err := loadSequencePreviewJobProjection(ctx, tx, record.JobID)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	return result, nil
}

func validateSequencePreviewIntentAssets(
	ctx context.Context,
	tx *sql.Tx,
	intent application.SequencePreviewRenderIntent,
) error {
	for _, asset := range intent.Assets {
		var revision uint64
		var fingerprint string
		if err := tx.QueryRowContext(ctx, `
SELECT revision, accepted_fingerprint FROM assets
WHERE id = ? AND project_id = ? AND tombstoned = 0`,
			asset.ID.String(), intent.ProjectID.String(),
		).Scan(&revision, &fingerprint); err != nil {
			return application.ErrRenderInputRequired
		}
		if revision != asset.Revision.Value() || fingerprint != asset.AcceptedFingerprint.String() {
			return application.ErrRenderInputRequired
		}
	}
	return nil
}

func validateSequencePreviewProducers(
	ctx context.Context,
	tx *sql.Tx,
	parameters application.SequencePreviewJobParameters,
) error {
	for _, input := range parameters.Inputs {
		var clipStream, clipAsset, producerAsset, kind, producerProject string
		if err := tx.QueryRowContext(ctx, `
SELECT clip.source_stream_id, clip.asset_id, detail.asset_id, job.kind, job.project_id
FROM clips clip
JOIN media_job_details detail ON detail.job_id = ?
JOIN work_jobs job ON job.id = detail.job_id
WHERE clip.id = ? AND clip.project_id = ? AND clip.sequence_id = ?
  AND clip.tombstoned = 0 AND clip.enabled = 1`,
			input.ProducerJobID.String(), input.ClipID.String(), parameters.ProjectID.String(),
			parameters.SequenceID.String(),
		).Scan(&clipStream, &clipAsset, &producerAsset, &kind, &producerProject); err != nil {
			return application.ErrRenderInputRequired
		}
		if clipStream != input.SourceStreamID.String() || clipAsset != producerAsset ||
			kind != string(domain.MediaJobProxy) || producerProject != parameters.ProjectID.String() {
			return application.ErrRenderInputRequired
		}
	}
	return nil
}

func findSequencePreviewJob(
	ctx context.Context,
	tx *sql.Tx,
	record application.EnsureSequencePreviewJobRecord,
) (application.SequencePreviewJobProjection, bool, error) {
	var idValue, digest, canonical, intentSchema, intentDigest, intentJSON string
	err := tx.QueryRowContext(ctx, `
SELECT job.id, job.parameters_digest, job.parameters_json,
       detail.render_intent_schema, detail.render_intent_digest, detail.render_intent_json
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE job.logical_key = ? AND job.kind = 'sequence-preview'
  AND (
    job.state IN ('blocked', 'queued', 'running', 'failed', 'cancelled') OR
    (job.state = 'succeeded' AND EXISTS (
      SELECT 1
      FROM sequence_preview_artifacts artifact
      WHERE artifact.id = detail.result_artifact_id AND artifact.state = 'ready'
    ))
  )
ORDER BY job.created_at DESC, job.id DESC LIMIT 1`, record.LogicalKey).Scan(
		&idValue, &digest, &canonical, &intentSchema, &intentDigest, &intentJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequencePreviewJobProjection{}, false, nil
	}
	if err != nil {
		return application.SequencePreviewJobProjection{}, false, err
	}
	if digest != record.Digest.String() || canonical != string(record.Canonical) ||
		intentSchema != application.SequencePreviewRenderIntentSchema ||
		intentDigest != record.IntentDigest.String() || intentJSON != string(record.IntentCanonical) {
		return application.SequencePreviewJobProjection{}, false, application.ErrSequencePreviewInvalid
	}
	id, err := domain.ParseWorkJobID(idValue)
	if err != nil {
		return application.SequencePreviewJobProjection{}, false, err
	}
	projection, err := loadSequencePreviewJobProjection(ctx, tx, id)
	return projection, err == nil, err
}

func (repository *SQLiteProjects) LoadSequencePreviewContinuation(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
) (application.SequencePreviewJobProjection, error) {
	if projectID.IsZero() || sequenceID.IsZero() || sequenceRevision.Value() == 0 || jobID.IsZero() {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	defer tx.Rollback()
	var tailValue string
	err = tx.QueryRowContext(ctx, `
WITH RECURSIVE chain(id) AS (
  SELECT job.id
  FROM work_jobs job
  JOIN projects project ON project.id = job.project_id
  JOIN sequence_preview_job_details detail ON detail.job_id = job.id
  WHERE job.id = ? AND job.kind = 'sequence-preview' AND job.project_id = ?
    AND detail.sequence_id = ? AND detail.sequence_revision = ?
    AND project.status = 'active'
  UNION
  SELECT retry.id
  FROM work_jobs retry
  JOIN chain predecessor ON retry.retry_of_job_id = predecessor.id
  JOIN sequence_preview_job_details retry_detail ON retry_detail.job_id = retry.id
  WHERE retry.kind = 'sequence-preview' AND retry.project_id = ?
    AND retry_detail.sequence_id = ? AND retry_detail.sequence_revision = ?
)
SELECT job.id
FROM chain
JOIN work_jobs job ON job.id = chain.id
WHERE NOT EXISTS (
  SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id
)
LIMIT 1`,
		jobID.String(), projectID.String(), sequenceID.String(), sequenceRevision.Value(),
		projectID.String(), sequenceID.String(), sequenceRevision.Value(),
	).Scan(&tailValue)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewNotFound
	}
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	tailID, err := domain.ParseWorkJobID(tailValue)
	if err != nil {
		return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
	}
	projection, err := loadSequencePreviewJobProjection(ctx, tx, tailID)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	return projection, nil
}

func loadSequencePreviewJobProjection(
	ctx context.Context,
	tx *sql.Tx,
	jobID domain.WorkJobID,
) (application.SequencePreviewJobProjection, error) {
	var idValue, state, createdAtValue, updatedAtValue string
	var progress uint16
	var planValue, artifactValue sql.NullString
	var terminalError sql.NullString
	if err := tx.QueryRowContext(ctx, `
SELECT job.id, job.state, job.progress_basis_points, job.terminal_error_code,
       detail.render_plan_digest, detail.result_artifact_id, job.created_at, job.updated_at
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE job.id = ?`, jobID.String()).Scan(
		&idValue, &state, &progress, &terminalError, &planValue, &artifactValue,
		&createdAtValue, &updatedAtValue,
	); err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	id, err := domain.ParseWorkJobID(idValue)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtValue)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtValue)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	result := application.SequencePreviewJobProjection{
		ID: id, State: domain.WorkJobState(state), ProgressBasisPoints: progress,
		CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
	if terminalError.Valid {
		result.TerminalErrorCode = &terminalError.String
	}
	if planValue.Valid {
		digest, parseErr := domain.ParseDigest(planValue.String)
		if parseErr != nil {
			return application.SequencePreviewJobProjection{}, parseErr
		}
		result.RenderPlanDigest = &digest
	}
	if artifactValue.Valid {
		artifactID, parseErr := domain.ParseArtifactID(artifactValue.String)
		if parseErr != nil {
			return application.SequencePreviewJobProjection{}, parseErr
		}
		artifact, loadErr := loadSequencePreviewArtifactSummary(ctx, tx, artifactID)
		if loadErr != nil {
			return application.SequencePreviewJobProjection{}, loadErr
		}
		result.Artifact = &artifact
	}
	return result, nil
}

func loadSequencePreviewArtifactSummary(
	ctx context.Context,
	tx *sql.Tx,
	artifactID domain.ArtifactID,
) (domain.SequencePreviewArtifactSummary, error) {
	var idValue, projectValue, sequenceValue, planValue, renderer, target, profile, state string
	var factsJSON, contentDigest string
	var sequenceRevision, byteSize uint64
	if err := tx.QueryRowContext(ctx, `
SELECT id, project_id, sequence_id, sequence_revision, render_plan_digest,
       renderer_version, renderer_target, output_profile, state, facts_json,
       byte_size, content_digest
FROM sequence_preview_artifacts WHERE id = ?`, artifactID.String()).Scan(
		&idValue, &projectValue, &sequenceValue, &sequenceRevision, &planValue,
		&renderer, &target, &profile, &state, &factsJSON, &byteSize, &contentDigest,
	); err != nil {
		return domain.SequencePreviewArtifactSummary{}, err
	}
	id, err := domain.ParseArtifactID(idValue)
	projectID, projectErr := domain.ParseProjectID(projectValue)
	sequenceID, sequenceErr := domain.ParseSequenceID(sequenceValue)
	revision, revisionErr := domain.NewRevision(sequenceRevision)
	planDigest, planErr := domain.ParseDigest(planValue)
	digest, digestErr := domain.ParseDigest(contentDigest)
	size, sizeErr := domain.NewUInt64(byteSize)
	if err != nil || projectErr != nil || sequenceErr != nil || revisionErr != nil ||
		planErr != nil || digestErr != nil || sizeErr != nil {
		return domain.SequencePreviewArtifactSummary{}, application.ErrSequencePreviewInvalid
	}
	var facts domain.SequencePreviewMediaFacts
	if err := json.Unmarshal([]byte(factsJSON), &facts); err != nil ||
		application.ValidateSequencePreviewFacts(facts) != nil {
		return domain.SequencePreviewArtifactSummary{}, application.ErrSequencePreviewInvalid
	}
	return domain.SequencePreviewArtifactSummary{
		ID: id, ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: revision,
		RenderPlanDigest: planDigest, RendererVersion: renderer, RendererTarget: target,
		Profile: profile, State: domain.SequencePreviewArtifactState(state), Facts: facts,
		ByteSize: size, ContentDigest: digest,
	}, nil
}

func validateSequencePreviewJobRecord(record application.EnsureSequencePreviewJobRecord) error {
	if record.JobID.IsZero() || record.CreatedAt.IsZero() || record.LogicalKey == "" ||
		len(record.LogicalKey) > 1024 || record.Parameters.Validate() != nil {
		return application.ErrSequencePreviewInvalid
	}
	canonical, digest, normalized, err := application.CanonicalSequencePreviewJobParameters(record.Parameters)
	if err != nil || normalized.Validate() != nil || !bytes.Equal(canonical, record.Canonical) || digest != record.Digest {
		return application.ErrSequencePreviewInvalid
	}
	intent, intentCanonical, intentDigest, err := application.CanonicalSequencePreviewRenderIntent(
		record.RenderIntent, normalized.Inputs,
	)
	if err != nil || intent.ProjectID != normalized.ProjectID || intent.SequenceID != normalized.SequenceID ||
		intent.SequenceRevision != normalized.SequenceRevision ||
		!bytes.Equal(intentCanonical, record.IntentCanonical) || intentDigest != record.IntentDigest {
		return application.ErrSequencePreviewInvalid
	}
	return nil
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
