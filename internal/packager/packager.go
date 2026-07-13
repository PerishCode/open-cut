package packager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/atomicfile"
	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/target"
	"github.com/PerishCode/open-cut/internal/workspace"
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
	if err := deploy(ctx, repositoryRoot, payloadPackage.Name, deployedApp, stdout, stderr); err != nil {
		return Result{}, err
	}
	resourcesRoot := filepath.Join(workRoot, "payload-resources")
	for _, sidecar := range topology.Sidecars {
		manifest, err := workspace.LoadPackage(repositoryRoot, sidecar.App)
		if err != nil {
			return Result{}, err
		}
		destination := filepath.Join(resourcesRoot, "sidecars", sidecar.App)
		if err := deploy(ctx, repositoryRoot, manifest.Name, destination, stdout, stderr); err != nil {
			return Result{}, err
		}
	}
	if err := workspace.WriteTopology(filepath.Join(resourcesRoot, "payload-topology.json"), topology); err != nil {
		return Result{}, err
	}

	electronOutput := filepath.Join(workRoot, "electron-out")
	builderConfig := map[string]any{
		"productName":     payloadPackage.ProductName,
		"asar":            false,
		"npmRebuild":      false,
		"electronVersion": payloadPackage.DevDependencies["electron"],
		"directories":     map[string]string{"app": deployedApp, "output": electronOutput},
		"files":           []string{"dist/**/*", "package.json", "node_modules/**/*"},
		"extraResources":  []map[string]string{{"from": resourcesRoot, "to": "payload"}},
		"mac":             map[string]any{"identity": nil, "target": "dir"},
		"win":             map[string]any{"target": "dir"},
		"linux":           map[string]any{"executableName": slug(payloadPackage.ProductName), "target": "dir"},
	}
	configPath := filepath.Join(workRoot, "electron-builder.json")
	if err := atomicfile.WriteJSON(configPath, builderConfig, 0o600); err != nil {
		return Result{}, err
	}
	environment := append(os.Environ(), "CSC_IDENTITY_AUTO_DISCOVERY=false")
	if err := run(ctx, repositoryRoot, stdout, stderr, environment,
		"pnpm", "--filter", payloadPackage.Name, "exec", "electron-builder", "--dir",
		options.Target.ElectronPlatformFlag(), options.Target.ElectronArchFlag(), "--publish", "never", "--config", configPath); err != nil {
		return Result{}, fmt.Errorf("build Electron full pack: %w", err)
	}
	packRoot, packEntry, err := locateElectronPack(electronOutput, payloadPackage.ProductName, options.Target)
	if err != nil {
		return Result{}, err
	}
	if err := adHocSignMacPack(ctx, packRoot, options.Target, stdout, stderr); err != nil {
		return Result{}, err
	}

	releaseTree := filepath.Join(workRoot, "release-tree")
	launcherArtifact := options.Launcher
	if launcherArtifact == "" {
		launcherArtifact = filepath.Join(workRoot, options.Target.ExecutableName("launcher"))
		goEnvironment := append(os.Environ(), "CGO_ENABLED=0", "GOOS="+options.Target.GoOS(), "GOARCH="+options.Target.GoArch())
		if err := run(ctx, repositoryRoot, stdout, stderr, goEnvironment, "go", "build", "-o", launcherArtifact, "./cmd/launcher"); err != nil {
			return Result{}, fmt.Errorf("build launcher: %w", err)
		}
	}
	launcherName := filepath.Base(launcherArtifact)
	if err := copyFile(launcherArtifact, filepath.Join(releaseTree, "launcher", launcherName), 0o755); err != nil {
		return Result{}, err
	}
	if err := copyTree(packRoot, filepath.Join(releaseTree, "payload", "app"), options.Target.Platform == target.Win); err != nil {
		return Result{}, fmt.Errorf("stage Electron full pack: %w", err)
	}
	manifest := release.Manifest{
		Schema: release.ManifestSchema, Channel: version.Channel, Version: version.String(),
		Platform: options.Target.Platform, Arch: options.Target.Arch,
		Launcher:                 release.Entry{Entry: filepath.ToSlash(filepath.Join("launcher", launcherName))},
		Payload:                  release.Entry{Entry: filepath.ToSlash(filepath.Join("payload", "app", packEntry))},
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

func adHocSignMacPack(ctx context.Context, packRoot string, buildTarget target.Target, stdout, stderr io.Writer) error {
	if buildTarget.Platform != target.Mac || runtime.GOOS != "darwin" {
		return nil
	}
	applications, err := filepath.Glob(filepath.Join(packRoot, "*.app"))
	if err != nil {
		return err
	}
	if len(applications) != 1 {
		return fmt.Errorf("expected one macOS application under %s, found %d", packRoot, len(applications))
	}
	if err := run(ctx, packRoot, stdout, stderr, nil, "codesign", "--force", "--deep", "--sign", "-", applications[0]); err != nil {
		return fmt.Errorf("ad-hoc sign Electron full pack: %w", err)
	}
	return nil
}

func deploy(ctx context.Context, root, packageName, destination string, stdout, stderr io.Writer) error {
	if err := run(ctx, root, stdout, stderr, nil,
		"pnpm", "--filter", packageName, "deploy", "--prod", "--legacy", destination); err != nil {
		return fmt.Errorf("deploy %s: %w", packageName, err)
	}
	if err := removeExternalDeploySelfLink(destination, packageName); err != nil {
		return fmt.Errorf("sanitize deploy %s: %w", packageName, err)
	}
	return nil
}

// pnpm deploy --legacy can leave a package-name symlink in its virtual store
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
	info, err := os.Lstat(selfLink)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		followed, err := os.Stat(selfLink)
		if err != nil {
			return err
		}
		if !followed.IsDir() {
			return fmt.Errorf("deploy self-reference %s is neither a directory nor a symlink", selfLink)
		}
		resolved, err := filepath.EvalSymlinks(selfLink)
		if err != nil {
			return err
		}
		resolvedDestination, err := filepath.EvalSymlinks(destination)
		if err != nil {
			return err
		}
		if pathWithin(resolvedDestination, resolved) {
			return nil
		}
		return os.Remove(selfLink)
	}
	target, err := os.Readlink(selfLink)
	if err != nil {
		return err
	}
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(selfLink), resolved)
	}
	if pathWithin(destination, resolved) {
		return nil
	}
	return os.Remove(selfLink)
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func run(ctx context.Context, directory string, stdout, stderr io.Writer, environment []string, name string, arguments ...string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir, command.Stdout, command.Stderr = directory, stdout, stderr
	if environment != nil {
		command.Env = environment
	}
	return command.Run()
}

