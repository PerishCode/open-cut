package controller

import (
	"context"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type creatorCaptionGesturePreviewHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       application.CreatorCaptionGesturePreviewInput
}

type creatorCaptionGesturePreviewHTTPOutput struct {
	Body application.CreatorCaptionGesturePreview
}

func RegisterCreatorCaptionGesturePreview(
	api huma.API,
	reads *application.EditReads,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "preview-creator-caption-gesture", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/caption-gesture-preview",
		Summary: "Plan one exact Creator manual Caption gesture over complete collision and Alignment state",
		Tags:    []string{"creator"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *creatorCaptionGesturePreviewHTTPInput) (*creatorCaptionGesturePreviewHTTPOutput, error) {
		preview, err := reads.CaptionGestureForCreator(ctx, input.ProjectID, input.SequenceID, input.Body)
		if err != nil {
			return nil, creatorEditError(err)
		}
		return &creatorCaptionGesturePreviewHTTPOutput{Body: preview}, nil
	})
}
