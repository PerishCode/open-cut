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

const sequenceExportContentRoute = "/v1/internal/platform/export-content/{lease}"

type sequenceExportDeliveryLeaseHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	ArtifactID domain.ArtifactID `path:"artifactId"`
}

type sequenceExportDeliveryLeaseHTTPOutput struct {
	Body service.SequenceExportDeliveryLease
}

func RegisterSequenceExportDelivery(
	mux *http.ServeMux,
	api huma.API,
	delivery *service.SequenceExportDeliveryService,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "create-platform-sequence-export-delivery-lease", Method: http.MethodPost,
		Path:    "/v1/internal/platform/projects/{projectId}/export-artifacts/{artifactId}/leases",
		Summary: "Create an Electron-main-only ExportArtifact delivery lease", Tags: []string{"exports"},
		Middlewares: requireAuthority(api, authorizer),
		Extensions:  map[string]any{"x-open-cut-surface": "internal-trusted-platform"},
	}, func(
		ctx context.Context,
		input *sequenceExportDeliveryLeaseHTTPInput,
	) (*sequenceExportDeliveryLeaseHTTPOutput, error) {
		if delivery == nil {
			return nil, huma.Error503ServiceUnavailable("sequence export delivery is unavailable")
		}
		lease, err := delivery.Create(ctx, input.ProjectID, input.ArtifactID)
		if err != nil {
			return nil, sequenceExportDeliveryError(err)
		}
		return &sequenceExportDeliveryLeaseHTTPOutput{Body: lease}, nil
	})

	mux.HandleFunc("/v1/internal/platform/export-content/", sequenceExportContentHandler(delivery, authorizer))
}

func sequenceExportContentHandler(
	delivery *service.SequenceExportDeliveryService,
	authorizer service.Authorizer,
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if delivery == nil || authorizer == nil || request.Method != http.MethodGet || request.URL.RawQuery != "" {
			writeSequenceExportContentError(response, http.StatusNotFound)
			return
		}
		token := strings.TrimPrefix(request.URL.Path, "/v1/internal/platform/export-content/")
		if !strings.HasPrefix(token, "oc_export_") || strings.Contains(token, "/") {
			writeSequenceExportContentError(response, http.StatusNotFound)
			return
		}
		sessions := request.Header.Values(headerUISession)
		if len(sessions) != 1 || sessions[0] == "" || strings.Contains(sessions[0], ",") ||
			request.Header.Get(headerCLIGrant) != "" || request.Header.Get(headerCLIChallenge) != "" ||
			request.Header.Get(headerCLISignature) != "" {
			writeSequenceExportContentError(response, http.StatusUnauthorized)
			return
		}
		authority, err := authorizer.Authorize(request.Context(), service.AuthorizationRequest{
			Method: request.Method, Route: sequenceExportContentRoute, Path: request.URL.EscapedPath(),
			UISession: sessions[0],
		})
		if err != nil || authority.Surface != application.AuthorityFirstPartyUI {
			writeSequenceExportContentError(response, http.StatusUnauthorized)
			return
		}
		ctx, err := application.ContextWithAuthority(request.Context(), authority)
		if err != nil {
			writeSequenceExportContentError(response, http.StatusUnauthorized)
			return
		}
		binder, ok := authorizer.(service.UISessionContextBinder)
		if !ok {
			writeSequenceExportContentError(response, http.StatusUnauthorized)
			return
		}
		ctx, err = binder.BindUISession(ctx, sessions[0])
		if err != nil {
			writeSequenceExportContentError(response, http.StatusUnauthorized)
			return
		}
		writer := &mediaStatusWriter{ResponseWriter: response}
		if err := delivery.ServeContent(ctx, writer, token); err == nil || writer.wroteHeader {
			return
		} else if errors.Is(err, service.ErrSequenceExportDeliveryInvalid) ||
			errors.Is(err, service.ErrSequenceExportDeliveryExpired) {
			writeSequenceExportContentError(response, http.StatusNotFound)
		} else if errors.Is(err, application.ErrSequenceExportNotFound) ||
			errors.Is(err, application.ErrSequenceExportIntegrity) {
			writeSequenceExportContentError(response, http.StatusGone)
		} else {
			writeSequenceExportContentError(response, http.StatusInternalServerError)
		}
	}
}

func writeSequenceExportContentError(response http.ResponseWriter, status int) {
	response.Header().Set("Cache-Control", "private, no-store, max-age=0")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
}

func sequenceExportDeliveryError(err error) error {
	switch {
	case errors.Is(err, application.ErrSequenceExportNotFound):
		return huma.Error404NotFound("sequence export artifact not found")
	case errors.Is(err, application.ErrSequenceExportIntegrity):
		return huma.Error410Gone("sequence export artifact failed integrity validation")
	case errors.Is(err, service.ErrSequenceExportDeliveryInvalid),
		errors.Is(err, application.ErrSequenceExportInvalid):
		return huma.Error422UnprocessableEntity("sequence export delivery request is invalid", err)
	case errors.Is(err, service.ErrSequenceExportDeliveryExpired), errors.Is(err, service.ErrUnauthorized),
		errors.Is(err, application.ErrAuthorityMissing), errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("sequence export delivery authority denied")
	default:
		return huma.Error500InternalServerError("sequence export delivery failed", err)
	}
}
