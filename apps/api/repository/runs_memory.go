package repository

import (
	"context"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *MemoryProjects) BeginAgentRun(
	_ context.Context,
	record application.BeginAgentRunRecord,
) (application.AgentRunOutcome, error) {
	if err := validateBeginAgentRun(record); err != nil {
		return application.AgentRunOutcome{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	requestKey := memoryRunRequestKey(record.Actor, record.RequestID)
	if existing, ok := repository.runRequests[requestKey]; ok {
		if existing.command != "run begin" || existing.digest != record.InputDigest || existing.projectID != record.ProjectID {
			return application.AgentRunOutcome{}, application.ErrRunRequestReused
		}
		return application.AgentRunOutcome{Run: repository.runs[existing.runID.String()], Replayed: true}, nil
	}
	genesis, ok := repository.projects[record.ProjectID.String()]
	if !ok {
		return application.AgentRunOutcome{}, application.ErrProjectNotFound
	}
	if genesis.Project.Status != domain.ProjectActive {
		return application.AgentRunOutcome{}, application.ErrProjectNotActive
	}
	detail := application.AgentRunDetail{
		ID: record.RunID, ProjectID: record.ProjectID, Intent: record.Intent, Actor: record.Actor,
		Status: application.AgentRunActive, StartedProjectRevision: genesis.Project.Revision,
		LatestObservedProjectRevision: genesis.Project.Revision,
		CurrentTurn: application.AgentTurn{
			ID: record.TurnID, RunID: record.RunID, ProjectID: record.ProjectID, Generation: domain.Revision(1),
			Status: application.AgentTurnActive, StartedAt: record.CreatedAt.UTC(),
		},
		CreatedAt: record.CreatedAt.UTC(), UpdatedAt: record.CreatedAt.UTC(),
	}
	detail.ActivityCursor = repository.appendMemoryRunActivity(
		record.ProjectID, genesis.Project.Revision, record.Actor, record.ActivityEventID,
		"run.began", record.RunID, record.TurnID, "run-began", record.CreatedAt,
	)
	repository.runs[record.RunID.String()] = detail
	repository.runRequests[requestKey] = memoryRunRequest{
		command: "run begin", digest: record.InputDigest, projectID: record.ProjectID, runID: record.RunID,
	}
	return application.AgentRunOutcome{Run: detail}, nil
}

func (repository *MemoryProjects) ShowAgentRun(
	_ context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
) (application.AgentRunDetail, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	detail, ok := repository.runs[runID.String()]
	if !ok || detail.ProjectID != projectID {
		return application.AgentRunDetail{}, application.ErrRunNotFound
	}
	return detail, nil
}

func (repository *MemoryProjects) TransitionAgentRun(
	_ context.Context,
	record application.TransitionAgentRunRecord,
) (application.AgentRunOutcome, error) {
	if err := validateTransitionAgentRun(record); err != nil {
		return application.AgentRunOutcome{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	commandName := "run " + string(record.Transition)
	requestKey := memoryRunRequestKey(record.Actor, record.RequestID)
	if existing, ok := repository.runRequests[requestKey]; ok {
		if existing.command != commandName || existing.digest != record.InputDigest ||
			existing.projectID != record.ProjectID || existing.runID != record.RunID {
			return application.AgentRunOutcome{}, application.ErrRunRequestReused
		}
		return application.AgentRunOutcome{Run: repository.runs[record.RunID.String()], Replayed: true}, nil
	}
	detail, ok := repository.runs[record.RunID.String()]
	if !ok || detail.ProjectID != record.ProjectID {
		return application.AgentRunOutcome{}, application.ErrRunNotFound
	}
	if detail.Actor != record.Actor {
		return application.AgentRunOutcome{}, application.ErrRunActorMismatch
	}
	if isTerminalRun(detail.Status) {
		return application.AgentRunOutcome{}, application.ErrRunTerminal
	}
	if detail.CurrentTurn.ID != record.ExpectedTurnID || detail.CurrentTurn.Generation != record.ExpectedGeneration {
		return application.AgentRunOutcome{}, application.ErrRunStaleTurn
	}
	genesis, ok := repository.projects[record.ProjectID.String()]
	if !ok {
		return application.AgentRunOutcome{}, application.ErrProjectNotFound
	}
	now := record.OccurredAt.UTC()
	eventKind := "run." + string(record.Transition) + "d"
	summaryCode := "run-" + string(record.Transition) + "d"
	activityTurn := detail.CurrentTurn.ID
	switch record.Transition {
	case application.RunTransitionResume:
		if record.NewTurnID == nil || record.NewTurnID.IsZero() {
			return application.AgentRunOutcome{}, application.ErrRunInvalid
		}
		next, err := detail.CurrentTurn.Generation.Next()
		if err != nil {
			return application.AgentRunOutcome{}, err
		}
		ended := now
		detail.CurrentTurn.Status = application.AgentTurnSuperseded
		detail.CurrentTurn.EndedAt = &ended
		detail.CurrentTurn = application.AgentTurn{
			ID: *record.NewTurnID, RunID: record.RunID, ProjectID: record.ProjectID, Generation: next,
			Status: application.AgentTurnActive, StartedAt: now,
		}
		detail.Status = application.AgentRunActive
		detail.WaitingReason = ""
		activityTurn = *record.NewTurnID
		eventKind, summaryCode = "run.resumed", "run-resumed"
	case application.RunTransitionComplete:
		if detail.Status == application.AgentRunWaiting {
			return application.AgentRunOutcome{}, application.ErrRunBlocked
		}
		detail.Status = application.AgentRunCompleted
		detail.CurrentTurn.Status = application.AgentTurnCompleted
		detail.CurrentTurn.EndedAt = &now
		detail.CompletedAt = &now
		eventKind, summaryCode = "run.completed", "run-completed"
	case application.RunTransitionCancel:
		detail.Status = application.AgentRunCancelled
		detail.CurrentTurn.Status = application.AgentTurnCancelled
		detail.CurrentTurn.EndedAt = &now
		detail.CompletedAt = &now
		eventKind, summaryCode = "run.cancelled", "run-cancelled"
	default:
		return application.AgentRunOutcome{}, application.ErrRunInvalid
	}
	detail.LatestObservedProjectRevision = genesis.Project.Revision
	detail.UpdatedAt = now
	detail.ActivityCursor = repository.appendMemoryRunActivity(
		record.ProjectID, genesis.Project.Revision, record.Actor, record.ActivityEventID,
		eventKind, record.RunID, activityTurn, summaryCode, record.OccurredAt,
	)
	repository.runs[record.RunID.String()] = detail
	repository.runRequests[requestKey] = memoryRunRequest{
		command: commandName, digest: record.InputDigest, projectID: record.ProjectID, runID: record.RunID,
	}
	return application.AgentRunOutcome{Run: detail}, nil
}

func (repository *MemoryProjects) appendMemoryRunActivity(
	projectID domain.ProjectID,
	projectRevision domain.Revision,
	actor domain.ActorRef,
	eventID domain.ActivityEventID,
	kind string,
	runID domain.RunID,
	turnID domain.TurnID,
	summaryCode string,
	at time.Time,
) domain.Cursor {
	scope := application.ActivityScope{Kind: application.ActivityScopeProject, ID: projectID.String()}
	stored := repository.activity[activityScopeKey(scope)]
	cursor, _ := domain.NewCursor(uint64(len(stored) + 1))
	actorValue := &application.ActivityActor{Kind: actor.Kind, ID: actor.IDString()}
	projectValue := projectID
	revisionValue := projectRevision
	repository.activity[activityScopeKey(scope)] = append(stored, application.ActivityEvent{
		Schema: application.ActivitySchema, EventID: eventID, Scope: scope, Cursor: cursor, Kind: kind,
		OccurredAt: at.UTC(), Actor: actorValue, ProjectID: &projectValue, ProjectRevision: &revisionValue,
		Outcome: &application.ActivityOutcomeRef{Kind: "run", ID: runID.String()}, SummaryCode: summaryCode,
		ChangedEntityRefs: []application.ChangedEntityRef{},
	})
	_ = turnID
	return cursor
}

func memoryRunRequestKey(actor domain.ActorRef, requestID domain.RequestID) string {
	return actor.IDString() + "\x00" + requestID.String()
}
