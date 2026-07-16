package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ProposeEdit(
	ctx context.Context,
	record application.ProposeEditRecord,
) (application.EditProposalResult, error) {
	if err := validateProposeEditRecord(record); err != nil {
		return application.EditProposalResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.EditProposalResult{}, err
	}
	defer tx.Rollback()
	if existing, err := loadEditRequest(ctx, tx, record.Actor, record.RequestID); err == nil {
		if existing.Command != "edit propose" || existing.InputDigest != record.InputDigest ||
			existing.ProjectID != record.ProjectID {
			return application.EditProposalResult{}, application.ErrEditRequestReused
		}
		proposal, err := loadEditProposal(ctx, tx, record.ProjectID, existing.ProposalID)
		if err != nil {
			return application.EditProposalResult{}, err
		}
		cursor, err := activityCursorForEvent(ctx, tx, existing.ActivityEventID.String())
		if err != nil {
			return application.EditProposalResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return application.EditProposalResult{}, err
		}
		return application.EditProposalResult{Proposal: proposal, ActivityCursor: cursor, Replayed: true}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return application.EditProposalResult{}, err
	}
	state, err := loadEditNormalizationState(ctx, tx, record)
	if err != nil {
		return application.EditProposalResult{}, fmt.Errorf("load edit normalization state: %w", err)
	}
	proposal, canonical, err := application.NormalizeEditProposal(application.NormalizeEditInput{
		ProposalID: record.ProposalID, ProjectID: record.ProjectID, SequenceID: record.SequenceID,
		RunID: record.RunID, TurnID: record.TurnID, Actor: record.Actor,
		Allocation: record.Allocation, Input: record.Input, CreatedAt: record.CreatedAt, State: state,
	})
	if err != nil {
		return application.EditProposalResult{}, fmt.Errorf("normalize EditProposal: %w", err)
	}
	preconditions, _ := json.Marshal(proposal.Preconditions)
	allocation, _ := json.Marshal(proposal.Allocation)
	operations, _ := json.Marshal(proposal.Operations)
	inverse, _ := json.Marshal(proposal.InversePreview)
	changes, _ := json.Marshal(proposal.Changes)
	impact, _ := json.Marshal(proposal.Impact)
	createdAt := formatInstant(record.CreatedAt)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO edit_proposals (
  id, project_id, schema_version, digest, canonical_json, actor_kind, actor_id, status, created_at,
  request_id, sequence_id, run_id, turn_id, base_project_revision, intent, preconditions_json,
  allocation_json, operations_json, inverse_preview_json, changes_json, impact_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		proposal.ID.String(), proposal.ProjectID.String(), domain.EditProposalSchema,
		proposal.Digest.String(), string(canonical), proposal.Actor.Kind, proposal.Actor.IDString(), createdAt,
		proposal.RequestID.String(), proposal.SequenceID.String(), nullableProposalRunID(proposal.RunID),
		nullableProposalTurnID(proposal.TurnID),
		proposal.BaseProjectRevision.Value(), proposal.Intent, string(preconditions), string(allocation),
		string(operations), string(inverse), string(changes), string(impact), createdAt,
	); err != nil {
		return application.EditProposalResult{}, fmt.Errorf("persist EditProposal: %w", err)
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ProposalID        domain.ProposalID              `json:"proposalId"`
		ProposalDigest    domain.Digest                  `json:"proposalDigest"`
	}{ChangedEntityRefs: []application.ChangedEntityRef{}, ProposalID: proposal.ID, ProposalDigest: proposal.Digest})
	if err != nil {
		return application.EditProposalResult{}, err
	}
	cursor, err := appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: proposal.ProjectID.String(), EventID: record.ActivityEventID.String(),
		Kind: "edit.proposed", OccurredAt: createdAt, ActorKind: string(record.Actor.Kind), ActorID: record.Actor.IDString(),
		ProjectID: proposal.ProjectID.String(), ProjectRevision: int64(state.ProjectRevision.Value()),
		OutcomeKind: "proposal", OutcomeID: proposal.ID.String(), SummaryCode: "edit-proposed", Payload: payload,
	})
	if err != nil {
		return application.EditProposalResult{}, err
	}
	if err := insertEditRequest(ctx, tx, editRequestRecord{
		Actor: record.Actor, RequestID: record.RequestID, Command: "edit propose",
		InputDigest: record.InputDigest, InputCanonical: record.InputCanonical,
		ProjectID: record.ProjectID, RunID: record.RunID, TurnID: record.TurnID,
		ProposalID: proposal.ID, ActivityEventID: record.ActivityEventID, CreatedAt: record.CreatedAt,
	}); err != nil {
		return application.EditProposalResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.EditProposalResult{}, err
	}
	return application.EditProposalResult{Proposal: proposal, ActivityCursor: cursor}, nil
}

