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

func (repository *SQLiteProjects) ApplyEdit(
	ctx context.Context,
	record application.ApplyEditRecord,
) (application.EditCommitResult, error) {
	if err := validateApplyEditRecord(record); err != nil {
		return application.EditCommitResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.EditCommitResult{}, err
	}
	defer tx.Rollback()
	if replayed, err := loadEditCommitReplay(ctx, tx, record.Actor, record.RequestID, "edit apply", record.InputDigest, record.ProjectID); err == nil {
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
	proposal, err := loadEditProposal(ctx, tx, record.ProjectID, record.ProposalID)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if proposal.Actor.IDString() != record.Actor.IDString() || proposal.RunID == nil || *proposal.RunID != record.RunID ||
		proposal.TurnID == nil || *proposal.TurnID != record.TurnID || proposal.Digest != record.Input.ProposalDigest {
		return application.EditCommitResult{}, application.ErrEditConflict
	}
	if proposal.Status == domain.ProposalApplied && proposal.AppliedTransactionID != nil {
		return repository.convergeAppliedProposal(ctx, tx, record, proposal)
	}
	if proposal.Status == domain.ProposalStale {
		return application.EditCommitResult{}, application.ErrProposalStale
	}
	if proposal.Status != domain.ProposalOpen {
		return application.EditCommitResult{}, application.ErrProposalTerminal
	}
	transaction, err := prepareEditTransaction(ctx, tx, proposal, record.TransactionID, record.OccurredAt, nil)
	if err != nil {
		if errors.Is(err, application.ErrEditConflict) {
			_, _ = tx.ExecContext(ctx, `UPDATE edit_proposals SET status = 'stale', updated_at = ? WHERE id = ?`,
				formatInstant(record.OccurredAt), proposal.ID.String())
		}
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
UPDATE edit_proposals SET status = 'applied', applied_transaction_id = ?, updated_at = ?
WHERE id = ? AND status = 'pending'`,
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
		Actor: record.Actor, RequestID: record.RequestID, Command: "edit apply",
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

func prepareEditTransaction(
	ctx context.Context,
	tx *sql.Tx,
	proposal domain.EditProposal,
	transactionID domain.TransactionID,
	committedAt time.Time,
	undoes *domain.TransactionID,
) (domain.EditTransaction, error) {
	if err := validateProposalPreconditions(ctx, tx, proposal); err != nil {
		return domain.EditTransaction{}, err
	}
	var currentProjectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, proposal.ProjectID.String()).Scan(&currentProjectRevision); err != nil {
		return domain.EditTransaction{}, err
	}
	projectRevision, err := domain.NewRevision(currentProjectRevision)
	if err != nil {
		return domain.EditTransaction{}, err
	}
	nextProjectRevision, err := projectRevision.Next()
	if err != nil {
		return domain.EditTransaction{}, err
	}
	changes := append([]domain.EntityRevisionChange(nil), proposal.Changes...)
	if proposalChangesNarrative(proposal.Operations) {
		change, err := currentAggregateChange(ctx, tx, proposal.ProjectID, domain.EntityNarrativeDocument)
		if err != nil {
			return domain.EditTransaction{}, err
		}
		changes = append(changes, change)
	}
	if proposalChangesSequence(proposal.Operations) {
		change, err := currentAggregateChange(ctx, tx, proposal.ProjectID, domain.EntitySequence)
		if err != nil {
			return domain.EditTransaction{}, err
		}
		changes = append(changes, change)
	}
	sortEntityChanges(changes)
	transaction := domain.EditTransaction{
		ID: transactionID, ProposalID: proposal.ID, ProjectID: proposal.ProjectID,
		Actor: proposal.Actor, Intent: proposal.Intent, Operations: proposal.Operations,
		InverseOperations: proposal.InversePreview, Changes: changes,
		CommittedProjectRevision: nextProjectRevision, UndoesTransactionID: undoes,
		CommittedAt: committedAt.UTC(),
	}
	_, digest, err := domain.CanonicalDigest("open-cut/edit-transaction", domain.EditTransactionSchema, struct {
		Actor                    domain.ActorRef                  `json:"actor"`
		Changes                  []domain.EntityRevisionChange    `json:"changes"`
		CommittedProjectRevision domain.Revision                  `json:"committedProjectRevision"`
		Intent                   string                           `json:"intent"`
		Inverse                  []domain.NormalizedEditOperation `json:"inverse"`
		Operations               []domain.NormalizedEditOperation `json:"operations"`
		ProjectID                domain.ProjectID                 `json:"projectId"`
		ProposalDigest           domain.Digest                    `json:"proposalDigest"`
		Undoes                   *domain.TransactionID            `json:"undoesTransactionId,omitempty"`
	}{
		Actor: transaction.Actor, Changes: transaction.Changes,
		CommittedProjectRevision: transaction.CommittedProjectRevision,
		Intent:                   transaction.Intent, Inverse: transaction.InverseOperations,
		Operations: transaction.Operations, ProjectID: transaction.ProjectID,
		ProposalDigest: proposal.Digest, Undoes: undoes,
	})
	if err != nil {
		return domain.EditTransaction{}, err
	}
	transaction.Digest = digest
	return transaction, nil
}

func persistAndApplyEditTransaction(
	ctx context.Context,
	tx *sql.Tx,
	proposal domain.EditProposal,
	transaction domain.EditTransaction,
	runID domain.RunID,
	turnID domain.TurnID,
) error {
	operations, _ := json.Marshal(transaction.Operations)
	inverse, _ := json.Marshal(transaction.InverseOperations)
	changes, _ := json.Marshal(transaction.Changes)
	var undoes any
	if transaction.UndoesTransactionID != nil {
		undoes = transaction.UndoesTransactionID.String()
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO edit_transactions (
  id, project_id, proposal_id, project_revision, schema_version, operation_json, inverse_json,
  actor_kind, actor_id, committed_at, run_id, turn_id, intent, digest, changes_json, undoes_transaction_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		transaction.ID.String(), transaction.ProjectID.String(), transaction.ProposalID.String(),
		transaction.CommittedProjectRevision.Value(), domain.EditTransactionSchema,
		string(operations), string(inverse), transaction.Actor.Kind, transaction.Actor.IDString(),
		formatInstant(transaction.CommittedAt), nullableRunID(runID), nullableTurnID(turnID), transaction.Intent,
		transaction.Digest.String(), string(changes), undoes,
	); err != nil {
		return fmt.Errorf("persist EditTransaction: %w", err)
	}
	changeIndex := make(map[string]domain.EntityRevisionChange, len(transaction.Changes))
	for _, change := range transaction.Changes {
		changeIndex[string(change.Kind)+"\x00"+change.ID] = change
	}
	for _, operation := range transaction.Operations {
		if err := applyNormalizedOperation(ctx, tx, transaction.ProjectID, transaction.ID, operation, changeIndex); err != nil {
			return err
		}
	}
	leafChanges := normalizedOperationEntityKeys(transaction.Operations)
	for _, change := range transaction.Changes {
		if _, leaf := leafChanges[string(change.Kind)+"\x00"+change.ID]; leaf {
			continue
		}
		switch change.Kind {
		case domain.EntityNarrativeDocument, domain.EntityNarrativeNode,
			domain.EntitySequence, domain.EntityTrack:
			if err := applyAggregateRevision(ctx, tx, transaction.ProjectID, change); err != nil {
				return err
			}
		}
	}
	current := transaction.CommittedProjectRevision.Value() - 1
	result, err := tx.ExecContext(ctx, `UPDATE projects SET revision = ? WHERE id = ? AND revision = ?`,
		transaction.CommittedProjectRevision.Value(), transaction.ProjectID.String(), current)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrEditConflict
	}
	return nil
}
