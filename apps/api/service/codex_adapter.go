package service

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrAgentAdapterMissing         = errors.New("local Agent adapter is missing")
	ErrAgentAdapterUnauthenticated = errors.New("local Agent adapter is unauthenticated")
	ErrAgentAdapterIncompatible    = errors.New("local Agent adapter is incompatible")
)

type CodexCLIAdapterConfig struct {
	Executable          string
	Version             string
	DataDir             string
	StableCLIExecutable string
	Environment         []string
}

type CodexCLIAdapter struct {
	config  CodexCLIAdapterConfig
	runner  AgentProcessRunner
	runtime *CodexRuntimeStore
}

func NewCodexCLIAdapter(config CodexCLIAdapterConfig, runner AgentProcessRunner) (*CodexCLIAdapter, error) {
	if runner == nil || !validCodexCLIAdapterConfig(config) {
		return nil, ErrAgentAdapterIncompatible
	}
	runtime, err := NewCodexRuntimeStore(config.DataDir, config.StableCLIExecutable)
	if err != nil {
		return nil, err
	}
	return &CodexCLIAdapter{config: config, runner: runner, runtime: runtime}, nil
}

func (adapter *CodexCLIAdapter) ID() string { return application.AgentBridgeAdapterCodexV1 }

func (adapter *CodexCLIAdapter) Version() string {
	if adapter == nil {
		return ""
	}
	return adapter.config.Version
}

func (adapter *CodexCLIAdapter) Check(context.Context) error {
	if adapter == nil || adapter.runner == nil || adapter.runtime == nil || !validCodexCLIAdapterConfig(adapter.config) {
		return ErrAgentAdapterIncompatible
	}
	return nil
}

func (adapter *CodexCLIAdapter) Execute(
	ctx context.Context,
	turn AgentAdapterTurn,
	observer AgentProcessObserver,
) error {
	if err := adapter.Check(ctx); err != nil {
		return err
	}
	if observer == nil || turn.ProjectID.IsZero() || turn.RunID.IsZero() || turn.TurnID.IsZero() ||
		len(turn.Prompt) == 0 || len(turn.RecoveryPrompt) == 0 ||
		(turn.SequenceID != nil && turn.SequenceID.IsZero()) ||
		(turn.NativeSessionID != "" && !validOpaqueCodexSession(turn.NativeSessionID)) {
		return ErrAgentProcessInvalid
	}
	paths, err := adapter.runtime.Prepare(turn.RunID, turn.TurnID)
	if err != nil {
		return err
	}
	defer adapter.runtime.CollectTurn(turn.RunID, turn.TurnID)
	stableCLIDirectory := filepath.Dir(adapter.config.StableCLIExecutable)
	result, decoder, err := adapter.executeOnce(ctx, turn, paths, stableCLIDirectory, observer)
	if err != nil && ctx.Err() == nil && decoder.FreshFallbackSafe() {
		if noticeErr := observer.ObserveContextRebuilt(ctx); noticeErr != nil {
			return noticeErr
		}
		turn.NativeSessionID = ""
		result, _, err = adapter.executeOnce(ctx, turn, paths, stableCLIDirectory, observer)
	}
	if err != nil {
		return err
	}
	if result.MessageCount == 0 || result.NativeSessionID == "" ||
		(turn.NativeSessionID != "" && result.NativeSessionID != turn.NativeSessionID) {
		return ErrAgentProcessProtocol
	}
	return nil
}

func (adapter *CodexCLIAdapter) executeOnce(
	ctx context.Context,
	turn AgentAdapterTurn,
	paths CodexRuntimePaths,
	stableCLIDirectory string,
	observer AgentProcessObserver,
) (AgentProcessResult, *CodexJSONLDecoder, error) {
	args, prompt := codexTurnArguments(turn, stableCLIDirectory)
	decoder := NewCodexJSONLDecoder()
	if turn.NativeSessionID != "" {
		decoder = NewCodexResumeJSONLDecoder(turn.NativeSessionID)
	}
	result, err := adapter.runner.Run(ctx, AgentProcessInvocation{
		Executable: adapter.config.Executable, Args: args, Directory: paths.Scratch,
		Env: codexHostEnvironment(adapter.config.Environment, paths.Home, stableCLIDirectory), Prompt: prompt,
	}, decoder, observer)
	return result, decoder, err
}

