package harness

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

const harnessVersion = "1.0.0-harness.1"
const steadyHarnessVersion = "2.0.0-harness.1"
const failingHarnessVersion = "3.0.0-harness.1"

func RunColdStart(ctx context.Context, workspace, launcherArtifact, payloadArtifact string) Report {
	started := time.Now()
	report := Report{Schema: 1, Scenario: "genesis-download-confirm-offline-rollback"}
	hostTarget := target.Host()
	check := func(name string, err error) bool {
		entry := Check{Name: name, Passed: err == nil}
		if err != nil {
			entry.Detail = err.Error()
		}
		report.Checks = append(report.Checks, entry)
		return err == nil
	}

	identity, err := cell.New("harness", "cold-start")
	if !check("cell-identity", err) {
		return finish(report, started)
	}
	roots := config.RootSet{
		BootstrapRoot: filepath.Join(workspace, "bootstrap"), StoreRoot: filepath.Join(workspace, "roots", "store"),
		CacheRoot: filepath.Join(workspace, "roots", "cache"), RuntimeRoot: filepath.Join(workspace, "roots", "runtime"),
		LogRoot: filepath.Join(workspace, "roots", "logs"),
	}
	paths, err := layout.Resolve(roots, identity)
	if !check("root-layout", err) || !check("create-roots", paths.Ensure()) {
		return finish(report, started)
	}
	bootstrapLauncher := filepath.Join(roots.BootstrapRoot, hostTarget.ExecutableName("launcher"))
	releaseTree := filepath.Join(workspace, "fixture-origin", "tree")
	versionedLauncher := filepath.Join(releaseTree, "launcher", hostTarget.ExecutableName("launcher"))
	payload := filepath.Join(releaseTree, "payload", hostTarget.ExecutableName("fixture-runtime"))
	topologyPath := filepath.Join(releaseTree, "payload", "runtime-topology.json")
	if !check("install-bootstrap-launcher", copyExecutable(launcherArtifact, bootstrapLauncher)) ||
		!check("stage-versioned-launcher", copyExecutable(launcherArtifact, versionedLauncher)) ||
		!check("stage-fixture-payload", copyExecutable(payloadArtifact, payload)) ||
		!check("write-fixture-topology", writeFixtureTopology(topologyPath, payload)) {
		return finish(report, started)
	}

	manifest := release.Manifest{
		Schema: release.ManifestSchema, Channel: identity.Channel, Version: harnessVersion,
		Platform: hostTarget.Platform, Arch: hostTarget.Arch,
		Launcher:                 release.Entry{Entry: filepath.ToSlash(filepath.Join("launcher", filepath.Base(versionedLauncher)))},
		Payload:                  release.Entry{Entry: filepath.ToSlash(filepath.Join("payload", filepath.Base(topologyPath)))},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: time.Now().UTC(),
	}
	if !check("write-release-manifest", atomicfile.WriteJSON(filepath.Join(releaseTree, "manifest.json"), manifest, 0o600)) {
		return finish(report, started)
	}
	bundlePath := filepath.Join(workspace, "fixture-origin", "release-bundle.tar.zst")
	if !check("pack-release-bundle", bundle.Pack(releaseTree, bundlePath)) {
		return finish(report, started)
	}
	digest, size, err := bundle.SHA256(bundlePath)
	if !check("digest-release-bundle", err) {
		return finish(report, started)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if !check("generate-trust-key", err) {
		return finish(report, started)
	}
	steadyTree := filepath.Join(workspace, "fixture-origin", "tree-v2")
	steadyLauncher := filepath.Join(steadyTree, "launcher", hostTarget.ExecutableName("launcher"))
	steadyPayload := filepath.Join(steadyTree, "payload", hostTarget.ExecutableName("fixture-runtime"))
	steadyTopology := filepath.Join(steadyTree, "payload", "runtime-topology.json")
	steadyManifest := manifest
	steadyManifest.Version = steadyHarnessVersion
	steadyManifest.PublishedAt = time.Now().UTC()
	steadyBundlePath := filepath.Join(workspace, "fixture-origin", "release-bundle-v2.tar.zst")
	if !check("stage-steady-launcher", copyExecutable(launcherArtifact, steadyLauncher)) ||
		!check("stage-steady-payload", copyExecutable(payloadArtifact, steadyPayload)) ||
		!check("write-steady-topology", writeFixtureTopology(steadyTopology, steadyPayload)) ||
		!check("write-steady-manifest", atomicfile.WriteJSON(filepath.Join(steadyTree, "manifest.json"), steadyManifest, 0o600)) ||
		!check("pack-steady-bundle", bundle.Pack(steadyTree, steadyBundlePath)) {
		return finish(report, started)
	}
	steadyDigest, steadySize, err := bundle.SHA256(steadyBundlePath)
	if !check("digest-steady-bundle", err) {
		return finish(report, started)
	}
	newPublicKey, newPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if !check("generate-rotated-trust-key", err) {
		return finish(report, started)
	}
	var metadata, rootMetadata []byte
	var metadataLock sync.RWMutex
	fixtureServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata/root.json":
			metadataLock.RLock()
			selected := append([]byte(nil), rootMetadata...)
			metadataLock.RUnlock()
			if len(selected) == 0 {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(selected)
		case "/metadata/harness/" + hostTarget.String() + "/latest.json":
			metadataLock.RLock()
			selected := append([]byte(nil), metadata...)
			metadataLock.RUnlock()
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(selected)
		case "/releases/" + harnessVersion + "/release-bundle.tar.zst":
			http.ServeFile(writer, request, bundlePath)
		case "/releases/" + steadyHarnessVersion + "/release-bundle.tar.zst":
			http.ServeFile(writer, request, steadyBundlePath)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer fixtureServer.Close()
	now := time.Now().UTC()
	envelope, err := release.SignEnvelope(release.Descriptor{
		Schema: release.ReleaseMetadataSchema, Channel: identity.Channel, Version: harnessVersion,
		Platform: hostTarget.Platform, Arch: hostTarget.Arch,
		Bundle: release.BundleDescriptor{
			Path: "releases/" + harnessVersion + "/release-bundle.tar.zst", Size: size, SHA256: digest,
		},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}, "fixture", privateKey)
	if !check("sign-release-metadata", err) {
		return finish(report, started)
	}
	metadata, err = json.Marshal(envelope)
	if !check("encode-release-metadata", err) {
		return finish(report, started)
	}
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: identity.Channel, Namespace: identity.Namespace, Roots: roots,
		ProtocolFloor: "bootstrap.v1", UpdateOrigins: []string{fixtureServer.URL},
	}
	bootstrap.InitialTrustRoot = config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{ID: "fixture", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}}
	bootstrapPath := filepath.Join(roots.BootstrapRoot, "bootstrap.json")
	if !check("write-bootstrap-config", atomicfile.WriteJSON(bootstrapPath, bootstrap, 0o600)) {
		return finish(report, started)
	}
	logs := filepath.Join(workspace, "reports", "logs")
	if !check("create-log-root", os.MkdirAll(logs, 0o700)) {
		return finish(report, started)
	}
	if !check("genesis-network-launch", runLauncher(ctx, bootstrapLauncher, bootstrapPath, filepath.Join(logs, "genesis.log"), nil)) {
		return finish(report, started)
	}
	confirmed, err := state.Load(paths.StateFile, identity.Channel)
	if !check("load-confirmed-state", err) || !check("downloaded-candidate-confirmed", validateConfirmed(confirmed, harnessVersion)) {
		return finish(report, started)
	}
	rotatedRoot := release.TrustRoot{
		Schema: release.TrustRootSchema, Version: 2, Threshold: 1,
		Keys: []config.TrustKey{{ID: "rotated", PublicKey: base64.StdEncoding.EncodeToString(newPublicKey)}},
	}
	rootEnvelope, err := release.SignTrustRoot(rotatedRoot, "fixture", privateKey)
	if !check("sign-rotated-trust-root", err) {
		return finish(report, started)
	}
	steadyEnvelope, err := release.SignEnvelope(release.Descriptor{
		Schema: release.ReleaseMetadataSchema, Channel: identity.Channel, Version: steadyHarnessVersion,
		Platform: hostTarget.Platform, Arch: hostTarget.Arch,
		Bundle: release.BundleDescriptor{
			Path: "releases/" + steadyHarnessVersion + "/release-bundle.tar.zst",
			Size: steadySize, SHA256: steadyDigest,
		},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}, "rotated", newPrivateKey)
	if !check("sign-steady-release-with-rotated-key", err) {
		return finish(report, started)
	}
	encodedRoot, err := json.Marshal(rootEnvelope)
	if !check("encode-rotated-trust-root", err) {
		return finish(report, started)
	}
	encodedSteady, err := json.Marshal(steadyEnvelope)
	if !check("encode-steady-release", err) {
		return finish(report, started)
	}
	metadataLock.Lock()
	rootMetadata, metadata = encodedRoot, encodedSteady
	metadataLock.Unlock()
	if !check("steady-update-handoff", runLauncher(ctx, bootstrapLauncher, bootstrapPath, filepath.Join(logs, "steady-update.log"), []string{
		"OC_FIXTURE_REQUEST_UPDATE_FROM=" + harnessVersion,
		"OC_FIXTURE_UPDATE_DELAY_MS=750",
		"OC_FIXTURE_LIFETIME_MS=1600",
	})) {
		return finish(report, started)
	}
	confirmed, err = state.Load(paths.StateFile, identity.Channel)
	if !check("load-steady-state", err) || !check("steady-candidate-confirmed", validateConfirmed(confirmed, steadyHarnessVersion)) {
		return finish(report, started)
	}
	var persistedRoot release.TrustRoot
	persistedRootBytes, err := os.ReadFile(paths.TrustRootFile)
	if err == nil {
		err = json.Unmarshal(persistedRootBytes, &persistedRoot)
	}
	if !check("load-rotated-trust-root", err) || !check("rotated-trust-root-active", func() error {
		if persistedRoot.Version != 2 || len(persistedRoot.Keys) != 1 || persistedRoot.Keys[0].ID != "rotated" {
			return fmt.Errorf("unexpected persisted trust root: %+v", persistedRoot)
		}
		return nil
	}()) {
		return finish(report, started)
	}
	fixtureServer.Close()
	if !check("offline-last-good-launch", runLauncher(ctx, bootstrapLauncher, bootstrapPath, filepath.Join(logs, "offline.log"), nil)) {
		return finish(report, started)
	}
	afterOffline, err := state.Load(paths.StateFile, identity.Channel)
	if !check("reload-offline-state", err) {
		return finish(report, started)
	}
	check("offline-state-stable", func() error {
		if afterOffline != confirmed {
			return fmt.Errorf("offline boot changed activation state")
		}
		return nil
	}())

	failingRoot := filepath.Join(paths.Versions, failingHarnessVersion)
	failingLauncher := filepath.Join(failingRoot, "launcher", filepath.Base(versionedLauncher))
	failingPayload := filepath.Join(failingRoot, "payload", filepath.Base(payload))
	failingTopology := filepath.Join(failingRoot, "payload", "runtime-topology.json")
	if !check("install-failing-launcher", copyExecutable(launcherArtifact, failingLauncher)) ||
		!check("install-failing-payload", copyExecutable(payloadArtifact, failingPayload)) ||
		!check("write-failing-topology", writeFixtureTopology(failingTopology, failingPayload)) {
		return finish(report, started)
	}
	failingManifest := manifest
	failingManifest.Version = failingHarnessVersion
	failingManifest.PublishedAt = time.Now().UTC()
	if !check("write-failing-manifest", atomicfile.WriteJSON(filepath.Join(failingRoot, "manifest.json"), failingManifest, 0o600)) {
		return finish(report, started)
	}
	failingCandidate, err := state.Prepare(confirmed, identity.Channel, failingHarnessVersion)
	if !check("prepare-failing-candidate", err) || !check("persist-failing-candidate", state.Save(paths.StateFile, identity.Channel, failingCandidate)) {
		return finish(report, started)
	}
	failure := runLauncher(ctx, bootstrapLauncher, bootstrapPath, filepath.Join(logs, "rollback.log"), []string{
		"OC_FIXTURE_READY_DELAY_MS=2000", "OC_FIXTURE_LIFETIME_MS=200",
	})
	check("reject-pre-ready-exit", func() error {
		if failure == nil {
			return fmt.Errorf("failing candidate unexpectedly succeeded")
		}
		return nil
	}())
	rolledBack, err := state.Load(paths.StateFile, identity.Channel)
	if !check("load-rolled-back-state", err) {
		return finish(report, started)
	}
	check("rollback-retained-last-good", func() error {
		if rolledBack.Active != steadyHarnessVersion || rolledBack.LastGood != steadyHarnessVersion || rolledBack.Candidate != "" {
			encoded, _ := json.Marshal(rolledBack)
			return fmt.Errorf("unexpected rolled-back state %s", encoded)
		}
		return nil
	}())
	return finish(report, started)
}

func writeFixtureTopology(filename, payload string) error {
	return runtimetopology.Write(filename, runtimetopology.Topology{Schema: runtimetopology.Schema, Processes: []runtimetopology.Process{{
		App: "fixture-runtime", Command: filepath.Base(payload), WorkingDirectory: ".",
		Capabilities: []protocol.Capability{protocol.CapabilityUpdateTransition},
	}}})
}

func runLauncher(ctx context.Context, launcherPath, bootstrapPath, logPath string, extraEnv []string) error {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	launchContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return lifecycle.Run(launchContext, lifecycle.BootstrapProcess(launcherPath, bootstrapPath, lifecycle.ProcessSpec{
		Stdout: logFile, Stderr: logFile, Env: append(os.Environ(), extraEnv...),
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
	}))
}

func validateConfirmed(runtimeState state.Runtime, expected string) error {
	if runtimeState.Active != expected || runtimeState.LastGood != expected || runtimeState.Candidate != "" {
		encoded, _ := json.Marshal(runtimeState)
		return fmt.Errorf("unexpected confirmed state %s", encoded)
	}
	return nil
}

func copyExecutable(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
