package service

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/authwire"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	CLIChallengeTTL    = 30 * time.Second
	CLIPairingTTL      = 10 * time.Minute
	CLIScopeUpgradeTTL = 10 * time.Minute
	cliChallengeLimit  = 256
)

const (
	CLIChallengeSchema = authwire.CLIChallengeSchema
	CLIRole            = authwire.CLIRole
)

func NoBodyDigest(commandName string) string { return authwire.NoBodyDigest(commandName) }

var (
	ErrCLIChallengeInvalid      = errors.New("CLI challenge is invalid")
	ErrCLIChallengeExpired      = errors.New("CLI challenge expired")
	ErrCLIRateLimited           = errors.New("CLI challenge is rate limited")
	ErrCLIGrantDenied           = errors.New("CLI grant was denied")
	ErrCLIGrantRevoked          = errors.New("CLI grant was revoked")
	ErrCLIGrantScopeDenied      = errors.New("CLI grant scope is denied")
	ErrCLIGrantAuthorityChanged = errors.New("CLI grant authority changed")
)

type CLIPairingRequiredError struct {
	Grant application.CLIGrant
}

func (err *CLIPairingRequiredError) Error() string { return "CLI pairing is required" }

type CLIScopeUpgradeRequiredError struct {
	Upgrade application.CLIGrantScopeUpgrade
}

func (err *CLIScopeUpgradeRequiredError) Error() string { return "CLI scope upgrade is required" }

type CLIChallengeConfig struct {
	InstallationID         string
	InstallationGeneration uint64
	CellGeneration         uint64
	PublicKey              ed25519.PublicKey
}

type CLIChallengeRequest = authwire.CLIChallengeRequest
type CLIChallengeResult = authwire.CLIChallengeResult

type CLIAuthorizationBootstrap interface {
	ChallengeCLI(context.Context, CLIChallengeRequest) (CLIChallengeResult, error)
	ListCLIGrants(context.Context) ([]application.CLIGrant, error)
	ListCLIGrantScopeUpgrades(context.Context) ([]application.CLIGrantScopeUpgrade, error)
	DecideCLIGrant(context.Context, string, bool) (application.CLIGrant, error)
	DecideCLIGrantScopeUpgrade(context.Context, string, bool) (application.CLIGrantScopeUpgrade, application.CLIGrant, error)
	RevokeCLIGrant(context.Context, string) (application.CLIGrant, error)
}

type cliChallenge struct {
	result    CLIChallengeResult
	canonical []byte
}

type CLIAuthorizationService struct {
	mu          sync.Mutex
	config      CLIChallengeConfig
	repository  application.AuthorizationRepository
	runBinder   application.AgentRunBindingRepository
	identities  application.IdentityGenerator
	clock       application.Clock
	random      io.Reader
	registry    *command.Registry
	apiInstance string
	publicKey   string
	fingerprint string
	allScopes   []string
	challenges  map[string]cliChallenge
	lastIssued  map[string]time.Time
}

func NewCLIAuthorizationService(
	ctx context.Context,
	config CLIChallengeConfig,
	repository application.AuthorizationRepository,
	runBinder application.AgentRunBindingRepository,
	identities application.IdentityGenerator,
	clock application.Clock,
	random io.Reader,
) (*CLIAuthorizationService, error) {
	if repository == nil || identities == nil || clock == nil || random == nil {
		return nil, fmt.Errorf("CLI authorization dependencies are required")
	}
	if _, err := domain.ParseRequestID(config.InstallationID); err != nil ||
		config.InstallationGeneration < 1 || len(config.PublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("CLI installation trust is invalid")
	}
	instance, err := identities.NewID(ctx, clock.Now().UTC())
	if err != nil {
		return nil, err
	}
	if _, err := domain.ParseActivityEventID(instance); err != nil {
		return nil, err
	}
	registry := command.InitialRegistry()
	scopes := registry.AgentScopes()
	allScopes := make([]string, len(scopes))
	for index, scope := range scopes {
		allScopes[index] = string(scope)
	}
	digest := sha256.Sum256(config.PublicKey)
	return &CLIAuthorizationService{
		config: config, repository: repository, identities: identities, clock: clock, random: random,
		runBinder: runBinder,
		registry:  registry, apiInstance: instance,
		publicKey:   base64.StdEncoding.EncodeToString(config.PublicKey),
		fingerprint: "sha256:" + hex.EncodeToString(digest[:]), allScopes: allScopes,
		challenges: make(map[string]cliChallenge), lastIssued: make(map[string]time.Time),
	}, nil
}

