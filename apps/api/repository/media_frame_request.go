package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) RequestMediaFrameSet(
	ctx context.Context,
	record application.RequestMediaFrameSetRecord,
) (application.MediaFrameSetRequestResult, error) {
	if err := validateMediaFrameSetRequest(record); err != nil {
		return application.MediaFrameSetRequestResult{}, application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.MediaFrameSetRequestResult{}, err
	}
	defer tx.Rollback()
	var liveRun int
	if err := tx.QueryRowContext(ctx, `
SELECT 1
FROM agent_runs run
JOIN agent_turns turn ON turn.id = run.current_turn_id AND turn.run_id = run.id
WHERE run.id = ? AND run.project_id = ? AND run.actor_id = ?
  AND run.status IN ('active', 'waiting')
  AND turn.id = ? AND turn.status = 'active'`,
		record.RunID.String(), record.ProjectID.String(), record.Actor.IDString(), record.TurnID.String(),
	).Scan(&liveRun); errors.Is(err, sql.ErrNoRows) {
		return application.MediaFrameSetRequestResult{}, application.ErrRunStaleTurn
	} else if err != nil {
		return application.MediaFrameSetRequestResult{}, err
	}
	var fingerprint, mediaType string
	if err := tx.QueryRowContext(ctx, `
SELECT asset.accepted_fingerprint, stream.media_type
FROM assets asset
JOIN asset_media_state state ON state.asset_id = asset.id
JOIN source_streams stream ON stream.id = ? AND stream.asset_id = asset.id
  AND stream.fingerprint = asset.accepted_fingerprint
WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0
  AND state.observed_fingerprint = asset.accepted_fingerprint
  AND state.availability IN ('online', 'managed')`,
		record.Parameters.SourceStreamID.String(), record.AssetID.String(), record.ProjectID.String(),
	).Scan(&fingerprint, &mediaType); errors.Is(err, sql.ErrNoRows) {
		return application.MediaFrameSetRequestResult{}, application.ErrAssetInvalid
	} else if err != nil {
		return application.MediaFrameSetRequestResult{}, err
	}
	if fingerprint != record.Parameters.Fingerprint.String() || mediaType != string(domain.MediaVideo) {
		return application.MediaFrameSetRequestResult{}, application.ErrAssetInvalid
	}
	jobID := record.JobID
	var existing string
	err = tx.QueryRowContext(ctx, `
SELECT id FROM work_jobs
WHERE logical_key = ? AND state IN ('blocked', 'queued', 'running', 'succeeded')`, record.LogicalKey).Scan(&existing)
	created := false
	if err == nil {
		jobID, err = domain.ParseMediaJobID(existing)
		if err != nil {
			return application.MediaFrameSetRequestResult{}, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		created = true
		at := formatInstant(record.RequestedAt.UTC())
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, kind, state, pool, priority_class, logical_key,
  parameters_digest, parameters_json, producer_version, created_at, updated_at
) VALUES (?, 'project', ?, 'frame-sample-set', 'blocked', 'interactive-cpu', 'interactive', ?, ?, ?, ?, ?, ?)`,
			record.JobID.String(), record.ProjectID.String(), record.LogicalKey,
			record.ParametersDigest.String(), string(record.ParametersJSON), application.FrameSetProfile, at, at,
		); err != nil {
			return application.MediaFrameSetRequestResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO media_job_details (job_id, asset_id) VALUES (?, ?)`,
			record.JobID.String(), record.AssetID.String()); err != nil {
			return application.MediaFrameSetRequestResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'media-executor/frame-sample-set', ?)`,
			record.JobID.String(), at,
		); err != nil {
			return application.MediaFrameSetRequestResult{}, err
		}
	} else {
		return application.MediaFrameSetRequestResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
VALUES (?, 'run', ?, ?)`, jobID.String(), record.RunID.String(), formatInstant(record.RequestedAt.UTC())); err != nil {
		return application.MediaFrameSetRequestResult{}, err
	}
	if created {
		if err := appendFrameSetRequestedActivity(ctx, tx, record); err != nil {
			return application.MediaFrameSetRequestResult{}, err
		}
	}
	selected, err := loadMediaJobSummary(ctx, tx, jobID)
	if err != nil {
		return application.MediaFrameSetRequestResult{}, err
	}
	cursor, err := loadActivityHead(ctx, tx, "project", record.ProjectID.String())
	if err != nil {
		return application.MediaFrameSetRequestResult{}, err
	}
	result := application.MediaFrameSetRequestResult{
		Status: application.MediaFrameSetAccepted, Job: selected,
		Resources: []application.FrameResourceLease{}, ActivityCursor: cursor,
	}
	if selected.State == domain.MediaJobSucceeded && selected.ResultArtifactID != nil {
		result.Status, result.ArtifactID = application.MediaFrameSetReady, selected.ResultArtifactID
	}
	if err := tx.Commit(); err != nil {
		return application.MediaFrameSetRequestResult{}, err
	}
	return result, nil
}

func validateMediaFrameSetRequest(record application.RequestMediaFrameSetRecord) error {
	if record.JobID.IsZero() || record.ProjectID.IsZero() || record.AssetID.IsZero() ||
		record.RunID.IsZero() || record.TurnID.IsZero() || record.Actor.Validate() != nil ||
		record.Actor.Kind != domain.ActorAgent || record.Parameters.Validate() != nil ||
		record.Parameters.AssetID != record.AssetID || record.ParametersDigest.String() == "" ||
		record.LogicalKey == "" || len(record.LogicalKey) > 1024 || record.ActivityEventID.IsZero() ||
		record.RequestedAt.IsZero() || !json.Valid(record.ParametersJSON) {
		return application.ErrAssetInvalid
	}
	canonical, digest, err := application.CanonicalFrameSetParameters(record.Parameters)
	if err != nil || !bytes.Equal(canonical, record.ParametersJSON) || digest != record.ParametersDigest {
		return application.ErrAssetInvalid
	}
	return nil
}

func appendFrameSetRequestedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.RequestMediaFrameSetRecord,
) error {
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, record.ProjectID.String()).Scan(&projectRevision); err != nil {
		return err
	}
	revision, err := domain.NewRevision(projectRevision)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		JobID             domain.MediaJobID              `json:"jobId"`
		RunID             domain.RunID                   `json:"runId"`
		TurnID            domain.TurnID                  `json:"turnId"`
		SourceStreamID    domain.SourceStreamID          `json:"sourceStreamId"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{{
			Kind: "asset-media-state", ID: record.AssetID.String(), Revision: revision,
		}},
		JobID: record.JobID, RunID: record.RunID, TurnID: record.TurnID,
		SourceStreamID: record.Parameters.SourceStreamID,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.ProjectID.String(), EventID: record.ActivityEventID.String(),
		Kind: "media.frame-set-requested", OccurredAt: formatInstant(record.RequestedAt.UTC()),
		ActorKind: string(record.Actor.Kind), ActorID: record.Actor.IDString(),
		ProjectID: record.ProjectID.String(), ProjectRevision: int64(revision.Value()),
		OutcomeKind: "media-job", OutcomeID: record.JobID.String(),
		SummaryCode: "media-frame-set-requested", Payload: payload,
	})
	return err
}
