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

type projectCreateInput struct {
	Body application.CreateProjectInput
}

type projectCreateOutput struct {
	Body application.CreateProjectResult
}

type projectListInput = command.ProjectListInput

type projectListOutput struct {
	Body command.ProjectListData
}

type projectShowInput = command.ProjectShowInput

type projectShowOutput struct {
	Body command.ProjectShowData
}

func RegisterProjects(
	api huma.API,
	projects *application.Projects,
	reads *application.ProjectReads,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	creatorAuthority := requireAuthority(api, authorizer)
	huma.Register(api, huma.Operation{
		OperationID: "create-project",
		Method:      http.MethodPost,
		Path:        "/v1/projects",
		Summary:     "Create a Project with its initial narrative and Sequence",
		Tags:        []string{"projects"},
		Middlewares: creatorAuthority,
		Extensions:  map[string]any{"x-open-cut-surface": "creator"},
	}, func(ctx context.Context, input *projectCreateInput) (*projectCreateOutput, error) {
		created, err := projects.Create(ctx, input.Body)
		if err != nil {
			return nil, projectError(err)
		}
		return &projectCreateOutput{Body: created}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/v1/projects",
		Summary:     "List a bounded page of Project summaries",
		Tags:        []string{"projects"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "project", "list"),
		Extensions:  commandExtensions("project", "list"),
	}, func(ctx context.Context, input *projectListInput) (*projectListOutput, error) {
		page, err := reads.List(ctx, application.ListProjectsInput{
			Status: input.Status, After: input.After, Limit: input.Limit,
		})
		if err != nil {
			return nil, projectError(err)
		}
		return &projectListOutput{Body: page}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "show-project",
		Method:      http.MethodGet,
		Path:        "/v1/projects/{id}",
		Summary:     "Show one bounded Project overview",
		Tags:        []string{"projects"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "project", "show"),
		Extensions:  commandExtensions("project", "show"),
	}, func(ctx context.Context, input *projectShowInput) (*projectShowOutput, error) {
		overview, err := reads.Show(ctx, input.ProjectID)
		if err != nil {
			return nil, projectError(err)
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, overview, application.CommandReceiptSucceeded,
			[]application.CommandReceiptRef{
				commandReceiptRef("project", overview.Project.ID.String(), overview.Project.Revision),
			}, overview.Project.Revision, overview.ActivityCursor,
		); err != nil {
			return nil, projectError(err)
		}
		return &projectShowOutput{Body: overview}, nil
	})
}

func projectError(err error) error {
	switch {
	case errors.Is(err, application.ErrProjectNotFound):
		return commandStatusError(command.StatusNotFound, huma.Error404NotFound("project not found"))
	case errors.Is(err, application.ErrRequestIdentityReused),
		errors.Is(err, application.ErrInvalidPageCursor),
		errors.Is(err, application.ErrInvalidProjectStatus),
		errors.Is(err, domain.ErrInvalidProjectName),
		errors.Is(err, domain.ErrInvalidSequenceFormat),
		errors.Is(err, domain.ErrInvalidRequestID):
		return commandStatusError(command.StatusInvalid, huma.Error422UnprocessableEntity("project request is invalid", err))
	case errors.Is(err, application.ErrAuthorityMissing),
		errors.Is(err, application.ErrAuthorityInvalid),
		errors.Is(err, application.ErrAuthorityScopeDenied):
		return huma.Error403Forbidden("project authority denied")
	default:
		return commandStatusError(command.StatusFailed, huma.Error500InternalServerError("project operation failed", err))
	}
}
