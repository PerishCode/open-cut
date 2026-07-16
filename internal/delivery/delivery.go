package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/install"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/publisher"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/internal/verifier"
	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

type InstallOptions struct {
	RepositoryRoot string
	Workspace      string
	OriginRoot     string
	OriginURL      string
	KeyPath        string
	Channel        string
	Namespace      string
	Target         target.Target
	Headless       bool
	Stdout         io.Writer
	Stderr         io.Writer
}

type InstallResult struct {
	Schema      int             `json:"schema"`
	Receipt     string          `json:"receipt"`
	InstallRoot string          `json:"installRoot"`
	Bootstrap   string          `json:"bootstrap"`
	CLI         string          `json:"cli"`
	HostPID     int             `json:"hostPid"`
	Status      protocol.Status `json:"status"`
}

type RunOptions struct {
	Receipt   string
	Workspace string
	Headless  bool
	Stdout    io.Writer
	Stderr    io.Writer
}

type UninstallResult struct {
	Schema       int      `json:"schema"`
	Receipt      string   `json:"receipt"`
	Purged       bool     `json:"purged"`
	Removed      []string `json:"removed"`
	Preserved    []string `json:"preserved,omitempty"`
	BrokerClosed bool     `json:"brokerClosed"`
}

func Install(ctx context.Context, options InstallOptions) (InstallResult, error) {
	for name, value := range map[string]string{
		"repository": options.RepositoryRoot, "workspace": options.Workspace, "origin": options.OriginRoot,
	} {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return InstallResult{}, err
		}
		switch name {
		case "repository":
			options.RepositoryRoot = absolute
		case "workspace":
			options.Workspace = absolute
		case "origin":
			options.OriginRoot = absolute
		}
	}
	controlConfig, err := workspace.Load(options.RepositoryRoot)
	if err != nil {
		return InstallResult{}, err
	}
	verified, err := verifier.VerifyOrigin(options.OriginRoot, options.Channel, options.Target, options.KeyPath, time.Now())
	if err != nil {
		return InstallResult{}, fmt.Errorf("verify delivery origin: %w", err)
	}
	key, _, err := publisher.LoadKey(options.KeyPath)
	if err != nil {
		return InstallResult{}, err
	}
	installLayout, err := lifecycle.PrepareNativeInstall(options.Target, options.Workspace, lifecycle.InstallProduct{
		Name: "Open Cut", ExecutableName: "Open Cut", BundleID: "local.open-cut.launcher",
	})
	if err != nil {
		return InstallResult{}, err
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return InstallResult{}, err
	}
	for _, build := range []struct{ output, source string }{
		{installLayout.HostPath, "./cmd/platform-host"},
		{installLayout.LauncherPath, "./cmd/launcher"},
		{installLayout.CLIPath, "./cmd/cli"},
	} {
		if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
			Executable: goTool, Args: []string{"build", "-trimpath", "-o", build.output, build.source},
			Directory: options.RepositoryRoot, Stdout: options.Stderr, Stderr: options.Stderr,
			Env:     options.Target.GoBuildEnvironment(os.Environ()),
			Profile: lifecycle.ProfileProduction,
		}); err != nil {
			return InstallResult{}, fmt.Errorf("build %s: %w", build.source, err)
		}
	}
	userRoot := filepath.Join(options.Workspace, "user")
	roots := config.RootSet{
		BootstrapRoot: filepath.Join(userRoot, "bootstrap"), StoreRoot: filepath.Join(userRoot, "store"),
		CacheRoot: filepath.Join(userRoot, "cache"), RuntimeRoot: filepath.Join(userRoot, "runtime"), LogRoot: filepath.Join(userRoot, "logs"),
	}
	identity, _ := cell.New(options.Channel, options.Namespace)
	dataDir, err := lifecycle.ResolveProductDataDir(filepath.Join(userRoot, "data"), "open-cut", options.Channel, options.Namespace)
	if err != nil {
		return InstallResult{}, err
	}
	installationRoot := filepath.Join(userRoot, "identity")
	installation, err := lifecycle.EnsureDevelopmentInstallationIdentity(installationRoot, controlConfig.InstallationKeyRoles)
	if err != nil {
		return InstallResult{}, fmt.Errorf("provision harness installation identity: %w", err)
	}
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: options.Channel, Namespace: options.Namespace, DataDir: dataDir, Roots: roots,
		Installation:  installation.Assertion(),
		UpdateOrigins: []string{options.OriginURL}, ProtocolFloor: "bootstrap.v1",
		InitialTrustRoot: config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{ID: key.KeyID, PublicKey: key.PublicKey}}},
	}
	bootstrapPath := filepath.Join(roots.BootstrapRoot, "bootstrap.json")
	if err := atomicfile.WriteJSON(bootstrapPath, bootstrap, 0o600); err != nil {
		return InstallResult{}, err
	}
	paths, err := layout.Resolve(roots, identity)
	if err != nil {
		return InstallResult{}, err
	}
	receiptPath := filepath.Join(options.Workspace, "receipts", "install-receipt.json")
	receipt := install.Receipt{
		Schema: install.ReceiptSchema, Target: options.Target, InstallRoot: installLayout.InstallRoot,
		HostPath: installLayout.HostPath, LauncherPath: installLayout.LauncherPath,
		CLIPath: installLayout.CLIPath, BootstrapPath: bootstrapPath,
		ManagedRoots: []string{roots.BootstrapRoot, paths.Store, paths.Cache, paths.Runtime, paths.Log, dataDir, installationRoot},
		Channel:      options.Channel, Namespace: options.Namespace, IdentityBackend: install.IdentityBackendDevelopmentFile,
	}
	for _, destination := range []string{installLayout.InternalReceiptPath, receiptPath} {
		if err := install.SaveReceipt(destination, receipt); err != nil {
			return InstallResult{}, err
		}
	}
	if err := lifecycle.SignNativeInstall(ctx, options.Target, installLayout, options.Stderr, options.Stderr); err != nil {
		return InstallResult{}, fmt.Errorf("sign launcher app: %w", err)
	}
	result, err := Run(ctx, RunOptions{
		Receipt: receiptPath, Workspace: options.Workspace, Headless: options.Headless,
		Stdout: options.Stdout, Stderr: options.Stderr,
	})
	if err != nil {
		return InstallResult{}, fmt.Errorf("installed runtime %s did not become ready: %w", verified.Version, err)
	}
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: installLayout.CLIPath,
		Args:       []string{"status", "--receipt", receiptPath},
		Directory:  installLayout.InstallRoot,
		Env:        os.Environ(),
		Stdout:     io.Discard,
		Stderr:     options.Stderr,
		Profile:    lifecycle.ProfileProduction,
	}); err != nil {
		return InstallResult{}, fmt.Errorf("installed product CLI could not inspect active cell: %w", err)
	}
	return result, nil
}

