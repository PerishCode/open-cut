package launcher

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

	"github.com/PerishCode/open-cut/internal/atomicfile"
	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/internal/update"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

var ErrRecoveryRequired = errors.New("no installed release is available; recovery download is required")

type B0Options struct {
	BootstrapPath  string
	ConnectTimeout time.Duration
	ReadyTimeout   time.Duration
	StabilityHold  time.Duration
	Stdout         io.Writer
	Stderr         io.Writer
}

type cellSnapshot struct {
	Schema    int    `json:"schema"`
	Channel   string `json:"channel"`
	Namespace string `json:"namespace"`
}

func RunB0(ctx context.Context, options B0Options) error {
	if options.ConnectTimeout <= 0 {
		options.ConnectTimeout = 10 * time.Second
	}
	if options.ReadyTimeout <= 0 {
		options.ReadyTimeout = 2 * time.Minute
	}
	if options.StabilityHold <= 0 {
		options.StabilityHold = 500 * time.Millisecond
	}
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}

	bootstrap, err := config.LoadBootstrap(options.BootstrapPath)
	if err != nil {
		return err
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	if err := ensureCellSnapshot(paths.CellFile, identity); err != nil {
		return err
	}
	installer := update.Installer{}
	if err := installer.Recover(bootstrap, paths); err != nil {
		return fmt.Errorf("recover interrupted update: %w", err)
	}
	runtimeState, err := state.Load(paths.StateFile, identity.Channel)
	if err != nil {
		return err
	}
	transition := func(transitionContext context.Context, _ protocol.UpdateTransitionRequest) (protocol.UpdateTransitionResponse, error) {
		version, installErr := installer.InstallLatest(transitionContext, bootstrap, paths)
		if installErr != nil {
			return protocol.UpdateTransitionResponse{}, installErr
		}
		updated, loadErr := state.Load(paths.StateFile, identity.Channel)
		if loadErr != nil {
			return protocol.UpdateTransitionResponse{}, loadErr
		}
		if updated.Candidate == version {
			return protocol.UpdateTransitionResponse{Status: "prepared", Version: version, RestartRequired: true}, nil
		}
		return protocol.UpdateTransitionResponse{Status: "current", Version: version}, nil
	}
	cellBroker, err := broker.Start(broker.Options{
		Identity: identity, Paths: paths, Generation: runtimeState.Generation, UpdateTransition: transition,
	})
	if errors.Is(err, broker.ErrAlreadyRunning) {
		existing, loadErr := client.Load(paths.ControlFile, paths.OwnerTokenFile)
		if loadErr != nil {
			return fmt.Errorf("cell lock held but broker rendezvous failed: %w", loadErr)
		}
		_, controlErr := existing.Control(ctx, "show")
		return controlErr
	}
	if err != nil {
		return err
	}
	defer cellBroker.Close()
	for {
		runtimeState, err = state.Load(paths.StateFile, identity.Channel)
		if err != nil {
			return err
		}
		selected := runtimeState.Candidate
		isCandidate := selected != ""
		if selected == "" {
			selected = runtimeState.Active
		}
		if selected == "" {
			if _, err := installer.InstallLatest(ctx, bootstrap, paths); err != nil {
				return errors.Join(ErrRecoveryRequired, err)
			}
			continue
		}
		if err := runManagedRelease(ctx, options, bootstrap, identity, paths, cellBroker, runtimeState, selected, isCandidate); err != nil {
			return err
		}
		next, err := state.Load(paths.StateFile, identity.Channel)
		if err != nil {
			return err
		}
		if next.Candidate == "" || next.Candidate == selected {
			return nil
		}
		absentContext, cancel := context.WithTimeout(ctx, options.ConnectTimeout)
		err = cellBroker.WaitAbsent(absentContext, "payload")
		cancel()
		if err != nil {
			return err
		}
		fmt.Fprintf(options.Stdout, "handoff %s -> %s\n", selected, next.Candidate)
	}
}

