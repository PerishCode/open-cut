package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	AgentBridgeRequestSchema         = "open-cut/agent-bridge-request/v1"
	AgentBridgeAdapterCodexV1        = "codex-cli-v1"
	AgentBridgePromptV2              = "open-cut-agent-v2"
	MaximumCreatorMessageBytes       = 32 * 1024
	MaximumAgentTurnTextBytes        = 256 * 1024
	MaximumAgentRecoveryBytes        = 768 * 1024
	MaximumAgentRecoveryMessages     = 200
	MaximumAgentRecoveryReceipts     = 100
	MaximumAgentRecoveryReceiptBytes = 192 * 1024
	MaximumConversationPage          = 100
	MaximumAgentBridgeRunPage        = 20
	MaximumAgentBridgeTurnPage       = 100
)

var (
	ErrAgentBridgeInvalid       = errors.New("Agent bridge input is invalid")
	ErrAgentBridgeNotFound      = errors.New("Agent bridge Run was not found")
	ErrAgentBridgeBusy          = errors.New("Agent bridge Run already has an active turn")
	ErrAgentBridgeTerminal      = errors.New("Agent bridge Run is terminal")
	ErrAgentBridgeStaleTurn     = errors.New("Agent bridge Turn generation is stale")
	ErrAgentBridgeRequestReused = errors.New("Agent bridge request identity was reused")
	ErrAgentBridgeBindingDenied = errors.New("Agent bridge Agent binding was denied")
	ErrAgentContextStale        = errors.New("Agent context attachment is stale")
)

type AgentConversationRole string

const (
	AgentConversationCreator AgentConversationRole = "creator"
	AgentConversationAgent   AgentConversationRole = "agent"
	AgentConversationNotice  AgentConversationRole = "notice"
)

const AgentConversationContextRebuilt = "context-rebuilt"

type AgentBridgeAvailabilityState string

const (
	AgentBridgeAvailable       AgentBridgeAvailabilityState = "available"
	AgentBridgeMissing         AgentBridgeAvailabilityState = "missing"
	AgentBridgeUnauthenticated AgentBridgeAvailabilityState = "unauthenticated"
	AgentBridgeIncompatible    AgentBridgeAvailabilityState = "incompatible"
)

type AgentBridgeAvailability struct {
	AdapterID     string                       `json:"adapterId" enum:"codex-cli-v1"`
	Version       string                       `json:"version,omitempty" maxLength:"128"`
	PromptVersion string                       `json:"promptVersion" enum:"open-cut-agent-v2"`
	State         AgentBridgeAvailabilityState `json:"state" enum:"available,missing,unauthenticated,incompatible"`
}

type AgentConversationMessage struct {
	ID          domain.ConversationMessageID `json:"id" format:"uuid"`
	ProjectID   domain.ProjectID             `json:"projectId" format:"uuid"`
	RunID       domain.RunID                 `json:"runId" format:"uuid"`
	TurnID      domain.TurnID                `json:"turnId" format:"uuid"`
	Ordinal     domain.Cursor                `json:"ordinal" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Role        AgentConversationRole        `json:"role" enum:"creator,agent,notice"`
	Text        string                       `json:"text" minLength:"1" maxLength:"262144"`
	Attachments []AgentContextAttachment     `json:"attachments" maxItems:"64" nullable:"false"`
	CreatedAt   time.Time                    `json:"createdAt"`
}

type AgentBridgeTurn struct {
	ID         domain.TurnID      `json:"id" format:"uuid"`
	Generation domain.Revision    `json:"generation" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	SequenceID *domain.SequenceID `json:"sequenceId,omitempty" format:"uuid"`
	Status     AgentTurnStatus    `json:"status" enum:"starting,active,detached,completed,failed,cancelled,superseded"`
	StartedAt  time.Time          `json:"startedAt"`
	EndedAt    *time.Time         `json:"endedAt,omitempty"`
}

