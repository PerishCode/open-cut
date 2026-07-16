package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) EnsurePendingCLIGrantScopeUpgrade(
	ctx context.Context,
	pending application.PendingCLIGrantScopeUpgrade,
) (application.CLIGrantScopeUpgrade, error) {
	normalized, err := validatePendingCLIGrantScopeUpgrade(pending)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	defer tx.Rollback()
	grant, err := scanCLIGrant(tx.QueryRowContext(ctx, cliGrantSelect+` WHERE id = ?`, pending.GrantID))
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	if grant.Status != application.CLIGrantActive || grant.Revision != pending.FromRevision ||
		!scopeSetContains(normalized, grant.Scopes) || len(normalized) <= len(grant.Scopes) {
		return application.CLIGrantScopeUpgrade{}, application.ErrCLIUpgradeInvalid
	}
	existing, err := scanCLIGrantScopeUpgrade(tx.QueryRowContext(ctx, cliGrantScopeUpgradeSelect+`
WHERE grant_id = ? AND status = 'pending'`, pending.GrantID))
	if err == nil {
		if pending.CreatedAt.Before(existing.ExpiresAt) && existing.FromRevision == pending.FromRevision &&
			slices.Equal(existing.RequestedScopes, normalized) {
			if err := tx.Commit(); err != nil {
				return application.CLIGrantScopeUpgrade{}, err
			}
			return existing, nil
		}
		status := application.CLIGrantScopeUpgradeSuperseded
		if !pending.CreatedAt.Before(existing.ExpiresAt) {
			status = application.CLIGrantScopeUpgradeExpired
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE installation_grant_scope_upgrades
SET status = ?, decided_at = ?
WHERE id = ? AND status = 'pending'`, status, formatInstant(pending.CreatedAt), existing.ID); err != nil {
			return application.CLIGrantScopeUpgrade{}, err
		}
	} else if !errors.Is(err, application.ErrCLIUpgradeNotFound) {
		return application.CLIGrantScopeUpgrade{}, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO installation_grant_scope_upgrades (
  id, grant_id, from_revision, requested_scopes_json, status, created_at, expires_at
) VALUES (?, ?, ?, ?, 'pending', ?, ?)`,
		pending.ID, pending.GrantID, pending.FromRevision.Value(), string(encoded),
		formatInstant(pending.CreatedAt), formatInstant(pending.ExpiresAt),
	); err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	digest, _ := application.CLIScopeDigest(normalized)
	return application.CLIGrantScopeUpgrade{
		ID: pending.ID, GrantID: pending.GrantID, FromRevision: pending.FromRevision,
		RequestedScopes: normalized, RequestedScopeDigest: digest,
		Status:    application.CLIGrantScopeUpgradePending,
		CreatedAt: pending.CreatedAt.UTC(), ExpiresAt: pending.ExpiresAt.UTC(),
	}, nil
}

func (repository *SQLiteProjects) ListCLIGrantScopeUpgrades(
	ctx context.Context,
	installationID string,
) ([]application.CLIGrantScopeUpgrade, error) {
	rows, err := repository.db.QueryContext(ctx, cliGrantScopeUpgradeSelect+`
JOIN installation_grants AS grant ON grant.id = upgrade.grant_id
WHERE grant.installation_id = ?
ORDER BY upgrade.created_at DESC, upgrade.id`, installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]application.CLIGrantScopeUpgrade, 0)
	for rows.Next() {
		upgrade, err := scanCLIGrantScopeUpgrade(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, upgrade)
	}
	return result, rows.Err()
}

