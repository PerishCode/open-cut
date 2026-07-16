package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type platformSourceGrantHTTPInput struct {
	Body service.PlatformSourceSelection
}

type sourceGrantHTTPOutput struct {
	Body application.SourceGrantResult
}

type assetRegisterHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	Body      application.RegisterAssetInput
}

type assetRegisterHTTPOutput struct {
	Body application.AssetRegisterResult
}

type assetListHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	After     string           `query:"after" maxLength:"512"`
	Limit     uint16           `query:"limit" minimum:"1" maximum:"100"`
}

type assetListHTTPOutput struct {
	Body command.AssetListData
}

type assetInspectHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	AssetID   domain.AssetID   `path:"assetId"`
}

type assetInspectHTTPOutput struct {
	Body command.AssetInspectData
}

type assetFramesHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	RunID     domain.RunID     `path:"runId"`
	TurnID    domain.TurnID    `path:"turnId"`
	AssetID   domain.AssetID   `path:"assetId"`
	Body      command.AssetFramesInput
}

type assetFramesHTTPOutput struct {
	Body application.MediaFrameSetRequestResult
}

type transcriptReadHTTPInput struct {
	ProjectID  domain.ProjectID `path:"projectId"`
	AssetID    domain.AssetID   `path:"assetId"`
	ArtifactID string           `query:"artifactId" maxLength:"36"`
	After      string           `query:"after" maxLength:"10"`
	Limit      uint16           `query:"limit" minimum:"1" maximum:"50"`
}

type transcriptReadHTTPOutput struct {
	Body command.TranscriptReadData
}

type transcriptDefaultSelectHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	AssetID   domain.AssetID   `path:"assetId"`
	Body      application.SelectTranscriptDefaultInput
}

type transcriptDefaultSelectHTTPOutput struct {
	Body application.TranscriptDefaultSelection
}

