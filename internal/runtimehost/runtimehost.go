package runtimehost

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/environment"
)

const (
	initialRestartDelay  = 100 * time.Millisecond
	maximumRestartDelay  = 5 * time.Second
	stableProcessWindow  = 30 * time.Second
	sessionCapabilityTTL = 7 * 24 * time.Hour
	sessionRenewInterval = 12 * time.Hour
)

type Options struct {
	Descriptor   protocol.ControlDescriptor
	Token        string
	Channel      string
	Namespace    string
	DataDir      string
	App          string
	Mode         protocol.LifecycleMode
	Presentation protocol.Presentation
	Source       string
	Plan         runtimetopology.Plan
	ReadyTimeout time.Duration
	Stdout       io.Writer
	Stderr       io.Writer
}

type Result struct {
	Schema int             `json:"schema"`
	Apps   []string        `json:"apps"`
	Status protocol.Status `json:"status"`
}

type processExit struct {
	app        string
	generation uint64
	err        error
}

type restartRequest struct {
	app        string
	generation uint64
}

type managedProcess struct {
	definition runtimetopology.ResolvedProcess
	command    *lifecycle.Process
	startedAt  time.Time
	generation uint64
	backoff    time.Duration
	restart    *time.Timer
}

func Run(ctx context.Context, options Options, ready chan<- Result) (resultErr error) {
	if err := options.Plan.Validate(); err != nil {
		return err
	}
	if options.Descriptor.Protocol != protocol.Version || options.Token == "" || options.Channel == "" ||
		options.Namespace == "" || options.App == "" || !filepath.IsAbs(options.DataDir) || filepath.Clean(options.DataDir) != options.DataDir ||
		!options.Mode.Valid() || !options.Presentation.Valid() || options.Source == "" {
		return fmt.Errorf("runtime host requires a complete sidecar launch envelope")
	}
	if options.ReadyTimeout <= 0 {
		options.ReadyTimeout = 45 * time.Second
	}
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}

	session, err := client.DialSession(ctx, options.Descriptor, options.Token, client.Registration{
		Channel: options.Channel, Namespace: options.Namespace, App: options.App, Mode: options.Mode, Source: options.Source,
	})
	if err != nil {
		return fmt.Errorf("register aggregate runtime: %w", err)
	}
	exitCode := 0
	defer func() {
		if resultErr != nil {
			exitCode = 1
		}
		_ = session.Close(exitCode)
	}()

	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go heartbeat(session, heartbeatDone)
	commands := make(chan protocol.ControlCommand, 1)
	go func() {
		for {
			command, readErr := session.ReadCommand(context.Background())
			if readErr != nil {
				return
			}
			select {
			case commands <- command:
			default:
			}
		}
	}()

	control := client.New(options.Descriptor, options.Token)
	exits := make(chan processExit, len(options.Plan.Processes)*2)
	restarts := make(chan restartRequest, len(options.Plan.Processes)*2)
	processes := make(map[string]*managedProcess, len(options.Plan.Processes))
	for _, definition := range options.Plan.Processes {
		processes[definition.App] = &managedProcess{definition: definition, backoff: initialRestartDelay}
	}
	for _, app := range runtimetopology.Apps(options.Plan) {
		managed := processes[app]
		if err := startProcess(ctx, control, options, managed, exits); err != nil {
			fmt.Fprintf(options.Stderr, "runtime app %s start failed; retrying: %v\n", app, err)
			scheduleRestart(managed, restarts)
		}
	}

	apps := runtimetopology.Apps(options.Plan)
	readyDeadline := time.NewTimer(options.ReadyTimeout)
	defer readyDeadline.Stop()
	statusTicker := time.NewTicker(50 * time.Millisecond)
	defer statusTicker.Stop()
	renewTicker := time.NewTicker(sessionRenewInterval)
	defer renewTicker.Stop()
	confirmed := false

	for {
		select {
		case <-ctx.Done():
			shutdown(control, processes, exits, false)
			return nil
		case command := <-commands:
			if command == protocol.ControlCommandShow {
				continue
			}
			shutdown(control, processes, exits, true)
			return nil
		case exited := <-exits:
			managed := processes[exited.app]
			if managed == nil || managed.generation != exited.generation {
				continue
			}
			managed.command = nil
			if time.Since(managed.startedAt) >= stableProcessWindow {
				managed.backoff = initialRestartDelay
			}
			if exited.err != nil {
				fmt.Fprintf(options.Stderr, "runtime app %s exited; recovering: %v\n", exited.app, exited.err)
			} else {
				fmt.Fprintf(options.Stderr, "runtime app %s exited; recovering\n", exited.app)
			}
			scheduleRestart(managed, restarts)
		case restart := <-restarts:
			managed := processes[restart.app]
			if managed == nil || managed.generation != restart.generation || managed.command != nil {
				continue
			}
			managed.restart = nil
			if err := startProcess(ctx, control, options, managed, exits); err != nil {
				fmt.Fprintf(options.Stderr, "runtime app %s restart failed; retrying: %v\n", restart.app, err)
				scheduleRestart(managed, restarts)
			}
		case <-statusTicker.C:
			if confirmed {
				continue
			}
			status, statusErr := control.Status(ctx)
			if statusErr != nil || !allReady(status, apps) {
				continue
			}
			if err := session.Ready(); err != nil {
				shutdown(control, processes, exits, false)
				return fmt.Errorf("mark aggregate runtime READY: %w", err)
			}
			status, err = control.Status(ctx)
			if err != nil {
				shutdown(control, processes, exits, false)
				return fmt.Errorf("read ready runtime status: %w", err)
			}
			confirmed = true
			if !readyDeadline.Stop() {
				select {
				case <-readyDeadline.C:
				default:
				}
			}
			if ready != nil {
				ready <- Result{Schema: 1, Apps: apps, Status: status}
			}
		case <-renewTicker.C:
			renewContext, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, renewErr := session.Renew(renewContext, sessionCapabilityTTL)
			cancel()
			if renewErr != nil {
				fmt.Fprintf(options.Stderr, "runtime capability renewal failed; retrying later: %v\n", renewErr)
				continue
			}
		case <-readyDeadline.C:
			if confirmed {
				continue
			}
			shutdown(control, processes, exits, false)
			return fmt.Errorf("runtime apps %v did not reach READY within %s", apps, options.ReadyTimeout)
		}
	}
}

