package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
)

const (
	maximumAgentProbeBytes    = 64 * 1024
	maximumAgentProbeDuration = 10 * time.Second
)

var ErrAgentProbeFailed = errors.New("local Agent qualification probe failed")

type AgentProbeInvocation struct {
	Executable string
	Args       []string
	Env        []string
}

type AgentProbeRunner interface {
	Run(context.Context, AgentProbeInvocation) ([]byte, error)
}

type AgentProbeEngine struct {
	profile lifecycle.Profile
}

func NewAgentProbeEngine(profile lifecycle.Profile) (*AgentProbeEngine, error) {
	if profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
		profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness {
		return nil, ErrAgentProbeFailed
	}
	return &AgentProbeEngine{profile: profile}, nil
}

func (engine *AgentProbeEngine) Run(
	ctx context.Context,
	invocation AgentProbeInvocation,
) ([]byte, error) {
	if engine == nil || !cleanAbsoluteAgentPath(invocation.Executable) || invocation.Env == nil {
		return nil, ErrAgentProbeFailed
	}
	probeContext, cancel := context.WithTimeout(ctx, maximumAgentProbeDuration)
	defer cancel()
	output := &boundedProbeOutput{}
	process, err := lifecycle.Start(probeContext, lifecycle.ProcessSpec{
		Executable: invocation.Executable, Args: append([]string(nil), invocation.Args...),
		Env: append([]string(nil), invocation.Env...), Stdin: strings.NewReader(""),
		Stdout: output, Stderr: output, Profile: engine.profile,
		Presentation: lifecycle.PresentationHeadless, ContainProcessTree: true,
		TerminationGrace: time.Second,
	})
	if err != nil {
		return nil, ErrAgentProbeFailed
	}
	if err := process.Wait(); err != nil || output.exceededLimit() || probeContext.Err() != nil {
		return nil, ErrAgentProbeFailed
	}
	return output.bytes(), nil
}

type boundedProbeOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	exceeded bool
}

func (output *boundedProbeOutput) Write(value []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	originalLength := len(value)
	remaining := maximumAgentProbeBytes - output.buffer.Len()
	if remaining <= 0 {
		output.exceeded = true
		return originalLength, nil
	}
	if len(value) > remaining {
		output.exceeded = true
		value = value[:remaining]
	}
	_, _ = output.buffer.Write(value)
	return originalLength, nil
}

func (output *boundedProbeOutput) bytes() []byte {
	output.mu.Lock()
	defer output.mu.Unlock()
	return bytes.Clone(output.buffer.Bytes())
}

func (output *boundedProbeOutput) exceededLimit() bool {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.exceeded
}

type CodexLocatorConfig struct {
	DataDir             string
	StableCLIExecutable string
	Candidates          []string
	Environment         []string
}

func LocateCodexCLI(
	ctx context.Context,
	config CodexLocatorConfig,
	probe AgentProbeRunner,
) (CodexCLIAdapterConfig, error) {
	if probe == nil || !cleanAbsoluteAgentPath(config.DataDir) ||
		!cleanAbsoluteAgentPath(config.StableCLIExecutable) || config.Environment == nil {
		return CodexCLIAdapterConfig{}, ErrAgentAdapterIncompatible
	}
	runtimeStore, err := NewCodexRuntimeStore(config.DataDir, config.StableCLIExecutable)
	if err != nil {
		return CodexCLIAdapterConfig{}, err
	}
	paths, err := runtimeStore.PrepareQualification()
	if err != nil {
		return CodexCLIAdapterConfig{}, ErrAgentAdapterIncompatible
	}
	defer runtimeStore.CollectQualification()
	environment := codexHostEnvironment(
		config.Environment, paths.Home, filepath.Dir(config.StableCLIExecutable),
	)
	isolatedEnvironment := codexHostEnvironment(
		append(append([]string(nil), config.Environment...), "CODEX_HOME="+paths.Home),
		paths.Home,
		filepath.Dir(config.StableCLIExecutable),
	)
	found, unauthenticated := false, false
	for _, candidate := range qualifiedAgentCandidates(config.Candidates) {
		found = true
		versionOutput, probeErr := probe.Run(ctx, AgentProbeInvocation{
			Executable: candidate, Args: []string{"--version"}, Env: isolatedEnvironment,
		})
		version, compatible := parseObservedCodexVersion(string(versionOutput))
		if probeErr != nil || !compatible {
			continue
		}
		execHelp, probeErr := probe.Run(ctx, AgentProbeInvocation{
			Executable: candidate, Args: []string{"exec", "--help"}, Env: isolatedEnvironment,
		})
		if probeErr != nil || !supportsCodexExecIsolation(execHelp) {
			continue
		}
		if _, probeErr = probe.Run(ctx, AgentProbeInvocation{
			Executable: candidate,
			Args: []string{
				"debug", "prompt-input",
				"-c", "default_permissions=\"open-cut-agent\"",
				"-c", "permissions.open-cut-agent=" + codexPermissionProfile(filepath.Dir(config.StableCLIExecutable)),
				"open-cut adapter qualification",
			},
			Env: isolatedEnvironment,
		}); probeErr != nil {
			continue
		}
		if _, probeErr = probe.Run(ctx, AgentProbeInvocation{
			Executable: candidate, Args: []string{"login", "status"}, Env: environment,
		}); probeErr != nil {
			unauthenticated = true
			continue
		}
		return CodexCLIAdapterConfig{
			Executable: candidate, Version: version, DataDir: config.DataDir,
			StableCLIExecutable: config.StableCLIExecutable,
			Environment:         append([]string(nil), config.Environment...),
		}, nil
	}
	if unauthenticated {
		return CodexCLIAdapterConfig{}, ErrAgentAdapterUnauthenticated
	}
	if found {
		return CodexCLIAdapterConfig{}, ErrAgentAdapterIncompatible
	}
	return CodexCLIAdapterConfig{}, ErrAgentAdapterMissing
}

