package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/domain"
)

type recordingAgentObserver struct {
	session      string
	rebuilt      int
	messages     []string
	presentation []service.AgentPresentationEvent
}

type recordingAgentProcessRunner struct {
	invocation service.AgentProcessInvocation
}

type scriptedAgentProbe struct {
	loginError error
	versions   map[string]string
	seen       []service.AgentProbeInvocation
}

func (probe *scriptedAgentProbe) Run(
	_ context.Context,
	invocation service.AgentProbeInvocation,
) ([]byte, error) {
	probe.seen = append(probe.seen, invocation)
	command := strings.Join(invocation.Args, " ")
	switch {
	case command == "--version":
		version := probe.versions[invocation.Executable]
		if version == "" {
			version = probe.versions[filepath.Base(invocation.Executable)]
		}
		return []byte(version), nil
	case strings.HasPrefix(command, "debug prompt-input "):
		return []byte(`[]`), nil
	case strings.HasPrefix(command, "execpolicy check "):
		return []byte(`{"decision":"allow"}`), nil
	case command == "login status":
		return []byte("Logged in using ChatGPT"), probe.loginError
	default:
		return nil, service.ErrAgentProbeFailed
	}
}

func (runner *recordingAgentProcessRunner) Run(
	ctx context.Context,
	invocation service.AgentProcessInvocation,
	decoder service.AgentStreamDecoder,
	observer service.AgentProcessObserver,
) (service.AgentProcessResult, error) {
	runner.invocation = invocation
	session := "new-private-session"
	for index := 0; index+1 < len(invocation.Args); index++ {
		if invocation.Args[index] == "--all" && index+len(invocation.Args) > 2 {
			session = invocation.Args[len(invocation.Args)-2]
			break
		}
	}
	lines := []string{
		`{"type":"thread.started","thread_id":"` + session + `"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`,
		`{"type":"turn.completed"}`,
	}
	for _, line := range lines {
		if err := decoder.Consume(ctx, []byte(line), observer); err != nil {
			return service.AgentProcessResult{}, err
		}
	}
	return decoder.Finish()
}

func (observer *recordingAgentObserver) ObserveNativeSession(_ context.Context, value string) error {
	observer.session = value
	return nil
}

func (observer *recordingAgentObserver) ObserveContextRebuilt(context.Context) error {
	observer.rebuilt++
	return nil
}

func (observer *recordingAgentObserver) ObserveAgentMessage(_ context.Context, value string) error {
	observer.messages = append(observer.messages, value)
	return nil
}

func (observer *recordingAgentObserver) ObserveAgentPresentation(
	_ context.Context,
	value service.AgentPresentationEvent,
) error {
	observer.presentation = append(observer.presentation, value)
	return nil
}

func TestAgentProcessEngineDecodesOnlySafeCodexEvents(t *testing.T) {
	observer := &recordingAgentObserver{}
	result, err := runAgentHelper(t, context.Background(), "success", 5*time.Second, observer)
	if err != nil {
		t.Fatal(err)
	}
	if result.NativeSessionID != "019f6c70-0dc2-78de-8227-e548de0173b8" || result.MessageCount != 1 ||
		observer.session != result.NativeSessionID || len(observer.messages) != 1 ||
		observer.messages[0] != "I updated the narrative through open-cut." {
		t.Fatalf("result=%+v observer=%+v", result, observer)
	}
	want := []service.AgentPresentationEvent{
		{Kind: service.AgentPresentationTurnStarted},
		{Kind: service.AgentPresentationToolStarted, Tool: service.AgentPresentationCommand},
		{Kind: service.AgentPresentationToolCompleted, Tool: service.AgentPresentationCommand},
		{Kind: service.AgentPresentationMessage},
		{Kind: service.AgentPresentationTurnCompleted},
	}
	if fmt.Sprint(observer.presentation) != fmt.Sprint(want) {
		t.Fatalf("presentation=%+v want=%+v", observer.presentation, want)
	}
}

