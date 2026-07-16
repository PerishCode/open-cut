package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func validateApplyEditRecord(record application.ApplyEditRecord) error {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.RunID.IsZero() ||
		record.TurnID.IsZero() || record.ProposalID.IsZero() || record.ApplicationID.IsZero() ||
		record.TransactionID.IsZero() || record.ActivityEventID.IsZero() ||
		record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorAgent ||
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

func loadEditCommitReplay(
	ctx context.Context,
	tx *sql.Tx,
	actor domain.ActorRef,
	requestID domain.RequestID,
	commandName string,
	digest domain.Digest,
	projectID domain.ProjectID,
) (application.EditCommitResult, error) {
	stored, err := loadEditRequest(ctx, tx, actor, requestID)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	if stored.Command != commandName || stored.InputDigest != digest || stored.ProjectID != projectID ||
		stored.TransactionID == nil {
		return application.EditCommitResult{}, application.ErrEditRequestReused
	}
	proposal, err := loadEditProposal(ctx, tx, projectID, stored.ProposalID)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	transaction, err := loadEditTransaction(ctx, tx, projectID, *stored.TransactionID)
	if err != nil {
		return application.EditCommitResult{}, err
	}
	cursor, err := activityCursorForEvent(ctx, tx, stored.ActivityEventID.String())
	if err != nil {
		return application.EditCommitResult{}, err
	}
	return application.EditCommitResult{Proposal: proposal, Transaction: transaction, ActivityCursor: cursor}, nil
}

func validateProposalPreconditions(ctx context.Context, tx *sql.Tx, proposal domain.EditProposal) error {
	for _, precondition := range proposal.Preconditions {
		current, err := loadEditEntityRevision(ctx, tx, proposal.ProjectID, precondition.Kind, precondition.ID)
		if err != nil || current != precondition.Revision {
			return application.ErrEditConflict
		}
	}
	for _, change := range proposal.Changes {
		if change.Before == nil {
			exists, err := editEntityExists(ctx, tx, proposal.ProjectID, change.Kind, change.ID)
			if err != nil {
				return err
			}
			if exists {
				return application.ErrEditConflict
			}
			continue
		}
		current, err := loadEditEntityRevision(ctx, tx, proposal.ProjectID, change.Kind, change.ID)
		if err != nil || current != *change.Before {
			return application.ErrEditConflict
		}
	}
	return nil
}

func editEntityExists(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	kind domain.EditEntityKind,
	id string,
) (bool, error) {
	_, err := loadEditEntityRevision(ctx, tx, projectID, kind, id)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, application.ErrEditInvalid) {
		return false, nil
	}
	return false, err
}

func currentAggregateChange(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	kind domain.EditEntityKind,
) (domain.EntityRevisionChange, error) {
	var id string
	var revisionValue uint64
	var query string
	switch kind {
	case domain.EntityNarrativeDocument:
		query = `SELECT d.id, d.revision FROM projects p JOIN narrative_documents d ON d.id = p.narrative_document_id WHERE p.id = ?`
	case domain.EntitySequence:
		query = `SELECT s.id, s.revision FROM projects p JOIN sequences s ON s.id = p.main_sequence_id WHERE p.id = ?`
	default:
		return domain.EntityRevisionChange{}, application.ErrEditInvalid
	}
	if err := tx.QueryRowContext(ctx, query, projectID.String()).Scan(&id, &revisionValue); err != nil {
		return domain.EntityRevisionChange{}, err
	}
	before, err := domain.NewRevision(revisionValue)
	if err != nil {
		return domain.EntityRevisionChange{}, err
	}
	after, err := before.Next()
	if err != nil {
		return domain.EntityRevisionChange{}, err
	}
	copyBefore := before
	return domain.EntityRevisionChange{Kind: kind, ID: id, Before: &copyBefore, After: after}, nil
}

func proposalChangesNarrative(operations []domain.NormalizedEditOperation) bool {
	for _, operation := range operations {
		if operation.Type == domain.NormalizedPutNarrativeNode {
			return true
		}
	}
	return false
}

func proposalChangesSequence(operations []domain.NormalizedEditOperation) bool {
	for _, operation := range operations {
		if operation.Type == domain.NormalizedPutCaption || operation.Type == domain.NormalizedPutClip ||
			operation.Type == domain.NormalizedPutLinkGroup {
			return true
		}
	}
	return false
}

