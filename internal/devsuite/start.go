package devsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/devsession"
	"github.com/PerishCode/open-cut/internal/procident"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/environment"
)

const (
	controlReadyPatience = 20 * time.Second
	suiteReadyPatience   = 120 * time.Second
	capabilityTTL        = 7 * 24 * time.Hour
)

type StartResult struct {
	Schema      int             `json:"schema"`
	BaseDir     string          `json:"baseDir"`
	ControlFile string          `json:"controlFile"`
	Generation  uint64          `json:"generation"`
	Stamp       string          `json:"stamp"`
	Apps        []string        `json:"apps"`
	Status      protocol.Status `json:"status"`
}

// Start builds the workspace, spawns the detached control member and app
// members with the generation's stamp, records the roster, and confirms READY
// once. On READY timeout the suite is left running for diagnosis and an error
// is returned.
func Start(ctx context.Context, repositoryRoot, baseDir string, stdout, stderr io.Writer) (StartResult, error) {
	paths, err := devsession.ResolveCellPaths(baseDir)
	if err != nil {
		return StartResult{}, err
	}
	if err := paths.Ensure(); err != nil {
		return StartResult{}, err
	}
	if running, err := client.Load(paths.ControlFile, paths.ObserverTokenFile); err == nil {
		if _, statusErr := running.Status(ctx); statusErr == nil {
			return StartResult{}, fmt.Errorf("development suite is already running; use `oc-control dev status` or `dev stop`")
		}
	}
	if err := devsession.BuildWorkspace(ctx, repositoryRoot, stderr); err != nil {
		return StartResult{}, fmt.Errorf("build workspace: %w", err)
	}
	controlConfig, err := workspace.Load(repositoryRoot)
	if err != nil {
		return StartResult{}, err
	}
	topology, err := workspace.DiscoverTopology(repositoryRoot, controlConfig)
	if err != nil {
		return StartResult{}, err
	}
	plan, err := devsession.ResolvePlan(repositoryRoot, controlConfig, topology)
	if err != nil {
		return StartResult{}, err
	}
	commandRoot := filepath.Dir(filepath.Dir(baseDir))
	installation, err := lifecycle.EnsureDevelopmentInstallationIdentity(
		filepath.Join(commandRoot, "identity"), controlConfig.InstallationKeyRoles,
	)
	if err != nil {
		return StartResult{}, fmt.Errorf("load development installation identity: %w", err)
	}

	generation := nextGeneration(paths.Runtime)
	stamp, err := NewStamp("dev", "default", generation)
	if err != nil {
		return StartResult{}, err
	}
	if err := RecordGeneration(paths.Runtime, generation); err != nil {
		return StartResult{}, err
	}
	PruneMemberLogs(paths.Log, generation)

	roster := Roster{Stamp: stamp, BaseDir: baseDir, RepositoryRoot: repositoryRoot}
	control, err := spawnControlMember(ctx, repositoryRoot, baseDir, paths.Log, stamp)
	if err != nil {
		return StartResult{}, err
	}
	roster.Members = append(roster.Members, control)
	if err := WriteRoster(RosterPath(paths.Runtime), roster); err != nil {
		return StartResult{}, err
	}
	descriptor, err := awaitControlMember(paths.ControlFile, control)
	if err != nil {
		return StartResult{}, err
	}
	conductor, err := client.Load(paths.ControlFile, ConductorTokenPath(paths.Runtime))
	if err != nil {
		return StartResult{}, fmt.Errorf("control member rendezvous: %w", err)
	}

	cdpPort, err := devsession.ReserveLoopbackPort()
	if err != nil {
		return StartResult{}, fmt.Errorf("reserve development CDP port: %w", err)
	}
	roster.CDPPort = cdpPort
	suiteEnvironment := map[string]string{
		lifecycle.SignerSocketEnvironment: SignerSocketPath(paths.Runtime),
		devsession.CDPPortEnvironment:     strconv.Itoa(cdpPort),
	}
	for _, definition := range plan.Processes {
		member, err := spawnAppMember(ctx, conductor, definition, spawnContext{
			baseDir: baseDir, logDir: paths.Log, stamp: stamp, descriptor: descriptor,
			installation: installation.Assertion(), environment: suiteEnvironment,
		})
		if err != nil {
			return StartResult{}, fmt.Errorf("spawn %s: %w (suite left as-is; `dev stop` to clean up)", definition.App, err)
		}
		roster.Members = append(roster.Members, member)
		if err := WriteRoster(RosterPath(paths.Runtime), roster); err != nil {
			return StartResult{}, err
		}
	}

	apps := runtimetopology.Apps(plan)
	status, err := awaitSuiteReady(ctx, paths.ControlFile, paths.ObserverTokenFile, apps)
	if err != nil {
		return StartResult{}, fmt.Errorf(
			"%w; the suite is left running — inspect with `oc-control dev status` and `oc-control dev logs`", err,
		)
	}
	return StartResult{
		Schema: 1, BaseDir: baseDir, ControlFile: paths.ControlFile,
		Generation: generation, Stamp: stamp.String(), Apps: apps, Status: status,
	}, nil
}

func nextGeneration(runtimeDir string) uint64 {
	last := LastGeneration(runtimeDir)
	if roster, err := LoadRoster(RosterPath(runtimeDir)); err == nil && roster.Stamp.Generation > last {
		last = roster.Stamp.Generation
	}
	return last + 1
}

