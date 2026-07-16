package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

const mediaContentRoute = "/v1/media/content/{lease}"

type mediaLeaseHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	AssetID   domain.AssetID   `path:"assetId"`
	Body      service.MediaLeaseRequest
}

type mediaLeaseHTTPOutput struct {
	Body service.MediaLeaseResult
}

type sourcePositionHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	AssetID   domain.AssetID   `path:"assetId"`
	Body      service.SourcePositionRequest
}

type sourcePositionHTTPOutput struct {
	Body service.SourcePositionResult
}

type sequencePreviewLeaseHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       service.SequencePreviewLeaseRequest
}

type sequencePreviewLeaseHTTPOutput struct {
	Body service.SequencePreviewLeaseResult
}

func RegisterMediaDelivery(
	mux *http.ServeMux,
	api huma.API,
	leases *service.MediaLeaseService,
	sequenceLeases *service.SequencePreviewLeaseService,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "create-media-lease", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/assets/{assetId}/media-leases",
		Summary: "Prepare a creator Viewer source-preview capability",
		Tags:    []string{"media"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *mediaLeaseHTTPInput) (*mediaLeaseHTTPOutput, error) {
		if leases == nil {
			return nil, huma.Error503ServiceUnavailable("media delivery is unavailable")
		}
		result, err := leases.CreateSourcePreview(ctx, input.ProjectID, input.AssetID, input.Body)
		if err != nil {
			return nil, mediaDeliveryError(err)
		}
		return &mediaLeaseHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "resolve-source-preview-position", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/assets/{assetId}/source-position",
		Summary: "Resolve one bounded exact position in a pinned creator Source Viewer lease",
		Tags:    []string{"media"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *sourcePositionHTTPInput) (*sourcePositionHTTPOutput, error) {
		if leases == nil {
			return nil, huma.Error503ServiceUnavailable("media delivery is unavailable")
		}
		result, err := leases.ResolveSourcePosition(ctx, input.ProjectID, input.AssetID, input.Body)
		if err != nil {
			return nil, mediaDeliveryError(err)
		}
		return &sourcePositionHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-sequence-preview-lease", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/media-leases",
		Summary: "Prepare an immutable creator Viewer sequence-preview capability",
		Tags:    []string{"media"}, Middlewares: requireAuthority(api, authorizer),
		Extensions: map[string]any{"x-open-cut-surface": "first-party-creator"},
	}, func(ctx context.Context, input *sequencePreviewLeaseHTTPInput) (*sequencePreviewLeaseHTTPOutput, error) {
		if sequenceLeases == nil {
			return nil, huma.Error503ServiceUnavailable("sequence preview delivery is unavailable")
		}
		result, err := sequenceLeases.Create(ctx, input.ProjectID, input.SequenceID, input.Body)
		if err != nil {
			return nil, mediaDeliveryError(err)
		}
		return &sequencePreviewLeaseHTTPOutput{Body: result}, nil
	})

	mux.HandleFunc("/v1/media/content/", mediaContentHandler(leases, sequenceLeases, authorizer))
}

