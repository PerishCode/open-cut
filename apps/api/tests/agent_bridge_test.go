package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

type scriptedAgentAdapter struct {
	execute func(context.Context, service.AgentAdapterTurn, service.AgentProcessObserver) error
}

func (adapter scriptedAgentAdapter) ID() string      { return application.AgentBridgeAdapterCodexV1 }
func (adapter scriptedAgentAdapter) Version() string { return "codex-test@1" }
func (adapter scriptedAgentAdapter) Check(context.Context) error {
	return nil
}
func (adapter scriptedAgentAdapter) Execute(
	ctx context.Context,
	turn service.AgentAdapterTurn,
	observer service.AgentProcessObserver,
) error {
	return adapter.execute(ctx, turn, observer)
}

type recordingPresentationPublisher struct {
	mu     sync.Mutex
	events []service.AgentPresentationEvent
}

func (publisher *recordingPresentationPublisher) PublishAgentPresentation(
	_ domain.RunID,
	_ domain.TurnID,
	event service.AgentPresentationEvent,
) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.events = append(publisher.events, event)
	return nil
}

func (publisher *recordingPresentationPublisher) SubscribeAgentPresentation(
	runID domain.RunID,
	turnID domain.TurnID,
) (*service.AgentPresentationSubscription, error) {
	return nil, service.ErrAgentPresentationUnavailable
}

