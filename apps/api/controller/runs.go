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

type runBeginInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	Body      command.RunBeginInput
}

type runShowInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
}

type runWaitInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	After     string           `query:"after" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
}

type runTransitionInput[Body any] struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	TurnID    domain.TurnID    `path:"turnId"`
	Body      Body
}

type runOutput struct {
	Body command.RunData
}

func RegisterRuns(api huma.API, runs *application.AgentRuns, authorizer service.Authorizer) {
	registerRunBegin(api, runs, authorizer)
	registerRunShow(api, runs, authorizer)
	registerRunWait(api, runs, authorizer)
	registerRunResume(api, runs, authorizer)
	registerRunComplete(api, runs, authorizer)
	registerRunCancel(api, runs, authorizer)
}

func registerRunWait(api huma.API, runs *application.AgentRuns, authorizer service.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "wait-agent-run", Method: http.MethodGet,
		Path: "/v1/projects/{projectId}/runs/{runId}/wait", Summary: "Wait a bounded interval for durable AgentRun activity",
		Tags: []string{"runs"}, Middlewares: requireCommandAuthority(api, runs, authorizer, "run", "wait"),
		Extensions: commandExtensions("run", "wait"),
	}, func(ctx context.Context, input *runWaitInput) (*runOutput, error) {
		var after domain.Cursor
		if input.After != "" {
			if err := after.UnmarshalText([]byte(input.After)); err != nil {
				return nil, runError(application.ErrRunInvalid)
			}
		}
		result, err := runs.Wait(ctx, input.ProjectID, input.RunID, application.RunWaitInput{After: after})
		if err != nil {
			return nil, runError(err)
		}
		return &runOutput{Body: result}, nil
	})
}

func registerRunBegin(api huma.API, runs *application.AgentRuns, authorizer service.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "begin-agent-run", Method: http.MethodPost,
		Path: "/v1/projects/{projectId}/runs", Summary: "Begin a durable standalone AgentRun",
		Tags: []string{"runs"}, Middlewares: requireCommandBodyAuthority(api, runs, authorizer, "run", "begin"),
		Extensions: commandExtensions("run", "begin"),
	}, func(ctx context.Context, input *runBeginInput) (*runOutput, error) {
		result, err := runs.Begin(ctx, input.ProjectID, input.Body)
		if err != nil {
			return nil, runError(err)
		}
		return &runOutput{Body: result}, nil
	})
}

func registerRunShow(api huma.API, runs *application.AgentRuns, authorizer service.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "show-agent-run", Method: http.MethodGet,
		Path: "/v1/projects/{projectId}/runs/{runId}", Summary: "Show a durable AgentRun",
		Tags: []string{"runs"}, Middlewares: requireCommandAuthority(api, runs, authorizer, "run", "show"),
		Extensions: commandExtensions("run", "show"),
	}, func(ctx context.Context, input *runShowInput) (*runOutput, error) {
		result, err := runs.Show(ctx, input.ProjectID, input.RunID)
		if err != nil {
			return nil, runError(err)
		}
		return &runOutput{Body: result}, nil
	})
}

func registerRunResume(api huma.API, runs *application.AgentRuns, authorizer service.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "resume-agent-run", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/resume",
		Summary: "Resume an AgentRun with a new writer-turn generation", Tags: []string{"runs"},
		Middlewares: requireCommandBodyAuthority(api, runs, authorizer, "run", "resume"),
		Extensions:  commandExtensions("run", "resume"),
	}, func(ctx context.Context, input *runTransitionInput[command.RunResumeInput]) (*runOutput, error) {
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		result, err := runs.Resume(ctx, input.ProjectID, input.RunID, input.TurnID, input.Body)
		if err != nil {
			return nil, runError(err)
		}
		return &runOutput{Body: result}, nil
	})
}

func registerRunComplete(api huma.API, runs *application.AgentRuns, authorizer service.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "complete-agent-run", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/complete",
		Summary: "Explicitly complete an AgentRun", Tags: []string{"runs"},
		Middlewares: requireCommandBodyAuthority(api, runs, authorizer, "run", "complete"),
		Extensions:  commandExtensions("run", "complete"),
	}, func(ctx context.Context, input *runTransitionInput[command.RunCompleteInput]) (*runOutput, error) {
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		result, err := runs.Complete(ctx, input.ProjectID, input.RunID, input.TurnID, input.Body)
		if err != nil {
			return nil, runError(err)
		}
		return &runOutput{Body: result}, nil
	})
}

func registerRunCancel(api huma.API, runs *application.AgentRuns, authorizer service.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "cancel-agent-run", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/cancel",
		Summary: "Explicitly cancel an AgentRun without reverting committed work", Tags: []string{"runs"},
		Middlewares: requireCommandBodyAuthority(api, runs, authorizer, "run", "cancel"),
		Extensions:  commandExtensions("run", "cancel"),
	}, func(ctx context.Context, input *runTransitionInput[command.RunCancelInput]) (*runOutput, error) {
		if err := replayPriorCommandFailure(ctx, runs, input.ProjectID); err != nil {
			return nil, err
		}
		result, err := runs.Cancel(ctx, input.ProjectID, input.RunID, input.TurnID, input.Body)
		if err != nil {
			return nil, runError(err)
		}
		return &runOutput{Body: result}, nil
	})
}

func runError(err error) error {
	switch {
	case errors.Is(err, application.ErrRunNotFound), errors.Is(err, application.ErrProjectNotFound):
		return commandStatusError(command.StatusNotFound, huma.Error404NotFound("AgentRun was not found"))
	case errors.Is(err, application.ErrRunStaleTurn):
		return huma.ErrorWithHeaders(
			huma.Error409Conflict("AgentTurn generation is stale", err),
			http.Header{command.StatusHeader: []string{string(command.StatusStaleTurn)}},
		)
	case errors.Is(err, application.ErrRunTerminal), errors.Is(err, application.ErrRunBlocked),
		errors.Is(err, application.ErrRunActorMismatch), errors.Is(err, application.ErrRunBridgeManaged),
		errors.Is(err, application.ErrRunRequestReused),
		errors.Is(err, application.ErrProjectNotActive):
		return huma.ErrorWithHeaders(
			huma.Error409Conflict("AgentRun transition conflicts with current state", err),
			http.Header{command.StatusHeader: []string{string(command.StatusConflict)}},
		)
	case errors.Is(err, application.ErrRunInvalid), errors.Is(err, domain.ErrInvalidDurableID),
		errors.Is(err, domain.ErrInvalidRequestID):
		return commandStatusError(command.StatusInvalid, huma.Error422UnprocessableEntity("AgentRun request is invalid", err))
	case errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("AgentRun authority denied")
	default:
		return commandStatusError(command.StatusFailed, huma.Error500InternalServerError("AgentRun operation failed", err))
	}
}
