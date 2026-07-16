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
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	UIChallengeSchema = "open-cut/ui-challenge/v1"
	UISessionSchema   = "open-cut/ui-session/v1"
	UIRole            = "first-party-ui"
	UIChallengeTTL    = 30 * time.Second
	UISessionTTL      = 10 * time.Minute
	challengeLimit    = 256
	sessionLimit      = 1024
)

var (
	ErrUIChallengeInvalid = errors.New("UI challenge is invalid")
	ErrUIChallengeExpired = errors.New("UI challenge expired")
	ErrUIOriginDenied     = errors.New("UI origin is denied")
	ErrUIRateLimited      = errors.New("UI challenge is rate limited")
)

type UISessionConfig struct {
	InstallationID         string
	InstallationGeneration uint64
	CellGeneration         uint64
	PublicKey              ed25519.PublicKey
	AllowedOrigins         []string
	AllowDevelopmentOrigin bool
}

type UIChallengeRequest struct {
	ClientInstance string `json:"clientInstance" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Origin         string `json:"origin" minLength:"1" maxLength:"512"`
}

type UIChallengeResult struct {
	Schema                 string    `json:"schema" enum:"open-cut/ui-challenge/v1"`
	Nonce                  string    `json:"nonce"`
	ExpiresAt              time.Time `json:"expiresAt"`
	InstallationID         string    `json:"installationId"`
	InstallationGeneration uint64    `json:"installationGeneration" minimum:"1"`
	CellGeneration         uint64    `json:"cellGeneration"`
	APIInstanceID          string    `json:"apiInstanceId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	ClientInstance         string    `json:"clientInstance"`
	Origin                 string    `json:"origin"`
	Role                   string    `json:"role" enum:"first-party-ui"`
	SigningPayload         string    `json:"signingPayload" doc:"Base64url canonical challenge bytes"`
}

type UISessionRequest struct {
	Nonce     string `json:"nonce" minLength:"43" maxLength:"43"`
	Signature string `json:"signature" doc:"Base64url Ed25519 signature"`
}

