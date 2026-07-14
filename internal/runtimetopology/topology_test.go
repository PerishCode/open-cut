package runtimetopology

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLoadAndResolve(t *testing.T) {
	root := t.TempDir()
	command := filepath.Join(root, "bin", "runtime")
	working := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(command, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "runtime-topology.json")
	if err := Write(filename, Topology{Schema: 1, Processes: []Process{{
		App: "web", Command: "bin/runtime", Args: []string{"dist/sidecar/index.js"},
		WorkingDirectory: "apps/web", Env: map[string]string{"MODE": "test"}, UnsetEnv: []string{"ELECTRON_RUN_AS_NODE"},
	}}}); err != nil {
		t.Fatal(err)
	}
	plan, err := Resolve(filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Processes) != 1 || plan.Processes[0].Command != command || plan.Processes[0].WorkingDirectory != working {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestTopologyRejectsEscapesAndDuplicateApps(t *testing.T) {
	for name, topology := range map[string]Topology{
		"command escape": {Schema: 1, Processes: []Process{{App: "web", Command: "../node", WorkingDirectory: "."}}},
		"duplicate app": {Schema: 1, Processes: []Process{
			{App: "web", Command: "bin/node", WorkingDirectory: "."},
			{App: "web", Command: "bin/node", WorkingDirectory: "."},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := topology.Validate(); err == nil {
				t.Fatal("invalid topology validated")
			}
		})
	}
}
