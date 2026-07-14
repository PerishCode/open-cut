package devsession

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/runtimehost"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/protocol"
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
		command := exec.CommandContext(ctx, "pnpm", "-r", "--if-present", "run", "build")
		command.Dir, command.Stdout, command.Stderr = repositoryRoot, stderr, stderr
		if err := command.Run(); err != nil {
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
			Channel: identity.Channel, Namespace: identity.Namespace, Mode: "dev", Source: "oc-control",
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
	node, err := exec.LookPath("node")
	if err != nil {
		return runtimetopology.Plan{}, fmt.Errorf("resolve Node runtime: %w", err)
	}
	electron, err := resolveElectronBinary(repositoryRoot, config.PayloadWorkspace)
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
		}
		plan.Processes = append(plan.Processes, process)
	}
	if err := plan.Validate(); err != nil {
		return runtimetopology.Plan{}, err
	}
	return plan, nil
}

func resolveElectronBinary(repositoryRoot, payloadWorkspace string) (string, error) {
	packageRoot := filepath.Join(repositoryRoot, "apps", payloadWorkspace, "node_modules", "electron")
	data, err := os.ReadFile(filepath.Join(packageRoot, "path.txt"))
	if err != nil {
		return "", fmt.Errorf("resolve Electron binary: %w", err)
	}
	relative := strings.TrimSpace(string(data))
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("electron/path.txt must contain a relative binary path")
	}
	binary := filepath.Join(packageRoot, "dist", relative)
	contained, err := filepath.Rel(packageRoot, binary)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Electron binary escapes its package")
	}
	info, err := os.Stat(binary)
	if err != nil {
		return "", fmt.Errorf("stat Electron binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Electron binary is not a regular file")
	}
	return binary, nil
}
