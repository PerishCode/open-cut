package controller

import (
	"context"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type creatorRoughCutPreviewHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       application.RoughCutDerivationPreviewInput
}

type creatorRoughCutPreviewHTTPOutput struct {
	Body application.RoughCutDerivationPreview
}

func RegisterCreatorRoughCutPreview(
	api huma.API,
	reads *application.EditReads,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "preview-creator-rough-cut", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/rough-cut-preview",
		Summary: "Preview one deterministic Creator PaperEdit-to-Sequence rough cut",
		Tags:    []string{"editing"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *creatorRoughCutPreviewHTTPInput) (*creatorRoughCutPreviewHTTPOutput, error) {
		preview, err := reads.RoughCutDerivationForCreator(ctx, input.ProjectID, input.SequenceID, input.Body)
		if err != nil {
			return nil, creatorEditError(err)
		}
		return &creatorRoughCutPreviewHTTPOutput{Body: preview}, nil
	})
}
