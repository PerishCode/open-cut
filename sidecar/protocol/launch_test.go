package protocol

import (
	"strings"
	"testing"
	"time"
)

func TestLaunchEnvironmentUsesGeneratedBindings(t *testing.T) {
	launch := SidecarLaunch{
		Control: ControlDescriptor{
			Schema: 1, Protocol: Version, Address: "127.0.0.1:4321", PID: 7,
			SessionID: "session", Generation: 2, StartedAt: time.Unix(1, 0).UTC(),
		},
		App: "web", Token: "token", Channel: "beta", Namespace: "test", DataDir: "/tmp/open-cut/beta/test",
		Installation: testInstallationAssertion(),
		Mode:         LifecycleModeHarness, Presentation: PresentationHeadless, Source: "test",
	}
	values, err := LaunchEnvironmentMap(launch)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != SidecarEnvironmentCount || values[SidecarEnvToken] != launch.Token || !strings.Contains(values[SidecarEnvControl], `"protocol":"sidecar.v1"`) {
		t.Fatalf("environment=%v", values)
	}
	merged, err := AppendLaunchEnvironment([]string{SidecarEnvToken + "=old", "PATH=/bin"}, launch)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(merged, "\n")
	if strings.Contains(joined, SidecarEnvToken+"=old") || !strings.Contains(joined, SidecarEnvToken+"=token") {
		t.Fatalf("launch environment did not replace existing token: %s", joined)
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	loaded, err := LoadLaunchEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Control.SessionID != launch.Control.SessionID || loaded.Namespace != launch.Namespace || loaded.DataDir != launch.DataDir ||
		loaded.App != launch.App || loaded.Presentation != launch.Presentation {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func testInstallationAssertion() InstallationAssertion {
	return InstallationAssertion{
		Schema: 1, InstallationID: "installation-test", Generation: 1,
		Keys: []InstallationPublicKey{{
			Role: "harness", Algorithm: InstallationKeyAlgorithmEd25519,
			PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		}},
	}
}
