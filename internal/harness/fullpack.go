package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/environment"
)

const fullPackReadyTimeout = 45 * time.Second

func RunFullPack(ctx context.Context, workspace, repositoryRoot, bundlePath string) Report {
	started := time.Now()
	report := Report{Schema: 1, Scenario: "runtime-topology-peer-sidecars"}
	check := func(name string, err error) bool {
		entry := Check{Name: name, Passed: err == nil}
		if err != nil {
			entry.Detail = err.Error()
		}
		report.Checks = append(report.Checks, entry)
		return err == nil
	}

	versionRoot := filepath.Join(workspace, "extracted-release")
	if !check("extract-bundle", bundle.Extract(bundlePath, versionRoot)) {
		return finish(report, started)
	}
	manifestPath := filepath.Join(versionRoot, "manifest.json")
	manifest, err := release.LoadManifest(manifestPath)
	if !check("load-release-manifest", err) || !check("manifest-matches-host", manifest.ValidateHost(manifest.Channel, "bootstrap.v1")) {
		return finish(report, started)
	}
	launcherEntry, err := release.ResolveEntry(versionRoot, manifest.Launcher.Entry, "launcher")
	if !check("resolve-versioned-launcher", err) {
		return finish(report, started)
	}
	topologyEntry, err := release.ResolveEntry(versionRoot, manifest.Payload.Entry, "payload")
	if !check("resolve-opaque-runtime-topology", err) {
		return finish(report, started)
	}
	plan, err := runtimetopology.Resolve(topologyEntry)
	if !check("load-generated-runtime-topology", err) || !check("topology-has-peer-apps", requireApps(runtimetopology.Apps(plan), "api", "electron", "web")) {
		return finish(report, started)
	}
	_, err = release.ResolveCLI(versionRoot, manifest)
	if !check("resolve-versioned-product-cli", err) {
		return finish(report, started)
	}

	identity, err := cell.New(manifest.Channel, "full-pack")
	if !check("cell-identity", err) {
		return finish(report, started)
	}
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(workspace, "bootstrap"), StoreRoot: filepath.Join(workspace, "roots", "store"),
		CacheRoot: filepath.Join(workspace, "roots", "cache"), RuntimeRoot: filepath.Join(workspace, "roots", "runtime"),
		LogRoot: filepath.Join(workspace, "roots", "logs"),
	}, identity)
	if !check("root-layout", err) {
		return finish(report, started)
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1, HeartbeatTimeout: 10 * time.Second})
	if !check("broker-start", err) {
		return finish(report, started)
	}
	defer cellBroker.Close()
	dataDir := filepath.Join(workspace, identity.Suffix())
	controlConfig, err := workspaceConfig(repositoryRoot)
	if !check("load-workspace", err) {
		return finish(report, started)
	}
	installationIdentity, err := harnessInstallationIdentity(workspace, controlConfig.InstallationKeyRoles)
	if !check("installation-identity", err) {
		return finish(report, started)
	}
	// Keep the endpoint seed near the harness root: Darwin's sockaddr path limit
	// is shorter than a fully expanded cell runtime path. Windows derives a named
	// pipe from the same seed so Node and Go share one IPC transport.
	signer, err := lifecycle.StartDevelopmentSigner(filepath.Join(workspace, "signer.sock"), installationIdentity)
	if !check("start-lifecycle-signer", err) {
		return finish(report, started)
	}
	defer signer.Close()
	runtimeToken, err := cellBroker.MintRuntimeToken("payload", time.Minute)
	if !check("mint-runtime-capability", err) {
		return finish(report, started)
	}
	launchEnvironment, err := protocol.AppendLaunchEnvironment(environment.Merge(
		os.Environ(), nil, map[string]string{lifecycle.SignerSocketEnvironment: signer.Socket()},
	), protocol.SidecarLaunch{
		App: "runtime", Control: cellBroker.Descriptor(), Token: runtimeToken, Channel: identity.Channel,
		Namespace: identity.Namespace, DataDir: dataDir, Installation: installationIdentity.Assertion(), Mode: protocol.LifecycleModeHarness,
		Presentation: protocol.PresentationHeadless, Source: "oc-control",
	})
	if !check("encode-sidecar-launch-envelope", err) {
		return finish(report, started)
	}
	logs := filepath.Join(workspace, "reports", "logs")
	if !check("create-log-root", os.MkdirAll(logs, 0o700)) {
		return finish(report, started)
	}
	logPath := filepath.Join(logs, "full-pack.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if !check("open-full-pack-log", err) {
		return finish(report, started)
	}
	defer logFile.Close()
	command, startErr := lifecycle.Start(ctx, lifecycle.VersionedProcess(launcherEntry, manifestPath, lifecycle.ProcessSpec{
		Directory:    versionRoot,
		Stdout:       logFile,
		Stderr:       logFile,
		Profile:      lifecycle.ProfileHarness,
		Presentation: lifecycle.PresentationHeadless,
		Env:          launchEnvironment,
	}))
	if !check("start-versioned-runtime-runner", startErr) {
		return finish(report, started)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	defer func() {
		_ = command.Kill()
	}()
	for _, subject := range []string{"payload", "api", "electron", "web"} {
		readyContext, cancel := context.WithTimeout(ctx, fullPackReadyTimeout)
		ready := make(chan error, 1)
		go func() { ready <- cellBroker.WaitReady(readyContext, subject) }()
		var readyErr error
		select {
		case readyErr = <-ready:
		case exitErr := <-exited:
			readyErr = fmt.Errorf("runtime runner exited before %s READY: %v", subject, exitErr)
		}
		cancel()
		if readyErr != nil {
			_ = logFile.Sync()
			if tail, tailErr := readLogTail(logPath, 16*1024); tailErr == nil {
				readyErr = fmt.Errorf("%w; full-pack.log tail:\n%s", readyErr, tail)
			}
		}
		if !check(subject+"-ready", readyErr) {
			return finish(report, started)
		}
	}
	if !check("packaged-api-sqlite-at-ready", func() error {
		info, err := os.Stat(filepath.Join(dataDir, "api", "database", "open-cut.db"))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("packaged API database is not a regular file")
		}
		return nil
	}()) {
		return finish(report, started)
	}
	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if !check("owner-rendezvous", err) {
		return finish(report, started)
	}
	status, err := owner.Status(ctx)
	if !check("packaged-tree-status", err) || !check("four-peer-ready-sessions", validateFullPackStatus(status)) {
		return finish(report, started)
	}
	if !check("packaged-api-http-health-at-ready", validateAPIHealth(ctx, status)) {
		return finish(report, started)
	}
	response, err := owner.Control(ctx, protocol.ControlCommandShutdown)
	if !check("broadcast-shutdown", err) || !check("shutdown-reached-runtime-tree", acceptedSessions(response, 4)) {
		return finish(report, started)
	}
	select {
	case exitErr := <-exited:
		check("runtime-runner-clean-exit", exitErr)
	case <-time.After(10 * time.Second):
		check("runtime-runner-clean-exit", errors.New("runtime runner did not exit after shutdown"))
	}
	return finish(report, started)
}