func TestAgentBridgeRuntimePersistsOnlySafeConversationAndPausesAfterCompletion(t *testing.T) {
	store, _, projectID := newSQLiteAgentBridgeProject(t)
	defer store.Close()
	reads, err := application.NewProjectReads(store)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := reads.Show(creatorContext(t), projectID)
	if err != nil {
		t.Fatal(err)
	}
	start, _ := domain.NewRationalTime(0, 1)
	duration, _ := domain.NewRationalTime(60, 1)
	visibleRange, _ := domain.NewTimeRange(start, duration)
	attachments := []application.AgentContextAttachment{{
		Kind: application.AgentContextSequenceRange,
		Range: &application.AgentContextSequenceRangeRef{
			SequenceID: overview.Project.MainSequenceID, Revision: overview.MainSequenceRevision, Range: visibleRange,
		},
	}}
	bridges := newAgentBridgesForTest(t, store)
	publisher := &recordingPresentationPublisher{}
	adapter := scriptedAgentAdapter{execute: func(
		ctx context.Context,
		turn service.AgentAdapterTurn,
		observer service.AgentProcessObserver,
	) error {
		if !strings.Contains(turn.Prompt, "stable recursive CLI") || !strings.Contains(turn.Prompt, "narrowest operation") ||
			!strings.Contains(turn.Prompt, "last-resort safety net") || !strings.Contains(turn.Prompt, "sequence-range") ||
			!strings.Contains(turn.RecoveryPrompt, "Write a concise opening") || turn.NativeSessionID != "" {
			t.Fatalf("turn=%+v", turn)
		}
		if err := observer.ObserveNativeSession(ctx, "private-codex-thread"); err != nil {
			return err
		}
		if err := observer.ObserveAgentPresentation(ctx, service.AgentPresentationEvent{Kind: service.AgentPresentationToolStarted, Tool: service.AgentPresentationCommand}); err != nil {
			return err
		}
		return observer.ObserveAgentMessage(ctx, "Drafted the opening through the product CLI.")
	}}
	runtime := newAgentBridgeRuntimeForTest(t, bridges, store, adapter, publisher)
	requestID, _ := domain.ParseRequestID("gesture:agent:begin:1")
	result, err := runtime.Begin(creatorContext(t), projectID, application.AgentBridgeBeginInput{
		RequestID: requestID, Message: "Write a concise opening",
		SequenceID: &overview.Project.MainSequenceID, Attachments: attachments,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAgentTurnCheckpoint(t, store, projectID, overview.Project.Revision, result.Run.CurrentTurn.ID)
	waitForAgentBridge(t, runtime, projectID, result.Run.ID, func(run application.AgentBridgeRun) bool {
		return run.Status == application.AgentRunPaused && run.CurrentTurn.Status == application.AgentTurnCompleted
	})
	page, err := runtime.Conversation(creatorContext(t), projectID, result.Run.ID, application.AgentConversationListInput{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 2 || page.Messages[0].Role != application.AgentConversationCreator ||
		len(page.Messages[0].Attachments) != 1 || page.Messages[0].Attachments[0].Kind != application.AgentContextSequenceRange ||
		page.Messages[1].Role != application.AgentConversationAgent ||
		strings.Contains(page.Messages[1].Text, "private-codex-thread") {
		t.Fatalf("conversation=%+v", page)
	}
	if _, err := store.ShowAgentRun(context.Background(), projectID, result.Run.ID); !errors.Is(err, application.ErrRunNotFound) {
		t.Fatalf("standalone ShowAgentRun error=%v", err)
	}
}

func TestAgentBridgeOwnedRunAllowsOnlyAgentShowAndExplicitComplete(t *testing.T) {
	store, _, projectID := newSQLiteAgentBridgeProject(t)
	defer store.Close()
	bridges := newAgentBridgesForTest(t, store)
	requestID, _ := domain.ParseRequestID("gesture:agent:bridge-boundary")
	begin, err := bridges.Begin(creatorContext(t), projectID, application.AgentBridgeBeginInput{
		RequestID: requestID, Message: "Finish the opening and explicitly complete the Run",
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 16, 5, 0, 0, 0, time.UTC)
	agentValue, _ := domain.GenerateUUIDv7(now)
	agentID, _ := domain.ParseAgentID(agentValue)
	grantID, _ := domain.GenerateUUIDv7(now.Add(time.Millisecond))
	grant, err := store.EnsurePendingCLIGrant(context.Background(), application.PendingCLIGrant{
		ID: grantID, InstallationID: "installation-agent-bridge-boundary", AgentID: agentID,
		PublicKey: "agent-bridge-boundary-key", Fingerprint: "sha256:" + strings.Repeat("c", 64),
		Scopes: []string{"run:read", "run:write"}, CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err = store.DecideCLIGrant(context.Background(), grant.ID, true, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	eventValue, _ := domain.GenerateUUIDv7(now.Add(2 * time.Second))
	eventID, _ := domain.ParseActivityEventID(eventValue)
	if err := store.BindAuthorizedAgentRun(
		context.Background(), begin.Run.ID, begin.Run.CurrentTurn.ID, agentID,
		grant.ID, grant.Revision, eventID, now.Add(2*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	runs, err := application.NewAgentRuns(
		store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(func() time.Time { return now.Add(3 * time.Second) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	agentCtx, err := application.ContextWithAuthority(context.Background(), application.Authority{
		Surface: application.AuthorityProductCLI, InstallationID: "installation-agent-bridge-boundary",
		GrantID: grant.ID, Actor: domain.AgentActor(agentID), Invocation: testAgentInvocation(),
	})
	if err != nil {
		t.Fatal(err)
	}
	shown, err := runs.Show(agentCtx, projectID, begin.Run.ID)
	if err != nil || shown.Run.CurrentTurn.ID != begin.Run.CurrentTurn.ID {
		t.Fatalf("shown=%+v err=%v", shown, err)
	}
	encoded, err := json.Marshal(shown)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"adapter", "agentVersion", "promptVersion", "nativeSessionId"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("run show leaked %q: %s", private, encoded)
		}
	}
	evidenceValue, _ := domain.GenerateUUIDv7(now.Add(2500 * time.Millisecond))
	evidenceID, _ := domain.ParseCommandReceiptID(evidenceValue)
	receiptDigest := domain.Digest("sha256:" + strings.Repeat("d", 64))
	evidenceCtx, err := application.ContextWithAuthority(context.Background(), application.Authority{
		Surface: application.AuthorityProductCLI, InstallationID: "installation-agent-bridge-boundary",
		GrantID: grant.ID, Actor: domain.AgentActor(agentID),
		Invocation: &application.CommandInvocation{
			ID: evidenceID, Command: "run evidence fixture", Fingerprint: receiptDigest,
			Class: application.CommandReceiptEvidence, InputDigest: receiptDigest,
			Context: application.InvocationContext{
				ProjectID: &projectID, RunID: &begin.Run.ID, TurnID: &begin.Run.CurrentTurn.ID,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.RecordEvidence(evidenceCtx, projectID, application.CommandEvidenceResult{
		Status: application.CommandReceiptSucceeded, Result: shown,
		ResultRefs: []application.CommandReceiptRef{{Kind: "run", ID: begin.Run.ID.String()}},
	}); err != nil {
		t.Fatal(err)
	}
	resumeID, _ := domain.ParseRequestID("agent:bridge:resume:denied")
	if _, err := runs.Resume(agentCtx, projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, application.RunResumeInput{
		RequestID: resumeID, ExpectedGeneration: begin.Run.CurrentTurn.Generation,
	}); !errors.Is(err, application.ErrRunBridgeManaged) {
		t.Fatalf("bridge resume error=%v", err)
	}
	failureValue, _ := domain.GenerateUUIDv7(now.Add(2600 * time.Millisecond))
	failureID, _ := domain.ParseCommandReceiptID(failureValue)
	failureRequestID, _ := domain.ParseRequestID("agent:bridge:resume:http-conflict")
	failureAuthority := application.Authority{
		Surface: application.AuthorityProductCLI, InstallationID: "installation-agent-bridge-boundary",
		GrantID: grant.ID, Actor: domain.AgentActor(agentID),
		Invocation: &application.CommandInvocation{
			ID: failureID, Command: "run resume", Fingerprint: receiptDigest,
			Class: application.CommandReceiptOutcome, InputDigest: receiptDigest, RequestID: &failureRequestID,
			Context: application.InvocationContext{
				ProjectID: &projectID, RunID: &begin.Run.ID, TurnID: &begin.Run.CurrentTurn.ID,
			},
		},
	}
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, nil, nil, nil, runs,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fixedAuthorizer{authority: failureAuthority},
	)
	server := httptest.NewServer(mux)
	defer server.Close()
	conflict := postJSON(t, server,
		"/v1/projects/"+projectID.String()+"/runs/"+begin.Run.ID.String()+"/turns/"+
			begin.Run.CurrentTurn.ID.String()+"/resume",
		command.RunResumeInput{RequestID: failureRequestID, ExpectedGeneration: begin.Run.CurrentTurn.Generation}, "",
	)
	if conflict.Code != http.StatusConflict ||
		conflict.Header().Get(command.StatusHeader) != string(command.StatusConflict) {
		t.Fatalf("resume conflict=%d headers=%v body=%s", conflict.Code, conflict.Header(), conflict.Body.String())
	}
	failureReceipts, err := runs.Receipts(
		creatorContext(t), projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, 0, 50,
	)
	if err != nil || len(failureReceipts.Receipts) != 2 ||
		failureReceipts.Receipts[1].ID != failureID ||
		failureReceipts.Receipts[1].Class != application.CommandReceiptOutcome ||
		failureReceipts.Receipts[1].Status != application.CommandReceiptConflict {
		t.Fatalf("failure receipts=%+v err=%v", failureReceipts, err)
	}
	replayValue, _ := domain.GenerateUUIDv7(now.Add(2700 * time.Millisecond))
	replayID, _ := domain.ParseCommandReceiptID(replayValue)
	failureAuthority.Invocation.ID = replayID
	replayCtx, err := application.ContextWithAuthority(context.Background(), failureAuthority)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runs.RecordBusinessResult(replayCtx, projectID, application.CommandBusinessResult{
		Status: application.CommandReceiptConflict,
		Result: struct {
			Status string `json:"status"`
		}{Status: "conflict"},
	})
	if err != nil || replayed.ID != failureID {
		t.Fatalf("replayed receipt=%+v err=%v", replayed, err)
	}
	replayMux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, nil, nil, nil, runs,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fixedAuthorizer{authority: failureAuthority},
	)
	replayServer := httptest.NewServer(replayMux)
	defer replayServer.Close()
	replayResponse := postJSON(t, replayServer,
		"/v1/projects/"+projectID.String()+"/runs/"+begin.Run.ID.String()+"/turns/"+
			begin.Run.CurrentTurn.ID.String()+"/resume",
		command.RunResumeInput{RequestID: failureRequestID, ExpectedGeneration: begin.Run.CurrentTurn.Generation}, "",
	)
	if replayResponse.Code != http.StatusConflict ||
		replayResponse.Header().Get(command.StatusHeader) != string(command.StatusConflict) ||
		replayResponse.Header().Get("X-Open-Cut-Internal-Receipt-Replay") != "" {
		t.Fatalf("replayed HTTP result=%d headers=%v body=%s", replayResponse.Code, replayResponse.Header(), replayResponse.Body.String())
	}
	replayedReceipts, err := runs.Receipts(
		creatorContext(t), projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, 0, 50,
	)
	if err != nil || len(replayedReceipts.Receipts) != 2 {
		t.Fatalf("HTTP replay receipts=%+v err=%v", replayedReceipts, err)
	}
	mismatchValue, _ := domain.GenerateUUIDv7(now.Add(2710 * time.Millisecond))
	mismatchID, _ := domain.ParseCommandReceiptID(mismatchValue)
	mismatchAuthority := failureAuthority
	mismatchInvocation := *failureAuthority.Invocation
	mismatchInvocation.ID = mismatchID
	mismatchInvocation.InputDigest = domain.Digest("sha256:" + strings.Repeat("f", 64))
	mismatchAuthority.Invocation = &mismatchInvocation
	mismatchMux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, nil, nil, nil, runs,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fixedAuthorizer{authority: mismatchAuthority},
	)
	mismatchServer := httptest.NewServer(mismatchMux)
	defer mismatchServer.Close()
	mismatchResponse := postJSON(t, mismatchServer,
		"/v1/projects/"+projectID.String()+"/runs/"+begin.Run.ID.String()+"/turns/"+
			begin.Run.CurrentTurn.ID.String()+"/resume",
		command.RunResumeInput{RequestID: failureRequestID, ExpectedGeneration: begin.Run.CurrentTurn.Generation}, "",
	)
	if mismatchResponse.Code != http.StatusUnprocessableEntity ||
		mismatchResponse.Header().Get(command.StatusHeader) != string(command.StatusInvalid) ||
		mismatchResponse.Header().Get("X-Open-Cut-Internal-Receipt-Replay") != "" {
		t.Fatalf("mismatched replay=%d headers=%v body=%s", mismatchResponse.Code, mismatchResponse.Header(), mismatchResponse.Body.String())
	}
	staleValue, _ := domain.GenerateUUIDv7(now.Add(2725 * time.Millisecond))
	staleID, _ := domain.ParseCommandReceiptID(staleValue)
	staleRequestID, _ := domain.ParseRequestID("agent:bridge:complete:stale")
	staleAuthority := failureAuthority
	staleAuthority.Invocation = &application.CommandInvocation{
		ID: staleID, Command: "run complete", Fingerprint: receiptDigest,
		Class: application.CommandReceiptOutcome, InputDigest: receiptDigest, RequestID: &staleRequestID,
		Context: application.InvocationContext{
			ProjectID: &projectID, RunID: &begin.Run.ID, TurnID: &begin.Run.CurrentTurn.ID,
		},
	}
	staleMux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, nil, nil, nil, runs,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fixedAuthorizer{authority: staleAuthority},
	)
	staleServer := httptest.NewServer(staleMux)
	defer staleServer.Close()
	staleGeneration, _ := domain.NewRevision(begin.Run.CurrentTurn.Generation.Value() + 1)
	staleResponse := postJSON(t, staleServer,
		"/v1/projects/"+projectID.String()+"/runs/"+begin.Run.ID.String()+"/turns/"+
			begin.Run.CurrentTurn.ID.String()+"/complete",
		command.RunCompleteInput{RequestID: staleRequestID, ExpectedGeneration: staleGeneration}, "",
	)
	if staleResponse.Code != http.StatusConflict ||
		staleResponse.Header().Get(command.StatusHeader) != string(command.StatusStaleTurn) {
		t.Fatalf("stale HTTP result=%d headers=%v body=%s", staleResponse.Code, staleResponse.Header(), staleResponse.Body.String())
	}
	afterStale, err := runs.Receipts(
		creatorContext(t), projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, 0, 50,
	)
	if err != nil || len(afterStale.Receipts) != 2 {
		t.Fatalf("stale receipt boundary=%+v err=%v", afterStale, err)
	}
	malformedRequest, err := http.NewRequest(
		http.MethodPost,
		staleServer.URL+"/v1/projects/"+projectID.String()+"/runs/"+begin.Run.ID.String()+"/turns/"+
			begin.Run.CurrentTurn.ID.String()+"/complete",
		strings.NewReader("{"),
	)
	if err != nil {
		t.Fatal(err)
	}
	malformedRequest.Header.Set("Content-Type", "application/json")
	malformedResponse, err := staleServer.Client().Do(malformedRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer malformedResponse.Body.Close()
	if malformedResponse.StatusCode != http.StatusUnprocessableEntity ||
		malformedResponse.Header.Get(command.StatusHeader) != "" {
		t.Fatalf("malformed pre-boundary=%d headers=%v", malformedResponse.StatusCode, malformedResponse.Header)
	}
	afterMalformed, err := runs.Receipts(
		creatorContext(t), projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, 0, 50,
	)
	if err != nil || len(afterMalformed.Receipts) != 2 {
		t.Fatalf("malformed receipt boundary=%+v err=%v", afterMalformed, err)
	}
	cancelID, _ := domain.ParseRequestID("agent:bridge:cancel:denied")
	if _, err := runs.Cancel(agentCtx, projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, application.RunCancelInput{
		RequestID: cancelID, ExpectedGeneration: begin.Run.CurrentTurn.Generation,
	}); !errors.Is(err, application.ErrRunBridgeManaged) {
		t.Fatalf("bridge cancel error=%v", err)
	}
	completeID, _ := domain.ParseRequestID("agent:bridge:complete:allowed")
	outcomeValue, _ := domain.GenerateUUIDv7(now.Add(2750 * time.Millisecond))
	outcomeID, _ := domain.ParseCommandReceiptID(outcomeValue)
	outcomeCtx, err := application.ContextWithAuthority(context.Background(), application.Authority{
		Surface: application.AuthorityProductCLI, InstallationID: "installation-agent-bridge-boundary",
		GrantID: grant.ID, Actor: domain.AgentActor(agentID),
		Invocation: &application.CommandInvocation{
			ID: outcomeID, Command: "run complete", Fingerprint: receiptDigest,
			Class: application.CommandReceiptOutcome, InputDigest: receiptDigest, RequestID: &completeID,
			Context: application.InvocationContext{
				ProjectID: &projectID, RunID: &begin.Run.ID, TurnID: &begin.Run.CurrentTurn.ID,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := runs.Complete(outcomeCtx, projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, application.RunCompleteInput{
		RequestID: completeID, ExpectedGeneration: begin.Run.CurrentTurn.Generation,
	})
	if err != nil || completed.Run.Status != application.AgentRunCompleted {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	receipts, err := runs.Receipts(
		creatorContext(t), projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, 0, 50,
	)
	if err != nil || len(receipts.Receipts) != 3 || receipts.Receipts[0].Class != application.CommandReceiptEvidence ||
		receipts.Receipts[1].Status != application.CommandReceiptConflict ||
		receipts.Receipts[2].Class != application.CommandReceiptOutcome ||
		receipts.Receipts[2].ActivityCursor == nil || receipts.Receipts[2].RequestID == nil ||
		*receipts.Receipts[2].RequestID != completeID {
		t.Fatalf("receipts=%+v err=%v", receipts, err)
	}
	if _, err := bridges.AppendAgentMessage(
		context.Background(), projectID, begin.Run.ID, begin.Run.CurrentTurn.ID, "The opening is complete.",
	); err != nil {
		t.Fatalf("append final Agent presentation: %v", err)
	}
	if _, err := bridges.AppendContextRebuiltNotice(
		context.Background(), projectID, begin.Run.ID, begin.Run.CurrentTurn.ID,
	); !errors.Is(err, application.ErrAgentBridgeStaleTurn) {
		t.Fatalf("terminal context notice error=%v", err)
	}
	if err := store.FinishAgentBridgeTurn(context.Background(), application.AgentBridgeRuntimeRecord{
		ProjectID: projectID, RunID: begin.Run.ID, TurnID: begin.Run.CurrentTurn.ID,
		Outcome: application.AgentBridgeRuntimeCompleted, OccurredAt: now.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("finish completed bridge turn: %v", err)
	}
}

func TestAgentBridgeFreshRecoveryKeepsSafeConversationAndDurableResetNotice(t *testing.T) {
	store, _, projectID := newSQLiteAgentBridgeProject(t)
	defer store.Close()
	bridges := newAgentBridgesForTest(t, store)
	var mu sync.Mutex
	invocations := make([]service.AgentAdapterTurn, 0, 2)
	adapter := scriptedAgentAdapter{execute: func(
		ctx context.Context,
		turn service.AgentAdapterTurn,
		observer service.AgentProcessObserver,
	) error {
		mu.Lock()
		invocations = append(invocations, turn)
		index := len(invocations)
		mu.Unlock()
		if index == 1 {
			return observer.ObserveAgentMessage(ctx, "Use the second title treatment from my proposal.")
		}
		if _, err := bridges.AppendContextRebuiltNotice(ctx, turn.ProjectID, turn.RunID, turn.TurnID); err != nil {
			return err
		}
		return observer.ObserveAgentMessage(ctx, "Continued from the safe rebuilt context.")
	}}
	runtime := newAgentBridgeRuntimeForTest(t, bridges, store, adapter, &recordingPresentationPublisher{})
	beginID, _ := domain.ParseRequestID("gesture:agent:recovery:begin")
	begin, err := runtime.Begin(creatorContext(t), projectID, application.AgentBridgeBeginInput{
		RequestID: beginID, Message: "Propose two concise title treatments",
	})
	if err != nil {
		t.Fatal(err)
	}
	first := waitForAgentBridge(t, runtime, projectID, begin.Run.ID, func(run application.AgentBridgeRun) bool {
		return run.Status == application.AgentRunPaused
	})
	continueID, _ := domain.ParseRequestID("gesture:agent:recovery:continue")
	if _, err := runtime.Continue(creatorContext(t), projectID, begin.Run.ID, application.AgentBridgeContinueInput{
		RequestID: continueID, ExpectedGeneration: first.CurrentTurn.Generation,
		Message: "Apply the second option",
	}); err != nil {
		t.Fatal(err)
	}
	waitForAgentBridge(t, runtime, projectID, begin.Run.ID, func(run application.AgentBridgeRun) bool {
		return run.Status == application.AgentRunPaused && run.CurrentTurn.Generation.Value() == 2
	})
	turns, err := runtime.Turns(creatorContext(t), projectID, begin.Run.ID, 0, 50)
	if err != nil || len(turns.Turns) != 2 || turns.Turns[0].Generation.Value() != 2 ||
		turns.Turns[1].Generation.Value() != 1 || turns.NextBefore != nil {
		t.Fatalf("turns=%+v err=%v", turns, err)
	}
	mu.Lock()
	if len(invocations) != 2 || strings.Contains(invocations[1].Prompt, "second title treatment") ||
		!strings.Contains(invocations[1].Prompt, "Apply the second option") ||
		!strings.Contains(invocations[1].RecoveryPrompt, `"role":"agent"`) ||
		!strings.Contains(invocations[1].RecoveryPrompt, "second title treatment") {
		t.Fatalf("invocations=%+v", invocations)
	}
	mu.Unlock()
	page, err := runtime.Conversation(
		creatorContext(t), projectID, begin.Run.ID, application.AgentConversationListInput{Limit: 20},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 5 || page.Messages[3].Role != application.AgentConversationNotice ||
		page.Messages[3].Text != application.AgentConversationContextRebuilt {
		t.Fatalf("conversation=%+v", page.Messages)
	}
}

func TestAgentBridgeInterruptCommitsDurableStateBeforeProcessCancellation(t *testing.T) {
	store, _, projectID := newSQLiteAgentBridgeProject(t)
	defer store.Close()
	bridges := newAgentBridgesForTest(t, store)
	started := make(chan struct{})
	stopped := make(chan struct{})
	adapter := scriptedAgentAdapter{execute: func(
		ctx context.Context,
		_ service.AgentAdapterTurn,
		_ service.AgentProcessObserver,
	) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}}
	runtime := newAgentBridgeRuntimeForTest(t, bridges, store, adapter, &recordingPresentationPublisher{})
	requestID, _ := domain.ParseRequestID("gesture:agent:begin:interrupt")
	result, err := runtime.Begin(creatorContext(t), projectID, application.AgentBridgeBeginInput{
		RequestID: requestID, Message: "Wait while I inspect the draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("adapter did not start")
	}
	interruptID, _ := domain.ParseRequestID("gesture:agent:interrupt:1")
	interrupted, err := runtime.Interrupt(creatorContext(t), projectID, result.Run.ID, result.Run.CurrentTurn.ID, application.AgentBridgeTransitionInput{
		RequestID: interruptID, ExpectedGeneration: result.Run.CurrentTurn.Generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Run.Status != application.AgentRunPaused ||
		interrupted.Run.CurrentTurn.Status != application.AgentTurnCancelled ||
		interrupted.Run.WaitingReason != "creator-interrupted" {
		t.Fatalf("interrupted=%+v", interrupted.Run)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("adapter process was not cancelled")
	}
	shown, err := runtime.Show(creatorContext(t), projectID, result.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Status != application.AgentRunPaused || shown.CurrentTurn.Status != application.AgentTurnCancelled {
		t.Fatalf("shown=%+v", shown)
	}
}

func TestAgentBridgePairingApprovalDoesNotBindUntilExactGrantRevisionAuthorizes(t *testing.T) {
	store, _, projectID := newSQLiteAgentBridgeProject(t)
	defer store.Close()
	bridges := newAgentBridgesForTest(t, store)
	beginID, _ := domain.ParseRequestID("gesture:agent:begin:pairing")
	result, err := bridges.Begin(creatorContext(t), projectID, application.AgentBridgeBeginInput{
		RequestID: beginID, Message: "Create a title card",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	agentValue, _ := domain.GenerateUUIDv7(now)
	agentID, _ := domain.ParseAgentID(agentValue)
	grantID, _ := domain.GenerateUUIDv7(now.Add(time.Millisecond))
	grant, err := store.EnsurePendingCLIGrant(context.Background(), application.PendingCLIGrant{
		ID: grantID, InstallationID: "installation-agent-bridge", AgentID: agentID,
		PublicKey: "agent-bridge-public-key", Fingerprint: "sha256:" + strings.Repeat("a", 64),
		Scopes: []string{"project:read"}, CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AssociateAgentRunPairing(
		context.Background(), result.Run.ID, result.Run.CurrentTurn.ID, grant.ID, grant.Revision, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideCLIGrant(context.Background(), grant.ID, true, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	approvedOnly, err := bridges.Show(creatorContext(t), projectID, result.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approvedOnly.AgentID != nil || approvedOnly.Status != application.AgentRunAuthorizing {
		t.Fatalf("approval unexpectedly bound Run: %+v", approvedOnly)
	}
	eventValue, _ := domain.GenerateUUIDv7(now.Add(2 * time.Second))
	eventID, _ := domain.ParseActivityEventID(eventValue)
	nextRevision, _ := grant.Revision.Next()
	if err := store.BindAuthorizedAgentRun(
		context.Background(), result.Run.ID, result.Run.CurrentTurn.ID, agentID,
		grant.ID, nextRevision, eventID, now.Add(2*time.Second),
	); !errors.Is(err, application.ErrAgentBridgeBindingDenied) {
		t.Fatalf("wrong grant revision error=%v", err)
	}
	if err := store.BindAuthorizedAgentRun(
		context.Background(), result.Run.ID, result.Run.CurrentTurn.ID, agentID,
		grant.ID, grant.Revision, eventID, now.Add(2*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	bound, err := bridges.Show(creatorContext(t), projectID, result.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bound.AgentID == nil || *bound.AgentID != agentID || bound.Status != application.AgentRunActive {
		t.Fatalf("bound=%+v", bound)
	}
}

func TestAgentBridgeHTTPIsCreatorOnlyAndNeverExposesAdapterInternals(t *testing.T) {
	store, _, projectID := newSQLiteAgentBridgeProject(t)
	defer store.Close()
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	bridges := newAgentBridgesForTest(t, store)
	hub := service.NewAgentPresentationHub()
	adapter := scriptedAgentAdapter{execute: func(
		ctx context.Context,
		_ service.AgentAdapterTurn,
		observer service.AgentProcessObserver,
	) error {
		if err := observer.ObserveNativeSession(ctx, "never-public"); err != nil {
			return err
		}
		return observer.ObserveAgentMessage(ctx, "The first pass is ready.")
	}}
	runtime := newAgentBridgeRuntimeForTest(t, bridges, store, adapter, hub)
	mux, api := controller.NewRouterWithAgentBridge(
		service.NewHealth(repository.StaticHealth{}), nil, nil,
		projects, reads, activity, runs, edits, editReads, media, assetReads, sourceAccess,
		nil, nil, nil, nil, nil, runtime, creatorAuthorizer{}, nil,
	)
	server := httptest.NewServer(mux)
	defer server.Close()
	availability := httptest.NewRecorder()
	mux.ServeHTTP(availability, httptest.NewRequest(http.MethodGet, server.URL+"/v1/agent/availability", nil))
	if availability.Code != http.StatusOK || !strings.Contains(availability.Body.String(), `"state":"available"`) ||
		!strings.Contains(availability.Body.String(), `"version":"codex-test@1"`) ||
		strings.Contains(availability.Body.String(), "never-public") {
		t.Fatalf("availability=%d body=%s", availability.Code, availability.Body.String())
	}
	requestID, _ := domain.ParseRequestID("gesture:http:agent:begin")
	response := postJSON(t, server, "/v1/projects/"+projectID.String()+"/agent/runs", application.AgentBridgeBeginInput{
		RequestID: requestID, Message: "Make a first pass", Attachments: []application.AgentContextAttachment{},
	}, "")
	if response.Code != http.StatusOK {
		t.Fatalf("begin=%d body=%s", response.Code, response.Body.String())
	}
	encoded := response.Body.String()
	if strings.Contains(encoded, "never-public") || strings.Contains(encoded, "nativeSession") ||
		strings.Contains(encoded, "codex-cli-v1") || strings.Contains(encoded, "adapter") {
		t.Fatalf("begin leaked adapter internals: %s", encoded)
	}
	var result application.AgentBridgeResult
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatal(err)
	}
	listRequest := httptest.NewRequest(
		http.MethodGet, server.URL+"/v1/projects/"+projectID.String()+"/agent/runs?limit=10", nil,
	)
	listed := httptest.NewRecorder()
	mux.ServeHTTP(listed, listRequest)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), result.Run.ID.String()) {
		t.Fatalf("list=%d body=%s", listed.Code, listed.Body.String())
	}
	turnsRequest := httptest.NewRequest(
		http.MethodGet, server.URL+"/v1/projects/"+projectID.String()+"/agent/runs/"+result.Run.ID.String()+
			"/turns?limit=50", nil,
	)
	turnsResponse := httptest.NewRecorder()
	mux.ServeHTTP(turnsResponse, turnsRequest)
	if turnsResponse.Code != http.StatusOK || !strings.Contains(turnsResponse.Body.String(), result.Run.CurrentTurn.ID.String()) ||
		!strings.Contains(turnsResponse.Body.String(), `"turns":[`) {
		t.Fatalf("turns=%d body=%s", turnsResponse.Code, turnsResponse.Body.String())
	}
	waitForAgentBridge(t, runtime, projectID, result.Run.ID, func(run application.AgentBridgeRun) bool {
		return run.Status == application.AgentRunPaused
	})
	request := httptest.NewRequest(http.MethodGet, server.URL+"/v1/projects/"+projectID.String()+
		"/agent/runs/"+result.Run.ID.String()+"/conversation", nil)
	conversation := httptest.NewRecorder()
	mux.ServeHTTP(conversation, request)
	if conversation.Code != http.StatusOK || strings.Contains(conversation.Body.String(), "never-public") {
		t.Fatalf("conversation=%d body=%s", conversation.Code, conversation.Body.String())
	}
	receiptRequest := httptest.NewRequest(http.MethodGet, server.URL+"/v1/projects/"+projectID.String()+
		"/agent/runs/"+result.Run.ID.String()+"/turns/"+result.Run.CurrentTurn.ID.String()+"/receipts?limit=50", nil)
	receiptResponse := httptest.NewRecorder()
	mux.ServeHTTP(receiptResponse, receiptRequest)
	if receiptResponse.Code != http.StatusOK || !strings.Contains(receiptResponse.Body.String(), `"receipts":[]`) {
		t.Fatalf("receipts=%d body=%s", receiptResponse.Code, receiptResponse.Body.String())
	}
	document, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), `"operationId":"begin-creator-agent-run"`) ||
		!strings.Contains(string(document), `"operationId":"list-creator-agent-turns"`) ||
		!strings.Contains(string(document), `"operationId":"list-creator-agent-turn-receipts"`) ||
		!strings.Contains(string(document), `"operationId":"show-local-agent-availability"`) ||
		!strings.Contains(string(document), `"x-open-cut-surface":"first-party-creator"`) {
		t.Fatalf("OpenAPI is missing Creator AgentBridge routes")
	}
}

func newSQLiteAgentBridgeProject(
	t *testing.T,
) (*repository.SQLiteProjects, *application.Projects, domain.ProjectID) {
	t.Helper()
	store, err := repository.OpenSQLiteProjects(context.Background(), filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	projects, err := application.NewProjects(
		store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	requestID, _ := domain.ParseRequestID("gesture:create:agent-bridge")
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: requestID, Name: "Agent bridge project",
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	return store, projects, created.Project.Project.ID
}

func newAgentBridgesForTest(t *testing.T, store application.AgentBridgeRepository) *application.AgentBridges {
	t.Helper()
	bridges, err := application.NewAgentBridges(
		store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	return bridges
}

func newAgentBridgeRuntimeForTest(
	t *testing.T,
	bridges *application.AgentBridges,
	store application.AgentBridgeRepository,
	adapter service.AgentTurnAdapter,
	publisher service.AgentPresentationBus,
) *service.AgentBridgeService {
	t.Helper()
	runtime, err := service.NewAgentBridgeService(
		context.Background(), bridges, store, adapter, publisher, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func waitForAgentBridge(
	t *testing.T,
	runtime *service.AgentBridgeService,
	projectID domain.ProjectID,
	runID domain.RunID,
	done func(application.AgentBridgeRun) bool,
) application.AgentBridgeRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := runtime.Show(creatorContext(t), projectID, runID)
		if err == nil && done(run) {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("AgentBridge did not reach expected state")
	return application.AgentBridgeRun{}
}
