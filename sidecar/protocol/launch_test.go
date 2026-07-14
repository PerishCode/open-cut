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
		Token: "token", Channel: "beta", Namespace: "test", Mode: "harness", Source: "test",
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
	if loaded.Control.SessionID != launch.Control.SessionID || loaded.Namespace != launch.Namespace {
		t.Fatalf("loaded=%+v", loaded)
	}
}
