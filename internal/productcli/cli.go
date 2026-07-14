package productcli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printHelp(options.Stdout)
		return 0
	}
	if args[0] == "__active" {
		return runActive(ctx, args[1:], options.Stdout, options.Stderr)
	}
	if args[0] != "status" {
		fmt.Fprintf(options.Stderr, "unknown command %q\n", args[0])
		printHelp(options.Stderr)
		return 2
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
	receipt, err := install.LoadReceipt(*receiptPath)
	if err != nil {
		fmt.Fprintf(options.Stderr, "load install receipt: %v\n", err)
		return 1
	}
	resolution, err := ResolveActiveCLI(receipt.BootstrapPath)
	if err != nil {
		fmt.Fprintf(options.Stderr, "resolve active CLI: %v\n", err)
		return 1
	}
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: resolution.CLIExecutable,
		Args:       []string{"__active", "--bootstrap", receipt.BootstrapPath, "status"},
		Directory:  resolution.VersionRoot,
		Env:        os.Environ(),
		Stdout:     options.Stdout,
		Stderr:     options.Stderr,
		Profile:    lifecycle.ProfileProduction,
	}); err != nil {
		fmt.Fprintf(options.Stderr, "run active CLI: %v\n", err)
		return 1
	}
	return 0
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

func runActive(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("active", flag.ContinueOnError)
	set.SetOutput(stderr)
	bootstrapPath := set.String("bootstrap", "", "bootstrap path")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if *bootstrapPath == "" || set.NArg() != 1 || set.Arg(0) != "status" {
		fmt.Fprintln(stderr, "active CLI requires --bootstrap <path> status")
		return 2
	}
	return writeStatus(ctx, *bootstrapPath, stdout, stderr)
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

func printHelp(writer io.Writer) {
	fmt.Fprint(writer, `Open Cut CLI attaches to the active cell without depending on the product API.

Usage:
  open-cut status [--receipt <install-receipt.json>]
  open-cut status --bootstrap <bootstrap.json>
`)
}
