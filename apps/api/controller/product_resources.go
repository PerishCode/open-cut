package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/danielgtaylor/huma/v2"
)

type productResourceListOutput struct {
	Body application.ProductResourceSnapshot
}

type productResourceAcquireInput struct {
	Name string `path:"resourceName" pattern:"^[a-z][a-z0-9.-]{0,127}$"`
	Body application.AcquireProductResourceInput
}

type productResourceAcquireOutput struct {
	Body application.AcquireProductResourceResult
}

func RegisterProductResources(
	api huma.API,
	resources *application.ProductResources,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "list-product-resources", Method: http.MethodGet,
		Path: "/v1/product/resources", Summary: "List active-payload product resources and local state",
		Tags: []string{"product"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, _ *struct{}) (*productResourceListOutput, error) {
		if resources == nil {
			return nil, huma.Error503ServiceUnavailable("product resources are unavailable")
		}
		result, err := resources.List(ctx)
		if err != nil {
			return nil, productResourceError(err)
		}
		return &productResourceListOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "acquire-product-resource", Method: http.MethodPost,
		Path:    "/v1/product/resources/{resourceName}/acquisition",
		Summary: "Authorize acquisition of one authenticated product resource",
		Tags:    []string{"product"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *productResourceAcquireInput) (*productResourceAcquireOutput, error) {
		if resources == nil {
			return nil, huma.Error503ServiceUnavailable("product resources are unavailable")
		}
		result, err := resources.Acquire(ctx, input.Name, input.Body)
		if err != nil {
			return nil, productResourceError(err)
		}
		return &productResourceAcquireOutput{Body: result}, nil
	})
}

func productResourceError(err error) error {
	switch {
	case errors.Is(err, application.ErrProductResourceNotFound):
		return huma.Error404NotFound("product resource not found")
	case errors.Is(err, application.ErrRequestIdentityReused):
		return huma.Error409Conflict("product resource request identity conflicts", err)
	case errors.Is(err, application.ErrProductResourceInvalid):
		return huma.Error422UnprocessableEntity("product resource request is invalid", err)
	case errors.Is(err, application.ErrAuthorityMissing),
		errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("product resource authority denied")
	default:
		return huma.Error500InternalServerError("product resource operation failed", err)
	}
}
