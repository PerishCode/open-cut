package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) BeginAgentBridge(
	ctx context.Context,
	record application.BeginAgentBridgeRecord,
) (application.AgentBridgeResult, error) {
	if err := validateBeginAgentBridge(record); err != nil {
		return application.AgentBridgeResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	defer tx.Rollback()
	if result, found, err := loadAgentBridgeReplay(ctx, tx, record.Creator, record.RequestID, "begin", record.RequestDigest, record.ProjectID); err != nil {
		return application.AgentBridgeResult{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return application.AgentBridgeResult{}, err
		}
		result.Replayed = true
		return result, nil
	}
	projectRevision, err := activeProjectRevision(ctx, tx, record.ProjectID)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := validateBridgeSequence(ctx, tx, record.ProjectID, record.SequenceID); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := validateAgentContextAttachments(ctx, tx, record.ProjectID, record.SequenceID, record.Attachments); err != nil {
		return application.AgentBridgeResult{}, err
	}
	now := formatInstant(record.CreatedAt)
	creatorID := record.Creator.IDString()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_runs (
  id, project_id, intent, initiator_kind, initiator_id, actor_id, authorization_state,
  status, started_project_revision, latest_observed_project_revision, current_turn_id,
  created_at, updated_at
) VALUES (?, ?, ?, 'creator', ?, NULL, 'pending', 'authorizing', ?, ?, ?, ?, ?)`,
		record.RunID.String(), record.ProjectID.String(), record.Intent, creatorID,
		projectRevision.Value(), projectRevision.Value(), record.TurnID.String(), now, now,
	); err != nil {
		return application.AgentBridgeResult{}, fmt.Errorf("persist Creator AgentRun: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_turns (
  id, run_id, project_id, generation, adapter, agent_version, prompt_version, status, started_at
) VALUES (?, ?, ?, 1, ?, 'unresolved', 'open-cut-agent-v1', 'starting', ?)`,
		record.TurnID.String(), record.RunID.String(), record.ProjectID.String(),
		application.AgentBridgeAdapterCodexV1, now,
	); err != nil {
		return application.AgentBridgeResult{}, fmt.Errorf("persist Creator AgentTurn: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_bridge_runs (run_id, adapter_id, created_at) VALUES (?, ?, ?)`,
		record.RunID.String(), application.AgentBridgeAdapterCodexV1, now,
	); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := insertAgentBridgeTurn(ctx, tx, record.TurnID, record.SequenceID, now); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := insertAgentContextAttachments(ctx, tx, record.TurnID, record.Attachments); err != nil {
		return application.AgentBridgeResult{}, err
	}
	message := application.AgentConversationMessage{
		ID: record.MessageID, ProjectID: record.ProjectID, RunID: record.RunID, TurnID: record.TurnID,
		Ordinal: 1, Role: application.AgentConversationCreator, Text: record.Intent,
		Attachments: append([]application.AgentContextAttachment(nil), record.Attachments...), CreatedAt: record.CreatedAt.UTC(),
	}
	if err := insertAgentConversationMessage(ctx, tx, message); err != nil {
		return application.AgentBridgeResult{}, err
	}
	cursor, err := appendRunActivity(
		ctx, tx, record.ProjectID, projectRevision, record.Creator, record.ActivityEventID,
		"run.authorizing", record.RunID, record.TurnID, "run-authorizing", record.CreatedAt,
	)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := insertAgentBridgeRequest(ctx, tx, record.Creator, record.RequestID, "begin", record.RequestDigest,
		record.RequestCanonical, record.ProjectID, record.RunID, record.TurnID, &record.MessageID,
		record.ActivityEventID, now); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AgentBridgeResult{}, err
	}
	run := application.AgentBridgeRun{
		ID: record.RunID, ProjectID: record.ProjectID, Intent: record.Intent,
		Status: application.AgentRunAuthorizing,
		CurrentTurn: application.AgentBridgeTurn{
			ID: record.TurnID, Generation: 1, SequenceID: record.SequenceID,
			Status: application.AgentTurnStarting, StartedAt: record.CreatedAt.UTC(),
		},
		ActivityCursor: cursor, CreatedAt: record.CreatedAt.UTC(), UpdatedAt: record.CreatedAt.UTC(),
	}
	return application.AgentBridgeResult{Run: run, Message: &message}, nil
}

