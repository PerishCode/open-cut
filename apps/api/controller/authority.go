package controller

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/authwire"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const (
	headerUISession         = "X-Open-Cut-UI-Session"
	headerCLIGrant          = authwire.HeaderGrant
	headerCLIChallenge      = authwire.HeaderChallenge
	headerCLISignature      = authwire.HeaderSignature
	headerCLIAuthStatus     = authwire.HeaderAuthStatus
	headerCLIPairingID      = authwire.HeaderPairingID
	headerCLIScopeUpgradeID = authwire.HeaderScopeUpgradeID
)

func requireAuthority(api huma.API, authorizer service.Authorizer) huma.Middlewares {
	return requireAuthorityForCommand(api, nil, authorizer, false, nil)
}

func requireCommandAuthority(
	api huma.API,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
	path ...string,
) huma.Middlewares {
	return requireAuthorityForCommand(api, runs, authorizer, false, path)
}

func requireCommandBodyAuthority(
	api huma.API,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
	path ...string,
) huma.Middlewares {
	return requireAuthorityForCommand(api, runs, authorizer, true, path)
}

func requireAuthorityForCommand(
	api huma.API,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
	signedBody bool,
	path []string,
) huma.Middlewares {
	if authorizer == nil {
		authorizer = service.RejectAuthorizer{}
	}
	commandName := ""
	fingerprint := ""
	requiredScope := ""
	var descriptor command.Descriptor
	if len(path) > 0 {
		registry := command.InitialRegistry()
		var err error
		descriptor, err = registry.Lookup(path)
		if err != nil {
			panic("API command authority is not registered: " + strings.Join(path, " "))
		}
		fingerprint, err = registry.Fingerprint(path)
		if err != nil {
			panic("API command fingerprint is unavailable: " + strings.Join(path, " "))
		}
		commandName = strings.Join(path, " ")
		requiredScope = string(descriptor.RequiredScope)
	}
	middlewares := huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		bodyDigest := ""
		if commandName != "" {
			bodyDigest = authwire.NoBodyDigest(commandName)
		}
		if signedBody {
			request, _ := humago.Unwrap(ctx)
			raw, err := io.ReadAll(io.LimitReader(request.Body, authwire.MaximumCommandBodyBytes+1))
			if err != nil || len(raw) > authwire.MaximumCommandBodyBytes {
				_ = huma.WriteErr(api, ctx, http.StatusRequestEntityTooLarge, "command body exceeds its limit")
				return
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
			request.ContentLength = int64(len(raw))
			digest, err := authwire.CommandBodyDigest(commandName, raw)
			if err != nil {
				_ = huma.WriteErr(api, ctx, http.StatusUnprocessableEntity, "command body is not canonical JSON")
				return
			}
			bodyDigest = digest.String()
		}
		requestURL := ctx.URL()
		authority, err := authorizer.Authorize(ctx.Context(), service.AuthorizationRequest{
			Method: ctx.Method(), Route: ctx.Operation().Path, Path: requestURL.EscapedPath(), Query: requestURL.RawQuery,
			BodyDigest: bodyDigest, Command: commandName,
			CommandFingerprint: fingerprint, RequiredScope: requiredScope,
			UISession: ctx.Header(headerUISession), CLIGrant: ctx.Header(headerCLIGrant),
			CLIChallenge: ctx.Header(headerCLIChallenge), CLISignature: ctx.Header(headerCLISignature),
		})
		if err != nil {
			responseStatus := http.StatusUnauthorized
			var pairing *service.CLIPairingRequiredError
			var upgrade *service.CLIScopeUpgradeRequiredError
			if errors.As(err, &pairing) {
				ctx.SetHeader(headerCLIAuthStatus, authwire.AuthStatusPairingRequired)
				ctx.SetHeader(headerCLIPairingID, pairing.Grant.ID)
			} else if errors.As(err, &upgrade) {
				responseStatus = http.StatusForbidden
				ctx.SetHeader(headerCLIAuthStatus, authwire.AuthStatusScopeUpgradeRequired)
				ctx.SetHeader(headerCLIScopeUpgradeID, upgrade.Upgrade.ID)
			} else if errors.Is(err, service.ErrCLIGrantDenied) {
				ctx.SetHeader(headerCLIAuthStatus, authwire.AuthStatusPairingDenied)
			} else if errors.Is(err, service.ErrCLIGrantRevoked) {
				ctx.SetHeader(headerCLIAuthStatus, authwire.AuthStatusGrantRevoked)
			} else if errors.Is(err, service.ErrCLIGrantScopeDenied) {
				ctx.SetHeader(headerCLIAuthStatus, authwire.AuthStatusScopeDenied)
			} else if errors.Is(err, service.ErrCLIGrantAuthorityChanged) {
				ctx.SetHeader(headerCLIAuthStatus, authwire.AuthStatusGrantAuthorityChanged)
			}
			_ = huma.WriteErr(api, ctx, responseStatus, "product authority required")
			return
		}
		authorized, err := application.ContextWithAuthority(ctx.Context(), authority)
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "product authority invalid")
			return
		}
		if authority.Surface == application.AuthorityFirstPartyUI {
			binder, ok := authorizer.(service.UISessionContextBinder)
			if ok {
				authorized, err = binder.BindUISession(authorized, ctx.Header(headerUISession))
				if err != nil {
					_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "first-party session binding rejected")
					return
				}
			}
		}
		next(huma.WithContext(ctx, authorized))
	}}
	if commandName != "" && descriptor.Receipt != command.ReceiptNone {
		middlewares = append(middlewares, recordCommandBusinessFailures(api, runs, descriptor))
	}
	return middlewares
}
