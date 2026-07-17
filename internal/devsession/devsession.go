package devsession

import (
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/runtimehost"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/tool"
)

type Result struct {
	Schema      int             `json:"schema"`
	BaseDir     string          `json:"baseDir"`
	ControlFile string          `json:"controlFile"`
	Apps        []string        `json:"apps"`
	Status      protocol.Status `json:"status"`
}

// CDPPortEnvironment mirrors the Electron payload's harness CDP opt-in; dev
// sessions reserve a loopback port so tooling can inspect the live renderer.
const CDPPortEnvironment = "OPEN_CUT_HARNESS_CDP_PORT"

// PayloadCDPEndpointName is the sidecar session endpoint under which the
// Electron payload publishes its loopback CDP origin.
const PayloadCDPEndpointName = "payload-cdp"

// ResolveCellPaths maps a resolved dev base directory to its cell layout so
// tooling can reach the live cell's control descriptor and owner token.
func ResolveCellPaths(baseDir string) (layout.CellPaths, error) {
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return layout.CellPaths{}, err
	}
	identity, err := cell.New("dev", "default")
	if err != nil {
		return layout.CellPaths{}, err
	}
	commandRoot := filepath.Dir(filepath.Dir(baseDir))
	if filepath.Join(commandRoot, identity.Suffix()) != baseDir {
		return layout.CellPaths{}, fmt.Errorf("development base directory must end in %s", identity.Suffix())
	}
	return layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(commandRoot, "bootstrap"), StoreRoot: filepath.Join(commandRoot, "store"),
		CacheRoot: filepath.Join(commandRoot, "cache"), RuntimeRoot: filepath.Join(commandRoot, "runtime"),
		LogRoot: filepath.Join(commandRoot, "logs"),
	}, identity)
}

func ResolveBaseDir(repositoryRoot, requested string) (string, error) {
	repositoryRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", err
	}
	identity, _ := cell.New("dev", "default")
	baseDir := requested
	if baseDir == "" {
		baseDir = filepath.Join(repositoryRoot, ".tmp", "oc-control", "dev", identity.Suffix())
	} else {
		baseDir, err = filepath.Abs(baseDir)
		if err != nil {
			return "", err
		}
	}
	commandRoot := filepath.Dir(filepath.Dir(baseDir))
	if filepath.Join(commandRoot, identity.Suffix()) != baseDir {
		return "", fmt.Errorf("development base directory must end in %s", identity.Suffix())
	}
	return baseDir, nil
}

func Run(ctx context.Context, repositoryRoot, baseDir string, stdout, stderr io.Writer, ready chan<- Result) error {
	return run(ctx, repositoryRoot, baseDir, stdout, stderr, ready, buildWorkspace)
}

type workspaceBuilder func(context.Context, string, io.Writer) error

