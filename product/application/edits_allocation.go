package application

import (
	"context"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func (edits *Edits) allocateProposal(
	ctx context.Context,
	at time.Time,
	operations []EditOperationInput,
) (domain.ProposalID, domain.ActivityEventID, []domain.LocalAllocation, error) {
	proposalValue, err := edits.identities.NewID(ctx, at)
	if err != nil {
		return domain.ProposalID{}, domain.ActivityEventID{}, nil, err
	}
	proposalID, err := domain.ParseProposalID(proposalValue)
	if err != nil {
		return domain.ProposalID{}, domain.ActivityEventID{}, nil, err
	}
	eventID, err := edits.newActivityEventID(ctx, at)
	if err != nil {
		return domain.ProposalID{}, domain.ActivityEventID{}, nil, err
	}
	allocation := make([]domain.LocalAllocation, 0)
	for _, operation := range operations {
		if operation.CreateAs != nil {
			value, err := edits.identities.NewID(ctx, at)
			if err != nil {
				return domain.ProposalID{}, domain.ActivityEventID{}, nil, err
			}
			allocation = append(allocation, domain.LocalAllocation{
				Local: *operation.CreateAs, Kind: createdKind(operation.Type), ID: value,
			})
		}
		locals := make([]struct {
			local *domain.LocalID
			kind  domain.EditEntityKind
		}, 0, 4+len(operation.SplitOutputs)*2)
		locals = append(locals,
			struct {
				local *domain.LocalID
				kind  domain.EditEntityKind
			}{operation.CreateLinkGroupAs, domain.EntityLinkGroup},
			struct {
				local *domain.LocalID
				kind  domain.EditEntityKind
			}{operation.LeftLinkGroupAs, domain.EntityLinkGroup},
			struct {
				local *domain.LocalID
				kind  domain.EditEntityKind
			}{operation.RightLinkGroupAs, domain.EntityLinkGroup},
		)
		for index := range operation.SplitOutputs {
			locals = append(locals,
				struct {
					local *domain.LocalID
					kind  domain.EditEntityKind
				}{&operation.SplitOutputs[index].LeftAs, domain.EntityClip},
				struct {
					local *domain.LocalID
					kind  domain.EditEntityKind
				}{&operation.SplitOutputs[index].RightAs, domain.EntityClip},
			)
		}
		for _, identity := range locals {
			if identity.local == nil {
				continue
			}
			value, err := edits.identities.NewID(ctx, at)
			if err != nil {
				return domain.ProposalID{}, domain.ActivityEventID{}, nil, err
			}
			allocation = append(allocation, domain.LocalAllocation{
				Local: *identity.local, Kind: identity.kind, ID: value,
			})
		}
		for _, output := range operation.DerivedCaptions {
			for _, identity := range []struct {
				local domain.LocalID
				kind  domain.EditEntityKind
			}{{output.CaptionAs, domain.EntityCaption}, {output.AlignmentAs, domain.EntityAlignment}} {
				value, err := edits.identities.NewID(ctx, at)
				if err != nil {
					return domain.ProposalID{}, domain.ActivityEventID{}, nil, err
				}
				allocation = append(allocation, domain.LocalAllocation{
					Local: identity.local, Kind: identity.kind, ID: value,
				})
			}
		}
		for _, output := range operation.DerivedRoughCut {
			identities := []struct {
				local domain.LocalID
				kind  domain.EditEntityKind
			}{{output.AlignmentAs, domain.EntityAlignment}}
			if output.Video != nil {
				identities = append(identities, struct {
					local domain.LocalID
					kind  domain.EditEntityKind
				}{output.Video.ClipAs, domain.EntityClip})
			}
			if output.Audio != nil {
				identities = append(identities, struct {
					local domain.LocalID
					kind  domain.EditEntityKind
				}{output.Audio.ClipAs, domain.EntityClip})
			}
			if output.LinkGroupAs != nil {
				identities = append(identities, struct {
					local domain.LocalID
					kind  domain.EditEntityKind
				}{*output.LinkGroupAs, domain.EntityLinkGroup})
			}
			for _, identity := range identities {
				value, err := edits.identities.NewID(ctx, at)
				if err != nil {
					return domain.ProposalID{}, domain.ActivityEventID{}, nil, err
				}
				allocation = append(allocation, domain.LocalAllocation{
					Local: identity.local, Kind: identity.kind, ID: value,
				})
			}
		}
	}
	return proposalID, eventID, allocation, nil
}

func (edits *Edits) allocateApply(
	ctx context.Context,
	at time.Time,
) (domain.ProposalApplicationID, domain.TransactionID, domain.ActivityEventID, error) {
	applicationValue, err := edits.identities.NewID(ctx, at)
	if err != nil {
		return domain.ProposalApplicationID{}, domain.TransactionID{}, domain.ActivityEventID{}, err
	}
	applicationID, err := domain.ParseProposalApplicationID(applicationValue)
	if err != nil {
		return domain.ProposalApplicationID{}, domain.TransactionID{}, domain.ActivityEventID{}, err
	}
	transactionValue, err := edits.identities.NewID(ctx, at)
	if err != nil {
		return domain.ProposalApplicationID{}, domain.TransactionID{}, domain.ActivityEventID{}, err
	}
	transactionID, err := domain.ParseTransactionID(transactionValue)
	if err != nil {
		return domain.ProposalApplicationID{}, domain.TransactionID{}, domain.ActivityEventID{}, err
	}
	eventID, err := edits.newActivityEventID(ctx, at)
	return applicationID, transactionID, eventID, err
}

func (edits *Edits) allocateUndo(
	ctx context.Context,
	at time.Time,
) (domain.ProposalID, domain.ProposalApplicationID, domain.TransactionID, domain.ActivityEventID, error) {
	proposalValue, err := edits.identities.NewID(ctx, at)
	if err != nil {
		return domain.ProposalID{}, domain.ProposalApplicationID{}, domain.TransactionID{}, domain.ActivityEventID{}, err
	}
	proposalID, err := domain.ParseProposalID(proposalValue)
	if err != nil {
		return domain.ProposalID{}, domain.ProposalApplicationID{}, domain.TransactionID{}, domain.ActivityEventID{}, err
	}
	applicationID, transactionID, eventID, err := edits.allocateApply(ctx, at)
	return proposalID, applicationID, transactionID, eventID, err
}

func (edits *Edits) newActivityEventID(ctx context.Context, at time.Time) (domain.ActivityEventID, error) {
	value, err := edits.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(value)
}
