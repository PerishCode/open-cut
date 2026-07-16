package client

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/sidecar/protocol"
)

func TestResolveDataDirAppendsValidatedAppIdentity(t *testing.T) {
	base := t.TempDir()
	launch := protocol.SidecarLaunch{
		App: "api", Token: "token", Channel: "dev", Namespace: "default", DataDir: base,
		Installation: protocol.InstallationAssertion{
			Schema: 1, InstallationID: "installation-test", Generation: 1,
			Keys: []protocol.InstallationPublicKey{{
				Role: "harness", Algorithm: protocol.InstallationKeyAlgorithmEd25519,
				PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			}},
		},
		Mode: protocol.LifecycleModeDev, Presentation: protocol.PresentationHeadless, Source: "test",
		Control: protocol.ControlDescriptor{
			Schema: 1, Protocol: protocol.Version, Address: "127.0.0.1:1", SessionID: "session", StartedAt: time.Now(),
		},
	}
	resolved, err := ResolveDataDir(launch)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "api"); resolved != want {
		t.Fatalf("ResolveDataDir() = %q, want %q", resolved, want)
	}
	launch.App = "../api"
	if _, err := ResolveDataDir(launch); err == nil {
		t.Fatal("unsafe app identity resolved a data directory")
	}
}
