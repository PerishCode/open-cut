package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type projectVersionListHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	Before    string           `query:"before" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Limit     uint16           `query:"limit" minimum:"1" maximum:"50" default:"20"`
}

type projectVersionCreateHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	Body      application.CreateProjectVersionInput
}

type projectVersionRestoreHTTPInput struct {
	ProjectID domain.ProjectID        `path:"projectId"`
	VersionID domain.ProjectVersionID `path:"versionId"`
	Body      application.RestoreProjectVersionInput
}

type projectVersionPageHTTPOutput struct {
	Body application.ProjectVersionPage
}
type projectVersionCreateHTTPOutput struct {
	Body application.CreateProjectVersionResult
}
type projectVersionRestoreHTTPOutput struct {
	Body application.RestoreProjectVersionResult
}

func RegisterProjectVersions(api huma.API, versions *application.ProjectVersions, authorizer service.Authorizer) {
	extensions := map[string]any{"x-open-cut-surface": "first-party-creator"}
	huma.Register(api, huma.Operation{
		OperationID: "list-project-versions", Method: http.MethodGet,
		Path: "/v1/projects/{projectId}/versions", Summary: "List lightweight project recovery checkpoints",
		Tags: []string{"creator"}, Middlewares: requireAuthority(api, authorizer), Extensions: extensions,
	}, func(ctx context.Context, input *projectVersionListHTTPInput) (*projectVersionPageHTTPOutput, error) {
		if versions == nil {
			return nil, huma.Error503ServiceUnavailable("Project versions are unavailable")
		}
		var before domain.ProjectVersionID
		if input.Before != "" {
			var err error
			before, err = domain.ParseProjectVersionID(input.Before)
			if err != nil {
				return nil, projectVersionError(application.ErrProjectVersionInvalid)
			}
		}
		result, err := versions.List(ctx, input.ProjectID, before, input.Limit)
		if err != nil {
			return nil, projectVersionError(err)
		}
		return &projectVersionPageHTTPOutput{Body: result}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "create-project-version", Method: http.MethodPost,
		Path: "/v1/projects/{projectId}/versions", Summary: "Save a named lightweight project recovery checkpoint",
		Tags: []string{"creator"}, Middlewares: requireAuthority(api, authorizer), Extensions: extensions,
	}, func(ctx context.Context, input *projectVersionCreateHTTPInput) (*projectVersionCreateHTTPOutput, error) {
		if versions == nil {
			return nil, huma.Error503ServiceUnavailable("Project versions are unavailable")
		}
		result, err := versions.Create(ctx, input.ProjectID, input.Body)
		if err != nil {
			return nil, projectVersionError(err)
		}
		return &projectVersionCreateHTTPOutput{Body: result}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "restore-project-version", Method: http.MethodPost,
		Path: "/v1/projects/{projectId}/versions/{versionId}/restore", Summary: "Atomically restore a project checkpoint as a new creative revision",
		Tags: []string{"creator"}, Middlewares: requireAuthority(api, authorizer), Extensions: extensions,
	}, func(ctx context.Context, input *projectVersionRestoreHTTPInput) (*projectVersionRestoreHTTPOutput, error) {
		if versions == nil {
			return nil, huma.Error503ServiceUnavailable("Project versions are unavailable")
		}
		result, err := versions.Restore(ctx, input.ProjectID, input.VersionID, input.Body)
		if err != nil {
			return nil, projectVersionError(err)
		}
		return &projectVersionRestoreHTTPOutput{Body: result}, nil
	})
}

func projectVersionError(err error) error {
	switch {
	case errors.Is(err, application.ErrProjectNotFound), errors.Is(err, application.ErrProjectVersionNotFound):
		return huma.Error404NotFound("Project version was not found")
	case errors.Is(err, application.ErrEditConflict), errors.Is(err, application.ErrProjectVersionRequestReused):
		return huma.Error409Conflict("Project version request conflicts with the current project revision", err)
	case errors.Is(err, application.ErrProjectVersionInvalid), errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error422UnprocessableEntity("Project version request is invalid", err)
	default:
		return huma.Error500InternalServerError("Project version operation failed", err)
	}
}
