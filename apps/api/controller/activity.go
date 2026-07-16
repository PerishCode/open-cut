package controller

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
)

const activityPollInterval = 250 * time.Millisecond

type activityListInput = command.ActivityListInput

type activityListOutput struct {
	Body command.ActivityListData
}

type activityStreamInput struct {
	ProjectID string `query:"projectId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	After     string `query:"after" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
}

func RegisterActivity(
	api huma.API,
	reads *application.ActivityReads,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "list-activity",
		Method:      http.MethodGet,
		Path:        "/v1/activity",
		Summary:     "List durable activity strictly after a scoped cursor",
		Tags:        []string{"activity"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "activity", "list"),
		Extensions:  commandExtensions("activity", "list"),
	}, func(ctx context.Context, input *activityListInput) (*activityListOutput, error) {
		var projectID *domain.ProjectID
		if !input.ProjectID.IsZero() {
			projectID = &input.ProjectID
		}
		after, err := parseActivityCursor(input.After)
		if err != nil {
			return nil, activityError(err)
		}
		page, err := reads.List(ctx, application.ListActivityInput{
			ProjectID: projectID, After: after, Limit: input.Limit,
		})
		if err != nil {
			return nil, activityError(err)
		}
		return &activityListOutput{Body: page}, nil
	})

	sse.Register(api, huma.Operation{
		OperationID: "watch-activity",
		Method:      http.MethodGet,
		Path:        "/v1/events",
		Summary:     "Watch durable activity after a scoped cursor",
		Tags:        []string{"activity"},
		Middlewares: requireAuthority(api, authorizer),
		Extensions:  map[string]any{"x-open-cut-surface": "creator"},
	}, map[string]any{"activity": &application.ActivityEvent{}}, func(
		ctx context.Context,
		input *activityStreamInput,
		send sse.Sender,
	) {
		projectID, err := optionalProjectID(input.ProjectID)
		if err != nil {
			return
		}
		after, err := parseActivityCursor(input.After)
		if err != nil {
			return
		}
		for {
			page, err := reads.List(ctx, application.ListActivityInput{
				ProjectID: projectID, After: after, Limit: 500,
			})
			if err != nil {
				return
			}
			for _, event := range page.Events {
				// The Huma SSE helper models IDs as platform-sized integers. Product
				// cursors are exact uint64 decimal strings, so the event payload and
				// explicit `after` query remain the lossless resume contract.
				if err := send(sse.Message{Data: event, Retry: 1000}); err != nil {
					return
				}
				after = event.Cursor
			}
			if page.HasMore {
				continue
			}
			if page.Cursor.Value() > after.Value() {
				after = page.Cursor
			}
			timer := time.NewTimer(activityPollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})
}

func parseActivityCursor(value string) (domain.Cursor, error) {
	if value == "" {
		return 0, nil
	}
	var cursor domain.Cursor
	if err := cursor.UnmarshalText([]byte(value)); err != nil {
		return 0, application.ErrInvalidActivityScope
	}
	return cursor, nil
}

func optionalProjectID(value string) (*domain.ProjectID, error) {
	if value == "" {
		return nil, nil
	}
	id, err := domain.ParseProjectID(value)
	if err != nil {
		return nil, application.ErrInvalidActivityScope
	}
	return &id, nil
}

func activityError(err error) error {
	if errors.Is(err, application.ErrInvalidActivityScope) {
		return huma.Error422UnprocessableEntity("activity scope is invalid", err)
	}
	if errors.Is(err, application.ErrAuthorityMissing) || errors.Is(err, application.ErrAuthorityInvalid) {
		return huma.Error403Forbidden("activity authority denied")
	}
	return huma.Error500InternalServerError("activity operation failed", err)
}
