// Package controlcli is the oc-control command surface. cobra owns the
// command tree, flag parsing, help, and argument rejection; every leaf
// declares cobra.NoArgs or an exact positional arity, so accepting stray
// arguments is structurally impossible. Command bodies keep the historical
// contract: they print their own diagnostics to stderr and yield an exit
// code (0 success, 1 runtime failure, 2 usage), carried through cobra as an
// exitCodeError. Errors cobra itself produces (unknown command, bad flag,
// arity) are usage errors and exit 2.
package controlcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

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

// exitCodeError carries a command body's exit code through cobra. The body
// has already printed its diagnostics; Run only translates the code.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func asExit(code int) error {
	if code == 0 {
		return nil
	}
	return exitCodeError{code: code}
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := newRootCommand(stdout, stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var exit exitCodeError
	if errors.As(err, &exit) {
		return exit.code
	}
	fmt.Fprintln(stderr, err)
	return 2
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "oc-control",
		Short:         "oc-control controls the Open Cut development and runtime substrate.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.CompletionOptions.HiddenDefaultCmd = true
	root.AddCommand(
		newVersionCommand(stdout, stderr),
		newDoctorCommand(stdout, stderr),
		newBootstrapCommand(stdout, stderr),
		newCleanCommand(stdout, stderr),
		newDevCommand(stdout, stderr),
		newPackCommand(stdout, stderr),
		newProtocolCommand(stdout, stderr),
		newReleaseCommand(stdout, stderr),
		newServeCommand(stdout, stderr),
		newVerifyCommand(stdout, stderr),
		newInspectCommand(stdout, stderr),
		newStatusCommand(stdout, stderr),
		newControlCommand(protocol.ControlCommandShow, "Bring the cell's interactive payload to the foreground", stdout, stderr),
		newControlCommand(protocol.ControlCommandShutdown, "Shut down the running cell and wait for its control descriptor to vanish", stdout, stderr),
		newHarnessCommand(stdout, stderr),
	)
	return root
}

// requireSubcommand keeps the historical exit-2 contract for bare parent
// commands instead of cobra's default help-and-succeed.
func requireSubcommand(command *cobra.Command) {
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		fmt.Fprintln(cmd.ErrOrStderr(), cmd.UsageString())
		return exitCodeError{code: 2}
	}
	command.Args = cobra.NoArgs
}

func newVersionCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "version", Short: "Print build information", Args: cobra.NoArgs}
	asJSON := command.Flags().Bool("json", false, "print machine-readable build information")
	command.RunE = func(*cobra.Command, []string) error {
		info := buildinfo.Current()
		if *asJSON {
			return asExit(writeOutput(stdout, stderr, info))
		}
		fmt.Fprintf(stdout, "oc-control %s %s\n", info.ModuleVersion, info.SidecarProtocol)
		fmt.Fprintf(stdout, "executable: %s\nrevision: %s\nmodified: %t\n", info.Executable, info.VCSRevision, info.VCSModified)
		return nil
	}
	return command
}

type doctorCheck struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	OK      bool   `json:"ok"`
}

func newDoctorCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "doctor", Short: "Check required host tooling", Args: cobra.NoArgs}
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		checks := make([]doctorCheck, 0, 3)
		passed := true
		for _, toolName := range []string{"go", "node", "pnpm"} {
			info, inspectErr := tool.Inspect(cmd.Context(), toolName)
			check := doctorCheck{Name: toolName, Path: info.Path, Version: info.Version, OK: inspectErr == nil}
			if !check.OK {
				passed = false
			}
			checks = append(checks, check)
		}
		json.NewEncoder(stdout).Encode(map[string]any{"ok": passed, "checks": checks})
		if !passed {
			return exitCodeError{code: 1}
		}
		return nil
	}
	_ = stderr
	return command
}

func newBootstrapCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "bootstrap", Short: "Prepare the repository development substrate", Args: cobra.NoArgs}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		result, err := devbootstrap.Run(cmd.Context(), devbootstrap.Options{
			RepositoryRoot: *repository,
			Stdout:         stderr,
			Stderr:         stderr,
		})
		if err != nil {
			fmt.Fprintf(stderr, "bootstrap: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, result))
	}
	return command
}

func newCleanCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "clean", Short: "Remove generated surfaces", Args: cobra.NoArgs}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	scope := command.Flags().String("scope", string(cleaner.ScopeTemp), "generated surface: temp, build, quick, or all")
	dryRun := command.Flags().Bool("dry-run", false, "report without removing generated paths")
	command.RunE = func(*cobra.Command, []string) error {
		report, err := cleaner.Clean(*repository, cleaner.Scope(*scope), *dryRun)
		if err != nil {
			fmt.Fprintf(stderr, "clean: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, report))
	}
	return command
}

func newDevCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "dev", Short: "Run the development cell", Args: cobra.NoArgs}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	baseDir := command.Flags().String("base-dir", "", "development base directory; defaults below the repository")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		return asExit(runDev(cmd.Context(), *repository, *baseDir, stdout, stderr))
	}
	command.AddCommand(newDevInspectCommand(stdout, stderr), newDevRecordCommand(stdout, stderr))
	return command
}

func runDev(ctx context.Context, repository, baseDir string, stdout, stderr io.Writer) int {
	repositoryRoot, err := filepath.Abs(repository)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	selectedBaseDir, err := devsession.ResolveBaseDir(repositoryRoot, baseDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ready := make(chan devsession.Result, 1)
	done := make(chan error, 1)
	go func() {
		done <- devsession.Run(ctx, repositoryRoot, selectedBaseDir, stderr, stderr, ready)
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

func newPackCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "pack <mac|win|linux>",
		Short: "Build a signed release bundle for a target",
		Args:  cobra.ExactArgs(1),
	}
	arch := command.Flags().String("arch", "", "target architecture: arm64 or x64")
	output := command.Flags().String("output", "", "bundle path; defaults to dist/releases/<version>/<target>")
	version := command.Flags().String("version", "", "canonical X.Y.Z-channel.N version")
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	launcher := command.Flags().String("launcher", "", "prebuilt target launcher")
	keepWork := command.Flags().Bool("keep-work", false, "preserve successful .tmp/oc-control/pack workspace")
	command.RunE = func(cmd *cobra.Command, args []string) error {
		if *version == "" || *arch == "" {
			fmt.Fprintln(stderr, "pack requires a platform, --arch, and --version")
			return exitCodeError{code: 2}
		}
		buildTarget, err := target.New(args[0], *arch)
		if err != nil {
			fmt.Fprintf(stderr, "pack: %v\n", err)
			return exitCodeError{code: 2}
		}
		result, err := packager.Pack(cmd.Context(), packager.Options{
			RepositoryRoot: *repository, Version: *version, Target: buildTarget, Output: *output, Launcher: *launcher,
			KeepWork: *keepWork, Stdout: stderr, Stderr: stderr,
		})
		if err != nil {
			fmt.Fprintf(stderr, "pack: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, result))
	}
	return command
}

func newProtocolCommand(stdout, stderr io.Writer) *cobra.Command {
	parent := &cobra.Command{Use: "protocol", Short: "Generate or check the sidecar protocol surface"}
	requireSubcommand(parent)
	for _, mode := range []protocolgen.Mode{protocolgen.ModeGenerate, protocolgen.ModeCheck} {
		command := &cobra.Command{Use: string(mode), Short: string(mode) + " the generated protocol surface", Args: cobra.NoArgs}
		repository := command.Flags().String("repo", ".", "open-cut repository root")
		command.RunE = func(cmd *cobra.Command, _ []string) error {
			result, err := protocolgen.Run(cmd.Context(), *repository, mode, stderr, stderr)
			if err != nil {
				fmt.Fprintf(stderr, "protocol %s: %v\n", mode, err)
				return exitCodeError{code: 1}
			}
			return asExit(writeOutput(stdout, stderr, result))
		}
		parent.AddCommand(command)
	}
	return parent
}

func newReleaseCommand(stdout, stderr io.Writer) *cobra.Command {
	parent := &cobra.Command{Use: "release", Short: "Create and sign release material"}
	requireSubcommand(parent)

	keygen := &cobra.Command{Use: "keygen", Short: "Generate a development signing key", Args: cobra.NoArgs}
	keygenOutput := keygen.Flags().String("output", "", "private development signing key path")
	keyID := keygen.Flags().String("id", "dev", "signing key ID")
	keygen.RunE = func(*cobra.Command, []string) error {
		if *keygenOutput == "" {
			fmt.Fprintln(stderr, "release keygen requires --output")
			return exitCodeError{code: 2}
		}
		key, err := publisher.GenerateKey(*keygenOutput, *keyID)
		if err != nil {
			fmt.Fprintf(stderr, "release keygen: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, map[string]any{"schema": 1, "keyId": key.KeyID, "publicKey": key.PublicKey, "path": *keygenOutput}))
	}

	create := &cobra.Command{Use: "create", Short: "Publish a signed release into an origin", Args: cobra.NoArgs}
	bundlePath := create.Flags().String("bundle", "", "target release bundle")
	origin := create.Flags().String("origin", "", "origin directory")
	key := create.Flags().String("key", "", "Ed25519 signing key generated by release keygen")
	expires := create.Flags().Duration("expires", 24*time.Hour, "release metadata validity")
	create.RunE = func(*cobra.Command, []string) error {
		if *bundlePath == "" || *origin == "" || *key == "" {
			fmt.Fprintln(stderr, "release create requires --bundle, --origin, and --key")
			return exitCodeError{code: 2}
		}
		result, err := publisher.Create(*bundlePath, *origin, *key, *expires, time.Now())
		if err != nil {
			fmt.Fprintf(stderr, "release create: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, result))
	}

	displayVersion := &cobra.Command{Use: "display-version <X.Y.Z-channel.N>", Short: "Print the display form of a canonical version", Args: cobra.ExactArgs(1)}
	displayVersion.RunE = func(_ *cobra.Command, args []string) error {
		version, err := release.ParseVersion(args[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitCodeError{code: 1}
		}
		fmt.Fprintln(stdout, version.Display())
		return nil
	}

	parent.AddCommand(keygen, create, displayVersion)
	return parent
}

func newServeCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "serve", Short: "Serve a release origin over loopback HTTP", Args: cobra.NoArgs}
	root := command.Flags().String("root", "", "release origin directory")
	listen := command.Flags().String("listen", "127.0.0.1:0", "loopback TCP listen address")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		if *root == "" {
			fmt.Fprintln(stderr, "serve requires --root")
			return exitCodeError{code: 2}
		}
		server, err := originserver.Start(*root, *listen)
		if err != nil {
			fmt.Fprintf(stderr, "serve: %v\n", err)
			return exitCodeError{code: 1}
		}
		defer server.Close()
		if writeOutput(stdout, stderr, map[string]any{"schema": 1, "root": server.Root, "endpoint": server.Endpoint}) != 0 {
			return exitCodeError{code: 1}
		}
		served := make(chan error, 1)
		go func() { served <- server.Wait() }()
		select {
		case <-cmd.Context().Done():
			return nil
		case err := <-served:
			if err != nil {
				fmt.Fprintf(stderr, "serve: %v\n", err)
				return exitCodeError{code: 1}
			}
			return nil
		}
	}
	return command
}

func newVerifyCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "verify <mac|win|linux>",
		Short: "Verify a release bundle or origin for a target",
		Args:  cobra.ExactArgs(1),
	}
	arch := command.Flags().String("arch", "", "target architecture: arm64 or x64")
	bundlePath := command.Flags().String("bundle", "", "release bundle")
	origin := command.Flags().String("origin", "", "release origin directory")
	channel := command.Flags().String("channel", "", "release channel for origin verification")
	key := command.Flags().String("key", "", "release signing key for origin verification")
	command.RunE = func(_ *cobra.Command, args []string) error {
		buildTarget, err := target.New(args[0], *arch)
		if err != nil {
			fmt.Fprintf(stderr, "verify: %v\n", err)
			return exitCodeError{code: 2}
		}
		if (*bundlePath == "") == (*origin == "") {
			fmt.Fprintln(stderr, "verify requires exactly one of --bundle or --origin")
			return exitCodeError{code: 2}
		}
		var report verifier.Report
		if *bundlePath != "" {
			report, err = verifier.VerifyBundle(*bundlePath, buildTarget)
		} else {
			if *channel == "" || *key == "" {
				fmt.Fprintln(stderr, "origin verification requires --channel and --key")
				return exitCodeError{code: 2}
			}
			report, err = verifier.VerifyOrigin(*origin, *channel, buildTarget, *key, time.Now())
		}
		if err != nil {
			fmt.Fprintf(stderr, "verify: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, report))
	}
	return command
}

func newInspectCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "inspect", Short: "Inspect an install receipt", Args: cobra.NoArgs}
	receipt := command.Flags().String("receipt", "", "install receipt path")
	command.RunE = func(*cobra.Command, []string) error {
		if *receipt == "" {
			fmt.Fprintln(stderr, "inspect requires --receipt")
			return exitCodeError{code: 2}
		}
		result, err := delivery.Inspect(*receipt)
		if err != nil {
			fmt.Fprintf(stderr, "inspect: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, result))
	}
	return command
}

func newStatusCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "status", Short: "Read a running cell's status", Args: cobra.NoArgs}
	bootstrapPath := command.Flags().String("bootstrap", "", "bootstrap.json path")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		controlClient, _, code := loadClient(*bootstrapPath, stderr, true)
		if code != 0 {
			return exitCodeError{code: code}
		}
		status, err := controlClient.Status(cmd.Context())
		if err != nil {
			fmt.Fprintf(stderr, "status: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, status))
	}
	return command
}

func newControlCommand(control protocol.ControlCommand, short string, stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: string(control), Short: short, Args: cobra.NoArgs}
	bootstrapPath := command.Flags().String("bootstrap", "", "bootstrap.json path")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		controlClient, paths, code := loadClient(*bootstrapPath, stderr, false)
		if code != 0 {
			return exitCodeError{code: code}
		}
		response, err := controlClient.Control(cmd.Context(), control)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", control, err)
			return exitCodeError{code: 1}
		}
		if control == protocol.ControlCommandShutdown {
			if err := waitForControlExit(cmd.Context(), paths.ControlFile, 15*time.Second); err != nil {
				fmt.Fprintf(stderr, "shutdown: %v\n", err)
				return exitCodeError{code: 1}
			}
		}
		return asExit(writeOutput(stdout, stderr, response))
	}
	return command
}

func loadClient(bootstrapPath string, stderr io.Writer, observer bool) (*client.Client, layout.CellPaths, int) {
	if bootstrapPath == "" {
		fmt.Fprintln(stderr, "--bootstrap is required")
		return nil, layout.CellPaths{}, 2
	}
	bootstrap, err := config.LoadBootstrap(bootstrapPath)
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

func newHarnessCommand(stdout, stderr io.Writer) *cobra.Command {
	parent := &cobra.Command{Use: "harness", Short: "Run substrate acceptance scenarios"}
	requireSubcommand(parent)

	guard := &cobra.Command{Use: "guard", Short: "Run the repository guard checks", Args: cobra.NoArgs}
	guardRepository := guard.Flags().String("repo", ".", "open-cut repository root")
	guard.RunE = func(cmd *cobra.Command, _ []string) error {
		result := harnessguard.Run(cmd.Context(), *guardRepository)
		if writeOutput(stdout, stderr, result) != 0 || !result.Passed {
			return exitCodeError{code: 1}
		}
		return nil
	}
	parent.AddCommand(guard)

	for _, scenario := range []string{"broker", "sidecars", "cold-start", "full-pack"} {
		command := &cobra.Command{Use: scenario, Short: "Run the " + scenario + " scenario", Args: cobra.NoArgs}
		workspace := command.Flags().String("workspace", "", "harness workspace; defaults below the repository")
		repository := command.Flags().String("repo", ".", "repository root for the sidecar-entry scenario")
		var bundlePath *string
		if scenario == "full-pack" {
			bundlePath = command.Flags().String("bundle", "", "release-bundle.tar.zst for the full-pack scenario")
		}
		command.RunE = func(cmd *cobra.Command, _ []string) error {
			bundle := ""
			if bundlePath != nil {
				bundle = *bundlePath
			}
			return asExit(runHarnessScenario(cmd.Context(), scenario, *workspace, *repository, bundle, stdout, stderr))
		}
		parent.AddCommand(command)
	}

	parent.AddCommand(
		newHarnessInstallCommand(stdout, stderr),
		newHarnessRunCommand(stdout, stderr),
		newHarnessUninstallCommand(stdout, stderr),
	)
	return parent
}

func runHarnessScenario(ctx context.Context, scenario, workspace, repository, bundlePath string, stdout, stderr io.Writer) int {
	selected := workspace
	if selected == "" {
		repositoryRoot, err := filepath.Abs(repository)
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
		if bundlePath == "" {
			fmt.Fprintln(stderr, "harness full-pack requires --bundle")
			return 2
		}
		absoluteBundle, err := filepath.Abs(bundlePath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		repositoryRoot, err := filepath.Abs(repository)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		report = harness.RunFullPack(ctx, selected, repositoryRoot, absoluteBundle)
	} else if scenario == "sidecars" {
		repositoryRoot, err := filepath.Abs(repository)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
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
		report = harness.RunSidecars(ctx, selected, repositoryRoot)
	} else {
		repositoryRoot, err := filepath.Abs(repository)
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

func newHarnessInstallCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "install <mac|win|linux>",
		Short: "Install a verified release into an isolated workspace",
		Args:  cobra.ExactArgs(1),
	}
	arch := command.Flags().String("arch", "", "target architecture")
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	workspace := command.Flags().String("workspace", "", "isolated install workspace")
	origin := command.Flags().String("origin", "", "verified release origin directory")
	originURL := command.Flags().String("origin-url", "", "HTTP origin consumed by the installed launcher")
	key := command.Flags().String("key", "", "development release key")
	channel := command.Flags().String("channel", "beta", "release channel")
	namespace := command.Flags().String("namespace", "delivery", "cell namespace")
	headless := command.Flags().Bool("headless", false, "run the installed Electron carrier without a window")
	command.RunE = func(cmd *cobra.Command, args []string) error {
		if *arch == "" || *workspace == "" || *origin == "" || *originURL == "" || *key == "" {
			fmt.Fprintln(stderr, "harness install requires --arch, --workspace, --origin, --origin-url, and --key")
			return exitCodeError{code: 2}
		}
		buildTarget, err := target.New(args[0], *arch)
		if err != nil {
			fmt.Fprintf(stderr, "harness install: %v\n", err)
			return exitCodeError{code: 2}
		}
		result, err := delivery.Install(cmd.Context(), delivery.InstallOptions{
			RepositoryRoot: *repository, Workspace: *workspace, OriginRoot: *origin, OriginURL: *originURL,
			KeyPath: *key, Channel: *channel, Namespace: *namespace, Target: buildTarget,
			Headless: *headless, Stdout: stderr, Stderr: stderr,
		})
		if err != nil {
			fmt.Fprintf(stderr, "harness install: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, result))
	}
	return command
}

func newHarnessRunCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "run", Short: "Run an installed workspace", Args: cobra.NoArgs}
	workspace := command.Flags().String("workspace", "", "isolated install workspace")
	receipt := command.Flags().String("receipt", "", "install receipt path")
	headless := command.Flags().Bool("headless", false, "run the installed Electron carrier without a window")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		if *workspace == "" || *receipt == "" {
			fmt.Fprintln(stderr, "harness run requires --workspace and --receipt")
			return exitCodeError{code: 2}
		}
		result, err := delivery.Run(cmd.Context(), delivery.RunOptions{
			Receipt: *receipt, Workspace: *workspace, Headless: *headless, Stdout: stderr, Stderr: stderr,
		})
		if err != nil {
			fmt.Fprintf(stderr, "harness run: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, result))
	}
	return command
}

func newHarnessUninstallCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "uninstall", Short: "Uninstall an installed workspace", Args: cobra.NoArgs}
	workspace := command.Flags().String("workspace", "", "isolated install workspace")
	receipt := command.Flags().String("receipt", "", "install receipt path")
	purge := command.Flags().Bool("purge", false, "remove all receipt-owned cold-start roots")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		if *workspace == "" || *receipt == "" {
			fmt.Fprintln(stderr, "harness uninstall requires --workspace and --receipt")
			return exitCodeError{code: 2}
		}
		result, err := delivery.Uninstall(cmd.Context(), *receipt, *workspace, *purge)
		if err != nil {
			fmt.Fprintf(stderr, "harness uninstall: %v\n", err)
			return exitCodeError{code: 1}
		}
		return asExit(writeOutput(stdout, stderr, result))
	}
	return command
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