type storedEditRequest struct {
	Command         string
	InputDigest     domain.Digest
	ProjectID       domain.ProjectID
	ProposalID      domain.ProposalID
	ApplicationID   *domain.ProposalApplicationID
	TransactionID   *domain.TransactionID
	ActivityEventID domain.ActivityEventID
}

func loadEditRequest(
	ctx context.Context,
	tx *sql.Tx,
	actor domain.ActorRef,
	requestID domain.RequestID,
) (storedEditRequest, error) {
	var commandName, digestValue, projectValue, proposalValue, eventValue string
	var applicationValue, transactionValue sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT command, input_digest, project_id, proposal_id, application_id, transaction_id, activity_event_id
FROM edit_request_identities WHERE actor_kind = ? AND actor_id = ? AND request_id = ?`,
		actor.Kind, actor.IDString(), requestID.String()).Scan(
		&commandName, &digestValue, &projectValue, &proposalValue,
		&applicationValue, &transactionValue, &eventValue,
	)
	if err != nil {
		return storedEditRequest{}, err
	}
	digest, err := domain.ParseDigest(digestValue)
	if err != nil {
		return storedEditRequest{}, err
	}
	projectID, err := domain.ParseProjectID(projectValue)
	if err != nil {
		return storedEditRequest{}, err
	}
	proposalID, err := domain.ParseProposalID(proposalValue)
	if err != nil {
		return storedEditRequest{}, err
	}
	eventID, err := domain.ParseActivityEventID(eventValue)
	if err != nil {
		return storedEditRequest{}, err
	}
	result := storedEditRequest{
		Command: commandName, InputDigest: digest, ProjectID: projectID,
		ProposalID: proposalID, ActivityEventID: eventID,
	}
	if applicationValue.Valid {
		parsed, err := domain.ParseProposalApplicationID(applicationValue.String)
		if err != nil {
			return storedEditRequest{}, err
		}
		result.ApplicationID = &parsed
	}
	if transactionValue.Valid {
		parsed, err := domain.ParseTransactionID(transactionValue.String)
		if err != nil {
			return storedEditRequest{}, err
		}
		result.TransactionID = &parsed
	}
	return result, nil
}

type editRequestRecord struct {
	Actor           domain.ActorRef
	RequestID       domain.RequestID
	Command         string
	InputDigest     domain.Digest
	InputCanonical  []byte
	ProjectID       domain.ProjectID
	RunID           domain.RunID
	TurnID          domain.TurnID
	ProposalID      domain.ProposalID
	ApplicationID   *domain.ProposalApplicationID
	TransactionID   *domain.TransactionID
	ActivityEventID domain.ActivityEventID
	CreatedAt       time.Time
}

func insertEditRequest(ctx context.Context, tx *sql.Tx, record editRequestRecord) error {
	var applicationID, transactionID any
	if record.ApplicationID != nil {
		applicationID = record.ApplicationID.String()
	}
	if record.TransactionID != nil {
		transactionID = record.TransactionID.String()
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO edit_request_identities (
  actor_kind, actor_id, request_id, command, input_digest, input_json,
  project_id, run_id, turn_id, proposal_id, application_id, transaction_id,
  activity_event_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Actor.Kind, record.Actor.IDString(), record.RequestID.String(), record.Command,
		record.InputDigest.String(), string(record.InputCanonical), record.ProjectID.String(),
		nullableRunID(record.RunID), nullableTurnID(record.TurnID), record.ProposalID.String(),
		applicationID, transactionID, record.ActivityEventID.String(), formatInstant(record.CreatedAt),
	); err != nil {
		return fmt.Errorf("persist edit request identity: %w", err)
	}
	return nil
}

func validateProposeEditRecord(record application.ProposeEditRecord) error {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.RunID.IsZero() ||
		record.TurnID.IsZero() || record.ProposalID.IsZero() || record.ActivityEventID.IsZero() ||
		record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorAgent ||
		!json.Valid(record.InputCanonical) || record.CreatedAt.IsZero() {
		return application.ErrEditInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrEditInvalid
	}
	if _, err := domain.ParseDigest(record.InputDigest.String()); err != nil {
		return application.ErrEditInvalid
	}
	return nil
}
