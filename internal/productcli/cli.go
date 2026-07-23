package productcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/install"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

type Resolution struct {
	Active        string `json:"active"`
	Bootstrap     string `json:"bootstrap"`
	VersionRoot   string `json:"versionRoot"`
	CLIExecutable string `json:"cliExecutable"`
}

type Status struct {
	Schema int             `json:"schema"`
	Active string          `json:"active"`
	Cell   protocol.Status `json:"cell"`
}

type Options struct {
	Executable string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
}

func Run(ctx context.Context, args []string, options Options) int {
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	if options.Stdin == nil {
		options.Stdin = strings.NewReader("")
	}
	if len(args) > 0 && args[0] == "__active" {
		return runActive(ctx, args[1:], options.Stdin, options.Stdout, options.Stderr)
	}
	if len(args) == 0 || isHelpInvocation(args) || (len(args) == 1 && args[0] == "help") {
		if len(args) == 0 || args[0] == "help" {
			args = []string{"--help"}
		}
		return dispatchActive(ctx, args, "", options)
	}
	if args[0] != "status" {
		return dispatchActive(ctx, args, "", options)
	}
	set := flag.NewFlagSet("status", flag.ContinueOnError)
	set.SetOutput(options.Stderr)
	receiptPath := set.String("receipt", "", "install receipt path")
	bootstrapPath := set.String("bootstrap", "", "bootstrap path for direct versioned invocation")
	if err := set.Parse(args[1:]); err != nil || set.NArg() != 0 {
		return 2
	}
	if *bootstrapPath != "" {
		return writeStatus(ctx, *bootstrapPath, options.Stdout, options.Stderr)
	}
	if *receiptPath == "" {
		if options.Executable == "" {
			fmt.Fprintln(options.Stderr, "cannot locate the install receipt without an executable path")
			return 1
		}
		*receiptPath = lifecycle.DefaultReceiptPath(options.Executable)
	}
	return dispatchActive(ctx, []string{"status"}, *receiptPath, options)
}

func ResolveActiveCLI(bootstrapPath string) (Resolution, error) {
	bootstrap, err := config.LoadBootstrap(bootstrapPath)
	if err != nil {
		return Resolution{}, err
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		return Resolution{}, err
	}
	runtimeState, err := state.Load(paths.StateFile, identity.Channel)
	if err != nil {
		return Resolution{}, err
	}
	if runtimeState.Active == "" {
		return Resolution{}, fmt.Errorf("cell has no active release")
	}
	versionRoot := filepath.Join(paths.Versions, runtimeState.Active)
	manifest, err := release.LoadManifest(filepath.Join(versionRoot, "manifest.json"))
	if err != nil {
		return Resolution{}, err
	}
	if manifest.Version != runtimeState.Active {
		return Resolution{}, fmt.Errorf("active release manifest version does not match runtime state")
	}
	if err := manifest.ValidateHost(identity.Channel, bootstrap.ProtocolFloor); err != nil {
		return Resolution{}, err
	}
	executable, err := release.ResolveCLI(versionRoot, manifest)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{
		Active: runtimeState.Active, Bootstrap: bootstrapPath, VersionRoot: versionRoot, CLIExecutable: executable,
	}, nil
}

