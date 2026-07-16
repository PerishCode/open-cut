package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) CompleteMediaRenderInput(
	ctx context.Context,
	input application.CompleteMediaRenderInput,
) error {
	if err := validateRenderInputPublication(input); err != nil {
		return application.ErrAssetInvalid
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	verification, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if err := verifyMediaAttempt(
		ctx, verification, input.Claim, input.CompletedAt.UTC(), domain.MediaJobRenderInput,
	); err != nil {
		verification.Rollback()
		return err
	}
	rematerializing := false
	var existingArtifactID, existingProjectID, existingState, existingDigest string
	var existingParametersJSON, existingReference string
	var existingByteSize uint64
	err = verification.QueryRowContext(ctx, `
SELECT id, project_id, state, byte_size, content_digest, parameters_json, byte_reference
FROM media_artifacts
WHERE asset_id = ? AND kind = 'render-input' AND producer_version = ?
  AND input_fingerprint = ? AND parameters_digest = ?`,
		input.Claim.AssetID.String(), input.Claim.ExecutorVersion,
		input.Manifest.Fingerprint.String(), input.Claim.ParametersDigest.String(),
	).Scan(
		&existingArtifactID, &existingProjectID, &existingState, &existingByteSize,
		&existingDigest, &existingParametersJSON, &existingReference,
	)
	if err == nil {
		parsed, parseErr := domain.ParseArtifactID(existingArtifactID)
		if parseErr != nil || existingProjectID != input.Claim.ProjectID.String() ||
			existingState != string(domain.ArtifactEvicted) || existingByteSize != input.ByteSize.Value() ||
			existingDigest != input.ContentDigest.String() || existingParametersJSON != string(input.Claim.ParametersJSON) ||
			existingReference != "artifact:media/"+existingArtifactID {
			verification.Rollback()
			return application.ErrAssetInvalid
		}
		input.ArtifactID = parsed
		rematerializing = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		verification.Rollback()
		return err
	}
	if err := verification.Commit(); err != nil {
		return err
	}

	finalRoot, err := repository.publishRenderInputFiles(input)
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
	if err := verifyMediaAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC(), domain.MediaJobRenderInput); err != nil {
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
	fingerprint := input.Manifest.Fingerprint.String()
	if accepted != fingerprint || observed != fingerprint ||
		parametersDigest != input.Claim.ParametersDigest.String() || parametersJSON != string(input.Claim.ParametersJSON) {
		return application.ErrMediaLeaseLost
	}
	at := formatInstant(input.CompletedAt.UTC())
	if rematerializing {
		result, err := tx.ExecContext(ctx, `
UPDATE media_artifacts SET state = 'ready'
WHERE id = ? AND project_id = ? AND asset_id = ? AND kind = 'render-input' AND state = 'evicted'
  AND producer_version = ? AND input_fingerprint = ? AND parameters_digest = ?
  AND parameters_json = ? AND byte_reference = ? AND byte_size = ? AND content_digest = ?`,
			input.ArtifactID.String(), input.Claim.ProjectID.String(), input.Claim.AssetID.String(),
			input.Claim.ExecutorVersion, fingerprint, input.Claim.ParametersDigest.String(),
			string(input.Claim.ParametersJSON), "artifact:media/"+input.ArtifactID.String(),
			input.ByteSize.Value(), input.ContentDigest.String(),
		)
		if err != nil {
			return err
		}
		if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
			return application.ErrMediaLeaseLost
		}
	} else if _, err := tx.ExecContext(ctx, `
INSERT INTO media_artifacts (
  id, project_id, asset_id, kind, producer_version, input_fingerprint,
  parameters_digest, parameters_json, state, byte_reference, byte_size, content_digest, created_at
) VALUES (?, ?, ?, 'render-input', ?, ?, ?, ?, 'ready', ?, ?, ?, ?)`,
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
		"media.render-input-ready", "media-render-input-ready", nil,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		committed = true
		return err
	}
	committed = true
	return nil
}

func validateRenderInputPublication(input application.CompleteMediaRenderInput) error {
	if input.EventID.IsZero() || input.ArtifactID.IsZero() || input.CompletedAt.IsZero() ||
		input.Claim.Kind != domain.MediaJobRenderInput || input.Claim.AcceptedFingerprint == nil ||
		input.Parameters.Validate() != nil || input.Parameters.AssetID != input.Claim.AssetID ||
		input.Parameters.Kind != domain.MediaJobRenderInput || input.Parameters.Profile != application.RenderInputProfile ||
		input.Manifest.Validate() != nil || input.Manifest.AssetID != input.Parameters.AssetID ||
		input.Manifest.Fingerprint != *input.Claim.AcceptedFingerprint ||
		input.Manifest.Profile != input.Parameters.Profile || input.Manifest.Producer != input.Claim.ExecutorVersion ||
		input.Workspace == nil || len(input.ManifestCanonical) == 0 ||
		input.ByteSize.Value() > application.MaximumRenderInputArtifactSize {
		return domain.ErrInvalidMediaFacts
	}
	canonical, digest, err := application.CanonicalRenderInputArtifactManifest(input.Manifest)
	if err != nil || !bytes.Equal(canonical, input.ManifestCanonical) || digest != input.ContentDigest {
		return domain.ErrInvalidMediaFacts
	}
	total := uint64(len(input.ManifestCanonical)) + input.Manifest.Media.ByteSize.Value()
	if input.Manifest.Video != nil {
		total += input.Manifest.Video.TimeMap.ByteSize.Value()
	}
	if total != input.ByteSize.Value() {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func (repository *SQLiteProjects) publishRenderInputFiles(
	input application.CompleteMediaRenderInput,
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
	if err := writeDurableArtifactFile(filepath.Join(stageRoot, "manifest.json"), input.ManifestCanonical); err != nil {
		return "", err
	}
	if err := copyPreparedRenderInputFile(stageRoot, input.Workspace, input.Manifest.Media); err != nil {
		return "", err
	}
	if input.Manifest.Video != nil {
		if err := copyPreparedRenderInputFile(stageRoot, input.Workspace, input.Manifest.Video.TimeMap); err != nil {
			return "", err
		}
		file, err := os.Open(filepath.Join(stageRoot, input.Manifest.Video.TimeMap.Path))
		if err != nil {
			return "", err
		}
		validationErr := application.ValidateSourceProxyTimeMapReader(file, input.Manifest.Video.FrameCount.Value())
		closeErr := file.Close()
		if validationErr != nil {
			return "", validationErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	if err := syncDirectory(stageRoot); err != nil {
		return "", err
	}
	finalRoot := filepath.Join(artifactRoot, input.ArtifactID.String())
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		return "", fmt.Errorf("render-input artifact destination already exists")
	}
	if err := os.Rename(stageRoot, finalRoot); err != nil {
		return "", err
	}
	if err := syncDirectory(artifactRoot); err != nil {
		return "", err
	}
	return finalRoot, nil
}

func copyPreparedRenderInputFile(
	stageRoot string,
	workspace application.PreparedMediaWorkspace,
	record application.RenderInputArtifactFile,
) error {
	source, err := workspace.Open(record.Path)
	if err != nil {
		return err
	}
	defer source.Close()
	destination := filepath.Join(stageRoot, filepath.FromSlash(record.Path))
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		file.Close()
		if remove {
			_ = os.Remove(destination)
		}
	}()
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(source, int64(record.ByteSize.Value())+1))
	if err != nil || uint64(written) != record.ByteSize.Value() ||
		"sha256:"+hex.EncodeToString(digest.Sum(nil)) != record.SHA256.String() {
		return fmt.Errorf("prepared render-input file failed content verification")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}
