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
	"github.com/danielgtaylor/huma/v2/sse"
)

type agentBridgeBeginHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	Body      application.AgentBridgeBeginInput
}

type agentBridgeListHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	Limit     uint32           `query:"limit" minimum:"1" maximum:"20" default:"10"`
}

type agentBridgeRunHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
}

type agentBridgeContinueHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	Body      application.AgentBridgeContinueInput
}

type agentBridgeTransitionHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	TurnID    domain.TurnID    `path:"turnId"`
	Body      application.AgentBridgeTransitionInput
}

type agentBridgePresentationHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	TurnID    domain.TurnID    `path:"turnId"`
}

type agentBridgeConversationHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	After     domain.Cursor    `query:"after" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Limit     uint32           `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type agentBridgeReceiptsHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	TurnID    domain.TurnID    `path:"turnId"`
	After     domain.Cursor    `query:"after" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Limit     uint32           `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type agentBridgeTurnsHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	Before    domain.Cursor    `query:"before" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Limit     uint32           `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type agentBridgeResultHTTPOutput struct {
	Body application.AgentBridgeResult
}

type agentBridgeRunHTTPOutput struct {
	Body application.AgentBridgeRun
}

type agentBridgeRunPageHTTPOutput struct {
	Body application.AgentBridgeRunPage
}

type agentBridgeConversationHTTPOutput struct {
	Body application.AgentConversationPage
}

type agentBridgeReceiptsHTTPOutput struct {
	Body application.TurnReceiptPage
}

type agentBridgeTurnsHTTPOutput struct {
	Body application.AgentBridgeTurnPage
}

type agentBridgeAvailabilityHTTPOutput struct {
	Body application.AgentBridgeAvailability
}

