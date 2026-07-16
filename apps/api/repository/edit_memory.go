package repository

import (
	"context"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

// The memory repository is an OpenAPI/controller fixture. Runtime editing is
// always composed with SQLiteProjects, whose journal and projections are the
// durable product truth.
func (repository *MemoryProjects) ProposeEdit(
	context.Context,
	application.ProposeEditRecord,
) (application.EditProposalResult, error) {
	return application.EditProposalResult{}, application.ErrEditInvalid
}

func (repository *MemoryProjects) ApplyEdit(
	context.Context,
	application.ApplyEditRecord,
) (application.EditCommitResult, error) {
	return application.EditCommitResult{}, application.ErrEditInvalid
}

func (repository *MemoryProjects) UndoEdit(
	context.Context,
	application.UndoEditRecord,
) (application.EditCommitResult, error) {
	return application.EditCommitResult{}, application.ErrEditInvalid
}

func (repository *MemoryProjects) CommitCreatorEdit(
	context.Context,
	application.CommitCreatorEditRecord,
) (application.EditCommitResult, error) {
	return application.EditCommitResult{}, application.ErrEditInvalid
}

func (repository *MemoryProjects) UndoCreatorEdit(
	context.Context,
	application.UndoCreatorEditRecord,
) (application.EditCommitResult, error) {
	return application.EditCommitResult{}, application.ErrEditInvalid
}

func (repository *MemoryProjects) ReadNarrativeSubtree(
	context.Context,
	application.NarrativeSubtreeQuery,
) (application.NarrativeSubtreeResult, error) {
	return application.NarrativeSubtreeResult{}, application.ErrEditEntityNotFound
}

func (repository *MemoryProjects) ReadCaptionDerivationPreview(
	context.Context,
	application.CaptionDerivationPreviewQuery,
) (application.CaptionDerivationPreview, error) {
	return application.CaptionDerivationPreview{}, application.ErrEditEntityNotFound
}

func (repository *MemoryProjects) ReadRoughCutDerivationPreview(
	context.Context,
	application.RoughCutDerivationPreviewQuery,
) (application.RoughCutDerivationPreview, error) {
	return application.RoughCutDerivationPreview{}, application.ErrEditEntityNotFound
}

func (repository *MemoryProjects) ReadCreatorTimelineGesturePreview(
	context.Context,
	application.CreatorTimelineGesturePreviewQuery,
) (application.CreatorTimelineGesturePreviewResult, error) {
	return application.CreatorTimelineGesturePreviewResult{}, application.ErrEditEntityNotFound
}

func (repository *MemoryProjects) ReadCreatorCaptionGesturePreview(
	context.Context,
	application.CreatorCaptionGesturePreviewQuery,
) (application.CreatorCaptionGesturePreview, error) {
	return application.CreatorCaptionGesturePreview{}, application.ErrEditEntityNotFound
}

func (repository *MemoryProjects) ReadCreatorClipPlacementPreview(
	context.Context,
	application.CreatorClipPlacementPreviewQuery,
) (application.CreatorClipPlacementPreview, error) {
	return application.CreatorClipPlacementPreview{}, application.ErrEditEntityNotFound
}

func (repository *MemoryProjects) ReadSequenceWindow(
	context.Context,
	application.SequenceWindowQuery,
) (application.SequenceWindowResult, error) {
	return application.SequenceWindowResult{}, application.ErrEditEntityNotFound
}

func (repository *MemoryProjects) ReadEditEntity(
	context.Context,
	domain.ProjectID,
	domain.EditEntityKind,
	string,
) (application.EditEntityDetail, error) {
	return application.EditEntityDetail{}, application.ErrEditEntityNotFound
}

func (repository *MemoryProjects) ReadEditProposal(
	context.Context,
	domain.ProjectID,
	domain.ProposalID,
) (domain.EditProposal, domain.Cursor, error) {
	return domain.EditProposal{}, 0, application.ErrProposalNotFound
}

func (repository *MemoryProjects) ReadTransactionHistory(
	context.Context,
	application.TransactionHistoryQuery,
) (application.TransactionHistoryResult, error) {
	return application.TransactionHistoryResult{}, application.ErrProjectNotFound
}

func (repository *MemoryProjects) ReadCreatorTransactionHistory(
	context.Context,
	application.CreatorTransactionHistoryQuery,
) (application.CreatorTransactionHistoryResult, error) {
	return application.CreatorTransactionHistoryResult{}, application.ErrProjectNotFound
}
