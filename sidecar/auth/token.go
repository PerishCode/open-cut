package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/sidecar/protocol"
)

const TokenVersion = 1

type Claims struct {
	Version      int                   `json:"v"`
	SessionID    string                `json:"sid"`
	Generation   uint64                `json:"gen"`
	Subject      string                `json:"sub"`
	Role         protocol.Role         `json:"role"`
	Capabilities []protocol.Capability `json:"cap"`
	IssuedAt     int64                 `json:"iat"`
	ExpiresAt    int64                 `json:"exp"`
	Nonce        string                `json:"nonce"`
	DelegatedBy  string                `json:"delegatedBy,omitempty"`
}

func (claims Claims) Has(capability protocol.Capability) bool {
	for _, candidate := range claims.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

type Manager struct {
	key        []byte
	sessionID  string
	generation uint64
	now        func() time.Time
}

func NewManager(sessionID string, generation uint64) (*Manager, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate token signing key: %w", err)
	}
	return newManager(key, sessionID, generation, time.Now), nil
}

func newManager(key []byte, sessionID string, generation uint64, now func() time.Time) *Manager {
	return &Manager{key: key, sessionID: sessionID, generation: generation, now: now}
}

func (manager *Manager) Mint(subject string, role protocol.Role, capabilities []protocol.Capability, ttl time.Duration) (string, error) {
	return manager.MintDelegated(subject, role, capabilities, ttl, "")
}

func (manager *Manager) MintDelegated(subject string, role protocol.Role, capabilities []protocol.Capability, ttl time.Duration, delegatedBy string) (string, error) {
	if subject == "" || ttl <= 0 {
		return "", fmt.Errorf("token subject and positive ttl are required")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	now := manager.now().UTC()
	claims := Claims{
		Version: TokenVersion, SessionID: manager.sessionID, Generation: manager.generation,
		Subject: subject, Role: role, Capabilities: append([]protocol.Capability(nil), capabilities...),
		IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(),
		Nonce: base64.RawURLEncoding.EncodeToString(nonceBytes), DelegatedBy: delegatedBy,
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := manager.sign(encoded)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (manager *Manager) Verify(token string, required protocol.Capability) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Claims{}, fmt.Errorf("malformed capability token")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, manager.sign(parts[0])) {
		return Claims{}, fmt.Errorf("invalid capability signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("decode capability claims: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, fmt.Errorf("decode capability claims: %w", err)
	}
	if claims.Version != TokenVersion || claims.SessionID != manager.sessionID || claims.Generation != manager.generation {
		return Claims{}, fmt.Errorf("capability belongs to another session generation")
	}
	now := manager.now().Unix()
	if claims.ExpiresAt <= now || claims.IssuedAt > now+30 {
		return Claims{}, fmt.Errorf("capability is expired or not yet valid")
	}
	if required != "" && !claims.Has(required) {
		return Claims{}, fmt.Errorf("capability %q is required", required)
	}
	return claims, nil
}

func (manager *Manager) sign(encoded string) []byte {
	mac := hmac.New(sha256.New, manager.key)
	mac.Write([]byte(encoded))
	return mac.Sum(nil)
}
