package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) EnsureLocalCreator(
	ctx context.Context,
	candidate domain.CreatorID,
	createdAt time.Time,
) (domain.CreatorID, error) {
	if candidate.IsZero() {
		return domain.CreatorID{}, domain.ErrInvalidDurableID
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.CreatorID{}, err
	}
	defer tx.Rollback()
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT id FROM local_creators WHERE singleton = 1`).Scan(&stored)
	if err != nil && err != sql.ErrNoRows {
		return domain.CreatorID{}, err
	}
	if err == sql.ErrNoRows {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO local_creators (id, singleton, created_at) VALUES (?, 1, ?)`,
			candidate.String(), createdAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return domain.CreatorID{}, fmt.Errorf("persist local creator: %w", err)
		}
		stored = candidate.String()
	}
	creator, err := domain.ParseCreatorID(stored)
	if err != nil {
		return domain.CreatorID{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.CreatorID{}, err
	}
	return creator, nil
}

func (repository *SQLiteProjects) AppendAuthorizationAudit(
	ctx context.Context,
	record application.AuthorizationAudit,
) error {
	if _, err := domain.ParseActivityEventID(record.ID); err != nil {
		return err
	}
	if _, err := domain.ParseRequestID(record.InstallationID); err != nil {
		return err
	}
	if record.PrincipalKind != application.AuthorityFirstPartyUI && record.PrincipalKind != application.AuthorityProductCLI {
		return application.ErrAuthorityInvalid
	}
	if record.PrincipalID == "" || record.Action == "" || record.Outcome == "" {
		return application.ErrAuthorityInvalid
	}
	_, err := repository.db.ExecContext(ctx, `
INSERT INTO authorization_audit (
  id, installation_id, principal_kind, principal_id, action, outcome, request_digest, occurred_at
) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?)
`, record.ID, record.InstallationID, record.PrincipalKind, record.PrincipalID,
		record.Action, record.Outcome, record.RequestDigest, record.OccurredAt.UTC().Format(time.RFC3339Nano))
	return err
}
