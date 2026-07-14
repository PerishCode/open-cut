package packager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/filesystem"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

type Options struct {
	RepositoryRoot string
	Version        string
	Target         target.Target
	Output         string
	Launcher       string
	SkipBuild      bool
	KeepWork       bool
	Stdout         io.Writer
	Stderr         io.Writer
}

type Result struct {
	Schema           int    `json:"schema"`
	Version          string `json:"version"`
	Target           string `json:"target"`
	Bundle           string `json:"bundle"`
	SHA256           string `json:"sha256"`
	Size             int64  `json:"size"`
	LauncherEntry    string `json:"launcherEntry"`
	PayloadEntry     string `json:"payloadEntry"`
	PayloadWorkspace string `json:"payloadWorkspace"`
	WorkRoot         string `json:"workRoot,omitempty"`
}

func Pack(ctx context.Context, options Options) (result Result, resultErr error) {
	repositoryRoot, err := filepath.Abs(options.RepositoryRoot)
	if err != nil {
		return Result{}, err
	}
	version, err := release.ParseVersion(options.Version)
	if err != nil {
		return Result{}, err
	}
	controlConfig, err := workspace.Load(repositoryRoot)
	if err != nil {
		return Result{}, err
	}
	payloadPackage, err := workspace.LoadPackage(repositoryRoot, controlConfig.PayloadWorkspace)
	if err != nil {
		return Result{}, err
	}
	if payloadPackage.ProductName == "" || payloadPackage.Main == "" || payloadPackage.DevDependencies["electron"] == "" {
		return Result{}, fmt.Errorf("payload workspace requires productName, main, and a pinned electron devDependency")
	}
	if err := options.Target.Validate(); err != nil {
		return Result{}, err
	}
	packagingPolicy, err := lifecycle.NativePackagingPolicy(options.Target)
	if err != nil {
		return Result{}, err
	}
	outputPath := options.Output
	if outputPath == "" {
		name := slug(payloadPackage.ProductName) + "-" + version.String() + "-" + options.Target.String() + ".release-bundle.tar.zst"
		outputPath = filepath.Join(repositoryRoot, "dist", "releases", version.String(), options.Target.String(), name)
	}
	output, err := filepath.Abs(outputPath)
	if err != nil {
		return Result{}, err
	}
	transaction, err := randomID()
	if err != nil {
		return Result{}, err
	}
	workRoot := filepath.Join(repositoryRoot, ".tmp", "oc-control", "pack", transaction)
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		return Result{}, err
	}
	succeeded := false
	defer func() {
		if succeeded && !options.KeepWork {
			if err := os.RemoveAll(workRoot); resultErr == nil && err != nil {
				resultErr = err
			}
		}
	}()
	stdout, stderr := options.Stdout, options.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if !options.SkipBuild {
		if err := run(ctx, repositoryRoot, stdout, stderr, nil, "pnpm", "-r", "--if-present", "run", "build"); err != nil {
			return Result{}, fmt.Errorf("build workspace: %w", err)
		}
	}
	topology, err := workspace.DiscoverTopology(repositoryRoot, controlConfig)
	if err != nil {
		return Result{}, err
	}

	deployedApp := filepath.Join(workRoot, "electron-app")
	if err := deploy(ctx, repositoryRoot, payloadPackage.Name, deployedApp, packagingPolicy.HoistedDependencyLayout, stdout, stderr); err != nil {
		return Result{}, err
	}
	resourcesRoot := filepath.Join(workRoot, "payload-resources")
	for _, sidecar := range topology.Sidecars {
		if sidecar.App == controlConfig.PayloadWorkspace {
			continue
		}
		manifest, err := workspace.LoadPackage(repositoryRoot, sidecar.App)
		if err != nil {
			return Result{}, err
		}
		destination := filepath.Join(resourcesRoot, "sidecars", sidecar.App)
		if err := deploy(ctx, repositoryRoot, manifest.Name, destination, packagingPolicy.HoistedDependencyLayout, stdout, stderr); err != nil {
			return Result{}, err
		}
	}
	electronOutput := filepath.Join(workRoot, "electron-out")
	builderConfig := lifecycle.ElectronBuilderConfig(
		payloadPackage.ProductName, payloadPackage.DevDependencies["electron"], deployedApp, electronOutput, resourcesRoot,
	)
	configPath := filepath.Join(workRoot, "electron-builder.json")
	if err := atomicfile.WriteJSON(configPath, builderConfig, 0o600); err != nil {
		return Result{}, err
	}
	if err := lifecycle.BuildElectronPack(
		ctx, options.Target, repositoryRoot, payloadPackage.Name, configPath, stdout, stderr,
	); err != nil {
		return Result{}, fmt.Errorf("build Electron full pack: %w", err)
	}
	packRoot, packEntry, err := lifecycle.LocateElectronPack(electronOutput, payloadPackage.ProductName, options.Target)
	if err != nil {
		return Result{}, err
	}
	if err := lifecycle.FinalizeElectronPack(ctx, packRoot, options.Target, stdout, stderr); err != nil {
		return Result{}, err
	}

	releaseTree := filepath.Join(workRoot, "release-tree")
	launcherArtifact := options.Launcher
	if launcherArtifact == "" {
		launcherArtifact = filepath.Join(workRoot, options.Target.ExecutableName("launcher"))
		goEnvironment := options.Target.GoBuildEnvironment(os.Environ())
		if err := run(ctx, repositoryRoot, stdout, stderr, goEnvironment, "go", "build", "-o", launcherArtifact, "./cmd/launcher"); err != nil {
			return Result{}, fmt.Errorf("build launcher: %w", err)
		}
	}
	launcherName := filepath.Base(launcherArtifact)
	if err := copyFile(launcherArtifact, filepath.Join(releaseTree, "launcher", launcherName), 0o755); err != nil {
		return Result{}, err
	}
	if err := copyTree(
		packRoot,
		filepath.Join(releaseTree, "payload", "app"),
		packagingPolicy.MaterializeLinks,
		repositoryRoot,
	); err != nil {
		return Result{}, fmt.Errorf("stage Electron full pack: %w", err)
	}
	runtimeTopology, err := packagedRuntimeTopology(packRoot, packEntry, options.Target, controlConfig.PayloadWorkspace, topology)
	if err != nil {
		return Result{}, err
	}
	topologyEntry := filepath.Join("payload", "runtime-topology.json")
	if err := runtimetopology.Write(filepath.Join(releaseTree, topologyEntry), runtimeTopology); err != nil {
		return Result{}, err
	}
	if _, err := runtimetopology.Resolve(filepath.Join(releaseTree, topologyEntry)); err != nil {
		return Result{}, fmt.Errorf("validate staged runtime topology: %w", err)
	}
	manifest := release.Manifest{
		Schema: release.ManifestSchema, Channel: version.Channel, Version: version.String(),
		Platform: options.Target.Platform, Arch: options.Target.Arch,
		Launcher:                 release.Entry{Entry: filepath.ToSlash(filepath.Join("launcher", launcherName))},
		Payload:                  release.Entry{Entry: filepath.ToSlash(topologyEntry)},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: time.Now().UTC(),
	}
	if err := atomicfile.WriteJSON(filepath.Join(releaseTree, "manifest.json"), manifest, 0o600); err != nil {
		return Result{}, err
	}
	if err := bundle.Pack(releaseTree, output); err != nil {
		return Result{}, err
	}
	digest, size, err := bundle.SHA256(output)
	if err != nil {
		return Result{}, err
	}
	succeeded = true
	result = Result{
		Schema: 1, Version: version.String(), Target: options.Target.String(), Bundle: output, SHA256: digest, Size: size,
		LauncherEntry: manifest.Launcher.Entry, PayloadEntry: manifest.Payload.Entry,
		PayloadWorkspace: controlConfig.PayloadWorkspace,
	}
	if options.KeepWork {
		result.WorkRoot = workRoot
	}
	return result, nil
}

