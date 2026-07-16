package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/danielgtaylor/huma/v2"
)

type productStatusHTTPOutput struct {
	Body command.ProductStatusData
}

func RegisterProductStatus(
	api huma.API,
	status *application.ProductStatus,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "show-product-status", Method: http.MethodGet,
		Path: "/v1/product/status", Summary: "Show semantic product feature availability",
		Tags: []string{"product"}, Middlewares: requireCommandAuthority(api, runs, authorizer, "product", "status"),
		Extensions: commandExtensions("product", "status"),
	}, func(ctx context.Context, _ *struct{}) (*productStatusHTTPOutput, error) {
		if status == nil {
			return nil, huma.Error503ServiceUnavailable("product status is unavailable")
		}
		snapshot, err := status.Read(ctx)
		if err != nil {
			switch {
			case errors.Is(err, application.ErrAuthorityMissing),
				errors.Is(err, application.ErrAuthorityInvalid),
				errors.Is(err, application.ErrAuthorityScopeDenied):
				return nil, huma.Error403Forbidden("product status authority denied")
			default:
				return nil, huma.Error500InternalServerError("product status failed", err)
			}
		}
		return &productStatusHTTPOutput{Body: snapshot}, nil
	})
}
