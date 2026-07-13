package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

const fullPackReadyTimeout = 45 * time.Second

const fullPackDriver = `const [clientURL, supervisorURL, resourcesRoot] = process.argv.slice(2);
const { SidecarConnection, loadSidecarLaunch } = await import(clientURL);
const { startPayloadChildren } = await import(supervisorURL);
const launch = loadSidecarLaunch();
let runtime;
let children;
let stopping = false;
async function stop(code = 0) {
  if (stopping) return;
  stopping = true;
  await children?.stop();
  runtime?.close(code);
  setTimeout(() => process.exit(code), 25);
}
runtime = await SidecarConnection.connect({
  app: "electron",
  launch,
  onCommand: async (command) => {
    if (command === "shutdown") await stop();
  },
});
children = await startPayloadChildren(resourcesRoot, runtime, launch);
runtime.ready();
process.once("SIGINT", () => void stop(130));
process.once("SIGTERM", () => void stop(143));
`

func RunFullPack(ctx context.Context, workspace, bundlePath string) Report {
	started := time.Now()
	report := Report{Schema: 1, Scenario: "electron-full-pack-sidecar-tree"}
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
	manifest, err := release.LoadManifest(filepath.Join(versionRoot, "manifest.json"))
	if !check("load-release-manifest", err) || !check("manifest-matches-host", manifest.ValidateHost(manifest.Channel, "bootstrap.v1")) {
		return finish(report, started)
	}
	payloadEntry, err := release.ResolveEntry(versionRoot, manifest.Payload.Entry, "payload")
	if !check("resolve-opaque-payload-entry", err) {
		return finish(report, started)
	}
	resourcesRoot, err := fullPackResources(payloadEntry)
	if !check("resolve-electron-resources", err) {
		return finish(report, started)
	}
	payloadResources := filepath.Join(resourcesRoot, "payload")
	topologyPath := filepath.Join(payloadResources, "payload-topology.json")
	topology, err := loadHarnessTopology(topologyPath)
	if !check("load-generated-topology", err) || !check("topology-has-web-api", requireApps(topology, "api", "web")) {
		return finish(report, started)
	}
	supervisor := filepath.Join(resourcesRoot, "app", "dist", "sidecar", "supervisor.js")
	clientModule := filepath.Join(resourcesRoot, "app", "node_modules", "@open-cut", "sidecar-client", "dist", "index.js")
	for name, path := range map[string]string{"packaged-supervisor-exists": supervisor, "packaged-client-exists": clientModule} {
		info, statErr := os.Stat(path)
		if statErr == nil && !info.Mode().IsRegular() {
			statErr = fmt.Errorf("%s is not a regular file", path)
		}
		if !check(name, statErr) {
			return finish(report, started)
		}
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
	runtimeToken, err := cellBroker.MintRuntimeToken("payload", time.Minute)
	if !check("mint-runtime-capability", err) {
		return finish(report, started)
	}
	descriptor, err := json.Marshal(cellBroker.Descriptor())
	if !check("encode-control-descriptor", err) {
		return finish(report, started)
	}
	driver := filepath.Join(workspace, "full-pack-driver.mjs")
	if !check("write-full-pack-driver", os.WriteFile(driver, []byte(fullPackDriver), 0o600)) {
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
	command := exec.CommandContext(ctx, payloadEntry, driver, fileURL(clientModule), fileURL(supervisor), payloadResources)
	command.Dir, command.Stdout, command.Stderr = versionRoot, logFile, logFile
	command.Env = append(os.Environ(),
		"ELECTRON_RUN_AS_NODE=1",
		"OC_PAYLOAD_RESOURCES="+payloadResources,
		"OC_SIDECAR_CONTROL="+string(descriptor),
		"OC_SIDECAR_TOKEN="+runtimeToken,
		"OC_SIDECAR_CHANNEL="+identity.Channel,
		"OC_SIDECAR_NAMESPACE="+identity.Namespace,
		"OC_SIDECAR_MODE=harness",
		"OC_SIDECAR_SOURCE=oc-control",
	)
	if !check("start-packaged-electron-runtime", command.Start()) {
		return finish(report, started)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}()
	for _, subject := range append([]string{"payload"}, topology...) {
		readyContext, cancel := context.WithTimeout(ctx, fullPackReadyTimeout)
		ready := make(chan error, 1)
		go func() { ready <- cellBroker.WaitReady(readyContext, subject) }()
		var readyErr error
		select {
		case readyErr = <-ready:
		case exitErr := <-exited:
			command.Process = nil
			readyErr = fmt.Errorf("packaged runtime exited before %s READY: %v", subject, exitErr)
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
	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if !check("owner-rendezvous", err) {
		return finish(report, started)
	}
	status, err := owner.Status(ctx)
	if !check("packaged-tree-status", err) || !check("three-scoped-ready-sessions", validateFullPackStatus(status)) {
		return finish(report, started)
	}
	response, err := owner.Control(ctx, "shutdown")
	if !check("broadcast-shutdown", err) || !check("shutdown-reached-runtime-tree", acceptedSessions(response, 3)) {
		return finish(report, started)
	}
	select {
	case exitErr := <-exited:
		command.Process = nil
		check("packaged-runtime-clean-exit", exitErr)
	case <-time.After(10 * time.Second):
		check("packaged-runtime-clean-exit", errors.New("packaged runtime did not exit after shutdown"))
	}
	return finish(report, started)
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

func fullPackResources(payloadEntry string) (string, error) {
	var resources string
	if runtime.GOOS == "darwin" {
		resources = filepath.Join(filepath.Dir(filepath.Dir(payloadEntry)), "Resources")
	} else {
		resources = filepath.Join(filepath.Dir(payloadEntry), "resources")
	}
	info, err := os.Stat(resources)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Electron resources path is not a directory")
	}
	return resources, nil
}

func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func loadHarnessTopology(path string) ([]string, error) {
	var document struct {
		Schema   int `json:"schema"`
		Sidecars []struct {
			App   string `json:"app"`
			Entry string `json:"entry"`
		} `json:"sidecars"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if document.Schema != 1 || len(document.Sidecars) == 0 {
		return nil, fmt.Errorf("invalid payload topology")
	}
	apps := make([]string, 0, len(document.Sidecars))
	for _, sidecar := range document.Sidecars {
		if sidecar.App == "" || sidecar.Entry == "" {
			return nil, fmt.Errorf("invalid payload topology sidecar")
		}
		apps = append(apps, sidecar.App)
	}
	sort.Strings(apps)
	return apps, nil
}

func requireApps(actual []string, expected ...string) error {
	sort.Strings(expected)
	if fmt.Sprint(actual) != fmt.Sprint(expected) {
		return fmt.Errorf("topology apps = %v, want %v", actual, expected)
	}
	return nil
}

func validateFullPackStatus(status protocol.Status) error {
	if len(status.Sessions) != 3 {
		return fmt.Errorf("got %d sessions, want 3", len(status.Sessions))
	}
	seen := make(map[string]protocol.SessionStatus)
	for _, session := range status.Sessions {
		if !session.Ready {
			return fmt.Errorf("session %s is not ready", session.Subject)
		}
		seen[session.Subject] = session
	}
	if seen["payload"].App != "electron" {
		return fmt.Errorf("opaque payload session did not register the Electron carrier")
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
