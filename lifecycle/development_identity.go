package lifecycle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const developmentIdentitySchema = 1

type storedDevelopmentKey struct {
	Role       string                            `json:"role"`
	Algorithm  protocol.InstallationKeyAlgorithm `json:"algorithm"`
	PublicKey  string                            `json:"publicKey"`
	PrivateKey string                            `json:"privateKey"`
}

type storedDevelopmentIdentity struct {
	Schema         int                    `json:"schema"`
	InstallationID string                 `json:"installationId"`
	Generation     uint64                 `json:"generation"`
	Keys           []storedDevelopmentKey `json:"keys"`
}

type DevelopmentInstallationIdentity struct {
	assertion protocol.InstallationAssertion
	private   map[string]ed25519.PrivateKey
}

func EnsureDevelopmentInstallationIdentity(root string, roles []string) (DevelopmentInstallationIdentity, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return DevelopmentInstallationIdentity{}, fmt.Errorf("development identity root must be a clean absolute path")
	}
	normalizedRoles, err := normalizeInstallationRoles(roles)
	if err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	filename := filepath.Join(root, "identity.json")
	data, err := os.ReadFile(filename)
	if err == nil {
		return decodeDevelopmentIdentity(data, normalizedRoles)
	}
	if !os.IsNotExist(err) {
		return DevelopmentInstallationIdentity{}, err
	}
	created, err := createDevelopmentIdentity(normalizedRoles)
	if err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	if err := atomicfile.WriteJSON(filename, created, 0o600); err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	if err := os.Chmod(filename, 0o600); err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	return decodeDevelopmentIdentity(encoded, normalizedRoles)
}

func LoadDevelopmentInstallationIdentity(root string, roles []string) (DevelopmentInstallationIdentity, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return DevelopmentInstallationIdentity{}, fmt.Errorf("development identity root must be a clean absolute path")
	}
	normalizedRoles, err := normalizeInstallationRoles(roles)
	if err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	data, err := os.ReadFile(filepath.Join(root, "identity.json"))
	if err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	return decodeDevelopmentIdentity(data, normalizedRoles)
}

func (identity DevelopmentInstallationIdentity) Assertion() protocol.InstallationAssertion {
	assertion := identity.assertion
	assertion.Keys = append([]protocol.InstallationPublicKey(nil), assertion.Keys...)
	return assertion
}

func (identity DevelopmentInstallationIdentity) Sign(role string, message []byte) ([]byte, error) {
	privateKey, ok := identity.private[role]
	if !ok {
		return nil, fmt.Errorf("development installation role %q is unavailable", role)
	}
	return ed25519.Sign(privateKey, message), nil
}

func createDevelopmentIdentity(roles []string) (storedDevelopmentIdentity, error) {
	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		return storedDevelopmentIdentity{}, err
	}
	identity := storedDevelopmentIdentity{
		Schema: developmentIdentitySchema, InstallationID: "installation-" + hex.EncodeToString(randomID),
		Generation: 1, Keys: make([]storedDevelopmentKey, 0, len(roles)),
	}
	for _, role := range roles {
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return storedDevelopmentIdentity{}, err
		}
		identity.Keys = append(identity.Keys, storedDevelopmentKey{
			Role: role, Algorithm: protocol.InstallationKeyAlgorithmEd25519,
			PublicKey:  base64.StdEncoding.EncodeToString(publicKey),
			PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
		})
	}
	return identity, nil
}

func decodeDevelopmentIdentity(data []byte, roles []string) (DevelopmentInstallationIdentity, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var stored storedDevelopmentIdentity
	if err := decoder.Decode(&stored); err != nil {
		return DevelopmentInstallationIdentity{}, fmt.Errorf("decode development installation identity: %w", err)
	}
	if stored.Schema != developmentIdentitySchema || stored.Generation < 1 || len(stored.Keys) != len(roles) {
		return DevelopmentInstallationIdentity{}, fmt.Errorf("development installation identity does not match requested roles")
	}
	assertion := protocol.InstallationAssertion{
		Schema: 1, InstallationID: stored.InstallationID, Generation: stored.Generation,
		Keys: make([]protocol.InstallationPublicKey, 0, len(stored.Keys)),
	}
	privateKeys := make(map[string]ed25519.PrivateKey, len(stored.Keys))
	for index, role := range roles {
		key := stored.Keys[index]
		if key.Role != role || key.Algorithm != protocol.InstallationKeyAlgorithmEd25519 {
			return DevelopmentInstallationIdentity{}, fmt.Errorf("development installation identity role set changed")
		}
		publicKey, publicErr := base64.StdEncoding.DecodeString(key.PublicKey)
		privateKey, privateErr := base64.StdEncoding.DecodeString(key.PrivateKey)
		if publicErr != nil || privateErr != nil || len(publicKey) != ed25519.PublicKeySize || len(privateKey) != ed25519.PrivateKeySize ||
			!bytes.Equal(privateKey[32:], publicKey) {
			return DevelopmentInstallationIdentity{}, fmt.Errorf("development installation key %q is invalid", role)
		}
		assertion.Keys = append(assertion.Keys, protocol.InstallationPublicKey{
			Role: role, Algorithm: key.Algorithm, PublicKey: key.PublicKey,
		})
		privateKeys[role] = ed25519.PrivateKey(append([]byte(nil), privateKey...))
	}
	if err := assertion.Validate(); err != nil {
		return DevelopmentInstallationIdentity{}, err
	}
	return DevelopmentInstallationIdentity{assertion: assertion, private: privateKeys}, nil
}

func normalizeInstallationRoles(roles []string) ([]string, error) {
	if len(roles) == 0 {
		return nil, fmt.Errorf("development installation identity requires at least one role")
	}
	normalized := append([]string(nil), roles...)
	sort.Strings(normalized)
	for index, role := range normalized {
		if role == "" || index > 0 && normalized[index-1] == role {
			return nil, fmt.Errorf("development installation roles must be unique non-empty labels")
		}
	}
	return normalized, nil
}
