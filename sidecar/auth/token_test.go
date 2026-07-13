package auth

import (
	"testing"
	"time"

	"github.com/PerishCode/open-cut/sidecar/protocol"
)

func TestCapabilityIsSessionBoundAndScoped(t *testing.T) {
	now := time.Unix(1000, 0)
	manager := newManager([]byte("01234567890123456789012345678901"), "session-a", 3, func() time.Time { return now })
	token, err := manager.Mint("web", protocol.RoleSidecar, []protocol.Capability{protocol.CapabilityRuntimeReady}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.Verify(token, protocol.CapabilityRuntimeReady)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "web" {
		t.Fatalf("subject = %q", claims.Subject)
	}
	if _, err := manager.Verify(token, protocol.CapabilityUpdateTransition); err == nil {
		t.Fatal("token unexpectedly had update capability")
	}
	other := newManager([]byte("01234567890123456789012345678901"), "session-b", 3, func() time.Time { return now })
	if _, err := other.Verify(token, protocol.CapabilityRuntimeReady); err == nil {
		t.Fatal("token unexpectedly crossed sessions")
	}
}

func TestCapabilityExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	manager := newManager([]byte("01234567890123456789012345678901"), "session", 1, func() time.Time { return now })
	token, err := manager.Mint("api", protocol.RoleSidecar, []protocol.Capability{protocol.CapabilityRuntimeReady}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err := manager.Verify(token, protocol.CapabilityRuntimeReady); err == nil {
		t.Fatal("expired token verified")
	}
}
