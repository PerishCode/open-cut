package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
)

const (
	MaximumAgentJSONLLineBytes = 256 * 1024
	MaximumAgentPromptBytes    = 1024 * 1024
	MaximumAgentTurnDuration   = 30 * time.Minute
)

var (
	ErrAgentProcessInvalid       = errors.New("Agent process invocation is invalid")
	ErrAgentProcessProtocol      = errors.New("Agent process protocol is incompatible")
	ErrAgentProcessResourceLimit = errors.New("Agent process resource limit was exceeded")
	ErrAgentProcessFailed        = errors.New("Agent process failed")
)

type AgentProcessInvocation struct {
	Executable string
	Args       []string
	Directory  string
	Env        []string
	Prompt     string
	Timeout    time.Duration
}

type AgentProcessResult struct {
	NativeSessionID string
	MessageCount    uint32
}

type AgentPresentationKind string

const (
	AgentPresentationTurnStarted    AgentPresentationKind = "turn-started"
	AgentPresentationContextRebuilt AgentPresentationKind = "context-rebuilt"
	AgentPresentationToolStarted    AgentPresentationKind = "tool-started"
	AgentPresentationToolCompleted  AgentPresentationKind = "tool-completed"
	AgentPresentationMessage        AgentPresentationKind = "message-completed"
	AgentPresentationTurnCompleted  AgentPresentationKind = "turn-completed"
	AgentPresentationTurnFailed     AgentPresentationKind = "turn-failed"
)

type AgentPresentationTool string

const (
	AgentPresentationCommand    AgentPresentationTool = "command"
	AgentPresentationFileChange AgentPresentationTool = "file-change"
	AgentPresentationReasoning  AgentPresentationTool = "reasoning"
	AgentPresentationWebSearch  AgentPresentationTool = "web-search"
	AgentPresentationPlan       AgentPresentationTool = "plan"
)

type AgentPresentationEvent struct {
	Kind AgentPresentationKind
	Tool AgentPresentationTool
}

type AgentProcessObserver interface {
	ObserveNativeSession(context.Context, string) error
	ObserveContextRebuilt(context.Context) error
	ObserveAgentMessage(context.Context, string) error
	ObserveAgentPresentation(context.Context, AgentPresentationEvent) error
}

type AgentStreamDecoder interface {
	Consume(context.Context, []byte, AgentProcessObserver) error
	Finish() (AgentProcessResult, error)
}

type AgentProcessEngine struct {
	profile lifecycle.Profile
}

type AgentProcessRunner interface {
	Run(context.Context, AgentProcessInvocation, AgentStreamDecoder, AgentProcessObserver) (AgentProcessResult, error)
}

func NewAgentProcessEngine(profile lifecycle.Profile) (*AgentProcessEngine, error) {
	if profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
		profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness {
		return nil, ErrAgentProcessInvalid
	}
	return &AgentProcessEngine{profile: profile}, nil
}

func (engine *AgentProcessEngine) Run(
	ctx context.Context,
	invocation AgentProcessInvocation,
	decoder AgentStreamDecoder,
	observer AgentProcessObserver,
) (AgentProcessResult, error) {
	if engine == nil || decoder == nil || observer == nil || !validAgentProcessInvocation(invocation) {
		return AgentProcessResult{}, ErrAgentProcessInvalid
	}
	timeout := invocation.Timeout
	if timeout == 0 {
		timeout = MaximumAgentTurnDuration
	}
	runContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stdoutWriter := io.Pipe()
	process, err := lifecycle.Start(runContext, lifecycle.ProcessSpec{
		Executable: invocation.Executable,
		Args:       append([]string(nil), invocation.Args...),
		Directory:  invocation.Directory,
		Env:        append([]string(nil), invocation.Env...),
		Stdin:      strings.NewReader(invocation.Prompt),
		Stdout:     stdoutWriter,
		Stderr:     io.Discard,
		Profile:    engine.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	if err != nil {
		_ = stdout.Close()
		_ = stdoutWriter.Close()
		return AgentProcessResult{}, fmt.Errorf("%w: start", ErrAgentProcessFailed)
	}
	waited := make(chan error, 1)
	go func() {
		waitErr := process.Wait()
		_ = stdoutWriter.CloseWithError(waitErr)
		waited <- waitErr
	}()
	decodeErr := consumeAgentStream(runContext, stdout, decoder, observer)
	if decodeErr != nil {
		cancel()
		_ = stdout.CloseWithError(decodeErr)
	}
	waitErr := <-waited
	if ctx.Err() != nil {
		return AgentProcessResult{}, ctx.Err()
	}
	if errors.Is(runContext.Err(), context.DeadlineExceeded) {
		return AgentProcessResult{}, context.DeadlineExceeded
	}
	if decodeErr != nil {
		if errors.Is(decodeErr, ErrAgentProcessProtocol) || errors.Is(decodeErr, ErrAgentProcessResourceLimit) {
			return AgentProcessResult{}, decodeErr
		}
		return AgentProcessResult{}, fmt.Errorf("%w: decode", ErrAgentProcessFailed)
	}
	if waitErr != nil {
		return AgentProcessResult{}, fmt.Errorf("%w: exit", ErrAgentProcessFailed)
	}
	return decoder.Finish()
}

func consumeAgentStream(
	ctx context.Context,
	reader io.Reader,
	decoder AgentStreamDecoder,
	observer AgentProcessObserver,
) error {
	buffered := bufio.NewReaderSize(reader, MaximumAgentJSONLLineBytes+1)
	for {
		line, readErr := buffered.ReadSlice('\n')
		if errors.Is(readErr, bufio.ErrBufferFull) || len(line) > MaximumAgentJSONLLineBytes+1 {
			return ErrAgentProcessResourceLimit
		}
		if len(line) > 0 {
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) > MaximumAgentJSONLLineBytes {
				return ErrAgentProcessResourceLimit
			}
		}
		if len(line) == 0 {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
			return ErrAgentProcessProtocol
		}
		if err := decoder.Consume(ctx, line, observer); err != nil {
			return err
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func validAgentProcessInvocation(invocation AgentProcessInvocation) bool {
	if !filepath.IsAbs(invocation.Executable) || filepath.Clean(invocation.Executable) != invocation.Executable ||
		!filepath.IsAbs(invocation.Directory) || filepath.Clean(invocation.Directory) != invocation.Directory ||
		invocation.Env == nil || len(invocation.Prompt) == 0 || len([]byte(invocation.Prompt)) > MaximumAgentPromptBytes ||
		invocation.Timeout < 0 || invocation.Timeout > MaximumAgentTurnDuration {
		return false
	}
	for _, value := range invocation.Args {
		if strings.IndexByte(value, 0) >= 0 {
			return false
		}
	}
	for _, value := range invocation.Env {
		name, _, found := strings.Cut(value, "=")
		if !found || name == "" || strings.IndexByte(name, 0) >= 0 {
			return false
		}
	}
	return true
}
