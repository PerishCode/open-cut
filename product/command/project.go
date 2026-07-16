package command

import (
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type ProjectListInput struct {
	Status domain.ProjectStatus `json:"status,omitempty" query:"status" enum:"active,archived,tombstoned" doc:"Optional lifecycle filter"`
	After  string               `json:"after,omitempty" query:"after" maxLength:"512" doc:"Opaque query-local continuation cursor"`
	Limit  uint16               `json:"limit,omitempty" query:"limit" minimum:"1" maximum:"100" default:"50" doc:"Maximum summaries to return"`
}

type ProjectShowInput struct {
	ProjectID domain.ProjectID `json:"projectId" path:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$" doc:"Project to inspect"`
}

type ProjectSummary = application.ProjectSummary

type ProjectListData = application.ListProjectsResult

type TrackSummary = application.TrackSummary

type ProjectOverview = application.ProjectOverview

type ProjectShowData = application.ProjectOverview
