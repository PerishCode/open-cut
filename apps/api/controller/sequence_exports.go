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

type exportStartHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	RunID      domain.RunID      `path:"runId"`
	TurnID     domain.TurnID     `path:"turnId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       command.ExportStartInput
}

type exportJobHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	TurnID    domain.TurnID    `path:"turnId"`
	JobID     domain.WorkJobID `path:"jobId"`
}

type exportCancelHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	TurnID    domain.TurnID    `path:"turnId"`
	JobID     domain.WorkJobID `path:"jobId"`
	Body      command.ExportCancelInput
}

type exportRetryHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	TurnID    domain.TurnID    `path:"turnId"`
	JobID     domain.WorkJobID `path:"jobId"`
	Body      command.ExportRetryInput
}

type exportHTTPOutput struct {
	Body command.ExportData
}

type creatorExportStartHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       command.ExportStartInput
}

type creatorExportJobHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	JobID     domain.WorkJobID `path:"jobId"`
}

type creatorExportCancelInput struct {
	RequestID domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
}

type creatorExportCancelHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	JobID     domain.WorkJobID `path:"jobId"`
	Body      creatorExportCancelInput
}

type creatorExportHistoryHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	After     string           `query:"after" maxLength:"512"`
	Limit     uint16           `query:"limit" minimum:"1" maximum:"50" default:"20"`
}

type creatorExportHistoryHTTPOutput struct {
	Body command.ExportHistoryData
}

type creatorExportDeleteHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	JobID     domain.WorkJobID `path:"jobId"`
	Body      application.SequenceExportDeleteArtifactInput
}

func RegisterSequenceExports(
	api huma.API,
	exports *application.SequenceExports,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	creatorSurface := map[string]any{"x-open-cut-surface": "first-party-creator"}
	huma.Register(api, huma.Operation{
		OperationID: "list-creator-sequence-exports", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/exports",
		Summary: "List bounded project export lineages as Creator", Tags: []string{"exports"},
		Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *creatorExportHistoryHTTPInput) (*creatorExportHistoryHTTPOutput, error) {
		if exports == nil {
			return nil, huma.Error503ServiceUnavailable("sequence export is unavailable")
		}
		result, err := exports.ListForCreator(ctx, input.ProjectID, application.ListSequenceExportHistoryInput{
			After: input.After, Limit: input.Limit,
		})
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &creatorExportHistoryHTTPOutput{Body: command.ExportHistoryDataFrom(result)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "start-creator-sequence-export", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/exports",
		Summary: "Start one Creator-owned pinned full-quality Sequence export", Tags: []string{"exports"},
		Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *creatorExportStartHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, huma.Error503ServiceUnavailable("sequence export is unavailable")
		}
		applicationInput, err := input.Body.ApplicationInput()
		if err != nil {
			return nil, sequenceExportError(err)
		}
		result, err := exports.StartForCreator(ctx, input.ProjectID, input.SequenceID, applicationInput)
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "show-creator-sequence-export", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/exports/{jobId}",
		Summary: "Show one project export lineage as Creator", Tags: []string{"exports"},
		Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *creatorExportJobHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, huma.Error503ServiceUnavailable("sequence export is unavailable")
		}
		result, err := exports.ShowForCreator(
			ctx, input.ProjectID, application.SequenceExportShowInput{JobID: input.JobID},
		)
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "retry-creator-sequence-export", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/exports/{jobId}/retry",
		Summary: "Retry one recoverable project export lineage as Creator", Tags: []string{"exports"},
		Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *creatorExportJobHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, huma.Error503ServiceUnavailable("sequence export is unavailable")
		}
		result, err := exports.RetryForCreator(
			ctx, input.ProjectID, application.SequenceExportRetryInput{JobID: input.JobID},
		)
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "cancel-creator-sequence-export", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/exports/{jobId}/cancel",
		Summary: "Cancel one active project export lineage as Creator", Tags: []string{"exports"},
		Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *creatorExportCancelHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, huma.Error503ServiceUnavailable("sequence export is unavailable")
		}
		result, err := exports.CancelForCreator(ctx, input.ProjectID, application.SequenceExportCancelInput{
			RequestID: input.Body.RequestID, JobID: input.JobID,
		})
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "delete-creator-sequence-export-artifact", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/exports/{jobId}/artifact/delete",
		Summary: "Explicitly delete one current-tail ExportArtifact as Creator", Tags: []string{"exports"},
		Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *creatorExportDeleteHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, huma.Error503ServiceUnavailable("sequence export is unavailable")
		}
		result, err := exports.DeleteArtifactForCreator(ctx, input.ProjectID, input.JobID, input.Body)
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "start-sequence-export", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/sequences/{sequenceId}/exports",
		Summary: "Start one pinned full-quality Sequence export", Tags: []string{"exports"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "export", "start"),
		Extensions:  commandExtensions("export", "start"),
	}, func(ctx context.Context, input *exportStartHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, commandStatusError(command.StatusUnavailable, huma.Error503ServiceUnavailable("sequence export is unavailable"))
		}
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		applicationInput, err := input.Body.ApplicationInput()
		if err != nil {
			return nil, sequenceExportError(err)
		}
		result, err := exports.Start(
			ctx, input.ProjectID, input.SequenceID, input.RunID, input.TurnID, applicationInput,
		)
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "show-sequence-export", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/exports/{jobId}",
		Summary: "Show one durable export lineage", Tags: []string{"exports"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "export", "show"),
		Extensions:  commandExtensions("export", "show"),
	}, func(ctx context.Context, input *exportJobHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, huma.Error503ServiceUnavailable("sequence export is unavailable")
		}
		result, err := exports.Show(ctx, input.ProjectID, input.RunID, input.TurnID,
			application.SequenceExportShowInput{JobID: input.JobID})
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "retry-sequence-export", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/exports/{jobId}/retry",
		Summary: "Retry one recoverable export lineage", Tags: []string{"exports"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "export", "retry"),
		Extensions:  commandExtensions("export", "retry"),
	}, func(ctx context.Context, input *exportRetryHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, commandStatusError(command.StatusUnavailable, huma.Error503ServiceUnavailable("sequence export is unavailable"))
		}
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		if input.Body.JobID != input.JobID {
			return nil, sequenceExportError(application.ErrSequenceExportInvalid)
		}
		result, err := exports.Retry(ctx, input.ProjectID, input.RunID, input.TurnID,
			application.SequenceExportRetryInput{JobID: input.JobID})
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "cancel-sequence-export", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/exports/{jobId}/cancel",
		Summary: "Cancel one active export lineage", Tags: []string{"exports"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "export", "cancel"),
		Extensions:  commandExtensions("export", "cancel"),
	}, func(ctx context.Context, input *exportCancelHTTPInput) (*exportHTTPOutput, error) {
		if exports == nil {
			return nil, commandStatusError(command.StatusUnavailable, huma.Error503ServiceUnavailable("sequence export is unavailable"))
		}
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		if input.Body.JobID != input.JobID {
			return nil, sequenceExportError(application.ErrSequenceExportInvalid)
		}
		result, err := exports.Cancel(ctx, input.ProjectID, input.RunID, input.TurnID,
			application.SequenceExportCancelInput{RequestID: input.Body.RequestID, JobID: input.JobID})
		if err != nil {
			return nil, sequenceExportError(err)
		}
		return &exportHTTPOutput{Body: command.ExportDataFrom(result)}, nil
	})
}

func sequenceExportError(err error) error {
	switch {
	case errors.Is(err, application.ErrSequenceExportNotFound),
		errors.Is(err, application.ErrProjectNotFound), errors.Is(err, application.ErrRenderSequenceNotFound):
		return commandStatusError(command.StatusNotFound, huma.Error404NotFound("sequence export resource not found"))
	case errors.Is(err, application.ErrRunStaleTurn):
		return huma.ErrorWithHeaders(
			huma.Error409Conflict("sequence export request has a stale AgentTurn", err),
			http.Header{command.StatusHeader: []string{string(command.StatusStaleTurn)}},
		)
	case errors.Is(err, application.ErrSequenceExportRecovery),
		errors.Is(err, application.ErrSequenceExportReused),
		errors.Is(err, application.ErrSequenceExportArtifactInUse),
		errors.Is(err, application.ErrRenderSequenceConflict):
		return commandStatusError(command.StatusConflict, huma.Error409Conflict("sequence export conflicts with current state", err))
	case errors.Is(err, application.ErrSequenceExportUnavailable):
		return commandStatusError(command.StatusUnavailable, huma.Error503ServiceUnavailable("sequence export runtime is unavailable"))
	case errors.Is(err, application.ErrSequenceExportInvalid),
		errors.Is(err, application.ErrInvalidPageCursor),
		errors.Is(err, application.ErrRenderPlanInvalid):
		return commandStatusError(command.StatusInvalid, huma.Error422UnprocessableEntity("sequence export request is invalid", err))
	case errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("sequence export authority denied")
	case errors.Is(err, application.ErrRenderInputRequired), errors.Is(err, application.ErrRenderFontRequired):
		return commandStatusError(command.StatusConflict, huma.Error409Conflict("sequence export prerequisites are unavailable", err))
	default:
		return commandStatusError(command.StatusFailed, huma.Error500InternalServerError("sequence export operation failed", err))
	}
}