func (service *CLIAuthorizationService) ChallengeCLI(
	ctx context.Context,
	request CLIChallengeRequest,
) (CLIChallengeResult, error) {
	if _, err := domain.ParseRequestID(request.ClientInstance); err != nil {
		return CLIChallengeResult{}, ErrCLIChallengeInvalid
	}
	path := strings.Fields(request.Command)
	if len(path) != 2 || strings.Join(path, " ") != request.Command {
		return CLIChallengeResult{}, ErrCLIChallengeInvalid
	}
	descriptor, err := service.registry.Lookup(path)
	if err != nil {
		return CLIChallengeResult{}, ErrCLIChallengeInvalid
	}
	fingerprint, err := service.registry.Fingerprint(path)
	expectedMethod := http.MethodPost
	// Business mutability and HTTP transport method are independent: the pure
	// rough-cut preview carries structured input and therefore uses POST.
	if descriptor.Mutability == command.ReadOnly && request.Command != "edit derive-rough-cut" {
		expectedMethod = http.MethodGet
	}
	bodyDigestValid := false
	if _, digestErr := domain.ParseDigest(request.BodyDigest); digestErr == nil {
		bodyDigestValid = expectedMethod == http.MethodPost || request.BodyDigest == authwire.NoBodyDigest(request.Command)
	}
	requestIdentityValid := request.RequestID == ""
	if descriptor.RequestIdentity {
		_, requestIdentityErr := domain.ParseRequestID(request.RequestID)
		requestIdentityValid = requestIdentityErr == nil
	}
	if err != nil || fingerprint != request.CommandFingerprint || !bodyDigestValid || !requestIdentityValid ||
		descriptor.RequestIdentity != (request.RequestID != "") || request.Method != expectedMethod ||
		!validCLIHTTPBinding(path, request.Path) || !validCommandContext(descriptor, request.Context, request.Path) {
		return CLIChallengeResult{}, ErrCLIChallengeInvalid
	}
	query, err := canonicalQuery(request.Query)
	if err != nil || query != request.Query {
		return CLIChallengeResult{}, ErrCLIChallengeInvalid
	}
	settings, err := service.repository.LoadCLIInvocationPolicy(ctx)
	if err != nil {
		return CLIChallengeResult{}, err
	}
	policy, err := application.NewInvocationPolicySnapshot(settings, request.PolicyOverride)
	if err != nil {
		return CLIChallengeResult{}, ErrCLIChallengeInvalid
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	if previous := service.lastIssued[request.ClientInstance]; !previous.IsZero() && now.Sub(previous) < 250*time.Millisecond {
		return CLIChallengeResult{}, ErrCLIRateLimited
	}
	if len(service.challenges) >= cliChallengeLimit {
		return CLIChallengeResult{}, ErrCLIRateLimited
	}
	nonce, err := randomToken(service.random, "")
	if err != nil {
		return CLIChallengeResult{}, err
	}
	invocationValue, err := service.identities.NewID(ctx, now)
	if err != nil {
		return CLIChallengeResult{}, err
	}
	invocationID, err := domain.ParseCommandReceiptID(invocationValue)
	if err != nil {
		return CLIChallengeResult{}, err
	}
	inputDigest, err := cliInvocationDigest(request, query)
	if err != nil {
		return CLIChallengeResult{}, err
	}
	grantID := ""
	var grantRevision *domain.Revision
	grantScopeDigest := ""
	grant, err := service.repository.FindCLIGrant(ctx, service.config.InstallationID, service.publicKey)
	if err == nil {
		grantID = grant.ID
		revision := grant.Revision
		grantRevision = &revision
		grantScopeDigest = grant.ScopeDigest.String()
	} else if !errors.Is(err, application.ErrCLIGrantNotFound) {
		return CLIChallengeResult{}, err
	}
	result := CLIChallengeResult{
		Schema: CLIChallengeSchema, InvocationID: invocationID, GrantID: grantID, GrantRevision: grantRevision,
		GrantScopeDigest: grantScopeDigest, Nonce: nonce, ExpiresAt: now.Add(CLIChallengeTTL),
		InstallationID: service.config.InstallationID, InstallationGeneration: service.config.InstallationGeneration,
		CellGeneration: service.config.CellGeneration, APIInstanceID: service.apiInstance,
		ClientInstance: request.ClientInstance, Command: request.Command,
		CommandFingerprint: request.CommandFingerprint, RequiredScope: string(descriptor.RequiredScope),
		Method: request.Method, Path: request.Path, Query: query, BodyDigest: request.BodyDigest,
		InputDigest: inputDigest, RequestID: request.RequestID, Receipt: descriptor.Receipt,
		Context: request.Context, Policy: policy, Role: CLIRole,
	}
	canonical, err := canonicalCLIChallenge(result)
	if err != nil {
		return CLIChallengeResult{}, err
	}
	result.SigningPayload = base64.RawURLEncoding.EncodeToString(canonical)
	service.challenges[nonce] = cliChallenge{result: result, canonical: canonical}
	service.lastIssued[request.ClientInstance] = now
	return result, nil
}

func (service *CLIAuthorizationService) Authorize(
	ctx context.Context,
	request AuthorizationRequest,
) (application.Authority, error) {
	if request.UISession != "" || request.CLIChallenge == "" || request.CLISignature == "" ||
		request.Command == "" || request.CommandFingerprint == "" || request.RequiredScope == "" {
		return application.Authority{}, ErrUnauthorized
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	challenge, exists := service.challenges[request.CLIChallenge]
	if exists {
		delete(service.challenges, request.CLIChallenge)
	}
	service.cleanupLocked(now)
	service.mu.Unlock()
	if !exists {
		return application.Authority{}, ErrCLIChallengeInvalid
	}
	if !now.Before(challenge.result.ExpiresAt) {
		return application.Authority{}, ErrCLIChallengeExpired
	}
	query, err := canonicalQuery(request.Query)
	if err != nil || query != challenge.result.Query || request.Method != challenge.result.Method ||
		request.Path != challenge.result.Path || request.Command != challenge.result.Command ||
		request.CommandFingerprint != challenge.result.CommandFingerprint ||
		request.RequiredScope != challenge.result.RequiredScope || request.BodyDigest != challenge.result.BodyDigest ||
		request.CLIGrant != challenge.result.GrantID {
		return application.Authority{}, ErrUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(request.CLISignature)
	if err != nil || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(service.config.PublicKey, challenge.canonical, signature) {
		_ = service.audit(ctx, "unpaired", "cli-command:"+request.Command, "denied", digestBytes(challenge.canonical))
		return application.Authority{}, ErrUnauthorized
	}
	grant, err := service.repository.FindCLIGrant(ctx, service.config.InstallationID, service.publicKey)
	createdGrant := false
	if errors.Is(err, application.ErrCLIGrantNotFound) {
		grant, err = service.createPendingGrant(ctx, now)
		createdGrant = err == nil
	}
	if err != nil {
		return application.Authority{}, err
	}
	if request.CLIGrant != "" && request.CLIGrant != grant.ID {
		return application.Authority{}, ErrUnauthorized
	}
	if grant.Status == application.CLIGrantPending && !now.Before(grant.ExpiresAt) {
		grant, err = service.createPendingGrant(ctx, now)
		if err != nil {
			return application.Authority{}, err
		}
	}
	if challenge.result.GrantID == "" {
		if !createdGrant {
			return application.Authority{}, ErrCLIGrantAuthorityChanged
		}
	} else if challenge.result.GrantRevision == nil ||
		grant.Revision != *challenge.result.GrantRevision ||
		grant.ScopeDigest.String() != challenge.result.GrantScopeDigest {
		return application.Authority{}, ErrCLIGrantAuthorityChanged
	}
	switch grant.Status {
	case application.CLIGrantPending:
		if err := service.associateAgentRunPairing(ctx, challenge.result.Context, grant, now); err != nil {
			return application.Authority{}, err
		}
		_ = service.audit(ctx, grant.ID, "cli-command:"+request.Command, "pairing-required", digestBytes(challenge.canonical))
		return application.Authority{}, &CLIPairingRequiredError{Grant: grant}
	case application.CLIGrantDenied:
		return application.Authority{}, ErrCLIGrantDenied
	case application.CLIGrantRevoked:
		return application.Authority{}, ErrCLIGrantRevoked
	case application.CLIGrantExpired:
		return application.Authority{}, &CLIPairingRequiredError{Grant: grant}
	case application.CLIGrantActive:
		if !slices.Contains(grant.Scopes, request.RequiredScope) {
			upgrade, createErr := service.createPendingScopeUpgrade(ctx, grant, request.RequiredScope, now)
			if createErr != nil {
				return application.Authority{}, createErr
			}
			_ = service.audit(ctx, grant.ID, "cli-scope-upgrade:"+request.Command, "scope-upgrade-required", digestBytes(challenge.canonical))
			return application.Authority{}, &CLIScopeUpgradeRequiredError{Upgrade: upgrade}
		}
	default:
		return application.Authority{}, ErrUnauthorized
	}
	if err := service.bindAuthorizedAgentRun(ctx, challenge.result.Context, grant, now); err != nil {
		return application.Authority{}, err
	}
	if err := service.audit(
		ctx, grant.ID, "cli-command:"+request.Command, "authorized", digestBytes(challenge.canonical),
	); err != nil {
		return application.Authority{}, ErrUnauthorized
	}
	return application.Authority{
		Surface: application.AuthorityProductCLI, InstallationID: service.config.InstallationID,
		GrantID: grant.ID, Actor: domain.AgentActor(grant.AgentID), Policy: challenge.result.Policy.Effective,
		Invocation: &application.CommandInvocation{
			ID: challenge.result.InvocationID, Command: challenge.result.Command,
			Fingerprint: domain.Digest(challenge.result.CommandFingerprint), Class: challenge.result.Receipt,
			InputDigest: challenge.result.InputDigest, Context: challenge.result.Context,
			RequestID: optionalRequestID(challenge.result.RequestID),
		},
	}, nil
}

func (service *CLIAuthorizationService) associateAgentRunPairing(
	ctx context.Context,
	commandContext command.Context,
	grant application.CLIGrant,
	at time.Time,
) error {
	if service.runBinder == nil || commandContext.RunID == nil || commandContext.TurnID == nil {
		return nil
	}
	err := service.runBinder.AssociateAgentRunPairing(
		ctx, *commandContext.RunID, *commandContext.TurnID, grant.ID, grant.Revision, at,
	)
	if errors.Is(err, application.ErrAgentBridgeNotFound) {
		return nil
	}
	return err
}

func (service *CLIAuthorizationService) bindAuthorizedAgentRun(
	ctx context.Context,
	commandContext command.Context,
	grant application.CLIGrant,
	at time.Time,
) error {
	if service.runBinder == nil || commandContext.RunID == nil || commandContext.TurnID == nil {
		return nil
	}
	value, err := service.identities.NewID(ctx, at)
	if err != nil {
		return err
	}
	eventID, err := domain.ParseActivityEventID(value)
	if err != nil {
		return err
	}
	err = service.runBinder.BindAuthorizedAgentRun(
		ctx, *commandContext.RunID, *commandContext.TurnID, grant.AgentID, grant.ID, grant.Revision, eventID, at,
	)
	if errors.Is(err, application.ErrAgentBridgeNotFound) {
		return nil
	}
	return err
}

func (service *CLIAuthorizationService) ListCLIGrants(ctx context.Context) ([]application.CLIGrant, error) {
	if err := requireCreatorAuthority(ctx); err != nil {
		return nil, err
	}
	grants, err := service.repository.ListCLIGrants(ctx, service.config.InstallationID)
	if err != nil {
		return nil, err
	}
	now := service.clock.Now().UTC()
	for index := range grants {
		if grants[index].Status == application.CLIGrantPending && !now.Before(grants[index].ExpiresAt) {
			grants[index].Status = application.CLIGrantExpired
		}
	}
	return grants, nil
}

func (service *CLIAuthorizationService) ListCLIGrantScopeUpgrades(
	ctx context.Context,
) ([]application.CLIGrantScopeUpgrade, error) {
	if err := requireCreatorAuthority(ctx); err != nil {
		return nil, err
	}
	upgrades, err := service.repository.ListCLIGrantScopeUpgrades(ctx, service.config.InstallationID)
	if err != nil {
		return nil, err
	}
	now := service.clock.Now().UTC()
	for index := range upgrades {
		if upgrades[index].Status == application.CLIGrantScopeUpgradePending &&
			!now.Before(upgrades[index].ExpiresAt) {
			upgrades[index].Status = application.CLIGrantScopeUpgradeExpired
		}
	}
	return upgrades, nil
}

func (service *CLIAuthorizationService) DecideCLIGrant(
	ctx context.Context,
	id string,
	approve bool,
) (application.CLIGrant, error) {
	authority, err := application.AuthorityFromContext(ctx)
	if err != nil || authority.Surface != application.AuthorityFirstPartyUI {
		return application.CLIGrant{}, ErrUnauthorized
	}
	grant, err := service.repository.DecideCLIGrant(ctx, id, approve, service.clock.Now().UTC())
	if err != nil {
		return application.CLIGrant{}, err
	}
	outcome := "denied"
	if approve {
		outcome = "approved"
	}
	if err := service.audit(ctx, authority.Actor.IDString(), "cli-pairing.decide:"+id, outcome, ""); err != nil {
		return application.CLIGrant{}, err
	}
	return grant, nil
}

func (service *CLIAuthorizationService) RevokeCLIGrant(
	ctx context.Context,
	id string,
) (application.CLIGrant, error) {
	authority, err := application.AuthorityFromContext(ctx)
	if err != nil || authority.Surface != application.AuthorityFirstPartyUI {
		return application.CLIGrant{}, ErrUnauthorized
	}
	grant, err := service.repository.RevokeCLIGrant(ctx, id, service.clock.Now().UTC())
	if err != nil {
		return application.CLIGrant{}, err
	}
	if err := service.audit(ctx, authority.Actor.IDString(), "cli-pairing.revoke:"+id, "revoked", ""); err != nil {
		return application.CLIGrant{}, err
	}
	return grant, nil
}

func (service *CLIAuthorizationService) DecideCLIGrantScopeUpgrade(
	ctx context.Context,
	id string,
	approve bool,
) (application.CLIGrantScopeUpgrade, application.CLIGrant, error) {
	authority, err := application.AuthorityFromContext(ctx)
	if err != nil || authority.Surface != application.AuthorityFirstPartyUI {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, ErrUnauthorized
	}
	upgrade, grant, err := service.repository.DecideCLIGrantScopeUpgrade(
		ctx, id, approve, service.clock.Now().UTC(),
	)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, err
	}
	outcome := "denied"
	if approve {
		outcome = "approved"
	}
	if err := service.audit(ctx, authority.Actor.IDString(), "cli-scope-upgrade.decide:"+id, outcome, upgrade.RequestedScopeDigest.String()); err != nil {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, err
	}
	return upgrade, grant, nil
}

func (service *CLIAuthorizationService) createPendingGrant(
	ctx context.Context,
	now time.Time,
) (application.CLIGrant, error) {
	grantValue, err := service.identities.NewID(ctx, now)
	if err != nil {
		return application.CLIGrant{}, err
	}
	agentValue, err := service.identities.NewID(ctx, now)
	if err != nil {
		return application.CLIGrant{}, err
	}
	agent, err := domain.ParseAgentID(agentValue)
	if err != nil {
		return application.CLIGrant{}, err
	}
	return service.repository.EnsurePendingCLIGrant(ctx, application.PendingCLIGrant{
		ID: grantValue, InstallationID: service.config.InstallationID, AgentID: agent,
		PublicKey: service.publicKey, Fingerprint: service.fingerprint,
		Scopes: service.allScopes, CreatedAt: now, ExpiresAt: now.Add(CLIPairingTTL),
	})
}

func (service *CLIAuthorizationService) createPendingScopeUpgrade(
	ctx context.Context,
	grant application.CLIGrant,
	requiredScope string,
	now time.Time,
) (application.CLIGrantScopeUpgrade, error) {
	value, err := service.identities.NewID(ctx, now)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	requested := append(append([]string(nil), grant.Scopes...), requiredScope)
	normalized, err := application.NormalizeCLIScopes(requested)
	if err != nil {
		return application.CLIGrantScopeUpgrade{}, err
	}
	return service.repository.EnsurePendingCLIGrantScopeUpgrade(ctx, application.PendingCLIGrantScopeUpgrade{
		ID: value, GrantID: grant.ID, FromRevision: grant.Revision,
		RequestedScopes: normalized, CreatedAt: now, ExpiresAt: now.Add(CLIScopeUpgradeTTL),
	})
}

func (service *CLIAuthorizationService) audit(
	ctx context.Context,
	principalID, action, outcome, requestDigest string,
) error {
	now := service.clock.Now().UTC()
	value, err := service.identities.NewID(ctx, now)
	if err != nil {
		return err
	}
	return service.repository.AppendAuthorizationAudit(ctx, application.AuthorizationAudit{
		ID: value, InstallationID: service.config.InstallationID,
		PrincipalKind: application.AuthorityProductCLI, PrincipalID: principalID,
		Action: action, Outcome: outcome, RequestDigest: requestDigest, OccurredAt: now,
	})
}

func (service *CLIAuthorizationService) cleanupLocked(now time.Time) {
	for nonce, challenge := range service.challenges {
		if !now.Before(challenge.result.ExpiresAt) {
			delete(service.challenges, nonce)
		}
	}
}

func canonicalCLIChallenge(challenge CLIChallengeResult) ([]byte, error) {
	return json.Marshal(struct {
		APIInstanceID          string                               `json:"apiInstanceId"`
		InvocationID           domain.CommandReceiptID              `json:"invocationId"`
		BodyDigest             string                               `json:"bodyDigest"`
		InputDigest            domain.Digest                        `json:"inputDigest"`
		CellGeneration         uint64                               `json:"cellGeneration"`
		ClientInstance         string                               `json:"clientInstance"`
		Command                string                               `json:"command"`
		CommandFingerprint     string                               `json:"commandFingerprint"`
		Context                command.Context                      `json:"context"`
		ExpiresAt              string                               `json:"expiresAt"`
		GrantID                string                               `json:"grantId"`
		GrantRevision          *domain.Revision                     `json:"grantRevision,omitempty"`
		GrantScopeDigest       string                               `json:"grantScopeDigest,omitempty"`
		InstallationGeneration uint64                               `json:"installationGeneration"`
		InstallationID         string                               `json:"installationId"`
		Method                 string                               `json:"method"`
		Nonce                  string                               `json:"nonce"`
		Path                   string                               `json:"path"`
		Policy                 application.InvocationPolicySnapshot `json:"policy"`
		Receipt                application.CommandReceiptClass      `json:"receipt"`
		RequestID              string                               `json:"requestId,omitempty"`
		Query                  string                               `json:"query"`
		RequiredScope          string                               `json:"requiredScope"`
		Role                   string                               `json:"role"`
		Schema                 string                               `json:"schema"`
	}{
		APIInstanceID: challenge.APIInstanceID, InvocationID: challenge.InvocationID,
		BodyDigest: challenge.BodyDigest, InputDigest: challenge.InputDigest,
		CellGeneration: challenge.CellGeneration, ClientInstance: challenge.ClientInstance,
		Command: challenge.Command, CommandFingerprint: challenge.CommandFingerprint, Context: challenge.Context,
		ExpiresAt: challenge.ExpiresAt.Format(time.RFC3339Nano), GrantID: challenge.GrantID,
		GrantRevision: challenge.GrantRevision, GrantScopeDigest: challenge.GrantScopeDigest,
		InstallationGeneration: challenge.InstallationGeneration, InstallationID: challenge.InstallationID,
		Method: challenge.Method, Nonce: challenge.Nonce, Path: challenge.Path, Query: challenge.Query,
		Policy: challenge.Policy, Receipt: challenge.Receipt, RequestID: challenge.RequestID,
		RequiredScope: challenge.RequiredScope, Role: challenge.Role, Schema: challenge.Schema,
	})
}

func cliInvocationDigest(request CLIChallengeRequest, query string) (domain.Digest, error) {
	canonical, err := json.Marshal(struct {
		BodyDigest         string          `json:"bodyDigest"`
		Command            string          `json:"command"`
		CommandFingerprint string          `json:"commandFingerprint"`
		Context            command.Context `json:"context"`
		Method             string          `json:"method"`
		Path               string          `json:"path"`
		Query              string          `json:"query"`
		RequestID          string          `json:"requestId,omitempty"`
	}{
		BodyDigest: request.BodyDigest, Command: request.Command, CommandFingerprint: request.CommandFingerprint,
		Context: request.Context, Method: request.Method, Path: request.Path, Query: query, RequestID: request.RequestID,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return domain.ParseDigest("sha256:" + hex.EncodeToString(digest[:]))
}

func optionalRequestID(value string) *domain.RequestID {
	if value == "" {
		return nil
	}
	parsed, err := domain.ParseRequestID(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func canonicalQuery(raw string) (string, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", err
	}
	return values.Encode(), nil
}

func requireCreatorAuthority(ctx context.Context) error {
	authority, err := application.AuthorityFromContext(ctx)
	if err != nil || authority.Surface != application.AuthorityFirstPartyUI {
		return ErrUnauthorized
	}
	return nil
}

type CombinedAuthorizer struct {
	UI  *UISessionService
	CLI *CLIAuthorizationService
}

func (authorizer CombinedAuthorizer) BindUISession(
	ctx context.Context,
	token string,
) (context.Context, error) {
	if authorizer.UI == nil {
		return nil, ErrUnauthorized
	}
	return authorizer.UI.BindUISession(ctx, token)
}

func (authorizer CombinedAuthorizer) Authorize(
	ctx context.Context,
	request AuthorizationRequest,
) (application.Authority, error) {
	if request.UISession != "" {
		if authorizer.UI == nil {
			return application.Authority{}, ErrUnauthorized
		}
		return authorizer.UI.Authorize(ctx, request)
	}
	if authorizer.CLI == nil {
		return application.Authority{}, ErrUnauthorized
	}
	return authorizer.CLI.Authorize(ctx, request)
}

func (authorizer CombinedAuthorizer) ChallengeUI(
	ctx context.Context,
	request UIChallengeRequest,
) (UIChallengeResult, error) {
	if authorizer.UI == nil {
		return UIChallengeResult{}, ErrUnauthorized
	}
	return authorizer.UI.ChallengeUI(ctx, request)
}

func (authorizer CombinedAuthorizer) ExchangeUI(
	ctx context.Context,
	request UISessionRequest,
) (UISessionResult, error) {
	if authorizer.UI == nil {
		return UISessionResult{}, ErrUnauthorized
	}
	return authorizer.UI.ExchangeUI(ctx, request)
}

func (authorizer CombinedAuthorizer) ChallengeCLI(
	ctx context.Context,
	request CLIChallengeRequest,
) (CLIChallengeResult, error) {
	if authorizer.CLI == nil {
		return CLIChallengeResult{}, ErrUnauthorized
	}
	return authorizer.CLI.ChallengeCLI(ctx, request)
}

func (authorizer CombinedAuthorizer) ListCLIGrants(ctx context.Context) ([]application.CLIGrant, error) {
	if authorizer.CLI == nil {
		return nil, ErrUnauthorized
	}
	return authorizer.CLI.ListCLIGrants(ctx)
}

func (authorizer CombinedAuthorizer) ListCLIGrantScopeUpgrades(
	ctx context.Context,
) ([]application.CLIGrantScopeUpgrade, error) {
	if authorizer.CLI == nil {
		return nil, ErrUnauthorized
	}
	return authorizer.CLI.ListCLIGrantScopeUpgrades(ctx)
}

func (authorizer CombinedAuthorizer) DecideCLIGrant(
	ctx context.Context,
	id string,
	approve bool,
) (application.CLIGrant, error) {
	if authorizer.CLI == nil {
		return application.CLIGrant{}, ErrUnauthorized
	}
	return authorizer.CLI.DecideCLIGrant(ctx, id, approve)
}

func (authorizer CombinedAuthorizer) RevokeCLIGrant(
	ctx context.Context,
	id string,
) (application.CLIGrant, error) {
	if authorizer.CLI == nil {
		return application.CLIGrant{}, ErrUnauthorized
	}
	return authorizer.CLI.RevokeCLIGrant(ctx, id)
}

func (authorizer CombinedAuthorizer) DecideCLIGrantScopeUpgrade(
	ctx context.Context,
	id string,
	approve bool,
) (application.CLIGrantScopeUpgrade, application.CLIGrant, error) {
	if authorizer.CLI == nil {
		return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, ErrUnauthorized
	}
	return authorizer.CLI.DecideCLIGrantScopeUpgrade(ctx, id, approve)
}
