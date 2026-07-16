package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	RunRequestSchema      = "open-cut/agent-run-request/v1"
	MaximumRunIntentBytes = 32_768
	runWaitPollInterval   = 100 * time.Millisecond
)

var (
	ErrRunNotFound      = errors.New("AgentRun was not found")
	ErrRunInvalid       = errors.New("AgentRun input is invalid")
	ErrRunTerminal      = errors.New("AgentRun is terminal")
	ErrRunBlocked       = errors.New("AgentRun has an unresolved blocker")
	ErrRunStaleTurn     = errors.New("AgentTurn generation is stale")
	ErrRunActorMismatch = errors.New("AgentRun actor does not match authority")
	ErrRunBridgeManaged = errors.New("AgentRun lifecycle is managed by AgentBridge")
	ErrRunRequestReused = errors.New("AgentRun request identity was reused")
)

type AgentRunStatus string

const (
	AgentRunAuthorizing AgentRunStatus = "authorizing"
	AgentRunActive      AgentRunStatus = "active"
	AgentRunWaiting     AgentRunStatus = "waiting"
	AgentRunPaused      AgentRunStatus = "paused"
	AgentRunCompleted   AgentRunStatus = "completed"
	AgentRunFailed      AgentRunStatus = "failed"
	AgentRunCancelled   AgentRunStatus = "cancelled"
)

type AgentTurnStatus string

const (
	AgentTurnStarting   AgentTurnStatus = "starting"
	AgentTurnActive     AgentTurnStatus = "active"
	AgentTurnDetached   AgentTurnStatus = "detached"
	AgentTurnCompleted  AgentTurnStatus = "completed"
	AgentTurnFailed     AgentTurnStatus = "failed"
	AgentTurnCancelled  AgentTurnStatus = "cancelled"
	AgentTurnSuperseded AgentTurnStatus = "superseded"
)

type AgentTurn struct {
	ID         domain.TurnID    `json:"id" format:"uuid"`
	RunID      domain.RunID     `json:"runId" format:"uuid"`
	ProjectID  domain.ProjectID `json:"projectId" format:"uuid"`
	Generation domain.Revision  `json:"generation" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Status     AgentTurnStatus  `json:"status" enum:"starting,active,detached,completed,failed,cancelled,superseded"`
	StartedAt  time.Time        `json:"startedAt"`
	EndedAt    *time.Time       `json:"endedAt,omitempty"`
}

type AgentRunDetail struct {
	ID                            domain.RunID     `json:"id" format:"uuid"`
	ProjectID                     domain.ProjectID `json:"projectId" format:"uuid"`
	Intent                        string           `json:"intent" maxLength:"32768"`
	Actor                         domain.ActorRef  `json:"actor"`
	Status                        AgentRunStatus   `json:"status" enum:"authorizing,active,waiting,paused,completed,failed,cancelled"`
	WaitingReason                 string           `json:"waitingReason,omitempty" maxLength:"128"`
	StartedProjectRevision        domain.Revision  `json:"startedProjectRevision" format:"uint64-decimal"`
	LatestObservedProjectRevision domain.Revision  `json:"latestObservedProjectRevision" format:"uint64-decimal"`
	CurrentTurn                   AgentTurn        `json:"currentTurn"`
	ActivityCursor                domain.Cursor    `json:"activityCursor" format:"uint64-decimal"`
	CreatedAt                     time.Time        `json:"createdAt"`
	UpdatedAt                     time.Time        `json:"updatedAt"`
	CompletedAt                   *time.Time       `json:"completedAt,omitempty"`
}

type RunBeginInput struct {
	RequestID domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Intent    string           `json:"intent" minLength:"1" maxLength:"4000"`
}

type RunShowInput struct{}

type RunWaitInput struct {
	After domain.Cursor `json:"after,omitempty" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
}

