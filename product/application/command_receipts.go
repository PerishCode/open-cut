package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	CommandReceiptSchema      = "open-cut/command-receipt/v2"
	MaximumCommandReceiptPage = 100
	MaximumCommandReceiptRefs = 256
)

var (
	ErrCommandReceiptInvalid       = errors.New("command receipt is invalid")
	ErrCommandReceiptNotFound      = errors.New("command receipt was not found")
	ErrCommandReceiptRequestReused = errors.New("command receipt request identity was reused")
)

type InvocationContext struct {
	ProjectID  *domain.ProjectID  `json:"projectId,omitempty"`
	SequenceID *domain.SequenceID `json:"sequenceId,omitempty"`
	RunID      *domain.RunID      `json:"runId,omitempty"`
	TurnID     *domain.TurnID     `json:"turnId,omitempty"`
}

func (value InvocationContext) Validate() error {
	if (value.ProjectID != nil && value.ProjectID.IsZero()) ||
		(value.SequenceID != nil && value.SequenceID.IsZero()) ||
		(value.RunID != nil && value.RunID.IsZero()) ||
		(value.TurnID != nil && value.TurnID.IsZero()) ||
		(value.TurnID != nil && value.RunID == nil) {
		return ErrCommandReceiptInvalid
	}
	return nil
}

type CommandReceiptClass string

const (
	CommandReceiptNone     CommandReceiptClass = "none"
	CommandReceiptEvidence CommandReceiptClass = "evidence"
	CommandReceiptOutcome  CommandReceiptClass = "outcome"
)

type CommandInvocation struct {
	ID          domain.CommandReceiptID
	Command     string
	Fingerprint domain.Digest
	Class       CommandReceiptClass
	InputDigest domain.Digest
	RequestID   *domain.RequestID
	Context     InvocationContext
}

func (value CommandInvocation) Validate() error {
	if value.ID.IsZero() || value.Command == "" || len(value.Command) > 128 ||
		value.Fingerprint == "" || value.InputDigest == "" || value.Context.Validate() != nil ||
		(value.Class != CommandReceiptNone && value.Class != CommandReceiptEvidence && value.Class != CommandReceiptOutcome) {
		return ErrCommandReceiptInvalid
	}
	if value.RequestID != nil {
		if _, err := domain.ParseRequestID(value.RequestID.String()); err != nil {
			return ErrCommandReceiptInvalid
		}
	}
	return nil
}

type CommandReceiptStatus string

const (
	CommandReceiptSucceeded        CommandReceiptStatus = "succeeded"
	CommandReceiptAccepted         CommandReceiptStatus = "accepted"
	CommandReceiptWaiting          CommandReceiptStatus = "waiting"
	CommandReceiptApprovalRequired CommandReceiptStatus = "approval-required"
	CommandReceiptConflict         CommandReceiptStatus = "conflict"
	CommandReceiptNotFound         CommandReceiptStatus = "not-found"
	CommandReceiptUnavailable      CommandReceiptStatus = "unavailable"
	CommandReceiptIncompatible     CommandReceiptStatus = "incompatible"
	CommandReceiptInvalid          CommandReceiptStatus = "invalid"
	CommandReceiptFailed           CommandReceiptStatus = "failed"
)

