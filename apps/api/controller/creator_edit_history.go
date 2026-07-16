package controller

import (
	"context"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type creatorEditHistoryHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	Before    string           `query:"before" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Limit     uint16           `query:"limit" minimum:"1" maximum:"50" default:"20"`
}

type creatorEditHistoryHTTPOutput struct {
	Body application.CreatorTransactionHistoryPage
}

func RegisterCreatorEditHistory(
	api huma.API,
	reads *application.EditReads,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "list-creator-edit-transactions", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/creator-edit/transactions",
		Summary: "List newest-first durable creative transaction history for the Creator Workspace",
		Tags:    []string{"creator"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *creatorEditHistoryHTTPInput) (*creatorEditHistoryHTTPOutput, error) {
		before, err := parseEditHistoryRevision(input.Before)
		if err != nil {
			return nil, creatorEditError(err)
		}
		page, err := reads.HistoryForCreator(ctx, input.ProjectID, before, input.Limit)
		if err != nil {
			return nil, creatorEditError(err)
		}
		return &creatorEditHistoryHTTPOutput{Body: page}, nil
	})
}
