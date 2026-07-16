package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/danielgtaylor/huma/v2"
)

type uiChallengeInput struct {
	Body service.UIChallengeRequest
}

type uiChallengeOutput struct {
	Body service.UIChallengeResult
}

type uiSessionInput struct {
	Body service.UISessionRequest
}

type uiSessionOutput struct {
	Body service.UISessionResult
}

func RegisterUISessions(api huma.API, authorizer service.Authorizer) {
	bootstrap, ok := authorizer.(service.UISessionBootstrap)
	if !ok {
		bootstrap = service.RejectAuthorizer{}
	}
	huma.Register(api, huma.Operation{
		OperationID: "create-ui-challenge",
		Method:      http.MethodPost,
		Path:        "/v1/auth/ui/challenges",
		Summary:     "Create a single-use first-party UI possession challenge",
		Tags:        []string{"authorization"},
		Extensions:  map[string]any{"x-open-cut-surface": "internal-first-party-bootstrap"},
	}, func(ctx context.Context, input *uiChallengeInput) (*uiChallengeOutput, error) {
		result, err := bootstrap.ChallengeUI(ctx, input.Body)
		if err != nil {
			return nil, uiSessionError(err)
		}
		return &uiChallengeOutput{Body: result}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "create-ui-session",
		Method:      http.MethodPost,
		Path:        "/v1/auth/ui/sessions",
		Summary:     "Exchange a signed challenge for an API-instance UI session",
		Tags:        []string{"authorization"},
		Extensions:  map[string]any{"x-open-cut-surface": "internal-first-party-bootstrap"},
	}, func(ctx context.Context, input *uiSessionInput) (*uiSessionOutput, error) {
		result, err := bootstrap.ExchangeUI(ctx, input.Body)
		if err != nil {
			return nil, uiSessionError(err)
		}
		return &uiSessionOutput{Body: result}, nil
	})
}

func uiSessionError(err error) error {
	switch {
	case errors.Is(err, service.ErrUIRateLimited):
		return huma.Error429TooManyRequests("UI session bootstrap is rate limited")
	case errors.Is(err, service.ErrUIChallengeInvalid), errors.Is(err, service.ErrUIOriginDenied):
		return huma.Error422UnprocessableEntity("UI session bootstrap request is invalid")
	case errors.Is(err, service.ErrUIChallengeExpired), errors.Is(err, service.ErrUnauthorized):
		return huma.Error401Unauthorized("UI possession proof was rejected")
	default:
		return huma.Error500InternalServerError("UI session bootstrap failed", err)
	}
}
