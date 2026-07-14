package harness

import (
	"testing"

	"github.com/PerishCode/open-cut/internal/workspace"
)

func TestSelectSidecarsUsesManifestTopology(t *testing.T) {
	topology := workspace.Topology{Schema: 1, Sidecars: []workspace.Sidecar{
		{App: "api", Command: "dist/sidecar/api-sidecar.exe"},
		{App: "electron", Command: workspace.SidecarCommandPayload},
		{App: "web", Command: workspace.SidecarCommandNode, Args: []string{"dist/sidecar/index.js"}},
	}}
	selected, err := selectSidecars(topology, "api", "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Sidecars) != 2 || selected.Sidecars[0].App != "api" || selected.Sidecars[1].App != "web" {
		t.Fatalf("selected=%+v", selected.Sidecars)
	}
	if _, err := selectSidecars(topology, "missing"); err == nil {
		t.Fatal("missing sidecar was accepted")
	}
}