func run(
	ctx context.Context,
	repositoryRoot, baseDir string,
	stdout, stderr io.Writer,
	ready chan<- Result,
	build workspaceBuilder,
) error {
	repositoryRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return err
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return err
	}
	identity, err := cell.New("dev", "default")
	if err != nil {
		return err
	}
	commandRoot := filepath.Dir(filepath.Dir(baseDir))
	if filepath.Join(commandRoot, identity.Suffix()) != baseDir {
		return fmt.Errorf("development base directory must end in %s", identity.Suffix())
	}
	paths, err := ResolveCellPaths(baseDir)
	if err != nil {
		return err
	}
	// The broker lock is the cell reservation. Acquire it before workspace build
	// so competing dev invocations cannot amplify CPU and memory usage.
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1})
	if err != nil {
		return err
	}
	defer cellBroker.Close()
	if err := build(ctx, repositoryRoot, stderr); err != nil {
		return fmt.Errorf("build workspace: %w", err)
	}
	controlConfig, err := workspace.Load(repositoryRoot)
	if err != nil {
		return err
	}
	topology, err := workspace.DiscoverTopology(repositoryRoot, controlConfig)
	if err != nil {
		return err
	}
	plan, err := ResolvePlan(repositoryRoot, controlConfig, topology)
	if err != nil {
		return err
	}
	installation, err := lifecycle.EnsureDevelopmentInstallationIdentity(
		filepath.Join(commandRoot, "identity"), controlConfig.InstallationKeyRoles,
	)
	if err != nil {
		return fmt.Errorf("load development installation identity: %w", err)
	}
	signer, err := lifecycle.StartDevelopmentSigner(filepath.Join(paths.Runtime, "signer.sock"), installation)
	if err != nil {
		return fmt.Errorf("start development lifecycle signer: %w", err)
	}
	defer signer.Close()
	runtimeToken, err := cellBroker.MintRuntimeToken("payload", 7*24*time.Hour)
	if err != nil {
		return err
	}
	cdpPort, err := reserveLoopbackPort()
	if err != nil {
		return fmt.Errorf("reserve development CDP port: %w", err)
	}
	runtimeReady := make(chan runtimehost.Result, 1)
	done := make(chan error, 1)
	go func() {
		done <- runtimehost.Run(ctx, runtimehost.Options{
			Descriptor: cellBroker.Descriptor(), Token: runtimeToken,
			Channel: identity.Channel, Namespace: identity.Namespace, DataDir: baseDir,
			Installation: installation.Assertion(), App: "runtime",
			Environment: map[string]string{
				lifecycle.SignerSocketEnvironment: signer.Socket(),
				CDPPortEnvironment:                strconv.Itoa(cdpPort),
			},
			Mode: protocol.LifecycleModeDev, Presentation: protocol.PresentationInteractive, Source: "oc-control",
			Plan: plan, Stdout: stdout, Stderr: stderr,
		}, runtimeReady)
	}()
	select {
	case runtimeResult := <-runtimeReady:
		ready <- Result{
			Schema: 1, BaseDir: baseDir, ControlFile: paths.ControlFile,
			Apps: runtimeResult.Apps, Status: runtimeResult.Status,
		}
		return <-done
	case err := <-done:
		return err
	}
}

// reserveLoopbackPort picks a currently free ephemeral port for the payload's
// CDP listener; the brief release window before Electron binds is acceptable
// for a single-creator development cell.
func reserveLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func buildWorkspace(ctx context.Context, repositoryRoot string, output io.Writer) error {
	pnpm, err := tool.Resolve("pnpm")
	if err != nil {
		return err
	}
	return lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: pnpm, Args: []string{"-r", "--if-present", "run", "build"}, Directory: repositoryRoot,
		Stdout: output, Stderr: output, Profile: lifecycle.ProfileDevelopment,
	})
}

// ResolvePlan adapts the language-neutral app manifests into host commands for
// checkout execution. Development and harness paths share this one resolver.
func ResolvePlan(repositoryRoot string, config workspace.Config, topology workspace.Topology) (runtimetopology.Plan, error) {
	var node string
	var electron string
	plan := runtimetopology.Plan{Processes: make([]runtimetopology.ResolvedProcess, 0, len(topology.Sidecars))}
	for _, sidecar := range topology.Sidecars {
		appRoot := filepath.Join(repositoryRoot, "apps", sidecar.App)
		process := runtimetopology.ResolvedProcess{
			App: sidecar.App, Args: append([]string(nil), sidecar.Args...), WorkingDirectory: appRoot,
		}
		switch sidecar.Command {
		case workspace.SidecarCommandPayload:
			if electron == "" {
				resolved, err := tool.ResolveElectron(repositoryRoot, config.PayloadWorkspace)
				if err != nil {
					return runtimetopology.Plan{}, err
				}
				electron = resolved
			}
			process.Command = electron
			process.Args = []string{"."}
			process.UnsetEnv = []string{"ELECTRON_RUN_AS_NODE"}
			process.Sandbox = lifecycle.SandboxChromium
		case workspace.SidecarCommandNode:
			if node == "" {
				resolved, err := tool.Resolve("node")
				if err != nil {
					return runtimetopology.Plan{}, fmt.Errorf("resolve Node runtime: %w", err)
				}
				node = resolved
			}
			process.Command = node
		default:
			process.Command = filepath.Join(appRoot, filepath.FromSlash(sidecar.Command))
		}
		plan.Processes = append(plan.Processes, process)
	}
	if err := plan.Validate(); err != nil {
		return runtimetopology.Plan{}, err
	}
	return plan, nil
}
