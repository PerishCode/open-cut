package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const maximumRetainedAgentTurnVersions = 32

func (repository *SQLiteProjects) CreateProjectVersion(
	ctx context.Context,
	record application.CreateProjectVersionRecord,
) (application.CreateProjectVersionResult, error) {
	if record.ID.IsZero() || record.ProjectID.IsZero() || record.Creator.Validate() != nil ||
		record.Creator.Kind != domain.ActorCreator || record.ActivityEventID.IsZero() ||
		!json.Valid(record.RequestCanonical) || record.Name == "" || record.CreatedAt.IsZero() {
		return application.CreateProjectVersionResult{}, application.ErrProjectVersionInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.CreateProjectVersionResult{}, err
	}
	defer tx.Rollback()
	if result, found, err := loadCreateVersionReplay(ctx, tx, record); err != nil {
		return application.CreateProjectVersionResult{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return application.CreateProjectVersionResult{}, err
		}
		result.Replayed = true
		return result, nil
	}
	version, err := insertProjectVersionSnapshot(ctx, tx, projectVersionCapture{
		ID: record.ID, ProjectID: record.ProjectID, CreatorID: *record.Creator.CreatorID,
		Source: application.ProjectVersionManual, Name: record.Name,
		Retention: application.ProjectVersionRetain, CreatedAt: record.CreatedAt,
	})
	if err != nil {
		return application.CreateProjectVersionResult{}, err
	}
	payload, _ := json.Marshal(struct {
		VersionID domain.ProjectVersionID `json:"versionId"`
	}{VersionID: version.ID})
	cursor, err := appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.ProjectID.String(), EventID: record.ActivityEventID.String(),
		Kind: "project.version-created", OccurredAt: formatInstant(record.CreatedAt),
		ActorKind: string(domain.ActorCreator), ActorID: record.Creator.IDString(),
		ProjectID: record.ProjectID.String(), ProjectRevision: int64(version.CapturedProjectRevision.Value()),
		OutcomeKind: "project-version", OutcomeID: version.ID.String(),
		SummaryCode: "project-version-created", Payload: payload,
	})
	if err != nil {
		return application.CreateProjectVersionResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO project_version_requests (
  creator_id, request_id, command, input_digest, input_json, project_id,
  version_id, activity_event_id, created_at
) VALUES (?, ?, 'create', ?, ?, ?, ?, ?, ?)`, record.Creator.IDString(), record.RequestID.String(),
		record.RequestDigest.String(), string(record.RequestCanonical), record.ProjectID.String(),
		version.ID.String(), record.ActivityEventID.String(), formatInstant(record.CreatedAt)); err != nil {
		return application.CreateProjectVersionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CreateProjectVersionResult{}, err
	}
	return application.CreateProjectVersionResult{Version: version, ActivityCursor: cursor}, nil
}

func (repository *SQLiteProjects) ListProjectVersions(
	ctx context.Context,
	projectID domain.ProjectID,
	before domain.ProjectVersionID,
	limit uint16,
) (application.ProjectVersionPage, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProjectVersionPage{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, projectID.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ProjectVersionPage{}, application.ErrProjectNotFound
		}
		return application.ProjectVersionPage{}, err
	}
	var rows *sql.Rows
	if before.IsZero() {
		rows, err = tx.QueryContext(ctx, projectVersionSelect+`
WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, projectID.String(), uint32(limit)+1)
	} else {
		var beforeCreatedAt string
		if err := tx.QueryRowContext(ctx, `SELECT created_at FROM project_versions WHERE id = ? AND project_id = ?`,
			before.String(), projectID.String()).Scan(&beforeCreatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return application.ProjectVersionPage{}, application.ErrProjectVersionNotFound
			}
			return application.ProjectVersionPage{}, err
		}
		rows, err = tx.QueryContext(ctx, projectVersionSelect+`
WHERE project_id = ? AND (created_at < ? OR (created_at = ? AND id < ?))
ORDER BY created_at DESC, id DESC LIMIT ?`, projectID.String(), beforeCreatedAt, beforeCreatedAt,
			before.String(), uint32(limit)+1)
	}
	if err != nil {
		return application.ProjectVersionPage{}, err
	}
	versions := make([]application.ProjectVersion, 0, uint32(limit)+1)
	for rows.Next() {
		version, err := scanProjectVersion(rows)
		if err != nil {
			rows.Close()
			return application.ProjectVersionPage{}, err
		}
		versions = append(versions, version)
	}
	if err := rows.Close(); err != nil {
		return application.ProjectVersionPage{}, err
	}
	if err := rows.Err(); err != nil {
		return application.ProjectVersionPage{}, err
	}
	cursor, err := loadActivityHead(ctx, tx, "project", projectID.String())
	if err != nil {
		return application.ProjectVersionPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.ProjectVersionPage{}, err
	}
	var nextBefore *domain.ProjectVersionID
	if len(versions) > int(limit) {
		next := versions[limit-1].ID
		nextBefore = &next
		versions = versions[:limit]
	}
	return application.ProjectVersionPage{Versions: versions, NextBefore: nextBefore, ActivityCursor: cursor}, nil
}

