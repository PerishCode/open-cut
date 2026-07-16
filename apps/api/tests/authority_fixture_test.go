package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type creatorAuthorizer struct{}

type fixedAuthorizer struct{ authority application.Authority }

func (authorizer fixedAuthorizer) Authorize(
	context.Context,
	service.AuthorizationRequest,
) (application.Authority, error) {
	return authorizer.authority, nil
}

func (creatorAuthorizer) Authorize(context.Context, service.AuthorizationRequest) (application.Authority, error) {
	creator, _ := domain.ParseCreatorID("018f0000-0000-7000-8000-000000000201")
	return application.Authority{
		Surface: application.AuthorityFirstPartyUI, InstallationID: "installation-api-test",
		Actor: domain.CreatorActor(creator),
	}, nil
}

type projectStore interface {
	application.ProjectRepository
	application.ProjectReadRepository
	application.ActivityRepository
	application.AgentRunRepository
}

type editingStore interface {
	application.EditRepository
	application.EditReadRepository
}

type mediaStore interface {
	application.MediaRepository
	application.AssetReadRepository
	ReadAssetSourceMaterial(context.Context, domain.AssetID) (domain.SourceGrantSummary, []byte, error)
}

func testProjectApplications(
	t *testing.T,
	store projectStore,
) (*application.Projects, *application.ProjectReads, *application.ActivityReads, *application.AgentRuns) {
	t.Helper()
	projects, err := application.NewProjects(
		store, application.UUIDv7IdentityGenerator{},
		application.ClockFunc(func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	reads, err := application.NewProjectReads(store)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := application.NewActivityReads(store)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := application.NewAgentRuns(
		store, application.UUIDv7IdentityGenerator{},
		application.ClockFunc(func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	return projects, reads, activity, runs
}

func testEditingApplications(
	t *testing.T,
	store editingStore,
) (*application.Edits, *application.EditReads) {
	t.Helper()
	edits, err := application.NewEdits(
		store, application.UUIDv7IdentityGenerator{},
		application.ClockFunc(func() time.Time { return time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	reads, err := application.NewEditReads(store)
	if err != nil {
		t.Fatal(err)
	}
	return edits, reads
}

func testMediaApplications(
	t *testing.T,
	store mediaStore,
) (*application.Media, *application.AssetReads, *service.SourceAccess) {
	t.Helper()
	media, err := application.NewMedia(
		store, application.UUIDv7IdentityGenerator{},
		application.ClockFunc(func() time.Time { return time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	reads, err := application.NewAssetReads(store)
	if err != nil {
		t.Fatal(err)
	}
	sourceAccess, err := service.NewSourceAccess(media, store)
	if err != nil {
		t.Fatal(err)
	}
	return media, reads, sourceAccess
}

func creatorContext(t *testing.T) context.Context {
	t.Helper()
	authority, err := (creatorAuthorizer{}).Authorize(context.Background(), service.AuthorizationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := application.ContextWithAuthority(context.Background(), authority)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func testAgentInvocation() *application.CommandInvocation {
	id, _ := domain.ParseCommandReceiptID("018f0000-0000-7000-8000-000000000299")
	digest := domain.Digest("sha256:" + strings.Repeat("0", 64))
	return &application.CommandInvocation{
		ID: id, Command: "test fixture", Fingerprint: digest,
		Class: application.CommandReceiptNone, InputDigest: digest,
	}
}
