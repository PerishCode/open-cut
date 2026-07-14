package runtimehost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

func TestAllReadyUsesAppIdentity(t *testing.T) {
	status := protocol.Status{Sessions: []protocol.SessionStatus{
		{Subject: "payload", App: "runtime", Ready: false},
		{Subject: "api", App: "api", Ready: true},
		{Subject: "web", App: "web", Ready: true},
	}}
	if !allReady(status, []string{"api", "web"}) {
		t.Fatal("ready peers were not recognized")
	}
	if allReady(status, []string{"api", "electron", "web"}) {
		t.Fatal("missing peer was accepted")
	}
}

func TestRuntimeHostRestartsPeerBeforeInitialReady(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	identity, _ := cell.New("test", "runtime-restart")
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(base, "bootstrap"), StoreRoot: filepath.Join(base, "store"),
		CacheRoot: filepath.Join(base, "cache"), RuntimeRoot: filepath.Join(base, "runtime"), LogRoot: filepath.Join(base, "logs"),
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	runtimeToken, err := cellBroker.MintRuntimeToken("payload", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(base, "starts.txt")
	plan := runtimetopology.Plan{Processes: []runtimetopology.ResolvedProcess{{
		App: "recovering-peer", Command: executable,
		Args: []string{"-test.run=^TestRuntimeHostHelperProcess$"}, WorkingDirectory: base,
		Env: map[string]string{"OC_RUNTIMEHOST_HELPER": "1", "OC_RUNTIMEHOST_COUNTER": counter},
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready := make(chan Result, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Descriptor: cellBroker.Descriptor(), Token: runtimeToken,
			Channel: identity.Channel, Namespace: identity.Namespace, App: "runtime",
			Mode: protocol.LifecycleModeHarness, Presentation: protocol.PresentationHeadless, Source: "harness",
			Plan: plan, ReadyTimeout: 5 * time.Second,
		}, ready)
	}()
	select {
	case result := <-ready:
		if !allReady(result.Status, []string{"recovering-peer"}) {
			t.Fatalf("recovered runtime was not ready: %+v", result.Status)
		}
	case err := <-done:
		t.Fatalf("runtime stopped before recovery: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	startsBytes, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	starts, err := strconv.Atoi(strings.TrimSpace(string(startsBytes)))
	if err != nil || starts < 2 {
		t.Fatalf("peer start count = %q, want at least 2", startsBytes)
	}
	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Control(context.Background(), protocol.ControlCommandShutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestRuntimeHostHelperProcess(t *testing.T) {
	if os.Getenv("OC_RUNTIMEHOST_HELPER") != "1" {
		return
	}
	counter := os.Getenv("OC_RUNTIMEHOST_COUNTER")
	starts := 0
	if data, err := os.ReadFile(counter); err == nil {
		starts, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	starts++
	if err := os.WriteFile(counter, []byte(strconv.Itoa(starts)), 0o600); err != nil {
		os.Exit(90)
	}
	if starts == 1 {
		os.Exit(23)
	}
	var descriptor protocol.ControlDescriptor
	if err := json.Unmarshal([]byte(os.Getenv(protocol.SidecarEnvControl)), &descriptor); err != nil {
		os.Exit(91)
	}
	session, err := client.DialSession(context.Background(), descriptor, os.Getenv(protocol.SidecarEnvToken), client.Registration{
		Channel: os.Getenv(protocol.SidecarEnvChannel), Namespace: os.Getenv(protocol.SidecarEnvNamespace),
		App: os.Getenv(protocol.SidecarEnvApp), Mode: protocol.LifecycleMode(os.Getenv(protocol.SidecarEnvMode)),
		Source: os.Getenv(protocol.SidecarEnvSource),
	})
	if err != nil {
		os.Exit(92)
	}
	if err := session.Ready(); err != nil {
		os.Exit(93)
	}
	for {
		command, err := session.ReadCommand(context.Background())
		if err != nil {
			os.Exit(94)
		}
		if command == protocol.ControlCommandShutdown {
			_ = session.Close(0)
			os.Exit(0)
		}
	}
}
