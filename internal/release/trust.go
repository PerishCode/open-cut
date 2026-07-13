package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/PerishCode/open-cut/internal/config"
)

const TrustRootSchema = 1

type TrustRoot struct {
	Schema    int               `json:"schema"`
	Version   uint64            `json:"version"`
	Threshold int               `json:"threshold"`
	Keys      []config.TrustKey `json:"keys"`
}

type TrustRootEnvelope struct {
	Signed     TrustRoot   `json:"signed"`
	Signatures []Signature `json:"signatures"`
}

func InitialTrustRoot(trust config.TrustConfig) (TrustRoot, error) {
	if err := trust.Validate("initialTrustRoot"); err != nil {
		return TrustRoot{}, err
	}
	return TrustRoot{Schema: TrustRootSchema, Version: 1, Threshold: trust.Threshold, Keys: trust.Keys}, nil
}

func (root TrustRoot) Validate() error {
	if root.Schema != TrustRootSchema || root.Version == 0 {
		return fmt.Errorf("invalid trust root schema or version")
	}
	return root.Config().Validate("trustRoot")
}

func (root TrustRoot) Config() config.TrustConfig {
	return config.TrustConfig{Threshold: root.Threshold, Keys: root.Keys}
}

func SignTrustRoot(root TrustRoot, keyID string, privateKey ed25519.PrivateKey) (TrustRootEnvelope, error) {
	if err := root.Validate(); err != nil {
		return TrustRootEnvelope{}, err
	}
	payload, err := json.Marshal(root)
	if err != nil {
		return TrustRootEnvelope{}, err
	}
	signature := ed25519.Sign(privateKey, payload)
	return TrustRootEnvelope{Signed: root, Signatures: []Signature{{
		KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(signature),
	}}}, nil
}

func AddTrustRootSignature(envelope TrustRootEnvelope, keyID string, privateKey ed25519.PrivateKey) (TrustRootEnvelope, error) {
	payload, err := json.Marshal(envelope.Signed)
	if err != nil {
		return TrustRootEnvelope{}, err
	}
	envelope.Signatures = append(envelope.Signatures, Signature{
		KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
	})
	return envelope, nil
}

func VerifyTrustRoot(envelope TrustRootEnvelope, current TrustRoot) error {
	if err := current.Validate(); err != nil {
		return fmt.Errorf("current trust root: %w", err)
	}
	if err := envelope.Signed.Validate(); err != nil {
		return fmt.Errorf("candidate trust root: %w", err)
	}
	if envelope.Signed.Version != current.Version+1 {
		return fmt.Errorf("trust root version must advance exactly from %d to %d", current.Version, current.Version+1)
	}
	payload, err := json.Marshal(envelope.Signed)
	if err != nil {
		return err
	}
	keys := make(map[string]ed25519.PublicKey, len(current.Keys))
	for _, key := range current.Keys {
		decoded, _ := base64.StdEncoding.DecodeString(key.PublicKey)
		keys[key.ID] = ed25519.PublicKey(decoded)
	}
	valid := make(map[string]bool)
	for _, signature := range envelope.Signatures {
		if valid[signature.KeyID] || keys[signature.KeyID] == nil {
			continue
		}
		decoded, decodeErr := base64.StdEncoding.DecodeString(signature.Signature)
		if decodeErr == nil && ed25519.Verify(keys[signature.KeyID], payload, decoded) {
			valid[signature.KeyID] = true
		}
	}
	if len(valid) < current.Threshold {
		return fmt.Errorf("trust root has %d valid old-root signatures, threshold is %d", len(valid), current.Threshold)
	}
	return nil
}