type RunResumeInput struct {
	RequestID          domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	ExpectedGeneration domain.Revision  `json:"expectedGeneration" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type RunCompleteInput = RunResumeInput
type RunCancelInput = RunResumeInput

type RunCommandResult struct {
	Run      AgentRunDetail `json:"run"`
	Replayed bool           `json:"replayed"`
}

type RunTransition string

const (
	RunTransitionResume   RunTransition = "resume"
	RunTransitionComplete RunTransition = "complete"
	RunTransitionCancel   RunTransition = "cancel"
)

type BeginAgentRunRecord struct {
	RunID           domain.RunID
	TurnID          domain.TurnID
	ProjectID       domain.ProjectID
	Actor           domain.ActorRef
	Intent          string
	RequestID       domain.RequestID
	InputDigest     domain.Digest
	InputCanonical  []byte
	ActivityEventID domain.ActivityEventID
	CreatedAt       time.Time
}

type TransitionAgentRunRecord struct {
	Transition         RunTransition
	ProjectID          domain.ProjectID
	RunID              domain.RunID
	ExpectedTurnID     domain.TurnID
	ExpectedGeneration domain.Revision
	NewTurnID          *domain.TurnID
	Actor              domain.ActorRef
	RequestID          domain.RequestID
	InputDigest        domain.Digest
	InputCanonical     []byte
	ActivityEventID    domain.ActivityEventID
	OccurredAt         time.Time
}

type AgentRunOutcome struct {
	Run      AgentRunDetail
	Replayed bool
}

type AgentRunRepository interface {
	BeginAgentRun(context.Context, BeginAgentRunRecord) (AgentRunOutcome, error)
	ShowAgentRun(context.Context, domain.ProjectID, domain.RunID) (AgentRunDetail, error)
	TransitionAgentRun(context.Context, TransitionAgentRunRecord) (AgentRunOutcome, error)
	CommandReceiptRepository
}

type AgentRuns struct {
	repository AgentRunRepository
	identities IdentityGenerator
	clock      Clock
}

func NewAgentRuns(repository AgentRunRepository, identities IdentityGenerator, clock Clock) (*AgentRuns, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("AgentRun application dependencies are required")
	}
	return &AgentRuns{repository: repository, identities: identities, clock: clock}, nil
}

func (runs *AgentRuns) Begin(
	ctx context.Context,
	projectID domain.ProjectID,
	input RunBeginInput,
) (RunCommandResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return RunCommandResult{}, err
	}
	if projectID.IsZero() || len(input.Intent) == 0 || len([]byte(input.Intent)) > MaximumRunIntentBytes {
		return RunCommandResult{}, ErrRunInvalid
	}
	requestID, err := domain.ParseRequestID(input.RequestID.String())
	if err != nil {
		return RunCommandResult{}, ErrRunInvalid
	}
	canonical, digest, err := runRequestDigest("run begin", authority.Actor, projectID, nil, nil, input)
	if err != nil {
		return RunCommandResult{}, err
	}
	now := runs.clock.Now().UTC()
	runID, turnID, eventID, err := runs.newRunIDs(ctx, now)
	if err != nil {
		return RunCommandResult{}, err
	}
	outcome, err := runs.repository.BeginAgentRun(ctx, BeginAgentRunRecord{
		RunID: runID, TurnID: turnID, ProjectID: projectID, Actor: authority.Actor,
		Intent: input.Intent, RequestID: requestID, InputDigest: digest, InputCanonical: canonical,
		ActivityEventID: eventID, CreatedAt: now,
	})
	if err != nil {
		return RunCommandResult{}, err
	}
	return RunCommandResult{Run: outcome.Run, Replayed: outcome.Replayed}, nil
}

func (runs *AgentRuns) Show(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
) (RunCommandResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return RunCommandResult{}, err
	}
	detail, err := runs.showAuthorized(ctx, authority, projectID, runID)
	if err != nil {
		return RunCommandResult{}, err
	}
	return RunCommandResult{Run: detail}, nil
}

func (runs *AgentRuns) Wait(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	input RunWaitInput,
) (RunCommandResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return RunCommandResult{}, err
	}
	if projectID.IsZero() || runID.IsZero() {
		return RunCommandResult{}, ErrRunInvalid
	}
	deadline := time.NewTimer(time.Duration(authority.EffectivePolicy().WaitMilliseconds) * time.Millisecond)
	defer deadline.Stop()
	poll := time.NewTicker(runWaitPollInterval)
	defer poll.Stop()
	for {
		detail, err := runs.showAuthorized(ctx, authority, projectID, runID)
		if err != nil {
			return RunCommandResult{}, err
		}
		if detail.ActivityCursor.Value() > input.After.Value() || terminalRunStatus(detail.Status) {
			return RunCommandResult{Run: detail}, nil
		}
		select {
		case <-ctx.Done():
			return RunCommandResult{}, ctx.Err()
		case <-deadline.C:
			return RunCommandResult{Run: detail}, nil
		case <-poll.C:
		}
	}
}

func (runs *AgentRuns) showAuthorized(
	ctx context.Context,
	authority Authority,
	projectID domain.ProjectID,
	runID domain.RunID,
) (AgentRunDetail, error) {
	detail, err := runs.repository.ShowAgentRun(ctx, projectID, runID)
	if err != nil {
		return AgentRunDetail{}, err
	}
	if detail.Actor.Kind != authority.Actor.Kind || detail.Actor.IDString() != authority.Actor.IDString() {
		return AgentRunDetail{}, ErrRunActorMismatch
	}
	return detail, nil
}

func terminalRunStatus(status AgentRunStatus) bool {
	return status == AgentRunCompleted || status == AgentRunFailed || status == AgentRunCancelled
}

func (runs *AgentRuns) Resume(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input RunResumeInput,
) (RunCommandResult, error) {
	return runs.transition(ctx, RunTransitionResume, projectID, runID, turnID, input)
}

func (runs *AgentRuns) Complete(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input RunCompleteInput,
) (RunCommandResult, error) {
	return runs.transition(ctx, RunTransitionComplete, projectID, runID, turnID, input)
}

func (runs *AgentRuns) Cancel(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input RunCancelInput,
) (RunCommandResult, error) {
	return runs.transition(ctx, RunTransitionCancel, projectID, runID, turnID, input)
}

func (runs *AgentRuns) transition(
	ctx context.Context,
	transition RunTransition,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input RunResumeInput,
) (RunCommandResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return RunCommandResult{}, err
	}
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() || input.ExpectedGeneration.Value() < 1 {
		return RunCommandResult{}, ErrRunInvalid
	}
	requestID, err := domain.ParseRequestID(input.RequestID.String())
	if err != nil {
		return RunCommandResult{}, ErrRunInvalid
	}
	commandName := "run " + string(transition)
	canonical, digest, err := runRequestDigest(commandName, authority.Actor, projectID, &runID, &turnID, input)
	if err != nil {
		return RunCommandResult{}, err
	}
	now := runs.clock.Now().UTC()
	eventID, err := runs.newActivityEventID(ctx, now)
	if err != nil {
		return RunCommandResult{}, err
	}
	var newTurnID *domain.TurnID
	if transition == RunTransitionResume {
		value, identityErr := runs.identities.NewID(ctx, now)
		if identityErr != nil {
			return RunCommandResult{}, identityErr
		}
		parsed, parseErr := domain.ParseTurnID(value)
		if parseErr != nil {
			return RunCommandResult{}, parseErr
		}
		newTurnID = &parsed
	}
	outcome, err := runs.repository.TransitionAgentRun(ctx, TransitionAgentRunRecord{
		Transition: transition, ProjectID: projectID, RunID: runID,
		ExpectedTurnID: turnID, ExpectedGeneration: input.ExpectedGeneration, NewTurnID: newTurnID,
		Actor: authority.Actor, RequestID: requestID, InputDigest: digest, InputCanonical: canonical,
		ActivityEventID: eventID, OccurredAt: now,
	})
	if err != nil {
		return RunCommandResult{}, err
	}
	return RunCommandResult{Run: outcome.Run, Replayed: outcome.Replayed}, nil
}

func productCLIAuthority(ctx context.Context) (Authority, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return Authority{}, err
	}
	if authority.Surface != AuthorityProductCLI || authority.Actor.Kind != domain.ActorAgent {
		return Authority{}, ErrAuthorityScopeDenied
	}
	return authority, nil
}

func (runs *AgentRuns) newRunIDs(
	ctx context.Context,
	at time.Time,
) (domain.RunID, domain.TurnID, domain.ActivityEventID, error) {
	values := make([]string, 3)
	for index := range values {
		value, err := runs.identities.NewID(ctx, at)
		if err != nil {
			return domain.RunID{}, domain.TurnID{}, domain.ActivityEventID{}, err
		}
		values[index] = value
	}
	runID, err := domain.ParseRunID(values[0])
	if err != nil {
		return domain.RunID{}, domain.TurnID{}, domain.ActivityEventID{}, err
	}
	turnID, err := domain.ParseTurnID(values[1])
	if err != nil {
		return domain.RunID{}, domain.TurnID{}, domain.ActivityEventID{}, err
	}
	eventID, err := domain.ParseActivityEventID(values[2])
	return runID, turnID, eventID, err
}

func (runs *AgentRuns) newActivityEventID(ctx context.Context, at time.Time) (domain.ActivityEventID, error) {
	value, err := runs.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(value)
}

func runRequestDigest(
	commandName string,
	actor domain.ActorRef,
	projectID domain.ProjectID,
	runID *domain.RunID,
	turnID *domain.TurnID,
	input any,
) ([]byte, domain.Digest, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, "", err
	}
	canonical, err := json.Marshal(struct {
		Domain  string `json:"domain"`
		Payload struct {
			Actor     domain.ActorRef  `json:"actor"`
			Command   string           `json:"command"`
			Input     json.RawMessage  `json:"input"`
			ProjectID domain.ProjectID `json:"projectId"`
			RunID     *domain.RunID    `json:"runId,omitempty"`
			TurnID    *domain.TurnID   `json:"turnId,omitempty"`
		} `json:"payload"`
		Schema string `json:"schema"`
	}{
		Domain: "open-cut/agent-run-request",
		Payload: struct {
			Actor     domain.ActorRef  `json:"actor"`
			Command   string           `json:"command"`
			Input     json.RawMessage  `json:"input"`
			ProjectID domain.ProjectID `json:"projectId"`
			RunID     *domain.RunID    `json:"runId,omitempty"`
			TurnID    *domain.TurnID   `json:"turnId,omitempty"`
		}{Actor: actor, Command: commandName, Input: inputJSON, ProjectID: projectID, RunID: runID, TurnID: turnID},
		Schema: RunRequestSchema,
	})
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(canonical)
	return canonical, domain.Digest("sha256:" + hex.EncodeToString(digest[:])), nil
}