type AgentBridgeRun struct {
	ID             domain.RunID     `json:"id" format:"uuid"`
	ProjectID      domain.ProjectID `json:"projectId" format:"uuid"`
	Intent         string           `json:"intent" minLength:"1" maxLength:"32768"`
	AgentID        *domain.AgentID  `json:"agentId,omitempty" format:"uuid"`
	Status         AgentRunStatus   `json:"status" enum:"authorizing,active,waiting,paused,completed,failed,cancelled"`
	WaitingReason  string           `json:"waitingReason,omitempty" maxLength:"128"`
	CurrentTurn    AgentBridgeTurn  `json:"currentTurn"`
	ActivityCursor domain.Cursor    `json:"activityCursor" format:"uint64-decimal"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
	CompletedAt    *time.Time       `json:"completedAt,omitempty"`
}

type AgentBridgeResult struct {
	Run      AgentBridgeRun            `json:"run"`
	Message  *AgentConversationMessage `json:"message,omitempty"`
	Replayed bool                      `json:"replayed"`
}

type AgentConversationPage struct {
	ProjectID domain.ProjectID           `json:"projectId" format:"uuid"`
	RunID     domain.RunID               `json:"runId" format:"uuid"`
	Messages  []AgentConversationMessage `json:"messages" maxItems:"100" nullable:"false"`
	NextAfter *domain.Cursor             `json:"nextAfter,omitempty" format:"uint64-decimal"`
}

type AgentBridgeRunPage struct {
	ProjectID domain.ProjectID `json:"projectId" format:"uuid"`
	Runs      []AgentBridgeRun `json:"runs" maxItems:"20" nullable:"false"`
}

type AgentBridgeTurnPage struct {
	ProjectID  domain.ProjectID  `json:"projectId" format:"uuid"`
	RunID      domain.RunID      `json:"runId" format:"uuid"`
	Turns      []AgentBridgeTurn `json:"turns" maxItems:"100" nullable:"false"`
	NextBefore *domain.Cursor    `json:"nextBefore,omitempty" format:"uint64-decimal"`
}

type AgentBridgeBeginInput struct {
	RequestID   domain.RequestID         `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Message     string                   `json:"message" minLength:"1" maxLength:"32768"`
	SequenceID  *domain.SequenceID       `json:"sequenceId,omitempty" format:"uuid"`
	Attachments []AgentContextAttachment `json:"attachments" maxItems:"64" nullable:"false"`
}

type AgentBridgeContinueInput struct {
	RequestID          domain.RequestID         `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	ExpectedGeneration domain.Revision          `json:"expectedGeneration" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Message            string                   `json:"message" minLength:"1" maxLength:"32768"`
	SequenceID         *domain.SequenceID       `json:"sequenceId,omitempty" format:"uuid"`
	Attachments        []AgentContextAttachment `json:"attachments" maxItems:"64" nullable:"false"`
}

