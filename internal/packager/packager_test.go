package packager

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestRunArtifactChecksExecutesOnlyContainedDeclaredCommand(t *testing.T) {
	if os.Getenv("OPEN_CUT_ARTIFACT_CHECK_HELPER") == "1" {
		return
	}
	appRoot := t.TempDir()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandName := target.Host().ExecutableName("artifact-check-helper")
	command := filepath.Join(appRoot, commandName)
	if err := copyFile(testExecutable, command, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPEN_CUT_ARTIFACT_CHECK_HELPER", "1")
	var stdout, stderr bytes.Buffer
	if err := runArtifactChecks(context.Background(), appRoot, []workspace.ArtifactCheck{{
		Command: filepath.ToSlash(commandName), Args: []string{"-test.run=TestRunArtifactChecksExecutesOnlyContainedDeclaredCommand"},
	}}, &stdout, &stderr); err != nil {
		t.Fatalf("artifact check failed: %v stderr=%q", err, stderr.String())
	}

	external := filepath.Join(filepath.Dir(appRoot), target.Host().ExecutableName("external-check"))
	if err := copyFile(testExecutable, external, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(appRoot, target.Host().ExecutableName("linked-check"))
	if err := os.Symlink(external, link); err == nil {
		if err := runArtifactChecks(context.Background(), appRoot, []workspace.ArtifactCheck{{
			Command: filepath.ToSlash(filepath.Base(link)),
		}}, &stdout, &stderr); err == nil {
			t.Fatal("escaping artifact check link was accepted")
		}
	}
}

func TestRemoveExternalDeploySelfLink(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "deploy")
	selfLink := filepath.Join(destination, "node_modules", ".pnpm", "node_modules", "@open-cut", "api")
	if err := os.MkdirAll(filepath.Dir(selfLink), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceWorkspace := filepath.Join(filepath.Dir(destination), "workspace", "apps", "api")
	if err := os.MkdirAll(sourceWorkspace, 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(selfLink), sourceWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, selfLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := removeExternalDeploySelfLink(destination, "@open-cut/api"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(selfLink); !os.IsNotExist(err) {
		t.Fatalf("external self-link still exists: %v", err)
	}
}

func TestLocateLinuxPackSelectsSluggedProductExecutable(t *testing.T) {
	output := t.TempDir()
	root := filepath.Join(output, "linux-unpacked")
	for _, name := range []string{"open-cut", "libEGL.so", "libffmpeg.so"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	packRoot, entry, err := lifecycle.LocateElectronPack(output, "Open Cut", target.Target{Platform: target.Linux, Arch: target.X64})
	if err != nil {
		t.Fatal(err)
	}
	if packRoot != root || entry != "open-cut" {
		t.Fatalf("root=%q entry=%q", packRoot, entry)
	}
}

func TestPackagedRuntimeTopologyKeepsElectronAndAppsAsPeers(t *testing.T) {
	packRoot := t.TempDir()
	helper := filepath.Join(packRoot, "Open Cut.app", "Contents", "Frameworks", "Open Cut Helper.app", "Contents", "MacOS", "Open Cut Helper")
	if err := os.MkdirAll(filepath.Dir(helper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	topology, err := packagedRuntimeTopology(
		packRoot,
		"Open Cut.app/Contents/MacOS/Open Cut",
		target.Target{Platform: target.Mac, Arch: target.ARM64},
		workspace.Topology{Schema: 1, Sidecars: []workspace.Sidecar{
			{App: "api", Command: "dist/sidecar/api-sidecar.exe"},
			{App: "electron", Command: workspace.SidecarCommandPayload},
			{App: "web", Command: workspace.SidecarCommandNode, Args: []string{"dist/sidecar/index.js"}},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.Processes) != 3 {
		t.Fatalf("processes=%+v", topology.Processes)
	}
	for _, process := range topology.Processes {
		if process.App == "electron" {
			if process.Command != "app/Open Cut.app/Contents/MacOS/Open Cut" || len(process.UnsetEnv) != 1 {
				t.Fatalf("electron process=%+v", process)
			}
			continue
		}
		if process.App == "web" {
			if process.Command != "app/Open Cut.app/Contents/Frameworks/Open Cut Helper.app/Contents/MacOS/Open Cut Helper" || process.Env["ELECTRON_RUN_AS_NODE"] != "1" {
				t.Fatalf("Node sidecar process=%+v", process)
			}
			continue
		}
		if process.Command != "app/Open Cut.app/Contents/Resources/payload/sidecars/api/dist/sidecar/api-sidecar.exe" || len(process.Env) != 0 {
			t.Fatalf("native sidecar process=%+v", process)
		}
	}
}

func TestRemoveExternalDeploySelfLinkPreservesInternalLink(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "deploy")
	selfLink := filepath.Join(destination, "node_modules", ".pnpm", "node_modules", "@open-cut", "api")
	target := filepath.Join(destination, "node_modules", ".pnpm", "api@workspace", "node_modules", "@open-cut", "api")
	if err := os.MkdirAll(filepath.Dir(selfLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(selfLink), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, selfLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := removeExternalDeploySelfLink(destination, "@open-cut/api"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(selfLink); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("internal self-link was not preserved: %v", err)
	}
}

func TestRemoveExternalDeploySelfLinkPreservesMaterializedDirectory(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "deploy")
	selfReference := filepath.Join(destination, "node_modules", ".pnpm", "node_modules", "@open-cut", "api")
	if err := os.MkdirAll(selfReference, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selfReference, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeExternalDeploySelfLink(destination, "@open-cut/api"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(selfReference); err != nil || !info.IsDir() {
		t.Fatalf("materialized self-reference was not preserved: %v", err)
	}
}

func TestRemoveExternalDeploySelfLinkRejectsUnsafePackageName(t *testing.T) {
	if err := removeExternalDeploySelfLink(t.TempDir(), "../../escape"); err == nil {
		t.Fatal("unsafe package name accepted")
	}
}

func TestCopyTreeDereferencesInternalDirectoryLink(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	packageRoot := filepath.Join(source, "node_modules", ".pnpm", "ws", "node_modules", "ws")
	link := filepath.Join(source, "node_modules", "ws")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte(`{"name":"ws"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(link), packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination, true, ""); err != nil {
		t.Fatal(err)
	}
	copiedLink := filepath.Join(destination, "node_modules", "ws")
	if info, err := os.Lstat(copiedLink); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("link was not materialized as a directory: info=%v err=%v", info, err)
	}
	if contents, err := os.ReadFile(filepath.Join(copiedLink, "package.json")); err != nil || string(contents) != `{"name":"ws"}` {
		t.Fatalf("materialized package mismatch: contents=%q err=%v", contents, err)
	}
}

func TestCopyTreePreservesInternalDirectoryLink(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(source, "Versions", "A")
	link := filepath.Join(source, "Versions", "Current")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "runtime"), []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("A", link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination, false, ""); err != nil {
		t.Fatal(err)
	}
	copiedLink := filepath.Join(destination, "Versions", "Current")
	if info, err := os.Lstat(copiedLink); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was not preserved: info=%v err=%v", info, err)
	}
	if target, err := os.Readlink(copiedLink); err != nil || target != "A" {
		t.Fatalf("preserved link target=%q err=%v", target, err)
	}
}

func TestCopyTreeDereferenceRejectsExternalLink(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	external := filepath.Join(parent, "external")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(source, "external")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := copyTree(source, filepath.Join(parent, "destination"), true, ""); err == nil {
		t.Fatal("external link was accepted")
	}
}

func TestCopyTreeDereferencesRepositoryDependencyLink(t *testing.T) {
	repository := t.TempDir()
	source := filepath.Join(repository, ".tmp", "full-pack")
	dependency := filepath.Join(repository, "packages", "client", "node_modules", "ws")
	link := filepath.Join(source, "resources", "payload", "node_modules", "ws")
	if err := os.MkdirAll(dependency, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "package.json"), []byte(`{"name":"ws"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dependency, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "destination")
	if err := copyTree(source, destination, true, repository); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(destination, "resources", "payload", "node_modules", "ws")
	if info, err := os.Lstat(copied); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("repository dependency was not materialized: info=%v err=%v", info, err)
	}
}

func TestCopyTreeRejectsRepositorySourceLink(t *testing.T) {
	repository := t.TempDir()
	source := filepath.Join(repository, ".tmp", "full-pack")
	productSource := filepath.Join(repository, "packages", "client", "src")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(productSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(productSource, filepath.Join(source, "source")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := copyTree(source, filepath.Join(t.TempDir(), "destination"), true, repository); err == nil {
		t.Fatal("repository source link was accepted")
	}
}