type projectVersionCapture struct {
	ID          domain.ProjectVersionID
	ProjectID   domain.ProjectID
	CreatorID   domain.CreatorID
	Source      application.ProjectVersionSource
	Name        string
	TriggerKind string
	TriggerID   string
	Retention   application.ProjectVersionRetention
	CreatedAt   time.Time
}

func insertProjectVersionSnapshot(
	ctx context.Context,
	tx *sql.Tx,
	capture projectVersionCapture,
) (application.ProjectVersion, error) {
	if capture.Source != application.ProjectVersionManual {
		var revision uint64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ? AND status = 'active'`,
			capture.ProjectID.String()).Scan(&revision); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return application.ProjectVersion{}, application.ErrProjectNotFound
			}
			return application.ProjectVersion{}, err
		}
		row := tx.QueryRowContext(ctx, projectVersionSelect+`
WHERE project_id = ? AND captured_project_revision = ? AND source = ?`,
			capture.ProjectID.String(), revision, capture.Source)
		if existing, scanErr := scanProjectVersion(row); scanErr == nil {
			return existing, nil
		} else if !errors.Is(scanErr, sql.ErrNoRows) {
			return application.ProjectVersion{}, scanErr
		}
	}
	state, canonical, digest, compressed, err := captureProjectVersionState(ctx, tx, capture.ProjectID)
	if err != nil {
		return application.ProjectVersion{}, versionStateError("capture", err)
	}
	var parent any
	var parentValue string
	err = tx.QueryRowContext(ctx, `
SELECT id FROM project_versions WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`,
		capture.ProjectID.String()).Scan(&parentValue)
	if err == nil {
		parent = parentValue
	} else if !errors.Is(err, sql.ErrNoRows) {
		return application.ProjectVersion{}, err
	}
	var name, triggerKind, triggerID any
	if capture.Name != "" {
		name = capture.Name
	}
	if capture.TriggerKind != "" {
		triggerKind, triggerID = capture.TriggerKind, capture.TriggerID
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO project_versions (
  id, project_id, parent_version_id, captured_project_revision, source, name,
  trigger_kind, trigger_id, state_schema, state_digest, state_bytes, state_byte_size,
  retention, creator_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, capture.ID.String(), capture.ProjectID.String(), parent,
		state.ProjectRevision.Value(), capture.Source, name, triggerKind, triggerID,
		projectVersionStateSchema, digest.String(), compressed, len(canonical), capture.Retention,
		capture.CreatorID.String(), formatInstant(capture.CreatedAt)); err != nil {
		return application.ProjectVersion{}, fmt.Errorf("persist project version: %w", err)
	}
	result := application.ProjectVersion{
		ID: capture.ID, ProjectID: capture.ProjectID, CapturedProjectRevision: state.ProjectRevision,
		Source: capture.Source, Name: capture.Name, TriggerKind: capture.TriggerKind, TriggerID: capture.TriggerID,
		Digest: digest, Retention: capture.Retention, CreatedAt: capture.CreatedAt.UTC(),
	}
	result.ByteSize, _ = domain.NewUInt64(uint64(len(canonical)))
	if parentValue != "" {
		parsed, parseErr := domain.ParseProjectVersionID(parentValue)
		if parseErr != nil {
			return application.ProjectVersion{}, application.ErrProjectVersionInvalid
		}
		result.ParentVersionID = &parsed
	}
	if capture.Source == application.ProjectVersionAgentTurn {
		if err := pruneAutomaticAgentTurnVersions(ctx, tx, capture.ProjectID); err != nil {
			return application.ProjectVersion{}, err
		}
	}
	return result, nil
}

func pruneAutomaticAgentTurnVersions(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID) error {
	_, err := tx.ExecContext(ctx, `
DELETE FROM project_versions
WHERE id IN (
  SELECT candidate.id FROM project_versions candidate
  WHERE candidate.project_id = ? AND candidate.source = 'agent-turn' AND candidate.retention = 'automatic'
    AND NOT EXISTS (
      SELECT 1 FROM project_version_requests request
      WHERE request.version_id = candidate.id OR request.safety_version_id = candidate.id
    )
  ORDER BY candidate.created_at DESC, candidate.id DESC
  LIMIT -1 OFFSET ?
)`, projectID.String(), maximumRetainedAgentTurnVersions)
	return err
}

const projectVersionSelect = `
SELECT id, project_id, parent_version_id, captured_project_revision, source,
       name, trigger_kind, trigger_id, state_digest, state_byte_size, retention, created_at
FROM project_versions `

type versionScanner interface{ Scan(...any) error }

func scanProjectVersion(scanner versionScanner) (application.ProjectVersion, error) {
	var idValue, projectValue, source, digestValue, retention, createdAt string
	var parent, name, triggerKind, triggerID sql.NullString
	var revision, byteSize uint64
	if err := scanner.Scan(&idValue, &projectValue, &parent, &revision, &source,
		&name, &triggerKind, &triggerID, &digestValue, &byteSize, &retention, &createdAt); err != nil {
		return application.ProjectVersion{}, err
	}
	id, idErr := domain.ParseProjectVersionID(idValue)
	projectID, projectErr := domain.ParseProjectID(projectValue)
	revisionValue, revisionErr := domain.NewRevision(revision)
	digest, digestErr := domain.ParseDigest(digestValue)
	size, sizeErr := domain.NewUInt64(byteSize)
	instant, timeErr := time.Parse(time.RFC3339Nano, createdAt)
	if idErr != nil || projectErr != nil || revisionErr != nil || digestErr != nil || sizeErr != nil || timeErr != nil {
		return application.ProjectVersion{}, application.ErrProjectVersionInvalid
	}
	result := application.ProjectVersion{ID: id, ProjectID: projectID, CapturedProjectRevision: revisionValue,
		Source: application.ProjectVersionSource(source), Digest: digest, ByteSize: size,
		Retention: application.ProjectVersionRetention(retention), CreatedAt: instant.UTC()}
	if name.Valid {
		result.Name = name.String
	}
	if triggerKind.Valid {
		result.TriggerKind = triggerKind.String
	}
	if triggerID.Valid {
		result.TriggerID = triggerID.String
	}
	if parent.Valid {
		parsed, err := domain.ParseProjectVersionID(parent.String)
		if err != nil {
			return application.ProjectVersion{}, application.ErrProjectVersionInvalid
		}
		result.ParentVersionID = &parsed
	}
	return result, nil
}

func loadCreateVersionReplay(
	ctx context.Context,
	tx *sql.Tx,
	record application.CreateProjectVersionRecord,
) (application.CreateProjectVersionResult, bool, error) {
	var digestValue, projectValue, versionValue, eventValue string
	err := tx.QueryRowContext(ctx, `
SELECT input_digest, project_id, version_id, activity_event_id
FROM project_version_requests WHERE creator_id = ? AND request_id = ?`,
		record.Creator.IDString(), record.RequestID.String()).Scan(&digestValue, &projectValue, &versionValue, &eventValue)
	if errors.Is(err, sql.ErrNoRows) {
		return application.CreateProjectVersionResult{}, false, nil
	}
	if err != nil {
		return application.CreateProjectVersionResult{}, false, err
	}
	if digestValue != record.RequestDigest.String() || projectValue != record.ProjectID.String() {
		return application.CreateProjectVersionResult{}, false, application.ErrProjectVersionRequestReused
	}
	version, err := scanProjectVersion(tx.QueryRowContext(ctx, projectVersionSelect+`WHERE id = ?`, versionValue))
	if err != nil {
		return application.CreateProjectVersionResult{}, false, err
	}
	cursor, err := activityCursorForEvent(ctx, tx, eventValue)
	return application.CreateProjectVersionResult{Version: version, ActivityCursor: cursor}, true, err
}
