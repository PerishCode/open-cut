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

type sequenceFramesHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	RunID      domain.RunID      `path:"runId"`
	TurnID     domain.TurnID     `path:"turnId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       command.SequenceFramesInput
}

type sequenceFramesHTTPOutput struct {
	Body command.SequenceFramesData
}

func RegisterSequenceFrames(
	api huma.API,
	frames *application.SequenceFrames,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "inspect-sequence-frames", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/sequences/{sequenceId}/frames",
		Summary: "Inspect bounded exact frames of one committed Sequence revision",
		Tags:    []string{"sequences"}, Middlewares: requireCommandAuthority(api, runs, authorizer, "sequence", "frames"),
		Extensions: commandExtensions("sequence", "frames"),
	}, func(ctx context.Context, input *sequenceFramesHTTPInput) (*sequenceFramesHTTPOutput, error) {
		if frames == nil {
			return nil, commandStatusError(command.StatusUnavailable, huma.Error503ServiceUnavailable("sequence frame inspection is unavailable"))
		}
		applicationInput, err := input.Body.ApplicationInput()
		if err != nil {
			return nil, sequenceFramesError(err)
		}
		result, err := frames.Execute(
			ctx, input.ProjectID, input.SequenceID, input.RunID, input.TurnID, applicationInput,
		)
		if err != nil {
			return nil, sequenceFramesError(err)
		}
		data := command.SequenceFramesDataFrom(result)
		status := application.CommandReceiptSucceeded
		if result.Status == application.SequenceFrameSetAccepted {
			status = application.CommandReceiptAccepted
		}
		refs := []application.CommandReceiptRef{
			commandReceiptRef("sequence", result.SequenceID.String(), result.SequenceRevision),
			commandReceiptRef("work-job", result.Job.ID.String(), 0),
		}
		if result.ArtifactID != nil {
			refs = append(refs, commandReceiptRef("artifact", result.ArtifactID.String(), 0))
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, data, status, refs, 0, result.ActivityCursor,
		); err != nil {
			return nil, sequenceFramesError(err)
		}
		return &sequenceFramesHTTPOutput{Body: data}, nil
	})
}

func sequenceFramesError(err error) error {
	switch {
	case errors.Is(err, application.ErrSequenceFramesNotFound),
		errors.Is(err, application.ErrSequencePreviewNotFound),
		errors.Is(err, application.ErrProjectNotFound):
		return commandStatusError(command.StatusNotFound, huma.Error404NotFound("sequence frame resource not found"))
	case errors.Is(err, application.ErrRunStaleTurn):
		return huma.ErrorWithHeaders(
			huma.Error409Conflict("sequence frame request has a stale AgentTurn", err),
			http.Header{command.StatusHeader: []string{string(command.StatusStaleTurn)}},
		)
	case errors.Is(err, application.ErrSequenceFramesRecovery), errors.Is(err, application.ErrEditConflict):
		return commandStatusError(command.StatusConflict, huma.Error409Conflict("sequence frame recovery conflicts with current state", err))
	case errors.Is(err, application.ErrSequenceFramesInvalid),
		errors.Is(err, application.ErrSequencePreviewInvalid):
		return commandStatusError(command.StatusInvalid, huma.Error422UnprocessableEntity("sequence frame request is invalid", err))
	case errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("sequence frame authority denied")
	default:
		return commandStatusError(command.StatusFailed, huma.Error500InternalServerError("sequence frame operation failed", err))
	}
}
