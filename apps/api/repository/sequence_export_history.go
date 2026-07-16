package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type sequenceExportHistoryRoot struct {
	id        domain.WorkJobID
	origin    application.SequenceExportOrigin
	createdAt time.Time
}

func (repository *SQLiteProjects) ListSequenceExportHistory(
	ctx context.Context,
	query application.SequenceExportHistoryQuery,
) (application.SequenceExportHistoryPage, error) {
	if query.ProjectID.IsZero() || query.Limit < 1 || query.Limit > 50 {
		return application.SequenceExportHistoryPage{}, application.ErrSequenceExportInvalid
	}
	if query.AfterRootID != "" {
		if _, err := domain.ParseWorkJobID(query.AfterRootID); err != nil {
			return application.SequenceExportHistoryPage{}, application.ErrSequenceExportInvalid
		}
		if query.AfterCreatedAt.IsZero() {
			return application.SequenceExportHistoryPage{}, application.ErrSequenceExportInvalid
		}
	} else if !query.AfterCreatedAt.IsZero() {
		return application.SequenceExportHistoryPage{}, application.ErrSequenceExportInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceExportHistoryPage{}, err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRowContext(ctx, `
SELECT 1 FROM projects WHERE id = ? AND status = 'active'`, query.ProjectID.String()).Scan(&active); errors.Is(err, sql.ErrNoRows) {
		return application.SequenceExportHistoryPage{}, application.ErrProjectNotFound
	} else if err != nil {
		return application.SequenceExportHistoryPage{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT root.id, owner.owner_kind, root.created_at
FROM work_jobs root
JOIN sequence_export_job_details detail ON detail.job_id = root.id
JOIN work_job_owners owner ON owner.job_id = root.id
WHERE root.project_id = ? AND root.kind = 'sequence-export' AND root.retry_of_job_id IS NULL
  AND (? = '' OR julianday(root.created_at) < julianday(?) OR
       (julianday(root.created_at) = julianday(?) AND root.id < ?))
ORDER BY julianday(root.created_at) DESC, root.id DESC
LIMIT ?`, query.ProjectID.String(), query.AfterRootID,
		formatInstant(query.AfterCreatedAt), formatInstant(query.AfterCreatedAt), query.AfterRootID, query.Limit+1)
	if err != nil {
		return application.SequenceExportHistoryPage{}, err
	}
	roots := make([]sequenceExportHistoryRoot, 0, query.Limit+1)
	for rows.Next() {
		var rootValue, ownerKind, createdValue string
		if err := rows.Scan(&rootValue, &ownerKind, &createdValue); err != nil {
			rows.Close()
			return application.SequenceExportHistoryPage{}, err
		}
		rootID, err := domain.ParseWorkJobID(rootValue)
		if err != nil {
			rows.Close()
			return application.SequenceExportHistoryPage{}, application.ErrSequenceExportInvalid
		}
		origin := application.SequenceExportOriginCreator
		if ownerKind == string(application.SequenceExportOwnerAgentRun) {
			origin = application.SequenceExportOriginAgent
		} else if ownerKind != string(application.SequenceExportOwnerCreator) {
			rows.Close()
			return application.SequenceExportHistoryPage{}, application.ErrSequenceExportInvalid
		}
		createdAt, err := time.Parse(time.RFC3339Nano, createdValue)
		if err != nil {
			rows.Close()
			return application.SequenceExportHistoryPage{}, application.ErrSequenceExportInvalid
		}
		roots = append(roots, sequenceExportHistoryRoot{id: rootID, origin: origin, createdAt: createdAt.UTC()})
	}
	if err := rows.Close(); err != nil {
		return application.SequenceExportHistoryPage{}, err
	}
	if err := rows.Err(); err != nil {
		return application.SequenceExportHistoryPage{}, err
	}
	hasMore := len(roots) > query.Limit
	if hasMore {
		roots = roots[:query.Limit]
	}
	lineages := make([]application.SequenceExportLineage, 0, len(roots))
	for _, root := range roots {
		tailID, err := resolveSequenceExportTailID(ctx, tx, query.ProjectID, root.id)
		if err != nil {
			return application.SequenceExportHistoryPage{}, err
		}
		result, err := loadSequenceExportResult(ctx, tx, query.ProjectID, tailID)
		if err != nil {
			return application.SequenceExportHistoryPage{}, err
		}
		attemptCount, err := countSequenceExportLineage(ctx, tx, query.ProjectID, root.id)
		if err != nil {
			return application.SequenceExportHistoryPage{}, err
		}
		lineages = append(lineages, application.SequenceExportLineage{
			Origin: root.origin, AttemptCount: attemptCount,
			ArtifactAvailability: sequenceExportArtifactAvailability(result.Job.Artifact),
			RootCreatedAt:        root.createdAt, Export: result,
		})
	}
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.SequenceExportHistoryPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportHistoryPage{}, err
	}
	return application.SequenceExportHistoryPage{
		Lineages: lineages, HasMore: hasMore, ActivityCursor: cursor,
	}, nil
}

func countSequenceExportLineage(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	rootID domain.WorkJobID,
) (domain.UInt64, error) {
	var count uint64
	if err := tx.QueryRowContext(ctx, `
WITH RECURSIVE chain(id) AS (
  SELECT id FROM work_jobs
  WHERE id = ? AND project_id = ? AND kind = 'sequence-export' AND retry_of_job_id IS NULL
  UNION ALL
  SELECT retry.id FROM work_jobs retry
  JOIN chain predecessor ON retry.retry_of_job_id = predecessor.id
  WHERE retry.project_id = ? AND retry.kind = 'sequence-export'
)
SELECT COUNT(*) FROM chain`, rootID.String(), projectID.String(), projectID.String()).Scan(&count); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, application.ErrSequenceExportInvalid
	}
	return domain.NewUInt64(count)
}

func sequenceExportArtifactAvailability(
	artifact *domain.SequenceExportArtifactSummary,
) application.SequenceExportArtifactAvailability {
	if artifact == nil {
		return application.SequenceExportArtifactNone
	}
	switch artifact.State {
	case domain.SequenceExportArtifactValid:
		return application.SequenceExportArtifactReady
	case domain.SequenceExportArtifactInvalid:
		return application.SequenceExportArtifactInvalid
	case domain.SequenceExportArtifactDeleted:
		return application.SequenceExportArtifactDeleted
	default:
		return application.SequenceExportArtifactNone
	}
}