func startProcess(
	ctx context.Context,
	control *client.Client,
	options Options,
	managed *managedProcess,
	exits chan<- processExit,
) error {
	definition := managed.definition
	profile, err := lifecycleProfile(options.Mode)
	if err != nil {
		return err
	}
	delegated, err := control.DelegateSidecar(ctx, definition.App, sessionCapabilityTTL, definition.Capabilities)
	if err != nil {
		return fmt.Errorf("delegate capability: %w", err)
	}
	launchEnvironment, err := protocol.LaunchEnvironmentMap(protocol.SidecarLaunch{
		App: definition.App, Control: options.Descriptor, Token: delegated.Token, Channel: options.Channel,
		Namespace: options.Namespace, DataDir: options.DataDir, Mode: options.Mode,
		Presentation: options.Presentation, Source: options.Source,
	})
	if err != nil {
		return err
	}
	command, err := lifecycle.Start(ctx, lifecycle.ProcessSpec{
		Executable: definition.Command,
		Args:       definition.Args,
		Directory:  definition.WorkingDirectory,
		Stdout:     options.Stdout,
		Stderr:     options.Stderr,
		Profile:    profile,
		Sandbox:    definition.Sandbox,
		Env:        environment.Merge(os.Environ(), definition.UnsetEnv, definition.Env, launchEnvironment),
	})
	if err != nil {
		return err
	}
	managed.generation++
	managed.command = command
	managed.startedAt = time.Now()
	generation := managed.generation
	go func(app string, command *lifecycle.Process) {
		exits <- processExit{app: app, generation: generation, err: command.Wait()}
	}(definition.App, command)
	return nil
}

func lifecycleProfile(mode protocol.LifecycleMode) (lifecycle.Profile, error) {
	switch mode {
	case protocol.LifecycleModeProduction:
		return lifecycle.ProfileProduction, nil
	case protocol.LifecycleModePackaged:
		return lifecycle.ProfilePackaged, nil
	case protocol.LifecycleModeDev:
		return lifecycle.ProfileDevelopment, nil
	case protocol.LifecycleModeHarness:
		return lifecycle.ProfileHarness, nil
	default:
		return "", fmt.Errorf("unsupported runtime lifecycle mode %q", mode)
	}
}

func scheduleRestart(managed *managedProcess, restarts chan<- restartRequest) {
	if managed.restart != nil {
		return
	}
	delay := managed.backoff
	if delay <= 0 {
		delay = initialRestartDelay
	}
	managed.backoff = min(delay*2, maximumRestartDelay)
	generation := managed.generation
	managed.restart = time.AfterFunc(delay, func() {
		restarts <- restartRequest{app: managed.definition.App, generation: generation}
	})
}

func allReady(status protocol.Status, apps []string) bool {
	ready := make(map[string]bool, len(status.Sessions))
	for _, session := range status.Sessions {
		ready[session.App] = session.Ready
	}
	for _, app := range apps {
		if !ready[app] {
			return false
		}
	}
	return true
}

func shutdown(
	control *client.Client,
	processes map[string]*managedProcess,
	exits <-chan processExit,
	alreadyBroadcast bool,
) {
	running := make(map[string]uint64, len(processes))
	for app, managed := range processes {
		if managed.restart != nil {
			managed.restart.Stop()
			managed.restart = nil
		}
		if managed.command != nil {
			running[app] = managed.generation
		}
	}
	if !alreadyBroadcast && len(running) > 0 {
		requestContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = control.Control(requestContext, protocol.ControlCommandShutdown)
		cancel()
	}
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	for len(running) > 0 {
		select {
		case exited := <-exits:
			if running[exited.app] == exited.generation {
				delete(running, exited.app)
			}
		case <-deadline.C:
			for app, generation := range running {
				managed := processes[app]
				if managed != nil && managed.generation == generation && managed.command != nil {
					_ = managed.command.Kill()
				}
			}
			return
		}
	}
}

func heartbeat(session *client.Session, done <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_ = session.Heartbeat()
		}
	}
}