func locateElectronPack(output, productName string, buildTarget target.Target) (string, string, error) {
	entries, err := os.ReadDir(output)
	if err != nil {
		return "", "", err
	}
	directories := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, filepath.Join(output, entry.Name()))
		}
	}
	sort.Strings(directories)
	for _, root := range directories {
		var executable string
		switch buildTarget.Platform {
		case target.Mac:
			apps, _ := filepath.Glob(filepath.Join(root, "*.app", "Contents", "MacOS", "*"))
			executable = selectExecutable(apps, productName)
		case target.Win:
			apps, _ := filepath.Glob(filepath.Join(root, "*.exe"))
			executable = selectExecutable(apps, productName+".exe")
		default:
			rootEntries, _ := os.ReadDir(root)
			var candidates []string
			for _, entry := range rootEntries {
				if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 && entry.Name() != "chrome-sandbox" && entry.Name() != "chrome_crashpad_handler" {
					candidates = append(candidates, filepath.Join(root, entry.Name()))
				}
			}
			executable = selectExecutable(candidates, slug(productName))
		}
		if executable == "" {
			continue
		}
		relative, err := filepath.Rel(root, executable)
		if err != nil {
			return "", "", err
		}
		return root, relative, nil
	}
	return "", "", fmt.Errorf("could not locate Electron full pack entry under %s", output)
}

func selectExecutable(candidates []string, preferred string) string {
	sort.Strings(candidates)
	for _, candidate := range candidates {
		if filepath.Base(candidate) == preferred {
			return candidate
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}

func copyTree(source, destination string, dereferenceLinks bool) error {
	root, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	return copyTreeEntry(root, physicalRoot, root, destination, dereferenceLinks, make(map[string]bool))
}

// copyTreeEntry deliberately materializes links for Windows payloads. pnpm
// deploy uses directory junctions inside its virtual store, and those reparse
// points must not leak into a final-user archive that may be extracted without
// Windows symlink privileges. Other targets retain safe relative symlinks for
// native layouts such as macOS frameworks.
func copyTreeEntry(root, physicalRoot, source, destination string, dereferenceLinks bool, activeDirectories map[string]bool) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		if dereferenceLinks {
			return copyDereferencedEntry(root, physicalRoot, source, destination, dereferenceLinks, activeDirectories)
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
		physical, err := filepath.EvalSymlinks(source)
		if err != nil {
			return fmt.Errorf("resolve full pack symlink %s: %w", source, err)
		}
		if !pathWithin(physicalRoot, physical) {
			return fmt.Errorf("full pack symlink resolves outside pack root: %s", source)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.Symlink(link, destination)
	}

	switch {
	case info.IsDir():
		physical, err := filepath.EvalSymlinks(source)
		if err != nil {
			return err
		}
		if !pathWithin(physicalRoot, physical) {
			return fmt.Errorf("full pack directory resolves outside pack root: %s", source)
		}
		key := filepath.Clean(physical)
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
				return copyDereferencedEntry(root, physicalRoot, source, destination, dereferenceLinks, activeDirectories)
			}
		}
		return fmt.Errorf("unsupported full pack entry %s", source)
	}
}

func copyDereferencedEntry(root, physicalRoot, source, destination string, dereferenceLinks bool, activeDirectories map[string]bool) error {
	physical, err := filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("resolve full pack link %s: %w", source, err)
	}
	if !pathWithin(physicalRoot, physical) {
		return fmt.Errorf("full pack link resolves outside pack root: %s", source)
	}
	return copyTreeEntry(root, physicalRoot, physical, destination, dereferenceLinks, activeDirectories)
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
