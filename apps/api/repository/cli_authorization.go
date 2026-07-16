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

func (repository *SQLiteProjects) FindCLIGrant(
	ctx context.Context,
	installationID, publicKey string,
) (application.CLIGrant, error) {
	return scanCLIGrant(repository.db.QueryRowContext(ctx, cliGrantSelect+`
WHERE installation_id = ? AND role = 'product-cli' AND public_key = ?`, installationID, publicKey))
}

func (repository *SQLiteProjects) EnsurePendingCLIGrant(
	ctx context.Context,
	pending application.PendingCLIGrant,
) (application.CLIGrant, error) {
	if err := validatePendingCLIGrant(pending); err != nil {
		return application.CLIGrant{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.CLIGrant{}, err
	}
	defer tx.Rollback()
	stored, err := scanCLIGrant(tx.QueryRowContext(ctx, cliGrantSelect+`
WHERE installation_id = ? AND role = 'product-cli' AND public_key = ?`, pending.InstallationID, pending.PublicKey))
	if err == nil {
		if stored.Status == application.CLIGrantExpired ||
			(stored.Status == application.CLIGrantPending && !pending.CreatedAt.Before(stored.ExpiresAt)) {
			normalized, normalizeErr := application.NormalizeCLIScopes(pending.Scopes)
			if normalizeErr != nil {
				return application.CLIGrant{}, normalizeErr
			}
			scopes, encodeErr := json.Marshal(normalized)
			if encodeErr != nil {
				return application.CLIGrant{}, encodeErr
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE installation_grants
SET scopes_json = ?, status = 'pending', revision = revision + 1,
    created_at = ?, expires_at = ?, decided_at = NULL, revoked_at = NULL
WHERE id = ?`, string(scopes), formatInstant(pending.CreatedAt), formatInstant(pending.ExpiresAt), stored.ID); err != nil {
				return application.CLIGrant{}, err
			}
			stored.Scopes = normalized
			stored.Revision, _ = stored.Revision.Next()
			stored.ScopeDigest, _ = application.CLIScopeDigest(normalized)
			stored.Status = application.CLIGrantPending
			stored.CreatedAt = pending.CreatedAt.UTC()
			stored.ExpiresAt = pending.ExpiresAt.UTC()
			stored.DecidedAt = nil
			stored.RevokedAt = nil
		}
		if err := tx.Commit(); err != nil {
			return application.CLIGrant{}, err
		}
		return stored, nil
	}
	if !errors.Is(err, application.ErrCLIGrantNotFound) {
		return application.CLIGrant{}, err
	}
	normalized, err := application.NormalizeCLIScopes(pending.Scopes)
	if err != nil {
		return application.CLIGrant{}, err
	}
	scopes, err := json.Marshal(normalized)
	if err != nil {
		return application.CLIGrant{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO agent_principals (id, created_at) VALUES (?, ?)`,
		pending.AgentID.String(), formatInstant(pending.CreatedAt),
	); err != nil {
		return application.CLIGrant{}, fmt.Errorf("persist agent principal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO installation_grants (
  id, installation_id, agent_id, role, algorithm, public_key, public_key_fingerprint,
  scopes_json, status, created_at, expires_at
) VALUES (?, ?, ?, 'product-cli', 'ed25519', ?, ?, ?, 'pending', ?, ?)
`, pending.ID, pending.InstallationID, pending.AgentID.String(), pending.PublicKey, pending.Fingerprint,
		string(scopes), formatInstant(pending.CreatedAt), formatInstant(pending.ExpiresAt)); err != nil {
		return application.CLIGrant{}, fmt.Errorf("persist pending CLI grant: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.CLIGrant{}, err
	}
	grant := application.CLIGrant{
		ID: pending.ID, InstallationID: pending.InstallationID, AgentID: pending.AgentID,
		PublicKey: pending.PublicKey, PublicKeyFingerprint: pending.Fingerprint,
		Scopes: normalized, Status: application.CLIGrantPending,
		CreatedAt: pending.CreatedAt.UTC(), ExpiresAt: pending.ExpiresAt.UTC(),
	}
	grant.Revision, _ = domain.NewRevision(1)
	grant.ScopeDigest, _ = application.CLIScopeDigest(normalized)
	return grant, nil
}

func (repository *SQLiteProjects) ListCLIGrants(
	ctx context.Context,
	installationID string,
) ([]application.CLIGrant, error) {
	rows, err := repository.db.QueryContext(ctx, cliGrantSelect+`
WHERE installation_id = ? AND role = 'product-cli'
ORDER BY created_at DESC, id`, installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]application.CLIGrant, 0)
	for rows.Next() {
		grant, err := scanCLIGrant(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, grant)
	}
	return result, rows.Err()
}

func (repository *SQLiteProjects) DecideCLIGrant(
	ctx context.Context,
	id string,
	approve bool,
	decidedAt time.Time,
) (application.CLIGrant, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.CLIGrant{}, err
	}
	defer tx.Rollback()
	grant, err := scanCLIGrant(tx.QueryRowContext(ctx, cliGrantSelect+` WHERE id = ?`, id))
	if err != nil {
		return application.CLIGrant{}, err
	}
	if grant.Status != application.CLIGrantPending || !decidedAt.Before(grant.ExpiresAt) {
		if grant.Status == application.CLIGrantPending {
			_, _ = tx.ExecContext(ctx, `UPDATE installation_grants SET status = 'expired' WHERE id = ?`, id)
			_ = tx.Commit()
		}
		return application.CLIGrant{}, application.ErrCLIGrantNotPending
	}
	status := application.CLIGrantDenied
	if approve {
		status = application.CLIGrantActive
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE installation_grants SET status = ?, decided_at = ? WHERE id = ?`,
		status, formatInstant(decidedAt), id,
	); err != nil {
		return application.CLIGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CLIGrant{}, err
	}
	instant := decidedAt.UTC()
	grant.Status = status
	grant.DecidedAt = &instant
	return grant, nil
}

func (repository *SQLiteProjects) RevokeCLIGrant(
	ctx context.Context,
	id string,
	revokedAt time.Time,
) (application.CLIGrant, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.CLIGrant{}, err
	}
	defer tx.Rollback()
	grant, err := scanCLIGrant(tx.QueryRowContext(ctx, cliGrantSelect+` WHERE id = ?`, id))
	if err != nil {
		return application.CLIGrant{}, err
	}
	if grant.Status != application.CLIGrantActive {
		return application.CLIGrant{}, application.ErrCLIGrantNotActive
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE installation_grants SET status = 'revoked', revoked_at = ? WHERE id = ?`,
		formatInstant(revokedAt), id,
	); err != nil {
		return application.CLIGrant{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE installation_grant_scope_upgrades SET status = 'expired', decided_at = ? WHERE grant_id = ? AND status = 'pending'`,
		formatInstant(revokedAt), id,
	); err != nil {
		return application.CLIGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CLIGrant{}, err
	}
	instant := revokedAt.UTC()
	grant.Status = application.CLIGrantRevoked
	grant.RevokedAt = &instant
	return grant, nil
}

const cliGrantSelect = `
SELECT id, installation_id, agent_id, public_key, public_key_fingerprint,
       scopes_json, revision, status, created_at, expires_at, decided_at, revoked_at
FROM installation_grants `

func scanCLIGrant(row rowScanner) (application.CLIGrant, error) {
	var (
		grant                                         application.CLIGrant
		agentID, scopes, status, createdAt, expiresAt string
		revisionValue                                 uint64
		decidedAt, revokedAt                          sql.NullString
	)
	if err := row.Scan(
		&grant.ID, &grant.InstallationID, &agentID, &grant.PublicKey, &grant.PublicKeyFingerprint,
		&scopes, &revisionValue, &status, &createdAt, &expiresAt, &decidedAt, &revokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.CLIGrant{}, application.ErrCLIGrantNotFound
		}
		return application.CLIGrant{}, err
	}
	parsedAgent, err := domain.ParseAgentID(agentID)
	if err != nil {
		return application.CLIGrant{}, err
	}
	grant.AgentID = parsedAgent
	grant.Revision, err = domain.NewRevision(revisionValue)
	if err != nil || grant.Revision.Value() < 1 {
		return application.CLIGrant{}, application.ErrCLIUpgradeInvalid
	}
	grant.Status = application.CLIGrantStatus(status)
	if err := json.Unmarshal([]byte(scopes), &grant.Scopes); err != nil {
		return application.CLIGrant{}, err
	}
	grant.Scopes, err = application.NormalizeCLIScopes(grant.Scopes)
	if err != nil {
		return application.CLIGrant{}, err
	}
	grant.ScopeDigest, err = application.CLIScopeDigest(grant.Scopes)
	if err != nil {
		return application.CLIGrant{}, err
	}
	grant.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return application.CLIGrant{}, err
	}
	grant.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return application.CLIGrant{}, err
	}
	if decidedAt.Valid {
		instant, parseErr := time.Parse(time.RFC3339Nano, decidedAt.String)
		if parseErr != nil {
			return application.CLIGrant{}, parseErr
		}
		grant.DecidedAt = &instant
	}
	if revokedAt.Valid {
		instant, parseErr := time.Parse(time.RFC3339Nano, revokedAt.String)
		if parseErr != nil {
			return application.CLIGrant{}, parseErr
		}
		grant.RevokedAt = &instant
	}
	return grant, nil
}

func validatePendingCLIGrant(pending application.PendingCLIGrant) error {
	if _, err := domain.ParseActivityEventID(pending.ID); err != nil {
		return err
	}
	if _, err := domain.ParseRequestID(pending.InstallationID); err != nil {
		return err
	}
	if pending.AgentID.IsZero() || pending.PublicKey == "" || pending.Fingerprint == "" ||
		len(pending.Scopes) == 0 || !pending.CreatedAt.Before(pending.ExpiresAt) {
		return application.ErrAuthorityInvalid
	}
	if _, err := application.NormalizeCLIScopes(pending.Scopes); err != nil {
		return err
	}
	return nil
}

func formatInstant(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
