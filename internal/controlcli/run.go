package controlcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/internal/buildinfo"
	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/cleaner"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/delivery"
	"github.com/PerishCode/open-cut/internal/devbootstrap"
	"github.com/PerishCode/open-cut/internal/devsession"
	"github.com/PerishCode/open-cut/internal/harness"
	"github.com/PerishCode/open-cut/internal/harnessguard"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/originserver"
	"github.com/PerishCode/open-cut/internal/packager"
	"github.com/PerishCode/open-cut/internal/protocolgen"
	"github.com/PerishCode/open-cut/internal/publisher"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/verifier"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr)
	case "bootstrap":
		return runBootstrap(ctx, args[1:], stdout, stderr)
	case "clean":
		return runClean(args[1:], stdout, stderr)
	case "dev":
		return runDev(ctx, args[1:], stdout, stderr)
	case "pack":
		return runPack(ctx, args[1:], stdout, stderr)
	case "protocol":
		return runProtocol(ctx, args[1:], stdout, stderr)
	case "release":
		return runRelease(args[1:], stdout, stderr)
	case "serve":
		return runServe(ctx, args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "inspect":
		return runInspect(args[1:], stdout, stderr)
	case "status":
		return runStatus(ctx, args[1:], stdout, stderr)
	case string(protocol.ControlCommandShow), string(protocol.ControlCommandShutdown):
		return runControl(ctx, protocol.ControlCommand(args[0]), args[1:], stdout, stderr)
	case "harness":
		return runHarness(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printHelp(stderr)
		return 2
	}
}

func printHelp(writer io.Writer) {
	fmt.Fprint(writer, `oc-control controls the Open Cut development and runtime substrate.

Usage:
  oc-control version [--json]
  oc-control doctor
  oc-control bootstrap [--repo <path>]
  oc-control clean [--repo <path>] [--scope temp|build|all] [--dry-run]
  oc-control dev [--repo <path>] [--base-dir <path>] [--skip-build]
  oc-control pack <mac|win|linux> --arch <arm64|x64> --version <X.Y.Z-channel.N> [--output <path>]
  oc-control protocol generate [--repo <path>]
  oc-control protocol check [--repo <path>]
  oc-control release keygen --output <key.json> [--id dev]
  oc-control release create --bundle <bundle> --origin <dir> --key <key.json>
  oc-control release display-version <X.Y.Z-channel.N>
  oc-control serve --root <origin-dir> [--listen 127.0.0.1:0]
  oc-control verify <mac|win|linux> --arch <arm64|x64> --bundle <bundle>
  oc-control verify <mac|win|linux> --arch <arm64|x64> --origin <dir> --channel <channel> --key <key.json>
  oc-control inspect --receipt <install-receipt.json>
  oc-control status --bootstrap <bootstrap.json>
  oc-control show --bootstrap <bootstrap.json>
  oc-control shutdown --bootstrap <bootstrap.json>
  oc-control harness guard [--repo <path>]
  oc-control harness broker [--workspace <path>]
  oc-control harness sidecars [--repo <path>] [--workspace <path>] [--skip-build]
  oc-control harness cold-start [--repo <path>] [--workspace <path>]
  oc-control harness full-pack --bundle <release-bundle.tar.zst> [--workspace <path>]
  oc-control harness install <mac|win|linux> --arch <arch> --workspace <path> --origin <dir> --origin-url <url> --key <key.json>
  oc-control harness run --workspace <path> --receipt <install-receipt.json> [--headless]
  oc-control harness uninstall --workspace <path> --receipt <install-receipt.json> [--purge]
`)
}