func packagedRuntimeTopology(
	packRoot, packEntry string,
	buildTarget target.Target,
	payloadWorkspace string,
	discovered workspace.Topology,
) (runtimetopology.Topology, error) {
	layout, err := lifecycle.ResolveRuntimeLayout(packRoot, packEntry, buildTarget)
	if err != nil {
		return runtimetopology.Topology{}, err
	}

	processes := make([]runtimetopology.Process, 0, len(discovered.Sidecars))
	for _, sidecar := range discovered.Sidecars {
		if sidecar.App == payloadWorkspace {
			processes = append(processes, runtimetopology.Process{
				App: sidecar.App, Command: layout.ElectronCommand, WorkingDirectory: "app",
				UnsetEnv: []string{"ELECTRON_RUN_AS_NODE"}, Sandbox: lifecycle.SandboxChromium,
			})
			continue
		}
		processes = append(processes, runtimetopology.Process{
			App: sidecar.App, Command: layout.NodeCommand, Args: []string{"dist/sidecar/index.js"},
			WorkingDirectory: filepath.ToSlash(filepath.Join(layout.PayloadResources, "sidecars", sidecar.App)),
			Env:              map[string]string{"ELECTRON_RUN_AS_NODE": "1"},
		})
	}
	topology := runtimetopology.Topology{Schema: runtimetopology.Schema, Processes: processes}
	if err := topology.Validate(); err != nil {
		return runtimetopology.Topology{}, err
	}
	return topology, nil
}

