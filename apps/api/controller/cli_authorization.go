package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/authwire"
	"github.com/danielgtaylor/huma/v2"
)

type cliChallengeInput struct {
	Body service.CLIChallengeRequest
}

type cliChallengeOutput struct {
	Body service.CLIChallengeResult
}

type cliGrantListOutput struct {
	Body struct {
		Grants   []application.CLIGrant             `json:"grants" maxItems:"256" nullable:"false"`
		Upgrades []application.CLIGrantScopeUpgrade `json:"upgrades" maxItems:"256" nullable:"false"`
	}
}

type cliGrantDecisionInput struct {
	ID string `path:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
}

type cliGrantDecisionOutput struct {
	Body application.CLIGrant
}

type cliScopeUpgradeDecisionOutput struct {
	Body struct {
		Upgrade application.CLIGrantScopeUpgrade `json:"upgrade"`
		Grant   application.CLIGrant             `json:"grant"`
	}
}

func RegisterCLIAuthorization(api huma.API, authorizer service.Authorizer) {
	bootstrap, ok := authorizer.(service.CLIAuthorizationBootstrap)
	if !ok {
		bootstrap = service.RejectAuthorizer{}
	}
	huma.Register(api, huma.Operation{
		OperationID: "create-cli-challenge", Method: http.MethodPost, Path: authwire.CLIChallengeRoute,
		Summary: "Create a single-use product CLI command challenge", Tags: []string{"authorization"},
		Extensions: map[string]any{"x-open-cut-surface": "internal-product-cli-bootstrap"},
	}, func(ctx context.Context, input *cliChallengeInput) (*cliChallengeOutput, error) {
		result, err := bootstrap.ChallengeCLI(ctx, input.Body)
		if err != nil {
			return nil, cliAuthorizationError(err)
		}
		return &cliChallengeOutput{Body: result}, nil
	})

	uiAuthority := requireAuthority(api, authorizer)
	huma.Register(api, huma.Operation{
		OperationID: "list-cli-pairings", Method: http.MethodGet, Path: "/v1/authorization/cli/pairings",
		Summary: "List this installation's product CLI grants", Tags: []string{"authorization"},
		Middlewares: uiAuthority, Extensions: map[string]any{"x-open-cut-surface": "creator"},
	}, func(ctx context.Context, _ *struct{}) (*cliGrantListOutput, error) {
		grants, err := bootstrap.ListCLIGrants(ctx)
		if err != nil {
			return nil, cliAuthorizationError(err)
		}
		upgrades, err := bootstrap.ListCLIGrantScopeUpgrades(ctx)
		if err != nil {
			return nil, cliAuthorizationError(err)
		}
		output := &cliGrantListOutput{}
		output.Body.Grants = grants
		output.Body.Upgrades = upgrades
		return output, nil
	})

	registerCLIGrantDecision(api, bootstrap, authorizer, true)
	registerCLIGrantDecision(api, bootstrap, authorizer, false)
	registerCLIScopeUpgradeDecision(api, bootstrap, authorizer, true)
	registerCLIScopeUpgradeDecision(api, bootstrap, authorizer, false)
	huma.Register(api, huma.Operation{
		OperationID: "revoke-cli-pairing", Method: http.MethodPost,
		Path:    "/v1/authorization/cli/pairings/{id}/revoke",
		Summary: "Revoke an active product CLI grant", Tags: []string{"authorization"},
		Middlewares: requireAuthority(api, authorizer),
		Extensions:  map[string]any{"x-open-cut-surface": "creator"},
	}, func(ctx context.Context, input *cliGrantDecisionInput) (*cliGrantDecisionOutput, error) {
		grant, err := bootstrap.RevokeCLIGrant(ctx, input.ID)
		if err != nil {
			return nil, cliAuthorizationError(err)
		}
		return &cliGrantDecisionOutput{Body: grant}, nil
	})
}

func registerCLIScopeUpgradeDecision(
	api huma.API,
	bootstrap service.CLIAuthorizationBootstrap,
	authorizer service.Authorizer,
	approve bool,
) {
	action := "deny"
	operation := "deny-cli-scope-upgrade"
	if approve {
		action = "approve"
		operation = "approve-cli-scope-upgrade"
	}
	huma.Register(api, huma.Operation{
		OperationID: operation, Method: http.MethodPost,
		Path:    "/v1/authorization/cli/scope-upgrades/{id}/" + action,
		Summary: action + " an exact pending product CLI scope upgrade", Tags: []string{"authorization"},
		Middlewares: requireAuthority(api, authorizer),
		Extensions:  map[string]any{"x-open-cut-surface": "creator"},
	}, func(ctx context.Context, input *cliGrantDecisionInput) (*cliScopeUpgradeDecisionOutput, error) {
		upgrade, grant, err := bootstrap.DecideCLIGrantScopeUpgrade(ctx, input.ID, approve)
		if err != nil {
			return nil, cliAuthorizationError(err)
		}
		output := &cliScopeUpgradeDecisionOutput{}
		output.Body.Upgrade = upgrade
		output.Body.Grant = grant
		return output, nil
	})
}

func registerCLIGrantDecision(
	api huma.API,
	bootstrap service.CLIAuthorizationBootstrap,
	authorizer service.Authorizer,
	approve bool,
) {
	action := "deny"
	operation := "deny-cli-pairing"
	if approve {
		action = "approve"
		operation = "approve-cli-pairing"
	}
	huma.Register(api, huma.Operation{
		OperationID: operation, Method: http.MethodPost,
		Path:    "/v1/authorization/cli/pairings/{id}/" + action,
		Summary: action + " an exact pending product CLI grant", Tags: []string{"authorization"},
		Middlewares: requireAuthority(api, authorizer),
		Extensions:  map[string]any{"x-open-cut-surface": "creator"},
	}, func(ctx context.Context, input *cliGrantDecisionInput) (*cliGrantDecisionOutput, error) {
		grant, err := bootstrap.DecideCLIGrant(ctx, input.ID, approve)
		if err != nil {
			return nil, cliAuthorizationError(err)
		}
		return &cliGrantDecisionOutput{Body: grant}, nil
	})
}

func cliAuthorizationError(err error) error {
	switch {
	case errors.Is(err, service.ErrCLIRateLimited):
		return huma.Error429TooManyRequests("CLI authorization is rate limited")
	case errors.Is(err, service.ErrCLIChallengeInvalid):
		return huma.Error422UnprocessableEntity("CLI challenge is invalid")
	case errors.Is(err, service.ErrCLIChallengeExpired), errors.Is(err, service.ErrUnauthorized):
		return huma.Error401Unauthorized("CLI authorization was rejected")
	case errors.Is(err, application.ErrCLIGrantNotFound):
		return huma.Error404NotFound("CLI grant was not found")
	case errors.Is(err, application.ErrCLIGrantNotPending):
		return huma.Error409Conflict("CLI grant is no longer pending")
	case errors.Is(err, application.ErrCLIGrantNotActive):
		return huma.Error409Conflict("CLI grant is no longer active")
	case errors.Is(err, application.ErrCLIUpgradeNotFound):
		return huma.Error404NotFound("CLI scope upgrade was not found")
	case errors.Is(err, application.ErrCLIUpgradeNotPending):
		return huma.Error409Conflict("CLI scope upgrade is no longer pending")
	default:
		return huma.Error500InternalServerError("CLI authorization failed", err)
	}
}