func codexTurnArguments(turn AgentAdapterTurn, stableCLIDirectory string) ([]string, string) {
	appState := map[string]string{
		"PATH":                stableCLIDirectory,
		"OPEN_CUT_PROJECT_ID": turn.ProjectID.String(),
		"OPEN_CUT_RUN_ID":     turn.RunID.String(),
		"OPEN_CUT_TURN_ID":    turn.TurnID.String(),
		"OPEN_CUT_OUTPUT":     "json",
		"OPEN_CUT_WAIT_MS":    "30000",
	}
	if turn.SequenceID != nil {
		appState["OPEN_CUT_SEQUENCE_ID"] = turn.SequenceID.String()
	}
	overrides := []string{
		"approval_policy=\"never\"",
		"allow_login_shell=false",
		"default_permissions=\"open-cut-agent\"",
		"permissions.open-cut-agent=" + codexPermissionProfile(stableCLIDirectory),
		"shell_environment_policy.inherit=\"none\"",
		"shell_environment_policy.ignore_default_excludes=false",
		"shell_environment_policy.set=" + tomlInlineStringMap(appState),
		"web_search=\"disabled\"",
	}
	args := []string{"exec"}
	prompt := turn.RecoveryPrompt
	if turn.NativeSessionID != "" {
		args = append(args, "resume")
		prompt = turn.Prompt
	}
	args = append(
		args,
		"--json",
		"--strict-config",
		"--skip-git-repo-check",
		"--ignore-user-config",
		"--ignore-rules",
	)
	if turn.NativeSessionID != "" {
		args = append(args, "--all")
	}
	for _, override := range overrides {
		args = append(args, "-c", override)
	}
	if turn.NativeSessionID != "" {
		args = append(args, turn.NativeSessionID)
	}
	args = append(args, "-")
	return args, prompt
}

func codexPermissionProfile(stableCLIDirectory string) string {
	return `{filesystem={":minimal"="read",` + strconv.Quote(stableCLIDirectory) +
		`="read",":workspace_roots"={"."="write"}},network={enabled=true,mode="limited",` +
		`domains={"127.0.0.1"="allow","localhost"="allow"},allow_upstream_proxy=false,` +
		`allow_local_binding=false}}`
}

func tomlInlineStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Quote(values[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func codexHostEnvironment(base []string, privateHome, stableCLIDirectory string) []string {
	allowed := map[string]bool{
		"HOME": true, "USER": true, "LOGNAME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
		"TMPDIR": true, "TMP": true, "TEMP": true, "SYSTEMROOT": true, "WINDIR": true,
		"COMSPEC": true, "PATHEXT": true, "USERPROFILE": true, "HOMEDRIVE": true, "HOMEPATH": true,
		"CODEX_HOME": true, "HTTPS_PROXY": true, "HTTP_PROXY": true,
		"ALL_PROXY": true, "NO_PROXY": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	}
	values := make(map[string]string)
	for _, entry := range base {
		name, value, found := strings.Cut(entry, "=")
		canonical := strings.ToUpper(name)
		if found && allowed[canonical] {
			values[canonical] = canonical + "=" + value
		}
	}
	values["CODEX_SQLITE_HOME"] = "CODEX_SQLITE_HOME=" + privateHome
	values["PATH"] = "PATH=" + stableCLIDirectory
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}

func validCodexCLIAdapterConfig(config CodexCLIAdapterConfig) bool {
	return cleanAbsoluteAgentPath(config.Executable) && cleanAbsoluteAgentPath(config.DataDir) &&
		cleanAbsoluteAgentPath(config.StableCLIExecutable) &&
		config.Version != "" && len(config.Version) <= 128 && config.Environment != nil
}

func (adapter *CodexCLIAdapter) CollectAgentRun(runID domain.RunID) error {
	if adapter == nil || adapter.runtime == nil {
		return ErrAgentAdapterIncompatible
	}
	return adapter.runtime.CollectRun(runID)
}

func cleanAbsoluteAgentPath(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value
}

var _ AgentTurnAdapter = (*CodexCLIAdapter)(nil)
var _ AgentProcessRunner = (*AgentProcessEngine)(nil)
