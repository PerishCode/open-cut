package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrCLIGrantNotFound     = errors.New("CLI grant was not found")
	ErrCLIGrantNotPending   = errors.New("CLI grant is not pending")
	ErrCLIGrantNotActive    = errors.New("CLI grant is not active")
	ErrCLIUpgradeNotFound   = errors.New("CLI scope upgrade was not found")
	ErrCLIUpgradeNotPending = errors.New("CLI scope upgrade is not pending")
	ErrCLIUpgradeInvalid    = errors.New("CLI scope upgrade is invalid")
)

var cliScopePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$`)

type CLIGrantStatus string

const (
	CLIGrantPending CLIGrantStatus = "pending"
	CLIGrantActive  CLIGrantStatus = "active"
	CLIGrantDenied  CLIGrantStatus = "denied"
	CLIGrantRevoked CLIGrantStatus = "revoked"
	CLIGrantExpired CLIGrantStatus = "expired"
)

type CLIGrant struct {
	ID                   string          `json:"id" format:"uuid"`
	InstallationID       string          `json:"installationId"`
	AgentID              domain.AgentID  `json:"agentId" format:"uuid"`
	PublicKey            string          `json:"-"`
	PublicKeyFingerprint string          `json:"publicKeyFingerprint" format:"sha256-digest"`
	Scopes               []string        `json:"scopes" maxItems:"64" nullable:"false"`
	Revision             domain.Revision `json:"revision" minimum:"1"`
	ScopeDigest          domain.Digest   `json:"scopeDigest" format:"sha256-digest"`
	Status               CLIGrantStatus  `json:"status" enum:"pending,active,denied,revoked,expired"`
	CreatedAt            time.Time       `json:"createdAt"`
	ExpiresAt            time.Time       `json:"expiresAt"`
	DecidedAt            *time.Time      `json:"decidedAt,omitempty"`
	RevokedAt            *time.Time      `json:"revokedAt,omitempty"`
}

type CLIGrantScopeUpgradeStatus string

const (
	CLIGrantScopeUpgradePending    CLIGrantScopeUpgradeStatus = "pending"
	CLIGrantScopeUpgradeApproved   CLIGrantScopeUpgradeStatus = "approved"
	CLIGrantScopeUpgradeDenied     CLIGrantScopeUpgradeStatus = "denied"
	CLIGrantScopeUpgradeExpired    CLIGrantScopeUpgradeStatus = "expired"
	CLIGrantScopeUpgradeSuperseded CLIGrantScopeUpgradeStatus = "superseded"
)

type CLIGrantScopeUpgrade struct {
	ID                   string                     `json:"id" format:"uuid"`
	GrantID              string                     `json:"grantId" format:"uuid"`
	FromRevision         domain.Revision            `json:"fromRevision" minimum:"1"`
	RequestedScopes      []string                   `json:"requestedScopes" maxItems:"64" nullable:"false"`
	RequestedScopeDigest domain.Digest              `json:"requestedScopeDigest" format:"sha256-digest"`
	Status               CLIGrantScopeUpgradeStatus `json:"status" enum:"pending,approved,denied,expired,superseded"`
	CreatedAt            time.Time                  `json:"createdAt"`
	ExpiresAt            time.Time                  `json:"expiresAt"`
	DecidedAt            *time.Time                 `json:"decidedAt,omitempty"`
}

type PendingCLIGrantScopeUpgrade struct {
	ID              string
	GrantID         string
	FromRevision    domain.Revision
	RequestedScopes []string
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type PendingCLIGrant struct {
	ID             string
	InstallationID string
	AgentID        domain.AgentID
	PublicKey      string
	Fingerprint    string
	Scopes         []string
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

type AuthorizationAudit struct {
	ID             string
	InstallationID string
	PrincipalKind  AuthoritySurface
	PrincipalID    string
	Action         string
	Outcome        string
	RequestDigest  string
	OccurredAt     time.Time
}

type AuthorizationRepository interface {
	EnsureLocalCreator(context.Context, domain.CreatorID, time.Time) (domain.CreatorID, error)
	LoadCLIInvocationPolicy(context.Context) (InvocationPolicySettings, error)
	AppendAuthorizationAudit(context.Context, AuthorizationAudit) error
	FindCLIGrant(context.Context, string, string) (CLIGrant, error)
	EnsurePendingCLIGrant(context.Context, PendingCLIGrant) (CLIGrant, error)
	ListCLIGrants(context.Context, string) ([]CLIGrant, error)
	DecideCLIGrant(context.Context, string, bool, time.Time) (CLIGrant, error)
	RevokeCLIGrant(context.Context, string, time.Time) (CLIGrant, error)
	EnsurePendingCLIGrantScopeUpgrade(context.Context, PendingCLIGrantScopeUpgrade) (CLIGrantScopeUpgrade, error)
	ListCLIGrantScopeUpgrades(context.Context, string) ([]CLIGrantScopeUpgrade, error)
	DecideCLIGrantScopeUpgrade(context.Context, string, bool, time.Time) (CLIGrantScopeUpgrade, CLIGrant, error)
}

func NormalizeCLIScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 || len(scopes) > 64 {
		return nil, ErrCLIUpgradeInvalid
	}
	result := append([]string(nil), scopes...)
	sort.Strings(result)
	write := 0
	for _, scope := range result {
		if !cliScopePattern.MatchString(scope) {
			return nil, ErrCLIUpgradeInvalid
		}
		if write > 0 && result[write-1] == scope {
			continue
		}
		result[write] = scope
		write++
	}
	return result[:write], nil
}

func CLIScopeDigest(scopes []string) (domain.Digest, error) {
	normalized, err := NormalizeCLIScopes(scopes)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		Domain  string `json:"domain"`
		Payload struct {
			Scopes []string `json:"scopes"`
		} `json:"payload"`
		Schema string `json:"schema"`
	}{
		Domain: "open-cut/cli-grant-scopes",
		Payload: struct {
			Scopes []string `json:"scopes"`
		}{Scopes: normalized},
		Schema: "open-cut/cli-grant-scopes/v1",
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return domain.Digest("sha256:" + hex.EncodeToString(digest[:])), nil
}