type spawnContext struct {
	baseDir      string
	logDir       string
	stamp        Stamp
	descriptor   protocol.ControlDescriptor
	installation protocol.InstallationAssertion
	environment  map[string]string
}

func openMemberLog(logDir, app string, generation uint64) (*os.File, string, error) {
	path := MemberLogPath(logDir, app, generation)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", err
	}
	return file, path, nil
}

func spawnControlMember(ctx context.Context, repositoryRoot, baseDir, logDir string, stamp Stamp) (Member, error) {
	executable, err := os.Executable()
	if err != nil {
		return Member{}, err
	}
	logFile, logPath, err := openMemberLog(logDir, ControlApp, stamp.Generation)
	if err != nil {
		return Member{}, err
	}
	defer logFile.Close()
	args := []string{
		"dev", "control-member",
		"--repo", repositoryRoot,
		"--base-dir", baseDir,
		"--generation", strconv.FormatUint(stamp.Generation, 10),
		stamp.Argument(),
	}
	process, err := lifecycle.Start(ctx, lifecycle.ProcessSpec{
		Executable: executable, Args: args, Directory: repositoryRoot,
		Stdout: logFile, Stderr: logFile,
		Profile: lifecycle.ProfileDevelopment, Detached: true,
	})
	if err != nil {
		return Member{}, err
	}
	return recordMember(ControlApp, executable, args, repositoryRoot, logPath, process), nil
}

func spawnAppMember(
	ctx context.Context,
	conductor *client.Client,
	definition runtimetopology.ResolvedProcess,
	spawn spawnContext,
) (Member, error) {
	delegated, err := conductor.DelegateSidecar(ctx, definition.App, capabilityTTL, definition.Capabilities)
	if err != nil {
		return Member{}, fmt.Errorf("delegate capability: %w", err)
	}
	launchEnvironment, err := protocol.LaunchEnvironmentMap(protocol.SidecarLaunch{
		App: definition.App, Control: spawn.descriptor, Token: delegated.Token,
		Channel: spawn.stamp.Channel, Namespace: spawn.stamp.Namespace, DataDir: spawn.baseDir,
		Installation: spawn.installation, Mode: protocol.LifecycleModeDev,
		Presentation: protocol.PresentationInteractive, Source: "oc-control",
	})
	if err != nil {
		return Member{}, err
	}
	logFile, logPath, err := openMemberLog(spawn.logDir, definition.App, spawn.stamp.Generation)
	if err != nil {
		return Member{}, err
	}
	defer logFile.Close()
	args := append(append([]string(nil), definition.Args...), spawn.stamp.Argument())
	process, err := lifecycle.Start(ctx, lifecycle.ProcessSpec{
		Executable: definition.Command, Args: args, Directory: definition.WorkingDirectory,
		Stdout: logFile, Stderr: logFile,
		Profile: lifecycle.ProfileDevelopment, Sandbox: definition.Sandbox, Detached: true,
		Env: environment.Merge(os.Environ(), definition.UnsetEnv, spawn.environment, definition.Env, launchEnvironment),
	})
	if err != nil {
		return Member{}, err
	}
	return recordMember(definition.App, definition.Command, args, definition.WorkingDirectory, logPath, process), nil
}

func recordMember(app, executable string, args []string, directory, logPath string, process *lifecycle.Process) Member {
	member := Member{
		App: app, Executable: executable, Args: args, Directory: directory,
		PID: process.PID(), StartedAt: time.Now().UTC(), Log: logPath,
	}
	if created, err := procident.CreateTimeMs(member.PID); err == nil {
		member.CreateTimeMs = created
	}
	return member
}

func awaitControlMember(controlFile string, control Member) (protocol.ControlDescriptor, error) {
	deadline := time.Now().Add(controlReadyPatience)
	for time.Now().Before(deadline) {
		var descriptor protocol.ControlDescriptor
		if err := readJSONFile(controlFile, &descriptor); err == nil && descriptor.PID == control.PID {
			return descriptor, nil
		}
		if VerifyMember(control) == MemberStopped {
			return protocol.ControlDescriptor{}, fmt.Errorf(
				"control member exited during startup; last log: %s", tailFile(control.Log, 5),
			)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return protocol.ControlDescriptor{}, fmt.Errorf(
		"control member did not publish its descriptor within %s; log: %s", controlReadyPatience, control.Log,
	)
}

func awaitSuiteReady(ctx context.Context, controlFile, observerTokenFile string, apps []string) (protocol.Status, error) {
	deadline := time.Now().Add(suiteReadyPatience)
	var last protocol.Status
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return protocol.Status{}, err
		}
		observer, err := client.Load(controlFile, observerTokenFile)
		if err == nil {
			status, statusErr := observer.Status(ctx)
			if statusErr == nil {
				last = status
				if allReady(status, apps) {
					return status, nil
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	pending := make([]string, 0, len(apps))
	ready := make(map[string]bool, len(last.Sessions))
	for _, session := range last.Sessions {
		ready[session.App] = session.Ready
	}
	for _, app := range apps {
		if !ready[app] {
			pending = append(pending, app)
		}
	}
	return protocol.Status{}, fmt.Errorf("suite apps %v did not reach READY within %s", pending, suiteReadyPatience)
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

func readJSONFile(path string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

func tailFile(path string, lines int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "(unreadable)"
	}
	return strings.ReplaceAll(tailString(string(raw), lines), "\n", " | ")
}

func tailString(content string, lines int) string {
	all := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}
