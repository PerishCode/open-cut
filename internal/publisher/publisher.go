package publisher

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

type Key struct {
	Schema     int    `json:"schema"`
	KeyID      string `json:"keyId"`
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

type Result struct {
	Schema       int                `json:"schema"`
	Version      string             `json:"version"`
	Target       string             `json:"target"`
	OriginRoot   string             `json:"originRoot"`
	Bundle       string             `json:"bundle"`
	Release      string             `json:"release"`
	Latest       string             `json:"latest"`
	InitialTrust config.TrustConfig `json:"initialTrustRoot"`
}

func GenerateKey(path, keyID string) (Key, error) {
	if strings.TrimSpace(keyID) == "" {
		return Key{}, fmt.Errorf("key ID is required")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Key{}, err
	}
	key := Key{
		Schema: 1, KeyID: keyID,
		PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
		PublicKey:  base64.StdEncoding.EncodeToString(publicKey),
	}
	if err := atomicfile.WriteJSON(path, key, 0o600); err != nil {
		return Key{}, err
	}
	return key, nil
}

func LoadKey(path string) (Key, ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Key{}, nil, err
	}
	var key Key
	if err := json.Unmarshal(data, &key); err != nil {
		return Key{}, nil, err
	}
	privateKey, privateErr := base64.StdEncoding.DecodeString(key.PrivateKey)
	publicKey, publicErr := base64.StdEncoding.DecodeString(key.PublicKey)
	if key.Schema != 1 || key.KeyID == "" || privateErr != nil || publicErr != nil || len(privateKey) != ed25519.PrivateKeySize || len(publicKey) != ed25519.PublicKeySize || !ed25519.PublicKey(publicKey).Equal(ed25519.PrivateKey(privateKey).Public()) {
		return Key{}, nil, fmt.Errorf("invalid Ed25519 release key")
	}
	return key, ed25519.PrivateKey(privateKey), nil
}

func Create(bundlePath, originRoot, keyPath string, expiry time.Duration, now time.Time) (Result, error) {
	if expiry <= 0 {
		return Result{}, fmt.Errorf("positive metadata expiry is required")
	}
	bundlePath, err := filepath.Abs(bundlePath)
	if err != nil {
		return Result{}, err
	}
	originRoot, err = filepath.Abs(originRoot)
	if err != nil {
		return Result{}, err
	}
	manifestBytes, err := bundle.ReadFile(bundlePath, "manifest.json", 1<<20)
	if err != nil {
		return Result{}, err
	}
	var manifest release.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return Result{}, fmt.Errorf("decode bundle manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Result{}, err
	}
	digest, size, err := bundle.SHA256(bundlePath)
	if err != nil {
		return Result{}, err
	}
	buildTarget := target.Target{Platform: manifest.Platform, Arch: manifest.Arch}
	bundleName := "open-cut-" + manifest.Version + "-" + buildTarget.String() + ".release-bundle.tar.zst"
	relativeBundle := filepath.ToSlash(filepath.Join("releases", manifest.Version, buildTarget.String(), bundleName))
	destination := filepath.Join(originRoot, filepath.FromSlash(relativeBundle))
	if err := copyArtifact(bundlePath, destination, digest, size); err != nil {
		return Result{}, err
	}
	key, privateKey, err := LoadKey(keyPath)
	if err != nil {
		return Result{}, err
	}
	releasePath := filepath.Join(originRoot, "releases", manifest.Version, buildTarget.String(), "release.json")
	latestPath := filepath.Join(originRoot, "metadata", manifest.Channel, buildTarget.String(), "latest.json")
	trust := config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{ID: key.KeyID, PublicKey: key.PublicKey}}}
	if existing, readErr := os.ReadFile(releasePath); readErr == nil {
		var envelope release.Envelope
		if err := json.Unmarshal(existing, &envelope); err != nil {
			return Result{}, fmt.Errorf("decode immutable release metadata: %w", err)
		}
		if err := release.VerifyEnvelopeTarget(envelope, trust, manifest.Channel, manifest.MinimumBootstrapProtocol, buildTarget, now); err != nil {
			return Result{}, fmt.Errorf("verify immutable release metadata: %w", err)
		}
		if envelope.Signed.Version != manifest.Version || envelope.Signed.Bundle.Path != relativeBundle || envelope.Signed.Bundle.SHA256 != digest || envelope.Signed.Bundle.Size != size {
			return Result{}, fmt.Errorf("immutable release metadata conflicts with bundle %s", bundlePath)
		}
		if err := atomicfile.Write(latestPath, existing, 0o644); err != nil {
			return Result{}, err
		}
		return Result{
			Schema: 1, Version: manifest.Version, Target: buildTarget.String(), OriginRoot: originRoot,
			Bundle: destination, Release: releasePath, Latest: latestPath, InitialTrust: trust,
		}, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Result{}, readErr
	}
	publishedAt := now.UTC()
	envelope, err := release.SignEnvelope(release.Descriptor{
		Schema: release.ReleaseMetadataSchema, Channel: manifest.Channel, Version: manifest.Version,
		Platform: manifest.Platform, Arch: manifest.Arch,
		Bundle:                   release.BundleDescriptor{Path: relativeBundle, Size: size, SHA256: digest},
		MinimumBootstrapProtocol: manifest.MinimumBootstrapProtocol,
		PublishedAt:              publishedAt, ExpiresAt: publishedAt.Add(expiry),
	}, key.KeyID, privateKey)
	if err != nil {
		return Result{}, err
	}
	if err := atomicfile.WriteJSON(releasePath, envelope, 0o644); err != nil {
		return Result{}, err
	}
	if err := atomicfile.WriteJSON(latestPath, envelope, 0o644); err != nil {
		return Result{}, err
	}
	return Result{
		Schema: 1, Version: manifest.Version, Target: buildTarget.String(), OriginRoot: originRoot,
		Bundle: destination, Release: releasePath, Latest: latestPath,
		InitialTrust: trust,
	}, nil
}

func copyArtifact(source, destination, digest string, size int64) error {
	if existingDigest, existingSize, err := bundle.SHA256(destination); err == nil {
		if existingDigest == digest && existingSize == size {
			return nil
		}
		return fmt.Errorf("release artifact already exists with different content: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return errors.Join(copyErr, syncErr, closeErr)
	}
	return nil
}
