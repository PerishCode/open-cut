package client

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

func dialTestSession(t *testing.T, mode protocol.LifecycleMode) (*Session, *broker.Broker) {
	t.Helper()
	base := t.TempDir()
	identity, err := cell.New("test", "client-abandon-"+string(mode))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(base, "bootstrap"), StoreRoot: filepath.Join(base, "store"),
		CacheRoot: filepath.Join(base, "cache"), RuntimeRoot: filepath.Join(base, "runtime"),
		LogRoot: filepath.Join(base, "logs"),
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	token, err := cellBroker.MintRuntimeToken("payload", time.Hour)
	if err != nil {
		cellBroker.Close()
		t.Fatal(err)
	}
	session, err := DialSession(context.Background(), cellBroker.Descriptor(), token, Registration{
		Channel: identity.Channel, Namespace: identity.Namespace, App: "payload", Mode: mode, Source: "test",
	})
	if err != nil {
		cellBroker.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close(0) })
	return session, cellBroker
}

func TestSessionAbandonsAfterBrokerLossInDevelopmentModes(t *testing.T) {
	previous := developmentAbandonWindow
	developmentAbandonWindow = 200 * time.Millisecond
	t.Cleanup(func() { developmentAbandonWindow = previous })

	for _, mode := range []protocol.LifecycleMode{protocol.LifecycleModeDev, protocol.LifecycleModeHarness} {
		session, cellBroker := dialTestSession(t, mode)
		cellBroker.Close()
		select {
		case <-session.Abandoned():
		case <-time.After(10 * time.Second):
			t.Fatalf("mode %s: session did not abandon after broker loss", mode)
		}
		drained := false
		for range 128 {
			if _, err := session.ReadEvent(context.Background()); err != nil {
				drained = true
				break
			}
		}
		if !drained {
			t.Fatalf("mode %s: abandoned session still serves events after drain", mode)
		}
	}
}

func TestSessionKeepsReconnectingAfterBrokerLossInProductionModes(t *testing.T) {
	previous := developmentAbandonWindow
	developmentAbandonWindow = 100 * time.Millisecond
	t.Cleanup(func() { developmentAbandonWindow = previous })

	for _, mode := range []protocol.LifecycleMode{protocol.LifecycleModeProduction, protocol.LifecycleModePackaged} {
		session, cellBroker := dialTestSession(t, mode)
		cellBroker.Close()
		select {
		case <-session.Abandoned():
			t.Fatalf("mode %s: session must not fail closed", mode)
		case <-time.After(1 * time.Second):
		}
	}
}