func mediaContentHandler(
	leases *service.MediaLeaseService,
	sequenceLeases *service.SequencePreviewLeaseService,
	authorizer service.Authorizer,
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		writer := &mediaStatusWriter{ResponseWriter: response}
		if authorizer == nil ||
			(request.Method != http.MethodGet && request.Method != http.MethodHead) || request.URL.RawQuery != "" {
			writeMediaContentError(writer, http.StatusNotFound)
			return
		}
		token := strings.TrimPrefix(request.URL.Path, "/v1/media/content/")
		if token == "" || strings.Contains(token, "/") {
			writeMediaContentError(writer, http.StatusNotFound)
			return
		}
		sessions := request.Header.Values(headerUISession)
		if len(sessions) != 1 || sessions[0] == "" || strings.Contains(sessions[0], ",") ||
			request.Header.Get(headerCLIGrant) != "" || request.Header.Get(headerCLIChallenge) != "" ||
			request.Header.Get(headerCLISignature) != "" {
			writeMediaContentError(writer, http.StatusUnauthorized)
			return
		}
		authority, err := authorizer.Authorize(request.Context(), service.AuthorizationRequest{
			Method: request.Method, Route: mediaContentRoute, Path: request.URL.EscapedPath(),
			UISession: sessions[0],
		})
		if err != nil || authority.Surface != application.AuthorityFirstPartyUI {
			writeMediaContentError(writer, http.StatusUnauthorized)
			return
		}
		ctx, err := application.ContextWithAuthority(request.Context(), authority)
		if err != nil {
			writeMediaContentError(writer, http.StatusUnauthorized)
			return
		}
		binder, ok := authorizer.(service.UISessionContextBinder)
		if !ok {
			writeMediaContentError(writer, http.StatusUnauthorized)
			return
		}
		ctx, err = binder.BindUISession(ctx, sessions[0])
		if err != nil {
			writeMediaContentError(writer, http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasPrefix(token, "oc_media_") && leases != nil:
			err = leases.ServeContent(ctx, writer, request.WithContext(ctx), token)
		case strings.HasPrefix(token, "oc_sequence_") && sequenceLeases != nil:
			err = sequenceLeases.ServeContent(ctx, writer, request.WithContext(ctx), token)
		default:
			writeMediaContentError(writer, http.StatusNotFound)
			return
		}
		if err == nil || writer.wroteHeader {
			return
		}
		switch {
		case errors.Is(err, service.ErrMediaRangeInvalid):
			writeMediaContentError(writer, http.StatusRequestedRangeNotSatisfiable)
		case errors.Is(err, service.ErrMediaLeaseInvalid), errors.Is(err, service.ErrMediaLeaseExpired):
			writeMediaContentError(writer, http.StatusNotFound)
		case errors.Is(err, application.ErrAssetInvalid), errors.Is(err, application.ErrAssetNotFound),
			errors.Is(err, application.ErrSequencePreviewInvalid),
			errors.Is(err, application.ErrSequencePreviewNotFound),
			errors.Is(err, application.ErrSequencePreviewIntegrity):
			writeMediaContentError(writer, http.StatusGone)
		default:
			writeMediaContentError(writer, http.StatusInternalServerError)
		}
	}
}

type mediaStatusWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (writer *mediaStatusWriter) WriteHeader(status int) {
	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *mediaStatusWriter) Write(content []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(content)
}

func writeMediaContentError(writer http.ResponseWriter, status int) {
	writer.Header().Set("Cache-Control", "private, no-store, max-age=0")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
}

func mediaDeliveryError(err error) error {
	switch {
	case errors.Is(err, application.ErrProjectNotFound), errors.Is(err, application.ErrAssetNotFound),
		errors.Is(err, application.ErrRenderSequenceNotFound),
		errors.Is(err, application.ErrSequencePreviewNotFound):
		return huma.Error404NotFound("media resource not found")
	case errors.Is(err, application.ErrRenderSequenceConflict),
		errors.Is(err, application.ErrRenderInputRequired),
		errors.Is(err, application.ErrRenderFontRequired),
		errors.Is(err, application.ErrSequencePreviewRecovery):
		return huma.Error409Conflict("media preparation precondition was not satisfied", err)
	case errors.Is(err, application.ErrAssetInvalid),
		errors.Is(err, application.ErrSequencePreviewInvalid),
		errors.Is(err, service.ErrMediaLeaseInvalid):
		return huma.Error422UnprocessableEntity("media delivery request is invalid", err)
	case errors.Is(err, service.ErrMediaLeaseExpired), errors.Is(err, service.ErrUnauthorized),
		errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("media delivery authority denied")
	default:
		return huma.Error500InternalServerError("media delivery failed", err)
	}
}