func (repository *SQLiteProjects) DecideCLIGrantScopeUpgrade(
	ctx context.Context,
	id string,
	approve bool,
	decidedAt time.Time,
) (application.CLIGrantScopeUpgrade, application.CLIGrant, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, err
	}
	defer tx.Rollback()
	upgrade, err := scanCLIGrantScopeUpgrade(tx.QueryRowContext(ctx, cliGrantScopeUpgradeSelect+` WHERE upgrade.id = ?`, id))
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, err
	}
	if upgrade.Status != application.CLIGrantScopeUpgradePending || !decidedAt.Before(upgrade.ExpiresAt) {
		if upgrade.Status == application.CLIGrantScopeUpgradePending {
			_, _ = tx.ExecContext(ctx, `
UPDATE installation_grant_scope_upgrades SET status = 'expired', decided_at = ? WHERE id = ?`,
				formatInstant(decidedAt), id)
			_ = tx.Commit()
		}
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, application.ErrCLIUpgradeNotPending
	}
	grant, err := scanCLIGrant(tx.QueryRowContext(ctx, cliGrantSelect+` WHERE id = ?`, upgrade.GrantID))
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, err
	}
	if grant.Status != application.CLIGrantActive || grant.Revision != upgrade.FromRevision {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, application.ErrCLIUpgradeNotPending
	}
	status := application.CLIGrantScopeUpgradeDenied
	if approve {
		encoded, encodeErr := json.Marshal(upgrade.RequestedScopes)
		if encodeErr != nil {
			return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, encodeErr
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE installation_grants SET scopes_json = ?, revision = revision + 1 WHERE id = ? AND revision = ?`,
			string(encoded), grant.ID, grant.Revision.Value()); err != nil {
			return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, err
		}
		status = application.CLIGrantScopeUpgradeApproved
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE installation_grant_scope_upgrades SET status = ?, decided_at = ? WHERE id = ? AND status = 'pending'`,
		status, formatInstant(decidedAt), id); err != nil {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, err
	}
	instant := decidedAt.UTC()
	upgrade.Status = status
	upgrade.DecidedAt = &instant
	if approve {
		grant.Scopes = append([]string(nil), upgrade.RequestedScopes...)
		grant.Revision, _ = grant.Revision.Next()
		grant.ScopeDigest = upgrade.RequestedScopeDigest
	}
	return upgrade, grant, nil
}

const cliGrantScopeUpgradeSelect = `
SELECT upgrade.id, upgrade.grant_id, upgrade.from_revision, upgrade.requested_scopes_json,
       upgrade.status, upgrade.created_at, upgrade.expires_at, upgrade.decided_at
FROM installation_grant_scope_upgrades AS upgrade `

func scanCLIGrantScopeUpgrade(row rowScanner) (application.CLIGrantScopeUpgrade, error) {
	var upgrade application.CLIGrantScopeUpgrade
	var revision uint64
	var scopes, status, createdAt, expiresAt string
	var decidedAt sql.NullString
	if err := row.Scan(
		&upgrade.ID, &upgrade.GrantID, &revision, &scopes, &status,
		&createdAt, &expiresAt, &decidedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.CLIGrantScopeUpgrade{}, application.ErrCLIUpgradeNotFound
		}
		return application.CLIGrantScopeUpgrade{}, err
	}
	parsedRevision, err := domain.NewRevision(revision)
	if err != nil || parsedRevision.Value() < 1 {
		return application.CLIGrantScopeUpgrade{}, application.ErrCLIUpgradeInvalid
	}
	upgrade.FromRevision = parsedRevision
	if err := json.Unmarshal([]byte(scopes), &upgrade.RequestedScopes); err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	upgrade.RequestedScopes, err = application.NormalizeCLIScopes(upgrade.RequestedScopes)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	upgrade.RequestedScopeDigest, err = application.CLIScopeDigest(upgrade.RequestedScopes)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	upgrade.Status = application.CLIGrantScopeUpgradeStatus(status)
	upgrade.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	upgrade.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	if decidedAt.Valid {
		instant, parseErr := time.Parse(time.RFC3339Nano, decidedAt.String)
		if parseErr != nil {
			return application.CLIGrantScopeUpgrade{}, parseErr
		}
		upgrade.DecidedAt = &instant
	}
	return upgrade, nil
}

func validatePendingCLIGrantScopeUpgrade(
	pending application.PendingCLIGrantScopeUpgrade,
) ([]string, error) {
	if _, err := domain.ParseActivityEventID(pending.ID); err != nil {
		return nil, err
	}
	if _, err := domain.ParseActivityEventID(pending.GrantID); err != nil {
		return nil, err
	}
	if pending.FromRevision.Value() < 1 || !pending.CreatedAt.Before(pending.ExpiresAt) {
		return nil, application.ErrCLIUpgradeInvalid
	}
	return application.NormalizeCLIScopes(pending.RequestedScopes)
}

func scopeSetContains(superset, subset []string) bool {
	for _, expected := range subset {
		if _, exists := slices.BinarySearch(superset, expected); !exists {
			return false
		}
	}
	return true
}