type CodexQualificationPaths struct {
	Home string
}

func (store *CodexRuntimeStore) PrepareQualification() (CodexQualificationPaths, error) {
	if store == nil {
		return CodexQualificationPaths{}, ErrAgentAdapterIncompatible
	}
	home := filepath.Join(store.dataDir, "agent", codexAdapterDirectory, "qualification", "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return CodexQualificationPaths{}, err
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return CodexQualificationPaths{}, err
	}
	return CodexQualificationPaths{Home: home}, nil
}

func (store *CodexRuntimeStore) CollectQualification() error {
	if store == nil {
		return ErrAgentAdapterIncompatible
	}
	return removePrivateRuntime(filepath.Join(store.dataDir, "agent", codexAdapterDirectory, "qualification"))
}

func SystemCodexCandidates() []string {
	candidates := make([]string, 0, 3)
	if value, err := exec.LookPath("codex"); err == nil {
		candidates = append(candidates, value)
	}
	if runtime.GOOS == "windows" {
		if root := os.Getenv("LOCALAPPDATA"); root != "" {
			candidates = append(candidates, filepath.Join(root, "Programs", "OpenAI", "Codex", "bin", "codex.exe"))
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", "codex"))
	}
	return qualifiedAgentCandidates(candidates)
}

func qualifiedAgentCandidates(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		resolved, err := resolveAgentCandidate(value)
		if err != nil {
			continue
		}
		if _, exists := seen[resolved]; exists {
			continue
		}
		seen[resolved] = struct{}{}
		result = append(result, resolved)
	}
	return result
}

func resolveAgentCandidate(value string) (string, error) {
	if !filepath.IsAbs(value) {
		resolved, err := filepath.Abs(value)
		if err != nil {
			return "", err
		}
		value = resolved
	}
	value = filepath.Clean(value)
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		return "", ErrAgentAdapterMissing
	}
	return resolved, nil
}

func parseObservedCodexVersion(value string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 || fields[0] != "codex-cli" {
		return "", false
	}
	version := "codex-cli " + fields[1]
	if len(version) > 128 {
		return "", false
	}
	for _, value := range fields[1] {
		if unicode.IsControl(value) {
			return "", false
		}
	}
	return version, true
}

func supportsCodexExecIsolation(value []byte) bool {
	output := string(value)
	for _, capability := range []string{"--ignore-user-config", "--ignore-rules"} {
		if !strings.Contains(output, capability) {
			return false
		}
	}
	return true
}

type UnavailableAgentAdapter struct {
	err error
}

func NewUnavailableAgentAdapter(err error) *UnavailableAgentAdapter {
	if !errors.Is(err, ErrAgentAdapterMissing) && !errors.Is(err, ErrAgentAdapterUnauthenticated) &&
		!errors.Is(err, ErrAgentAdapterIncompatible) {
		err = ErrAgentAdapterIncompatible
	}
	return &UnavailableAgentAdapter{err: err}
}

func (*UnavailableAgentAdapter) ID() string      { return application.AgentBridgeAdapterCodexV1 }
func (*UnavailableAgentAdapter) Version() string { return "unavailable" }
func (adapter *UnavailableAgentAdapter) Check(context.Context) error {
	if adapter == nil || adapter.err == nil {
		return ErrAgentAdapterIncompatible
	}
	return adapter.err
}
func (adapter *UnavailableAgentAdapter) Execute(
	context.Context,
	AgentAdapterTurn,
	AgentProcessObserver,
) error {
	return adapter.Check(context.Background())
}

var _ io.Writer = (*boundedProbeOutput)(nil)
var _ AgentProbeRunner = (*AgentProbeEngine)(nil)
var _ AgentTurnAdapter = (*UnavailableAgentAdapter)(nil)
