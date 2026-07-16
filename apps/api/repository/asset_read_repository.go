package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ListAssetDetails(
	ctx context.Context,
	query application.AssetListQuery,
) (application.AssetListResult, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.AssetListResult{}, err
	}
	defer tx.Rollback()
	var projectExists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, query.ProjectID.String()).Scan(&projectExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.AssetListResult{}, application.ErrProjectNotFound
		}
		return application.AssetListResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM assets
WHERE project_id = ? AND tombstoned = 0 AND id > ?
ORDER BY id LIMIT ?`, query.ProjectID.String(), query.AfterID, query.Limit+1)
	if err != nil {
		return application.AssetListResult{}, err
	}
	ids := make([]domain.AssetID, 0, query.Limit+1)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return application.AssetListResult{}, err
		}
		id, err := domain.ParseAssetID(value)
		if err != nil {
			rows.Close()
			return application.AssetListResult{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return application.AssetListResult{}, err
	}
	if err := rows.Err(); err != nil {
		return application.AssetListResult{}, err
	}
	hasMore := len(ids) > query.Limit
	if hasMore {
		ids = ids[:query.Limit]
	}
	assets := make([]domain.AssetDetail, 0, len(ids))
	for _, id := range ids {
		detail, err := loadAssetDetail(ctx, tx, query.ProjectID, id)
		if err != nil {
			return application.AssetListResult{}, err
		}
		assets = append(assets, detail)
	}
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.AssetListResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AssetListResult{}, err
	}
	return application.AssetListResult{Assets: assets, HasMore: hasMore, ActivityCursor: cursor}, nil
}

func (repository *SQLiteProjects) ReadAssetDetail(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
) (domain.AssetDetail, domain.Cursor, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.AssetDetail{}, 0, err
	}
	defer tx.Rollback()
	detail, err := loadAssetDetail(ctx, tx, projectID, assetID)
	if err != nil {
		return domain.AssetDetail{}, 0, err
	}
	cursor, err := loadActivityHead(ctx, tx, "project", projectID.String())
	if err != nil {
		return domain.AssetDetail{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return domain.AssetDetail{}, 0, err
	}
	return detail, cursor, nil
}

func loadAssetDetail(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	assetID domain.AssetID,
) (domain.AssetDetail, error) {
	var (
		assetValue, projectValue, grantValue, displayName, importMode, availability string
		acceptedValue, observedValue, factsJSON                                     sql.NullString
		revisionValue                                                               uint64
		tombstoned                                                                  bool
	)
	err := tx.QueryRowContext(ctx, `
SELECT a.id, a.project_id, a.revision, a.source_grant_id, a.display_name,
       a.import_mode, a.accepted_fingerprint, a.tombstoned,
       m.availability, m.observed_fingerprint, m.facts_json