func RegisterAgentBridge(
	api huma.API,
	bridge *service.AgentBridgeService,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	creatorSurface := map[string]any{"x-open-cut-surface": "first-party-creator"}
	huma.Register(api, huma.Operation{
		OperationID: "show-local-agent-availability", Method: http.MethodGet,
		Path: "/v1/agent/availability", Summary: "Show safe local Agent adapter availability",
		Tags: []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, _ *struct{}) (*agentBridgeAvailabilityHTTPOutput, error) {
		if bridge == nil {
			return &agentBridgeAvailabilityHTTPOutput{Body: application.AgentBridgeAvailability{
				AdapterID: application.AgentBridgeAdapterCodexV1, PromptVersion: service.AgentPromptVersion,
				State: application.AgentBridgeIncompatible,
			}}, nil
		}
		return &agentBridgeAvailabilityHTTPOutput{Body: bridge.Availability(ctx)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-creator-agent-turns", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/agent/runs/{runId}/turns",
		Summary: "List authoritative historical turns for one Creator-started Agent run",
		Tags:    []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *agentBridgeTurnsHTTPInput) (*agentBridgeTurnsHTTPOutput, error) {
		if bridge == nil {
			return nil, huma.Error503ServiceUnavailable("Agent turn history is unavailable")
		}
		result, err := bridge.Turns(ctx, input.ProjectID, input.RunID, input.Before, input.Limit)
		if err != nil {
			return nil, agentBridgeError(err)
		}
		return &agentBridgeTurnsHTTPOutput{Body: result}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "list-creator-agent-turn-receipts", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/agent/runs/{runId}/turns/{turnId}/receipts",
		Summary: "List the independent durable command receipt ledger for one Agent turn",
		Tags:    []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *agentBridgeReceiptsHTTPInput) (*agentBridgeReceiptsHTTPOutput, error) {
		if runs == nil {
			return nil, huma.Error503ServiceUnavailable("Agent receipt ledger is unavailable")
		}
		result, err := runs.Receipts(
			ctx, input.ProjectID, input.RunID, input.TurnID, input.After, input.Limit,
		)
		if err != nil {
			return nil, agentBridgeError(err)
		}
		return &agentBridgeReceiptsHTTPOutput{Body: result}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "list-creator-agent-runs", Method: http.MethodGet,
		Path: "/v1/projects/{projectId}/agent/runs", Summary: "List bounded recent Creator-started Agent runs",
		Tags: []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *agentBridgeListHTTPInput) (*agentBridgeRunPageHTTPOutput, error) {
		if bridge == nil {
			return nil, huma.Error503ServiceUnavailable("local Agent adapter is unavailable")
		}
		result, err := bridge.List(ctx, input.ProjectID, input.Limit)
		if err != nil {
			return nil, agentBridgeError(err)
		}
		return &agentBridgeRunPageHTTPOutput{Body: result}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "begin-creator-agent-run", Method: http.MethodPost,
		Path: "/v1/projects/{projectId}/agent/runs", Summary: "Submit one Creator message as a durable Agent turn",
		Tags: []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *agentBridgeBeginHTTPInput) (*agentBridgeResultHTTPOutput, error) {
		if bridge == nil {
			return nil, huma.Error503ServiceUnavailable("local Agent adapter is unavailable")
		}
		result, err := bridge.Begin(ctx, input.ProjectID, input.Body)
		if err != nil {
			return nil, agentBridgeError(err)
		}
		return &agentBridgeResultHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "show-creator-agent-run", Method: http.MethodGet,
		Path: "/v1/projects/{projectId}/agent/runs/{runId}", Summary: "Show one Creator-started Agent run",
		Tags: []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *agentBridgeRunHTTPInput) (*agentBridgeRunHTTPOutput, error) {
		if bridge == nil {
			return nil, huma.Error503ServiceUnavailable("local Agent adapter is unavailable")
		}
		result, err := bridge.Show(ctx, input.ProjectID, input.RunID)
		if err != nil {
			return nil, agentBridgeError(err)
		}
		return &agentBridgeRunHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "continue-creator-agent-run", Method: http.MethodPost,
		Path: "/v1/projects/{projectId}/agent/runs/{runId}/messages", Summary: "Submit the next Creator message as a new Agent turn",
		Tags: []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *agentBridgeContinueHTTPInput) (*agentBridgeResultHTTPOutput, error) {
		if bridge == nil {
			return nil, huma.Error503ServiceUnavailable("local Agent adapter is unavailable")
		}
		result, err := bridge.Continue(ctx, input.ProjectID, input.RunID, input.Body)
		if err != nil {
			return nil, agentBridgeError(err)
		}
		return &agentBridgeResultHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-creator-agent-conversation", Method: http.MethodGet,
		Path: "/v1/projects/{projectId}/agent/runs/{runId}/conversation", Summary: "List the durable safe Agent conversation ledger",
		Tags: []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, func(ctx context.Context, input *agentBridgeConversationHTTPInput) (*agentBridgeConversationHTTPOutput, error) {
		if bridge == nil {
			return nil, huma.Error503ServiceUnavailable("local Agent adapter is unavailable")
		}
		result, err := bridge.Conversation(ctx, input.ProjectID, input.RunID, application.AgentConversationListInput{
			After: input.After, Limit: input.Limit,
		})
		if err != nil {
			return nil, agentBridgeError(err)
		}
		return &agentBridgeConversationHTTPOutput{Body: result}, nil
	})

	sse.Register(api, huma.Operation{
		OperationID: "watch-creator-agent-presentation", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/agent/runs/{runId}/turns/{turnId}/presentation",
		Summary: "Watch process-local safe presentation events for the active Agent turn",
		Tags:    []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: creatorSurface,
	}, map[string]any{"presentation": &service.AgentPresentationEnvelope{}}, func(
		ctx context.Context,
		input *agentBridgePresentationHTTPInput,
		send sse.Sender,
	) {
		if bridge == nil {
			return
		}
		subscription, err := bridge.SubscribePresentation(ctx, input.ProjectID, input.RunID, input.TurnID)
		if err != nil {
			return
		}
		defer subscription.Close()
		for {
			event, ok := subscription.Next(ctx)
			if !ok || send(sse.Message{Data: event}) != nil {
				return
			}
		}
	})

	registerAgentBridgeTransition(api, bridge, authorizer, creatorSurface, application.AgentBridgeInterrupt)
	registerAgentBridgeTransition(api, bridge, authorizer, creatorSurface, application.AgentBridgeCancel)
}

func registerAgentBridgeTransition(
	api huma.API,
	bridge *service.AgentBridgeService,
	authorizer service.Authorizer,
	extensions map[string]any,
	transition application.AgentBridgeTransition,
) {
	operationID, summary := "interrupt-creator-agent-turn", "Stop the active Agent turn without terminating its Run"
	if transition == application.AgentBridgeCancel {
		operationID, summary = "cancel-creator-agent-run", "Terminate an Agent run without reverting committed work"
	}
	huma.Register(api, huma.Operation{
		OperationID: operationID, Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/agent/runs/{runId}/turns/{turnId}/" + string(transition),
		Summary: summary, Tags: []string{"agent"}, Middlewares: requireAuthority(api, authorizer), Extensions: extensions,
	}, func(ctx context.Context, input *agentBridgeTransitionHTTPInput) (*agentBridgeResultHTTPOutput, error) {
		if bridge == nil {
			return nil, huma.Error503ServiceUnavailable("local Agent adapter is unavailable")
		}
		var result application.AgentBridgeResult
		var err error
		if transition == application.AgentBridgeInterrupt {
			result, err = bridge.Interrupt(ctx, input.ProjectID, input.RunID, input.TurnID, input.Body)
		} else {
			result, err = bridge.Cancel(ctx, input.ProjectID, input.RunID, input.TurnID, input.Body)
		}
		if err != nil {
			return nil, agentBridgeError(err)
		}
		return &agentBridgeResultHTTPOutput{Body: result}, nil
	})
}

func agentBridgeError(err error) error {
	switch {
	case errors.Is(err, application.ErrAgentBridgeNotFound), errors.Is(err, application.ErrProjectNotFound):
		return huma.Error404NotFound("Agent run was not found")
	case errors.Is(err, application.ErrCommandReceiptNotFound):
		return huma.Error404NotFound("Agent receipt ledger was not found")
	case errors.Is(err, application.ErrAgentBridgeStaleTurn):
		return huma.ErrorWithHeaders(
			huma.Error409Conflict("Agent turn generation is stale", err),
			http.Header{command.StatusHeader: []string{string(command.StatusStaleTurn)}},
		)
	case errors.Is(err, application.ErrAgentContextStale):
		return huma.Error409Conflict("Agent context attachment is stale", err)
	case errors.Is(err, application.ErrAgentBridgeBusy), errors.Is(err, application.ErrAgentBridgeTerminal),
		errors.Is(err, application.ErrAgentBridgeRequestReused), errors.Is(err, application.ErrAgentBridgeBindingDenied),
		errors.Is(err, application.ErrProjectNotActive):
		return huma.Error409Conflict("Agent run conflicts with current state", err)
	case errors.Is(err, application.ErrAgentBridgeInvalid), errors.Is(err, application.ErrCommandReceiptInvalid),
		errors.Is(err, domain.ErrInvalidDurableID),
		errors.Is(err, domain.ErrInvalidRequestID):
		return huma.Error422UnprocessableEntity("Agent request is invalid", err)
	case errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("Agent authority denied")
	case errors.Is(err, service.ErrAgentProcessInvalid), errors.Is(err, service.ErrAgentProcessFailed),
		errors.Is(err, service.ErrAgentProcessProtocol), errors.Is(err, service.ErrAgentProcessResourceLimit),
		errors.Is(err, service.ErrAgentAdapterMissing), errors.Is(err, service.ErrAgentAdapterUnauthenticated),
		errors.Is(err, service.ErrAgentAdapterIncompatible):
		return huma.Error503ServiceUnavailable("local Agent adapter is unavailable")
	default:
		return huma.Error500InternalServerError("Agent operation failed", err)
	}
}
