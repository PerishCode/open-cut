package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) BeginAgentRun(
	ctx context.Context,
	record application.BeginAgentRunRecord,
) (application.AgentRunOutcome, error) {
	if err := validateBeginAgentRun(record); err != nil {
		return application.AgentRunOutcome{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.AgentRunOutcome{}, err
	}
	defer tx.Rollback()
	if replayed, err := loadExistingRunRequest(ctx, tx, record.Actor, record.RequestID, "run begin", record.InputDigest, record.ProjectID); err == nil {
		detail, err := loadAgentRun(ctx, tx, record.ProjectID, replayed)
		if err != nil {
			return application.AgentRunOutcome{}, err
		}
		if err := tx.Commit(); err != nil {
			return application.AgentRunOutcome{}, err
		}
		return application.AgentRunOutcome{Run: detail, Replayed: true}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return application.AgentRunOutcome{}, err
	}
	var projectRevisionValue uint64
	var projectStatus string
	if err := tx.QueryRowContext(ctx, `SELECT revision, status FROM projects WHERE id = ?`, record.ProjectID.String()).Scan(
		&projectRevisionValue, &projectStatus,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.AgentRunOutcome{}, application.ErrProjectNotFound
		}
		return application.AgentRunOutcome{}, err
	}
	if projectStatus != string(domain.ProjectActive) {
		return application.AgentRunOutcome{}, application.ErrProjectNotActive
	}
	projectRevision, err := domain.NewRevision(projectRevisionValue)
	if err != nil || projectRevision.Value() < 1 {
		return application.AgentRunOutcome{}, application.ErrRunInvalid
	}
	createdAt := formatInstant(record.CreatedAt)
	actorID := record.Actor.IDString()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_runs (
  id, project_id, intent, initiator_kind, initiator_id, actor_id, authorization_state,
  status, started_project_revision, latest_observed_project_revision, current_turn_id,
  created_at, updated_at
) VALUES (?, ?, ?, 'agent', ?, ?, 'bound', 'active', ?, ?, ?, ?, ?)`,
		record.RunID.String(), record.ProjectID.String(), record.Intent, actorID, actorID,
		projectRevision.Value(), projectRevision.Value(), record.TurnID.String(), createdAt, createdAt,
	); err != nil {
		return application.AgentRunOutcome{}, fmt.Errorf("persist AgentRun: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_turns (
  id, run_id, project_id, generation, adapter, agent_version, prompt_version, status, started_at
) VALUES (?, ?, ?, 1, 'standalone-cli', 'external', 'standalone', 'active', ?)`,
		record.TurnID.String(), record.RunID.String(), record.ProjectID.String(), createdAt,
	); err != nil {
		return application.AgentRunOutcome{}, fmt.Errorf("persist first AgentTurn: %w", err)
	}
	cursor, err := appendRunActivity(ctx, tx, record.ProjectID, projectRevision, record.Actor,
		record.ActivityEventID, "run.began", record.RunID, record.TurnID, "run-began", record.CreatedAt)
	if err != nil {
		return application.AgentRunOutcome{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO run_request_identities (
  actor_id, request_id, command, input_digest, input_json, project_id, run_id, turn_id,
  activity_event_id, created_at
) VALUES (?, ?, 'run begin', ?, ?, ?, ?, ?, ?, ?)`,
		actorID, record.RequestID.String(), record.InputDigest.String(), string(record.InputCanonical),
		record.ProjectID.String(), record.RunID.String(), record.TurnID.String(),
		record.ActivityEventID.String(), createdAt,
	); err != nil {
		return application.AgentRunOutcome{}, fmt.Errorf("persist run begin request: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.AgentRunOutcome{}, err
	}
	detail := application.AgentRunDetail{
		ID: record.RunID, ProjectID: record.ProjectID, Intent: record.Intent, Actor: record.Actor,
		Status: application.AgentRunActive, StartedProjectRevision: projectRevision,
		LatestObservedProjectRevision: projectRevision,
		CurrentTurn: application.AgentTurn{
			ID: record.TurnID, RunID: record.RunID, ProjectID: record.ProjectID,
			Generation: 1, Status: application.AgentTurnActive, StartedAt: record.CreatedAt.UTC(),
		},
		ActivityCursor: cursor, CreatedAt: record.CreatedAt.UTC(), UpdatedAt: record.CreatedAt.UTC(),
	}
	return application.AgentRunOutcome{Run: detail}, nil
}

func (repository *SQLiteProjects) ShowAgentRun(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
) (application.AgentRunDetail, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	defer tx.Rollback()
	detail, err := loadAgentRun(ctx, tx, projectID, runID)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AgentRunDetail{}, err
	}
	return detail, nil
}

func (repository *SQLiteProjects) TransitionAgentRun(
	ctx context.Context,
	record application.TransitionAgentRunRecord,
) (application.AgentRunOutcome, error) {
	if err := validateTransitionAgentRun(record); err != nil {
		return application.AgentRunOutcome{}, err
	}
	commandName := "run " + string(record.Transition)
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.AgentRunOutcome{}, err
	}
	defer tx.Rollback()
	if replayed, err := loadExistingRunRequest(ctx, tx, record.Actor, record.RequestID, commandName, record.InputDigest, record.ProjectID); err == nil {
		if replayed != record.RunID {
			return application.AgentRunOutcome{}, application.ErrRunRequestReused
		}
		detail, err := loadAgentRun(ctx, tx, record.ProjectID, record.RunID)
		if err != nil {
			return application.AgentRunOutcome{}, err
		}
		if err := tx.Commit(); err != nil {
			return application.AgentRunOutcome{}, err
		}
		return application.AgentRunOutcome{Run: detail, Replayed: true}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return application.AgentRunOutcome{}, err
	}
	detail, err := loadAgentRun(ctx, tx, record.ProjectID, record.RunID)
	if err != nil {
		return application.AgentRunOutcome{}, err
	}
	if detail.Actor.Kind != record.Actor.Kind || detail.Actor.IDString() != record.Actor.IDString() {
		return application.AgentRunOutcome{}, application.ErrRunActorMismatch
	}
	bridgeManaged, err := agentRunBridgeManaged(ctx, tx, record.RunID)
	if err != nil {
		return application.AgentRunOutcome{}, err
	}
	if bridgeManaged && record.Transition != application.RunTransitionComplete {
		return application.AgentRunOutcome{}, application.ErrRunBridgeManaged
	}
	if isTerminalRun(detail.Status) {
		return application.AgentRunOutcome{}, application.ErrRunTerminal
	}
	if detail.CurrentTurn.ID != record.ExpectedTurnID || detail.CurrentTurn.Generation != record.ExpectedGeneration {
		return application.AgentRunOutcome{}, application.ErrRunStaleTurn
	}
	var projectRevisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, record.ProjectID.String()).Scan(&projectRevisionValue); err != nil {
		return application.AgentRunOutcome{}, err
	}
	projectRevision, err := domain.NewRevision(projectRevisionValue)
	if err != nil {
		return application.AgentRunOutcome{}, err
	}
	now := formatInstant(record.OccurredAt)
	resultTurnID := record.ExpectedTurnID
	summaryCode := "run-" + string(record.Transition) + "d"
	eventKind := "run." + string(record.Transition) + "d"
	switch record.Transition {
	case application.RunTransitionResume:
		if record.NewTurnID == nil || record.NewTurnID.IsZero() {
			return application.AgentRunOutcome{}, application.ErrRunInvalid
		}
		if err := repository.revokeTurnScratchLeases(ctx, tx, record.RunID, record.ExpectedTurnID); err != nil {
			return application.AgentRunOutcome{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_turns SET status = 'superseded', ended_at = ?
WHERE id = ? AND run_id = ?`, now, record.ExpectedTurnID.String(), record.RunID.String()); err != nil {
			return application.AgentRunOutcome{}, err
		}
		nextGeneration, err := detail.CurrentTurn.Generation.Next()
		if err != nil {
			return application.AgentRunOutcome{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_turns (
  id, run_id, project_id, generation, adapter, agent_version, prompt_version, status, started_at
) VALUES (?, ?, ?, ?, 'standalone-cli', 'external', 'standalone', 'active', ?)`,
			record.NewTurnID.String(), record.RunID.String(), record.ProjectID.String(), nextGeneration.Value(), now); err != nil {
			return application.AgentRunOutcome{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs
SET status = 'active', waiting_reason = NULL, current_turn_id = ?,
    latest_observed_project_revision = ?, updated_at = ?
WHERE id = ?`, record.NewTurnID.String(), projectRevision.Value(), now, record.RunID.String()); err != nil {
			return application.AgentRunOutcome{}, err
		}
		resultTurnID = *record.NewTurnID
		summaryCode = "run-resumed"
		eventKind = "run.resumed"
	case application.RunTransitionComplete:
		if detail.Status == application.AgentRunWaiting {
			return application.AgentRunOutcome{}, application.ErrRunBlocked
		}
		if err := repository.revokeTurnScratchLeases(ctx, tx, record.RunID, record.ExpectedTurnID); err != nil {
			return application.AgentRunOutcome{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_turns SET status = 'completed', ended_at = ? WHERE id = ?`, now, record.ExpectedTurnID.String()); err != nil {
			return application.AgentRunOutcome{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs
SET status = 'completed', latest_observed_project_revision = ?, updated_at = ?, completed_at = ?
WHERE id = ?`, projectRevision.Value(), now, now, record.RunID.String()); err != nil {
			return application.AgentRunOutcome{}, err
		}
		summaryCode = "run-completed"
		eventKind = "run.completed"
	case application.RunTransitionCancel:
		if err := repository.revokeTurnScratchLeases(ctx, tx, record.RunID, record.ExpectedTurnID); err != nil {
			return application.AgentRunOutcome{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_turns SET status = 'cancelled', ended_at = ? WHERE id = ?`, now, record.ExpectedTurnID.String()); err != nil {
			return application.AgentRunOutcome{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs
SET status = 'cancelled', latest_observed_project_revision = ?, updated_at = ?, completed_at = ?
WHERE id = ?`, projectRevision.Value(), now, now, record.RunID.String()); err != nil {
			return application.AgentRunOutcome{}, err
		}
		summaryCode = "run-cancelled"
		eventKind = "run.cancelled"
	default:
		return application.AgentRunOutcome{}, application.ErrRunInvalid
	}
	cursor, err := appendRunActivity(ctx, tx, record.ProjectID, projectRevision, record.Actor,
		record.ActivityEventID, eventKind, record.RunID, resultTurnID, summaryCode, record.OccurredAt)
	if err != nil {
		return application.AgentRunOutcome{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO run_request_identities (
  actor_id, request_id, command, input_digest, input_json, project_id, run_id, turn_id,
  activity_event_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Actor.IDString(), record.RequestID.String(), commandName, record.InputDigest.String(),
		string(record.InputCanonical), record.ProjectID.String(), record.RunID.String(), resultTurnID.String(),
		record.ActivityEventID.String(), now,
	); err != nil {
		return application.AgentRunOutcome{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AgentRunOutcome{}, err
	}
	updated, err := repository.ShowAgentRun(ctx, record.ProjectID, record.RunID)
	if err != nil {
		return application.AgentRunOutcome{}, err
	}
	updated.ActivityCursor = cursor
	return application.AgentRunOutcome{Run: updated}, nil
}

const agentRunSelect = `
SELECT run.id, run.project_id, run.intent, run.actor_id, run.status, run.waiting_reason,
       run.started_project_revision, run.latest_observed_project_revision,
       run.created_at, run.updated_at, run.completed_at,
       turn.id, turn.generation, turn.adapter, turn.agent_version, turn.prompt_version,
       turn.native_session_id, turn.status, turn.started_at, turn.ended_at,
       COALESCE(head.cursor, 0)
FROM agent_runs AS run
JOIN agent_turns AS turn ON turn.id = run.current_turn_id
LEFT JOIN activity_heads AS head ON head.scope_kind = 'project' AND head.scope_id = run.project_id `

func loadAgentRun(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	runID domain.RunID,
) (application.AgentRunDetail, error) {
	row := tx.QueryRowContext(ctx, agentRunSelect+`
WHERE run.id = ? AND run.project_id = ?`, runID.String(), projectID.String())
	var detail application.AgentRunDetail
	var runValue, projectValue, status, startedRevision, observedRevision string
	var actorID, waitingReason, completedAt sql.NullString
	var runCreatedAt, runUpdatedAt string
	var turnID string
	var turnGeneration uint64
	var adapter, agentVersion, promptVersion, turnStatus, turnStartedAt string
	var nativeSession, turnEndedAt sql.NullString
	var cursorValue uint64
	if err := row.Scan(
		&runValue, &projectValue, &detail.Intent, &actorID, &status, &waitingReason,
		&startedRevision, &observedRevision, &runCreatedAt, &runUpdatedAt, &completedAt,
		&turnID, &turnGeneration, &adapter, &agentVersion, &promptVersion,
		&nativeSession, &turnStatus, &turnStartedAt, &turnEndedAt, &cursorValue,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.AgentRunDetail{}, application.ErrRunNotFound
		}
		return application.AgentRunDetail{}, err
	}
	parsedRun, err := domain.ParseRunID(runValue)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	parsedProject, err := domain.ParseProjectID(projectValue)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	if !actorID.Valid {
		return application.AgentRunDetail{}, application.ErrRunNotFound
	}
	parsedAgent, err := domain.ParseAgentID(actorID.String)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	if err := detail.StartedProjectRevision.UnmarshalText([]byte(startedRevision)); err != nil {
		return application.AgentRunDetail{}, err
	}
	if err := detail.LatestObservedProjectRevision.UnmarshalText([]byte(observedRevision)); err != nil {
		return application.AgentRunDetail{}, err
	}
	parsedTurn, err := domain.ParseTurnID(turnID)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	generation, err := domain.NewRevision(turnGeneration)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	cursor, err := domain.NewCursor(cursorValue)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	detail.ID = parsedRun
	detail.ProjectID = parsedProject
	detail.Actor = domain.AgentActor(parsedAgent)
	detail.Status = application.AgentRunStatus(status)
	detail.WaitingReason = waitingReason.String
	detail.ActivityCursor = cursor
	detail.CreatedAt, err = time.Parse(time.RFC3339Nano, runCreatedAt)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	detail.UpdatedAt, err = time.Parse(time.RFC3339Nano, runUpdatedAt)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	if completedAt.Valid {
		instant, parseErr := time.Parse(time.RFC3339Nano, completedAt.String)
		if parseErr != nil {
			return application.AgentRunDetail{}, parseErr
		}
		detail.CompletedAt = &instant
	}
	turnStarted, err := time.Parse(time.RFC3339Nano, turnStartedAt)
	if err != nil {
		return application.AgentRunDetail{}, err
	}
	detail.CurrentTurn = application.AgentTurn{
		ID: parsedTurn, RunID: parsedRun, ProjectID: parsedProject, Generation: generation,
		Status: application.AgentTurnStatus(turnStatus), StartedAt: turnStarted,
	}
	if turnEndedAt.Valid {
		instant, parseErr := time.Parse(time.RFC3339Nano, turnEndedAt.String)
		if parseErr != nil {
			return application.AgentRunDetail{}, parseErr
		}
		detail.CurrentTurn.EndedAt = &instant
	}
	return detail, nil
}

func agentRunBridgeManaged(ctx context.Context, tx *sql.Tx, runID domain.RunID) (bool, error) {
	var managed bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM agent_bridge_runs WHERE run_id = ?)`, runID.String()).Scan(&managed); err != nil {
		return false, err
	}
	return managed, nil
}

func loadExistingRunRequest(
	ctx context.Context,
	tx *sql.Tx,
	actor domain.ActorRef,
	requestID domain.RequestID,
	commandName string,
	digest domain.Digest,
	projectID domain.ProjectID,
) (domain.RunID, error) {
	var storedCommand, storedDigest, storedProject, storedRun string
	err := tx.QueryRowContext(ctx, `
SELECT command, input_digest, project_id, run_id
FROM run_request_identities WHERE actor_id = ? AND request_id = ?`,
		actor.IDString(), requestID.String()).Scan(&storedCommand, &storedDigest, &storedProject, &storedRun)
	if err != nil {
		return domain.RunID{}, err
	}
	if storedCommand != commandName || storedDigest != digest.String() || storedProject != projectID.String() {
		return domain.RunID{}, application.ErrRunRequestReused
	}
	return domain.ParseRunID(storedRun)
}

func appendRunActivity(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	projectRevision domain.Revision,
	actor domain.ActorRef,
	eventID domain.ActivityEventID,
	kind string,
	runID domain.RunID,
	turnID domain.TurnID,
	summaryCode string,
	at time.Time,
) (domain.Cursor, error) {
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		RunID             domain.RunID                   `json:"runId"`
		TurnID            domain.TurnID                  `json:"turnId"`
	}{ChangedEntityRefs: []application.ChangedEntityRef{}, RunID: runID, TurnID: turnID})
	if err != nil {
		return 0, err
	}
	record := activityRecord{
		ScopeKind: "project", ScopeID: projectID.String(), EventID: eventID.String(), Kind: kind,
		OccurredAt: formatInstant(at), ActorKind: string(actor.Kind), ActorID: actor.IDString(),
		ProjectID: projectID.String(), ProjectRevision: int64(projectRevision.Value()),
		OutcomeKind: "run", OutcomeID: runID.String(), SummaryCode: summaryCode, Payload: payload,
	}
	cursor, err := appendActivity(ctx, tx, record)
	if err != nil {
		return 0, err
	}
	// `run begin` allocates its Run and Turn inside this transaction, so those
	// identities are not part of the pre-authorized CLI context. Supplying them
	// here keeps its outcome receipt in the same commit as the Run itself.
	if err := commandOutcomeReceiptFromActivity(ctx, tx, record, cursor, runID, turnID); err != nil {
		return 0, err
	}
	return cursor, nil
}

func validateBeginAgentRun(record application.BeginAgentRunRecord) error {
	if record.RunID.IsZero() || record.TurnID.IsZero() || record.ProjectID.IsZero() ||
		record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorAgent ||
		len(record.Intent) == 0 || len([]byte(record.Intent)) > application.MaximumRunIntentBytes ||
		record.ActivityEventID.IsZero() || !json.Valid(record.InputCanonical) {
		return application.ErrRunInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrRunInvalid
	}
	return nil
}

func validateTransitionAgentRun(record application.TransitionAgentRunRecord) error {
	if record.ProjectID.IsZero() || record.RunID.IsZero() || record.ExpectedTurnID.IsZero() ||
		record.ExpectedGeneration.Value() < 1 || record.Actor.Validate() != nil ||
		record.Actor.Kind != domain.ActorAgent || record.ActivityEventID.IsZero() || !json.Valid(record.InputCanonical) {
		return application.ErrRunInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrRunInvalid
	}
	return nil
}

func isTerminalRun(status application.AgentRunStatus) bool {
	return status == application.AgentRunCompleted || status == application.AgentRunFailed ||
		status == application.AgentRunCancelled
}