func (repository *SQLiteProjects) ContinueAgentBridge(
	ctx context.Context,
	record application.ContinueAgentBridgeRecord,
) (application.AgentBridgeResult, error) {
	if err := validateContinueAgentBridge(record); err != nil {
		return application.AgentBridgeResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	defer tx.Rollback()
	if result, found, err := loadAgentBridgeReplay(ctx, tx, record.Creator, record.RequestID, "continue", record.RequestDigest, record.ProjectID); err != nil {
		return application.AgentBridgeResult{}, err
	} else if found {
		if result.Run.ID != record.RunID {
			return application.AgentBridgeResult{}, application.ErrAgentBridgeRequestReused
		}
		if err := tx.Commit(); err != nil {
			return application.AgentBridgeResult{}, err
		}
		result.Replayed = true
		return result, nil
	}
	run, authorizationState, initiatorID, err := loadAgentBridgeTx(ctx, tx, record.ProjectID, record.RunID)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if initiatorID != record.Creator.IDString() {
		return application.AgentBridgeResult{}, application.ErrAgentBridgeNotFound
	}
	if terminalAgentRun(run.Status) {
		return application.AgentBridgeResult{}, application.ErrAgentBridgeTerminal
	}
	if run.CurrentTurn.Generation != record.ExpectedGeneration {
		return application.AgentBridgeResult{}, application.ErrAgentBridgeStaleTurn
	}
	if run.CurrentTurn.Status == application.AgentTurnStarting || run.CurrentTurn.Status == application.AgentTurnActive {
		return application.AgentBridgeResult{}, application.ErrAgentBridgeBusy
	}
	if err := validateBridgeSequence(ctx, tx, record.ProjectID, record.SequenceID); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := validateAgentContextAttachments(ctx, tx, record.ProjectID, record.SequenceID, record.Attachments); err != nil {
		return application.AgentBridgeResult{}, err
	}
	nextGeneration, err := run.CurrentTurn.Generation.Next()
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	now := formatInstant(record.CreatedAt)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_turns (
  id, run_id, project_id, generation, adapter, agent_version, prompt_version, status, started_at
) VALUES (?, ?, ?, ?, ?, 'unresolved', 'open-cut-agent-v1', 'starting', ?)`,
		record.TurnID.String(), record.RunID.String(), record.ProjectID.String(), nextGeneration.Value(),
		application.AgentBridgeAdapterCodexV1, now,
	); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := insertAgentBridgeTurn(ctx, tx, record.TurnID, record.SequenceID, now); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := insertAgentContextAttachments(ctx, tx, record.TurnID, record.Attachments); err != nil {
		return application.AgentBridgeResult{}, err
	}
	nextStatus := application.AgentRunActive
	if authorizationState == "pending" {
		nextStatus = application.AgentRunAuthorizing
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs
SET current_turn_id = ?, status = ?, waiting_reason = NULL, updated_at = ?
WHERE id = ? AND project_id = ?`, record.TurnID.String(), string(nextStatus), now,
		record.RunID.String(), record.ProjectID.String()); err != nil {
		return application.AgentBridgeResult{}, err
	}
	ordinal, err := nextConversationOrdinal(ctx, tx, record.RunID)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	message := application.AgentConversationMessage{
		ID: record.MessageID, ProjectID: record.ProjectID, RunID: record.RunID, TurnID: record.TurnID,
		Ordinal: ordinal, Role: application.AgentConversationCreator, Text: record.Message,
		Attachments: append([]application.AgentContextAttachment(nil), record.Attachments...), CreatedAt: record.CreatedAt.UTC(),
	}
	if err := insertAgentConversationMessage(ctx, tx, message); err != nil {
		return application.AgentBridgeResult{}, err
	}
	projectRevision, err := activeProjectRevision(ctx, tx, record.ProjectID)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	cursor, err := appendRunActivity(ctx, tx, record.ProjectID, projectRevision, record.Creator,
		record.ActivityEventID, "run.resumed", record.RunID, record.TurnID, "run-resumed", record.CreatedAt)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := insertAgentBridgeRequest(ctx, tx, record.Creator, record.RequestID, "continue", record.RequestDigest,
		record.RequestCanonical, record.ProjectID, record.RunID, record.TurnID, &record.MessageID,
		record.ActivityEventID, now); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AgentBridgeResult{}, err
	}
	run.CurrentTurn = application.AgentBridgeTurn{
		ID: record.TurnID, Generation: nextGeneration, SequenceID: record.SequenceID,
		Status: application.AgentTurnStarting, StartedAt: record.CreatedAt.UTC(),
	}
	run.Status, run.WaitingReason, run.ActivityCursor, run.UpdatedAt = nextStatus, "", cursor, record.CreatedAt.UTC()
	return application.AgentBridgeResult{Run: run, Message: &message}, nil
}