func TestAgentProcessEngineRejectsUnknownOrOversizedCodexOutput(t *testing.T) {
	for _, test := range []struct {
		mode string
		want error
	}{
		{mode: "unknown-event", want: service.ErrAgentProcessProtocol},
		{mode: "oversized-line", want: service.ErrAgentProcessResourceLimit},
		{mode: "oversized-message-total", want: service.ErrAgentProcessResourceLimit},
	} {
		t.Run(test.mode, func(t *testing.T) {
			_, err := runAgentHelper(t, context.Background(), test.mode, 5*time.Second, &recordingAgentObserver{})
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestAgentProcessEngineContainsCancellationAndHidesNativeFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := runAgentHelper(t, ctx, "wait", 5*time.Second, &recordingAgentObserver{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	_, err = runAgentHelper(t, context.Background(), "native-failure", 5*time.Second, &recordingAgentObserver{})
	if !errors.Is(err, service.ErrAgentProcessFailed) || strings.Contains(fmt.Sprint(err), "private-native-detail") {
		t.Fatalf("failure=%v", err)
	}
}

func TestCodexCLIAdapterKeepsPromptOnStdinAndInjectsOnlyClosedAppState(t *testing.T) {
	root := t.TempDir()
	runner := &recordingAgentProcessRunner{}
	adapter, err := service.NewCodexCLIAdapter(service.CodexCLIAdapterConfig{
		Executable: filepath.Join(root, "bin", "codex"), Version: "codex-cli 0.144.4",
		DataDir: filepath.Join(root, "api"), StableCLIExecutable: filepath.Join(root, "stable-cli", "open-cut"),
		Environment: []string{"HOME=" + root, "PATH=/untrusted", "OPENAI_API_KEY=must-not-pass", "LANG=en_US.UTF-8"},
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	projectID := mustProjectID(t, time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC))
	runID := mustRunID(t, time.Date(2026, 7, 16, 6, 0, 0, int(time.Millisecond), time.UTC))
	turnID := mustTurnID(t, time.Date(2026, 7, 16, 6, 0, 0, int(2*time.Millisecond), time.UTC))
	sequenceID := mustSequenceID(t, time.Date(2026, 7, 16, 6, 0, 0, int(3*time.Millisecond), time.UTC))
	observer := &recordingAgentObserver{}
	if err := adapter.Execute(context.Background(), service.AgentAdapterTurn{
		ProjectID: projectID, RunID: runID, TurnID: turnID, SequenceID: &sequenceID,
		Prompt: "current creator prompt", RecoveryPrompt: "private recovery prompt",
	}, observer); err != nil {
		t.Fatal(err)
	}
	if runner.invocation.Prompt != "private recovery prompt" || runner.invocation.Directory != filepath.Join(
		root, "api", "scratch", "runs", runID.String(), "turns", turnID.String(), "agent",
	) {
		t.Fatalf("invocation=%+v", runner.invocation)
	}
	arguments := strings.Join(runner.invocation.Args, "\x00")
	for _, forbidden := range []string{"current creator prompt", "private recovery prompt", "OPENAI_API_KEY", "/untrusted"} {
		if strings.Contains(arguments, forbidden) {
			t.Fatalf("argv leaked %q: %q", forbidden, arguments)
		}
	}
	for _, required := range []string{
		"exec", "--json", "--strict-config", "--skip-git-repo-check", "web_search=\"disabled\"",
		"OPEN_CUT_PROJECT_ID", projectID.String(), "OPEN_CUT_SEQUENCE_ID", sequenceID.String(),
		"OPEN_CUT_RUN_ID", runID.String(), "OPEN_CUT_TURN_ID", turnID.String(), "OPEN_CUT_OUTPUT", "OPEN_CUT_WAIT_MS",
	} {
		if !strings.Contains(arguments, required) {
			t.Fatalf("argv missing %q: %q", required, arguments)
		}
	}
	environment := strings.Join(runner.invocation.Env, "\n")
	if strings.Contains(environment, "OPENAI_API_KEY") || strings.Contains(environment, "/untrusted") ||
		!strings.Contains(environment, "CODEX_HOME="+filepath.Join(
			root, "api", "agent", "codex-cli-v1", "runs", runID.String(), "home",
		)) ||
		!strings.Contains(environment, "PATH="+filepath.Join(root, "stable-cli")) {
		t.Fatalf("environment=%q", environment)
	}
	if len(observer.messages) != 1 || observer.session != "new-private-session" {
		t.Fatalf("observer=%+v", observer)
	}
}

func TestCodexCLIAdapterResumesOnlyTheExactOpaqueSession(t *testing.T) {
	root := t.TempDir()
	runner := &recordingAgentProcessRunner{}
	adapter, err := service.NewCodexCLIAdapter(service.CodexCLIAdapterConfig{
		Executable: filepath.Join(root, "codex"), Version: "codex-test",
		DataDir: filepath.Join(root, "api"), StableCLIExecutable: filepath.Join(root, "cli", "open-cut"),
		Environment: []string{},
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	session := "opaque-session-id"
	if err := adapter.Execute(context.Background(), service.AgentAdapterTurn{
		ProjectID: mustProjectID(t, time.Now()), RunID: mustRunID(t, time.Now().Add(time.Millisecond)),
		TurnID: mustTurnID(t, time.Now().Add(2*time.Millisecond)), NativeSessionID: session,
		Prompt: "current", RecoveryPrompt: "recovery",
	}, &recordingAgentObserver{}); err != nil {
		t.Fatal(err)
	}
	arguments := strings.Join(runner.invocation.Args, "\x00")
	if runner.invocation.Prompt != "current" || !strings.Contains(arguments, "exec\x00resume") ||
		!strings.Contains(arguments, "--all\x00") || !strings.Contains(arguments, session+"\x00-") {
		t.Fatalf("resume invocation=%+v", runner.invocation)
	}
}

type resumeFallbackRunner struct {
	started     bool
	invocations []service.AgentProcessInvocation
}

func (runner *resumeFallbackRunner) Run(
	ctx context.Context,
	invocation service.AgentProcessInvocation,
	decoder service.AgentStreamDecoder,
	observer service.AgentProcessObserver,
) (service.AgentProcessResult, error) {
	runner.invocations = append(runner.invocations, invocation)
	if len(runner.invocations) == 1 {
		if err := decoder.Consume(ctx, []byte(`{"type":"thread.started","thread_id":"missing-session"}`), observer); err != nil {
			return service.AgentProcessResult{}, err
		}
		if runner.started {
			if err := decoder.Consume(ctx, []byte(`{"type":"turn.started"}`), observer); err != nil {
				return service.AgentProcessResult{}, err
			}
		}
		return service.AgentProcessResult{}, decoder.Consume(ctx, []byte(`{"type":"error"}`), observer)
	}
	for _, line := range []string{
		`{"type":"thread.started","thread_id":"fresh-session"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"recovered"}}`,
		`{"type":"turn.completed"}`,
	} {
		if err := decoder.Consume(ctx, []byte(line), observer); err != nil {
			return service.AgentProcessResult{}, err
		}
	}
	return decoder.Finish()
}

func TestCodexCLIAdapterFallsBackOnlyBeforeNativeTurnStarts(t *testing.T) {
	for _, test := range []struct {
		name        string
		started     bool
		wantCalls   int
		wantRebuilt int
		wantError   error
	}{
		{name: "pre-start", wantCalls: 2, wantRebuilt: 1},
		{name: "post-start", started: true, wantCalls: 1, wantError: service.ErrAgentProcessFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			runner := &resumeFallbackRunner{started: test.started}
			adapter, err := service.NewCodexCLIAdapter(service.CodexCLIAdapterConfig{
				Executable: filepath.Join(root, "codex"), Version: "codex-test",
				DataDir: filepath.Join(root, "api"), StableCLIExecutable: filepath.Join(root, "cli", "open-cut"),
				Environment: []string{},
			}, runner)
			if err != nil {
				t.Fatal(err)
			}
			observer := &recordingAgentObserver{}
			err = adapter.Execute(context.Background(), service.AgentAdapterTurn{
				ProjectID: mustProjectID(t, time.Now()), RunID: mustRunID(t, time.Now().Add(time.Millisecond)),
				TurnID: mustTurnID(t, time.Now().Add(2*time.Millisecond)), NativeSessionID: "missing-session",
				Prompt: "current", RecoveryPrompt: "bounded recovery",
			}, observer)
			if !errors.Is(err, test.wantError) || len(runner.invocations) != test.wantCalls ||
				observer.rebuilt != test.wantRebuilt {
				t.Fatalf("error=%v calls=%d observer=%+v", err, len(runner.invocations), observer)
			}
			if test.wantCalls == 2 && (runner.invocations[0].Prompt != "current" ||
				runner.invocations[1].Prompt != "bounded recovery" || observer.session != "fresh-session") {
				t.Fatalf("fallback invocations=%+v observer=%+v", runner.invocations, observer)
			}
		})
	}
}

func TestCodexRuntimeStoreDerivesPrivateRunHomeAndTurnScratch(t *testing.T) {
	root := t.TempDir()
	store, err := service.NewCodexRuntimeStore(
		filepath.Join(root, "api"), filepath.Join(root, "stable", "open-cut"),
	)
	if err != nil {
		t.Fatal(err)
	}
	runID := mustRunID(t, time.Date(2026, 7, 16, 7, 0, 0, 0, time.UTC))
	turnID := mustTurnID(t, time.Date(2026, 7, 16, 7, 0, 0, int(time.Millisecond), time.UTC))
	paths, err := store.Prepare(runID, turnID)
	if err != nil {
		t.Fatal(err)
	}
	wantHome := filepath.Join(root, "api", "agent", "codex-cli-v1", "runs", runID.String(), "home")
	wantScratch := filepath.Join(root, "api", "scratch", "runs", runID.String(), "turns", turnID.String(), "agent")
	if paths.Home != wantHome || paths.Scratch != wantScratch {
		t.Fatalf("paths=%+v", paths)
	}
	config, err := os.ReadFile(filepath.Join(paths.Home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := os.ReadFile(filepath.Join(paths.Home, "rules", "open-cut.rules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`default_permissions = "open-cut-agent"`, `cli_auth_credentials_store = "keyring"`,
		`[permissions.open-cut-agent.filesystem]`, `":minimal" = "read"`,
		`[permissions.open-cut-agent.filesystem.":workspace_roots"]`, `enabled = false`,
	} {
		if !strings.Contains(string(config), required) {
			t.Fatalf("config missing %q:\n%s", required, config)
		}
	}
	if strings.Contains(string(config), "danger-full-access") || strings.Contains(string(config), "auth.json") ||
		!strings.Contains(string(rules), `pattern = ["open-cut"]`) {
		t.Fatalf("unsafe runtime config=%q rules=%q", config, rules)
	}
	if _, err := os.Stat(filepath.Join(paths.Home, "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private home must not contain file credentials: %v", err)
	}
	for _, path := range []string{filepath.Join(paths.Home, "config.toml"), filepath.Join(paths.Home, "rules", "open-cut.rules")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private file %s mode=%v err=%v", path, info.Mode().Perm(), err)
		}
	}
	if err := store.CollectTurn(runID, turnID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("turn scratch survived collection: %v", err)
	}
	if _, err := os.Stat(paths.Home); err != nil {
		t.Fatalf("Run home must survive Turn collection: %v", err)
	}
	if err := store.CollectRun(runID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Run home survived Run collection: %v", err)
	}
}

func TestCodexLocatorUsesFirstFullyQualifiedCandidateAndKeyringHome(t *testing.T) {
	root := t.TempDir()
	first := executableFixture(t, filepath.Join(root, "first-codex"))
	second := executableFixture(t, filepath.Join(root, "second-codex"))
	stableCLI := executableFixture(t, filepath.Join(root, "stable", "open-cut"))
	probe := &scriptedAgentProbe{versions: map[string]string{
		filepath.Base(first): "codex-cli 0.143.9", filepath.Base(second): "codex-cli 0.144.4",
	}}
	config, err := service.LocateCodexCLI(context.Background(), service.CodexLocatorConfig{
		DataDir: filepath.Join(root, "api"), StableCLIExecutable: stableCLI,
		Candidates: []string{first, second}, Environment: []string{"HOME=" + root},
	}, probe)
	if err != nil {
		t.Fatal(err)
	}
	resolvedSecond, _ := filepath.EvalSymlinks(second)
	if config.Executable != resolvedSecond || config.Version != "codex-cli 0.144.4" {
		t.Fatalf("config=%+v", config)
	}
	joined := ""
	for _, invocation := range probe.seen {
		joined += strings.Join(invocation.Env, "\n") + "\n"
	}
	if !strings.Contains(joined, "CODEX_HOME="+filepath.Join(
		root, "api", "agent", "codex-cli-v1", "qualification", "home",
	)) || strings.Contains(joined, "auth.json") {
		t.Fatalf("probe environment=%q", joined)
	}
	if _, err := os.Stat(filepath.Join(root, "api", "agent", "codex-cli-v1", "qualification")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("qualification home survived: %v", err)
	}
}

func TestCodexLocatorClassifiesMissingUnauthenticatedAndIncompatible(t *testing.T) {
	root := t.TempDir()
	stableCLI := executableFixture(t, filepath.Join(root, "stable", "open-cut"))
	candidate := executableFixture(t, filepath.Join(root, "codex"))
	for _, test := range []struct {
		name       string
		candidates []string
		probe      *scriptedAgentProbe
		want       error
	}{
		{name: "missing", want: service.ErrAgentAdapterMissing, probe: &scriptedAgentProbe{}},
		{
			name: "incompatible", candidates: []string{candidate}, want: service.ErrAgentAdapterIncompatible,
			probe: &scriptedAgentProbe{versions: map[string]string{filepath.Base(candidate): "codex-cli 0.145.0"}},
		},
		{
			name: "unauthenticated", candidates: []string{candidate}, want: service.ErrAgentAdapterUnauthenticated,
			probe: &scriptedAgentProbe{
				versions: map[string]string{filepath.Base(candidate): "codex-cli 0.144.0"}, loginError: service.ErrAgentProbeFailed,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.LocateCodexCLI(context.Background(), service.CodexLocatorConfig{
				DataDir: filepath.Join(root, "api-"+test.name), StableCLIExecutable: stableCLI,
				Candidates: test.candidates, Environment: []string{},
			}, test.probe)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func executableFixture(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAgentPresentationHubIsNonReplayableBoundedAndTerminal(t *testing.T) {
	hub := service.NewAgentPresentationHub()
	runID := mustRunID(t, time.Date(2026, 7, 16, 4, 0, 0, 0, time.UTC))
	turnID := mustTurnID(t, time.Date(2026, 7, 16, 4, 0, 0, int(time.Millisecond), time.UTC))
	subscription, err := hub.SubscribeAgentPresentation(runID, turnID)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	for _, event := range []service.AgentPresentationEvent{
		{Kind: service.AgentPresentationTurnStarted},
		{Kind: service.AgentPresentationToolStarted, Tool: service.AgentPresentationCommand},
		{Kind: service.AgentPresentationTurnCompleted},
	} {
		if err := hub.PublishAgentPresentation(runID, turnID, event); err != nil {
			t.Fatal(err)
		}
	}
	for want := uint64(1); want <= 3; want++ {
		event, ok := subscription.Next(context.Background())
		if !ok || event.Sequence.Value() != want || event.RunID != runID || event.TurnID != turnID {
			t.Fatalf("event=%+v ok=%v wantSequence=%d", event, ok, want)
		}
	}
	if _, ok := subscription.Next(context.Background()); ok {
		t.Fatal("terminal presentation subscription stayed open")
	}
}

func TestAgentPresentationHubCapsSubscribers(t *testing.T) {
	hub := service.NewAgentPresentationHub()
	runID := mustRunID(t, time.Date(2026, 7, 16, 5, 0, 0, 0, time.UTC))
	turnID := mustTurnID(t, time.Date(2026, 7, 16, 5, 0, 0, int(time.Millisecond), time.UTC))
	subscriptions := make([]*service.AgentPresentationSubscription, 0, service.MaximumAgentPresentationSubscribers)
	for range service.MaximumAgentPresentationSubscribers {
		subscription, err := hub.SubscribeAgentPresentation(runID, turnID)
		if err != nil {
			t.Fatal(err)
		}
		subscriptions = append(subscriptions, subscription)
	}
	if _, err := hub.SubscribeAgentPresentation(runID, turnID); !errors.Is(err, service.ErrAgentPresentationUnavailable) {
		t.Fatalf("subscriber cap error=%v", err)
	}
	for _, subscription := range subscriptions {
		subscription.Close()
	}
}

func TestAgentProcessHelper(t *testing.T) {
	if os.Getenv("OPEN_CUT_AGENT_HELPER") != "1" {
		return
	}
	mode := os.Getenv("OPEN_CUT_AGENT_HELPER_MODE")
	encode := json.NewEncoder(os.Stdout)
	event := func(value any) { _ = encode.Encode(value) }
	switch mode {
	case "success":
		event(map[string]any{"type": "thread.started", "thread_id": "019f6c70-0dc2-78de-8227-e548de0173b8"})
		event(map[string]any{"type": "turn.started"})
		event(map[string]any{"type": "item.started", "item": map[string]any{"type": "command_execution", "command": "private"}})
		event(map[string]any{"type": "item.completed", "item": map[string]any{"type": "command_execution", "command": "private"}})
		event(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "I updated the narrative through open-cut."}})
		event(map[string]any{"type": "turn.completed", "usage": map[string]any{"input_tokens": 1}})
	case "unknown-event":
		event(map[string]any{"type": "thread.started", "thread_id": "session"})
		event(map[string]any{"type": "future.event", "private": "must-not-leak"})
	case "oversized-line":
		_, _ = fmt.Fprintln(os.Stdout, strings.Repeat("x", service.MaximumAgentJSONLLineBytes+1))
	case "oversized-message-total":
		event(map[string]any{"type": "thread.started", "thread_id": "session"})
		text := strings.Repeat("x", 140*1024)
		event(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": text}})
		event(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": text}})
	case "wait":
		time.Sleep(30 * time.Second)
	case "native-failure":
		_, _ = fmt.Fprintln(os.Stderr, "private-native-detail")
		os.Exit(17)
	default:
		os.Exit(18)
	}
	os.Exit(0)
}

func runAgentHelper(
	t *testing.T,
	ctx context.Context,
	mode string,
	timeout time.Duration,
	observer service.AgentProcessObserver,
) (service.AgentProcessResult, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := service.NewAgentProcessEngine(lifecycle.ProfileHarness)
	if err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "OPEN_CUT_AGENT_HELPER=1", "OPEN_CUT_AGENT_HELPER_MODE="+mode)
	return engine.Run(ctx, service.AgentProcessInvocation{
		Executable: executable,
		Args:       []string{"-test.run=^TestAgentProcessHelper$"},
		Directory:  t.TempDir(),
		Env:        environment,
		Prompt:     "test prompt",
		Timeout:    timeout,
	}, service.NewCodexJSONLDecoder(), observer)
}

func mustRunID(t *testing.T, at time.Time) domain.RunID {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.ParseRunID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustProjectID(t *testing.T, at time.Time) domain.ProjectID {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.ParseProjectID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSequenceID(t *testing.T, at time.Time) domain.SequenceID {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.ParseSequenceID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustTurnID(t *testing.T, at time.Time) domain.TurnID {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.ParseTurnID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