type AgentBridgeTransitionInput struct {
	RequestID          domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	ExpectedGeneration domain.Revision  `json:"expectedGeneration" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type AgentConversationListInput struct {
	After domain.Cursor `query:"after" format:"uint64-decimal"`
	Limit uint32        `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type BeginAgentBridgeRecord struct {
	VersionID        domain.ProjectVersionID
	RunID            domain.RunID
	TurnID           domain.TurnID
	ProjectID        domain.ProjectID
	SequenceID       *domain.SequenceID
	Attachments      []AgentContextAttachment
	Creator          domain.ActorRef
	Intent           string
	RequestID        domain.RequestID
	RequestDigest    domain.Digest
	RequestCanonical []byte
	MessageID        domain.ConversationMessageID
	ActivityEventID  domain.ActivityEventID
	CreatedAt        time.Time
}

type ContinueAgentBridgeRecord struct {
	VersionID          domain.ProjectVersionID
	ProjectID          domain.ProjectID
	RunID              domain.RunID
	ExpectedGeneration domain.Revision
	TurnID             domain.TurnID
	SequenceID         *domain.SequenceID
	Attachments        []AgentContextAttachment
	Creator            domain.ActorRef
	Message            string
	RequestID          domain.RequestID
	RequestDigest      domain.Digest
	RequestCanonical   []byte
	MessageID          domain.ConversationMessageID
	ActivityEventID    domain.ActivityEventID
	CreatedAt          time.Time
}

type AgentBridgeTransition string

const (
	AgentBridgeInterrupt AgentBridgeTransition = "interrupt"
	AgentBridgeCancel    AgentBridgeTransition = "cancel"
)

type TransitionAgentBridgeRecord struct {
	Transition         AgentBridgeTransition
	ProjectID          domain.ProjectID
	RunID              domain.RunID
	ExpectedTurnID     domain.TurnID
	ExpectedGeneration domain.Revision
	Creator            domain.ActorRef
	RequestID          domain.RequestID
	RequestDigest      domain.Digest
	RequestCanonical   []byte
	ActivityEventID    domain.ActivityEventID
	OccurredAt         time.Time
}

type AgentBridgeRuntimeOutcome string

const (
	AgentBridgeRuntimeCompleted     AgentBridgeRuntimeOutcome = "completed"
	AgentBridgeRuntimeDetached      AgentBridgeRuntimeOutcome = "detached"
	AgentBridgeRuntimeFailed        AgentBridgeRuntimeOutcome = "failed"
	AgentBridgeRuntimeResourceLimit AgentBridgeRuntimeOutcome = "resource-limit"
)

type AgentBridgeRuntimeRecord struct {
	ProjectID  domain.ProjectID
	RunID      domain.RunID
	TurnID     domain.TurnID
	Outcome    AgentBridgeRuntimeOutcome
	OccurredAt time.Time
}

type AgentBridgeInvocation struct {
	ProjectID           domain.ProjectID
	RunID               domain.RunID
	TurnID              domain.TurnID
	SequenceID          *domain.SequenceID
	Messages            []AgentConversationMessage
	OmittedMessageCount uint64
	Receipts            []CommandReceipt
	OmittedReceiptCount uint64
	NativeSessionID     string
}

type AgentBridgeRepository interface {
	BeginAgentBridge(context.Context, BeginAgentBridgeRecord) (AgentBridgeResult, error)
	ContinueAgentBridge(context.Context, ContinueAgentBridgeRecord) (AgentBridgeResult, error)
	TransitionAgentBridge(context.Context, TransitionAgentBridgeRecord) (AgentBridgeResult, error)
	ShowAgentBridge(context.Context, domain.ProjectID, domain.RunID) (AgentBridgeRun, error)
	ListAgentBridges(context.Context, domain.ProjectID, uint32) (AgentBridgeRunPage, error)
	ListAgentBridgeTurns(context.Context, domain.ProjectID, domain.RunID, domain.Cursor, uint32) (AgentBridgeTurnPage, error)
	ListAgentConversation(context.Context, domain.ProjectID, domain.RunID, domain.Cursor, uint32) (AgentConversationPage, error)
	ActivateAgentBridgeTurn(context.Context, domain.ProjectID, domain.RunID, domain.TurnID, string, string, time.Time) error
	AppendAgentBridgeMessage(context.Context, AgentConversationMessage) (AgentConversationMessage, error)
	FinishAgentBridgeTurn(context.Context, AgentBridgeRuntimeRecord) error
	SetAgentBridgeNativeSession(context.Context, domain.RunID, domain.TurnID, string) error
	PrepareAgentBridgeInvocation(context.Context, domain.ProjectID, domain.RunID, domain.TurnID) (AgentBridgeInvocation, error)
	RecoverAgentBridgeTurns(context.Context, time.Time) error
}

type AgentRunBindingRepository interface {
	AssociateAgentRunPairing(context.Context, domain.RunID, domain.TurnID, string, domain.Revision, time.Time) error
	BindAuthorizedAgentRun(context.Context, domain.RunID, domain.TurnID, domain.AgentID, string, domain.Revision, domain.ActivityEventID, time.Time) error
}

type AgentBridges struct {
	repository AgentBridgeRepository
	identities IdentityGenerator
	clock      Clock
}

func NewAgentBridges(repository AgentBridgeRepository, identities IdentityGenerator, clock Clock) (*AgentBridges, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("Agent bridge dependencies are required")
	}
	return &AgentBridges{repository: repository, identities: identities, clock: clock}, nil
}