func (repository *SQLiteProjects) TransitionAgentBridge(
	ctx context.Context,
	record application.TransitionAgentBridgeRecord,
) (application.AgentBridgeResult, error) {
	if err := validateTransitionAgentBridge(record); err != nil {
		return application.AgentBridgeResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	defer tx.Rollback()
	commandName := string(record.Transition)
	if result, found, err := loadAgentBridgeReplay(ctx, tx, record.Creator, record.RequestID, commandName, record.RequestDigest, record.ProjectID); err != nil {
		return application.AgentBridgeResult{}, err
	} else if found {
		if result.Run.ID != record.RunID {
			return application.AgentBridgeResult{}, application.ErrAgentBridgeRequestReused
		}
		if err := tx.Commit(); err != nil {
			return application.AgentBridgeResult{}, err
		}
		result.Replayed = true
		return result, nil
	}
	run, _, initiatorID, err := loadAgentBridgeTx(ctx, tx, record.ProjectID, record.RunID)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if initiatorID != record.Creator.IDString() {
		return application.AgentBridgeResult{}, application.ErrAgentBridgeNotFound
	}
	if terminalAgentRun(run.Status) {
		return application.AgentBridgeResult{}, application.ErrAgentBridgeTerminal
	}
	if run.CurrentTurn.ID != record.ExpectedTurnID || run.CurrentTurn.Generation != record.ExpectedGeneration {
		return application.AgentBridgeResult{}, application.ErrAgentBridgeStaleTurn
	}
	now := formatInstant(record.OccurredAt)
	if record.Transition == application.AgentBridgeInterrupt &&
		run.CurrentTurn.Status != application.AgentTurnStarting && run.CurrentTurn.Status != application.AgentTurnActive {
		return application.AgentBridgeResult{}, application.ErrAgentBridgeBusy
	}
	if run.CurrentTurn.Status == application.AgentTurnStarting || run.CurrentTurn.Status == application.AgentTurnActive {
		if _, err := tx.ExecContext(ctx, `UPDATE agent_turns SET status = 'cancelled', ended_at = ? WHERE id = ?`,
			now, run.CurrentTurn.ID.String()); err != nil {
			return application.AgentBridgeResult{}, err
		}
		if err := repository.revokeTurnScratchLeases(ctx, tx, record.RunID, run.CurrentTurn.ID); err != nil {
			return application.AgentBridgeResult{}, err
		}
		run.CurrentTurn.Status = application.AgentTurnCancelled
		ended := record.OccurredAt.UTC()
		run.CurrentTurn.EndedAt = &ended
	}
	eventKind, summaryCode := "run.interrupted", "run-interrupted"
	if record.Transition == application.AgentBridgeInterrupt {
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs SET status = ?, waiting_reason = 'creator-interrupted', updated_at = ? WHERE id = ?`,
			string(application.AgentRunPaused), now, record.RunID.String()); err != nil {
			return application.AgentBridgeResult{}, err
		}
		run.Status, run.WaitingReason = application.AgentRunPaused, "creator-interrupted"
	} else {
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs SET status = 'cancelled', waiting_reason = NULL, updated_at = ?, completed_at = ? WHERE id = ?`,
			now, now, record.RunID.String()); err != nil {
			return application.AgentBridgeResult{}, err
		}
		run.Status, run.WaitingReason = application.AgentRunCancelled, ""
		completed := record.OccurredAt.UTC()
		run.CompletedAt = &completed
		eventKind, summaryCode = "run.cancelled", "run-cancelled"
	}
	projectRevision, err := activeProjectRevision(ctx, tx, record.ProjectID)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	cursor, err := appendRunActivity(ctx, tx, record.ProjectID, projectRevision, record.Creator,
		record.ActivityEventID, eventKind, record.RunID, run.CurrentTurn.ID, summaryCode, record.OccurredAt)
	if err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := insertAgentBridgeRequest(ctx, tx, record.Creator, record.RequestID, commandName, record.RequestDigest,
		record.RequestCanonical, record.ProjectID, record.RunID, run.CurrentTurn.ID, nil,
		record.ActivityEventID, now); err != nil {
		return application.AgentBridgeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AgentBridgeResult{}, err
	}
	run.ActivityCursor, run.UpdatedAt = cursor, record.OccurredAt.UTC()
	return application.AgentBridgeResult{Run: run}, nil
}