func workspaceConfig(repositoryRoot string) (workspace.Config, error) {
	return workspace.Load(repositoryRoot)
}

func readLogTail(path string, maximum int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > maximum {
		data = data[len(data)-maximum:]
	}
	tail := strings.TrimSpace(string(data))
	if tail == "" {
		return "(empty)", nil
	}
	return tail, nil
}

func requireApps(actual []string, expected ...string) error {
	sort.Strings(actual)
	sort.Strings(expected)
	if fmt.Sprint(actual) != fmt.Sprint(expected) {
		return fmt.Errorf("topology apps = %v, want %v", actual, expected)
	}
	return nil
}

func validateFullPackStatus(status protocol.Status) error {
	if len(status.Sessions) != 4 {
		return fmt.Errorf("got %d sessions, want 4", len(status.Sessions))
	}
	seen := make(map[string]protocol.SessionStatus)
	for _, session := range status.Sessions {
		if !session.Ready {
			return fmt.Errorf("session %s is not ready", session.Subject)
		}
		seen[session.Subject] = session
	}
	if seen["payload"].App != "runtime" {
		return fmt.Errorf("aggregate payload session did not register the generic runtime runner")
	}
	if seen["electron"].App != "electron" {
		return fmt.Errorf("Electron did not register as an independent peer")
	}
	for _, app := range []string{"api", "web"} {
		session, ok := seen[app]
		if !ok || len(session.Endpoints) != 1 || session.Endpoints[0].Name != "http" {
			return fmt.Errorf("%s did not publish one HTTP endpoint", app)
		}
	}
	return nil
}

func acceptedSessions(response protocol.ControlResponse, expected int) error {
	if response.Accepted != expected {
		return fmt.Errorf("shutdown accepted by %d sessions, want %d", response.Accepted, expected)
	}
	return nil
}
