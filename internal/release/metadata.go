package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/utils/target"
)

const ReleaseMetadataSchema = 1

type BundleDescriptor struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Descriptor struct {
	Schema                   int              `json:"schema"`
	Channel                  string           `json:"channel"`
	Version                  string           `json:"version"`
	Platform                 target.Platform  `json:"platform"`
	Arch                     target.Arch      `json:"arch"`
	Bundle                   BundleDescriptor `json:"bundle"`
	MinimumBootstrapProtocol string           `json:"minimumBootstrapProtocol"`
	PublishedAt              time.Time        `json:"publishedAt"`
	ExpiresAt                time.Time        `json:"expiresAt"`
}

type Signature struct {
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

type Envelope struct {
	Signed     Descriptor  `json:"signed"`
	Signatures []Signature `json:"signatures"`
}

func SignEnvelope(descriptor Descriptor, keyID string, privateKey ed25519.PrivateKey) (Envelope, error) {
	if err := descriptor.Validate(descriptor.Channel, descriptor.MinimumBootstrapProtocol, time.Now()); err != nil {
		return Envelope{}, err
	}
	payload, err := json.Marshal(descriptor)
	if err != nil {
		return Envelope{}, err
	}
	signature := ed25519.Sign(privateKey, payload)
	return Envelope{Signed: descriptor, Signatures: []Signature{{KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(signature)}}}, nil
}

func VerifyEnvelope(envelope Envelope, trust config.TrustConfig, channel, protocolFloor string, now time.Time) error {
	return VerifyEnvelopeTarget(envelope, trust, channel, protocolFloor, target.Host(), now)
}

func VerifyEnvelopeTarget(envelope Envelope, trust config.TrustConfig, channel, protocolFloor string, expected target.Target, now time.Time) error {
	if err := envelope.Signed.Validate(channel, protocolFloor, now); err != nil {
		return err
	}
	if envelope.Signed.Platform != expected.Platform || envelope.Signed.Arch != expected.Arch {
		return fmt.Errorf("release metadata target %s-%s does not match %s", envelope.Signed.Platform, envelope.Signed.Arch, expected)
	}
	payload, err := json.Marshal(envelope.Signed)
	if err != nil {
		return err
	}
	keys := make(map[string]ed25519.PublicKey, len(trust.Keys))
	for _, key := range trust.Keys {
		decoded, err := base64.StdEncoding.DecodeString(key.PublicKey)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return fmt.Errorf("trust key %q is invalid", key.ID)
		}
		keys[key.ID] = ed25519.PublicKey(decoded)
	}
	valid := make(map[string]bool)
	for _, signature := range envelope.Signatures {
		if valid[signature.KeyID] {
			continue
		}
		publicKey := keys[signature.KeyID]
		if publicKey == nil {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(signature.Signature)
		if err == nil && ed25519.Verify(publicKey, payload, decoded) {
			valid[signature.KeyID] = true
		}
	}
	if len(valid) < trust.Threshold {
		return fmt.Errorf("release metadata has %d valid signatures, threshold is %d", len(valid), trust.Threshold)
	}
	return nil
}

func (descriptor Descriptor) ValidateHost(channel, protocolFloor string, now time.Time) error {
	if err := descriptor.Validate(channel, protocolFloor, now); err != nil {
		return err
	}
	host := target.Host()
	if descriptor.Platform != host.Platform || descriptor.Arch != host.Arch {
		return fmt.Errorf("release metadata does not target this host")
	}
	return nil
}

func (descriptor Descriptor) Validate(channel, protocolFloor string, now time.Time) error {
	if descriptor.Schema != ReleaseMetadataSchema {
		return fmt.Errorf("unsupported release metadata schema %d", descriptor.Schema)
	}
	if _, err := ParseVersionForChannel(descriptor.Version, channel); err != nil {
		return err
	}
	if descriptor.Channel != channel {
		return fmt.Errorf("release metadata does not target channel %s", channel)
	}
	if err := (target.Target{Platform: descriptor.Platform, Arch: descriptor.Arch}).Validate(); err != nil {
		return fmt.Errorf("invalid release target: %w", err)
	}
	if descriptor.MinimumBootstrapProtocol != protocolFloor {
		return fmt.Errorf("release metadata requires bootstrap protocol %q", descriptor.MinimumBootstrapProtocol)
	}
	if descriptor.PublishedAt.IsZero() || descriptor.ExpiresAt.IsZero() || !descriptor.ExpiresAt.After(now) || descriptor.PublishedAt.After(now.Add(5*time.Minute)) {
		return fmt.Errorf("release metadata publication or expiry window is invalid")
	}
	if descriptor.Bundle.Size <= 0 || descriptor.Bundle.Size > 16<<30 {
		return fmt.Errorf("release bundle size is invalid")
	}
	digest, err := hex.DecodeString(descriptor.Bundle.SHA256)
	if err != nil || len(digest) != 32 || strings.ToLower(descriptor.Bundle.SHA256) != descriptor.Bundle.SHA256 {
		return fmt.Errorf("release bundle SHA-256 is invalid")
	}
	if descriptor.Bundle.Path == "" || strings.ContainsRune(descriptor.Bundle.Path, '\\') || path.IsAbs(descriptor.Bundle.Path) {
		return fmt.Errorf("release bundle path must be origin-relative")
	}
	cleanBundlePath := path.Clean(descriptor.Bundle.Path)
	if cleanBundlePath != descriptor.Bundle.Path || cleanBundlePath == "." || strings.HasPrefix(cleanBundlePath, "../") || !strings.HasPrefix(cleanBundlePath, "releases/") {
		return fmt.Errorf("release bundle path must be clean and beneath releases/")
	}
	return nil
}
