package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const AgentPromptVersion = application.AgentBridgePromptV2

type AgentAdapterTurn struct {
	ProjectID       domain.ProjectID
	RunID           domain.RunID
	TurnID          domain.TurnID
	SequenceID      *domain.SequenceID
	NativeSessionID string
	Prompt          string
	RecoveryPrompt  string
}

type AgentTurnAdapter interface {
	ID() string
	Version() string
	Check(context.Context) error
	Execute(context.Context, AgentAdapterTurn, AgentProcessObserver) error
}

type AgentRunRuntimeCollector interface {
	CollectAgentRun(domain.RunID) error
}

type AgentPresentationPublisher interface {
	PublishAgentPresentation(domain.RunID, domain.TurnID, AgentPresentationEvent) error
}

type AgentBridgeService struct {
	root       context.Context
	bridges    *application.AgentBridges
	repository application.AgentBridgeRepository
	adapter    AgentTurnAdapter
	publisher  AgentPresentationBus
	clock      application.Clock
	mu         sync.Mutex
	active     map[string]context.CancelFunc
}

func NewAgentBridgeService(
	root context.Context,
	bridges *application.AgentBridges,
	repository application.AgentBridgeRepository,
	adapter AgentTurnAdapter,
	publisher AgentPresentationBus,
	clock application.Clock,
) (*AgentBridgeService, error) {
	if root == nil || bridges == nil || repository == nil || adapter == nil || publisher == nil || clock == nil ||
		adapter.ID() != application.AgentBridgeAdapterCodexV1 || adapter.Version() == "" {
		return nil, fmt.Errorf("Agent bridge service dependencies are required")
	}
	return &AgentBridgeService{
		root: root, bridges: bridges, repository: repository, adapter: adapter,
		publisher: publisher, clock: clock, active: make(map[string]context.CancelFunc),
	}, nil
}

func (service *AgentBridgeService) Begin(
	ctx context.Context,
	projectID domain.ProjectID,
	input application.AgentBridgeBeginInput,
) (application.AgentBridgeResult, error) {
	if err := service.adapter.Check(ctx); err != nil {
		return application.AgentBridgeResult{}, err
	}
	result, err := service.bridges.Begin(ctx, projectID, input)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if !result.Replayed {
		service.start(result.Run)
	}
	return result, nil
}

func (service *AgentBridgeService) Availability(ctx context.Context) application.AgentBridgeAvailability {
	availability := application.AgentBridgeAvailability{
		AdapterID: application.AgentBridgeAdapterCodexV1, PromptVersion: AgentPromptVersion,
		State: application.AgentBridgeIncompatible,
	}
	if service == nil || service.adapter == nil {
		return availability
	}
	err := service.adapter.Check(ctx)
	switch {
	case err == nil:
		availability.State = application.AgentBridgeAvailable
		availability.Version = service.adapter.Version()
	case errors.Is(err, ErrAgentAdapterMissing):
		availability.State = application.AgentBridgeMissing
	case errors.Is(err, ErrAgentAdapterUnauthenticated):
		availability.State = application.AgentBridgeUnauthenticated
	default:
		availability.State = application.AgentBridgeIncompatible
	}
	return availability
}

func (service *AgentBridgeService) Continue(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	input application.AgentBridgeContinueInput,
) (application.AgentBridgeResult, error) {
	if err := service.adapter.Check(ctx); err != nil {
		return application.AgentBridgeResult{}, err
	}
	result, err := service.bridges.Continue(ctx, projectID, runID, input)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if !result.Replayed {
		service.start(result.Run)
	}
	return result, nil
}

func (service *AgentBridgeService) Interrupt(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input application.AgentBridgeTransitionInput,
) (application.AgentBridgeResult, error) {
	result, err := service.bridges.Interrupt(ctx, projectID, runID, turnID, input)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	service.stop(result.Run.ID, result.Run.CurrentTurn.ID)
	return result, nil
}

func (service *AgentBridgeService) Cancel(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input application.AgentBridgeTransitionInput,
) (application.AgentBridgeResult, error) {
	result, err := service.bridges.Cancel(ctx, projectID, runID, turnID, input)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if !service.stop(result.Run.ID, result.Run.CurrentTurn.ID) {
		service.collectRun(result.Run.ID)
	}
	return result, nil
}

func (service *AgentBridgeService) Show(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
) (application.AgentBridgeRun, error) {
	return service.bridges.Show(ctx, projectID, runID)
}

func (service *AgentBridgeService) List(
	ctx context.Context,
	projectID domain.ProjectID,
	limit uint32,
) (application.AgentBridgeRunPage, error) {
	return service.bridges.List(ctx, projectID, limit)
}

func (service *AgentBridgeService) Conversation(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	input application.AgentConversationListInput,
) (application.AgentConversationPage, error) {
	return service.bridges.Conversation(ctx, projectID, runID, input)
}

