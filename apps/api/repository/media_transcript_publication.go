package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) CompleteMediaTranscript(
	ctx context.Context,
	input application.CompleteMediaTranscript,
) error {
	producerVersion, err := validateTranscriptPublication(input)
	if err != nil {
		return application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyMediaAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC(), domain.MediaJobTranscript); err != nil {
		return err
	}
	if err := verifyTranscriptPublicationInput(ctx, tx, input.Claim); err != nil {
		return err
	}
	at := formatInstant(input.CompletedAt.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO media_artifacts (
  id, project_id, asset_id, kind, producer_version, input_fingerprint,
  parameters_digest, parameters_json, state, byte_reference, byte_size, content_digest, created_at
) VALUES (?, ?, ?, 'transcript', ?, ?, ?, ?, 'ready', ?, ?, ?, ?)`,
		input.Artifact.ID.String(), input.Claim.ProjectID.String(), input.Claim.AssetID.String(),
		producerVersion, input.Claim.AcceptedFingerprint.String(), input.Claim.ParametersDigest.String(),
		string(input.Claim.ParametersJSON), "sqlite:transcript/"+input.Artifact.ID.String(),
		input.ByteSize.Value(), input.ContentDigest.String(), at,
	); err != nil {
		return err
	}
	tokenCount := 0
	for _, segment := range input.Artifact.Segments {
		tokenCount += len(segment.Tokens)
	}
	proof := input.Artifact.Normalization
	if _, err := tx.ExecContext(ctx, `
INSERT INTO transcript_artifacts (
  artifact_id, schema_version, binding_digest, source_stream_id, model_resource_id,
  detected_language, language_confidence_basis_points,
  source_start_value, source_start_scale, sample_rate, channels, sample_format,
  sample_count, pcm_byte_size, pcm_digest, channel_policy, timing_policy,
  segment_count, token_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.Artifact.ID.String(), input.Artifact.Schema, input.Artifact.BindingDigest.String(),
		input.Artifact.Binding.SourceStreamID.String(), input.Artifact.Binding.ModelResourceID.String(),
		input.Artifact.DetectedLanguage, nullableBasisPoints(input.Artifact.LanguageConfidenceBasisPoints),
		proof.SourceStartTime.Value.Value(), proof.SourceStartTime.Scale,
		proof.SampleRate, proof.Channels, proof.SampleFormat, proof.SampleCount.Value(),
		proof.PCMByteSize.Value(), proof.PCMDigest.String(), proof.ChannelPolicy, proof.TimingPolicy,
		len(input.Artifact.Segments), tokenCount,
	); err != nil {
		return err
	}
	for _, segment := range input.Artifact.Segments {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO transcript_segments (
  id, artifact_id, ordinal, source_start_value, source_start_scale,
  source_duration_value, source_duration_scale, text
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			segment.ID.String(), input.Artifact.ID.String(), segment.Ordinal,
			segment.SourceRange.Start.Value.Value(), segment.SourceRange.Start.Scale,
			segment.SourceRange.Duration.Value.Value(), segment.SourceRange.Duration.Scale, segment.Text,
		); err != nil {
			return err
		}
		for _, token := range segment.Tokens {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO transcript_tokens (
  id, segment_id, ordinal, source_start_value, source_start_scale,
  source_duration_value, source_duration_scale, text, confidence_basis_points
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				token.ID.String(), segment.ID.String(), token.Ordinal,
				token.SourceRange.Start.Value.Value(), token.SourceRange.Start.Scale,
				token.SourceRange.Duration.Value.Value(), token.SourceRange.Duration.Scale,
				token.Text, nullableBasisPoints(token.ConfidenceBasisPoints),
			); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO asset_transcript_selection (asset_id, artifact_id, selected_at)
VALUES (?, ?, ?)`, input.Claim.AssetID.String(), input.Artifact.ID.String(), at); err != nil {
		return err
	}
	if err := completeTranscriptJob(
		ctx, tx, input.Claim, input.Artifact.ID.String(), nil, input.EventID, input.CompletedAt,
		"media.transcript-ready", "media-transcript-ready",
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (repository *SQLiteProjects) CompleteMediaTranscriptNoAudio(
	ctx context.Context,
	input application.CompleteMediaTranscriptNoAudio,
) error {
	if input.EventID.IsZero() || input.CompletedAt.IsZero() ||
		input.Claim.Kind != domain.MediaJobTranscript || input.Claim.AcceptedFingerprint == nil ||
		!input.Claim.TranscriptNoAudio || input.Claim.TranscriptBinding != nil || input.Claim.SourceStream != nil {
		return application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyMediaAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC(), domain.MediaJobTranscript); err != nil {
		return err
	}
	if err := verifyTranscriptNoAudioInput(ctx, tx, input.Claim); err != nil {
		return err
	}
	resultCode := "no-audio"
	if err := completeTranscriptJob(
		ctx, tx, input.Claim, "", &resultCode, input.EventID, input.CompletedAt,
		"media.transcript-unavailable", "media-transcript-no-audio",
	); err != nil {
		return err
	}
	return tx.Commit()
}

func validateTranscriptPublication(input application.CompleteMediaTranscript) (string, error) {
	if input.EventID.IsZero() || input.CompletedAt.IsZero() || input.Claim.Kind != domain.MediaJobTranscript ||
		input.Claim.AcceptedFingerprint == nil || input.Claim.TranscriptBinding == nil ||
		input.Claim.TranscriptNoAudio || input.Claim.SourceStream == nil ||
		input.Artifact.Validate() != nil || input.Artifact.ID.IsZero() ||
		input.Artifact.ProjectID != input.Claim.ProjectID ||
		input.Artifact.Binding != *input.Claim.TranscriptBinding ||
		input.Artifact.Binding.AssetID != input.Claim.AssetID ||
		input.Artifact.Binding.Fingerprint != *input.Claim.AcceptedFingerprint ||
		input.Artifact.Binding.EngineVersion != input.Claim.ExecutorVersion ||
		input.Artifact.Binding.EngineTarget != input.Claim.ExecutorTarget ||
		len(input.ArtifactCanonical) == 0 || uint64(len(input.ArtifactCanonical)) != input.ByteSize.Value() {
		return "", domain.ErrInvalidTranscript
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/transcript-artifact", domain.TranscriptArtifactSchema, input.Artifact,
	)
	if err != nil || string(canonical) != string(input.ArtifactCanonical) || digest != input.ContentDigest {
		return "", domain.ErrInvalidTranscript
	}
	return application.TranscriptProducerVersion(input.Artifact.Binding)
}

func verifyTranscriptPublicationInput(
	ctx context.Context,
	tx *sql.Tx,
	claim application.MediaJobClaim,
) error {
	var accepted, observed, parametersDigest, parametersJSON, bindingDigest string
	if err := tx.QueryRowContext(ctx, `
SELECT asset.accepted_fingerprint, state.observed_fingerprint,
       job.parameters_digest, job.parameters_json, binding.binding_digest
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
JOIN assets asset ON asset.id = detail.asset_id
JOIN asset_media_state state ON state.asset_id = asset.id
JOIN transcript_job_bindings binding ON binding.job_id = job.id
WHERE job.id = ? AND asset.id = ? AND asset.project_id = ?`,
		claim.JobID.String(), claim.AssetID.String(), claim.ProjectID.String(),
	).Scan(&accepted, &observed, &parametersDigest, &parametersJSON, &bindingDigest); err != nil {
		return err
	}
	_, expectedBindingDigest, err := application.CanonicalTranscriptBinding(*claim.TranscriptBinding)
	if err != nil || accepted != claim.AcceptedFingerprint.String() || observed != accepted ||
		parametersDigest != claim.ParametersDigest.String() || parametersJSON != string(claim.ParametersJSON) ||
		bindingDigest != expectedBindingDigest.String() {
		return application.ErrMediaLeaseLost
	}
	loaded, err := loadTranscriptBinding(ctx, tx, claim.JobID, claim.AssetID, *claim.AcceptedFingerprint)
	if err != nil || loaded != *claim.TranscriptBinding {
		if err != nil {
			return err
		}
		return application.ErrMediaLeaseLost
	}
	return nil
}

func verifyTranscriptNoAudioInput(
	ctx context.Context,
	tx *sql.Tx,
	claim application.MediaJobClaim,
) error {
	var accepted, observed, parametersDigest, parametersJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT asset.accepted_fingerprint, state.observed_fingerprint,
       job.parameters_digest, job.parameters_json
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
JOIN assets asset ON asset.id = detail.asset_id
JOIN asset_media_state state ON state.asset_id = asset.id
WHERE job.id = ? AND asset.id = ? AND asset.project_id = ?
  AND NOT EXISTS (SELECT 1 FROM transcript_job_bindings binding WHERE binding.job_id = job.id)
  AND NOT EXISTS (
    SELECT 1 FROM source_streams stream
    WHERE stream.asset_id = asset.id AND stream.fingerprint = asset.accepted_fingerprint
      AND stream.media_type = 'audio'
  )`, claim.JobID.String(), claim.AssetID.String(), claim.ProjectID.String()).Scan(
		&accepted, &observed, &parametersDigest, &parametersJSON,
	); err != nil {
		return err
	}
	if accepted != claim.AcceptedFingerprint.String() || observed != accepted ||
		parametersDigest != claim.ParametersDigest.String() || parametersJSON != string(claim.ParametersJSON) {
		return application.ErrMediaLeaseLost
	}
	return nil
}

func completeTranscriptJob(
	ctx context.Context,
	tx *sql.Tx,
	claim application.MediaJobClaim,
	artifactID string,
	resultCode *string,
	eventID domain.ActivityEventID,
	at time.Time,
	activityKind, summaryCode string,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE media_job_details
SET result_artifact_id = NULLIF(?, ''), result_code = ?
WHERE job_id = ? AND result_artifact_id IS NULL AND result_code IS NULL`,
		artifactID, resultCode, claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrMediaLeaseLost
	}
	completedAt := formatInstant(at.UTC())
	result, err = tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = 'succeeded', progress_basis_points = 10000, updated_at = ?, terminal_error_code = NULL
WHERE id = ? AND state = 'running'`, completedAt, claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrMediaLeaseLost
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'succeeded', heartbeat_at = ?, ended_at = ?, diagnostics_json = '{}'
WHERE id = ? AND state = 'running'`, completedAt, completedAt, claim.AttemptID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrMediaLeaseLost
	}
	return appendMediaJobActivity(ctx, tx, claim, eventID, at, activityKind, summaryCode, nil)
}

func nullableBasisPoints(value *uint16) any {
	if value == nil {
		return nil
	}
	return int64(*value)
}
