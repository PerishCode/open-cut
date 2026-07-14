package controller

import (
	"context"
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/model"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
)

type projectListOutput struct {
	Body model.ProjectSnapshot
}

type projectPutInput struct {
	ID   string `path:"id" minLength:"1" maxLength:"128"`
	Body model.ProjectWrite
}

type projectPutOutput struct {
	Body model.ProjectUpserted
}

func RegisterProjects(api huma.API, current service.Projects) {
	huma.Register(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/v1/projects",
		Summary:     "List projects",
		Tags:        []string{"projects"},
	}, func(ctx context.Context, _ *struct{}) (*projectListOutput, error) {
		snapshot, err := current.List(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("project repository failed", err)
		}
		return &projectListOutput{Body: snapshot}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "put-project",
		Method:      http.MethodPut,
		Path:        "/v1/projects/{id}",
		Summary:     "Create or replace a project",
		Tags:        []string{"projects"},
	}, func(ctx context.Context, input *projectPutInput) (*projectPutOutput, error) {
		event, err := current.Put(ctx, model.Project{
			ID: input.ID, Name: input.Body.Name, Description: input.Body.Description,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("project repository failed", err)
		}
		return &projectPutOutput{Body: event}, nil
	})

	sse.Register(api, huma.Operation{
		OperationID: "watch-projects",
		Method:      http.MethodGet,
		Path:        "/v1/events",
		Summary:     "Watch project state",
		Tags:        []string{"projects"},
	}, map[string]any{
		"project.snapshot": &model.ProjectSnapshot{},
		"project.upserted": &model.ProjectUpserted{},
	}, func(ctx context.Context, _ *struct{}, send sse.Sender) {
		subscription, err := current.Subscribe(ctx)
		if err != nil {
			return
		}
		defer subscription.Close()
		if err := send(sse.Message{ID: int(subscription.Snapshot.Revision), Retry: 1000, Data: subscription.Snapshot}); err != nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-subscription.Events:
				if !ok || send(sse.Message{ID: int(event.Revision), Data: event}) != nil {
					return
				}
			}
		}
	})
}
