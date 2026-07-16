package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) convergeAppliedProposal(
	ctx context.Context,
	tx *sql.Tx,
	record application.ApplyEditRecord,
	proposal domain.EditProposal,
) (application.EditCommitResult, error) {
	transaction, err := loadEditTransaction(ctx, tx, proposal.ProjectID, *proposal.AppliedTransactionID)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if err := insertProposalApplication(ctx, tx, record.ApplicationID, proposal, record.Actor,
		record.RequestID, record.InputDigest, transaction.ID, record.OccurredAt); err != nil {
		return application.EditCommitResult{}, err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ProposalID        domain.ProposalID              `json:"proposalId"`
		TransactionID     domain.TransactionID           `json:"transactionId"`
	}{ChangedEntityRefs: []application.ChangedEntityRef{}, ProposalID: proposal.ID, TransactionID: transaction.ID})
	if err != nil {
		return application.EditCommitResult{}, err
	}
	var currentRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, proposal.ProjectID.String()).Scan(&currentRevision); err != nil {
		return application.EditCommitResult{}, err
	}
	cursor, err := appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: proposal.ProjectID.String(), EventID: record.ActivityEventID.String(),
		Kind: "edit.apply-converged", OccurredAt: formatInstant(record.OccurredAt),
		ActorKind: string(record.Actor.Kind), ActorID: record.Actor.IDString(),
		ProjectID: proposal.ProjectID.String(), ProjectRevision: int64(currentRevision),
		OutcomeKind: "transaction", OutcomeID: transaction.ID.String(),
		SummaryCode: "edit-apply-converged", Payload: payload,
	})
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
	return application.EditCommitResult{Proposal: proposal, Transaction: transaction, ActivityCursor: cursor}, nil
}