func (repository *SQLiteProjects) ShowAgentBridge(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
) (application.AgentBridgeRun, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.AgentBridgeRun{}, err
	}
	defer tx.Rollback()
	run, _, _, err := loadAgentBridgeTx(ctx, tx, projectID, runID)
	if err != nil {
		return application.AgentBridgeRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AgentBridgeRun{}, err
	}
	return run, nil
}

func (repository *SQLiteProjects) ListAgentBridges(
	ctx context.Context,
	projectID domain.ProjectID,
	limit uint32,
) (application.AgentBridgeRunPage, error) {
	if projectID.IsZero() || limit == 0 || limit > application.MaximumAgentBridgeRunPage {
		return application.AgentBridgeRunPage{}, application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.AgentBridgeRunPage{}, err
	}
	defer tx.Rollback()
	if _, err := activeProjectRevision(ctx, tx, projectID); err != nil {
		return application.AgentBridgeRunPage{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT run.id
FROM agent_runs AS run
JOIN agent_bridge_runs AS bridge ON bridge.run_id = run.id
WHERE run.project_id = ?
ORDER BY run.created_at DESC, run.id DESC
LIMIT ?`, projectID.String(), limit)
	if err != nil {
		return application.AgentBridgeRunPage{}, err
	}
	ids := make([]domain.RunID, 0, limit)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return application.AgentBridgeRunPage{}, err
		}
		id, err := domain.ParseRunID(value)
		if err != nil {
			rows.Close()
			return application.AgentBridgeRunPage{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return application.AgentBridgeRunPage{}, err
	}
	if err := rows.Err(); err != nil {
		return application.AgentBridgeRunPage{}, err
	}
	runs := make([]application.AgentBridgeRun, 0, len(ids))
	for _, id := range ids {
		run, _, _, err := loadAgentBridgeTx(ctx, tx, projectID, id)
		if err != nil {
			return application.AgentBridgeRunPage{}, err
		}
		runs = append(runs, run)
	}
	if err := tx.Commit(); err != nil {
		return application.AgentBridgeRunPage{}, err
	}
	return application.AgentBridgeRunPage{ProjectID: projectID, Runs: runs}, nil
}

func (repository *SQLiteProjects) ListAgentBridgeTurns(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	before domain.Cursor,
	limit uint32,
) (application.AgentBridgeTurnPage, error) {
	if projectID.IsZero() || runID.IsZero() || limit == 0 || limit > application.MaximumAgentBridgeTurnPage {
		return application.AgentBridgeTurnPage{}, application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.AgentBridgeTurnPage{}, err
	}
	defer tx.Rollback()
	if _, _, _, err := loadAgentBridgeTx(ctx, tx, projectID, runID); err != nil {
		return application.AgentBridgeTurnPage{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT turn.id, turn.generation, turn.status, turn.started_at, turn.ended_at, bridge.sequence_id
FROM agent_turns AS turn
JOIN agent_bridge_turns AS bridge ON bridge.turn_id = turn.id
WHERE turn.project_id = ? AND turn.run_id = ? AND (? = 0 OR turn.generation < ?)
ORDER BY turn.generation DESC
LIMIT ?`, projectID.String(), runID.String(), before.Value(), before.Value(), limit+1)
	if err != nil {
		return application.AgentBridgeTurnPage{}, err
	}
	turns := make([]application.AgentBridgeTurn, 0, limit+1)
	for rows.Next() {
		turn, scanErr := scanAgentBridgeTurn(rows)
		if scanErr != nil {
			rows.Close()
			return application.AgentBridgeTurnPage{}, scanErr
		}
		turns = append(turns, turn)
	}
	if err := rows.Close(); err != nil {
		return application.AgentBridgeTurnPage{}, err
	}
	if err := rows.Err(); err != nil {
		return application.AgentBridgeTurnPage{}, err
	}
	var next *domain.Cursor
	if len(turns) > int(limit) {
		turns = turns[:limit]
		cursor, cursorErr := domain.NewCursor(turns[len(turns)-1].Generation.Value())
		if cursorErr != nil {
			return application.AgentBridgeTurnPage{}, cursorErr
		}
		next = &cursor
	}
	if err := tx.Commit(); err != nil {
		return application.AgentBridgeTurnPage{}, err
	}
	return application.AgentBridgeTurnPage{
		ProjectID: projectID, RunID: runID, Turns: turns, NextBefore: next,
	}, nil
}

type agentBridgeTurnScanner interface{ Scan(...any) error }

func scanAgentBridgeTurn(scanner agentBridgeTurnScanner) (application.AgentBridgeTurn, error) {
	var id, status, startedAt string
	var generation uint64
	var endedAt, sequenceID sql.NullString
	if err := scanner.Scan(&id, &generation, &status, &startedAt, &endedAt, &sequenceID); err != nil {
		return application.AgentBridgeTurn{}, err
	}
	parsedID, err := domain.ParseTurnID(id)
	if err != nil {
		return application.AgentBridgeTurn{}, err
	}
	parsedGeneration, err := domain.NewRevision(generation)
	if err != nil {
		return application.AgentBridgeTurn{}, err
	}
	started, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return application.AgentBridgeTurn{}, err
	}
	turn := application.AgentBridgeTurn{
		ID: parsedID, Generation: parsedGeneration, Status: application.AgentTurnStatus(status), StartedAt: started,
	}
	if endedAt.Valid {
		ended, parseErr := time.Parse(time.RFC3339Nano, endedAt.String)
		if parseErr != nil {
			return application.AgentBridgeTurn{}, parseErr
		}
		turn.EndedAt = &ended
	}
	if sequenceID.Valid {
		parsed, parseErr := domain.ParseSequenceID(sequenceID.String)
		if parseErr != nil {
			return application.AgentBridgeTurn{}, parseErr
		}
		turn.SequenceID = &parsed
	}
	return turn, nil
}

