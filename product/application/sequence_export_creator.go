package application

import (
	"context"

	"github.com/PerishCode/open-cut/product/domain"
)

func (exports *SequenceExports) StartForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input SequenceExportStartInput,
) (SequenceExportResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || input.Validate() != nil {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	return exports.start(ctx, projectID, sequenceID, input, authority.Actor, SequenceExportOwner{
		Kind: SequenceExportOwnerCreator, ID: authority.Actor.IDString(),
	}, domain.RunID{}, domain.TurnID{})
}

func (exports *SequenceExports) ShowForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	input SequenceExportShowInput,
) (SequenceExportResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if projectID.IsZero() || input.JobID.IsZero() {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	return exports.repository.ReadSequenceExport(ctx, ReadSequenceExportRecord{
		ProjectID: projectID, Actor: authority.Actor,
		Owner: SequenceExportOwner{Kind: SequenceExportOwnerCreator, ID: authority.Actor.IDString()}, JobID: input.JobID,
	})
}

func (exports *SequenceExports) RetryForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	input SequenceExportRetryInput,
) (SequenceExportResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if projectID.IsZero() || input.JobID.IsZero() {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	return exports.retry(ctx, ReadSequenceExportRecord{
		ProjectID: projectID, Actor: authority.Actor,
		Owner: SequenceExportOwner{Kind: SequenceExportOwnerCreator, ID: authority.Actor.IDString()}, JobID: input.JobID,
	})
}

func (exports *SequenceExports) CancelForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	input SequenceExportCancelInput,
) (SequenceExportResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if projectID.IsZero() || input.JobID.IsZero() {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	return exports.cancel(ctx, ReadSequenceExportRecord{
		ProjectID: projectID, Actor: authority.Actor,
		Owner: SequenceExportOwner{Kind: SequenceExportOwnerCreator, ID: authority.Actor.IDString()}, JobID: input.JobID,
	}, input.RequestID)
}
