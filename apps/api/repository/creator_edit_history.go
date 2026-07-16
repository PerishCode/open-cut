package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ReadCreatorTransactionHistory(
	ctx context.Context,
	query application.CreatorTransactionHistoryQuery,
) (application.CreatorTransactionHistoryResult, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.CreatorTransactionHistoryResult{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, query.ProjectID.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.CreatorTransactionHistoryResult{}, application.ErrProjectNotFound
		}
		return application.CreatorTransactionHistoryResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM edit_transactions
WHERE project_id = ? AND digest IS NOT NULL AND (? = 0 OR project_revision < ?)
ORDER BY project_revision DESC, id DESC LIMIT ?`,
		query.ProjectID.String(), query.BeforeRevision.Value(), query.BeforeRevision.Value(), query.Limit+1)
	if err != nil {
		return application.CreatorTransactionHistoryResult{}, err
	}
	ids := make([]domain.TransactionID, 0, query.Limit+1)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return application.CreatorTransactionHistoryResult{}, err
		}
		id, err := domain.ParseTransactionID(value)
		if err != nil {
			rows.Close()
			return application.CreatorTransactionHistoryResult{}, application.ErrEditInvalid
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return application.CreatorTransactionHistoryResult{}, err
	}
	if err := rows.Err(); err != nil {
		return application.CreatorTransactionHistoryResult{}, err
	}
	hasMore := len(ids) > query.Limit
	if hasMore {
		ids = ids[:query.Limit]
	}
	items := make([]application.CreatorTransactionHistoryItem, 0, len(ids))
	for _, id := range ids {
		transaction, err := loadEditTransaction(ctx, tx, query.ProjectID, id)
		if err != nil {
			return application.CreatorTransactionHistoryResult{}, err
		}
		items = append(items, application.CreatorTransactionHistoryItem{
			ID: transaction.ID, Intent: transaction.Intent, Actor: transaction.Actor.Kind,
			CommittedProjectRevision: transaction.CommittedProjectRevision,
			Changes:                  transaction.Changes, UndoesTransactionID: transaction.UndoesTransactionID,
			CommittedAt: transaction.CommittedAt,
		})
	}
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.CreatorTransactionHistoryResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CreatorTransactionHistoryResult{}, err
	}
	return application.CreatorTransactionHistoryResult{
		Transactions: items, HasMore: hasMore, ActivityCursor: cursor,
	}, nil
}
