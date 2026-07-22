package tests

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSQLiteAgentRunIsIdempotentGenerationFencedAndDurable(t *testing.T) {
	parallelAPITest(t)
	createdAt := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	projects, _, activity, runs := testProjectApplications(t, store)
	creatorCtx := creatorContext(t)
	projectRequest, _ := domain.ParseRequestID("gesture:create-run-project")
	project, err := projects.Create(creatorCtx, application.CreateProjectInput{
		RequestID: projectRequest, Name: "Durable writer",
	})
	if err != nil {
		t.Fatal(err)
	}

	agentValue, _ := domain.GenerateUUIDv7(createdAt)
	agentID, _ := domain.ParseAgentID(agentValue)
	grantID, _ := domain.GenerateUUIDv7(createdAt.Add(time.Millisecond))
	grant, err := store.EnsurePendingCLIGrant(context.Background(), application.PendingCLIGrant{
		ID: grantID, InstallationID: "installation-run-test", AgentID: agentID,
		PublicKey: "run-test-public-key", Fingerprint: "sha256:" + strings.Repeat("b", 64),
		Scopes: []string{"run:read", "run:write"}, CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideCLIGrant(context.Background(), grant.ID, true, createdAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	agentCtx, err := application.ContextWithAuthority(context.Background(), application.Authority{
		Surface: application.AuthorityProductCLI, InstallationID: "installation-run-test",
		GrantID: grant.ID, Actor: domain.AgentActor(agentID),
		Policy:     application.InvocationPolicy{Output: application.OutputJSON, WaitMilliseconds: 1000},
		Invocation: testAgentInvocation(),
	})
	if err != nil {
		t.Fatal(err)
	}

	beginRequest, _ := domain.ParseRequestID("agent:run:begin:001")
	begin, err := runs.Begin(agentCtx, project.Project.Project.ID, application.RunBeginInput{
		RequestID: beginRequest, Intent: "Cut a concise product walkthrough",
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runs.Begin(agentCtx, project.Project.Project.ID, application.RunBeginInput{
		RequestID: beginRequest, Intent: "Cut a concise product walkthrough",
	})
	if err != nil || !replayed.Replayed || replayed.Run.ID != begin.Run.ID ||
		replayed.Run.CurrentTurn.ID != begin.Run.CurrentTurn.ID {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}

	resumeRequest, _ := domain.ParseRequestID("agent:run:resume:001")
	type resumeOutcome struct {
		result application.RunCommandResult
		err    error
	}
	resumedResult := make(chan resumeOutcome, 1)
	go func() {
		result, resumeErr := runs.Resume(
			agentCtx, project.Project.Project.ID, begin.Run.ID, begin.Run.CurrentTurn.ID,
			application.RunResumeInput{RequestID: resumeRequest, ExpectedGeneration: begin.Run.CurrentTurn.Generation},
		)
		resumedResult <- resumeOutcome{result: result, err: resumeErr}
	}()
	waited, err := runs.Wait(agentCtx, project.Project.Project.ID, begin.Run.ID, application.RunWaitInput{
		After: begin.Run.ActivityCursor,
	})
	if err != nil || waited.Run.ActivityCursor.Value() <= begin.Run.ActivityCursor.Value() {
		t.Fatalf("waited=%+v err=%v", waited, err)
	}
	resume := <-resumedResult
	resumed, err := resume.result, resume.err
	if err != nil || resumed.Run.CurrentTurn.Generation.Value() != 2 ||
		resumed.Run.CurrentTurn.ID == begin.Run.CurrentTurn.ID {
		t.Fatalf("resumed=%+v err=%v", resumed, err)
	}
	staleRequest, _ := domain.ParseRequestID("agent:run:resume:stale")
	_, err = runs.Resume(
		agentCtx, project.Project.Project.ID, begin.Run.ID, begin.Run.CurrentTurn.ID,
		application.RunResumeInput{RequestID: staleRequest, ExpectedGeneration: begin.Run.CurrentTurn.Generation},
	)
	if !errors.Is(err, application.ErrRunStaleTurn) {
		t.Fatalf("stale transition error=%v", err)
	}

	completeRequest, _ := domain.ParseRequestID("agent:run:complete:001")
	completed, err := runs.Complete(
		agentCtx, project.Project.Project.ID, begin.Run.ID, resumed.Run.CurrentTurn.ID,
		application.RunCompleteInput{RequestID: completeRequest, ExpectedGeneration: resumed.Run.CurrentTurn.Generation},
	)
	if err != nil || completed.Run.Status != application.AgentRunCompleted || completed.Run.CompletedAt == nil {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	page, err := activity.List(creatorCtx, application.ListActivityInput{ProjectID: &project.Project.Project.ID})
	if err != nil || len(page.Events) != 4 || page.Cursor.Value() != 4 ||
		page.Events[1].Kind != "run.began" || page.Events[2].Kind != "run.resumed" ||
		page.Events[3].Kind != "run.completed" {
		t.Fatalf("activity=%+v err=%v", page, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := repository.OpenSQLiteProjects(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedRuns, err := application.NewAgentRuns(
		reopened, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopenedRuns.Show(agentCtx, project.Project.Project.ID, begin.Run.ID)
	if err != nil || stored.Run.Status != application.AgentRunCompleted ||
		stored.Run.CurrentTurn.Generation.Value() != 2 || stored.Run.ActivityCursor.Value() != 4 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}