type CommandReceiptRef struct {
	Kind     string           `json:"kind" minLength:"1" maxLength:"64"`
	ID       string           `json:"id" minLength:"1" maxLength:"128"`
	Revision *domain.Revision `json:"revision,omitempty" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type CommandReceipt struct {
	Schema             string                  `json:"schema" enum:"open-cut/command-receipt/v2"`
	ID                 domain.CommandReceiptID `json:"id" format:"uuid"`
	ProjectID          domain.ProjectID        `json:"projectId" format:"uuid"`
	RunID              domain.RunID            `json:"runId" format:"uuid"`
	TurnID             domain.TurnID           `json:"turnId" format:"uuid"`
	Ordinal            domain.Cursor           `json:"ordinal" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Class              CommandReceiptClass     `json:"class" enum:"evidence,outcome"`
	Command            string                  `json:"command" minLength:"3" maxLength:"128"`
	CommandFingerprint domain.Digest           `json:"commandFingerprint" format:"sha256-digest"`
	InputDigest        domain.Digest           `json:"inputDigest" format:"sha256-digest"`
	RequestID          *domain.RequestID       `json:"requestId,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	ResultDigest       domain.Digest           `json:"resultDigest" format:"sha256-digest"`
	Status             CommandReceiptStatus    `json:"status" enum:"succeeded,accepted,waiting,approval-required,conflict,not-found,unavailable,incompatible,invalid,failed"`
	ResultRefs         []CommandReceiptRef     `json:"resultRefs" maxItems:"256" nullable:"false"`
	ProjectRevision    *domain.Revision        `json:"projectRevision,omitempty" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	ActivityCursor     *domain.Cursor          `json:"activityCursor,omitempty" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	CreatedAt          time.Time               `json:"createdAt"`
}

type TurnReceiptPage struct {
	ProjectID domain.ProjectID `json:"projectId" format:"uuid"`
	RunID     domain.RunID     `json:"runId" format:"uuid"`
	TurnID    domain.TurnID    `json:"turnId" format:"uuid"`
	Receipts  []CommandReceipt `json:"receipts" maxItems:"100" nullable:"false"`
	NextAfter *domain.Cursor   `json:"nextAfter,omitempty" format:"uint64-decimal"`
}

type CommandEvidenceResult struct {
	Status          CommandReceiptStatus
	Result          any
	ResultRefs      []CommandReceiptRef
	ProjectRevision *domain.Revision
	ActivityCursor  *domain.Cursor
}

type CommandBusinessResult = CommandEvidenceResult

type RecordCommandReceipt struct {
	Receipt CommandReceipt
	Actor   domain.ActorRef
}

type CommandReceiptRepository interface {
	RecordCommandReceipt(context.Context, RecordCommandReceipt) (CommandReceipt, error)
	FindCommandReceipt(context.Context, domain.ActorRef, domain.RequestID) (CommandReceipt, bool, error)
	ListCommandReceipts(context.Context, domain.ProjectID, domain.RunID, domain.TurnID, domain.Cursor, uint32) (TurnReceiptPage, error)
}

func (runs *AgentRuns) RecordEvidence(
	ctx context.Context,
	projectID domain.ProjectID,
	evidence CommandEvidenceResult,
) (CommandReceipt, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return CommandReceipt{}, err
	}
	if authority.Surface == AuthorityProductCLI && authority.Invocation != nil &&
		authority.Invocation.Class != CommandReceiptEvidence &&
		authority.Invocation.Context.RunID != nil && authority.Invocation.Context.TurnID != nil {
		return CommandReceipt{}, ErrCommandReceiptInvalid
	}
	return runs.recordBusinessResult(ctx, authority, projectID, evidence)
}

func (runs *AgentRuns) RecordBusinessResult(
	ctx context.Context,
	projectID domain.ProjectID,
	result CommandBusinessResult,
) (CommandReceipt, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return CommandReceipt{}, err
	}
	return runs.recordBusinessResult(ctx, authority, projectID, result)
}

func (runs *AgentRuns) PriorCommandReceipt(
	ctx context.Context,
	projectID domain.ProjectID,
) (CommandReceipt, bool, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return CommandReceipt{}, false, err
	}
	if authority.Surface != AuthorityProductCLI || authority.Invocation == nil ||
		authority.Invocation.RequestID == nil {
		return CommandReceipt{}, false, nil
	}
	receipt, exists, err := runs.repository.FindCommandReceipt(ctx, authority.Actor, *authority.Invocation.RequestID)
	if err != nil || !exists {
		return CommandReceipt{}, exists, err
	}
	invocation := authority.Invocation
	if projectID.IsZero() || invocation.Context.ProjectID == nil || invocation.Context.RunID == nil ||
		invocation.Context.TurnID == nil || *invocation.Context.ProjectID != projectID ||
		receipt.ProjectID != projectID || receipt.RunID != *invocation.Context.RunID ||
		receipt.TurnID != *invocation.Context.TurnID || receipt.Class != invocation.Class ||
		receipt.Command != invocation.Command || receipt.CommandFingerprint != invocation.Fingerprint ||
		receipt.InputDigest != invocation.InputDigest {
		return CommandReceipt{}, false, ErrCommandReceiptRequestReused
	}
	return receipt, true, nil
}

func (runs *AgentRuns) recordBusinessResult(
	ctx context.Context,
	authority Authority,
	projectID domain.ProjectID,
	result CommandBusinessResult,
) (CommandReceipt, error) {
	// First-party reads and standalone CLI reads have no Agent Turn ledger.
	// They are valid invocations, but deliberately produce no durable receipt.
	if authority.Surface == AuthorityFirstPartyUI ||
		(authority.Surface == AuthorityProductCLI && authority.Invocation != nil &&
			(authority.Invocation.Context.RunID == nil || authority.Invocation.Context.TurnID == nil)) {
		return CommandReceipt{}, nil
	}
	if authority.Surface != AuthorityProductCLI || authority.Invocation == nil ||
		authority.Invocation.Class == CommandReceiptNone || authority.Invocation.Context.RunID == nil ||
		authority.Invocation.Context.TurnID == nil || projectID.IsZero() ||
		authority.Invocation.Context.ProjectID == nil || *authority.Invocation.Context.ProjectID != projectID ||
		len(result.ResultRefs) > MaximumCommandReceiptRefs || !validCommandReceiptStatus(result.Status) {
		return CommandReceipt{}, ErrCommandReceiptInvalid
	}
	for _, ref := range result.ResultRefs {
		if err := validateCommandReceiptRef(ref); err != nil {
			return CommandReceipt{}, err
		}
	}
	digestDomain := "open-cut/command-evidence"
	if authority.Invocation.Class == CommandReceiptOutcome {
		digestDomain = "open-cut/command-outcome"
	}
	_, resultDigest, err := domain.CanonicalDigest(digestDomain, CommandReceiptSchema, result.Result)
	if err != nil {
		return CommandReceipt{}, ErrCommandReceiptInvalid
	}
	now := runs.clock.Now().UTC()
	receiptClass := authority.Invocation.Class
	receipt := CommandReceipt{
		Schema: CommandReceiptSchema, ID: authority.Invocation.ID, ProjectID: projectID,
		RunID: *authority.Invocation.Context.RunID, TurnID: *authority.Invocation.Context.TurnID,
		Class: receiptClass, Command: authority.Invocation.Command,
		CommandFingerprint: authority.Invocation.Fingerprint, InputDigest: authority.Invocation.InputDigest,
		RequestID: authority.Invocation.RequestID, ResultDigest: resultDigest, Status: result.Status,
		ResultRefs:      append([]CommandReceiptRef(nil), result.ResultRefs...),
		ProjectRevision: result.ProjectRevision, ActivityCursor: result.ActivityCursor, CreatedAt: now,
	}
	return runs.repository.RecordCommandReceipt(ctx, RecordCommandReceipt{Receipt: receipt, Actor: authority.Actor})
}

func (runs *AgentRuns) Receipts(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	after domain.Cursor,
	limit uint32,
) (TurnReceiptPage, error) {
	if _, err := agentBridgeCreatorAuthority(ctx); err != nil {
		return TurnReceiptPage{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() || limit > MaximumCommandReceiptPage {
		return TurnReceiptPage{}, ErrCommandReceiptInvalid
	}
	return runs.repository.ListCommandReceipts(ctx, projectID, runID, turnID, after, limit)
}

func validCommandReceiptStatus(status CommandReceiptStatus) bool {
	switch status {
	case CommandReceiptSucceeded, CommandReceiptAccepted, CommandReceiptWaiting, CommandReceiptApprovalRequired,
		CommandReceiptConflict, CommandReceiptNotFound, CommandReceiptUnavailable, CommandReceiptIncompatible,
		CommandReceiptInvalid, CommandReceiptFailed:
		return true
	default:
		return false
	}
}

func validateCommandReceiptRef(value CommandReceiptRef) error {
	if value.Kind == "" || len(value.Kind) > 64 || value.ID == "" || len(value.ID) > 128 ||
		(value.Revision != nil && value.Revision.Value() < 1) {
		return fmt.Errorf("%w: result reference", ErrCommandReceiptInvalid)
	}
	return nil
}
