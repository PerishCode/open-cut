package tool

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVersionArguments(t *testing.T) {
	if got := versionArguments("go"); len(got) != 1 || got[0] != "version" {
		t.Fatalf("go version args = %v", got)
	}
	if got := versionArguments("node"); len(got) != 1 || got[0] != "--version" {
		t.Fatalf("node version args = %v", got)
	}
}

func TestRepositoryStateRoundTrip(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	want := Command{Executable: executable, Prefix: []string{"tool.cjs"}}
	if err := SaveRepositoryState(root, RepositoryState{
		Schema: RepositoryStateSchema,
		Tools:  map[string]Command{"pnpm": want},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveRepository(root, "pnpm")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repository command = %+v, want %+v", got, want)
	}
	if !filepath.IsAbs(RepositoryStatePath(root)) {
		t.Fatalf("state path is not absolute: %s", RepositoryStatePath(root))
	}
}

func TestCommandArgumentsPreservePrefix(t *testing.T) {
	command := Command{Executable: "/node", Prefix: []string{"pnpm.cjs"}}
	got := command.Arguments("lint")
	if !reflect.DeepEqual(got, []string{"pnpm.cjs", "lint"}) {
		t.Fatalf("arguments = %v", got)
	}
	if !reflect.DeepEqual(command.Prefix, []string{"pnpm.cjs"}) {
		t.Fatalf("prefix mutated: %v", command.Prefix)
	}
}

func TestShellJoin(t *testing.T) {
	got := shellJoin([]string{"/path with spaces/node", "it's/pnpm.cjs"})
	want := "'/path with spaces/node' 'it'\"'\"'s/pnpm.cjs'"
	if got != want {
		t.Fatalf("shellJoin = %q, want %q", got, want)
	}
}

func TestRepositoryShimExportsItsToolDirectory(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := WriteRepositoryShims(root, map[string]Command{
		"pnpm": {Executable: executable},
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".oc-control", "bin", "pnpm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `PATH="$tool_bin:$PATH"`) {
		t.Fatalf("POSIX shim does not export its tool directory: %s", data)
	}
	windows, err := os.ReadFile(filepath.Join(root, ".oc-control", "bin", "pnpm.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(windows), `set "PATH=%~dp0;%PATH%"`) {
		t.Fatalf("Windows shim does not export its tool directory: %s", windows)
	}
}