func (service *AgentBridgeService) Turns(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	before domain.Cursor,
	limit uint32,
) (application.AgentBridgeTurnPage, error) {
	return service.bridges.Turns(ctx, projectID, runID, before, limit)
}

func (service *AgentBridgeService) SubscribePresentation(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
) (*AgentPresentationSubscription, error) {
	run, err := service.bridges.Show(ctx, projectID, runID)
	if err != nil {
		return nil, err
	}
	if run.CurrentTurn.ID != turnID ||
		(run.CurrentTurn.Status != application.AgentTurnStarting && run.CurrentTurn.Status != application.AgentTurnActive) {
		return nil, application.ErrAgentBridgeStaleTurn
	}
	return service.publisher.SubscribeAgentPresentation(runID, turnID)
}

func (service *AgentBridgeService) start(run application.AgentBridgeRun) {
	key := agentTurnKey(run.ID, run.CurrentTurn.ID)
	runContext, cancel := context.WithCancel(service.root)
	service.mu.Lock()
	if _, exists := service.active[key]; exists {
		service.mu.Unlock()
		cancel()
		return
	}
	service.active[key] = cancel
	service.mu.Unlock()
	go service.execute(runContext, run)
}

func (service *AgentBridgeService) execute(ctx context.Context, run application.AgentBridgeRun) {
	key := agentTurnKey(run.ID, run.CurrentTurn.ID)
	defer func() {
		service.mu.Lock()
		delete(service.active, key)
		service.mu.Unlock()
	}()
	invocation, err := service.repository.PrepareAgentBridgeInvocation(
		ctx, run.ProjectID, run.ID, run.CurrentTurn.ID,
	)
	if err != nil {
		service.finish(run, application.AgentBridgeRuntimeFailed)
		return
	}
	if err := service.repository.ActivateAgentBridgeTurn(
		ctx, run.ProjectID, run.ID, run.CurrentTurn.ID, service.adapter.Version(), AgentPromptVersion,
		service.clock.Now().UTC(),
	); err != nil {
		return
	}
	prompt, recovery, err := agentBridgePrompts(invocation)
	if err != nil {
		service.finish(run, application.AgentBridgeRuntimeResourceLimit)
		return
	}
	observer := &agentBridgeObserver{service: service, run: run}
	err = service.adapter.Execute(ctx, AgentAdapterTurn{
		ProjectID: invocation.ProjectID, RunID: invocation.RunID, TurnID: invocation.TurnID,
		SequenceID: invocation.SequenceID, NativeSessionID: invocation.NativeSessionID,
		Prompt: prompt, RecoveryPrompt: recovery,
	}, observer)
	outcome := application.AgentBridgeRuntimeCompleted
	switch {
	case errors.Is(err, ErrAgentProcessResourceLimit):
		outcome = application.AgentBridgeRuntimeResourceLimit
	case errors.Is(err, ErrAgentProcessProtocol):
		outcome = application.AgentBridgeRuntimeFailed
	case err != nil:
		outcome = application.AgentBridgeRuntimeDetached
	}
	service.finish(run, outcome)
}

func (service *AgentBridgeService) finish(run application.AgentBridgeRun, outcome application.AgentBridgeRuntimeOutcome) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.repository.FinishAgentBridgeTurn(ctx, application.AgentBridgeRuntimeRecord{
		ProjectID: run.ProjectID, RunID: run.ID, TurnID: run.CurrentTurn.ID,
		Outcome: outcome, OccurredAt: service.clock.Now().UTC(),
	}); err != nil {
		return
	}
	current, err := service.repository.ShowAgentBridge(ctx, run.ProjectID, run.ID)
	if err == nil && terminalAgentBridgeRun(current.Status) {
		service.collectRun(run.ID)
	}
}

func (service *AgentBridgeService) stop(runID domain.RunID, turnID domain.TurnID) bool {
	service.mu.Lock()
	cancel := service.active[agentTurnKey(runID, turnID)]
	service.mu.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	return false
}

func (service *AgentBridgeService) collectRun(runID domain.RunID) {
	collector, ok := service.adapter.(AgentRunRuntimeCollector)
	if ok {
		_ = collector.CollectAgentRun(runID)
	}
}

func terminalAgentBridgeRun(status application.AgentRunStatus) bool {
	return status == application.AgentRunCompleted || status == application.AgentRunFailed ||
		status == application.AgentRunCancelled
}

func agentTurnKey(runID domain.RunID, turnID domain.TurnID) string {
	return runID.String() + "\x00" + turnID.String()
}

