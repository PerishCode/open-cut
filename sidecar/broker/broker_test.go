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

func TestBrokerStreamsReversibleRevisionedPeerState(t *testing.T) {
	base := t.TempDir()
	identity, _ := cell.New("beta", "stream")
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(base, "bootstrap"), StoreRoot: filepath.Join(base, "store"),
		CacheRoot: filepath.Join(base, "cache"), RuntimeRoot: filepath.Join(base, "runtime"), LogRoot: filepath.Join(base, "logs"),
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	cellBroker, err := Start(Options{Identity: identity, Paths: paths, Generation: 3, HeartbeatTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()

	webToken, _ := cellBroker.MintSidecarToken("web", time.Minute)
	web, err := client.DialSession(context.Background(), cellBroker.Descriptor(), webToken, client.Registration{
		Channel: "beta", Namespace: "stream", App: "web", InstanceID: "web-instance-1", Mode: "test", Source: "harness",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer web.Close(0)
	observerToken, _ := cellBroker.MintSidecarToken("electron", time.Minute)
	observer, err := client.DialSession(context.Background(), cellBroker.Descriptor(), observerToken, client.Registration{
		Channel: "beta", Namespace: "stream", App: "electron", InstanceID: "electron-instance-1", Mode: "test", Source: "harness",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close(0)

	if err := web.Endpoint("http", "http://127.0.0.1:4100"); err != nil {
		t.Fatal(err)
	}
	if err := web.Ready(); err != nil {
		t.Fatal(err)
	}
	readyStatus := waitForStreamStatus(t, observer, func(status protocol.Status) bool {
		peer := statusSession(status, "web")
		return peer != nil && peer.Ready && peer.InstanceID == "web-instance-1" && len(peer.Endpoints) == 1
	})
	if readyStatus.Revision == 0 {
		t.Fatal("ready snapshot did not carry a revision")
	}

	if err := web.NotReady(); err != nil {
		t.Fatal(err)
	}
	unavailableStatus := waitForStreamStatus(t, observer, func(status protocol.Status) bool {
		peer := statusSession(status, "web")
		return peer != nil && !peer.Ready && len(peer.Endpoints) == 0
	})
	if unavailableStatus.Revision <= readyStatus.Revision {
		t.Fatalf("revision did not advance: ready=%d unavailable=%d", readyStatus.Revision, unavailableStatus.Revision)
	}

	if err := web.Close(0); err != nil {
		t.Fatal(err)
	}
	absentStatus := waitForStreamStatus(t, observer, func(status protocol.Status) bool {
		return statusSession(status, "web") == nil
	})
	if absentStatus.Revision <= unavailableStatus.Revision {
		t.Fatalf("revision did not advance on exit: unavailable=%d absent=%d", unavailableStatus.Revision, absentStatus.Revision)
	}
}

func TestGoSessionReconnectsAndReplaysDesiredState(t *testing.T) {
	base := t.TempDir()
	identity, _ := cell.New("beta", "reconnect")
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(base, "bootstrap"), StoreRoot: filepath.Join(base, "store"),
		CacheRoot: filepath.Join(base, "cache"), RuntimeRoot: filepath.Join(base, "runtime"), LogRoot: filepath.Join(base, "logs"),
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	cellBroker, err := Start(Options{Identity: identity, Paths: paths, Generation: 4, HeartbeatTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	token, _ := cellBroker.MintSidecarToken("api", time.Minute)
	session, err := client.DialSession(context.Background(), cellBroker.Descriptor(), token, client.Registration{
		Channel: "beta", Namespace: "reconnect", App: "api", InstanceID: "api-instance-1", Mode: "test", Source: "harness",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(0)
	if err := session.Endpoint("http", "http://127.0.0.1:4200"); err != nil {
		t.Fatal(err)
	}
	if err := session.Ready(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cellBroker.WaitReady(ctx, "api"); err != nil {
		t.Fatal(err)
	}
	cellBroker.mu.RLock()
	registered := cellBroker.sessions["api"]
	cellBroker.mu.RUnlock()
	if registered == nil {
		t.Fatal("api session was not registered")
	}
	registered.conn.Close()

	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		status, statusErr := owner.Status(context.Background())
		if statusErr == nil {
			peer := statusSession(status, "api")
			if peer != nil && peer.Ready && peer.InstanceID == "api-instance-1" && len(peer.Endpoints) == 1 {
				return
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("api session did not reconnect with desired state; last status=%+v err=%v", status, statusErr)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func waitForStreamStatus(t *testing.T, session *client.Session, accept func(protocol.Status) bool) protocol.Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		event, err := session.ReadEvent(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == protocol.EventStatus && event.Status != nil && accept(*event.Status) {
			return *event.Status
		}
	}
}

func statusSession(status protocol.Status, app string) *protocol.SessionStatus {
	for index := range status.Sessions {
		if status.Sessions[index].App == app {
			return &status.Sessions[index]
		}
	}
	return nil
}

func TestUpdateTransitionRequiresExplicitCapability(t *testing.T) {
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
			return protocol.UpdateTransitionResponse{Status: protocol.UpdateStatusPrepared, Version: "2.0.0-beta.1", RestartRequired: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	runtimeToken, _ := cellBroker.MintRuntimeToken("payload", time.Minute)
	transition, err := client.New(cellBroker.Descriptor(), runtimeToken).PrepareLatest(context.Background())
	if err != nil || transition.Status != protocol.UpdateStatusPrepared || !transition.RestartRequired {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	childToken, _ := cellBroker.MintSidecarToken("web", time.Minute)
	if _, err := client.New(cellBroker.Descriptor(), childToken).PrepareLatest(context.Background()); err == nil {
		t.Fatal("sidecar token requested update transition")
	}
	delegated, err := client.New(cellBroker.Descriptor(), runtimeToken).DelegateSidecar(
		context.Background(), "electron", time.Minute, []protocol.Capability{protocol.CapabilityUpdateTransition},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.New(cellBroker.Descriptor(), delegated.Token).PrepareLatest(context.Background()); err != nil {
		t.Fatalf("explicitly authorized sidecar could not request update: %v", err)
	}
	if calls.Load() != 2 {
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
	delegated, err := runtimeClient.DelegateSidecar(context.Background(), "api", 30*time.Second, nil)
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
	if _, err := childClient.Status(context.Background()); err != nil {
		t.Fatalf("delegated sidecar could not observe peers: %v", err)
	}
	renewed, err := childClient.Renew(context.Background(), 2*time.Minute)
	if err != nil || renewed.Subject != "api" || renewed.Token == delegated.Token {
		t.Fatalf("active sidecar capability did not renew: response=%+v err=%v", renewed, err)
	}
	if _, err := childClient.Status(context.Background()); err != nil {
		t.Fatalf("renewed sidecar capability could not observe peers: %v", err)
	}
	if _, err := childClient.PrepareLatest(context.Background()); err == nil {
		t.Fatal("renewal escalated sidecar update authority")
	}
	if _, err := childClient.DelegateSidecar(context.Background(), "web", time.Minute, nil); err == nil {
		t.Fatal("delegated child capability delegated again")
	}
	if _, err := client.DialSession(context.Background(), cellBroker.Descriptor(), delegated.Token, client.Registration{
		Channel: "beta", Namespace: "delegation", App: "web", Mode: "test", Source: "payload",
	}); err == nil {
		t.Fatal("api-bound child token registered as web")
	}
}