func runProtocol(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: oc-control protocol <generate|check> [--repo <path>]")
		return 2
	}
	mode := protocolgen.Mode(args[0])
	if mode != protocolgen.ModeGenerate && mode != protocolgen.ModeCheck {
		fmt.Fprintln(stderr, "usage: oc-control protocol <generate|check> [--repo <path>]")
		return 2
	}
	set := flag.NewFlagSet("protocol "+args[0], flag.ContinueOnError)
	set.SetOutput(stderr)
	repository := set.String("repo", ".", "open-cut repository root")
	if err := set.Parse(args[1:]); err != nil {
		return 2
	}
	if set.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: oc-control protocol <generate|check> [--repo <path>]")
		return 2
	}
	result, err := protocolgen.Run(ctx, *repository, mode, stderr, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "protocol %s: %v\n", mode, err)
		return 1
	}
	return writeOutput(stdout, stderr, result)
}

func runDev(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("dev", flag.ContinueOnError)
	set.SetOutput(stderr)
	repository := set.String("repo", ".", "open-cut repository root")
	baseDir := set.String("base-dir", "", "development base directory; defaults below the repository")
	skipBuild := set.Bool("skip-build", false, "use existing workspace build output")
	if err := set.Parse(args); err != nil {
		return 2
	}
	repositoryRoot, err := filepath.Abs(*repository)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	selectedBaseDir, err := devsession.ResolveBaseDir(repositoryRoot, *baseDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ready := make(chan devsession.Result, 1)
	done := make(chan error, 1)
	go func() {
		done <- devsession.Run(ctx, repositoryRoot, selectedBaseDir, stderr, stderr, *skipBuild, ready)
	}()
	select {
	case result := <-ready:
		if writeOutput(stdout, stderr, result) != 0 {
			return 1
		}
		if err := <-done; err != nil {
			fmt.Fprintf(stderr, "dev: %v\n", err)
			return 1
		}
		return 0
	case err := <-done:
		if err != nil {
			fmt.Fprintf(stderr, "dev: %v\n", err)
			return 1
		}
		return 0
	}
}

func runClean(args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("clean", flag.ContinueOnError)
	set.SetOutput(stderr)
	repository := set.String("repo", ".", "open-cut repository root")
	scope := set.String("scope", string(cleaner.ScopeTemp), "generated surface: temp, build, or all")
	dryRun := set.Bool("dry-run", false, "report without removing generated paths")
	if err := set.Parse(args); err != nil {
		return 2
	}
	report, err := cleaner.Clean(*repository, cleaner.Scope(*scope), *dryRun)
	if err != nil {
		fmt.Fprintf(stderr, "clean: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, report)
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("version", flag.ContinueOnError)
	set.SetOutput(stderr)
	asJSON := set.Bool("json", false, "print machine-readable build information")
	if err := set.Parse(args); err != nil {
		return 2
	}
	info := buildinfo.Current()
	if *asJSON {
		return writeOutput(stdout, stderr, info)
	}
	fmt.Fprintf(stdout, "oc-control %s %s\n", info.ModuleVersion, info.SidecarProtocol)
	fmt.Fprintf(stdout, "executable: %s\nrevision: %s\nmodified: %t\n", info.Executable, info.VCSRevision, info.VCSModified)
	return 0
}

type doctorCheck struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	OK      bool   `json:"ok"`
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("doctor", flag.ContinueOnError)
	set.SetOutput(stderr)
	if err := set.Parse(args); err != nil {
		return 2
	}
	if set.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: oc-control doctor")
		return 2
	}
	checks := make([]doctorCheck, 0, 3)
	passed := true
	for _, toolName := range []string{"go", "node", "pnpm"} {
		info, inspectErr := tool.Inspect(ctx, toolName)
		check := doctorCheck{Name: toolName, Path: info.Path, Version: info.Version, OK: inspectErr == nil}
		if !check.OK {
			passed = false
		}
		checks = append(checks, check)
	}
	json.NewEncoder(stdout).Encode(map[string]any{"ok": passed, "checks": checks})
	if !passed {
		return 1
	}
	return 0
}

func runBootstrap(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	set.SetOutput(stderr)
	repository := set.String("repo", ".", "open-cut repository root")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if set.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: oc-control bootstrap [--repo <path>]")
		return 2
	}
	result, err := devbootstrap.Run(ctx, devbootstrap.Options{
		RepositoryRoot: *repository,
		Stdout:         stderr,
		Stderr:         stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, result)
}

func runPack(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "pack requires a platform, --arch, and --version")
		return 2
	}
	platform := args[0]
	set := flag.NewFlagSet("pack "+platform, flag.ContinueOnError)
	set.SetOutput(stderr)
	arch := set.String("arch", "", "target architecture: arm64 or x64")
	output := set.String("output", "", "bundle path; defaults to dist/releases/<version>/<target>")
	version := set.String("version", "", "canonical X.Y.Z-channel.N version")
	repository := set.String("repo", ".", "open-cut repository root")
	launcher := set.String("launcher", "", "prebuilt target launcher")
	skipBuild := set.Bool("skip-build", false, "use existing pnpm build output")
	keepWork := set.Bool("keep-work", false, "preserve successful .tmp/oc-control/pack workspace")
	if err := set.Parse(args[1:]); err != nil {
		return 2
	}
	if *version == "" || *arch == "" {
		fmt.Fprintln(stderr, "pack requires a platform, --arch, and --version")
		return 2
	}
	buildTarget, err := target.New(platform, *arch)
	if err != nil {
		fmt.Fprintf(stderr, "pack: %v\n", err)
		return 2
	}
	result, err := packager.Pack(ctx, packager.Options{
		RepositoryRoot: *repository, Version: *version, Target: buildTarget, Output: *output, Launcher: *launcher,
		SkipBuild: *skipBuild, KeepWork: *keepWork, Stdout: stderr, Stderr: stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "pack: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, result)
}

func runRelease(args []string, stdout, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "display-version" {
		version, err := release.ParseVersion(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, version.Display())
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: oc-control release <keygen|create|display-version> [options]")
		return 2
	}
	switch args[0] {
	case "keygen":
		set := flag.NewFlagSet("release keygen", flag.ContinueOnError)
		set.SetOutput(stderr)
		output := set.String("output", "", "private development signing key path")
		keyID := set.String("id", "dev", "signing key ID")
		if err := set.Parse(args[1:]); err != nil {
			return 2
		}
		if *output == "" {
			fmt.Fprintln(stderr, "release keygen requires --output")
			return 2
		}
		key, err := publisher.GenerateKey(*output, *keyID)
		if err != nil {
			fmt.Fprintf(stderr, "release keygen: %v\n", err)
			return 1
		}
		return writeOutput(stdout, stderr, map[string]any{"schema": 1, "keyId": key.KeyID, "publicKey": key.PublicKey, "path": *output})
	case "create":
		set := flag.NewFlagSet("release create", flag.ContinueOnError)
		set.SetOutput(stderr)
		bundlePath := set.String("bundle", "", "target release bundle")
		origin := set.String("origin", "", "origin directory")
		key := set.String("key", "", "Ed25519 signing key generated by release keygen")
		expires := set.Duration("expires", 24*time.Hour, "release metadata validity")
		if err := set.Parse(args[1:]); err != nil {
			return 2
		}
		if *bundlePath == "" || *origin == "" || *key == "" {
			fmt.Fprintln(stderr, "release create requires --bundle, --origin, and --key")
			return 2
		}
		result, err := publisher.Create(*bundlePath, *origin, *key, *expires, time.Now())
		if err != nil {
			fmt.Fprintf(stderr, "release create: %v\n", err)
			return 1
		}
		return writeOutput(stdout, stderr, result)
	default:
		fmt.Fprintf(stderr, "unknown release command %q\n", args[0])
		return 2
	}
}

func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("serve", flag.ContinueOnError)
	set.SetOutput(stderr)
	root := set.String("root", "", "release origin directory")
	listen := set.String("listen", "127.0.0.1:0", "loopback TCP listen address")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if *root == "" {
		fmt.Fprintln(stderr, "serve requires --root")
		return 2
	}
	server, err := originserver.Start(*root, *listen)
	if err != nil {
		fmt.Fprintf(stderr, "serve: %v\n", err)
		return 1
	}
	defer server.Close()
	if writeOutput(stdout, stderr, map[string]any{"schema": 1, "root": server.Root, "endpoint": server.Endpoint}) != 0 {
		return 1
	}
	select {
	case <-ctx.Done():
		return 0
	case err := <-func() <-chan error {
		result := make(chan error, 1)
		go func() { result <- server.Wait() }()
		return result
	}():
		if err != nil {
			fmt.Fprintf(stderr, "serve: %v\n", err)
			return 1
		}
		return 0
	}
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "verify requires a target platform")
		return 2
	}
	set := flag.NewFlagSet("verify "+args[0], flag.ContinueOnError)
	set.SetOutput(stderr)
	arch := set.String("arch", "", "target architecture: arm64 or x64")
	bundlePath := set.String("bundle", "", "release bundle")
	origin := set.String("origin", "", "release origin directory")
	channel := set.String("channel", "", "release channel for origin verification")
	key := set.String("key", "", "release signing key for origin verification")
	if err := set.Parse(args[1:]); err != nil {
		return 2
	}
	buildTarget, err := target.New(args[0], *arch)
	if err != nil {
		fmt.Fprintf(stderr, "verify: %v\n", err)
		return 2
	}
	if (*bundlePath == "") == (*origin == "") {
		fmt.Fprintln(stderr, "verify requires exactly one of --bundle or --origin")
		return 2
	}
	var report verifier.Report
	if *bundlePath != "" {
		report, err = verifier.VerifyBundle(*bundlePath, buildTarget)
	} else {
		if *channel == "" || *key == "" {
			fmt.Fprintln(stderr, "origin verification requires --channel and --key")
			return 2
		}
		report, err = verifier.VerifyOrigin(*origin, *channel, buildTarget, *key, time.Now())
	}
	if err != nil {
		fmt.Fprintf(stderr, "verify: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, report)
}

func runInspect(args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("inspect", flag.ContinueOnError)
	set.SetOutput(stderr)
	receipt := set.String("receipt", "", "install receipt path")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if *receipt == "" {
		fmt.Fprintln(stderr, "inspect requires --receipt")
		return 2
	}
	result, err := delivery.Inspect(*receipt)
	if err != nil {
		fmt.Fprintf(stderr, "inspect: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, result)
}

func runStatus(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	controlClient, _, code := loadClient(args, stderr, true)
	if code != 0 {
		return code
	}
	status, err := controlClient.Status(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "status: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, status)
}

func runControl(ctx context.Context, command protocol.ControlCommand, args []string, stdout, stderr io.Writer) int {
	controlClient, paths, code := loadClient(args, stderr, false)
	if code != 0 {
		return code
	}
	response, err := controlClient.Control(ctx, command)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", command, err)
		return 1
	}
	if command == protocol.ControlCommandShutdown {
		if err := waitForControlExit(ctx, paths.ControlFile, 15*time.Second); err != nil {
			fmt.Fprintf(stderr, "shutdown: %v\n", err)
			return 1
		}
	}
	return writeOutput(stdout, stderr, response)
}

func loadClient(args []string, stderr io.Writer, observer bool) (*client.Client, layout.CellPaths, int) {
	set := flag.NewFlagSet("cell", flag.ContinueOnError)
	set.SetOutput(stderr)
	bootstrapPath := set.String("bootstrap", "", "bootstrap.json path")
	if err := set.Parse(args); err != nil {
		return nil, layout.CellPaths{}, 2
	}
	if *bootstrapPath == "" {
		fmt.Fprintln(stderr, "--bootstrap is required")
		return nil, layout.CellPaths{}, 2
	}
	bootstrap, err := config.LoadBootstrap(*bootstrapPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, layout.CellPaths{}, 1
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, layout.CellPaths{}, 1
	}
	tokenFile := paths.OwnerTokenFile
	if observer {
		tokenFile = paths.ObserverTokenFile
	}
	controlClient, err := client.Load(paths.ControlFile, tokenFile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, layout.CellPaths{}, 1
	}
	return controlClient, paths, 0
}

func waitForControlExit(ctx context.Context, controlFile string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(controlFile); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return fmt.Errorf("inspect control descriptor: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for control descriptor removal")
		case <-ticker.C:
		}
	}
}

func runHarness(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "guard" {
		return runHarnessGuard(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "install" {
		return runHarnessInstall(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "uninstall" {
		return runHarnessUninstall(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "run" {
		return runHarnessRun(ctx, args[1:], stdout, stderr)
	}
	if len(args) == 0 || (args[0] != "broker" && args[0] != "sidecars" && args[0] != "cold-start" && args[0] != "full-pack") {
		fmt.Fprintln(stderr, "usage: oc-control harness <guard|broker|sidecars|cold-start|full-pack|install|run|uninstall> [options]")
		return 2
	}
	scenario := args[0]
	set := flag.NewFlagSet("harness "+scenario, flag.ContinueOnError)
	set.SetOutput(stderr)
	workspace := set.String("workspace", "", "harness workspace; defaults below the repository")
	repository := set.String("repo", ".", "repository root for the sidecar-entry scenario")
	skipBuild := set.Bool("skip-build", false, "use existing workspace build output")
	bundlePath := set.String("bundle", "", "release-bundle.tar.zst for the full-pack scenario")
	if err := set.Parse(args[1:]); err != nil {
		return 2
	}
	selected := *workspace
	if selected == "" {
		repositoryRoot, err := filepath.Abs(*repository)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		selected = filepath.Join(repositoryRoot, ".tmp", "oc-control", "harness-"+scenario)
	} else {
		absolute, err := filepath.Abs(selected)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		selected = absolute
	}
	var report harness.Report
	if scenario == "broker" {
		report = harness.RunBroker(ctx, selected)
	} else if scenario == "full-pack" {
		if *bundlePath == "" {
			fmt.Fprintln(stderr, "harness full-pack requires --bundle")
			return 2
		}
		absoluteBundle, err := filepath.Abs(*bundlePath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		repositoryRoot, err := filepath.Abs(*repository)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		report = harness.RunFullPack(ctx, selected, repositoryRoot, absoluteBundle)
	} else if scenario == "sidecars" {
		repositoryRoot, err := filepath.Abs(*repository)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !*skipBuild {
			pnpm, resolveErr := tool.Resolve("pnpm")
			if resolveErr != nil {
				fmt.Fprintln(stderr, resolveErr)
				return 1
			}
			if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
				Executable: pnpm, Args: []string{"-r", "--if-present", "run", "build"}, Directory: repositoryRoot,
				Stdout: stderr, Stderr: stderr, Profile: lifecycle.ProfileHarness,
			}); err != nil {
				fmt.Fprintf(stderr, "workspace build: %v\n", err)
				return 1
			}
		}
		report = harness.RunSidecars(ctx, selected, repositoryRoot)
	} else {
		repositoryRoot, err := filepath.Abs(*repository)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		artifacts := filepath.Join(selected, "fixtures")
		if err := os.MkdirAll(artifacts, 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		hostTarget := target.Host()
		launcherArtifact := filepath.Join(artifacts, hostTarget.ExecutableName("launcher"))
		payloadArtifact := filepath.Join(artifacts, hostTarget.ExecutableName("fixture-runtime"))
		goTool, err := tool.Resolve("go")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, build := range []struct{ output, source string }{
			{launcherArtifact, "./cmd/launcher"}, {payloadArtifact, "./internal/harness/testpayload"},
		} {
			if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
				Executable: goTool, Args: []string{"build", "-o", build.output, build.source}, Directory: repositoryRoot,
				Stdout: stderr, Stderr: stderr, Profile: lifecycle.ProfileHarness,
			}); err != nil {
				fmt.Fprintf(stderr, "build %s: %v\n", build.source, err)
				return 1
			}
		}
		report = harness.RunColdStart(ctx, selected, launcherArtifact, payloadArtifact)
	}
	if writeOutput(stdout, stderr, report) != 0 || !report.Passed {
		return 1
	}
	return 0
}

func runHarnessGuard(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("harness guard", flag.ContinueOnError)
	set.SetOutput(stderr)
	repository := set.String("repo", ".", "open-cut repository root")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if set.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: oc-control harness guard [--repo <path>]")
		return 2
	}
	result := harnessguard.Run(ctx, *repository)
	if writeOutput(stdout, stderr, result) != 0 || !result.Passed {
		return 1
	}
	return 0
}

func runHarnessInstall(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "harness install requires a target platform")
		return 2
	}
	set := flag.NewFlagSet("harness install "+args[0], flag.ContinueOnError)
	set.SetOutput(stderr)
	arch := set.String("arch", "", "target architecture")
	repository := set.String("repo", ".", "open-cut repository root")
	workspace := set.String("workspace", "", "isolated install workspace")
	origin := set.String("origin", "", "verified release origin directory")
	originURL := set.String("origin-url", "", "HTTP origin consumed by the installed launcher")
	key := set.String("key", "", "development release key")
	channel := set.String("channel", "beta", "release channel")
	namespace := set.String("namespace", "delivery", "cell namespace")
	headless := set.Bool("headless", false, "run the installed Electron carrier without a window")
	if err := set.Parse(args[1:]); err != nil {
		return 2
	}
	if *arch == "" || *workspace == "" || *origin == "" || *originURL == "" || *key == "" {
		fmt.Fprintln(stderr, "harness install requires --arch, --workspace, --origin, --origin-url, and --key")
		return 2
	}
	buildTarget, err := target.New(args[0], *arch)
	if err != nil {
		fmt.Fprintf(stderr, "harness install: %v\n", err)
		return 2
	}
	result, err := delivery.Install(ctx, delivery.InstallOptions{
		RepositoryRoot: *repository, Workspace: *workspace, OriginRoot: *origin, OriginURL: *originURL,
		KeyPath: *key, Channel: *channel, Namespace: *namespace, Target: buildTarget,
		Headless: *headless, Stdout: stderr, Stderr: stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "harness install: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, result)
}

func runHarnessUninstall(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("harness uninstall", flag.ContinueOnError)
	set.SetOutput(stderr)
	workspace := set.String("workspace", "", "isolated install workspace")
	receipt := set.String("receipt", "", "install receipt path")
	purge := set.Bool("purge", false, "remove all receipt-owned cold-start roots")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if *workspace == "" || *receipt == "" {
		fmt.Fprintln(stderr, "harness uninstall requires --workspace and --receipt")
		return 2
	}
	result, err := delivery.Uninstall(ctx, *receipt, *workspace, *purge)
	if err != nil {
		fmt.Fprintf(stderr, "harness uninstall: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, result)
}

func runHarnessRun(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("harness run", flag.ContinueOnError)
	set.SetOutput(stderr)
	workspace := set.String("workspace", "", "isolated install workspace")
	receipt := set.String("receipt", "", "install receipt path")
	headless := set.Bool("headless", false, "run the installed Electron carrier without a window")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if *workspace == "" || *receipt == "" {
		fmt.Fprintln(stderr, "harness run requires --workspace and --receipt")
		return 2
	}
	result, err := delivery.Run(ctx, delivery.RunOptions{
		Receipt: *receipt, Workspace: *workspace, Headless: *headless, Stdout: stderr, Stderr: stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "harness run: %v\n", err)
		return 1
	}
	return writeOutput(stdout, stderr, result)
}

func writeOutput(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
