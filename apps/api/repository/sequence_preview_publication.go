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
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

var sequencePreviewFailureCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (repository *SQLiteProjects) CompleteSequencePreview(
	ctx context.Context,
	input application.CompleteSequencePreview,
) (resultErr error) {
	if err := validateSequencePreviewPublication(input); err != nil {
		return err
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	rematerializing, err := repository.resolveSequencePreviewRematerialization(ctx, &input)
	if err != nil {
		return err
	}
	finalRoot, err := repository.publishSequencePreviewFiles(input)
	if err != nil {
		return err
	}
	defer func() { discardUnpublishedTree(finalRoot, &resultErr) }()
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifySequencePreviewAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC()); err != nil {
		return err
	}
	var boundPlan string
	if err := tx.QueryRowContext(ctx, `
SELECT render_plan_digest FROM sequence_preview_job_details WHERE job_id = ?`,
		input.Claim.JobID.String()).Scan(&boundPlan); err != nil || boundPlan != input.Plan.Plan.Digest.String() {
		return application.ErrWorkLeaseLost
	}
	factsJSON, err := json.Marshal(input.Manifest.Facts)
	if err != nil {
		return err
	}
	at := formatInstant(input.CompletedAt.UTC())
	byteReference := "artifact:sequence-preview/" + input.ArtifactID.String()
	if rematerializing {
		result, err := tx.ExecContext(ctx, `
UPDATE sequence_preview_artifacts SET state = 'ready'
WHERE id = ? AND project_id = ? AND sequence_id = ? AND sequence_revision = ?
  AND render_plan_digest = ? AND renderer_version = ? AND renderer_target = ?
  AND output_profile = ? AND state = 'evicted' AND facts_json = ?
  AND byte_reference = ? AND byte_size = ? AND content_digest = ?`,
			input.ArtifactID.String(), input.Manifest.ProjectID.String(), input.Manifest.SequenceID.String(),
			input.Manifest.SequenceRevision.Value(), input.Manifest.RenderPlanDigest.String(),
			input.Manifest.RendererVersion, input.Manifest.RendererTarget, input.Manifest.Profile,
			string(factsJSON), byteReference, input.ByteSize.Value(), input.ContentDigest.String(),
		)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return application.ErrWorkLeaseLost
		}
	} else if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_preview_artifacts (
  id, project_id, sequence_id, sequence_revision, render_plan_digest,
  renderer_version, renderer_target, output_profile, state, facts_json,
  byte_reference, byte_size, content_digest, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?, ?, ?, ?)`,
		input.ArtifactID.String(), input.Manifest.ProjectID.String(), input.Manifest.SequenceID.String(),
		input.Manifest.SequenceRevision.Value(), input.Manifest.RenderPlanDigest.String(),
		input.Manifest.RendererVersion, input.Manifest.RendererTarget, input.Manifest.Profile,
		string(factsJSON), byteReference, input.ByteSize.Value(), input.ContentDigest.String(), at,
	); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE sequence_preview_job_details SET result_artifact_id = ?
WHERE job_id = ? AND render_plan_digest = ? AND result_artifact_id IS NULL`,
		input.ArtifactID.String(), input.Claim.JobID.String(), input.Plan.Plan.Digest.String(),
	)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrWorkLeaseLost
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
		return application.ErrWorkLeaseLost
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'succeeded', heartbeat_at = ?, ended_at = ?, diagnostics_json = '{}'
WHERE id = ? AND job_id = ? AND state = 'running'`,
		at, at, input.Claim.AttemptID.String(), input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM render_material_leases WHERE attempt_id = ?`,
		input.Claim.AttemptID.String()); err != nil {
		return err
	}
	if err := appendSequencePreviewActivity(
		ctx, tx, input.Claim, input.EventID, input.CompletedAt,
		"sequence.preview-ready", "sequence-preview-ready", nil,
	); err != nil {
		return err
	}
	return commitPublication(tx)
}

