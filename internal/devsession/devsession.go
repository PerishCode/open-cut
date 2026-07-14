package devsession

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
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
	Workspace   string          `json:"workspace"`
	ControlFile string          `json:"controlFile"`
	Apps        []string        `json:"apps"`
	Status      protocol.Status `json:"status"`
}

func Run(ctx context.Context, repositoryRoot, developmentRoot string, stdout, stderr io.Writer, skipBuild bool, ready chan<- Result) error {
	repositoryRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return err
	}
	developmentRoot, err = filepath.Abs(developmentRoot)
	if err != nil {
		return err
	}
	if !skipBuild {
		pnpm, err := tool.Resolve("pnpm")
		if err != nil {
			return err
		}
		if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
			Executable: pnpm, Args: []string{"-r", "--if-present", "run", "build"}, Directory: repositoryRoot,
			Stdout: stderr, Stderr: stderr, Profile: lifecycle.ProfileDevelopment,
		}); err != nil {
			return fmt.Errorf("build workspace: %w", err)
		}
	}
	controlConfig, err := workspace.Load(repositoryRoot)
	if err != nil {
		return err
	}
	topology, err := workspace.DiscoverTopology(repositoryRoot, controlConfig)
	if err != nil {
		return err
	}
	plan, err := developmentPlan(repositoryRoot, controlConfig, topology)
	if err != nil {
		return err
	}
	identity, err := cell.New("dev", "default")
	if err != nil {
		return err
	}
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(developmentRoot, "bootstrap"), StoreRoot: filepath.Join(developmentRoot, "store"),
		CacheRoot: filepath.Join(developmentRoot, "cache"), RuntimeRoot: filepath.Join(developmentRoot, "runtime"),
		LogRoot: filepath.Join(developmentRoot, "logs"),
	}, identity)
	if err != nil {
		return err
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1})
	if err != nil {
		return err
	}
	defer cellBroker.Close()
	runtimeToken, err := cellBroker.MintRuntimeToken("payload", 7*24*time.Hour)
	if err != nil {
		return err
	}
	runtimeReady := make(chan runtimehost.Result, 1)
	done := make(chan error, 1)
	go func() {
		done <- runtimehost.Run(ctx, runtimehost.Options{
			Descriptor: cellBroker.Descriptor(), Token: runtimeToken,
			Channel: identity.Channel, Namespace: identity.Namespace, App: "runtime",
			Mode: protocol.LifecycleModeDev, Presentation: protocol.PresentationInteractive, Source: "oc-control",
			Plan: plan, Stdout: stdout, Stderr: stderr,
		}, runtimeReady)
	}()
	select {
	case runtimeResult := <-runtimeReady:
		ready <- Result{
			Schema: 1, Workspace: developmentRoot, ControlFile: paths.ControlFile,
			Apps: runtimeResult.Apps, Status: runtimeResult.Status,
		}
		return <-done
	case err := <-done:
		return err
	}
}

func developmentPlan(repositoryRoot string, config workspace.Config, topology workspace.Topology) (runtimetopology.Plan, error) {
	node, err := tool.Resolve("node")
	if err != nil {
		return runtimetopology.Plan{}, fmt.Errorf("resolve Node runtime: %w", err)
	}
	electron, err := tool.ResolveElectron(repositoryRoot, config.PayloadWorkspace)
	if err != nil {
		return runtimetopology.Plan{}, err
	}
	plan := runtimetopology.Plan{Processes: make([]runtimetopology.ResolvedProcess, 0, len(topology.Sidecars))}
	for _, sidecar := range topology.Sidecars {
		appRoot := filepath.Join(repositoryRoot, "apps", sidecar.App)
		process := runtimetopology.ResolvedProcess{
			App: sidecar.App, Command: node,
			Args:             []string{filepath.Join(appRoot, "dist", "sidecar", "index.js")},
			WorkingDirectory: appRoot,
		}
		if sidecar.App == config.PayloadWorkspace {
			process.Command = electron
			process.Args = []string{"."}
			process.UnsetEnv = []string{"ELECTRON_RUN_AS_NODE"}
			process.Sandbox = lifecycle.SandboxChromium
		}
		plan.Processes = append(plan.Processes, process)
	}
	if err := plan.Validate(); err != nil {
		return runtimetopology.Plan{}, err
	}
	return plan, nil
}