func runActive(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 3 || args[0] != "--bootstrap" || args[1] == "" {
		fmt.Fprintln(stderr, "active CLI requires --bootstrap <path> <command>")
		return 2
	}
	bootstrapPath := args[1]
	commandArgs := args[2:]
	if len(commandArgs) == 1 && commandArgs[0] == "status" {
		return writeStatus(ctx, bootstrapPath, stdout, stderr)
	}
	if isHelpInvocation(commandArgs) {
		path := append([]string(nil), commandArgs[:len(commandArgs)-1]...)
		return writeDiscovery(bootstrapPath, path, stdout, stderr)
	}
	version, err := activeVersion(bootstrapPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return runBusiness(ctx, bootstrapPath, version, commandArgs, stdin, stdout, stderr)
}

func dispatchActive(ctx context.Context, args []string, receiptPath string, options Options) int {
	if receiptPath == "" {
		if options.Executable == "" {
			fmt.Fprintln(options.Stderr, "cannot locate the install receipt without an executable path")
			return 1
		}
		receiptPath = lifecycle.DefaultReceiptPath(options.Executable)
	}
	receipt, err := install.LoadReceipt(receiptPath)
	if err != nil {
		fmt.Fprintf(options.Stderr, "load install receipt: %v\n", err)
		return 1
	}
	if shouldEnsureBusinessReady(args) {
		if err := ensureBusinessReady(ctx, receipt, 2*time.Minute); err != nil {
			fmt.Fprintf(options.Stderr, "ensure product ready: %v\n", err)
		}
	}
	resolution, err := ResolveActiveCLI(receipt.BootstrapPath)
	if err != nil {
		fmt.Fprintf(options.Stderr, "resolve active CLI: %v\n", err)
		return 1
	}
	activeArgs := []string{"__active", "--bootstrap", receipt.BootstrapPath}
	activeArgs = append(activeArgs, args...)
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: resolution.CLIExecutable,
		Args:       activeArgs,
		Directory:  resolution.VersionRoot,
		Env:        activeCLIEnvironment(os.Environ(), receipt.HostPath),
		Stdin:      options.Stdin,
		Stdout:     options.Stdout,
		Stderr:     options.Stderr,
		Profile:    lifecycle.ProfileProduction,
	}); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode()
		}
		fmt.Fprintf(options.Stderr, "run active CLI: %v\n", err)
		return 1
	}
	return 0
}

func shouldEnsureBusinessReady(args []string) bool {
	return !isHelpInvocation(args) && !(len(args) == 1 && args[0] == "status")
}

func ensureBusinessReady(ctx context.Context, receipt install.Receipt, timeout time.Duration) error {
	bootstrap, err := config.LoadBootstrap(receipt.BootstrapPath)
	if err != nil {
		return err
	}
	identity, err := cell.New(bootstrap.Channel, bootstrap.Namespace)
	if err != nil {
		return err
	}
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		return err
	}
	if ready, reachable := observerAPIReady(ctx, paths); ready {
		return nil
	} else if !reachable {
		process, startErr := lifecycle.Start(ctx, lifecycle.ProcessSpec{
			Executable: receipt.HostPath, Directory: receipt.InstallRoot, Env: os.Environ(),
			Profile: lifecycle.ProfileProduction, Presentation: lifecycle.PresentationHeadless, Detached: true,
		})
		if startErr != nil {
			return fmt.Errorf("start installed lifecycle host: %w", startErr)
		}
		go func() { _ = process.Wait() }()
	}
	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-waitContext.Done():
			return fmt.Errorf("product API did not become ready within %s: %w", timeout, waitContext.Err())
		case <-ticker.C:
			if ready, _ := observerAPIReady(waitContext, paths); ready {
				return nil
			}
		}
	}
}

func observerAPIReady(ctx context.Context, paths layout.CellPaths) (ready, reachable bool) {
	observer, err := client.Load(paths.ControlFile, paths.ObserverTokenFile)
	if err != nil {
		return false, false
	}
	status, err := observer.Status(ctx)
	if err != nil {
		return false, false
	}
	for _, session := range status.Sessions {
		if session.App == "api" && session.Ready {
			return true, true
		}
	}
	return false, true
}

func activeCLIEnvironment(base []string, platformHost string) []string {
	blocked := map[string]bool{
		lifecycle.PlatformHostEnvironment: true,
		lifecycle.SignerSocketEnvironment: true,
	}
	result := make([]string, 0, len(base)+1)
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if !blocked[name] {
			result = append(result, entry)
		}
	}
	return append(result, lifecycle.PlatformHostEnvironment+"="+platformHost)
}

func writeDiscovery(bootstrapPath string, path []string, stdout, stderr io.Writer) int {
	version, err := activeVersion(bootstrapPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return writeDiscoveryForVersion(version, path, stdout, stderr)
}

func isHelpInvocation(args []string) bool {
	if len(args) < 1 || len(args) > 3 || args[len(args)-1] != "--help" {
		return false
	}
	for _, segment := range args[:len(args)-1] {
		if segment == "" || segment[0] == '-' {
			return false
		}
	}
	return true
}

func writeStatus(ctx context.Context, bootstrapPath string, stdout, stderr io.Writer) int {
	bootstrap, err := config.LoadBootstrap(bootstrapPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	runtimeState, err := state.Load(paths.StateFile, identity.Channel)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	observer, err := client.Load(paths.ControlFile, paths.ObserverTokenFile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cellStatus, err := observer.Status(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(Status{Schema: 1, Active: runtimeState.Active, Cell: cellStatus}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
