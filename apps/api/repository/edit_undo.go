package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) UndoEdit(
	ctx context.Context,
	record application.UndoEditRecord,
) (application.EditCommitResult, error) {
	if err := validateUndoEditRecord(record); err != nil {
		return application.EditCommitResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.EditCommitResult{}, err
	}
	defer tx.Rollback()
	if replayed, err := loadEditCommitReplay(ctx, tx, record.Actor, record.RequestID, "edit undo", record.InputDigest, record.ProjectID); err == nil {
		if err := tx.Commit(); err != nil {
			return application.EditCommitResult{}, err
		}
		replayed.Replayed = true
		return replayed, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return application.EditCommitResult{}, err
	}
	if err := validateEditWriter(ctx, tx, record.ProjectID, record.SequenceID, record.RunID, record.TurnID, record.Actor); err != nil {
		return application.EditCommitResult{}, err
	}
	target, err := loadEditTransaction(ctx, tx, record.ProjectID, record.TargetTransactionID)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if err := validateUndoTargetCurrent(ctx, tx, record.ProjectID, target); err != nil {
		return application.EditCommitResult{}, err
	}
	proposal, canonical, err := buildUndoProposal(ctx, tx, record, target)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if err := insertEditProposalRow(ctx, tx, proposal, canonical); err != nil {
		return application.EditCommitResult{}, err
	}
	transaction, err := prepareEditTransaction(
		ctx, tx, proposal, record.TransactionID, record.OccurredAt, &record.TargetTransactionID,
	)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if err := persistAndApplyEditTransaction(ctx, tx, proposal, transaction, record.RunID, record.TurnID); err != nil {
		return application.EditCommitResult{}, err
	}
	if err := insertProposalApplication(ctx, tx, record.ApplicationID, proposal, record.Actor,
		record.RequestID, record.InputDigest, transaction.ID, record.OccurredAt); err != nil {
		return application.EditCommitResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE edit_proposals SET status = 'applied', applied_transaction_id = ?, updated_at = ? WHERE id = ?`,
		transaction.ID.String(), formatInstant(record.OccurredAt), proposal.ID.String()); err != nil {
		return application.EditCommitResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_run_transactions (run_id, transaction_id, created_at) VALUES (?, ?, ?)`,
		record.RunID.String(), transaction.ID.String(), formatInstant(record.OccurredAt)); err != nil {
		return application.EditCommitResult{}, err
	}
	cursor, err := appendEditCommittedActivity(ctx, tx, transaction, record.ActivityEventID, record.RunID)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	applicationID, transactionID := record.ApplicationID, transaction.ID
	if err := insertEditRequest(ctx, tx, editRequestRecord{
		Actor: record.Actor, RequestID: record.RequestID, Command: "edit undo",
		InputDigest: record.InputDigest, InputCanonical: record.InputCanonical,
		ProjectID: record.ProjectID, RunID: record.RunID, TurnID: record.TurnID,
		ProposalID: proposal.ID, ApplicationID: &applicationID, TransactionID: &transactionID,
		ActivityEventID: record.ActivityEventID, CreatedAt: record.OccurredAt,
	}); err != nil {
		return application.EditCommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.EditCommitResult{}, err
	}
	proposal.Status = domain.ProposalApplied
	proposal.AppliedTransactionID = &transactionID
	return application.EditCommitResult{Proposal: proposal, Transaction: transaction, ActivityCursor: cursor}, nil
}

