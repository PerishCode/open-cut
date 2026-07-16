package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type creatorEditCommitHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       application.EditProposeInput
}

type creatorEditUndoHTTPInput struct {
	ProjectID     domain.ProjectID     `path:"projectId"`
	SequenceID    domain.SequenceID    `path:"sequenceId"`
	TransactionID domain.TransactionID `path:"transactionId"`
	Body          application.EditUndoInput
}

type creatorEditCommitHTTPOutput struct {
	Body application.EditCommitResult
}

func RegisterCreatorEdits(
	api huma.API,
	edits *application.Edits,
	authorizer service.Authorizer,
) {
	creatorSurface := map[string]any{"x-open-cut-surface": "first-party-creator"}
	huma.Register(api, huma.Operation{
		OperationID: "commit-creator-edit", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/edits",
		Summary: "Normalize and atomically commit one Creator edit",
		Tags:    []string{"editing"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: creatorSurface,
	}, func(ctx context.Context, input *creatorEditCommitHTTPInput) (*creatorEditCommitHTTPOutput, error) {
		result, err := edits.CommitForCreator(ctx, input.ProjectID, input.SequenceID, input.Body)
		if err != nil {
			return nil, creatorEditError(err)
		}
		return &creatorEditCommitHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "undo-creator-edit-transaction", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/transactions/{transactionId}/undo",
		Summary: "Atomically commit the stored inverse of one transaction as Creator",
		Tags:    []string{"editing"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: creatorSurface,
	}, func(ctx context.Context, input *creatorEditUndoHTTPInput) (*creatorEditCommitHTTPOutput, error) {
		result, err := edits.UndoForCreator(
			ctx, input.ProjectID, input.SequenceID, input.TransactionID, input.Body,
		)
		if err != nil {
			return nil, creatorEditError(err)
		}
		return &creatorEditCommitHTTPOutput{Body: result}, nil
	})
}

func creatorEditError(err error) error {
	switch {
	case errors.Is(err, application.ErrEditConflict), errors.Is(err, application.ErrProposalStale),
		errors.Is(err, application.ErrProposalTerminal), errors.Is(err, application.ErrEditRequestReused):
		return huma.Error409Conflict("edit conflicts with current state", err)
	case errors.Is(err, application.ErrProjectNotFound), errors.Is(err, application.ErrProposalNotFound),
		errors.Is(err, application.ErrTransactionNotFound), errors.Is(err, application.ErrEditEntityNotFound):
		return huma.Error404NotFound("edit resource not found")
	case errors.Is(err, application.ErrEditInvalid), errors.Is(err, application.ErrInvalidEditCursor):
		return huma.Error422UnprocessableEntity("edit request is invalid", err)
	case errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("edit authority denied")
	default:
		return huma.Error500InternalServerError("edit operation failed", err)
	}
}
