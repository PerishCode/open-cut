package controller

import (
	"context"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type creatorTimelineGesturePreviewHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       application.CreatorTimelineGesturePreviewInput
}

type creatorTimelineGesturePreviewHTTPOutput struct {
	Body application.CreatorTimelineGesturePreviewResult
}

func RegisterCreatorTimelineGesturePreview(
	api huma.API,
	reads *application.EditReads,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "preview-creator-timeline-gesture", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/timeline-gesture-preview",
		Summary: "Plan one exact Creator Timeline gesture over complete linked and Alignment state",
		Tags:    []string{"creator"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *creatorTimelineGesturePreviewHTTPInput) (*creatorTimelineGesturePreviewHTTPOutput, error) {
		preview, err := reads.TimelineGestureForCreator(ctx, input.ProjectID, input.SequenceID, input.Body)
		if err != nil {
			return nil, creatorEditError(err)
		}
		return &creatorTimelineGesturePreviewHTTPOutput{Body: preview}, nil
	})
}
