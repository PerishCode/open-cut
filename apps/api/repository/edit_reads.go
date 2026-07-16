package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func loadEditProposal(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	proposalID domain.ProposalID,
) (domain.EditProposal, error) {
	var idValue, projectValue, requestValue, actorKind, actorValue string
	var sequenceValue, runValue, turnValue sql.NullString
	var intent, digestValue, statusValue, createdValue string
	var baseRevision uint64
	var preconditionsJSON, allocationJSON, operationsJSON, inverseJSON, changesJSON, impactJSON string
	var appliedValue sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT id, project_id, sequence_id, run_id, turn_id, request_id, actor_kind, actor_id, intent,
       base_project_revision, preconditions_json, allocation_json, operations_json,
       inverse_preview_json, changes_json, impact_json, digest, status, created_at,
       applied_transaction_id
FROM edit_proposals WHERE id = ? AND project_id = ?`,
		proposalID.String(), projectID.String()).Scan(
		&idValue, &projectValue, &sequenceValue, &runValue, &turnValue, &requestValue, &actorKind, &actorValue, &intent,
		&baseRevision, &preconditionsJSON, &allocationJSON, &operationsJSON,
		&inverseJSON, &changesJSON, &impactJSON, &digestValue, &statusValue, &createdValue, &appliedValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EditProposal{}, application.ErrProposalNotFound
	}
	if err != nil {
		return domain.EditProposal{}, err
	}
	id, _ := domain.ParseProposalID(idValue)
	project, _ := domain.ParseProjectID(projectValue)
	request, err := domain.ParseRequestID(requestValue)
	if err != nil {
		return domain.EditProposal{}, err
	}
	actor, err := parseStoredActor(actorKind, actorValue)
	if err != nil {
		return domain.EditProposal{}, err
	}
	revision, err := domain.NewRevision(baseRevision)
	if err != nil {
		return domain.EditProposal{}, err
	}
	digest, err := domain.ParseDigest(digestValue)
	if err != nil {
		return domain.EditProposal{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdValue)
	if err != nil {
		return domain.EditProposal{}, err
	}
	proposal := domain.EditProposal{
		ID: id, ProjectID: project, RequestID: request,
		Actor: actor, Intent: intent, BaseProjectRevision: revision, Digest: digest,
		Status: storedProposalStatus(statusValue), CreatedAt: createdAt,
	}
	if sequenceValue.Valid {
		sequence, parseErr := domain.ParseSequenceID(sequenceValue.String)
		if parseErr != nil {
			return domain.EditProposal{}, parseErr
		}
		proposal.SequenceID = &sequence
	}
	if runValue.Valid {
		run, parseErr := domain.ParseRunID(runValue.String)
		if parseErr != nil {
			return domain.EditProposal{}, parseErr
		}
		proposal.RunID = &run
	}
	if turnValue.Valid {
		turn, parseErr := domain.ParseTurnID(turnValue.String)
		if parseErr != nil {
			return domain.EditProposal{}, parseErr
		}
		proposal.TurnID = &turn
	}
	if err := decodeEditJSON(preconditionsJSON, &proposal.Preconditions); err != nil {
		return domain.EditProposal{}, err
	}
	if err := decodeEditJSON(allocationJSON, &proposal.Allocation); err != nil {
		return domain.EditProposal{}, err
	}
	if err := decodeEditJSON(operationsJSON, &proposal.Operations); err != nil {
		return domain.EditProposal{}, err
	}
	if err := decodeEditJSON(inverseJSON, &proposal.InversePreview); err != nil {
		return domain.EditProposal{}, err
	}
	if err := decodeEditJSON(changesJSON, &proposal.Changes); err != nil {
		return domain.EditProposal{}, err
	}
	if err := decodeEditJSON(impactJSON, &proposal.Impact); err != nil {
		return domain.EditProposal{}, err
	}
	if appliedValue.Valid {
		transactionID, err := domain.ParseTransactionID(appliedValue.String)
		if err != nil {
			return domain.EditProposal{}, err
		}
		proposal.AppliedTransactionID = &transactionID
	}
	return proposal, nil
}

func loadEditTransaction(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
) (domain.EditTransaction, error) {
	var idValue, proposalValue, projectValue, actorKind, actorValue, intent string
	var operationsJSON, inverseJSON, changesJSON, digestValue, committedValue string
	var projectRevision uint64
	var undoValue sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT id, proposal_id, project_id, actor_kind, actor_id, intent, operation_json,
       inverse_json, changes_json, project_revision, digest, undoes_transaction_id, committed_at
FROM edit_transactions WHERE id = ? AND project_id = ? AND digest IS NOT NULL`,
		transactionID.String(), projectID.String()).Scan(
		&idValue, &proposalValue, &projectValue, &actorKind, &actorValue, &intent,
		&operationsJSON, &inverseJSON, &changesJSON, &projectRevision,
		&digestValue, &undoValue, &committedValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EditTransaction{}, application.ErrTransactionNotFound
	}
	if err != nil {
		return domain.EditTransaction{}, err
	}
	id, _ := domain.ParseTransactionID(idValue)
	proposalID, _ := domain.ParseProposalID(proposalValue)
	project, _ := domain.ParseProjectID(projectValue)
	actor, err := parseStoredActor(actorKind, actorValue)
	if err != nil {
		return domain.EditTransaction{}, err
	}
	revision, err := domain.NewRevision(projectRevision)
	if err != nil {
		return domain.EditTransaction{}, err
	}
	digest, err := domain.ParseDigest(digestValue)
	if err != nil {
		return domain.EditTransaction{}, err
	}
	committedAt, err := time.Parse(time.RFC3339Nano, committedValue)
	if err != nil {
		return domain.EditTransaction{}, err
	}
	result := domain.EditTransaction{
		ID: id, ProposalID: proposalID, ProjectID: project, Actor: actor, Intent: intent,
		CommittedProjectRevision: revision, Digest: digest, CommittedAt: committedAt,
	}
	if err := decodeEditJSON(operationsJSON, &result.Operations); err != nil {
		return domain.EditTransaction{}, err
	}
	if err := decodeEditJSON(inverseJSON, &result.InverseOperations); err != nil {
		return domain.EditTransaction{}, err
	}
	if err := decodeEditJSON(changesJSON, &result.Changes); err != nil {
		return domain.EditTransaction{}, err
	}
	if undoValue.Valid {
		undoes, err := domain.ParseTransactionID(undoValue.String)
		if err != nil {
			return domain.EditTransaction{}, err
		}
		result.UndoesTransactionID = &undoes
	}
	return result, nil
}

func parseStoredActor(kind, value string) (domain.ActorRef, error) {
	switch domain.CreativeActor(kind) {
	case domain.ActorCreator:
		id, err := domain.ParseCreatorID(value)
		if err != nil {
			return domain.ActorRef{}, err
		}
		return domain.CreatorActor(id), nil
	case domain.ActorAgent:
		id, err := domain.ParseAgentID(value)
		if err != nil {
			return domain.ActorRef{}, err
		}
		return domain.AgentActor(id), nil
	default:
		return domain.ActorRef{}, domain.ErrInvalidCreativeActor
	}
}

func storedProposalStatus(value string) domain.ProposalStatus {
	switch value {
	case "pending", "approval-pending":
		return domain.ProposalOpen
	case "applied":
		return domain.ProposalApplied
	case "stale":
		return domain.ProposalStale
	case "cancelled":
		return domain.ProposalCancelled
	default:
		return ""
	}
}

func decodeEditJSON(value string, target any) error {
	return json.Unmarshal([]byte(value), target)
}