type UISessionResult struct {
	Schema    string    `json:"schema" enum:"open-cut/ui-session/v1"`
	Session   string    `json:"session"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type UISessionBootstrap interface {
	ChallengeUI(context.Context, UIChallengeRequest) (UIChallengeResult, error)
	ExchangeUI(context.Context, UISessionRequest) (UISessionResult, error)
}

type uiChallenge struct {
	result    UIChallengeResult
	canonical []byte
	issuedAt  time.Time
}

type uiSession struct {
	expiresAt      time.Time
	clientInstance string
	origin         string
}

type UISessionService struct {
	mu           sync.Mutex
	config       UISessionConfig
	repository   application.AuthorizationRepository
	identities   application.IdentityGenerator
	clock        application.Clock
	random       io.Reader
	creator      domain.CreatorID
	apiInstance  string
	challenges   map[string]uiChallenge
	challengeKey map[string]string
	lastIssued   map[string]time.Time
	sessions     map[string]uiSession
}

func NewUISessionService(
	ctx context.Context,
	config UISessionConfig,
	repository application.AuthorizationRepository,
	identities application.IdentityGenerator,
	clock application.Clock,
	random io.Reader,
) (*UISessionService, error) {
	if repository == nil || identities == nil || clock == nil || random == nil {
		return nil, fmt.Errorf("UI session dependencies are required")
	}
	if _, err := domain.ParseRequestID(config.InstallationID); err != nil ||
		config.InstallationGeneration < 1 || len(config.PublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("UI session installation trust is invalid")
	}
	if len(config.AllowedOrigins) == 0 && !config.AllowDevelopmentOrigin {
		return nil, fmt.Errorf("UI session requires an origin policy")
	}
	now := clock.Now().UTC()
	creatorValue, err := identities.NewID(ctx, now)
	if err != nil {
		return nil, err
	}
	candidate, err := domain.ParseCreatorID(creatorValue)
	if err != nil {
		return nil, err
	}
	creator, err := repository.EnsureLocalCreator(ctx, candidate, now)
	if err != nil {
		return nil, err
	}
	instance, err := identities.NewID(ctx, now)
	if err != nil {
		return nil, err
	}
	if _, err := domain.ParseActivityEventID(instance); err != nil {
		return nil, err
	}
	return &UISessionService{
		config: config, repository: repository, identities: identities, clock: clock, random: random,
		creator: creator, apiInstance: instance, challenges: make(map[string]uiChallenge),
		challengeKey: make(map[string]string), lastIssued: make(map[string]time.Time),
		sessions: make(map[string]uiSession),
	}, nil
}

func (service *UISessionService) ChallengeUI(
	_ context.Context,
	request UIChallengeRequest,
) (UIChallengeResult, error) {
	if _, err := domain.ParseRequestID(request.ClientInstance); err != nil {
		return UIChallengeResult{}, ErrUIChallengeInvalid
	}
	origin, err := service.acceptOrigin(request.Origin)
	if err != nil {
		return UIChallengeResult{}, err
	}
	now := service.clock.Now().UTC()
	key := request.ClientInstance + "\x00" + origin
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	if previous := service.lastIssued[key]; !previous.IsZero() && now.Sub(previous) < 250*time.Millisecond {
		return UIChallengeResult{}, ErrUIRateLimited
	}
	if len(service.challenges) >= challengeLimit {
		return UIChallengeResult{}, ErrUIRateLimited
	}
	if previous := service.challengeKey[key]; previous != "" {
		delete(service.challenges, previous)
	}
	nonce, err := randomToken(service.random, "")
	if err != nil {
		return UIChallengeResult{}, err
	}
	result := UIChallengeResult{
		Schema: UIChallengeSchema, Nonce: nonce, ExpiresAt: now.Add(UIChallengeTTL),
		InstallationID:         service.config.InstallationID,
		InstallationGeneration: service.config.InstallationGeneration,
		CellGeneration:         service.config.CellGeneration, APIInstanceID: service.apiInstance,
		ClientInstance: request.ClientInstance, Origin: origin, Role: UIRole,
	}
	canonical, err := canonicalUIChallenge(result)
	if err != nil {
		return UIChallengeResult{}, err
	}
	result.SigningPayload = base64.RawURLEncoding.EncodeToString(canonical)
	service.challenges[nonce] = uiChallenge{result: result, canonical: canonical, issuedAt: now}
	service.challengeKey[key] = nonce
	service.lastIssued[key] = now
	return result, nil
}

func (service *UISessionService) ExchangeUI(
	ctx context.Context,
	request UISessionRequest,
) (UISessionResult, error) {
	now := service.clock.Now().UTC()
	service.mu.Lock()
	challenge, exists := service.challenges[request.Nonce]
	if exists {
		delete(service.challenges, request.Nonce)
		delete(service.challengeKey, challenge.result.ClientInstance+"\x00"+challenge.result.Origin)
	}
	service.cleanupLocked(now)
	service.mu.Unlock()
	if !exists {
		return UISessionResult{}, ErrUIChallengeInvalid
	}
	if !now.Before(challenge.result.ExpiresAt) {
		return UISessionResult{}, ErrUIChallengeExpired
	}
	signature, err := base64.RawURLEncoding.DecodeString(request.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(service.config.PublicKey, challenge.canonical, signature) {
		_ = service.audit(ctx, challenge.result.ClientInstance, "ui-session.exchange", "denied", digestBytes(challenge.canonical))
		return UISessionResult{}, ErrUnauthorized
	}
	token, err := randomToken(service.random, "oc_ui_")
	if err != nil {
		return UISessionResult{}, err
	}
	expiresAt := now.Add(UISessionTTL)
	if err := service.audit(
		ctx, challenge.result.ClientInstance, "ui-session.exchange", "issued", digestBytes(challenge.canonical),
	); err != nil {
		return UISessionResult{}, err
	}
	service.mu.Lock()
	service.cleanupLocked(now)
	if len(service.sessions) >= sessionLimit {
		service.mu.Unlock()
		return UISessionResult{}, ErrUIRateLimited
	}
	service.sessions[tokenHash(token)] = uiSession{
		expiresAt: expiresAt, clientInstance: challenge.result.ClientInstance, origin: challenge.result.Origin,
	}
	service.mu.Unlock()
	return UISessionResult{Schema: UISessionSchema, Session: token, ExpiresAt: expiresAt}, nil
}

func (service *UISessionService) Authorize(
	ctx context.Context,
	request AuthorizationRequest,
) (application.Authority, error) {
	if request.UISession == "" || request.CLIGrant != "" || request.CLIChallenge != "" || request.CLISignature != "" ||
		request.Method == "" || request.Route == "" {
		return application.Authority{}, ErrUnauthorized
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	session, exists := service.sessions[tokenHash(request.UISession)]
	service.mu.Unlock()
	if !exists || !now.Before(session.expiresAt) {
		return application.Authority{}, ErrUnauthorized
	}
	requestDigest := digestBytes([]byte(request.Method + "\n" + request.Route))
	if err := service.audit(ctx, session.clientInstance, "http:"+request.Method+":"+request.Route, "authorized", requestDigest); err != nil {
		return application.Authority{}, ErrUnauthorized
	}
	return application.Authority{
		Surface: application.AuthorityFirstPartyUI, InstallationID: service.config.InstallationID,
		Actor: domain.CreatorActor(service.creator),
	}, nil
}

func (service *UISessionService) acceptOrigin(value string) (string, error) {
	for _, allowed := range service.config.AllowedOrigins {
		if value == allowed {
			return value, nil
		}
	}
	if !service.config.AllowDevelopmentOrigin {
		return "", ErrUIOriginDenied
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.Port() == "" || parsed.User != nil ||
		(parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1") ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrUIOriginDenied
	}
	return strings.TrimSuffix(value, "/"), nil
}

func (service *UISessionService) cleanupLocked(now time.Time) {
	for nonce, challenge := range service.challenges {
		if !now.Before(challenge.result.ExpiresAt) {
			delete(service.challenges, nonce)
			delete(service.challengeKey, challenge.result.ClientInstance+"\x00"+challenge.result.Origin)
		}
	}
	for hash, session := range service.sessions {
		if !now.Before(session.expiresAt) {
			delete(service.sessions, hash)
		}
	}
}

func (service *UISessionService) audit(
	ctx context.Context,
	principalID, action, outcome, requestDigest string,
) error {
	now := service.clock.Now().UTC()
	value, err := service.identities.NewID(ctx, now)
	if err != nil {
		return err
	}
	if _, err := domain.ParseActivityEventID(value); err != nil {
		return err
	}
	return service.repository.AppendAuthorizationAudit(ctx, application.AuthorizationAudit{
		ID: value, InstallationID: service.config.InstallationID,
		PrincipalKind: application.AuthorityFirstPartyUI, PrincipalID: principalID,
		Action: action, Outcome: outcome, RequestDigest: requestDigest, OccurredAt: now,
	})
}

func canonicalUIChallenge(challenge UIChallengeResult) ([]byte, error) {
	return json.Marshal(struct {
		APIInstanceID          string `json:"apiInstanceId"`
		CellGeneration         uint64 `json:"cellGeneration"`
		ClientInstance         string `json:"clientInstance"`
		ExpiresAt              string `json:"expiresAt"`
		InstallationGeneration uint64 `json:"installationGeneration"`
		InstallationID         string `json:"installationId"`
		Nonce                  string `json:"nonce"`
		Origin                 string `json:"origin"`
		Role                   string `json:"role"`
		Schema                 string `json:"schema"`
	}{
		APIInstanceID: challenge.APIInstanceID, CellGeneration: challenge.CellGeneration,
		ClientInstance: challenge.ClientInstance, ExpiresAt: challenge.ExpiresAt.Format(time.RFC3339Nano),
		InstallationGeneration: challenge.InstallationGeneration, InstallationID: challenge.InstallationID,
		Nonce: challenge.Nonce, Origin: challenge.Origin, Role: challenge.Role, Schema: challenge.Schema,
	})
}

func randomToken(source io.Reader, prefix string) (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(source, value); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value), nil
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
