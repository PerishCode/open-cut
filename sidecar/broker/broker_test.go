package broker

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

func TestBrokerRegistrationReadyAndSingleInstance(t *testing.T) {
	base := t.TempDir()
	identity, err := cell.New("beta", "test")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(base, "bootstrap"), StoreRoot: filepath.Join(base, "store"),
		CacheRoot: filepath.Join(base, "cache"), RuntimeRoot: filepath.Join(base, "runtime"), LogRoot: filepath.Join(base, "logs"),
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	cellBroker, err := Start(Options{Identity: identity, Paths: paths, Generation: 7, HeartbeatTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	if _, err := Start(Options{Identity: identity, Paths: paths, Generation: 7}); err != ErrAlreadyRunning {
		t.Fatalf("second Start() error = %v, want ErrAlreadyRunning", err)
	}

	token, err := cellBroker.MintSidecarToken("web", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.DialSession(context.Background(), cellBroker.Descriptor(), token, client.Registration{
		Channel: "beta", Namespace: "test", App: "web", Mode: "test", Source: "harness",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(0)
	if err := session.Endpoint("http", "http://127.0.0.1:4000"); err != nil {
		t.Fatal(err)
	}
	if err := session.Ready(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := cellBroker.WaitReady(ctx, "web"); err != nil {
		t.Fatal(err)
	}

	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if err != nil {
		t.Fatal(err)
	}
	status, err := owner.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Sessions) != 1 || !status.Sessions[0].Ready || status.Sessions[0].App != "web" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestOnlyRuntimeCanRequestUpdateTransition(t *testing.T) {
	base := t.TempDir()
	identity, _ := cell.New("beta", "update")
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(base, "bootstrap"), StoreRoot: filepath.Join(base, "store"),
		CacheRoot: filepath.Join(base, "cache"), RuntimeRoot: filepath.Join(base, "runtime"), LogRoot: filepath.Join(base, "logs"),
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	cellBroker, err := Start(Options{
		Identity: identity, Paths: paths, Generation: 1,
		UpdateTransition: func(context.Context, protocol.UpdateTransitionRequest) (protocol.UpdateTransitionResponse, error) {
			calls.Add(1)
			return protocol.UpdateTransitionResponse{Status: "prepared", Version: "2.0.0-beta.1", RestartRequired: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	runtimeToken, _ := cellBroker.MintRuntimeToken("payload", time.Minute)
	transition, err := client.New(cellBroker.Descriptor(), runtimeToken).PrepareLatest(context.Background())
	if err != nil || transition.Status != "prepared" || !transition.RestartRequired {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	childToken, _ := cellBroker.MintSidecarToken("web", time.Minute)
	if _, err := client.New(cellBroker.Descriptor(), childToken).PrepareLatest(context.Background()); err == nil {
		t.Fatal("sidecar token requested update transition")
	}
	if calls.Load() != 1 {
		t.Fatalf("handler called %d times", calls.Load())
	}
}

func TestRuntimeDelegatesAppBoundChildCapability(t *testing.T) {
	base := t.TempDir()
	identity, _ := cell.New("beta", "delegation")
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(base, "bootstrap"), StoreRoot: filepath.Join(base, "store"),
		CacheRoot: filepath.Join(base, "cache"), RuntimeRoot: filepath.Join(base, "runtime"), LogRoot: filepath.Join(base, "logs"),
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	cellBroker, err := Start(Options{Identity: identity, Paths: paths, Generation: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	runtimeToken, err := cellBroker.MintRuntimeToken("payload", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runtimeClient := client.New(cellBroker.Descriptor(), runtimeToken)
	delegated, err := runtimeClient.DelegateSidecar(context.Background(), "api", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.DialSession(context.Background(), cellBroker.Descriptor(), delegated.Token, client.Registration{
		Channel: "beta", Namespace: "delegation", App: "api", Mode: "test", Source: "payload",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(0)
	childClient := client.New(cellBroker.Descriptor(), delegated.Token)
	if _, err := childClient.DelegateSidecar(context.Background(), "web", time.Minute); err == nil {
		t.Fatal("delegated child capability delegated again")
	}
	if _, err := client.DialSession(context.Background(), cellBroker.Descriptor(), delegated.Token, client.Registration{
		Channel: "beta", Namespace: "delegation", App: "web", Mode: "test", Source: "payload",
	}); err == nil {
		t.Fatal("api-bound child token registered as web")
	}
}
