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

const (
	creatorEditCommitRequest = "creator edit commit"
	creatorEditUndoRequest   = "creator edit undo"
)

func (repository *SQLiteProjects) CommitCreatorEdit(
	ctx context.Context,
	record application.CommitCreatorEditRecord,
) (application.EditCommitResult, error) {
	if err := validateCommitCreatorEditRecord(record); err != nil {
		return application.EditCommitResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.EditCommitResult{}, err
	}
	defer tx.Rollback()
	if replayed, err := loadEditCommitReplay(
		ctx, tx, record.Actor, record.RequestID, creatorEditCommitRequest,
		record.InputDigest, record.ProjectID,
	); err == nil {
		if err := tx.Commit(); err != nil {
			return application.EditCommitResult{}, err
		}
		replayed.Replayed = true
		return replayed, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return application.EditCommitResult{}, err
	}

	propose := application.ProposeEditRecord{
		ProjectID: record.ProjectID, SequenceID: record.SequenceID, Actor: record.Actor,
		RequestID: record.RequestID, ProposalID: record.ProposalID,
		Allocation: record.Allocation, Input: record.Input, CreatedAt: record.OccurredAt,
	}
	state, err := loadEditNormalizationState(ctx, tx, propose)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	proposal, canonical, err := application.NormalizeEditProposal(application.NormalizeEditInput{
		ProposalID: record.ProposalID, ProjectID: record.ProjectID, SequenceID: record.SequenceID,
		Actor: record.Actor, Allocation: record.Allocation, Input: record.Input,
		CreatedAt: record.OccurredAt, State: state,
	})
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if err := insertEditProposalRow(ctx, tx, proposal, canonical); err != nil {
		return application.EditCommitResult{}, err
	}
	transaction, err := prepareEditTransaction(
		ctx, tx, proposal, record.TransactionID, record.OccurredAt, nil,
	)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if err := persistAndApplyEditTransaction(
		ctx, tx, proposal, transaction, domain.RunID{}, domain.TurnID{},
	); err != nil {
		return application.EditCommitResult{}, err
	}
	if err := insertProposalApplication(
		ctx, tx, record.ApplicationID, proposal, record.Actor, record.RequestID,
		record.InputDigest, transaction.ID, record.OccurredAt,
	); err != nil {
		return application.EditCommitResult{}, err
	}
	if err := markEditProposalApplied(ctx, tx, proposal.ID, transaction.ID, record.OccurredAt); err != nil {
		return application.EditCommitResult{}, err
	}
	cursor, err := appendEditCommittedActivity(
		ctx, tx, transaction, record.ActivityEventID, domain.RunID{},
	)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	applicationID, transactionID := record.ApplicationID, transaction.ID
	if err := insertEditRequest(ctx, tx, editRequestRecord{
		Actor: record.Actor, RequestID: record.RequestID, Command: creatorEditCommitRequest,
		InputDigest: record.InputDigest, InputCanonical: record.InputCanonical,
		ProjectID: record.ProjectID, ProposalID: proposal.ID, ApplicationID: &applicationID,
		TransactionID: &transactionID, ActivityEventID: record.ActivityEventID,
		CreatedAt: record.OccurredAt,
	}); err != nil {
		return application.EditCommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.EditCommitResult{}, err
	}
	proposal.Status = domain.ProposalApplied
	proposal.AppliedTransactionID = &transactionID
	return application.EditCommitResult{
		Proposal: proposal, Transaction: transaction, ActivityCursor: cursor,
	}, nil
}

