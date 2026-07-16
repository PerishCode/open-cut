package controller

import (
	"context"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type creatorClipPlacementPreviewHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       application.CreatorClipPlacementPreviewInput
}

type creatorClipPlacementPreviewHTTPOutput struct {
	Body application.CreatorClipPlacementPreview
}

func RegisterCreatorClipPlacementPreview(
	api huma.API,
	reads *application.EditReads,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "preview-creator-clip-placement", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/clip-placement-preview",
		Summary: "Plan one exact Creator source-range placement on explicit existing tracks",
		Tags:    []string{"creator"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *creatorClipPlacementPreviewHTTPInput) (*creatorClipPlacementPreviewHTTPOutput, error) {
		preview, err := reads.ClipPlacementForCreator(ctx, input.ProjectID, input.SequenceID, input.Body)
		if err != nil {
			return nil, creatorEditError(err)
		}
		return &creatorClipPlacementPreviewHTTPOutput{Body: preview}, nil
	})
}