func Run(ctx context.Context, options RunOptions) (InstallResult, error) {
	receipt, err := install.LoadReceipt(options.Receipt)
	if err != nil {
		return InstallResult{}, err
	}
	workspace, err := filepath.Abs(options.Workspace)
	if err != nil {
		return InstallResult{}, err
	}
	if !within(workspace, receipt.InstallRoot) || !within(workspace, receipt.BootstrapPath) {
		return InstallResult{}, fmt.Errorf("receipt is outside harness workspace")
	}
	bootstrap, err := config.LoadBootstrap(receipt.BootstrapPath)
	if err != nil {
		return InstallResult{}, err
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		return InstallResult{}, err
	}
	if owner, loadErr := client.Load(paths.ControlFile, paths.OwnerTokenFile); loadErr == nil {
		if status, statusErr := owner.Status(ctx); statusErr == nil {
			if runtimeState, stateErr := state.Load(paths.StateFile, status.Channel); stateErr == nil && runtimeState.Active != "" {
				return InstallResult{
					Schema: 1, Receipt: options.Receipt, InstallRoot: receipt.InstallRoot,
					Bootstrap: receipt.BootstrapPath, CLI: receipt.CLIPath, Status: status,
				}, nil
			}
			status, waitErr := waitInstalledReady(ctx, paths, 2*time.Minute)
			if waitErr != nil {
				return InstallResult{}, waitErr
			}
			return InstallResult{
				Schema: 1, Receipt: options.Receipt, InstallRoot: receipt.InstallRoot,
				Bootstrap: receipt.BootstrapPath, CLI: receipt.CLIPath, Status: status,
			}, nil
		}
	}
	logs := filepath.Join(workspace, "delivery-logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		return InstallResult{}, err
	}
	logFile, err := os.OpenFile(filepath.Join(logs, "installed-runtime.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return InstallResult{}, err
	}
	presentation := lifecycle.PresentationInteractive
	if options.Headless {
		presentation = lifecycle.PresentationHeadless
	}
	process, err := lifecycle.Start(ctx, lifecycle.ProcessSpec{
		Executable: receipt.HostPath, Directory: receipt.InstallRoot, Env: os.Environ(), Presentation: presentation,
		Stdout: logFile, Stderr: logFile, Profile: lifecycle.ProfileProduction, Detached: true,
	})
	if err != nil {
		_ = logFile.Close()
		return InstallResult{}, err
	}
	_ = logFile.Close()
	status, err := waitInstalledReady(ctx, paths, 2*time.Minute)
	if err != nil {
		_ = process.Kill()
		return InstallResult{}, err
	}
	return InstallResult{
		Schema: 1, Receipt: options.Receipt, InstallRoot: receipt.InstallRoot,
		Bootstrap: receipt.BootstrapPath, CLI: receipt.CLIPath, HostPID: process.PID(), Status: status,
	}, nil
}

func Inspect(receiptPath string) (InstallResult, error) {
	receipt, err := install.LoadReceipt(receiptPath)
	if err != nil {
		return InstallResult{}, err
	}
	bootstrap, err := config.LoadBootstrap(receipt.BootstrapPath)
	if err != nil {
		return InstallResult{}, err
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		return InstallResult{}, err
	}
	owner, err := client.Load(paths.ControlFile, paths.ObserverTokenFile)
	if err != nil {
		return InstallResult{}, err
	}
	status, err := owner.Status(context.Background())
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Schema: 1, Receipt: receiptPath, InstallRoot: receipt.InstallRoot,
		Bootstrap: receipt.BootstrapPath, CLI: receipt.CLIPath, HostPID: receipt.HostPID, Status: status,
	}, nil
}

