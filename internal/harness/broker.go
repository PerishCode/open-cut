package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type Report struct {
	Schema     int     `json:"schema"`
	Scenario   string  `json:"scenario"`
	Passed     bool    `json:"passed"`
	DurationMS int64   `json:"durationMs"`
	Checks     []Check `json:"checks"`
}

func RunBroker(ctx context.Context, workspace string) Report {
	started := time.Now()
	report := Report{Schema: 1, Scenario: "broker-registration-ready"}
	check := func(name string, err error) bool {
		entry := Check{Name: name, Passed: err == nil}
		if err != nil {
			entry.Detail = err.Error()
		}
		report.Checks = append(report.Checks, entry)
		return err == nil
	}

	identity, err := cell.New("harness", "default")
	if !check("cell-identity", err) {
		return finish(report, started)
	}
	roots := config.RootSet{
		BootstrapRoot: filepath.Join(workspace, "bootstrap"),
		StoreRoot:     filepath.Join(workspace, "roots", "store"),
		CacheRoot:     filepath.Join(workspace, "roots", "cache"),
		RuntimeRoot:   filepath.Join(workspace, "roots", "runtime"),
		LogRoot:       filepath.Join(workspace, "roots", "logs"),
	}
	paths, err := layout.Resolve(roots, identity)
	if !check("root-layout", err) {
		return finish(report, started)
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1, HeartbeatTimeout: 3 * time.Second})
	if !check("broker-start", err) {
		return finish(report, started)
	}
	defer cellBroker.Close()
	second, secondErr := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1})
	if second != nil {
		second.Close()
	}
	check("single-instance", func() error {
		if !errors.Is(secondErr, broker.ErrAlreadyRunning) {
			return errors.New("second broker did not return ErrAlreadyRunning")
		}
		return nil
	}())

	token, err := cellBroker.MintSidecarToken("web", time.Minute)
	if !check("mint-scoped-capability", err) {
		return finish(report, started)
	}
	session, err := client.DialSession(ctx, cellBroker.Descriptor(), token, client.Registration{
		Channel: identity.Channel, Namespace: identity.Namespace, App: "web", Mode: protocol.LifecycleModeHarness, Source: "oc-control",
	})
	if !check("websocket-register", err) {
		return finish(report, started)
	}
	defer session.Close(0)
	if !check("publish-endpoint", session.Endpoint("http", "http://127.0.0.1:4010")) {
		return finish(report, started)
	}
	if !check("ready-event", session.Ready()) {
		return finish(report, started)
	}
	readyContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if !check("broker-observed-ready", cellBroker.WaitReady(readyContext, "web")) {
		return finish(report, started)
	}
	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if !check("owner-rendezvous", err) {
		return finish(report, started)
	}
	status, err := owner.Status(ctx)
	if !check("authenticated-status", err) {
		return finish(report, started)
	}
	check("status-isolated-cell", func() error {
		if status.Channel != identity.Channel || status.Namespace != identity.Namespace || len(status.Sessions) != 1 || !status.Sessions[0].Ready {
			return errors.New("status did not contain the ready harness session")
		}
		return nil
	}())
	ownerToken, err := os.ReadFile(paths.OwnerTokenFile)
	if !check("read-owner-capability", err) {
		return finish(report, started)
	}
	otherIdentities := []cell.Identity{
		{Channel: "harness", Namespace: "other"},
		{Channel: "preview", Namespace: "default"},
	}
	addresses := map[string]bool{cellBroker.Descriptor().Address: true}
	for _, otherIdentity := range otherIdentities {
		otherPaths, resolveErr := layout.Resolve(roots, otherIdentity)
		if !check("matrix-root-layout", resolveErr) {
			return finish(report, started)
		}
		otherBroker, startErr := broker.Start(broker.Options{Identity: otherIdentity, Paths: otherPaths, Generation: 1})
		if !check("matrix-broker-start", startErr) {
			return finish(report, started)
		}
		defer otherBroker.Close()
		check("matrix-distinct-listener", func() error {
			if addresses[otherBroker.Descriptor().Address] {
				return errors.New("two cells shared a listener")
			}
			addresses[otherBroker.Descriptor().Address] = true
			return nil
		}())
		_, crossOwnerErr := client.New(otherBroker.Descriptor(), strings.TrimSpace(string(ownerToken))).Status(ctx)
		check("matrix-owner-token-isolated", func() error {
			if crossOwnerErr == nil {
				return errors.New("owner capability crossed cell boundary")
			}
			return nil
		}())
		otherToken, tokenErr := otherBroker.MintRuntimeToken("payload", time.Minute)
		if !check("matrix-mint-runtime", tokenErr) {
			return finish(report, started)
		}
		_, crossRuntimeErr := client.New(cellBroker.Descriptor(), otherToken).Status(ctx)
		check("matrix-runtime-token-isolated", func() error {
			if crossRuntimeErr == nil {
				return errors.New("runtime capability crossed cell boundary")
			}
			return nil
		}())
	}
	return finish(report, started)
}

func finish(report Report, started time.Time) Report {
	report.DurationMS = time.Since(started).Milliseconds()
	report.Passed = true
	for _, check := range report.Checks {
		if !check.Passed {
			report.Passed = false
			break
		}
	}
	return report
}
