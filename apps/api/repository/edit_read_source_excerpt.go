package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func sourceExcerptEvidenceStatus(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	excerpt domain.SourceExcerptState,
) (domain.SourceExcerptEvidenceStatus, error) {
	var available bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM assets asset
  JOIN media_artifacts artifact
    ON artifact.id = ? AND artifact.project_id = asset.project_id
   AND artifact.asset_id = asset.id AND artifact.kind = 'transcript'
   AND artifact.state = 'ready' AND artifact.input_fingerprint = ?
  JOIN transcript_artifacts transcript
    ON transcript.artifact_id = artifact.id AND transcript.source_stream_id = ?
  WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0
    AND asset.accepted_fingerprint = ?
)`, excerpt.Evidence.ArtifactID.String(), excerpt.AcceptedFingerprint.String(),
		excerpt.Evidence.SourceStreamID.String(), excerpt.AssetID.String(), projectID.String(),
		excerpt.AcceptedFingerprint.String()).Scan(&available); err != nil {
		return "", err
	}
	if !available {
		return domain.SourceExcerptEvidenceStale, nil
	}
	startKey, endKey, err := sourceOrderKeys(excerpt.SourceRange)
	if err != nil {
		return "", application.ErrEditInvalid
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, revision FROM transcript_corrections
WHERE project_id = ? AND artifact_id = ? AND language = ? AND tombstoned = 0
  AND source_start_order_key < ? AND source_end_order_key > ?
ORDER BY source_start_order_key, id LIMIT 257`,
		projectID.String(), excerpt.Evidence.ArtifactID.String(), excerpt.Language.String(), endKey, startKey)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	current := make(map[string]domain.Revision)
	for rows.Next() {
		if len(current) >= 256 {
			return domain.SourceExcerptEvidenceStale, nil
		}
		var id string
		var revisionValue uint64
		if err := rows.Scan(&id, &revisionValue); err != nil {
			return "", err
		}
		revision, err := domain.NewRevision(revisionValue)
		if err != nil {
			return "", application.ErrEditInvalid
		}
		current[id] = revision
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(current) != len(excerpt.Evidence.CorrectionRevisions) {
		return domain.SourceExcerptEvidenceStale, nil
	}
	for _, pinned := range excerpt.Evidence.CorrectionRevisions {
		if current[pinned.ID.String()] != pinned.Revision {
			return domain.SourceExcerptEvidenceStale, nil
		}
	}
	return domain.SourceExcerptEvidenceExact, nil
}

func ensureSourceExcerptEvidenceStatus(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	excerpt domain.SourceExcerptState,
) error {
	status, err := sourceExcerptEvidenceStatus(ctx, tx, state.ProjectID, excerpt)
	if err != nil {
		return err
	}
	state.SourceExcerptEvidence[excerpt.ID.String()] = status
	return nil
}