func (bridges *AgentBridges) Begin(ctx context.Context, projectID domain.ProjectID, input AgentBridgeBeginInput) (AgentBridgeResult, error) {
	authority, err := agentBridgeCreatorAuthority(ctx)
	if err != nil || !validAgentBridgeMessage(input.Message) || projectID.IsZero() || !validOptionalSequence(input.SequenceID) ||
		ValidateAgentContextAttachments(input.Attachments) != nil {
		return AgentBridgeResult{}, ErrAgentBridgeInvalid
	}
	requestID, err := domain.ParseRequestID(input.RequestID.String())
	if err != nil {
		return AgentBridgeResult{}, ErrAgentBridgeInvalid
	}
	canonical, digest, err := agentBridgeDigest("begin", authority.Actor, projectID, domain.RunID{}, input)
	if err != nil {
		return AgentBridgeResult{}, err
	}
	now := bridges.clock.Now().UTC()
	values, err := bridges.newIDs(ctx, now, 5)
	if err != nil {
		return AgentBridgeResult{}, err
	}
	runID, _ := domain.ParseRunID(values[0])
	turnID, _ := domain.ParseTurnID(values[1])
	messageID, _ := domain.ParseConversationMessageID(values[2])
	eventID, _ := domain.ParseActivityEventID(values[3])
	versionID, _ := domain.ParseProjectVersionID(values[4])
	return bridges.repository.BeginAgentBridge(ctx, BeginAgentBridgeRecord{
		VersionID: versionID, RunID: runID, TurnID: turnID, ProjectID: projectID,
		SequenceID: input.SequenceID, Attachments: input.Attachments, Creator: authority.Actor, Intent: input.Message,
		RequestID: requestID, RequestDigest: digest, RequestCanonical: canonical,
		MessageID: messageID, ActivityEventID: eventID, CreatedAt: now,
	})
}

func (bridges *AgentBridges) Continue(ctx context.Context, projectID domain.ProjectID, runID domain.RunID, input AgentBridgeContinueInput) (AgentBridgeResult, error) {
	authority, err := agentBridgeCreatorAuthority(ctx)
	if err != nil || projectID.IsZero() || runID.IsZero() || input.ExpectedGeneration.Value() < 1 ||
		!validAgentBridgeMessage(input.Message) || !validOptionalSequence(input.SequenceID) ||
		ValidateAgentContextAttachments(input.Attachments) != nil {
		return AgentBridgeResult{}, ErrAgentBridgeInvalid
	}
	requestID, err := domain.ParseRequestID(input.RequestID.String())
	if err != nil {
		return AgentBridgeResult{}, ErrAgentBridgeInvalid
	}
	canonical, digest, err := agentBridgeDigest("continue", authority.Actor, projectID, runID, input)
	if err != nil {
		return AgentBridgeResult{}, err
	}
	now := bridges.clock.Now().UTC()
	values, err := bridges.newIDs(ctx, now, 4)
	if err != nil {
		return AgentBridgeResult{}, err
	}
	turnID, _ := domain.ParseTurnID(values[0])
	messageID, _ := domain.ParseConversationMessageID(values[1])
	eventID, _ := domain.ParseActivityEventID(values[2])
	versionID, _ := domain.ParseProjectVersionID(values[3])
	return bridges.repository.ContinueAgentBridge(ctx, ContinueAgentBridgeRecord{
		VersionID: versionID, ProjectID: projectID, RunID: runID, ExpectedGeneration: input.ExpectedGeneration,
		TurnID: turnID, SequenceID: input.SequenceID, Attachments: input.Attachments,
		Creator: authority.Actor, Message: input.Message,
		RequestID: requestID, RequestDigest: digest, RequestCanonical: canonical,
		MessageID: messageID, ActivityEventID: eventID, CreatedAt: now,
	})
}