func (repository *SQLiteProjects) FailSequencePreview(
	ctx context.Context,
	input application.FailSequencePreview,
) error {
	if input.Claim.Kind != domain.WorkJobSequencePreview || input.Claim.SequencePreview == nil ||
		input.EventID.IsZero() || input.FailedAt.IsZero() ||
		!sequencePreviewFailureCodePattern.MatchString(input.Code) {
		return application.ErrSequencePreviewInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifySequencePreviewAttempt(ctx, tx, input.Claim, input.FailedAt.UTC()); err != nil {
		return err
	}
	at := formatInstant(input.FailedAt.UTC())
	diagnostics, _ := json.Marshal(struct {
		Code   string `json:"code"`
		Detail string `json:"detail,omitempty"`
	}{Code: input.Code, Detail: input.Detail})
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM render_material_leases WHERE attempt_id = ?`,
		input.Claim.AttemptID.String()); err != nil {
		return err
	}
	if err := appendSequencePreviewActivity(
		ctx, tx, input.Claim, input.EventID, input.FailedAt,
		"sequence.preview-failed", "sequence-preview-failed", &input.Code,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func validateSequencePreviewPublication(input application.CompleteSequencePreview) error {
	if input.Claim.Kind != domain.WorkJobSequencePreview || input.Claim.SequencePreview == nil ||
		input.ArtifactID.IsZero() || input.EventID.IsZero() || input.CompletedAt.IsZero() ||
		input.Workspace == nil || input.Manifest.Validate() != nil ||
		input.Manifest.ProjectID != input.Claim.SequencePreview.ProjectID ||
		input.Manifest.SequenceID != input.Claim.SequencePreview.SequenceID ||
		input.Manifest.SequenceRevision != input.Claim.SequencePreview.SequenceRevision ||
		input.Manifest.RenderPlanDigest != input.Plan.Plan.Digest ||
		input.Manifest.RendererVersion != input.Claim.ExecutorVersion ||
		input.Manifest.RendererTarget != input.Claim.SequencePreview.Parameters.RendererTarget ||
		input.Manifest.Profile != input.Claim.SequencePreview.Parameters.OutputProfile {
		return application.ErrSequencePreviewInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-preview-artifact", domain.SequencePreviewArtifactSchema, input.Manifest,
	)
	if err != nil || !bytes.Equal(canonical, input.ManifestCanonical) || digest != input.ContentDigest ||
		len(canonical) > application.MaximumSequencePreviewManifestSize ||
		uint64(len(canonical))+input.Manifest.Media.ByteSize.Value() != input.ByteSize.Value() ||
		input.ByteSize.Value() > application.MaximumSequencePreviewArtifactSize {
		return application.ErrSequencePreviewInvalid
	}
	expected, err := application.SequencePreviewFactsForPlan(input.Plan.Plan.Payload)
	if err != nil || input.Manifest.Facts != expected {
		return application.ErrSequencePreviewInvalid
	}
	return nil
}

func (repository *SQLiteProjects) resolveSequencePreviewRematerialization(
	ctx context.Context,
	input *application.CompleteSequencePreview,
) (bool, error) {
	var idValue, state, factsJSON, byteReference, contentDigest string
	var byteSize uint64
	err := repository.db.QueryRowContext(ctx, `
SELECT id, state, facts_json, byte_reference, byte_size, content_digest
FROM sequence_preview_artifacts
WHERE render_plan_digest = ? AND renderer_version = ? AND renderer_target = ? AND output_profile = ?`,
		input.Manifest.RenderPlanDigest.String(), input.Manifest.RendererVersion,
		input.Manifest.RendererTarget, input.Manifest.Profile,
	).Scan(&idValue, &state, &factsJSON, &byteReference, &byteSize, &contentDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	id, err := domain.ParseArtifactID(idValue)
	if err != nil {
		return false, application.ErrSequencePreviewInvalid
	}
	wantFacts, _ := json.Marshal(input.Manifest.Facts)
	if state != string(domain.SequencePreviewArtifactEvicted) || factsJSON != string(wantFacts) ||
		byteReference != "artifact:sequence-preview/"+idValue || byteSize != input.ByteSize.Value() ||
		contentDigest != input.ContentDigest.String() {
		return false, application.ErrSequencePreviewInvalid
	}
	input.ArtifactID = id
	return true, nil
}

func (repository *SQLiteProjects) publishSequencePreviewFiles(
	input application.CompleteSequencePreview,
) (string, error) {
	workRoot := filepath.Join(repository.dataDir, "work", "sequence-preview-publication")
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-preview")
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
	if err := copySequencePreviewFile(stageRoot, input.Workspace, input.Manifest.Media); err != nil {
		return "", err
	}
	if err := syncDirectory(stageRoot); err != nil {
		return "", err
	}
	finalRoot := filepath.Join(artifactRoot, input.ArtifactID.String())
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		return "", fmt.Errorf("sequence preview artifact destination already exists")
	}
	if err := os.Rename(stageRoot, finalRoot); err != nil {
		return "", err
	}
	if err := syncDirectory(artifactRoot); err != nil {
		return "", err
	}
	return finalRoot, nil
}

func copySequencePreviewFile(
	stageRoot string,
	workspace application.PreparedMediaWorkspace,
	record application.SequencePreviewArtifactFile,
) error {
	source, err := workspace.Open(record.Path)
	if err != nil {
		return err
	}
	defer source.Close()
	path := filepath.Join(stageRoot, filepath.FromSlash(record.Path))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(source, int64(record.ByteSize.Value())+1))
	if copyErr == nil && uint64(written) != record.ByteSize.Value() {
		copyErr = application.ErrSequencePreviewInvalid
	}
	if copyErr == nil && "sha256:"+hex.EncodeToString(digest.Sum(nil)) != record.SHA256.String() {
		copyErr = application.ErrSequencePreviewInvalid
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func appendSequencePreviewActivity(
	ctx context.Context,
	tx *sql.Tx,
	claim application.WorkJobClaim,
	eventID domain.ActivityEventID,
	at time.Time,
	kind, summary string,
	failureCode *string,
) error {
	preview := claim.SequencePreview
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`,
		preview.ProjectID.String()).Scan(&projectRevision); err != nil {
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
		FailureCode       *string                        `json:"failureCode,omitempty"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{}, JobID: claim.JobID,
		AttemptID: claim.AttemptID, SequenceID: preview.SequenceID,
		SequenceRevision: preview.SequenceRevision, FailureCode: failureCode,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: preview.ProjectID.String(), EventID: eventID.String(), Kind: kind,
		OccurredAt: formatInstant(at.UTC()), ProjectID: preview.ProjectID.String(),
		ProjectRevision: int64(revision.Value()), OutcomeKind: "sequence-preview-job",
		OutcomeID: claim.JobID.String(), SummaryCode: summary, Payload: payload,
	})
	return err
}