func runManagedRelease(
	ctx context.Context,
	options B0Options,
	bootstrap config.Bootstrap,
	identity cell.Identity,
	paths layout.CellPaths,
	cellBroker *broker.Broker,
	runtimeState state.Runtime,
	selected string,
	isCandidate bool,
) error {
	versionRoot := filepath.Join(paths.Versions, selected)
	manifestPath := filepath.Join(versionRoot, "manifest.json")
	manifest, err := release.LoadManifest(manifestPath)
	if err != nil {
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, err)
	}
	if manifest.Version != selected {
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, fmt.Errorf("manifest version does not match selected version"))
	}
	if err := manifest.ValidateHost(identity.Channel, bootstrap.ProtocolFloor); err != nil {
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, err)
	}
	launcherEntry, err := release.ResolveEntry(versionRoot, manifest.Launcher.Entry, "launcher")
	if err != nil {
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, err)
	}
	runtimeToken, err := cellBroker.MintRuntimeToken("payload", options.ReadyTimeout+options.StabilityHold+time.Minute)
	if err != nil {
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, err)
	}
	descriptorJSON, _ := json.Marshal(cellBroker.Descriptor())
	command := exec.CommandContext(ctx, launcherEntry, "--role", "l1", "--manifest", manifestPath)
	command.Stdout, command.Stderr = options.Stdout, options.Stderr
	command.Env = append(os.Environ(),
		"OC_SIDECAR_CONTROL="+string(descriptorJSON),
		"OC_SIDECAR_TOKEN="+runtimeToken,
		"OC_SIDECAR_CHANNEL="+identity.Channel,
		"OC_SIDECAR_NAMESPACE="+identity.Namespace,
		"OC_SIDECAR_MODE=packaged",
		"OC_SIDECAR_SOURCE=launcher",
	)
	if err := command.Start(); err != nil {
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, fmt.Errorf("start versioned launcher: %w", err))
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()

	connectContext, cancelConnect := context.WithTimeout(ctx, options.ConnectTimeout)
	defer cancelConnect()
	registered := make(chan error, 1)
	go func() { registered <- cellBroker.WaitRegistered(connectContext, "payload") }()
	select {
	case processErr := <-exited:
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, processExitError("before registration", processErr))
	case registerErr := <-registered:
		if registerErr != nil {
			_ = command.Process.Kill()
			<-exited
			return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, registerErr)
		}
	}

	readyContext, cancelReady := context.WithTimeout(ctx, options.ReadyTimeout)
	defer cancelReady()
	ready := make(chan error, 1)
	go func() { ready <- cellBroker.WaitReady(readyContext, "payload") }()
	select {
	case processErr := <-exited:
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, processExitError("before READY", processErr))
	case readyErr := <-ready:
		if readyErr != nil {
			_ = command.Process.Kill()
			<-exited
			return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, readyErr)
		}
	}

	hold := time.NewTimer(options.StabilityHold)
	defer hold.Stop()
	select {
	case processErr := <-exited:
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, processExitError("during stability hold", processErr))
	case <-hold.C:
	case <-ctx.Done():
		_ = command.Process.Kill()
		<-exited
		return failCandidate(paths.StateFile, identity.Channel, runtimeState, isCandidate, ctx.Err())
	}

	if isCandidate {
		confirmed, err := state.Confirm(runtimeState, identity.Channel, selected)
		if err != nil {
			_ = command.Process.Kill()
			<-exited
			return err
		}
		if err := state.Save(paths.StateFile, identity.Channel, confirmed); err != nil {
			_ = command.Process.Kill()
			<-exited
			return err
		}
		fmt.Fprintf(options.Stdout, "confirmed %s\n", selected)
	}
	return <-exited
}

type L1Options struct {
	ManifestPath string
	Stdout       io.Writer
	Stderr       io.Writer
}

func RunL1(ctx context.Context, options L1Options) error {
	manifest, err := release.LoadManifest(options.ManifestPath)
	if err != nil {
		return err
	}
	versionRoot := filepath.Dir(options.ManifestPath)
	payloadEntry, err := release.ResolveEntry(versionRoot, manifest.Payload.Entry, "payload")
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, payloadEntry)
	command.Env = append(os.Environ(), "OC_RELEASE_VERSION="+manifest.Version)
	command.Stdout, command.Stderr = options.Stdout, options.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("payload exited: %w", err)
	}
	return nil
}

func ensureCellSnapshot(path string, identity cell.Identity) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return atomicfile.WriteJSON(path, cellSnapshot{Schema: 1, Channel: identity.Channel, Namespace: identity.Namespace}, 0o600)
	}
	if err != nil {
		return err
	}
	var snapshot cellSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode cell snapshot: %w", err)
	}
	if snapshot.Schema != 1 || snapshot.Channel != identity.Channel || snapshot.Namespace != identity.Namespace {
		return fmt.Errorf("cell snapshot does not match injected identity")
	}
	return nil
}

func failCandidate(statePath, channel string, runtimeState state.Runtime, isCandidate bool, cause error) error {
	if !isCandidate {
		return cause
	}
	rolledBack, err := state.Rollback(runtimeState, channel)
	if err != nil {
		return errors.Join(cause, err)
	}
	if err := state.Save(statePath, channel, rolledBack); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func processExitError(stage string, err error) error {
	if err == nil {
		return fmt.Errorf("versioned launcher exited cleanly %s", stage)
	}
	return fmt.Errorf("versioned launcher exited %s: %w", stage, err)
}