func Uninstall(ctx context.Context, receiptPath, workspace string, purge bool) (UninstallResult, error) {
	receiptPath, err := filepath.Abs(receiptPath)
	if err != nil {
		return UninstallResult{}, err
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return UninstallResult{}, err
	}
	receipt, err := install.LoadReceipt(receiptPath)
	if err != nil {
		return UninstallResult{}, err
	}
	for _, owned := range append([]string{receipt.InstallRoot}, receipt.ManagedRoots...) {
		if !within(workspace, owned) {
			return UninstallResult{}, fmt.Errorf("receipt path is outside harness workspace: %s", owned)
		}
	}
	brokerClosed := true
	brokerPID := 0
	bootstrap, bootstrapErr := config.LoadBootstrap(receipt.BootstrapPath)
	if bootstrapErr == nil {
		identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
		paths, resolveErr := layout.Resolve(bootstrap.Roots, identity)
		if resolveErr != nil {
			return UninstallResult{}, resolveErr
		}
		if descriptorBytes, readErr := os.ReadFile(paths.ControlFile); readErr == nil {
			brokerClosed = false
			var descriptor protocol.ControlDescriptor
			if jsonErr := json.Unmarshal(descriptorBytes, &descriptor); jsonErr == nil {
				brokerPID = descriptor.PID
			}
		}
		if owner, loadErr := client.Load(paths.ControlFile, paths.OwnerTokenFile); loadErr == nil {
			_, _ = owner.Control(ctx, protocol.ControlCommandShutdown)
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if _, statErr := os.Stat(paths.ControlFile); errors.Is(statErr, os.ErrNotExist) {
				brokerClosed = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !brokerClosed && brokerPID > 0 {
			if process, findErr := os.FindProcess(brokerPID); findErr == nil {
				_ = process.Kill()
			}
			killDeadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(killDeadline) {
				if _, statErr := os.Stat(paths.ControlFile); errors.Is(statErr, os.ErrNotExist) {
					brokerClosed = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	} else if _, statErr := os.Stat(receipt.BootstrapPath); !errors.Is(statErr, os.ErrNotExist) {
		return UninstallResult{}, bootstrapErr
	}
	if _, err := os.Lstat(receipt.InstallRoot); err == nil {
		if err := os.RemoveAll(receipt.InstallRoot); err != nil {
			return UninstallResult{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return UninstallResult{}, err
	}
	removed := []string{receipt.InstallRoot}
	preserved := append([]string(nil), receipt.ManagedRoots...)
	if purge {
		for _, root := range receipt.ManagedRoots {
			if err := os.RemoveAll(root); err != nil {
				return UninstallResult{}, err
			}
			removed = append(removed, root)
		}
		preserved = nil
	}
	return UninstallResult{
		Schema: 1, Receipt: receiptPath, Purged: purge, Removed: removed, Preserved: preserved, BrokerClosed: brokerClosed,
	}, nil
}

func waitInstalledReady(ctx context.Context, paths layout.CellPaths, timeout time.Duration) (protocol.Status, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
		if err == nil {
			status, statusErr := owner.Status(ctx)
			if statusErr == nil {
				for _, session := range status.Sessions {
					if session.Subject == "payload" && session.Ready {
						runtimeState, stateErr := state.Load(paths.StateFile, status.Channel)
						if stateErr == nil && runtimeState.Active != "" {
							return status, nil
						}
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return protocol.Status{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return protocol.Status{}, fmt.Errorf("timed out waiting for payload READY")
}

func within(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
