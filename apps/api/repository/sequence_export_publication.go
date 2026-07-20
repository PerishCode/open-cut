package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

var sequenceExportFailureCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (repository *SQLiteProjects) CompleteSequenceExport(
	ctx context.Context,
	input application.CompleteSequenceExport,
) (resultErr error) {
	if err := validateSequenceExportPublication(input); err != nil {
		return err
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	finalRoot, err := repository.publishSequenceExportFiles(input)
	if err != nil {
		return err
	}
	defer func() { discardUnpublishedTree(finalRoot, &resultErr) }()
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifySequenceExportAttempt(ctx, tx, input.Claim, input.CompletedAt.UTC()); err != nil {
		return err
	}
	var boundPlan string
	if err := tx.QueryRowContext(ctx, `
SELECT render_plan_digest FROM sequence_export_job_details WHERE job_id = ?`,
		input.Claim.JobID.String()).Scan(&boundPlan); err != nil || boundPlan != input.Plan.Plan.Digest.String() {
		return application.ErrWorkLeaseLost
	}
	factsJSON, err := json.Marshal(input.Manifest.Facts)
	if err != nil {
		return err
	}
	at := formatInstant(input.CompletedAt.UTC())
	byteReference := "artifact:sequence-export/" + input.ArtifactID.String()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_artifacts (
  id, producer_job_id, project_id, sequence_id, sequence_revision,
  render_plan_digest, renderer_version, renderer_target, output_profile,
  state, facts_json, byte_reference, byte_size, content_digest, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'valid', ?, ?, ?, ?, ?)`,
		input.ArtifactID.String(), input.Claim.JobID.String(), input.Manifest.ProjectID.String(),
		input.Manifest.SequenceID.String(), input.Manifest.SequenceRevision.Value(),
		input.Manifest.RenderPlanDigest.String(), input.Manifest.RendererVersion,
		input.Manifest.RendererTarget, input.Manifest.Profile, string(factsJSON), byteReference,
		input.ByteSize.Value(), input.ContentDigest.String(), at,
	); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE sequence_export_job_details SET result_artifact_id = ?
WHERE job_id = ? AND render_plan_digest = ? AND result_artifact_id IS NULL`,
		input.ArtifactID.String(), input.Claim.JobID.String(), input.Plan.Plan.Digest.String(),
	)
	if err != nil {
		return err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
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
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
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
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM render_material_leases WHERE attempt_id = ?`,
		input.Claim.AttemptID.String()); err != nil {
		return err
	}
	if err := appendSequenceExportTerminalActivity(
		ctx, tx, input.Claim, input.EventID, input.CompletedAt,
		"sequence.export-ready", "sequence-export-ready", nil,
	); err != nil {
		return err
	}
	return commitPublication(tx)
}

func (repository *SQLiteProjects) FailSequenceExport(
	ctx context.Context,
	input application.FailSequenceExport,
) error {
	if input.Claim.Kind != domain.WorkJobSequenceExport || input.Claim.SequenceExport == nil ||
		input.EventID.IsZero() || input.FailedAt.IsZero() ||
		!sequenceExportFailureCodePattern.MatchString(input.Code) {
		return application.ErrSequenceExportInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifySequenceExportAttempt(ctx, tx, input.Claim, input.FailedAt.UTC()); err != nil {
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
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	result, err = tx.ExecContext(ctx, `
UPDATE work_jobs SET state = 'failed', updated_at = ?, terminal_error_code = ?
WHERE id = ? AND state = 'running'`, at, input.Code, input.Claim.JobID.String())
	if err != nil {
		return err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.ErrWorkLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM render_material_leases WHERE attempt_id = ?`,
		input.Claim.AttemptID.String()); err != nil {
		return err
	}
	if err := appendSequenceExportTerminalActivity(
		ctx, tx, input.Claim, input.EventID, input.FailedAt,
		"sequence.export-failed", "sequence-export-failed", &input.Code,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func validateSequenceExportPublication(input application.CompleteSequenceExport) error {
	if input.Claim.Kind != domain.WorkJobSequenceExport || input.Claim.SequenceExport == nil ||
		input.ArtifactID.IsZero() || input.EventID.IsZero() || input.CompletedAt.IsZero() ||
		input.Workspace == nil || input.Manifest.Validate() != nil ||
		input.Manifest.ProducerJobID != input.Claim.JobID ||
		input.Manifest.ProjectID != input.Claim.SequenceExport.ProjectID ||
		input.Manifest.SequenceID != input.Claim.SequenceExport.SequenceID ||
		input.Manifest.SequenceRevision != input.Claim.SequenceExport.SequenceRevision ||
		input.Manifest.RenderPlanDigest != input.Plan.Plan.Digest ||
		input.Manifest.RendererVersion != input.Claim.ExecutorVersion ||
		input.Manifest.RendererTarget != input.Claim.SequenceExport.Parameters.RendererTarget ||
		input.Manifest.Profile != domain.SequenceExportProfileV1 {
		return application.ErrSequenceExportInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-artifact", domain.SequenceExportArtifactSchema, input.Manifest,
	)
	if err != nil || !bytes.Equal(canonical, input.ManifestCanonical) || digest != input.ContentDigest ||
		len(canonical) > application.MaximumSequenceExportManifestSize ||
		uint64(len(canonical))+input.Manifest.Media.ByteSize.Value() != input.ByteSize.Value() ||
		input.ByteSize.Value() > application.MaximumSequenceExportArtifactSize {
		return application.ErrSequenceExportInvalid
	}
	expected, err := application.SequenceExportFactsForPlan(input.Plan.Plan.Payload)
	if err != nil || input.Manifest.Facts != expected {
		return application.ErrSequenceExportInvalid
	}
	return nil
}

func (repository *SQLiteProjects) publishSequenceExportFiles(
	input application.CompleteSequenceExport,
) (string, error) {
	workRoot := filepath.Join(repository.dataDir, "work", "sequence-export-publication")
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-export")
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
	if err := copySequenceExportFile(stageRoot, input.Workspace, input.Manifest.Media); err != nil {
		return "", err
	}
	if err := syncDirectory(stageRoot); err != nil {
		return "", err
	}
	finalRoot := filepath.Join(artifactRoot, input.ArtifactID.String())
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		return "", fmt.Errorf("sequence export artifact destination already exists")
	}
	if err := os.Rename(stageRoot, finalRoot); err != nil {
		return "", err
	}
	if err := syncDirectory(artifactRoot); err != nil {
		return "", err
	}
	return finalRoot, nil
}

func copySequenceExportFile(
	stageRoot string,
	workspace application.PreparedMediaWorkspace,
	record application.SequenceExportArtifactFile,
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
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(source, int64(record.ByteSize.Value())+1))
	if copyErr == nil && uint64(written) != record.ByteSize.Value() {
		copyErr = application.ErrSequenceExportInvalid
	}
	if copyErr == nil && "sha256:"+hex.EncodeToString(digest.Sum(nil)) != record.SHA256.String() {
		copyErr = application.ErrSequenceExportInvalid
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

func appendSequenceExportTerminalActivity(
	ctx context.Context,
	tx *sql.Tx,
	claim application.WorkJobClaim,
	eventID domain.ActivityEventID,
	at time.Time,
	kind, summary string,
	failureCode *string,
) error {
	export := claim.SequenceExport
	var revisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`,
		export.ProjectID.String()).Scan(&revisionValue); err != nil {
		return err
	}
	revision, err := domain.NewRevision(revisionValue)
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
		AttemptID: claim.AttemptID, SequenceID: export.SequenceID,
		SequenceRevision: export.SequenceRevision, FailureCode: failureCode,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: export.ProjectID.String(), EventID: eventID.String(), Kind: kind,
		OccurredAt: formatInstant(at.UTC()), ProjectID: export.ProjectID.String(),
		ProjectRevision: int64(revision.Value()), OutcomeKind: "work-job",
		OutcomeID: claim.JobID.String(), SummaryCode: summary, Payload: payload,
	})
	return err
}