func (bridges *AgentBridges) Interrupt(ctx context.Context, projectID domain.ProjectID, runID domain.RunID, turnID domain.TurnID, input AgentBridgeTransitionInput) (AgentBridgeResult, error) {
	return bridges.transition(ctx, AgentBridgeInterrupt, projectID, runID, turnID, input)
}

func (bridges *AgentBridges) Cancel(ctx context.Context, projectID domain.ProjectID, runID domain.RunID, turnID domain.TurnID, input AgentBridgeTransitionInput) (AgentBridgeResult, error) {
	return bridges.transition(ctx, AgentBridgeCancel, projectID, runID, turnID, input)
}

func (bridges *AgentBridges) transition(ctx context.Context, transition AgentBridgeTransition, projectID domain.ProjectID, runID domain.RunID, turnID domain.TurnID, input AgentBridgeTransitionInput) (AgentBridgeResult, error) {
	authority, err := agentBridgeCreatorAuthority(ctx)
	if err != nil || projectID.IsZero() || runID.IsZero() || turnID.IsZero() || input.ExpectedGeneration.Value() < 1 {
		return AgentBridgeResult{}, ErrAgentBridgeInvalid
	}
	requestID, err := domain.ParseRequestID(input.RequestID.String())
	if err != nil {
		return AgentBridgeResult{}, ErrAgentBridgeInvalid
	}
	canonical, digest, err := agentBridgeDigest(string(transition), authority.Actor, projectID, runID, struct {
		TurnID domain.TurnID              `json:"turnId"`
		Input  AgentBridgeTransitionInput `json:"input"`
	}{TurnID: turnID, Input: input})
	if err != nil {
		return AgentBridgeResult{}, err
	}
	now := bridges.clock.Now().UTC()
	value, err := bridges.identities.NewID(ctx, now)
	if err != nil {
		return AgentBridgeResult{}, err
	}
	eventID, err := domain.ParseActivityEventID(value)
	if err != nil {
		return AgentBridgeResult{}, err
	}
	return bridges.repository.TransitionAgentBridge(ctx, TransitionAgentBridgeRecord{
		Transition: transition, ProjectID: projectID, RunID: runID,
		ExpectedTurnID: turnID, ExpectedGeneration: input.ExpectedGeneration, Creator: authority.Actor,
		RequestID: requestID, RequestDigest: digest, RequestCanonical: canonical,
		ActivityEventID: eventID, OccurredAt: now,
	})
}

func (bridges *AgentBridges) Show(ctx context.Context, projectID domain.ProjectID, runID domain.RunID) (AgentBridgeRun, error) {
	if _, err := agentBridgeCreatorAuthority(ctx); err != nil {
		return AgentBridgeRun{}, err
	}
	return bridges.repository.ShowAgentBridge(ctx, projectID, runID)
}

func (bridges *AgentBridges) List(ctx context.Context, projectID domain.ProjectID, limit uint32) (AgentBridgeRunPage, error) {
	if _, err := agentBridgeCreatorAuthority(ctx); err != nil {
		return AgentBridgeRunPage{}, err
	}
	if limit == 0 {
		limit = 10
	}
	if projectID.IsZero() || limit > MaximumAgentBridgeRunPage {
		return AgentBridgeRunPage{}, ErrAgentBridgeInvalid
	}
	return bridges.repository.ListAgentBridges(ctx, projectID, limit)
}

