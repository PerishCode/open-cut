package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) RecoverMediaJobs(
	ctx context.Context,
	executors []application.MediaExecutorRegistration,
	now time.Time,
) error {
	if !validMediaExecutors(executors) || now.IsZero() {
		return application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recoverExpiredWorkAttempts(ctx, tx, now.UTC()); err != nil {
		return err
	}
	if err := reconcileMediaPrerequisites(ctx, tx, executors, nil, now.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (repository *SQLiteProjects) ClaimMediaJob(
	ctx context.Context,
	input application.ClaimMediaJobInput,
) (application.MediaJobClaim, error) {
	if input.AttemptID.IsZero() || !validMediaExecutors(input.Executors) ||
		(input.OnlyJobID != nil && input.OnlyJobID.IsZero()) ||
		input.LeaseOwner == "" || len(input.LeaseOwner) > 128 ||
		input.Now.IsZero() || input.LeaseDuration < 3*time.Second || input.LeaseDuration > 10*time.Minute {
		return application.MediaJobClaim{}, application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	defer tx.Rollback()
	if err := recoverExpiredWorkAttempts(ctx, tx, input.Now.UTC()); err != nil {
		return application.MediaJobClaim{}, err
	}
	if err := reconcileMediaPrerequisites(ctx, tx, input.Executors, input.Resources, input.Now.UTC()); err != nil {
		return application.MediaJobClaim{}, err
	}
	var (
		jobValue, projectValue, assetValue, grantValue, kind, fileIdentity string
		parametersDigestValue, parametersJSON                              string
		acceptedValue                                                      sql.NullString
		byteSize, generation                                               uint64
		modifiedUnixNs                                                     int64
	)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(input.Executors)), ",")
	selectedClause := ""
	if input.OnlyJobID != nil {
		selectedClause = " AND j.id = ?"
	}
	query := `
SELECT j.id, j.project_id, detail.asset_id, a.source_grant_id, j.kind, a.accepted_fingerprint,
       sg.observed_byte_size, sg.observed_modified_unix_ns, sg.observed_file_identity,
       j.parameters_digest, j.parameters_json,
       COALESCE((SELECT MAX(generation) FROM work_job_attempts WHERE job_id = j.id), 0) + 1
FROM work_jobs j
JOIN media_job_details detail ON detail.job_id = j.id
JOIN assets a ON a.id = detail.asset_id
JOIN source_grants sg ON sg.id = a.source_grant_id
WHERE j.state = 'queued' AND j.kind IN (` + placeholders + `) AND j.cancellation_requested = 0
  AND a.tombstoned = 0 AND sg.state = 'active'
  AND (j.kind = 'identify' OR a.accepted_fingerprint IS NOT NULL)` + selectedClause + `
ORDER BY CASE j.priority_class WHEN 'interactive' THEN 0 WHEN 'foreground' THEN 1 ELSE 2 END,
         j.created_at, j.id
LIMIT 1`
	arguments := make([]any, 0, len(input.Executors))
	for _, executor := range input.Executors {
		arguments = append(arguments, string(executor.Kind))
	}
	if input.OnlyJobID != nil {
		arguments = append(arguments, input.OnlyJobID.String())
	}
	err = tx.QueryRowContext(ctx, query, arguments...).Scan(
		&jobValue, &projectValue, &assetValue, &grantValue, &kind, &acceptedValue,
		&byteSize, &modifiedUnixNs, &fileIdentity, &parametersDigestValue, &parametersJSON, &generation,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.MediaJobClaim{}, application.ErrNoMediaWork
	}
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	executorVersion, executorTarget := "", ""
	for _, executor := range input.Executors {
		if string(executor.Kind) == kind {
			executorVersion = executor.Version
			executorTarget = executor.Target
			break
		}
	}
	claim, err := parseMediaJobClaim(
		jobValue, projectValue, assetValue, grantValue, kind, generation,
		input, executorVersion, executorTarget, acceptedValue, byteSize, modifiedUnixNs, fileIdentity,
		parametersDigestValue, parametersJSON,
	)
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	if claim.Kind == domain.MediaJobFrameSet {
		parameters, parseErr := application.DecodeFrameSetParameters(claim.ParametersJSON)
		if parseErr != nil || parameters.AssetID != claim.AssetID || claim.AcceptedFingerprint == nil ||
			parameters.Fingerprint != *claim.AcceptedFingerprint {
			return application.MediaJobClaim{}, application.ErrAssetInvalid
		}
		var descriptorJSON string
		if err := tx.QueryRowContext(ctx, `
SELECT descriptor_json FROM source_streams
WHERE id = ? AND asset_id = ? AND fingerprint = ?`,
			parameters.SourceStreamID.String(), claim.AssetID.String(), parameters.Fingerprint.String(),
		).Scan(&descriptorJSON); err != nil {
			return application.MediaJobClaim{}, application.ErrAssetInvalid
		}
		var descriptor domain.SourceStreamDescriptor
		if err := json.Unmarshal([]byte(descriptorJSON), &descriptor); err != nil ||
			descriptor.Validate() != nil || descriptor.MediaType != domain.MediaVideo {
			return application.MediaJobClaim{}, application.ErrAssetInvalid
		}
		claim.SourceStream = &domain.SourceStream{ID: parameters.SourceStreamID, Descriptor: descriptor}
	} else if claim.Kind == domain.MediaJobProxy || claim.Kind == domain.MediaJobRenderInput {
		parameters, parseErr := application.DecodeInitialMediaJobParameters(claim.ParametersJSON)
		expectedProfile, profileErr := application.InitialMediaProfile(claim.Kind)
		if parseErr != nil || profileErr != nil || parameters.AssetID != claim.AssetID ||
			parameters.Kind != claim.Kind || parameters.Profile != expectedProfile || claim.AcceptedFingerprint == nil {
			return application.MediaJobClaim{}, application.ErrAssetInvalid
		}
		claim.SourceStreams, err = loadMediaClaimStreams(ctx, tx, claim.AssetID, *claim.AcceptedFingerprint)
		if err != nil {
			return application.MediaJobClaim{}, err
		}
	} else if claim.Kind == domain.MediaJobTranscript {
		parameters, parseErr := application.DecodeInitialMediaJobParameters(claim.ParametersJSON)
		if parseErr != nil || parameters.AssetID != claim.AssetID || parameters.Kind != domain.MediaJobTranscript ||
			parameters.Profile != application.TranscriptProfile || claim.AcceptedFingerprint == nil {
			return application.MediaJobClaim{}, application.ErrAssetInvalid
		}
		var bindingCount int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM transcript_job_bindings WHERE job_id = ?`, claim.JobID.String()).Scan(&bindingCount); err != nil {
			return application.MediaJobClaim{}, err
		}
		if bindingCount == 0 {
			streams, streamsErr := loadMediaClaimStreams(ctx, tx, claim.AssetID, *claim.AcceptedFingerprint)
			if streamsErr != nil {
				return application.MediaJobClaim{}, streamsErr
			}
			_, found, selectionErr := application.SelectDefaultTranscriptAudioStream(streams)
			if selectionErr != nil || found {
				return application.MediaJobClaim{}, application.ErrAssetInvalid
			}
			claim.TranscriptNoAudio = true
		} else if bindingCount != 1 {
			return application.MediaJobClaim{}, application.ErrAssetInvalid
		} else {
			binding, bindingErr := loadTranscriptBinding(
				ctx, tx, claim.JobID, claim.AssetID, *claim.AcceptedFingerprint,
			)
			if bindingErr != nil || binding.EngineVersion != claim.ExecutorVersion ||
				binding.EngineTarget != claim.ExecutorTarget {
				if bindingErr != nil {
					return application.MediaJobClaim{}, bindingErr
				}
				return application.MediaJobClaim{}, application.ErrAssetInvalid
			}
			claim.TranscriptBinding = &binding
			var descriptorJSON string
			if err := tx.QueryRowContext(ctx, `
SELECT descriptor_json FROM source_streams
WHERE id = ? AND asset_id = ? AND fingerprint = ?`,
				binding.SourceStreamID.String(), claim.AssetID.String(), claim.AcceptedFingerprint.String(),
			).Scan(&descriptorJSON); err != nil {
				return application.MediaJobClaim{}, application.ErrAssetInvalid
			}
			var descriptor domain.SourceStreamDescriptor
			if err := json.Unmarshal([]byte(descriptorJSON), &descriptor); err != nil ||
				descriptor.Validate() != nil || descriptor.MediaType != domain.MediaAudio {
				return application.MediaJobClaim{}, application.ErrAssetInvalid
			}
			_, descriptorDigest, digestErr := domain.CanonicalDigest(
				"open-cut/source-stream-descriptor", domain.MediaFactsSchema, descriptor,
			)
			if digestErr != nil || descriptorDigest != binding.SourceDescriptorDigest {
				return application.MediaJobClaim{}, application.ErrAssetInvalid
			}
			claim.SourceStream = &domain.SourceStream{ID: binding.SourceStreamID, Descriptor: descriptor}
		}
	}
	now, expires := formatInstant(input.Now.UTC()), formatInstant(claim.LeaseExpiresAt)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_attempts (
  id, job_id, generation, state, lease_owner, lease_expires_at, heartbeat_at,
  started_at, executor_version, temporary_output_identity
) VALUES (?, ?, ?, 'running', ?, ?, ?, ?, ?, ?)`,
		claim.AttemptID.String(), claim.JobID.String(), claim.Generation, claim.LeaseOwner,
		expires, now, now, claim.ExecutorVersion, claim.AttemptID.String(),
	); err != nil {
		return application.MediaJobClaim{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE work_jobs SET state = 'running', updated_at = ?
WHERE id = ? AND state = 'queued' AND cancellation_requested = 0`, now, claim.JobID.String())
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.MediaJobClaim{}, application.ErrMediaLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return application.MediaJobClaim{}, err
	}
	return claim, nil
}

func (repository *SQLiteProjects) RenewMediaJobLease(
	ctx context.Context,
	claim application.MediaJobClaim,
	now time.Time,
	duration time.Duration,
) error {
	if claim.JobID.IsZero() || claim.AttemptID.IsZero() || claim.LeaseOwner == "" || now.IsZero() ||
		duration < 3*time.Second || duration > 10*time.Minute {
		return application.ErrMediaLeaseLost
	}
	result, err := repository.db.ExecContext(ctx, `
UPDATE work_job_attempts
SET heartbeat_at = ?, lease_expires_at = ?
WHERE id = ? AND job_id = ? AND generation = ? AND lease_owner = ?
  AND state = 'running' AND lease_expires_at > ?`,
		formatInstant(now.UTC()), formatInstant(now.UTC().Add(duration)), claim.AttemptID.String(),
		claim.JobID.String(), claim.Generation, claim.LeaseOwner, formatInstant(now.UTC()),
	)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrMediaLeaseLost
	}
	return nil
}

func reconcileMediaPrerequisites(
	ctx context.Context,
	tx *sql.Tx,
	executors []application.MediaExecutorRegistration,
	resources []application.ProductResourceRegistration,
	now time.Time,
) error {
	if application.ValidateProductResourceRegistrations(resources) != nil {
		return application.ErrProductResourceInvalid
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM work_job_prerequisites
WHERE job_id IN (
  SELECT job.id FROM work_jobs job
  JOIN media_job_details detail ON detail.job_id = job.id
  WHERE job.state IN ('blocked', 'queued')
)`); err != nil {
		return err
	}
	if err := reconcileTranscriptBindings(ctx, tx, executors, resources, now.UTC()); err != nil {
		return err
	}
	at := formatInstant(now.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT dependent.id, 'fingerprint-required', 'job', producer.id, ?
FROM work_jobs dependent
JOIN media_job_details dependent_detail ON dependent_detail.job_id = dependent.id
JOIN assets asset ON asset.id = dependent_detail.asset_id
JOIN media_job_details producer_detail ON producer_detail.asset_id = dependent_detail.asset_id
JOIN work_jobs producer ON producer.id = producer_detail.job_id AND producer.kind = 'identify'
WHERE dependent.kind = 'probe' AND dependent.state IN ('blocked', 'queued')
  AND asset.accepted_fingerprint IS NULL`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT dependent.id, 'facts-required', 'job', producer.id, ?
FROM work_jobs dependent
JOIN media_job_details dependent_detail ON dependent_detail.job_id = dependent.id
JOIN asset_media_state media ON media.asset_id = dependent_detail.asset_id
JOIN media_job_details producer_detail ON producer_detail.asset_id = dependent_detail.asset_id
JOIN work_jobs producer ON producer.id = producer_detail.job_id AND producer.kind = 'probe'
WHERE dependent.kind IN ('frame-sample-set', 'proxy', 'waveform', 'transcript')
  AND dependent.state IN ('blocked', 'queued') AND media.facts_json IS NULL`, at); err != nil {
		return err
	}
	activeModelDigest := ""
	for _, resource := range resources {
		if resource.Name == application.TranscriptProfile && resource.Profile == application.TranscriptProfile {
			activeModelDigest = resource.EntryDigest.String()
			break
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT job.id, 'model-required', 'resource', 'whisper-small-multilingual-v1', ?
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
JOIN assets asset ON asset.id = detail.asset_id
JOIN asset_media_state media ON media.asset_id = asset.id
JOIN source_grants grant ON grant.id = asset.source_grant_id
WHERE job.kind = 'transcript' AND job.state IN ('blocked', 'queued')
  AND ((
    media.facts_json IS NULL AND NOT EXISTS (
      SELECT 1 FROM product_resources resource
      WHERE resource.installation_id = grant.installation_id
        AND resource.catalog_entry_id = 'whisper-small-multilingual-v1'
        AND resource.profile = 'whisper-small-multilingual-v1'
        AND resource.entry_digest = ? AND resource.state = 'ready'
    )
  ) OR (
    media.facts_json IS NOT NULL AND EXISTS (
      SELECT 1 FROM source_streams stream
      WHERE stream.asset_id = asset.id AND stream.fingerprint = asset.accepted_fingerprint
        AND stream.media_type = 'audio'
    ) AND NOT EXISTS (
      SELECT 1 FROM transcript_job_bindings binding
      JOIN product_resources resource ON resource.id = binding.model_resource_id
      WHERE binding.job_id = job.id
        AND binding.model_entry_digest = ?
        AND resource.installation_id = grant.installation_id
        AND resource.state = 'ready'
    )
  ))`, at, activeModelDigest, activeModelDigest); err != nil {
		return err
	}
	registered := make(map[domain.MediaJobKind]struct{}, len(executors))
	for _, executor := range executors {
		registered[executor.Kind] = struct{}{}
	}
	for _, kind := range []domain.MediaJobKind{
		domain.MediaJobIdentify, domain.MediaJobProbe, domain.MediaJobFrameSet,
		domain.MediaJobProxy, domain.MediaJobRenderInput, domain.MediaJobWaveform,
	} {
		if _, available := registered[kind]; available {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT id, 'executor-required', 'capability', ?, ?
FROM work_jobs WHERE kind = ? AND state IN ('blocked', 'queued')`,
			"media-executor/"+string(kind), at, string(kind)); err != nil {
			return err
		}
	}
	var transcriptExecutor *application.MediaExecutorRegistration
	for index := range executors {
		if executors[index].Kind == domain.MediaJobTranscript {
			transcriptExecutor = &executors[index]
			break
		}
	}
	if transcriptExecutor == nil {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT id, 'executor-required', 'capability', 'media-executor/transcript', ?
FROM work_jobs WHERE kind = 'transcript' AND state IN ('blocked', 'queued')`, at); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT job.id, 'executor-required', 'capability', 'media-executor/transcript', ?
FROM work_jobs job
JOIN transcript_job_bindings binding ON binding.job_id = job.id
WHERE job.kind = 'transcript' AND job.state IN ('blocked', 'queued')
  AND (binding.engine_version <> ? OR binding.engine_target <> ?)`,
		at, transcriptExecutor.Version, transcriptExecutor.Target); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = CASE
      WHEN EXISTS (SELECT 1 FROM work_job_prerequisites prerequisite WHERE prerequisite.job_id = work_jobs.id)
        THEN 'blocked'
      ELSE 'queued'
    END,
    updated_at = ?
WHERE state IN ('blocked', 'queued')`, at)
	return err
}

func queueSatisfiedMediaJobs(ctx context.Context, tx *sql.Tx, assetID domain.AssetID, at string) error {
	_, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = CASE
      WHEN EXISTS (SELECT 1 FROM work_job_prerequisites prerequisite WHERE prerequisite.job_id = work_jobs.id)
        THEN 'blocked'
      ELSE 'queued'
    END,
    updated_at = ?
WHERE id IN (SELECT job_id FROM media_job_details WHERE asset_id = ?)
  AND state IN ('blocked', 'queued')`, at, assetID.String())
	return err
}

func (repository *SQLiteProjects) CompleteMediaIdentification(
	ctx context.Context,
	input application.CompleteMediaIdentification,
) error {
	if input.EventID.IsZero() || input.CompletedAt.IsZero() || input.Observation != input.Claim.ExpectedObservation {
		return application.ErrAssetInvalid
	}
	if _, err := domain.ParseDigest(input.Fingerprint.String()); err != nil {
		return application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyMediaAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC(), domain.MediaJobIdentify); err != nil {
		return err
	}
	at := formatInstant(input.CompletedAt.UTC())
	result, err := tx.ExecContext(ctx, `
UPDATE assets SET accepted_fingerprint = ?
WHERE id = ? AND project_id = ? AND source_grant_id = ?
  AND (accepted_fingerprint IS NULL OR accepted_fingerprint = ?)`,
		input.Fingerprint.String(), input.Claim.AssetID.String(), input.Claim.ProjectID.String(),
		input.Claim.SourceGrantID.String(), input.Fingerprint.String(),
	)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrMediaLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE asset_media_state
SET availability = 'online', observed_fingerprint = ?, updated_at = ?
WHERE asset_id = ?`, input.Fingerprint.String(), at, input.Claim.AssetID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'succeeded', progress_basis_points = 10000,
    updated_at = ?, terminal_error_code = NULL
WHERE id = ? AND state = 'running'`, at, input.Claim.JobID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'succeeded', heartbeat_at = ?, ended_at = ?, diagnostics_json = '{}'
WHERE id = ? AND state = 'running'`,
		at, at, input.Claim.AttemptID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM work_job_prerequisites
WHERE kind = 'fingerprint-required'
  AND job_id IN (SELECT job_id FROM media_job_details WHERE asset_id = ?)`, input.Claim.AssetID.String()); err != nil {
		return err
	}
	if err := queueSatisfiedMediaJobs(ctx, tx, input.Claim.AssetID, at); err != nil {
		return err
	}
	if err := appendMediaJobActivity(ctx, tx, input.Claim, input.EventID, input.CompletedAt, "media.identified", "media-identified", nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (repository *SQLiteProjects) CompleteMediaProbe(
	ctx context.Context,
	input application.CompleteMediaProbe,
) error {
	if input.EventID.IsZero() || input.ArtifactID.IsZero() || input.CompletedAt.IsZero() ||
		input.Claim.Kind != domain.MediaJobProbe || input.Claim.AcceptedFingerprint == nil ||
		input.Facts.Validate() != nil {
		return application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyMediaAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC(), domain.MediaJobProbe); err != nil {
		return err
	}
	var accepted, observed, parametersDigest, parametersJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT a.accepted_fingerprint, state.observed_fingerprint, job.parameters_digest, job.parameters_json
FROM assets a
JOIN asset_media_state state ON state.asset_id = a.id
JOIN media_job_details detail ON detail.asset_id = a.id
JOIN work_jobs job ON job.id = detail.job_id AND job.id = ?
WHERE a.id = ? AND a.project_id = ?`,
		input.Claim.JobID.String(), input.Claim.AssetID.String(), input.Claim.ProjectID.String(),
	).Scan(&accepted, &observed, &parametersDigest, &parametersJSON); err != nil {
		return err
	}
	fingerprint := input.Claim.AcceptedFingerprint.String()
	if accepted != fingerprint || observed != fingerprint {
		return application.ErrMediaLeaseLost
	}
	for index := range input.Facts.Streams {
		stream := input.Facts.Streams[index]
		descriptorJSON, err := json.Marshal(stream.Descriptor)
		if err != nil {
			return err
		}
		_, descriptorDigest, err := domain.CanonicalDigest(
			"open-cut/source-stream-descriptor", domain.MediaFactsSchema, stream.Descriptor,
		)
		if err != nil {
			return err
		}
		var stableIDValue string
		err = tx.QueryRowContext(ctx, `
SELECT id FROM source_streams
WHERE asset_id = ? AND fingerprint = ? AND container_index = ?`,
			input.Claim.AssetID.String(), fingerprint, stream.Descriptor.Index,
		).Scan(&stableIDValue)
		if errors.Is(err, sql.ErrNoRows) {
			stableIDValue = stream.ID.String()
			_, err = tx.ExecContext(ctx, `
INSERT INTO source_streams (
			  id, asset_id, fingerprint, container_index, descriptor_digest, media_type, descriptor_json
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				stableIDValue, input.Claim.AssetID.String(), fingerprint, stream.Descriptor.Index,
				descriptorDigest.String(), string(stream.Descriptor.MediaType), string(descriptorJSON),
			)
		} else if err == nil {
			_, err = tx.ExecContext(ctx, `
UPDATE source_streams
SET descriptor_digest = ?, media_type = ?, descriptor_json = ?
WHERE id = ? AND asset_id = ? AND fingerprint = ? AND container_index = ?`,
				descriptorDigest.String(), string(stream.Descriptor.MediaType), string(descriptorJSON),
				stableIDValue, input.Claim.AssetID.String(), fingerprint, stream.Descriptor.Index,
			)
		}
		if err != nil {
			return err
		}
		stableID, err := domain.ParseSourceStreamID(stableIDValue)
		if err != nil {
			return err
		}
		input.Facts.Streams[index].ID = stableID
	}
	factsJSON, err := json.Marshal(input.Facts)
	if err != nil {
		return err
	}
	canonicalFacts, contentDigest, err := domain.CanonicalDigest(
		"open-cut/media-facts", domain.MediaFactsSchema, input.Facts,
	)
	if err != nil {
		return err
	}
	at := formatInstant(input.CompletedAt.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO media_artifacts (
  id, project_id, asset_id, kind, producer_version, input_fingerprint,
  parameters_digest, parameters_json, state, byte_reference, byte_size, content_digest, created_at
) VALUES (?, ?, ?, 'media-facts', ?, ?, ?, ?, 'ready', ?, ?, ?, ?)`,
		input.ArtifactID.String(), input.Claim.ProjectID.String(), input.Claim.AssetID.String(),
		input.Claim.ExecutorVersion, fingerprint, parametersDigest, parametersJSON,
		"sqlite:media-facts/"+input.ArtifactID.String(), len(canonicalFacts), contentDigest.String(), at,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE asset_media_state SET facts_json = ?, updated_at = ? WHERE asset_id = ?`,
		string(factsJSON), at, input.Claim.AssetID.String(),
	); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE media_job_details SET result_artifact_id = ?
WHERE job_id = ? AND result_artifact_id IS NULL`, input.ArtifactID.String(), input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrMediaLeaseLost
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'succeeded', progress_basis_points = 10000,
    updated_at = ?, terminal_error_code = NULL
WHERE id = ? AND state = 'running'`, at, input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrMediaLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM work_job_prerequisites
WHERE kind = 'facts-required'
  AND job_id IN (SELECT job_id FROM media_job_details WHERE asset_id = ?)`, input.Claim.AssetID.String()); err != nil {
		return err
	}
	if err := queueSatisfiedMediaJobs(ctx, tx, input.Claim.AssetID, at); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'succeeded', heartbeat_at = ?, ended_at = ?, diagnostics_json = '{}'
WHERE id = ? AND state = 'running'`, at, at, input.Claim.AttemptID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrMediaLeaseLost
	}
	if err := appendMediaJobActivity(
		ctx, tx, input.Claim, input.EventID, input.CompletedAt, "media.probed", "media-probed", nil,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (repository *SQLiteProjects) FailMediaJob(ctx context.Context, input application.FailMediaJobInput) error {
	if input.EventID.IsZero() || input.FailedAt.IsZero() || input.Code == "" || len(input.Code) > 64 ||
		(input.Availability != nil && *input.Availability != domain.AssetChanged &&
			*input.Availability != domain.AssetMissing && *input.Availability != domain.AssetUnreadable) {
		return application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyMediaAttempt(ctx, tx, input.Claim, input.FailedAt.UTC(), input.Claim.Kind); err != nil {
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
	if input.Availability != nil {
		if _, err := tx.ExecContext(ctx, `
UPDATE asset_media_state SET availability = ?, updated_at = ? WHERE asset_id = ?`,
			*input.Availability, at, input.Claim.AssetID.String()); err != nil {
			return err
		}
	}
	if err := appendMediaJobActivity(
		ctx, tx, input.Claim, input.EventID, input.FailedAt, "media.job-failed", "media-job-failed", &input.Code,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func parseMediaJobClaim(
	jobValue, projectValue, assetValue, grantValue, kind string,
	generation uint64,
	input application.ClaimMediaJobInput,
	executorVersion string,
	executorTarget string,
	acceptedValue sql.NullString,
	byteSize uint64,
	modifiedUnixNs int64,
	fileIdentity string,
	parametersDigestValue string,
	parametersJSON string,
) (application.MediaJobClaim, error) {
	jobID, err := domain.ParseMediaJobID(jobValue)
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	projectID, err := domain.ParseProjectID(projectValue)
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	assetID, err := domain.ParseAssetID(assetValue)
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	grantID, err := domain.ParseSourceGrantID(grantValue)
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	size, err := domain.NewUInt64(byteSize)
	if err != nil {
		return application.MediaJobClaim{}, err
	}
	claim := application.MediaJobClaim{
		JobID: jobID, AttemptID: input.AttemptID, ProjectID: projectID, AssetID: assetID,
		SourceGrantID: grantID, Kind: domain.MediaJobKind(kind), Generation: generation,
		ExecutorVersion: executorVersion, ExecutorTarget: executorTarget, LeaseOwner: input.LeaseOwner,
		LeaseExpiresAt: input.Now.UTC().Add(input.LeaseDuration),
		ExpectedObservation: domain.SourceObservation{
			ByteSize: size, ModifiedUnixNs: domain.NewInt64(modifiedUnixNs), FileIdentity: fileIdentity,
		},
		ParametersJSON: []byte(parametersJSON),
	}
	parametersDigest, err := domain.ParseDigest(parametersDigestValue)
	if err != nil || !json.Valid(claim.ParametersJSON) {
		return application.MediaJobClaim{}, application.ErrAssetInvalid
	}
	claim.ParametersDigest = parametersDigest
	if acceptedValue.Valid {
		fingerprint, parseErr := domain.ParseDigest(acceptedValue.String)
		if parseErr != nil {
			return application.MediaJobClaim{}, parseErr
		}
		claim.AcceptedFingerprint = &fingerprint
	}
	if executorVersion == "" ||
		(claim.Kind == domain.MediaJobIdentify && claim.AcceptedFingerprint != nil) ||
		((claim.Kind == domain.MediaJobProbe || claim.Kind == domain.MediaJobFrameSet ||
			claim.Kind == domain.MediaJobProxy || claim.Kind == domain.MediaJobRenderInput ||
			claim.Kind == domain.MediaJobTranscript) &&
			claim.AcceptedFingerprint == nil) {
		return application.MediaJobClaim{}, application.ErrAssetInvalid
	}
	return claim, nil
}

func validMediaExecutors(executors []application.MediaExecutorRegistration) bool {
	if len(executors) == 0 || len(executors) > 8 {
		return false
	}
	seen := make(map[domain.MediaJobKind]struct{}, len(executors))
	for _, executor := range executors {
		if (executor.Kind != domain.MediaJobIdentify && executor.Kind != domain.MediaJobProbe &&
			executor.Kind != domain.MediaJobFrameSet && executor.Kind != domain.MediaJobProxy &&
			executor.Kind != domain.MediaJobRenderInput &&
			executor.Kind != domain.MediaJobTranscript) || executor.Version == "" ||
			len(executor.Version) > 1024 || len(executor.Target) > 128 ||
			(executor.Kind == domain.MediaJobTranscript || executor.Kind == domain.MediaJobRenderInput) !=
				(executor.Target != "") {
			return false
		}
		if _, duplicate := seen[executor.Kind]; duplicate {
			return false
		}
		seen[executor.Kind] = struct{}{}
	}
	return true
}
