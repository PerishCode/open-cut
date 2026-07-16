package controller

import (
	"context"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type creatorCaptionPreviewHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       application.CreatorCaptionDerivationPreviewInput
}

type creatorCaptionPreviewHTTPOutput struct {
	Body application.CaptionDerivationPreview
}

func RegisterCreatorCaptionPreview(
	api huma.API,
	reads *application.EditReads,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "preview-creator-captions", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/caption-derivation-preview",
		Summary: "Preview one insert-only deterministic Creator SourceExcerpt-to-Caption derivation",
		Tags:    []string{"creator"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *creatorCaptionPreviewHTTPInput) (*creatorCaptionPreviewHTTPOutput, error) {
		preview, err := reads.CaptionDerivationForCreator(ctx, input.ProjectID, input.SequenceID, input.Body)
		if err != nil {
			return nil, creatorEditError(err)
		}
		return &creatorCaptionPreviewHTTPOutput{Body: preview}, nil
	})
}
