package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/devsession"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/environment"
)

type childProcess struct {
	app     string
	process *lifecycle.Process
	log     *os.File
	exited  chan error
}

func RunSidecars(ctx context.Context, workspaceRoot, repositoryRoot string) Report {
	started := time.Now()
	report := Report{Schema: 1, Scenario: "web-api-sidecar-entries"}
	check := func(name string, err error) bool {
		entry := Check{Name: name, Passed: err == nil}
		if err != nil {
			entry.Detail = err.Error()
		}
		report.Checks = append(report.Checks, entry)
		return err == nil
	}

	identity, err := cell.New("harness", "sidecars")
	if !check("cell-identity", err) {
		return finish(report, started)
	}
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(workspaceRoot, "bootstrap"), StoreRoot: filepath.Join(workspaceRoot, "roots", "store"),
		CacheRoot: filepath.Join(workspaceRoot, "roots", "cache"), RuntimeRoot: filepath.Join(workspaceRoot, "roots", "runtime"),
		LogRoot: filepath.Join(workspaceRoot, "roots", "logs"),
	}, identity)
	if !check("root-layout", err) {
		return finish(report, started)
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1, HeartbeatTimeout: 10 * time.Second})
	if !check("broker-start", err) {
		return finish(report, started)
	}
	defer cellBroker.Close()
	dataDir := filepath.Join(workspaceRoot, identity.Suffix())

	logRoot := filepath.Join(workspaceRoot, "reports", "logs")
	if !check("create-log-root", os.MkdirAll(logRoot, 0o700)) {
		return finish(report, started)
	}
	controlConfig, err := workspace.Load(repositoryRoot)
	if !check("load-workspace", err) {
		return finish(report, started)
	}
	installation, err := harnessInstallation(workspaceRoot, controlConfig.InstallationKeyRoles)
	if !check("installation-identity", err) {
		return finish(report, started)
	}
	topology, err := workspace.DiscoverTopology(repositoryRoot, controlConfig)
	if !check("discover-sidecars", err) {
		return finish(report, started)
	}
	selected, err := selectSidecars(topology, "api", "web")
	if !check("select-sidecars", err) {
		return finish(report, started)
	}
	plan, err := devsession.ResolvePlan(repositoryRoot, controlConfig, selected)
	if !check("resolve-runtime-plan", err) {
		return finish(report, started)
	}

	children := make([]*childProcess, 0, len(plan.Processes))
	defer func() {
		for _, child := range children {
			_ = child.process.Kill()
			child.log.Close()
		}
	}()
	for _, definition := range plan.Processes {
		app := definition.App
		token, tokenErr := cellBroker.MintSidecarToken(app, time.Minute)
		if !check("mint-"+app+"-capability", tokenErr) {
			return finish(report, started)
		}
		launchEnvironment, launchErr := protocol.LaunchEnvironmentMap(protocol.SidecarLaunch{
			App: app, Control: cellBroker.Descriptor(), Token: token, Channel: identity.Channel,
			Namespace: identity.Namespace, DataDir: dataDir, Installation: installation, Mode: protocol.LifecycleModeHarness,
			Presentation: protocol.PresentationHeadless, Source: "oc-control",
		})
		if !check("encode-"+app+"-launch-envelope", launchErr) {
			return finish(report, started)
		}
		if _, statErr := os.Stat(definition.Command); !check(app+"-entry-exists", statErr) {
			return finish(report, started)
		}
		logFile, logErr := os.OpenFile(filepath.Join(logRoot, app+".log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if !check("open-"+app+"-log", logErr) {
			return finish(report, started)
		}
		process, startErr := lifecycle.Start(ctx, lifecycle.ProcessSpec{
			Executable: definition.Command, Args: definition.Args, Directory: definition.WorkingDirectory,
			Stdout: logFile, Stderr: logFile, Profile: lifecycle.ProfileHarness,
			Presentation: lifecycle.PresentationHeadless,
			Sandbox:      definition.Sandbox,
			Env:          environment.Merge(os.Environ(), definition.UnsetEnv, definition.Env, launchEnvironment),
		})
		if !check("start-"+app+"-entry", startErr) {
			logFile.Close()
			return finish(report, started)
		}
		child := &childProcess{app: app, process: process, log: logFile, exited: make(chan error, 1)}
		children = append(children, child)
		go func() { child.exited <- child.process.Wait() }()
	}

	for _, child := range children {
		readyContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := cellBroker.WaitReady(readyContext, child.app)
		cancel()
		if !check(child.app+"-ready", err) {
			return finish(report, started)
		}
	}
	if !check("api-sqlite-at-ready", func() error {
		info, err := os.Stat(filepath.Join(dataDir, "api", "database", "open-cut.db"))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("API database is not a regular file")
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
	if !check("status", err) {
		return finish(report, started)
	}
	check("two-independent-sidecars", validateSidecarStatus(status))
	if !check("api-http-health-at-ready", validateAPIHealth(ctx, status)) {
		return finish(report, started)
	}
	response, err := owner.Control(ctx, protocol.ControlCommandShutdown)
	if !check("broadcast-shutdown", err) {
		return finish(report, started)
	}
	check("shutdown-reached-both", func() error {
		if response.Accepted != 2 {
			return fmt.Errorf("shutdown accepted by %d sessions", response.Accepted)
		}
		return nil
	}())
	for _, child := range children {
		select {
		case exitErr := <-child.exited:
			check(child.app+"-clean-exit", exitErr)
		case <-time.After(5 * time.Second):
			check(child.app+"-clean-exit", errors.New("sidecar did not exit after shutdown"))
		}
	}
	return finish(report, started)
}

func selectSidecars(topology workspace.Topology, apps ...string) (workspace.Topology, error) {
	wanted := make(map[string]bool, len(apps))
	for _, app := range apps {
		wanted[app] = true
	}
	selected := workspace.Topology{Schema: topology.Schema}
	for _, sidecar := range topology.Sidecars {
		if wanted[sidecar.App] {
			selected.Sidecars = append(selected.Sidecars, sidecar)
			delete(wanted, sidecar.App)
		}
	}
	if len(wanted) != 0 {
		missing := make([]string, 0, len(wanted))
		for app := range wanted {
			missing = append(missing, app)
		}
		sort.Strings(missing)
		return workspace.Topology{}, fmt.Errorf("runtime topology is missing sidecars %v", missing)
	}
	return selected, nil
}

func validateSidecarStatus(status protocol.Status) error {
	if len(status.Sessions) != 2 {
		return fmt.Errorf("got %d sessions, want 2", len(status.Sessions))
	}
	seen := make(map[string]bool)
	for _, session := range status.Sessions {
		if !session.Ready || len(session.Endpoints) != 1 || session.Endpoints[0].Name != "http" {
			return fmt.Errorf("session %s did not publish one ready HTTP endpoint", session.App)
		}
		seen[session.App] = true
	}
	if !seen["web"] || !seen["api"] {
		return fmt.Errorf("status did not contain both web and api")
	}
	return nil
}

func validateAPIHealth(ctx context.Context, status protocol.Status) error {
	var endpoint string
	for _, session := range status.Sessions {
		if session.App == "api" && len(session.Endpoints) == 1 && session.Endpoints[0].Name == "http" {
			endpoint = session.Endpoints[0].URL
			break
		}
	}
	if endpoint == "" {
		return fmt.Errorf("API endpoint is unavailable")
	}
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint+"/v1/health", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("API health returned %s", response.Status)
	}
	var health struct {
		OK      bool   `json:"ok"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return err
	}
	if !health.OK || health.Service != "api" {
		return fmt.Errorf("API health response is not ready")
	}
	return nil
}
