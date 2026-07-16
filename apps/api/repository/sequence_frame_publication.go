package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) CompleteSequenceFrameSet(
	ctx context.Context,
	input application.CompleteSequenceFrameSet,
) error {
	if err := validateSequenceFramePublication(input); err != nil {
		return err
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	finalRoot, err := repository.publishSequenceFrameFiles(input)
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
	if err := verifySequenceFrameAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC()); err != nil {
		return err
	}
	frame := input.Claim.SequenceFrames
	preview := frame.PreviewArtifact
	at := formatInstant(input.CompletedAt.UTC())
	byteReference := "artifact:sequence-frames/" + input.ArtifactID.String()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_frame_set_artifacts (
  id, producer_job_id, project_id, sequence_id, sequence_revision, preview_job_id,
  preview_artifact_id, preview_artifact_digest, render_plan_digest,
  parameters_digest, producer_version, profile, grid_policy, state,
  byte_reference, byte_size, content_digest, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?, ?, ?)`,
		input.ArtifactID.String(), input.Claim.JobID.String(), frame.ProjectID.String(), frame.SequenceID.String(),
		frame.SequenceRevision.Value(), frame.Parameters.PreviewJobID.String(), preview.ID.String(),
		preview.ContentDigest.String(), preview.RenderPlanDigest.String(), frame.ParametersDigest.String(),
		input.Claim.ExecutorVersion, frame.Parameters.Profile, frame.Parameters.GridPolicy,
		byteReference, input.ByteSize.Value(), input.ContentDigest.String(), at,
	); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE sequence_frame_set_job_details SET result_artifact_id = ?
WHERE job_id = ? AND preview_artifact_id = ? AND preview_artifact_digest = ?
  AND render_plan_digest = ? AND result_artifact_id IS NULL`, input.ArtifactID.String(),
		input.Claim.JobID.String(), preview.ID.String(), preview.ContentDigest.String(),
		preview.RenderPlanDigest.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_jobs SET state = 'succeeded', progress_basis_points = 10000,
  updated_at = ?, terminal_error_code = NULL
WHERE id = ? AND state = 'running'`, at, input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'succeeded', heartbeat_at = ?, ended_at = ?, diagnostics_json = '{}'
WHERE id = ? AND job_id = ? AND state = 'running'`, at, at,
		input.Claim.AttemptID.String(), input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	if err := appendSequenceFrameActivity(
		ctx, tx, input.Claim, input.EventID, input.CompletedAt,
		"sequence.frame-set-ready", "sequence-frame-set-ready", nil,
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

func (repository *SQLiteProjects) FailSequenceFrameSet(
	ctx context.Context,
	input application.FailSequenceFrameSet,
) error {
	if input.Claim.Kind != domain.WorkJobSequenceFrames || input.Claim.SequenceFrames == nil ||
		input.EventID.IsZero() || input.FailedAt.IsZero() || !sequencePreviewFailureCodePattern.MatchString(input.Code) {
		return application.ErrSequenceFramesInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifySequenceFrameAttempt(ctx, tx, input.Claim, input.FailedAt.UTC()); err != nil {
		return err
	}
	at := formatInstant(input.FailedAt.UTC())
	diagnostics, _ := json.Marshal(struct {
		Code string `json:"code"`
	}{Code: input.Code})
	result, err := tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'failed', heartbeat_at = ?, ended_at = ?, diagnostics_json = ?
WHERE id = ? AND job_id = ? AND state = 'running'`, at, at, string(diagnostics),
		input.Claim.AttemptID.String(), input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_jobs SET state = 'failed', updated_at = ?, terminal_error_code = ?
WHERE id = ? AND state = 'running'`, at, input.Code, input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	if err := appendSequenceFrameActivity(
		ctx, tx, input.Claim, input.EventID, input.FailedAt,
		"sequence.frame-set-failed", "sequence-frame-set-failed", &input.Code,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func verifySequenceFrameAttempt(
	ctx context.Context,
	tx *sql.Tx,
	claim application.WorkJobClaim,
	now time.Time,
) error {
	if claim.JobID.IsZero() || claim.AttemptID.IsZero() || claim.Kind != domain.WorkJobSequenceFrames ||
		claim.SequenceFrames == nil || claim.Media != nil || claim.SequencePreview != nil ||
		claim.LeaseOwner == "" || now.IsZero() {
		return application.ErrWorkLeaseLost
	}
	var attemptState, owner, expires, attemptExecutor, jobState, jobKind string
	var projectValue, digestValue, parametersJSON, sequenceValue string
	var previewArtifactValue, previewDigestValue, planDigestValue string
	var generation, sequenceRevision uint64
	err := tx.QueryRowContext(ctx, `
SELECT attempt.state, attempt.lease_owner, attempt.lease_expires_at,
       attempt.generation, attempt.executor_version,
       job.state, job.kind, job.project_id, job.parameters_digest, job.parameters_json,
       detail.sequence_id, detail.sequence_revision, detail.preview_artifact_id,
       detail.preview_artifact_digest, detail.render_plan_digest
FROM work_job_attempts attempt
JOIN work_jobs job ON job.id = attempt.job_id
JOIN sequence_frame_set_job_details detail ON detail.job_id = job.id
WHERE attempt.id = ? AND attempt.job_id = ?`, claim.AttemptID.String(), claim.JobID.String()).Scan(
		&attemptState, &owner, &expires, &generation, &attemptExecutor,
		&jobState, &jobKind, &projectValue, &digestValue, &parametersJSON,
		&sequenceValue, &sequenceRevision, &previewArtifactValue, &previewDigestValue, &planDigestValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrWorkLeaseLost
	}
	if err != nil {
		return err
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return application.ErrWorkLeaseLost
	}
	frame := claim.SequenceFrames
	canonical, digest, canonicalErr := application.CanonicalSequenceFrameSetParameters(frame.Parameters)
	preview := frame.PreviewArtifact
	if canonicalErr != nil || frame.ParametersDigest != digest || !bytes.Equal(frame.ParametersJSON, canonical) ||
		attemptState != "running" || owner != claim.LeaseOwner || generation != claim.Generation ||
		attemptExecutor != claim.ExecutorVersion || frame.Parameters.ExecutorVersion != claim.ExecutorVersion ||
		jobState != "running" || jobKind != string(domain.WorkJobSequenceFrames) ||
		projectValue != frame.ProjectID.String() || digestValue != digest.String() ||
		parametersJSON != string(canonical) || sequenceValue != frame.SequenceID.String() ||
		sequenceRevision != frame.SequenceRevision.Value() || previewArtifactValue != preview.ID.String() ||
		previewDigestValue != preview.ContentDigest.String() || planDigestValue != preview.RenderPlanDigest.String() ||
		!expiry.After(now.UTC()) {
		return application.ErrWorkLeaseLost
	}
	return nil
}

func validateSequenceFramePublication(input application.CompleteSequenceFrameSet) error {
	if input.Claim.Kind != domain.WorkJobSequenceFrames || input.Claim.SequenceFrames == nil ||
		input.ArtifactID.IsZero() || input.EventID.IsZero() || input.CompletedAt.IsZero() ||
		input.Manifest.Validate() != nil || len(input.PNGs) != len(input.Manifest.Samples) ||
		len(input.PNGs) != len(input.Claim.SequenceFrames.Parameters.Samples) ||
		len(input.ManifestCanonical) == 0 || input.ByteSize.Value() > application.MaximumSequenceFrameArtifactSize {
		return application.ErrSequenceFramesInvalid
	}
	frame := input.Claim.SequenceFrames
	preview := frame.PreviewArtifact
	expectedWidth, expectedHeight, dimensionErr := application.SequenceFrameOutputDimensions(
		preview.Facts.CanvasWidth, preview.Facts.CanvasHeight,
	)
	if dimensionErr != nil {
		return application.ErrSequenceFramesInvalid
	}
	if input.Manifest.ProjectID != frame.ProjectID || input.Manifest.SequenceID != frame.SequenceID ||
		input.Manifest.SequenceRevision != frame.SequenceRevision || input.Manifest.PreviewJobID != frame.Parameters.PreviewJobID ||
		input.Manifest.PreviewArtifactID != preview.ID || input.Manifest.PreviewArtifactDigest != preview.ContentDigest ||
		input.Manifest.RenderPlanDigest != preview.RenderPlanDigest || input.Manifest.Profile != frame.Parameters.Profile ||
		input.Manifest.GridPolicy != frame.Parameters.GridPolicy || input.Manifest.Producer != input.Claim.ExecutorVersion {
		return application.ErrSequenceFramesInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-frame-set-artifact", application.SequenceFrameArtifactSchema, input.Manifest,
	)
	if err != nil || !bytes.Equal(canonical, input.ManifestCanonical) || digest != input.ContentDigest {
		return application.ErrSequenceFramesInvalid
	}
	total := uint64(len(canonical))
	for index, sample := range input.Manifest.Samples {
		pngBytes := input.PNGs[index]
		hash := sha256.Sum256(pngBytes)
		if sample.SequenceFrameCoordinate != frame.Parameters.Samples[index] ||
			sample.Width != expectedWidth || sample.Height != expectedHeight ||
			sample.Path != fmt.Sprintf("frames/%03d.png", index) ||
			sample.ByteSize.Value() != uint64(len(pngBytes)) ||
			sample.SHA256.String() != "sha256:"+hex.EncodeToString(hash[:]) {
			return application.ErrSequenceFramesInvalid
		}
		reader := bytes.NewReader(pngBytes)
		decoded, err := png.Decode(reader)
		if err != nil || reader.Len() != 0 || decoded.Bounds().Dx() != int(sample.Width) ||
			decoded.Bounds().Dy() != int(sample.Height) || !opaqueSequenceFrame(decoded) {
			return application.ErrSequenceFramesInvalid
		}
		total += uint64(len(pngBytes))
	}
	if total != input.ByteSize.Value() || total > application.MaximumSequenceFrameArtifactSize {
		return application.ErrSequenceFramesInvalid
	}
	return nil
}

func opaqueSequenceFrame(frame image.Image) bool {
	bounds := frame.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := frame.At(x, y).RGBA()
			if alpha != 0xffff {
				return false
			}
		}
	}
	return true
}

func (repository *SQLiteProjects) publishSequenceFrameFiles(
	input application.CompleteSequenceFrameSet,
) (string, error) {
	workRoot := filepath.Join(repository.dataDir, "work", "sequence-frame-publication")
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-frames")
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
		if err := writeDurableArtifactFile(filepath.Join(framesRoot, fmt.Sprintf("%03d.png", index)), pngBytes); err != nil {
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
		return "", fmt.Errorf("sequence frame artifact destination already exists")
	}
	if err := os.Rename(stageRoot, finalRoot); err != nil {
		return "", err
	}
	if err := syncDirectory(artifactRoot); err != nil {
		return "", err
	}
	return finalRoot, nil
}

func appendSequenceFrameActivity(
	ctx context.Context,
	tx *sql.Tx,
	claim application.WorkJobClaim,
	eventID domain.ActivityEventID,
	at time.Time,
	kind, summary string,
	failureCode *string,
) error {
	frame := claim.SequenceFrames
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, frame.ProjectID.String()).Scan(&projectRevision); err != nil {
		return err
	}
	revision, err := domain.NewRevision(projectRevision)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		JobID             domain.WorkJobID               `json:"jobId"`
		AttemptID         domain.JobAttemptID            `json:"attemptId"`
		SequenceID        domain.SequenceID              `json:"sequenceId"`
		SequenceRevision  domain.Revision                `json:"sequenceRevision"`
		PreviewJobID      domain.WorkJobID               `json:"previewJobId"`
		FailureCode       *string                        `json:"failureCode,omitempty"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{}, JobID: claim.JobID,
		AttemptID: claim.AttemptID, SequenceID: frame.SequenceID,
		SequenceRevision: frame.SequenceRevision, PreviewJobID: frame.Parameters.PreviewJobID,
		FailureCode: failureCode,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: frame.ProjectID.String(), EventID: eventID.String(), Kind: kind,
		OccurredAt: formatInstant(at.UTC()), ProjectID: frame.ProjectID.String(),
		ProjectRevision: int64(revision.Value()), OutcomeKind: "sequence-frame-job",
		OutcomeID: claim.JobID.String(), SummaryCode: summary, Payload: payload,
	})
	return err
}