const agentBridgeSelect = `
SELECT run.id, run.project_id, run.intent, run.actor_id, run.authorization_state,
       run.status, run.waiting_reason, run.initiator_id, run.created_at, run.updated_at, run.completed_at,
       turn.id, turn.generation, turn.status, turn.started_at, turn.ended_at, bridge_turn.sequence_id,
       COALESCE(head.cursor, 0)
FROM agent_runs AS run
JOIN agent_bridge_runs AS bridge ON bridge.run_id = run.id
JOIN agent_turns AS turn ON turn.id = run.current_turn_id
JOIN agent_bridge_turns AS bridge_turn ON bridge_turn.turn_id = turn.id
LEFT JOIN activity_heads AS head ON head.scope_kind = 'project' AND head.scope_id = run.project_id `

func loadAgentBridgeTx(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	runID domain.RunID,
) (application.AgentBridgeRun, string, string, error) {
	var runValue, projectValue, intent, authorizationState, status, initiatorID string
	var actorID, waitingReason, completedAt, turnEndedAt, sequenceID sql.NullString
	var createdAt, updatedAt, turnID, turnStatus, turnStartedAt string
	var generation, cursor uint64
	err := tx.QueryRowContext(ctx, agentBridgeSelect+` WHERE run.id = ? AND run.project_id = ?`,
		runID.String(), projectID.String()).Scan(
		&runValue, &projectValue, &intent, &actorID, &authorizationState, &status, &waitingReason,
		&initiatorID, &createdAt, &updatedAt, &completedAt, &turnID, &generation, &turnStatus,
		&turnStartedAt, &turnEndedAt, &sequenceID, &cursor,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.AgentBridgeRun{}, "", "", application.ErrAgentBridgeNotFound
	}
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	parsedRun, err := domain.ParseRunID(runValue)
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	parsedProject, err := domain.ParseProjectID(projectValue)
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	parsedTurn, err := domain.ParseTurnID(turnID)
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	parsedGeneration, err := domain.NewRevision(generation)
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	parsedCursor, err := domain.NewCursor(cursor)
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	run := application.AgentBridgeRun{
		ID: parsedRun, ProjectID: parsedProject, Intent: intent, Status: application.AgentRunStatus(status),
		WaitingReason: waitingReason.String, ActivityCursor: parsedCursor,
		CurrentTurn: application.AgentBridgeTurn{ID: parsedTurn, Generation: parsedGeneration, Status: application.AgentTurnStatus(turnStatus)},
	}
	if actorID.Valid {
		agentID, parseErr := domain.ParseAgentID(actorID.String)
		if parseErr != nil {
			return application.AgentBridgeRun{}, "", "", parseErr
		}
		run.AgentID = &agentID
	}
	if sequenceID.Valid {
		parsed, parseErr := domain.ParseSequenceID(sequenceID.String)
		if parseErr != nil {
			return application.AgentBridgeRun{}, "", "", parseErr
		}
		run.CurrentTurn.SequenceID = &parsed
	}
	run.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	run.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	run.CurrentTurn.StartedAt, err = time.Parse(time.RFC3339Nano, turnStartedAt)
	if err != nil {
		return application.AgentBridgeRun{}, "", "", err
	}
	if completedAt.Valid {
		instant, parseErr := time.Parse(time.RFC3339Nano, completedAt.String)
		if parseErr != nil {
			return application.AgentBridgeRun{}, "", "", parseErr
		}
		run.CompletedAt = &instant
	}
	if turnEndedAt.Valid {
		instant, parseErr := time.Parse(time.RFC3339Nano, turnEndedAt.String)
		if parseErr != nil {
			return application.AgentBridgeRun{}, "", "", parseErr
		}
		run.CurrentTurn.EndedAt = &instant
	}
	return run, authorizationState, initiatorID, nil
}

