package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrProjectNotFound      = errors.New("project not found")
	ErrProjectNotActive     = errors.New("project is not active")
	ErrInvalidPageCursor    = errors.New("invalid project page cursor")
	ErrInvalidProjectStatus = errors.New("invalid project status filter")
)

type ProjectSummary struct {
	ID                  domain.ProjectID           `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Revision            domain.Revision            `json:"revision" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	LifecycleRevision   domain.Revision            `json:"lifecycleRevision" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Name                string                     `json:"name"`
	Status              domain.ProjectStatus       `json:"status" enum:"active,archived,tombstoned"`
	NarrativeDocumentID domain.NarrativeDocumentID `json:"narrativeDocumentId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	MainSequenceID      domain.SequenceID          `json:"mainSequenceId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
}

type TrackSummary struct {
	ID       domain.TrackID   `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Revision domain.Revision  `json:"revision" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Type     domain.TrackType `json:"type" enum:"video,audio,caption"`
	Label    string           `json:"label"`
}

type ProjectOverview struct {
	Project                   ProjectSummary         `json:"project"`
	NarrativeDocumentRevision domain.Revision        `json:"narrativeDocumentRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	NarrativeRootNodeID       domain.NarrativeNodeID `json:"narrativeRootNodeId"`
	MainSequenceRevision      domain.Revision        `json:"mainSequenceRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Format                    domain.SequenceFormat  `json:"format"`
	Tracks                    []TrackSummary         `json:"tracks" maxItems:"64" nullable:"false"`
	ActivityCursor            domain.Cursor          `json:"activityCursor" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
}

type ProjectListQuery struct {
	Status  domain.ProjectStatus
	AfterID string
	Limit   int
	ScopeID string
}

type ProjectListPage struct {
	Projects       []ProjectSummary
	HasMore        bool
	ActivityCursor domain.Cursor
}

type ProjectReadRepository interface {
	ListProjects(context.Context, ProjectListQuery) (ProjectListPage, error)
	ShowProject(context.Context, domain.ProjectID) (ProjectOverview, error)
}

type ProjectReads struct {
	repository ProjectReadRepository
}

func NewProjectReads(repository ProjectReadRepository) (*ProjectReads, error) {
	if repository == nil {
		return nil, fmt.Errorf("project read repository is required")
	}
	return &ProjectReads{repository: repository}, nil
}

type ListProjectsInput struct {
	Status domain.ProjectStatus
	After  string
	Limit  uint16
}

type ListProjectsResult struct {
	Projects       []ProjectSummary `json:"projects" maxItems:"100" nullable:"false"`
	NextAfter      string           `json:"nextAfter,omitempty" maxLength:"512"`
	ActivityCursor domain.Cursor    `json:"activityCursor" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
}

func (reads *ProjectReads) List(ctx context.Context, input ListProjectsInput) (ListProjectsResult, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return ListProjectsResult{}, err
	}
	status := input.Status
	if status == "" {
		status = domain.ProjectActive
	}
	if status != domain.ProjectActive && status != domain.ProjectArchived && status != domain.ProjectTombstoned {
		return ListProjectsResult{}, ErrInvalidProjectStatus
	}
	limit := int(input.Limit)
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return ListProjectsResult{}, fmt.Errorf("project list limit must be between 1 and 100")
	}
	afterID, err := decodeProjectPageCursor(input.After)
	if err != nil {
		return ListProjectsResult{}, err
	}
	page, err := reads.repository.ListProjects(ctx, ProjectListQuery{
		Status: status, AfterID: afterID, Limit: limit, ScopeID: authority.InstallationID,
	})
	if err != nil {
		return ListProjectsResult{}, err
	}
	result := ListProjectsResult{Projects: page.Projects, ActivityCursor: page.ActivityCursor}
	if page.HasMore && len(page.Projects) > 0 {
		result.NextAfter = encodeProjectPageCursor(page.Projects[len(page.Projects)-1].ID.String())
	}
	return result, nil
}

func (reads *ProjectReads) Show(ctx context.Context, id domain.ProjectID) (ProjectOverview, error) {
	if _, err := AuthorityFromContext(ctx); err != nil {
		return ProjectOverview{}, err
	}
	if id.IsZero() {
		return ProjectOverview{}, domain.ErrInvalidDurableID
	}
	return reads.repository.ShowProject(ctx, id)
}

func encodeProjectPageCursor(id string) string {
	return "project-id.v1." + base64.RawURLEncoding.EncodeToString([]byte(id))
}

func decodeProjectPageCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	const prefix = "project-id.v1."
	if !strings.HasPrefix(cursor, prefix) {
		return "", ErrInvalidPageCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, prefix))
	if err != nil {
		return "", ErrInvalidPageCursor
	}
	id, err := domain.ParseProjectID(string(decoded))
	if err != nil {
		return "", ErrInvalidPageCursor
	}
	return id.String(), nil
}
