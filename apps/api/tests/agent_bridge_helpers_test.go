package tests

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func newSQLiteAgentBridgeProject(
	t *testing.T,
) (*repository.SQLiteProjects, *application.Projects, domain.ProjectID) {
	t.Helper()
	store, err := repository.OpenSQLiteProjects(context.Background(), filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	projects, err := application.NewProjects(
		store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	requestID, _ := domain.ParseRequestID("gesture:create:agent-bridge")
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: requestID, Name: "Agent bridge project",
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	return store, projects, created.Project.Project.ID
}

func newAgentBridgesForTest(t *testing.T, store application.AgentBridgeRepository) *application.AgentBridges {
	t.Helper()
	bridges, err := application.NewAgentBridges(
		store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	return bridges
}

func newAgentBridgeRuntimeForTest(
	t *testing.T,
	bridges *application.AgentBridges,
	store application.AgentBridgeRepository,
	adapter service.AgentTurnAdapter,
	publisher service.AgentPresentationBus,
) *service.AgentBridgeService {
	t.Helper()
	runtime, err := service.NewAgentBridgeService(
		context.Background(), bridges, store, adapter, publisher, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func waitForAgentBridge(
	t *testing.T,
	runtime *service.AgentBridgeService,
	projectID domain.ProjectID,
	runID domain.RunID,
	done func(application.AgentBridgeRun) bool,
) application.AgentBridgeRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := runtime.Show(creatorContext(t), projectID, runID)
		if err == nil && done(run) {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("AgentBridge did not reach expected state")
	return application.AgentBridgeRun{}
}