func (bridges *AgentBridges) Turns(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	before domain.Cursor,
	limit uint32,
) (AgentBridgeTurnPage, error) {
	if _, err := agentBridgeCreatorAuthority(ctx); err != nil {
		return AgentBridgeTurnPage{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if projectID.IsZero() || runID.IsZero() || limit > MaximumAgentBridgeTurnPage {
		return AgentBridgeTurnPage{}, ErrAgentBridgeInvalid
	}
	return bridges.repository.ListAgentBridgeTurns(ctx, projectID, runID, before, limit)
}

func (bridges *AgentBridges) Conversation(ctx context.Context, projectID domain.ProjectID, runID domain.RunID, input AgentConversationListInput) (AgentConversationPage, error) {
	if _, err := agentBridgeCreatorAuthority(ctx); err != nil {
		return AgentConversationPage{}, err
	}
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	if projectID.IsZero() || runID.IsZero() || limit > MaximumConversationPage {
		return AgentConversationPage{}, ErrAgentBridgeInvalid
	}
	return bridges.repository.ListAgentConversation(ctx, projectID, runID, input.After, limit)
}

func (bridges *AgentBridges) AppendAgentMessage(ctx context.Context, projectID domain.ProjectID, runID domain.RunID, turnID domain.TurnID, text string) (AgentConversationMessage, error) {
	if len(text) == 0 || len([]byte(text)) > MaximumAgentTurnTextBytes {
		return AgentConversationMessage{}, ErrAgentBridgeInvalid
	}
	now := bridges.clock.Now().UTC()
	value, err := bridges.identities.NewID(ctx, now)
	if err != nil {
		return AgentConversationMessage{}, err
	}
	id, err := domain.ParseConversationMessageID(value)
	if err != nil {
		return AgentConversationMessage{}, err
	}
	message := AgentConversationMessage{ID: id, ProjectID: projectID, RunID: runID, TurnID: turnID, Role: AgentConversationAgent, Text: text, CreatedAt: now}
	message, err = bridges.repository.AppendAgentBridgeMessage(ctx, message)
	if err != nil {
		return AgentConversationMessage{}, err
	}
	return message, nil
}

func (bridges *AgentBridges) AppendContextRebuiltNotice(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
) (AgentConversationMessage, error) {
	now := bridges.clock.Now().UTC()
	value, err := bridges.identities.NewID(ctx, now)
	if err != nil {
		return AgentConversationMessage{}, err
	}
	id, err := domain.ParseConversationMessageID(value)
	if err != nil {
		return AgentConversationMessage{}, err
	}
	return bridges.repository.AppendAgentBridgeMessage(ctx, AgentConversationMessage{
		ID: id, ProjectID: projectID, RunID: runID, TurnID: turnID,
		Role: AgentConversationNotice, Text: AgentConversationContextRebuilt, CreatedAt: now,
	})
}

func (bridges *AgentBridges) newIDs(ctx context.Context, at time.Time, count int) ([]string, error) {
	values := make([]string, count)
	for index := range values {
		value, err := bridges.identities.NewID(ctx, at)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func agentBridgeCreatorAuthority(ctx context.Context) (Authority, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil || authority.Surface != AuthorityFirstPartyUI || authority.Actor.Kind != domain.ActorCreator {
		return Authority{}, ErrAuthorityScopeDenied
	}
	return authority, nil
}

func validAgentBridgeMessage(value string) bool {
	return len(value) > 0 && len([]byte(value)) <= MaximumCreatorMessageBytes
}

func validOptionalSequence(value *domain.SequenceID) bool {
	return value == nil || !value.IsZero()
}

func agentBridgeDigest(command string, actor domain.ActorRef, projectID domain.ProjectID, runID domain.RunID, input any) ([]byte, domain.Digest, error) {
	payload := struct {
		Actor     domain.ActorRef  `json:"actor"`
		Command   string           `json:"command"`
		Input     any              `json:"input"`
		ProjectID domain.ProjectID `json:"projectId"`
		RunID     *domain.RunID    `json:"runId,omitempty"`
	}{Actor: actor, Command: command, Input: input, ProjectID: projectID}
	if !runID.IsZero() {
		payload.RunID = &runID
	}
	return domain.CanonicalDigest("open-cut/agent-bridge-request", AgentBridgeRequestSchema, payload)
}