func (repository *SQLiteProjects) UndoCreatorEdit(
	ctx context.Context,
	record application.UndoCreatorEditRecord,
) (application.EditCommitResult, error) {
	if err := validateUndoCreatorEditRecord(record); err != nil {
		return application.EditCommitResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.EditCommitResult{}, err
	}
	defer tx.Rollback()
	if replayed, err := loadEditCommitReplay(
		ctx, tx, record.Actor, record.RequestID, creatorEditUndoRequest,
		record.InputDigest, record.ProjectID,
	); err == nil {
		if err := tx.Commit(); err != nil {
			return application.EditCommitResult{}, err
		}
		replayed.Replayed = true
		return replayed, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return application.EditCommitResult{}, err
	}
	if err := validateCreatorEditSequence(ctx, tx, record.ProjectID, record.SequenceID); err != nil {
		return application.EditCommitResult{}, err
	}
	target, err := loadEditTransaction(ctx, tx, record.ProjectID, record.TargetTransactionID)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if err := validateUndoTargetCurrent(ctx, tx, record.ProjectID, target); err != nil {
		return application.EditCommitResult{}, err
	}
	undo := application.UndoEditRecord{
		ProjectID: record.ProjectID, SequenceID: record.SequenceID, Actor: record.Actor,
		TargetTransactionID: record.TargetTransactionID, RequestID: record.RequestID,
		InputDigest: record.InputDigest, InputCanonical: record.InputCanonical,
		ProposalID: record.ProposalID, ApplicationID: record.ApplicationID,
		TransactionID: record.TransactionID, ActivityEventID: record.ActivityEventID,
		Input: record.Input, OccurredAt: record.OccurredAt,
	}
	proposal, canonical, err := buildUndoProposal(ctx, tx, undo, target)
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
	if err := persistAndApplyEditTransaction(
		ctx, tx, proposal, transaction, domain.RunID{}, domain.TurnID{},
	); err != nil {
		return application.EditCommitResult{}, err
	}
	if err := insertProposalApplication(
		ctx, tx, record.ApplicationID, proposal, record.Actor, record.RequestID,
		record.InputDigest, transaction.ID, record.OccurredAt,
	); err != nil {
		return application.EditCommitResult{}, err
	}
	if err := markEditProposalApplied(ctx, tx, proposal.ID, transaction.ID, record.OccurredAt); err != nil {
		return application.EditCommitResult{}, err
	}
	cursor, err := appendEditCommittedActivity(
		ctx, tx, transaction, record.ActivityEventID, domain.RunID{},
	)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	applicationID, transactionID := record.ApplicationID, transaction.ID
	if err := insertEditRequest(ctx, tx, editRequestRecord{
		Actor: record.Actor, RequestID: record.RequestID, Command: creatorEditUndoRequest,
		InputDigest: record.InputDigest, InputCanonical: record.InputCanonical,
		ProjectID: record.ProjectID, ProposalID: proposal.ID, ApplicationID: &applicationID,
		TransactionID: &transactionID, ActivityEventID: record.ActivityEventID,
		CreatedAt: record.OccurredAt,
	}); err != nil {
		return application.EditCommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.EditCommitResult{}, err
	}
	proposal.Status = domain.ProposalApplied
	proposal.AppliedTransactionID = &transactionID
	return application.EditCommitResult{
		Proposal: proposal, Transaction: transaction, ActivityCursor: cursor,
	}, nil
}

func markEditProposalApplied(
	ctx context.Context,
	tx *sql.Tx,
	proposalID domain.ProposalID,
	transactionID domain.TransactionID,
	at time.Time,
) error {
	_, err := tx.ExecContext(ctx, `
UPDATE edit_proposals SET status = 'applied', applied_transaction_id = ?, updated_at = ?
WHERE id = ? AND status = 'pending'`, transactionID.String(), formatInstant(at), proposalID.String())
	return err
}

func validateCommitCreatorEditRecord(record application.CommitCreatorEditRecord) error {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.ProposalID.IsZero() ||
		record.ApplicationID.IsZero() || record.TransactionID.IsZero() || record.ActivityEventID.IsZero() ||
		record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorCreator ||
		!json.Valid(record.InputCanonical) || record.OccurredAt.IsZero() {
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

func validateUndoCreatorEditRecord(record application.UndoCreatorEditRecord) error {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.TargetTransactionID.IsZero() ||
		record.ProposalID.IsZero() || record.ApplicationID.IsZero() || record.TransactionID.IsZero() ||
		record.ActivityEventID.IsZero() || record.Actor.Validate() != nil ||
		record.Actor.Kind != domain.ActorCreator || !json.Valid(record.InputCanonical) ||
		record.OccurredAt.IsZero() {
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