func (repository *MemoryProjects) EnsurePendingCLIGrantScopeUpgrade(
	_ context.Context,
	pending application.PendingCLIGrantScopeUpgrade,
) (application.CLIGrantScopeUpgrade, error) {
	normalized, err := validatePendingCLIGrantScopeUpgrade(pending)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	grant, ok := repository.grants[pending.GrantID]
	if !ok {
		return application.CLIGrantScopeUpgrade{}, application.ErrCLIGrantNotFound
	}
	if grant.Status != application.CLIGrantActive || grant.Revision != pending.FromRevision ||
		!scopeSetContains(normalized, grant.Scopes) || len(normalized) <= len(grant.Scopes) {
		return application.CLIGrantScopeUpgrade{}, application.ErrCLIUpgradeInvalid
	}
	if currentID := repository.pendingUpgrades[grant.ID]; currentID != "" {
		current := repository.grantUpgrades[currentID]
		if pending.CreatedAt.Before(current.ExpiresAt) && current.FromRevision == pending.FromRevision &&
			slices.Equal(current.RequestedScopes, normalized) {
			return cloneCLIGrantScopeUpgrade(current), nil
		}
		instant := pending.CreatedAt.UTC()
		current.Status = application.CLIGrantScopeUpgradeSuperseded
		if !pending.CreatedAt.Before(current.ExpiresAt) {
			current.Status = application.CLIGrantScopeUpgradeExpired
		}
		current.DecidedAt = &instant
		repository.grantUpgrades[currentID] = current
		delete(repository.pendingUpgrades, grant.ID)
	}
	digest, _ := application.CLIScopeDigest(normalized)
	upgrade := application.CLIGrantScopeUpgrade{
		ID: pending.ID, GrantID: pending.GrantID, FromRevision: pending.FromRevision,
		RequestedScopes: normalized, RequestedScopeDigest: digest,
		Status:    application.CLIGrantScopeUpgradePending,
		CreatedAt: pending.CreatedAt.UTC(), ExpiresAt: pending.ExpiresAt.UTC(),
	}
	repository.grantUpgrades[upgrade.ID] = upgrade
	repository.pendingUpgrades[grant.ID] = upgrade.ID
	return cloneCLIGrantScopeUpgrade(upgrade), nil
}

func (repository *MemoryProjects) ListCLIGrantScopeUpgrades(
	_ context.Context,
	installationID string,
) ([]application.CLIGrantScopeUpgrade, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	result := make([]application.CLIGrantScopeUpgrade, 0)
	for _, upgrade := range repository.grantUpgrades {
		grant, exists := repository.grants[upgrade.GrantID]
		if exists && grant.InstallationID == installationID {
			result = append(result, cloneCLIGrantScopeUpgrade(upgrade))
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.After(result[right].CreatedAt)
	})
	return result, nil
}

func (repository *MemoryProjects) DecideCLIGrantScopeUpgrade(
	_ context.Context,
	id string,
	approve bool,
	decidedAt time.Time,
) (application.CLIGrantScopeUpgrade, application.CLIGrant, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	upgrade, ok := repository.grantUpgrades[id]
	if !ok {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, application.ErrCLIUpgradeNotFound
	}
	if upgrade.Status != application.CLIGrantScopeUpgradePending || !decidedAt.Before(upgrade.ExpiresAt) {
		if upgrade.Status == application.CLIGrantScopeUpgradePending {
			instant := decidedAt.UTC()
			upgrade.Status = application.CLIGrantScopeUpgradeExpired
			upgrade.DecidedAt = &instant
			repository.grantUpgrades[id] = upgrade
			delete(repository.pendingUpgrades, upgrade.GrantID)
		}
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, application.ErrCLIUpgradeNotPending
	}
	grant, ok := repository.grants[upgrade.GrantID]
	if !ok || grant.Status != application.CLIGrantActive || grant.Revision != upgrade.FromRevision {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, application.ErrCLIUpgradeNotPending
	}
	instant := decidedAt.UTC()
	upgrade.DecidedAt = &instant
	upgrade.Status = application.CLIGrantScopeUpgradeDenied
	if approve {
		upgrade.Status = application.CLIGrantScopeUpgradeApproved
		grant.Scopes = append([]string(nil), upgrade.RequestedScopes...)
		grant.Revision, _ = grant.Revision.Next()
		grant.ScopeDigest = upgrade.RequestedScopeDigest
		repository.grants[grant.ID] = grant
	}
	repository.grantUpgrades[id] = upgrade
	delete(repository.pendingUpgrades, upgrade.GrantID)
	return cloneCLIGrantScopeUpgrade(upgrade), cloneCLIGrant(grant), nil
}

func cloneCLIGrantScopeUpgrade(upgrade application.CLIGrantScopeUpgrade) application.CLIGrantScopeUpgrade {
	upgrade.RequestedScopes = append([]string(nil), upgrade.RequestedScopes...)
	return upgrade
}
