package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type editProposeHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	RunID      domain.RunID      `path:"runId"`
	TurnID     domain.TurnID     `path:"turnId"`
	Body       command.EditProposeInput
}

type editApplyHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	RunID      domain.RunID      `path:"runId"`
	TurnID     domain.TurnID     `path:"turnId"`
	ProposalID domain.ProposalID `path:"proposalId"`
	Body       command.EditApplyInput
}

type editUndoHTTPInput struct {
	ProjectID     domain.ProjectID     `path:"projectId"`
	SequenceID    domain.SequenceID    `path:"sequenceId"`
	RunID         domain.RunID         `path:"runId"`
	TurnID        domain.TurnID        `path:"turnId"`
	TransactionID domain.TransactionID `path:"transactionId"`
	Body          command.EditUndoInput
}

type editProposalHTTPOutput struct {
	Body command.EditProposalData
}

type editCommitHTTPOutput struct {
	Body command.EditCommitData
}

const editCommandBasePath = "/v1/projects/{projectId}/sequences/{sequenceId}/runs/{runId}/turns/{turnId}/edit"

func RegisterEditCommands(
	api huma.API,
	edits *application.Edits,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "propose-edit", Method: http.MethodPost,
		Path: editCommandBasePath + "/proposals", Summary: "Normalize and durably journal an Edit Proposal",
		Tags: []string{"editing"}, Middlewares: requireCommandBodyAuthority(api, runs, authorizer, "edit", "propose"),
		Extensions: commandExtensions("edit", "propose"),
	}, func(ctx context.Context, input *editProposeHTTPInput) (*editProposalHTTPOutput, error) {
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		result, err := edits.Propose(
			ctx, input.ProjectID, input.SequenceID, input.RunID, input.TurnID, input.Body,
		)
		if err != nil {
			return nil, editError(err)
		}
		return &editProposalHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "apply-edit-proposal", Method: http.MethodPost,
		Path:    editCommandBasePath + "/proposals/{proposalId}/apply",
		Summary: "Atomically apply an exact Edit Proposal", Tags: []string{"editing"},
		Middlewares: requireCommandBodyAuthority(api, runs, authorizer, "edit", "apply"),
		Extensions:  commandExtensions("edit", "apply"),
	}, func(ctx context.Context, input *editApplyHTTPInput) (*editCommitHTTPOutput, error) {
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		result, err := edits.Apply(
			ctx, input.ProjectID, input.SequenceID, input.RunID, input.TurnID,
			input.ProposalID, input.Body,
		)
		if err != nil {
			return nil, editError(err)
		}
		return &editCommitHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "undo-edit-transaction", Method: http.MethodPost,
		Path:    editCommandBasePath + "/transactions/{transactionId}/undo",
		Summary: "Commit the exact stored inverse of an Edit Transaction", Tags: []string{"editing"},
		Middlewares: requireCommandBodyAuthority(api, runs, authorizer, "edit", "undo"),
		Extensions:  commandExtensions("edit", "undo"),
	}, func(ctx context.Context, input *editUndoHTTPInput) (*editCommitHTTPOutput, error) {
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		result, err := edits.Undo(
			ctx, input.ProjectID, input.SequenceID, input.RunID, input.TurnID,
			input.TransactionID, input.Body,
		)
		if err != nil {
			return nil, editError(err)
		}
		return &editCommitHTTPOutput{Body: result}, nil
	})
}

func editError(err error) error {
	switch {
	case errors.Is(err, application.ErrEditStaleTurn), errors.Is(err, application.ErrRunStaleTurn):
		return huma.ErrorWithHeaders(
			huma.Error409Conflict("AgentTurn is stale", err),
			http.Header{command.StatusHeader: []string{string(command.StatusStaleTurn)}},
		)
	case errors.Is(err, application.ErrEditConflict), errors.Is(err, application.ErrProposalStale),
		errors.Is(err, application.ErrProposalTerminal), errors.Is(err, application.ErrEditRequestReused):
		return huma.ErrorWithHeaders(
			huma.Error409Conflict("edit conflicts with current state", err),
			http.Header{command.StatusHeader: []string{string(command.StatusConflict)}},
		)
	case errors.Is(err, application.ErrProjectNotFound), errors.Is(err, application.ErrRunNotFound),
		errors.Is(err, application.ErrProposalNotFound), errors.Is(err, application.ErrTransactionNotFound),
		errors.Is(err, application.ErrEditEntityNotFound):
		return commandStatusError(command.StatusNotFound, huma.Error404NotFound("edit resource not found"))
	case errors.Is(err, application.ErrEditInvalid), errors.Is(err, application.ErrInvalidEditCursor):
		return commandStatusError(command.StatusInvalid, huma.Error422UnprocessableEntity("edit request is invalid", err))
	case errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied), errors.Is(err, application.ErrRunActorMismatch):
		return huma.Error403Forbidden("edit authority denied")
	default:
		return commandStatusError(command.StatusFailed, huma.Error500InternalServerError("edit operation failed", err))
	}
}
