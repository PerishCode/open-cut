package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type stagedSequenceExportDeletion struct {
	source string
	stage  string
}

func (repository *SQLiteProjects) DeleteSequenceExportArtifact(
	ctx context.Context,
	record application.DeleteSequenceExportArtifactRecord,
) (result application.SequenceExportResult, resultErr error) {
	if err := validateSequenceExportDeleteRecord(record); err != nil {
		return application.SequenceExportResult{}, err
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	defer tx.Rollback()
	if err := verifySequenceExportAccess(ctx, tx, record.ReadSequenceExportRecord, true); err != nil {
		return application.SequenceExportResult{}, err
	}
	replayRecord := application.ReplaySequenceExportRequestRecord{
		ReadSequenceExportRecord: record.ReadSequenceExportRecord, Command: "delete-artifact",
		RequestID: record.RequestID, RequestDigest: record.RequestDigest,
		RequestCanonical: record.RequestCanonical,
	}
	if replay, found, err := replaySequenceExportRequestTx(ctx, tx, replayRecord); err != nil {
		return application.SequenceExportResult{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return application.SequenceExportResult{}, err
		}
		replay.Replayed = true
		return replay, nil
	}
	tail, err := resolveSequenceExportTailID(ctx, tx, record.ProjectID, record.JobID)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	current, err := loadSequenceExportResult(ctx, tx, record.ProjectID, tail)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if current.Job.State != domain.MediaJobSucceeded || current.Job.Artifact == nil ||
		current.Job.Artifact.ID != record.ArtifactID ||
		(current.Job.Artifact.State != domain.SequenceExportArtifactValid &&
			current.Job.Artifact.State != domain.SequenceExportArtifactInvalid) {
		return application.SequenceExportResult{}, application.ErrSequenceExportRecovery
	}
	staged, err := repository.stageSequenceExportArtifactDeletion(record.ArtifactID)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	committed := false
	defer func() {
		if committed || staged.stage == "" {
			return
		}
		if restoreErr := restoreSequenceExportArtifactDeletion(staged); restoreErr != nil && resultErr == nil {
			resultErr = restoreErr
		}
	}()
	updated, err := tx.ExecContext(ctx, `
UPDATE sequence_export_artifacts SET state = 'deleted'
WHERE id = ? AND state IN ('valid', 'invalid')`, record.ArtifactID.String())
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if changed, rowsErr := updated.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.SequenceExportResult{}, application.ErrSequenceExportRecovery
	}
	if err := appendSequenceExportDeletedActivity(ctx, tx, record, tail); err != nil {
		return application.SequenceExportResult{}, err
	}
	at := formatInstant(record.DeletedAt.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_requests (
  actor_id, request_id, command, input_digest, input_json, project_id,
  owner_kind, owner_id, run_id, turn_id, job_id, activity_event_id, created_at
) VALUES (?, ?, 'delete-artifact', ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?)`,
		record.Actor.IDString(), record.RequestID.String(), record.RequestDigest.String(),
		string(record.RequestCanonical), record.ProjectID.String(), string(record.Owner.Kind), record.Owner.ID,
		tail.String(), record.ActivityEventID.String(), at,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	result, err = loadSequenceExportResult(ctx, tx, record.ProjectID, tail)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportResult{}, err
	}
	committed = true
	if staged.stage != "" {
		_ = os.RemoveAll(staged.stage)
		_ = syncDirectory(filepath.Dir(staged.stage))
	}
	return result, nil
}

func validateSequenceExportDeleteRecord(record application.DeleteSequenceExportArtifactRecord) error {
	if err := validateSequenceExportReadRecord(record.ReadSequenceExportRecord); err != nil ||
		record.Owner.Kind != application.SequenceExportOwnerCreator || record.ArtifactID.IsZero() ||
		record.ActivityEventID.IsZero() || record.DeletedAt.IsZero() || record.RequestDigest == "" ||
		!json.Valid(record.RequestCanonical) {
		return application.ErrSequenceExportInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrSequenceExportInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-delete-artifact", application.SequenceExportDeleteArtifactSchema, struct {
			ArtifactID domain.ArtifactID `json:"artifactId"`
			JobID      domain.WorkJobID  `json:"jobId"`
		}{record.ArtifactID, record.JobID},
	)
	if err != nil || digest != record.RequestDigest || !bytes.Equal(canonical, record.RequestCanonical) {
		return application.ErrSequenceExportInvalid
	}
	return nil
}

func (repository *SQLiteProjects) stageSequenceExportArtifactDeletion(
	artifactID domain.ArtifactID,
) (stagedSequenceExportDeletion, error) {
	canonical := filepath.Join(repository.dataDir, "artifacts", "sequence-export", artifactID.String())
	quarantine := filepath.Join(repository.dataDir, "work", "sequence-export-quarantine", artifactID.String())
	canonicalExists, err := pathExists(canonical)
	if err != nil {
		return stagedSequenceExportDeletion{}, err
	}
	quarantineExists, err := pathExists(quarantine)
	if err != nil {
		return stagedSequenceExportDeletion{}, err
	}
	if canonicalExists && quarantineExists {
		return stagedSequenceExportDeletion{}, application.ErrSequenceExportIntegrity
	}
	source := canonical
	if quarantineExists {
		source = quarantine
	} else if !canonicalExists {
		return stagedSequenceExportDeletion{}, nil
	}
	root := filepath.Join(repository.dataDir, "work", "sequence-export-deletions")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return stagedSequenceExportDeletion{}, err
	}
	stage := filepath.Join(root, artifactID.String())
	if exists, err := pathExists(stage); err != nil {
		return stagedSequenceExportDeletion{}, err
	} else if exists {
		return stagedSequenceExportDeletion{}, application.ErrSequenceExportIntegrity
	}
	if err := os.Rename(source, stage); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return stagedSequenceExportDeletion{}, application.ErrSequenceExportArtifactInUse
		}
		return stagedSequenceExportDeletion{}, err
	}
	if err := syncDirectory(filepath.Dir(source)); err != nil {
		_ = os.Rename(stage, source)
		return stagedSequenceExportDeletion{}, err
	}
	if err := syncDirectory(root); err != nil {
		_ = os.Rename(stage, source)
		return stagedSequenceExportDeletion{}, err
	}
	return stagedSequenceExportDeletion{source: source, stage: stage}, nil
}

func restoreSequenceExportArtifactDeletion(staged stagedSequenceExportDeletion) error {
	if exists, err := pathExists(staged.source); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("sequence export deletion source was recreated")
	}
	if err := os.Rename(staged.stage, staged.source); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(staged.source)); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(staged.stage))
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func appendSequenceExportDeletedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.DeleteSequenceExportArtifactRecord,
	jobID domain.WorkJobID,
) error {
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`,
		record.ProjectID.String()).Scan(&projectRevision); err != nil {
		return err
	}
	revision, err := domain.NewRevision(projectRevision)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ArtifactID        domain.ArtifactID              `json:"artifactId"`
		JobID             domain.WorkJobID               `json:"jobId"`
	}{[]application.ChangedEntityRef{}, record.ArtifactID, jobID})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.ProjectID.String(), EventID: record.ActivityEventID.String(),
		Kind: "sequence.export-artifact-deleted", OccurredAt: formatInstant(record.DeletedAt.UTC()),
		ActorKind: string(record.Actor.Kind), ActorID: record.Actor.IDString(),
		ProjectID: record.ProjectID.String(), ProjectRevision: int64(revision.Value()),
		OutcomeKind: "export-artifact", OutcomeID: record.ArtifactID.String(),
		SummaryCode: "sequence-export-artifact-deleted", Payload: payload,
	})
	return err
}
