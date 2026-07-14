package verifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/publisher"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/target"
)

type Report struct {
	Schema        int    `json:"schema"`
	OK            bool   `json:"ok"`
	Version       string `json:"version"`
	Target        string `json:"target"`
	Bundle        string `json:"bundle"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	LauncherEntry string `json:"launcherEntry"`
	PayloadEntry  string `json:"payloadEntry"`
	OriginRoot    string `json:"originRoot,omitempty"`
}

func VerifyBundle(path string, expected target.Target) (Report, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Report{}, err
	}
	workspace, err := os.MkdirTemp("", "oc-control-verify-*")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(workspace)
	tree := filepath.Join(workspace, "tree")
	if err := bundle.Extract(absolute, tree); err != nil {
		return Report{}, err
	}
	manifest, err := release.LoadManifest(filepath.Join(tree, "manifest.json"))
	if err != nil {
		return Report{}, err
	}
	actual := target.Target{Platform: manifest.Platform, Arch: manifest.Arch}
	if err := actual.Validate(); err != nil {
		return Report{}, err
	}
	if expected.Platform != "" && actual != expected {
		return Report{}, fmt.Errorf("bundle target %s does not match expected %s", actual, expected)
	}
	if _, err := release.ResolveEntry(tree, manifest.Launcher.Entry, "launcher"); err != nil {
		return Report{}, err
	}
	topologyEntry, err := release.ResolveEntry(tree, manifest.Payload.Entry, "payload")
	if err != nil {
		return Report{}, err
	}
	if _, err := runtimetopology.Resolve(topologyEntry); err != nil {
		return Report{}, err
	}
	digest, size, err := bundle.SHA256(absolute)
	if err != nil {
		return Report{}, err
	}
	return Report{
		Schema: 1, OK: true, Version: manifest.Version, Target: actual.String(), Bundle: absolute,
		SHA256: digest, Size: size, LauncherEntry: manifest.Launcher.Entry, PayloadEntry: manifest.Payload.Entry,
	}, nil
}

func VerifyOrigin(originRoot, channel string, expected target.Target, keyPath string, now time.Time) (Report, error) {
	root, err := filepath.Abs(originRoot)
	if err != nil {
		return Report{}, err
	}
	key, _, err := publisher.LoadKey(keyPath)
	if err != nil {
		return Report{}, err
	}
	trust := config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{ID: key.KeyID, PublicKey: key.PublicKey}}}
	latestPath := filepath.Join(root, "metadata", channel, expected.String(), "latest.json")
	latest, err := os.ReadFile(latestPath)
	if err != nil {
		return Report{}, err
	}
	var envelope release.Envelope
	if err := json.Unmarshal(latest, &envelope); err != nil {
		return Report{}, err
	}
	if err := release.VerifyEnvelopeTarget(envelope, trust, channel, "bootstrap.v1", expected, now); err != nil {
		return Report{}, err
	}
	releasePath := filepath.Join(root, "releases", envelope.Signed.Version, expected.String(), "release.json")
	releaseBytes, err := os.ReadFile(releasePath)
	if err != nil {
		return Report{}, err
	}
	if !bytes.Equal(bytes.TrimSpace(latest), bytes.TrimSpace(releaseBytes)) {
		return Report{}, fmt.Errorf("latest.json does not exactly match immutable release.json")
	}
	bundlePath := filepath.Join(root, filepath.FromSlash(envelope.Signed.Bundle.Path))
	relative, err := filepath.Rel(root, bundlePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Report{}, fmt.Errorf("release bundle escapes origin root")
	}
	report, err := VerifyBundle(bundlePath, expected)
	if err != nil {
		return Report{}, err
	}
	if report.Version != envelope.Signed.Version || report.SHA256 != envelope.Signed.Bundle.SHA256 || report.Size != envelope.Signed.Bundle.Size {
		return Report{}, fmt.Errorf("origin metadata does not match release bundle")
	}
	report.OriginRoot = root
	return report, nil
}
