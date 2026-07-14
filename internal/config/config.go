package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/internal/cell"
)

type RootSet struct {
	BootstrapRoot string `json:"bootstrapRoot"`
	StoreRoot     string `json:"storeRoot"`
	CacheRoot     string `json:"cacheRoot"`
	RuntimeRoot   string `json:"runtimeRoot"`
	LogRoot       string `json:"logRoot"`
}

func (roots RootSet) Validate() error {
	values := []struct {
		name string
		path string
	}{
		{"bootstrapRoot", roots.BootstrapRoot},
		{"storeRoot", roots.StoreRoot},
		{"cacheRoot", roots.CacheRoot},
		{"runtimeRoot", roots.RuntimeRoot},
		{"logRoot", roots.LogRoot},
	}
	for _, value := range values {
		if value.path == "" || !filepath.IsAbs(value.path) || filepath.Clean(value.path) != value.path {
			return fmt.Errorf("%s must be a clean absolute path", value.name)
		}
	}
	return nil
}

type TrustKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
}

type TrustConfig struct {
	Threshold int        `json:"threshold"`
	Keys      []TrustKey `json:"keys"`
}

func (trust TrustConfig) Validate(label string) error {
	if trust.Threshold < 1 || trust.Threshold > len(trust.Keys) {
		return fmt.Errorf("%s threshold is invalid", label)
	}
	seen := make(map[string]bool, len(trust.Keys))
	for _, key := range trust.Keys {
		if key.ID == "" || key.PublicKey == "" || seen[key.ID] {
			return fmt.Errorf("%s keys require unique id and publicKey", label)
		}
		seen[key.ID] = true
		decoded, err := base64.StdEncoding.DecodeString(key.PublicKey)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return fmt.Errorf("%s key %q is not a base64 Ed25519 public key", label, key.ID)
		}
	}
	return nil
}

type Bootstrap struct {
	Schema           int         `json:"schema"`
	Channel          string      `json:"channel"`
	Namespace        string      `json:"namespace"`
	DataDir          string      `json:"dataDir"`
	Roots            RootSet     `json:"roots"`
	UpdateOrigins    []string    `json:"updateOrigins"`
	ProtocolFloor    string      `json:"protocolFloor"`
	InitialTrustRoot TrustConfig `json:"initialTrustRoot"`
}

func LoadBootstrap(path string) (Bootstrap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Bootstrap{}, fmt.Errorf("read bootstrap config: %w", err)
	}
	var bootstrap Bootstrap
	if err := json.Unmarshal(data, &bootstrap); err != nil {
		return Bootstrap{}, fmt.Errorf("decode bootstrap config: %w", err)
	}
	if err := bootstrap.Validate(); err != nil {
		return Bootstrap{}, err
	}
	return bootstrap, nil
}

func (bootstrap Bootstrap) Validate() error {
	if bootstrap.Schema != 1 {
		return fmt.Errorf("unsupported bootstrap schema %d", bootstrap.Schema)
	}
	if _, err := cell.New(bootstrap.Channel, bootstrap.Namespace); err != nil {
		return fmt.Errorf("invalid cell identity: %w", err)
	}
	if err := bootstrap.Roots.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(bootstrap.DataDir) || filepath.Clean(bootstrap.DataDir) != bootstrap.DataDir {
		return fmt.Errorf("dataDir must be a clean absolute path")
	}
	if bootstrap.ProtocolFloor == "" {
		return fmt.Errorf("protocolFloor is required")
	}
	return bootstrap.InitialTrustRoot.Validate("initialTrustRoot")
}
