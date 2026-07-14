package controller

import (
	"context"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/model"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/danielgtaylor/huma/v2"
)

type healthOutput struct {
	Body model.Health
}

func RegisterHealth(api huma.API, current service.Health) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/v1/health",
		Summary:     "Get API health",
		Tags:        []string{"health"},
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		status, err := current.Get(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("health repository failed", err)
		}
		return &healthOutput{Body: status}, nil
	})
}