func deploy(ctx context.Context, root, packageName, destination string, hoisted bool, stdout, stderr io.Writer) error {
	var processEnvironment []string
	if hoisted {
		// pnpm's isolated Windows layout relies on junctions into its virtual
		// store. A release cannot preserve those links safely, and materializing
		// them changes the package's physical path (and therefore Node's lookup
		// chain). Hoisted deploy produces the same dependency closure as ordinary
		// directories, which remains valid after final staging and extraction.
		processEnvironment = environment.Merge(os.Environ(), nil, map[string]string{"npm_config_node_linker": "hoisted"})
	}
	if err := run(ctx, root, stdout, stderr, processEnvironment,
		"pnpm", "--config.inject-workspace-packages=true", "--filter", packageName, "deploy", "--prod", destination); err != nil {
		return fmt.Errorf("deploy %s: %w", packageName, err)
	}
	if err := removeExternalDeploySelfLink(destination, packageName); err != nil {
		return fmt.Errorf("sanitize deploy %s: %w", packageName, err)
	}
	return nil
}

// pnpm deploy can leave a package-name symlink in its virtual store
// that points back to the source workspace. The link is metadata for workspace
// development, not a production dependency, and would make the packaged app
// depend on the build machine. Remove only that exact self-link and only when it
// resolves outside the deployed tree. All other external symlinks remain fatal
// when copyTree stages the Electron full pack.
func removeExternalDeploySelfLink(destination, packageName string) error {
	virtualStore := filepath.Join(destination, "node_modules", ".pnpm", "node_modules")
	selfLink := filepath.Join(virtualStore, filepath.FromSlash(packageName))
	if !pathWithin(virtualStore, selfLink) || selfLink == virtualStore {
		return fmt.Errorf("unsafe package name %q", packageName)
	}
	_, err := os.Lstat(selfLink)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	followed, err := os.Stat(selfLink)
	if err != nil {
		return err
	}
	if !followed.IsDir() {
		return fmt.Errorf("deploy self-reference %s is not a directory", selfLink)
	}
	resolved, err := filesystem.Canonical(selfLink)
	if err != nil {
		return err
	}
	resolvedDestination, err := filesystem.Canonical(destination)
	if err != nil {
		return err
	}
	if pathWithin(resolvedDestination, resolved) {
		return nil
	}
	return os.Remove(selfLink)
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func run(ctx context.Context, directory string, stdout, stderr io.Writer, environment []string, name string, arguments ...string) error {
	command, err := tool.ResolveRepository(directory, name)
	if err != nil {
		return err
	}
	return lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: command.Executable, Args: command.Arguments(arguments...), Directory: directory, Env: environment,
		Stdout: stdout, Stderr: stderr, Profile: lifecycle.ProfileProduction,
	})
}

func copyTree(source, destination string, dereferenceLinks bool, repositoryDependencyRoot string) error {
	root, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	physicalRoot, err := filesystem.Canonical(root)
	if err != nil {
		return err
	}
	physicalRepositoryRoot := ""
	if repositoryDependencyRoot != "" {
		physicalRepositoryRoot, err = filesystem.Canonical(repositoryDependencyRoot)
		if err != nil {
			return err
		}
	}
	return copyTreeEntry(root, physicalRoot, physicalRepositoryRoot, root, destination, dereferenceLinks, make(map[string]bool))
}

