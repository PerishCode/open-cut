package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

type childProcess struct {
	app    string
	cmd    *exec.Cmd
	log    *os.File
	exited chan error
}

func RunSidecars(ctx context.Context, workspace, repositoryRoot string) Report {
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

	descriptorJSON, err := json.Marshal(cellBroker.Descriptor())
	if !check("encode-control-descriptor", err) {
		return finish(report, started)
	}
	logRoot := filepath.Join(workspace, "reports", "logs")
	if !check("create-log-root", os.MkdirAll(logRoot, 0o700)) {
		return finish(report, started)
	}
	children := make([]*childProcess, 0, 2)
	defer func() {
		for _, child := range children {
			if child.cmd.Process != nil {
				_ = child.cmd.Process.Kill()
			}
			child.log.Close()
		}
	}()

	for _, app := range []string{"api", "web"} {
		token, tokenErr := cellBroker.MintSidecarToken(app, time.Minute)
		if !check("mint-"+app+"-capability", tokenErr) {
			return finish(report, started)
		}
		entry := filepath.Join(repositoryRoot, "apps", app, "dist", "sidecar", "index.js")
		if _, statErr := os.Stat(entry); !check(app+"-entry-exists", statErr) {
			return finish(report, started)
		}
		logFile, logErr := os.OpenFile(filepath.Join(logRoot, app+".log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if !check("open-"+app+"-log", logErr) {
			return finish(report, started)
		}
		command := exec.CommandContext(ctx, "node", entry)
		command.Dir = repositoryRoot
		command.Stdout, command.Stderr = logFile, logFile
		command.Env = append(os.Environ(),
			"OC_SIDECAR_CONTROL="+string(descriptorJSON),
			"OC_SIDECAR_TOKEN="+token,
			"OC_SIDECAR_CHANNEL="+identity.Channel,
			"OC_SIDECAR_NAMESPACE="+identity.Namespace,
			"OC_SIDECAR_MODE=harness",
			"OC_SIDECAR_SOURCE=oc-control",
		)
		if !check("start-"+app+"-entry", command.Start()) {
			logFile.Close()
			return finish(report, started)
		}
		child := &childProcess{app: app, cmd: command, log: logFile, exited: make(chan error, 1)}
		children = append(children, child)
		go func() { child.exited <- child.cmd.Wait() }()
	}

	for _, child := range children {
		readyContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := cellBroker.WaitReady(readyContext, child.app)
		cancel()
		if !check(child.app+"-ready", err) {
			return finish(report, started)
		}
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
	response, err := owner.Control(ctx, "shutdown")
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
			child.cmd.Process = nil
		case <-time.After(5 * time.Second):
			check(child.app+"-clean-exit", errors.New("sidecar did not exit after shutdown"))
		}
	}
	return finish(report, started)
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
