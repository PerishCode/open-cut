package authwire

import (
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	CLIChallengeSchema = "open-cut/cli-challenge/v3"
	CLIRole            = "product-cli"

	CLIChallengeRoute    = "/v1/auth/cli/challenges"
	HeaderGrant          = "X-Open-Cut-CLI-Grant"
	HeaderChallenge      = "X-Open-Cut-CLI-Challenge"
	HeaderSignature      = "X-Open-Cut-CLI-Signature"
	HeaderAuthStatus     = "X-Open-Cut-CLI-Auth-Status"
	HeaderPairingID      = "X-Open-Cut-CLI-Pairing-ID"
	HeaderScopeUpgradeID = "X-Open-Cut-CLI-Scope-Upgrade-ID"

	AuthStatusPairingRequired       = "pairing-required"
	AuthStatusPairingDenied         = "pairing-denied"
	AuthStatusGrantRevoked          = "grant-revoked"
	AuthStatusScopeDenied           = "scope-denied"
	AuthStatusScopeUpgradeRequired  = "scope-upgrade-required"
	AuthStatusGrantAuthorityChanged = "grant-authority-changed"
)

type CLIChallengeRequest struct {
	ClientInstance     string                               `json:"clientInstance" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Command            string                               `json:"command" minLength:"3" maxLength:"128"`
	CommandFingerprint string                               `json:"commandFingerprint" format:"sha256-digest"`
	Method             string                               `json:"method" enum:"GET,POST"`
	Path               string                               `json:"path" minLength:"1" maxLength:"2048"`
	Query              string                               `json:"query" maxLength:"4096"`
	BodyDigest         string                               `json:"bodyDigest" format:"sha256-digest"`
	RequestID          string                               `json:"requestId,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Context            command.Context                      `json:"context"`
	PolicyOverride     application.InvocationPolicyOverride `json:"policyOverride"`
}

type CLIChallengeResult struct {
	Schema                 string                               `json:"schema" enum:"open-cut/cli-challenge/v3"`
	InvocationID           domain.CommandReceiptID              `json:"invocationId" format:"uuid"`
	GrantID                string                               `json:"grantId"`
	GrantRevision          *domain.Revision                     `json:"grantRevision,omitempty"`
	GrantScopeDigest       string                               `json:"grantScopeDigest,omitempty" format:"sha256-digest"`
	Nonce                  string                               `json:"nonce"`
	ExpiresAt              time.Time                            `json:"expiresAt"`
	InstallationID         string                               `json:"installationId"`
	InstallationGeneration uint64                               `json:"installationGeneration" minimum:"1"`
	CellGeneration         uint64                               `json:"cellGeneration"`
	APIInstanceID          string                               `json:"apiInstanceId" format:"uuid"`
	ClientInstance         string                               `json:"clientInstance"`
	Command                string                               `json:"command"`
	CommandFingerprint     string                               `json:"commandFingerprint" format:"sha256-digest"`
	RequiredScope          string                               `json:"requiredScope"`
	Method                 string                               `json:"method"`
	Path                   string                               `json:"path"`
	Query                  string                               `json:"query"`
	BodyDigest             string                               `json:"bodyDigest" format:"sha256-digest"`
	InputDigest            domain.Digest                        `json:"inputDigest" format:"sha256-digest"`
	RequestID              string                               `json:"requestId,omitempty"`
	Receipt                application.CommandReceiptClass      `json:"receipt" enum:"none,evidence,outcome"`
	Context                command.Context                      `json:"context"`
	Policy                 application.InvocationPolicySnapshot `json:"policy"`
	Role                   string                               `json:"role" enum:"product-cli"`
	SigningPayload         string                               `json:"signingPayload" doc:"Base64url canonical challenge bytes"`
}