func RegisterMedia(
	api huma.API,
	media *application.Media,
	reads *application.AssetReads,
	sourceAccess *service.SourceAccess,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "register-platform-source-grant", Method: http.MethodPost,
		Path:    "/v1/internal/platform/source-grants",
		Summary: "Register creator-selected platform source authority",
		Tags:    []string{"media"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "internal-trusted-platform"},
	}, func(ctx context.Context, input *platformSourceGrantHTTPInput) (*sourceGrantHTTPOutput, error) {
		result, err := sourceAccess.RegisterSelection(ctx, input.Body)
		if err != nil {
			return nil, mediaError(err)
		}
		return &sourceGrantHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "register-asset", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/assets",
		Summary: "Commit a creator-selected SourceGrant as a referenced Asset",
		Tags:    []string{"media"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *assetRegisterHTTPInput) (*assetRegisterHTTPOutput, error) {
		result, err := media.RegisterAsset(ctx, input.ProjectID, input.Body)
		if err != nil {
			return nil, mediaError(err)
		}
		return &assetRegisterHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-assets", Method: http.MethodGet,
		Path: "/v1/projects/{projectId}/assets", Summary: "List bounded Asset summaries",
		Tags: []string{"media"}, Middlewares: requireCommandAuthority(api, runs, authorizer, "asset", "list"),
		Extensions: commandExtensions("asset", "list"),
	}, func(ctx context.Context, input *assetListHTTPInput) (*assetListHTTPOutput, error) {
		result, err := reads.List(ctx, input.ProjectID, input.After, input.Limit)
		if err != nil {
			return nil, mediaError(err)
		}
		refs := make([]application.CommandReceiptRef, 0, len(result.Assets))
		for _, asset := range result.Assets {
			refs = append(refs, commandReceiptRef("asset", asset.ID.String(), asset.Revision))
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, result, application.CommandReceiptSucceeded,
			refs, 0, result.ActivityCursor,
		); err != nil {
			return nil, mediaError(err)
		}
		return &assetListHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "inspect-asset", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/assets/{assetId}",
		Summary: "Inspect one Asset and its operational media state",
		Tags:    []string{"media"}, Middlewares: requireCommandAuthority(api, runs, authorizer, "asset", "inspect"),
		Extensions: commandExtensions("asset", "inspect"),
	}, func(ctx context.Context, input *assetInspectHTTPInput) (*assetInspectHTTPOutput, error) {
		asset, cursor, err := reads.Inspect(ctx, input.ProjectID, input.AssetID)
		if err != nil {
			return nil, mediaError(err)
		}
		result := command.AssetInspectData{Asset: asset, ActivityCursor: cursor}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, result, application.CommandReceiptSucceeded,
			[]application.CommandReceiptRef{commandReceiptRef("asset", asset.ID.String(), asset.Revision)},
			0, cursor,
		); err != nil {
			return nil, mediaError(err)
		}
		return &assetInspectHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "request-asset-frames", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/assets/{assetId}/frames",
		Summary: "Request bounded exact frame resources", Tags: []string{"media"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "asset", "frames"),
		Extensions:  commandExtensions("asset", "frames"),
	}, func(ctx context.Context, input *assetFramesHTTPInput) (*assetFramesHTTPOutput, error) {
		if input.Body.AssetID != input.AssetID {
			return nil, mediaError(application.ErrAssetInvalid)
		}
		result, err := media.RequestFrames(
			ctx, input.ProjectID, input.AssetID, input.RunID, input.TurnID,
			application.RequestMediaFramesInput{
				SourceStreamID: input.Body.SourceStreamID, Times: input.Body.Times,
			},
		)
		if err != nil {
			return nil, mediaError(err)
		}
		status := application.CommandReceiptSucceeded
		if result.Status == application.MediaFrameSetAccepted {
			status = application.CommandReceiptAccepted
		}
		refs := []application.CommandReceiptRef{commandReceiptRef("media-job", result.Job.ID.String(), 0)}
		if result.ArtifactID != nil {
			refs = append(refs, commandReceiptRef("artifact", result.ArtifactID.String(), 0))
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, result, status, refs, 0, result.ActivityCursor,
		); err != nil {
			return nil, mediaError(err)
		}
		return &assetFramesHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "read-transcript", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/assets/{assetId}/transcript",
		Summary: "Read bounded original transcript recognition", Tags: []string{"media"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "transcript", "read"),
		Extensions:  commandExtensions("transcript", "read"),
	}, func(ctx context.Context, input *transcriptReadHTTPInput) (*transcriptReadHTTPOutput, error) {
		var artifactID *domain.ArtifactID
		if input.ArtifactID != "" {
			parsed, err := domain.ParseArtifactID(input.ArtifactID)
			if err != nil {
				return nil, mediaError(application.ErrTranscriptReadInvalid)
			}
			artifactID = &parsed
		}
		result, err := media.ReadTranscript(ctx, application.TranscriptReadQuery{
			ProjectID: input.ProjectID, AssetID: input.AssetID, ArtifactID: artifactID,
			After: input.After, Limit: input.Limit,
		})
		if err != nil {
			return nil, mediaError(err)
		}
		refs := []application.CommandReceiptRef{
			commandReceiptRef("asset", input.AssetID.String(), 0),
			commandReceiptRef("artifact", result.Artifact.ID.String(), 0),
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, result, application.CommandReceiptSucceeded,
			refs, 0, result.ActivityCursor,
		); err != nil {
			return nil, mediaError(err)
		}
		return &transcriptReadHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "select-default-transcript", Method: http.MethodPut,
		Path:    "/v1/projects/{projectId}/assets/{assetId}/transcript-selection",
		Summary: "Select the Creator default transcript artifact", Tags: []string{"media"},
		Middlewares: requireAuthority(api, authorizer),
		Extensions:  map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *transcriptDefaultSelectHTTPInput) (*transcriptDefaultSelectHTTPOutput, error) {
		result, err := media.SelectTranscriptDefault(ctx, input.ProjectID, input.AssetID, input.Body)
		if err != nil {
			return nil, mediaError(err)
		}
		return &transcriptDefaultSelectHTTPOutput{Body: result}, nil
	})
}

func mediaError(err error) error {
	switch {
	case errors.Is(err, application.ErrProjectNotFound), errors.Is(err, application.ErrAssetNotFound),
		errors.Is(err, application.ErrSourceGrantNotFound), errors.Is(err, application.ErrTranscriptNotFound):
		return commandStatusError(command.StatusNotFound, huma.Error404NotFound("media resource not found"))
	case errors.Is(err, application.ErrEditConflict), errors.Is(err, application.ErrAssetAlreadyImported),
		errors.Is(err, application.ErrAssetRequestReused), errors.Is(err, application.ErrSourceGrantReused),
		errors.Is(err, application.ErrTranscriptSelectionConflict):
		return commandStatusError(command.StatusConflict, huma.Error409Conflict("media request conflicts with current state", err))
	case errors.Is(err, application.ErrRunStaleTurn):
		return huma.ErrorWithHeaders(
			huma.Error409Conflict("media request has a stale AgentTurn", err),
			http.Header{command.StatusHeader: []string{string(command.StatusStaleTurn)}})
	case errors.Is(err, application.ErrAssetInvalid), errors.Is(err, application.ErrSourceGrantInvalid),
		errors.Is(err, application.ErrInvalidAssetCursor), errors.Is(err, service.ErrSourceSelectionInvalid),
		errors.Is(err, service.ErrSourceSelectionUnreadable), errors.Is(err, application.ErrTranscriptReadInvalid):
		return commandStatusError(command.StatusInvalid, huma.Error422UnprocessableEntity("media request is invalid", err))
	case errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("media authority denied")
	default:
		return commandStatusError(command.StatusFailed, huma.Error500InternalServerError("media operation failed", err))
	}
}