func buildUndoProposal(
	ctx context.Context,
	tx *sql.Tx,
	record application.UndoEditRecord,
	target domain.EditTransaction,
) (domain.EditProposal, []byte, error) {
	current, err := operationRevisionMap(ctx, tx, record.ProjectID, target.InverseOperations)
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	operations, finalRevisions, err := rebaseOperationSet(ctx, tx, record.ProjectID, target.InverseOperations, current)
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	inverse, _, err := rebaseOperationSet(ctx, tx, record.ProjectID, target.Operations, finalRevisions)
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	changes, err := undoChanges(ctx, tx, record.ProjectID, target, operations)
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	var projectRevisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, record.ProjectID.String()).Scan(&projectRevisionValue); err != nil {
		return domain.EditProposal{}, nil, err
	}
	projectRevision, err := domain.NewRevision(projectRevisionValue)
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	intent := record.Input.Intent
	if intent == "" {
		intent = "undo " + target.ID.String()
	}
	preconditions := make([]domain.EntityPrecondition, 0, len(changes))
	for _, change := range changes {
		if change.Before != nil {
			preconditions = append(preconditions, domain.EntityPrecondition{
				Kind: change.Kind, ID: change.ID, Revision: *change.Before,
			})
		}
	}
	var runID *domain.RunID
	var turnID *domain.TurnID
	if !record.RunID.IsZero() {
		value := record.RunID
		runID = &value
	}
	if !record.TurnID.IsZero() {
		value := record.TurnID
		turnID = &value
	}
	proposal := domain.EditProposal{
		ID: record.ProposalID, ProjectID: record.ProjectID, SequenceID: &record.SequenceID,
		RunID: runID, TurnID: turnID, RequestID: record.RequestID,
		Actor: record.Actor, Intent: intent, BaseProjectRevision: projectRevision,
		Preconditions: preconditions, Allocation: []domain.LocalAllocation{},
		Operations: operations, InversePreview: inverse, Changes: changes,
		Impact: domain.EditImpact{Classifier: domain.EditImpactClassifierV1, Class: "reversible-local"},
		Status: domain.ProposalOpen, CreatedAt: record.OccurredAt.UTC(),
	}
	canonical, digest, err := domain.CanonicalDigest("open-cut/edit-proposal", domain.EditProposalSchema, struct {
		Actor               domain.ActorRef                  `json:"actor"`
		Allocation          []domain.LocalAllocation         `json:"allocation"`
		BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
		Changes             []domain.EntityRevisionChange    `json:"changes"`
		Impact              domain.EditImpact                `json:"impact"`
		Intent              string                           `json:"intent"`
		Inverse             []domain.NormalizedEditOperation `json:"inverse"`
		Operations          []domain.NormalizedEditOperation `json:"operations"`
		Preconditions       []domain.EntityPrecondition      `json:"preconditions"`
		ProjectID           domain.ProjectID                 `json:"projectId"`
		RunID               *domain.RunID                    `json:"runId,omitempty"`
		SequenceID          domain.SequenceID                `json:"sequenceId"`
		TurnID              *domain.TurnID                   `json:"turnId,omitempty"`
		Undoes              domain.TransactionID             `json:"undoesTransactionId"`
	}{
		Actor: proposal.Actor, Allocation: proposal.Allocation,
		BaseProjectRevision: proposal.BaseProjectRevision,
		Changes:             proposal.Changes, Impact: proposal.Impact, Intent: proposal.Intent,
		Inverse: proposal.InversePreview, Operations: proposal.Operations,
		Preconditions: proposal.Preconditions, ProjectID: proposal.ProjectID,
		RunID: proposal.RunID, SequenceID: *proposal.SequenceID, TurnID: proposal.TurnID,
		Undoes: target.ID,
	})
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	proposal.Digest = digest
	return proposal, canonical, nil
}

func insertEditProposalRow(
	ctx context.Context,
	tx *sql.Tx,
	proposal domain.EditProposal,
	canonical []byte,
) error {
	preconditions, _ := json.Marshal(proposal.Preconditions)
	allocation, _ := json.Marshal(proposal.Allocation)
	operations, _ := json.Marshal(proposal.Operations)
	inverse, _ := json.Marshal(proposal.InversePreview)
	changes, _ := json.Marshal(proposal.Changes)
	impact, _ := json.Marshal(proposal.Impact)
	_, err := tx.ExecContext(ctx, `
INSERT INTO edit_proposals (
  id, project_id, schema_version, digest, canonical_json, actor_kind, actor_id, status, created_at,
  request_id, sequence_id, run_id, turn_id, base_project_revision, intent, preconditions_json,
  allocation_json, operations_json, inverse_preview_json, changes_json, impact_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		proposal.ID.String(), proposal.ProjectID.String(), domain.EditProposalSchema,
		proposal.Digest.String(), string(canonical), proposal.Actor.Kind, proposal.Actor.IDString(),
		formatInstant(proposal.CreatedAt), proposal.RequestID.String(), proposal.SequenceID.String(),
		nullableProposalRunID(proposal.RunID), nullableProposalTurnID(proposal.TurnID),
		proposal.BaseProjectRevision.Value(), proposal.Intent,
		string(preconditions), string(allocation), string(operations), string(inverse),
		string(changes), string(impact), formatInstant(proposal.CreatedAt))
	if err != nil {
		return fmt.Errorf("persist undo EditProposal: %w", err)
	}
	return nil
}