// copyTreeEntry deliberately materializes links for Windows payloads. pnpm
// deploy uses directory junctions inside its virtual store, and those reparse
// points must not leak into a final-user archive that may be extracted without
// Windows symlink privileges. Other targets retain safe relative symlinks for
// native layouts such as macOS frameworks.
func copyTreeEntry(root, physicalRoot, physicalRepositoryRoot, source, destination string, dereferenceLinks bool, activeDirectories map[string]bool) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		if dereferenceLinks {
			return copyDereferencedEntry(root, physicalRoot, physicalRepositoryRoot, source, destination, dereferenceLinks, activeDirectories)
		}
		link, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if link == "" || filepath.IsAbs(link) {
			return fmt.Errorf("full pack contains unsafe symlink %s", source)
		}
		resolved := filepath.Clean(filepath.Join(filepath.Dir(source), link))
		if !pathWithin(root, resolved) {
			return fmt.Errorf("full pack symlink escapes pack root: %s", source)
		}
		physical, err := filesystem.Canonical(source)
		if err != nil {
			return fmt.Errorf("resolve full pack symlink %s: %w", source, err)
		}
		if !allowedMaterializationPath(physicalRoot, "", physical) {
			return fmt.Errorf("full pack symlink resolves outside pack root: %s", source)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.Symlink(link, destination)
	}

	switch {
	case info.IsDir():
		physical, err := filesystem.Canonical(source)
		if err != nil {
			return err
		}
		if !allowedMaterializationPath(physicalRoot, physicalRepositoryRoot, physical) {
			return fmt.Errorf("full pack directory resolves outside pack root: %s", source)
		}
		key := filesystem.IdentityKey(physical)
		if activeDirectories[key] {
			return fmt.Errorf("full pack contains a directory link cycle at %s", source)
		}
		activeDirectories[key] = true
		defer delete(activeDirectories, key)
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTreeEntry(
				root,
				physicalRoot,
				physicalRepositoryRoot,
				filepath.Join(source, entry.Name()),
				filepath.Join(destination, entry.Name()),
				dereferenceLinks,
				activeDirectories,
			); err != nil {
				return err
			}
		}
		return nil
	case info.Mode().IsRegular():
		return copyFile(source, destination, info.Mode().Perm())
	default:
		// Windows directory junctions are reparse points but are not always
		// reported as ModeSymlink by os.Lstat. os.Stat follows them, allowing
		// the Windows materialization policy to handle both representations.
		if dereferenceLinks {
			followed, followErr := os.Stat(source)
			if followErr == nil && (followed.IsDir() || followed.Mode().IsRegular()) {
				return copyDereferencedEntry(root, physicalRoot, physicalRepositoryRoot, source, destination, dereferenceLinks, activeDirectories)
			}
		}
		return fmt.Errorf("unsupported full pack entry %s", source)
	}
}

func copyDereferencedEntry(root, physicalRoot, physicalRepositoryRoot, source, destination string, dereferenceLinks bool, activeDirectories map[string]bool) error {
	physical, err := filesystem.Canonical(source)
	if err != nil {
		return fmt.Errorf("resolve full pack link %s: %w", source, err)
	}
	if !allowedMaterializationPath(physicalRoot, physicalRepositoryRoot, physical) {
		return fmt.Errorf("full pack link resolves outside pack root: %s", source)
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	switch {
	case info.IsDir():
		key := filesystem.IdentityKey(physical)
		if activeDirectories[key] {
			return fmt.Errorf("full pack contains a directory link cycle at %s", source)
		}
		activeDirectories[key] = true
		defer delete(activeDirectories, key)
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTreeEntry(
				root,
				physicalRoot,
				physicalRepositoryRoot,
				filepath.Join(source, entry.Name()),
				filepath.Join(destination, entry.Name()),
				dereferenceLinks,
				activeDirectories,
			); err != nil {
				return err
			}
		}
		return nil
	case info.Mode().IsRegular():
		return copyFile(source, destination, info.Mode().Perm())
	default:
		return fmt.Errorf("unsupported dereferenced full pack entry %s", source)
	}
}

// allowedMaterializationPath accepts the full pack itself plus dependency
// material under this checkout's node_modules trees. pnpm's Windows deploy can
// point a copied workspace package dependency back to its source node_modules.
// That build-time edge is safe to read and materialize, but source files and
// global content-addressable stores remain outside the closure boundary.
func allowedMaterializationPath(physicalRoot, physicalRepositoryRoot, candidate string) bool {
	if pathWithin(physicalRoot, candidate) {
		return true
	}
	if physicalRepositoryRoot == "" || !pathWithin(physicalRepositoryRoot, candidate) {
		return false
	}
	relative, err := filepath.Rel(physicalRepositoryRoot, candidate)
	if err != nil {
		return false
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if strings.EqualFold(component, "node_modules") {
			return true
		}
	}
	return false
}

func copyFile(source, destination string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func randomID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result []rune
	separator := false
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			if separator && len(result) > 0 {
				result = append(result, '-')
			}
			result = append(result, character)
			separator = false
		} else {
			separator = true
		}
	}
	if len(result) == 0 {
		return "payload"
	}
	return string(result)
}