func loadAgentBridgeReplay(
	ctx context.Context,
	tx *sql.Tx,
	creator domain.ActorRef,
	requestID domain.RequestID,
	command string,
	digest domain.Digest,
	projectID domain.ProjectID,
) (application.AgentBridgeResult, bool, error) {
	var storedCommand, storedDigest, storedProject, runValue, messageValue string
	err := tx.QueryRowContext(ctx, `
SELECT command, input_digest, project_id, run_id, COALESCE(message_id, '')
FROM agent_bridge_requests WHERE creator_id = ? AND request_id = ?`,
		creator.IDString(), requestID.String()).Scan(
		&storedCommand, &storedDigest, &storedProject, &runValue, &messageValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.AgentBridgeResult{}, false, nil
	}
	if err != nil {
		return application.AgentBridgeResult{}, false, err
	}
	if storedCommand != command || storedDigest != digest.String() || storedProject != projectID.String() {
		return application.AgentBridgeResult{}, false, application.ErrAgentBridgeRequestReused
	}
	runID, err := domain.ParseRunID(runValue)
	if err != nil {
		return application.AgentBridgeResult{}, false, err
	}
	run, _, _, err := loadAgentBridgeTx(ctx, tx, projectID, runID)
	if err != nil {
		return application.AgentBridgeResult{}, false, err
	}
	result := application.AgentBridgeResult{Run: run}
	if messageValue != "" {
		messageID, parseErr := domain.ParseConversationMessageID(messageValue)
		if parseErr != nil {
			return application.AgentBridgeResult{}, false, parseErr
		}
		message, loadErr := loadAgentConversationMessage(ctx, tx, messageID)
		if loadErr != nil {
			return application.AgentBridgeResult{}, false, loadErr
		}
		result.Message = &message
	}
	return result, true, nil
}