func sortEntityChanges(changes []domain.EntityRevisionChange) {
	sort.Slice(changes, func(left, right int) bool {
		if changes[left].Kind != changes[right].Kind {
			return changes[left].Kind < changes[right].Kind
		}
		return changes[left].ID < changes[right].ID
	})
}

func insertProposalApplication(
	ctx context.Context,
	tx *sql.Tx,
	id domain.ProposalApplicationID,
	proposal domain.EditProposal,
	actor domain.ActorRef,
	requestID domain.RequestID,
	digest domain.Digest,
	transactionID domain.TransactionID,
	at time.Time,
) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO proposal_applications (
  id, project_id, proposal_id, actor_kind, actor_id, request_id, input_digest,
  status, transaction_id, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'committed', ?, ?, ?)`,
		id.String(), proposal.ProjectID.String(), proposal.ID.String(), actor.Kind, actor.IDString(),
		requestID.String(), digest.String(), transactionID.String(), formatInstant(at), formatInstant(at))
	if err != nil {
		return fmt.Errorf("persist ProposalApplication: %w", err)
	}
	return nil
}

func appendEditCommittedActivity(
	ctx context.Context,
	tx *sql.Tx,
	transaction domain.EditTransaction,
	eventID domain.ActivityEventID,
	runID domain.RunID,
) (domain.Cursor, error) {
	changed := make([]application.ChangedEntityRef, 0, len(transaction.Changes))
	for _, change := range transaction.Changes {
		changed = append(changed, application.ChangedEntityRef{Kind: string(change.Kind), ID: change.ID, Revision: change.After})
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ProposalID        domain.ProposalID              `json:"proposalId"`
		RunID             *domain.RunID                  `json:"runId,omitempty"`
		TransactionID     domain.TransactionID           `json:"transactionId"`
	}{ChangedEntityRefs: changed, ProposalID: transaction.ProposalID,
		RunID: optionalRunID(runID), TransactionID: transaction.ID})
	if err != nil {
		return 0, err
	}
	return appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: transaction.ProjectID.String(), EventID: eventID.String(),
		Kind: "edit.committed", OccurredAt: formatInstant(transaction.CommittedAt),
		ActorKind: string(transaction.Actor.Kind), ActorID: transaction.Actor.IDString(),
		ProjectID: transaction.ProjectID.String(), ProjectRevision: int64(transaction.CommittedProjectRevision.Value()),
		OutcomeKind: "transaction", OutcomeID: transaction.ID.String(), SummaryCode: "edit-committed", Payload: payload,
	})
}

func optionalRunID(value domain.RunID) *domain.RunID {
	if value.IsZero() {
		return nil
	}
	result := value
	return &result
}

func nullableRunID(value domain.RunID) any {
	if value.IsZero() {
		return nil
	}
	return value.String()
}

func nullableTurnID(value domain.TurnID) any {
	if value.IsZero() {
		return nil
	}
	return value.String()
}

func nullableProposalRunID(value *domain.RunID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func nullableProposalTurnID(value *domain.TurnID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func normalizedOperationEntityKeys(operations []domain.NormalizedEditOperation) map[string]struct{} {
	result := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		switch operation.Type {
		case domain.NormalizedPutNarrativeNode:
			result[string(domain.EntityNarrativeNode)+"\x00"+operation.NarrativeNode.ID().String()] = struct{}{}
		case domain.NormalizedPutCaption:
			result[string(domain.EntityCaption)+"\x00"+operation.Caption.ID.String()] = struct{}{}
		case domain.NormalizedPutAlignment:
			result[string(domain.EntityAlignment)+"\x00"+operation.Alignment.ID.String()] = struct{}{}
		case domain.NormalizedPutAsset:
			result[string(domain.EntityAsset)+"\x00"+operation.Asset.ID.String()] = struct{}{}
		case domain.NormalizedPutClip:
			result[string(domain.EntityClip)+"\x00"+operation.Clip.ID.String()] = struct{}{}
		case domain.NormalizedPutLinkGroup:
			result[string(domain.EntityLinkGroup)+"\x00"+operation.LinkGroup.ID.String()] = struct{}{}
		case domain.NormalizedPutTranscriptCorrection:
			result[string(domain.EntityTranscriptCorrection)+"\x00"+operation.TranscriptCorrection.ID.String()] = struct{}{}
		}
	}
	return result
}
