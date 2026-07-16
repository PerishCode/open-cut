package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

type ActivityScopeKind string

const (
	ActivityScopeProject      ActivityScopeKind = "project"
	ActivityScopeInstallation ActivityScopeKind = "installation"
)

var ErrInvalidActivityScope = errors.New("invalid activity scope")

type ActivityScope struct {
	Kind ActivityScopeKind `json:"kind" enum:"project,installation"`
	ID   string            `json:"id" minLength:"1" maxLength:"128"`
}

type ActivityActor struct {
	Kind domain.CreativeActor `json:"kind" enum:"creator,agent"`
	ID   string               `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
}

type ChangedEntityRef struct {
	Kind     string          `json:"kind"`
	ID       string          `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Revision domain.Revision `json:"revision" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
}

type ActivityOutcomeRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type ActivityEvent struct {
	Schema            string                 `json:"schema"`
	EventID           domain.ActivityEventID `json:"eventId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Scope             ActivityScope          `json:"scope"`
	Cursor            domain.Cursor          `json:"cursor" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Kind              string                 `json:"kind"`
	OccurredAt        time.Time              `json:"occurredAt"`
	Actor             *ActivityActor         `json:"actor,omitempty"`
	ProjectID         *domain.ProjectID      `json:"projectId,omitempty" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	ProjectRevision   *domain.Revision       `json:"projectRevision,omitempty" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	ChangedEntityRefs []ChangedEntityRef     `json:"changedEntityRefs" nullable:"false" maxItems:"2048"`
	Outcome           *ActivityOutcomeRef    `json:"outcome,omitempty"`
	SummaryCode       string                 `json:"summaryCode"`
}

type ActivityQuery struct {
	Scope ActivityScope
	After domain.Cursor
	Limit int
}

type ActivityPage struct {
	Events  []ActivityEvent `json:"events" nullable:"false" maxItems:"500"`
	Cursor  domain.Cursor   `json:"cursor" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	HasMore bool            `json:"hasMore"`
}

type ActivityRepository interface {
	ListActivity(context.Context, ActivityQuery) (ActivityPage, error)
}

type ActivityReads struct {
	repository ActivityRepository
}

func NewActivityReads(repository ActivityRepository) (*ActivityReads, error) {
	if repository == nil {
		return nil, fmt.Errorf("activity repository is required")
	}
	return &ActivityReads{repository: repository}, nil
}

type ListActivityInput struct {
	ProjectID *domain.ProjectID
	After     domain.Cursor
	Limit     uint16
}

func (reads *ActivityReads) List(ctx context.Context, input ListActivityInput) (ActivityPage, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return ActivityPage{}, err
	}
	scope := ActivityScope{Kind: ActivityScopeInstallation, ID: authority.InstallationID}
	if input.ProjectID != nil {
		if input.ProjectID.IsZero() {
			return ActivityPage{}, ErrInvalidActivityScope
		}
		scope = ActivityScope{Kind: ActivityScopeProject, ID: input.ProjectID.String()}
	}
	limit := int(input.Limit)
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 500 {
		return ActivityPage{}, fmt.Errorf("activity limit must be between 1 and 500")
	}
	return reads.repository.ListActivity(ctx, ActivityQuery{Scope: scope, After: input.After, Limit: limit})
}
