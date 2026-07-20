package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) CompleteMediaFrameSet(
	ctx context.Context,
	input application.CompleteMediaFrameSet,
) (resultErr error) {
	if err := validateFrameSetPublication(input); err != nil {
		return application.ErrAssetInvalid
	}
	verification, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if err := verifyMediaAttempt(
		ctx, verification, input.Claim, input.CompletedAt.UTC(), domain.MediaJobFrameSet,
	); err != nil {
		verification.Rollback()
		return err
	}
	if err := verification.Commit(); err != nil {
		return err
	}

	finalRoot, err := repository.publishFrameSetFiles(input)
	if err != nil {
		return err
	}
	defer func() { discardUnpublishedTree(finalRoot, &resultErr) }()

	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyMediaAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC(), domain.MediaJobFrameSet); err != nil {
		return err
	}
	var accepted, observed, parametersDigest, parametersJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT asset.accepted_fingerprint, state.observed_fingerprint,
       job.parameters_digest, job.parameters_json
FROM assets asset
JOIN asset_media_state state ON state.asset_id = asset.id
JOIN media_job_details detail ON detail.asset_id = asset.id
JOIN work_jobs job ON job.id = detail.job_id AND job.id = ?
WHERE asset.id = ? AND asset.project_id = ?`,
		input.Claim.JobID.String(), input.Claim.AssetID.String(), input.Claim.ProjectID.String(),
	).Scan(&accepted, &observed, &parametersDigest, &parametersJSON); err != nil {
		return err
	}
	fingerprint := input.Parameters.Fingerprint.String()
	if accepted != fingerprint || observed != fingerprint ||
		parametersDigest != input.Claim.ParametersDigest.String() || parametersJSON != string(input.Claim.ParametersJSON) {
		return application.ErrMediaLeaseLost
	}
	at := formatInstant(input.CompletedAt.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO media_artifacts (
  id, project_id, asset_id, kind, producer_version, input_fingerprint,
  parameters_digest, parameters_json, state, byte_reference, byte_size, content_digest, created_at
) VALUES (?, ?, ?, 'frame-sample-set', ?, ?, ?, ?, 'ready', ?, ?, ?, ?)`,
		input.ArtifactID.String(), input.Claim.ProjectID.String(), input.Claim.AssetID.String(),
		input.Claim.ExecutorVersion, fingerprint, input.Claim.ParametersDigest.String(),
		string(input.Claim.ParametersJSON), "artifact:media/"+input.ArtifactID.String(),
		input.ByteSize.Value(), input.ContentDigest.String(), at,
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
		ctx, tx, input.Claim, input.EventID, input.CompletedAt,
		"media.frame-set-ready", "media-frame-set-ready", nil,
	); err != nil {
		return err
	}
	return commitPublication(tx)
}

func validateFrameSetPublication(input application.CompleteMediaFrameSet) error {
	if input.EventID.IsZero() || input.ArtifactID.IsZero() || input.CompletedAt.IsZero() ||
		input.Claim.Kind != domain.MediaJobFrameSet || input.Claim.AcceptedFingerprint == nil ||
		input.Parameters.Validate() != nil || input.Parameters.AssetID != input.Claim.AssetID ||
		input.Parameters.Fingerprint != *input.Claim.AcceptedFingerprint ||
		input.Manifest.AssetID != input.Parameters.AssetID ||
		input.Manifest.Fingerprint != input.Parameters.Fingerprint ||
		input.Manifest.SourceStreamID != input.Parameters.SourceStreamID ||
		input.Manifest.Profile != input.Parameters.Profile ||
		input.Manifest.Producer != input.Claim.ExecutorVersion ||
		len(input.Manifest.Samples) != len(input.PNGs) || len(input.PNGs) != len(input.Parameters.Times) ||
		len(input.ManifestCanonical) == 0 || input.ByteSize.Value() > application.MaximumFrameSetArtifactSize {
		return domain.ErrInvalidMediaFacts
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/media-frame-set-artifact", application.FrameSetArtifactSchema, input.Manifest,
	)
	if err != nil || string(canonical) != string(input.ManifestCanonical) || digest != input.ContentDigest {
		return domain.ErrInvalidMediaFacts
	}
	total := uint64(len(input.ManifestCanonical))
	for index, sample := range input.Manifest.Samples {
		pngBytes := input.PNGs[index]
		digest := sha256.Sum256(pngBytes)
		expectedPath := fmt.Sprintf("frames/%03d.png", index)
		if sample.Path != expectedPath || sample.RequestedTime != input.Parameters.Times[index] ||
			sample.ByteSize.Value() != uint64(len(pngBytes)) ||
			sample.SHA256.String() != "sha256:"+hex.EncodeToString(digest[:]) {
			return domain.ErrInvalidMediaFacts
		}
		total += uint64(len(pngBytes))
	}
	if total != input.ByteSize.Value() {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func (repository *SQLiteProjects) publishFrameSetFiles(
	input application.CompleteMediaFrameSet,
) (string, error) {
	workRoot := filepath.Join(repository.dataDir, "work", "media-publication")
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "media")
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return "", err
	}
	stageRoot := filepath.Join(workRoot, input.Claim.AttemptID.String()+"-"+input.ArtifactID.String())
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		return "", err
	}
	defer os.RemoveAll(stageRoot)
	framesRoot := filepath.Join(stageRoot, "frames")
	if err := os.Mkdir(framesRoot, 0o700); err != nil {
		return "", err
	}
	if err := writeDurableArtifactFile(filepath.Join(stageRoot, "manifest.json"), input.ManifestCanonical); err != nil {
		return "", err
	}
	for index, pngBytes := range input.PNGs {
		if err := writeDurableArtifactFile(
			filepath.Join(framesRoot, fmt.Sprintf("%03d.png", index)), pngBytes,
		); err != nil {
			return "", err
		}
	}
	if err := syncDirectory(framesRoot); err != nil {
		return "", err
	}
	if err := syncDirectory(stageRoot); err != nil {
		return "", err
	}
	finalRoot := filepath.Join(artifactRoot, input.ArtifactID.String())
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		return "", fmt.Errorf("frame artifact destination already exists")
	}
	if err := os.Rename(stageRoot, finalRoot); err != nil {
		return "", err
	}
	if err := syncDirectory(artifactRoot); err != nil {
		return "", err
	}
	return finalRoot, nil
}

func writeDurableArtifactFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