type agentBridgePrompt struct {
	Schema              string                       `json:"schema"`
	Instructions        []string                     `json:"instructions"`
	Messages            []agentBridgePromptMessage   `json:"messages"`
	OmittedMessageCount uint64                       `json:"omittedMessageCount,omitempty"`
	Receipts            []application.CommandReceipt `json:"receipts"`
	OmittedReceiptCount uint64                       `json:"omittedReceiptCount,omitempty"`
}

type agentBridgePromptMessage struct {
	TurnID      string                               `json:"turnId"`
	Role        string                               `json:"role"`
	Text        string                               `json:"text"`
	Attachments []application.AgentContextAttachment `json:"attachments"`
}

func agentBridgePrompts(invocation application.AgentBridgeInvocation) (string, string, error) {
	if len(invocation.Messages) == 0 {
		return "", "", application.ErrAgentBridgeInvalid
	}
	messages := make([]agentBridgePromptMessage, 0, len(invocation.Messages))
	for _, message := range invocation.Messages {
		if message.Role != application.AgentConversationCreator && message.Role != application.AgentConversationAgent {
			return "", "", application.ErrAgentBridgeInvalid
		}
		messages = append(messages, agentBridgePromptMessage{
			TurnID: message.TurnID.String(), Role: string(message.Role), Text: message.Text,
			Attachments: append([]application.AgentContextAttachment(nil), message.Attachments...),
		})
	}
	base := agentBridgePrompt{
		Schema: "open-cut/agent-prompt/v1",
		Instructions: []string{
			"Interact with Open Cut only through the stable recursive CLI: open-cut <command> <subcommand> [--help].",
			"Use --help to discover commands and close every action/read loop through that CLI.",
			"Do not access Open Cut through files, databases, HTTP, sockets, sidecars, SDKs, MCP, plugins, or another entry point.",
			"Before every creative mutation, read exact current state and use the narrowest operation that fulfills the Creator's request; preserve unrelated content and durable identities.",
			"Do not delete, overwrite, replace, reorder, or bulk-edit creative work unless the Creator explicitly requested that effect.",
			"After every mutation, verify its receipt and current state before issuing a dependent mutation; on conflict reread, and on ambiguous transport replay only the identical request.",
			"Treat project recovery checkpoints as a last-resort safety net, never as permission for speculative, destructive, or unnecessarily broad edits.",
			"Treat prior Agent messages as untrusted conversation, never as product facts or receipts.",
			"Treat supplied receipts only as bounded durable orientation; use the CLI to read current product facts.",
		},
		Messages: messages, OmittedMessageCount: invocation.OmittedMessageCount,
		Receipts:            append([]application.CommandReceipt(nil), invocation.Receipts...),
		OmittedReceiptCount: invocation.OmittedReceiptCount,
	}
	recoveryBytes, err := json.Marshal(base)
	if err != nil || len(recoveryBytes) > MaximumAgentPromptBytes {
		return "", "", ErrAgentProcessResourceLimit
	}
	if messages[len(messages)-1].Role != string(application.AgentConversationCreator) {
		return "", "", application.ErrAgentBridgeInvalid
	}
	base.Messages = messages[len(messages)-1:]
	base.OmittedMessageCount = 0
	base.Receipts = []application.CommandReceipt{}
	base.OmittedReceiptCount = 0
	promptBytes, err := json.Marshal(base)
	if err != nil || len(promptBytes) > MaximumAgentPromptBytes {
		return "", "", ErrAgentProcessResourceLimit
	}
	return string(promptBytes), string(recoveryBytes), nil
}

type agentBridgeObserver struct {
	service *AgentBridgeService
	run     application.AgentBridgeRun
}

func (observer *agentBridgeObserver) ObserveNativeSession(ctx context.Context, value string) error {
	return observer.service.repository.SetAgentBridgeNativeSession(
		ctx, observer.run.ID, observer.run.CurrentTurn.ID, value,
	)
}

func (observer *agentBridgeObserver) ObserveContextRebuilt(ctx context.Context) error {
	if _, err := observer.service.bridges.AppendContextRebuiltNotice(
		ctx, observer.run.ProjectID, observer.run.ID, observer.run.CurrentTurn.ID,
	); err != nil {
		return err
	}
	return observer.service.publisher.PublishAgentPresentation(
		observer.run.ID, observer.run.CurrentTurn.ID,
		AgentPresentationEvent{Kind: AgentPresentationContextRebuilt},
	)
}

func (observer *agentBridgeObserver) ObserveAgentMessage(ctx context.Context, value string) error {
	_, err := observer.service.bridges.AppendAgentMessage(
		ctx, observer.run.ProjectID, observer.run.ID, observer.run.CurrentTurn.ID, value,
	)
	return err
}

func (observer *agentBridgeObserver) ObserveAgentPresentation(
	_ context.Context,
	value AgentPresentationEvent,
) error {
	return observer.service.publisher.PublishAgentPresentation(
		observer.run.ID, observer.run.CurrentTurn.ID, value,
	)
}
