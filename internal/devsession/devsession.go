package devsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

type Result struct {
	Schema      int             `json:"schema"`
	Workspace   string          `json:"workspace"`
	ControlFile string          `json:"controlFile"`
	Status      protocol.Status `json:"status"`
}

func Run(ctx context.Context, repositoryRoot, workspace string, stdout, stderr io.Writer, skipBuild bool, ready chan<- Result) error {
	repositoryRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return err
	}
	workspace, err = filepath.Abs(workspace)
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
	identity, _ := cell.New("dev", "default")
	paths, err := layout.Resolve(config.RootSet{
		BootstrapRoot: filepath.Join(workspace, "bootstrap"), StoreRoot: filepath.Join(workspace, "store"),
		CacheRoot: filepath.Join(workspace, "cache"), RuntimeRoot: filepath.Join(workspace, "runtime"), LogRoot: filepath.Join(workspace, "logs"),
	}, identity)
	if err != nil {
		return err
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: 1})
	if err != nil {
		return err
	}
	defer cellBroker.Close()
	descriptor, _ := json.Marshal(cellBroker.Descriptor())
	type process struct {
		command *exec.Cmd
		exited  chan error
	}
	children := make([]process, 0, 2)
	for _, app := range []string{"api", "web"} {
		token, err := cellBroker.MintSidecarToken(app, 24*time.Hour)
		if err != nil {
			return err
		}
		entry := filepath.Join(repositoryRoot, "apps", app, "dist", "sidecar", "index.js")
		command := exec.CommandContext(ctx, "node", entry)
		command.Dir, command.Stdout, command.Stderr = filepath.Join(repositoryRoot, "apps", app), stdout, stderr
		command.Env = append(os.Environ(),
			"OC_SIDECAR_CONTROL="+string(descriptor), "OC_SIDECAR_TOKEN="+token,
			"OC_SIDECAR_CHANNEL="+identity.Channel, "OC_SIDECAR_NAMESPACE="+identity.Namespace,
			"OC_SIDECAR_MODE=dev", "OC_SIDECAR_SOURCE=oc-control",
		)
		if err := command.Start(); err != nil {
			return err
		}
		child := process{command: command, exited: make(chan error, 1)}
		children = append(children, child)
		go func() { child.exited <- child.command.Wait() }()
	}
	defer func() {
		for _, child := range children {
			if child.command.Process != nil {
				_ = child.command.Process.Kill()
			}
		}
	}()
	for _, app := range []string{"api", "web"} {
		readyContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := cellBroker.WaitReady(readyContext, app)
		cancel()
		if err != nil {
			return err
		}
	}
	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if err != nil {
		return err
	}
	status, err := owner.Status(ctx)
	if err != nil {
		return err
	}
	ready <- Result{Schema: 1, Workspace: workspace, ControlFile: paths.ControlFile, Status: status}
	select {
	case <-ctx.Done():
		_, _ = owner.Control(context.Background(), "shutdown")
	case err := <-children[0].exited:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	case err := <-children[1].exited:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return nil
}