FROM assets a
JOIN asset_media_state m ON m.asset_id = a.id
WHERE a.id = ? AND a.project_id = ?`, assetID.String(), projectID.String()).Scan(
		&assetValue, &projectValue, &revisionValue, &grantValue, &displayName,
		&importMode, &acceptedValue, &tombstoned, &availability, &observedValue, &factsJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AssetDetail{}, application.ErrAssetNotFound
	}
	if err != nil {
		return domain.AssetDetail{}, err
	}
	parsedAsset, err := domain.ParseAssetID(assetValue)
	if err != nil {
		return domain.AssetDetail{}, err
	}
	parsedProject, err := domain.ParseProjectID(projectValue)
	if err != nil {
		return domain.AssetDetail{}, err
	}
	grantID, err := domain.ParseSourceGrantID(grantValue)
	if err != nil {
		return domain.AssetDetail{}, err
	}
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return domain.AssetDetail{}, err
	}
	detail := domain.AssetDetail{
		Asset: domain.AssetState{
			ID: parsedAsset, Revision: revision, ProjectID: parsedProject, SourceGrantID: grantID,
			DisplayName: displayName, ImportMode: domain.AssetImportMode(importMode), Tombstoned: tombstoned,
		},
		Availability: domain.AssetAvailability(availability),
		Artifacts:    []domain.ArtifactSummary{}, Jobs: []domain.MediaJobSummary{},
	}
	if acceptedValue.Valid {
		fingerprint, parseErr := domain.ParseDigest(acceptedValue.String)
		if parseErr != nil {
			return domain.AssetDetail{}, parseErr
		}
		detail.Asset.AcceptedFingerprint = &fingerprint
	}
	if observedValue.Valid {
		fingerprint, parseErr := domain.ParseDigest(observedValue.String)
		if parseErr != nil {
			return domain.AssetDetail{}, parseErr
		}
		detail.Fingerprint = &fingerprint
	}
	if factsJSON.Valid {
		var facts domain.MediaFacts
		if err := json.Unmarshal([]byte(factsJSON.String), &facts); err != nil {
			return domain.AssetDetail{}, err
		}
		detail.Facts = &facts
	}
	detail.Artifacts, err = loadArtifactSummaries(ctx, tx, assetID)
	if err != nil {
		return domain.AssetDetail{}, err
	}
	detail.Jobs, err = loadMediaJobSummaries(ctx, tx, assetID)
	if err != nil {
		return domain.AssetDetail{}, err
	}
	return detail, nil
}

func loadArtifactSummaries(
	ctx context.Context,
	tx *sql.Tx,
	assetID domain.AssetID,
) ([]domain.ArtifactSummary, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, kind, producer_version, input_fingerprint, state, byte_size, content_digest, created_at
FROM (
  SELECT id, kind, producer_version, input_fingerprint, state, byte_size, content_digest, created_at
  FROM media_artifacts WHERE asset_id = ? ORDER BY created_at DESC, id DESC LIMIT 32
) recent
ORDER BY created_at, id`, assetID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.ArtifactSummary, 0)
	for rows.Next() {
		var idValue, kind, producer, inputDigest, state, contentDigest, createdAt string
		var byteSize uint64
		if err := rows.Scan(&idValue, &kind, &producer, &inputDigest, &state, &byteSize, &contentDigest, &createdAt); err != nil {
			return nil, err
		}
		id, err := domain.ParseArtifactID(idValue)
		if err != nil {
			return nil, err
		}
		inputFingerprint, err := domain.ParseDigest(inputDigest)
		if err != nil {
			return nil, err
		}
		content, err := domain.ParseDigest(contentDigest)
		if err != nil {
			return nil, err
		}
		size, err := domain.NewUInt64(byteSize)
		if err != nil {
			return nil, err
		}
		instant, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.ArtifactSummary{
			ID: id, Kind: domain.ArtifactKind(kind), ProducerVersion: producer,
			InputFingerprint: inputFingerprint, State: domain.ArtifactState(state),
			ByteSize: size, ContentDigest: content, CreatedAt: instant.UTC(),
		})
	}
	return result, rows.Err()
}