func activeProjectRevision(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID) (domain.Revision, error) {
	var revision uint64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT revision, status FROM projects WHERE id = ?`, projectID.String()).Scan(&revision, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, application.ErrProjectNotFound
		}
		return 0, err
	}
	if status != string(domain.ProjectActive) {
		return 0, application.ErrProjectNotActive
	}
	return domain.NewRevision(revision)
}

func validateBridgeSequence(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, sequenceID *domain.SequenceID) error {
	if sequenceID == nil {
		return nil
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sequences WHERE id = ? AND project_id = ?`,
		sequenceID.String(), projectID.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrAgentBridgeInvalid
		}
		return err
	}
	return nil
}

func insertAgentBridgeTurn(ctx context.Context, tx *sql.Tx, turnID domain.TurnID, sequenceID *domain.SequenceID, createdAt string) error {
	var sequence any
	if sequenceID != nil {
		sequence = sequenceID.String()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_bridge_turns (turn_id, sequence_id, created_at) VALUES (?, ?, ?)`,
		turnID.String(), sequence, createdAt)
	return err
}

func insertAgentBridgeRequest(
	ctx context.Context,
	tx *sql.Tx,
	creator domain.ActorRef,
	requestID domain.RequestID,
	command string,
	digest domain.Digest,
	canonical []byte,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	messageID *domain.ConversationMessageID,
	eventID domain.ActivityEventID,
	createdAt string,
) error {
	var message any
	if messageID != nil {
		message = messageID.String()
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO agent_bridge_requests (
  creator_id, request_id, command, input_digest, input_json, project_id, run_id, turn_id,
  message_id, activity_event_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		creator.IDString(), requestID.String(), command, digest.String(), string(canonical), projectID.String(),
		runID.String(), turnID.String(), message, eventID.String(), createdAt,
	)
	return err
}

func validateBeginAgentBridge(record application.BeginAgentBridgeRecord) error {
	if record.RunID.IsZero() || record.TurnID.IsZero() || record.ProjectID.IsZero() || record.MessageID.IsZero() ||
		record.ActivityEventID.IsZero() || record.Creator.Validate() != nil || record.Creator.Kind != domain.ActorCreator ||
		len(record.Intent) == 0 || len([]byte(record.Intent)) > application.MaximumCreatorMessageBytes ||
		len(record.RequestCanonical) == 0 || application.ValidateAgentContextAttachments(record.Attachments) != nil {
		return application.ErrAgentBridgeInvalid
	}
	return nil
}

func validateContinueAgentBridge(record application.ContinueAgentBridgeRecord) error {
	if record.ProjectID.IsZero() || record.RunID.IsZero() || record.TurnID.IsZero() || record.MessageID.IsZero() ||
		record.ExpectedGeneration.Value() < 1 || record.ActivityEventID.IsZero() || record.Creator.Validate() != nil ||
		record.Creator.Kind != domain.ActorCreator || len(record.Message) == 0 ||
		len([]byte(record.Message)) > application.MaximumCreatorMessageBytes || len(record.RequestCanonical) == 0 ||
		application.ValidateAgentContextAttachments(record.Attachments) != nil {
		return application.ErrAgentBridgeInvalid
	}
	return nil
}

func validateTransitionAgentBridge(record application.TransitionAgentBridgeRecord) error {
	if record.Transition != application.AgentBridgeInterrupt && record.Transition != application.AgentBridgeCancel {
		return application.ErrAgentBridgeInvalid
	}
	if record.ProjectID.IsZero() || record.RunID.IsZero() || record.ExpectedGeneration.Value() < 1 ||
		record.ExpectedTurnID.IsZero() || record.ActivityEventID.IsZero() || record.Creator.Validate() != nil || record.Creator.Kind != domain.ActorCreator ||
		len(record.RequestCanonical) == 0 {
		return application.ErrAgentBridgeInvalid
	}
	return nil
}

func terminalAgentRun(status application.AgentRunStatus) bool {
	return status == application.AgentRunCompleted || status == application.AgentRunFailed || status == application.AgentRunCancelled
}
