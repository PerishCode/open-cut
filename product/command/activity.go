package command

import (
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type ActivityListInput struct {
	ProjectID domain.ProjectID `json:"projectId,omitempty" query:"projectId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$" doc:"Optional Project activity scope; defaults to the current installation"`
	After     string           `json:"after,omitempty" query:"after" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$" doc:"Return events strictly after this scope-local cursor"`
	Limit     uint16           `json:"limit,omitempty" query:"limit" minimum:"1" maximum:"500" default:"100" doc:"Maximum activity events to return"`
}

type ActivityListData = application.ActivityPage
