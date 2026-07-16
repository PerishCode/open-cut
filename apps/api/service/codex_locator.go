package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
)

const (
	qualifiedCodexMajor       = 0
	qualifiedCodexMinor       = 144
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
	found, unauthenticated := false, false
	for _, candidate := range qualifiedAgentCandidates(config.Candidates) {
		found = true
		versionOutput, probeErr := probe.Run(ctx, AgentProbeInvocation{
			Executable: candidate, Args: []string{"--version"}, Env: environment,
		})
		version, compatible := parseQualifiedCodexVersion(string(versionOutput))
		if probeErr != nil || !compatible {
			continue
		}
		if _, probeErr = probe.Run(ctx, AgentProbeInvocation{
			Executable: candidate,
			Args:       []string{"debug", "prompt-input", "open-cut adapter qualification"}, Env: environment,
		}); probeErr != nil {
			continue
		}
		ruleOutput, probeErr := probe.Run(ctx, AgentProbeInvocation{
			Executable: candidate,
			Args: []string{
				"execpolicy", "check", "--rules", filepath.Join(paths.Home, "rules", "open-cut.rules"),
				"--", filepath.Base(config.StableCLIExecutable), "--help",
			},
			Env: environment,
		})
		if probeErr != nil || !allowedCodexRuleProbe(ruleOutput) {
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
	for _, root := range []string{home, filepath.Join(home, "rules")} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return CodexQualificationPaths{}, err
		}
		if err := os.Chmod(root, 0o700); err != nil {
			return CodexQualificationPaths{}, err
		}
	}
	if err := replacePrivateFile(filepath.Join(home, "config.toml"), store.config()); err != nil {
		return CodexQualificationPaths{}, err
	}
	if err := replacePrivateFile(filepath.Join(home, "rules", "open-cut.rules"), store.rules()); err != nil {
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

func FindStableOpenCutCLI() (string, error) {
	value, err := exec.LookPath("open-cut")
	if err != nil {
		return "", ErrAgentAdapterIncompatible
	}
	resolved, err := resolveAgentCandidate(value)
	if err != nil {
		return "", ErrAgentAdapterIncompatible
	}
	name := filepath.Base(resolved)
	if name != "open-cut" && name != "open-cut.exe" {
		return "", ErrAgentAdapterIncompatible
	}
	return resolved, nil
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

func parseQualifiedCodexVersion(value string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 || fields[0] != "codex-cli" {
		return "", false
	}
	parts := strings.Split(fields[1], ".")
	if len(parts) != 3 {
		return "", false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	patch, patchErr := strconv.Atoi(parts[2])
	if majorErr != nil || minorErr != nil || patchErr != nil || patch < 0 ||
		major != qualifiedCodexMajor || minor != qualifiedCodexMinor {
		return "", false
	}
	return fmt.Sprintf("codex-cli %d.%d.%d", major, minor, patch), true
}

func allowedCodexRuleProbe(value []byte) bool {
	var output struct {
		Decision string `json:"decision"`
	}
	return json.Unmarshal(value, &output) == nil && output.Decision == "allow"
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
