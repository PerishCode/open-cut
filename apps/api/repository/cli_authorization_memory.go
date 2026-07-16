package repository

import (
	"context"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *MemoryProjects) FindCLIGrant(
	_ context.Context,
	installationID, publicKey string,
) (application.CLIGrant, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	id := repository.grantKeys[installationID+"\x00"+publicKey]
	grant, ok := repository.grants[id]
	if !ok {
		return application.CLIGrant{}, application.ErrCLIGrantNotFound
	}
	return cloneCLIGrant(grant), nil
}

func (repository *MemoryProjects) EnsurePendingCLIGrant(
	_ context.Context,
	pending application.PendingCLIGrant,
) (application.CLIGrant, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := pending.InstallationID + "\x00" + pending.PublicKey
	if id := repository.grantKeys[key]; id != "" {
		grant := repository.grants[id]
		if grant.Status == application.CLIGrantExpired ||
			(grant.Status == application.CLIGrantPending && !pending.CreatedAt.Before(grant.ExpiresAt)) {
			grant.Status = application.CLIGrantPending
			grant.Scopes, _ = application.NormalizeCLIScopes(pending.Scopes)
			grant.Revision, _ = grant.Revision.Next()
			grant.ScopeDigest, _ = application.CLIScopeDigest(grant.Scopes)
			grant.CreatedAt = pending.CreatedAt.UTC()
			grant.ExpiresAt = pending.ExpiresAt.UTC()
			grant.DecidedAt = nil
			grant.RevokedAt = nil
			repository.grants[id] = grant
		}
		return cloneCLIGrant(grant), nil
	}
	normalized, err := application.NormalizeCLIScopes(pending.Scopes)
	if err != nil {
		return application.CLIGrant{}, err
	}
	revision, _ := domain.NewRevision(1)
	digest, _ := application.CLIScopeDigest(normalized)
	grant := application.CLIGrant{
		ID: pending.ID, InstallationID: pending.InstallationID, AgentID: pending.AgentID,
		PublicKey: pending.PublicKey, PublicKeyFingerprint: pending.Fingerprint,
		Scopes: normalized, Revision: revision, ScopeDigest: digest, Status: application.CLIGrantPending,
		CreatedAt: pending.CreatedAt.UTC(), ExpiresAt: pending.ExpiresAt.UTC(),
	}
	repository.grantKeys[key] = grant.ID
	repository.grants[grant.ID] = grant
	return cloneCLIGrant(grant), nil
}

func (repository *MemoryProjects) ListCLIGrants(
	_ context.Context,
	installationID string,
) ([]application.CLIGrant, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	result := make([]application.CLIGrant, 0)
	for _, grant := range repository.grants {
		if grant.InstallationID == installationID {
			result = append(result, cloneCLIGrant(grant))
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

func (repository *MemoryProjects) DecideCLIGrant(
	_ context.Context,
	id string,
	approve bool,
	decidedAt time.Time,
) (application.CLIGrant, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	grant, ok := repository.grants[id]
	if !ok {
		return application.CLIGrant{}, application.ErrCLIGrantNotFound
	}
	if grant.Status != application.CLIGrantPending || !decidedAt.Before(grant.ExpiresAt) {
		if grant.Status == application.CLIGrantPending {
			grant.Status = application.CLIGrantExpired
			repository.grants[id] = grant
		}
		return application.CLIGrant{}, application.ErrCLIGrantNotPending
	}
	grant.Status = application.CLIGrantDenied
	if approve {
		grant.Status = application.CLIGrantActive
	}
	instant := decidedAt.UTC()
	grant.DecidedAt = &instant
	repository.grants[id] = grant
	return cloneCLIGrant(grant), nil
}

func (repository *MemoryProjects) RevokeCLIGrant(
	_ context.Context,
	id string,
	revokedAt time.Time,
) (application.CLIGrant, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	grant, ok := repository.grants[id]
	if !ok {
		return application.CLIGrant{}, application.ErrCLIGrantNotFound
	}
	if grant.Status != application.CLIGrantActive {
		return application.CLIGrant{}, application.ErrCLIGrantNotActive
	}
	instant := revokedAt.UTC()
	grant.Status = application.CLIGrantRevoked
	grant.RevokedAt = &instant
	repository.grants[id] = grant
	if upgradeID := repository.pendingUpgrades[id]; upgradeID != "" {
		upgrade := repository.grantUpgrades[upgradeID]
		upgrade.Status = application.CLIGrantScopeUpgradeExpired
		upgrade.DecidedAt = &instant
		repository.grantUpgrades[upgradeID] = upgrade
		delete(repository.pendingUpgrades, id)
	}
	return cloneCLIGrant(grant), nil
}

func cloneCLIGrant(grant application.CLIGrant) application.CLIGrant {
	grant.Scopes = append([]string(nil), grant.Scopes...)
	return grant
}