func loadMediaJobSummaries(
	ctx context.Context,
	tx *sql.Tx,
	assetID domain.AssetID,
) ([]domain.MediaJobSummary, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, kind, state, progress_basis_points, terminal_error_code,
       result_artifact_id, created_at, updated_at
FROM (
  SELECT job.id, job.kind, job.state, job.progress_basis_points,
         job.terminal_error_code, detail.result_artifact_id, job.created_at, job.updated_at
  FROM work_jobs job
  JOIN media_job_details detail ON detail.job_id = job.id
  WHERE detail.asset_id = ? ORDER BY job.created_at DESC, job.id DESC LIMIT 32
) recent
ORDER BY created_at, id`, assetID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.MediaJobSummary, 0)
	for rows.Next() {
		var idValue, kind, state, createdAt, updatedAt string
		var terminalError, artifactValue sql.NullString
		var progress uint16
		if err := rows.Scan(
			&idValue, &kind, &state, &progress, &terminalError, &artifactValue, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		id, err := domain.ParseMediaJobID(idValue)
		if err != nil {
			return nil, err
		}
		created, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		updated, err := time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, err
		}
		job := domain.MediaJobSummary{
			ID: id, Kind: domain.MediaJobKind(kind), State: domain.MediaJobState(state),
			ProgressBasisPoints: progress, Prerequisites: []domain.MediaJobPrerequisite{},
			CreatedAt: created.UTC(), UpdatedAt: updated.UTC(),
		}
		if terminalError.Valid {
			if len(terminalError.String) == 0 || len(terminalError.String) > 256 {
				return nil, domain.ErrInvalidMediaFacts
			}
			job.TerminalErrorCode = &terminalError.String
		}
		job.Prerequisites, err = loadMediaJobPrerequisites(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if artifactValue.Valid {
			artifactID, parseErr := domain.ParseArtifactID(artifactValue.String)
			if parseErr != nil {
				return nil, parseErr
			}
			job.ResultArtifactID = &artifactID
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func loadMediaJobSummary(
	ctx context.Context,
	tx *sql.Tx,
	jobID domain.MediaJobID,
) (domain.MediaJobSummary, error) {
	var idValue, kind, state, createdAt, updatedAt string
	var terminalError, artifactValue sql.NullString
	var progress uint16
	err := tx.QueryRowContext(ctx, `
SELECT job.id, job.kind, job.state, job.progress_basis_points,
       job.terminal_error_code, detail.result_artifact_id, job.created_at, job.updated_at
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
WHERE job.id = ?`, jobID.String()).Scan(
		&idValue, &kind, &state, &progress, &terminalError, &artifactValue, &createdAt, &updatedAt,
	)
	if err != nil {
		return domain.MediaJobSummary{}, err
	}
	id, err := domain.ParseMediaJobID(idValue)
	if err != nil {
		return domain.MediaJobSummary{}, err
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.MediaJobSummary{}, err
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return domain.MediaJobSummary{}, err
	}
	job := domain.MediaJobSummary{
		ID: id, Kind: domain.MediaJobKind(kind), State: domain.MediaJobState(state),
		ProgressBasisPoints: progress, Prerequisites: []domain.MediaJobPrerequisite{},
		CreatedAt: created.UTC(), UpdatedAt: updated.UTC(),
	}
	if terminalError.Valid {
		if len(terminalError.String) == 0 || len(terminalError.String) > 256 {
			return domain.MediaJobSummary{}, domain.ErrInvalidMediaFacts
		}
		job.TerminalErrorCode = &terminalError.String
	}
	job.Prerequisites, err = loadMediaJobPrerequisites(ctx, tx, id)
	if err != nil {
		return domain.MediaJobSummary{}, err
	}
	if artifactValue.Valid {
		artifactID, parseErr := domain.ParseArtifactID(artifactValue.String)
		if parseErr != nil {
			return domain.MediaJobSummary{}, parseErr
		}
		job.ResultArtifactID = &artifactID
	}
	return job, nil
}

func loadMediaJobPrerequisites(
	ctx context.Context,
	tx *sql.Tx,
	jobID domain.MediaJobID,
) ([]domain.MediaJobPrerequisite, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT kind, reference_kind, reference_id
FROM work_job_prerequisites
WHERE job_id = ?
ORDER BY kind, reference_kind, reference_id
LIMIT 9`, jobID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.MediaJobPrerequisite, 0)
	for rows.Next() {
		if len(result) >= 8 {
			return nil, application.ErrAssetInvalid
		}
		var kind, referenceKind, referenceID string
		if err := rows.Scan(&kind, &referenceKind, &referenceID); err != nil {
			return nil, err
		}
		prerequisite := domain.MediaJobPrerequisite{Kind: domain.MediaJobPrerequisiteKind(kind)}
		switch referenceKind {
		case "job":
			id, parseErr := domain.ParseMediaJobID(referenceID)
			if parseErr != nil {
				return nil, parseErr
			}
			prerequisite.JobID = &id
		case "resource":
			prerequisite.ResourceID = referenceID
		case "capability":
			prerequisite.Capability = referenceID
		default:
			return nil, application.ErrAssetInvalid
		}
		if prerequisite.Validate() != nil {
			return nil, application.ErrAssetInvalid
		}
		result = append(result, prerequisite)
	}
	return result, rows.Err()
}
