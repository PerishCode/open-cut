package tests

import (
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func assertAgentTurnCheckpoint(
	t *testing.T,
	store *repository.SQLiteProjects,
	projectID domain.ProjectID,
	projectRevision domain.Revision,
	turnID domain.TurnID,
) {
	t.Helper()
	versions, err := application.NewProjectVersions(
		store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	page, err := versions.List(creatorContext(t), projectID, domain.ProjectVersionID{}, 20)
	if err != nil || len(page.Versions) != 2 ||
		page.Versions[0].Source != application.ProjectVersionAgentTurn ||
		page.Versions[0].CapturedProjectRevision != projectRevision ||
		page.Versions[0].TriggerID != turnID.String() {
		t.Fatalf("Agent turn checkpoint=%+v err=%v", page, err)
	}
}
