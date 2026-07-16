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

func (repository *SQLiteProjects) ListAgentConversation(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	after domain.Cursor,
	limit uint32,
) (application.AgentConversationPage, error) {
	if projectID.IsZero() || runID.IsZero() || limit == 0 || limit > application.MaximumConversationPage {
		return application.AgentConversationPage{}, application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.AgentConversationPage{}, err
	}
	defer tx.Rollback()
	if _, _, _, err := loadAgentBridgeTx(ctx, tx, projectID, runID); err != nil {
		return application.AgentConversationPage{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, project_id, run_id, turn_id, ordinal, role, text, created_at
FROM agent_conversation_messages
WHERE project_id = ? AND run_id = ? AND ordinal > ?
ORDER BY ordinal ASC LIMIT ?`, projectID.String(), runID.String(), after.Value(), limit+1)
	if err != nil {
		return application.AgentConversationPage{}, err
	}
	defer rows.Close()
	messages := make([]application.AgentConversationMessage, 0, limit)
	for rows.Next() {
		message, scanErr := scanAgentConversationMessage(rows)
		if scanErr != nil {
			return application.AgentConversationPage{}, scanErr
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return application.AgentConversationPage{}, err
	}
	if err := rows.Close(); err != nil {
		return application.AgentConversationPage{}, err
	}
	for index := range messages {
		if messages[index].Role != application.AgentConversationCreator {
			continue
		}
		messages[index].Attachments, err = loadAgentContextAttachments(ctx, tx, messages[index].TurnID)
		if err != nil {
			return application.AgentConversationPage{}, err
		}
	}
	var next *domain.Cursor
	if len(messages) > int(limit) {
		messages = messages[:limit]
		value := messages[len(messages)-1].Ordinal
		next = &value
	}
	if err := tx.Commit(); err != nil {
		return application.AgentConversationPage{}, err
	}
	return application.AgentConversationPage{ProjectID: projectID, RunID: runID, Messages: messages, NextAfter: next}, nil
}

func (repository *SQLiteProjects) AppendAgentBridgeMessage(
	ctx context.Context,
	message application.AgentConversationMessage,
) (application.AgentConversationMessage, error) {
	if message.ID.IsZero() || message.ProjectID.IsZero() || message.RunID.IsZero() || message.TurnID.IsZero() ||
		(message.Role != application.AgentConversationAgent && message.Role != application.AgentConversationNotice) ||
		len(message.Text) == 0 ||
		len([]byte(message.Text)) > application.MaximumAgentTurnTextBytes || message.CreatedAt.IsZero() {
		return application.AgentConversationMessage{}, application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	defer tx.Rollback()
	run, _, _, err := loadAgentBridgeTx(ctx, tx, message.ProjectID, message.RunID)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	if run.CurrentTurn.ID != message.TurnID {
		return application.AgentConversationMessage{}, application.ErrAgentBridgeStaleTurn
	}
	if message.Role == application.AgentConversationNotice && message.Text != application.AgentConversationContextRebuilt {
		return application.AgentConversationMessage{}, application.ErrAgentBridgeInvalid
	}
	activeTurn := (run.CurrentTurn.Status == application.AgentTurnStarting || run.CurrentTurn.Status == application.AgentTurnActive) &&
		!terminalAgentRun(run.Status)
	completedPresentation := message.Role == application.AgentConversationAgent &&
		run.Status == application.AgentRunCompleted && run.CurrentTurn.Status == application.AgentTurnCompleted
	if !activeTurn && !completedPresentation {
		return application.AgentConversationMessage{}, application.ErrAgentBridgeStaleTurn
	}
	var total uint64
	if message.Role == application.AgentConversationAgent {
		if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(length(CAST(text AS BLOB))), 0)
FROM agent_conversation_messages WHERE turn_id = ? AND role = 'agent'`, message.TurnID.String()).Scan(&total); err != nil {
			return application.AgentConversationMessage{}, err
		}
		if total+uint64(len([]byte(message.Text))) > application.MaximumAgentTurnTextBytes {
			return application.AgentConversationMessage{}, application.ErrAgentBridgeInvalid
		}
	}
	message.Ordinal, err = nextConversationOrdinal(ctx, tx, message.RunID)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	if err := insertAgentConversationMessage(ctx, tx, message); err != nil {
		return application.AgentConversationMessage{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AgentConversationMessage{}, err
	}
	return message, nil
}

func (repository *SQLiteProjects) ActivateAgentBridgeTurn(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	agentVersion string,
	promptVersion string,
	at time.Time,
) error {
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() || agentVersion == "" || len(agentVersion) > 128 ||
		promptVersion == "" || len(promptVersion) > 128 || at.IsZero() {
		return application.ErrAgentBridgeInvalid
	}
	result, err := repository.db.ExecContext(ctx, `
UPDATE agent_turns
SET status = 'active', agent_version = ?, prompt_version = ?
WHERE id = ? AND run_id = ? AND project_id = ? AND status = 'starting'
  AND EXISTS (SELECT 1 FROM agent_bridge_turns WHERE turn_id = agent_turns.id)`,
		agentVersion, promptVersion, turnID.String(), runID.String(), projectID.String())
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return application.ErrAgentBridgeStaleTurn
	}
	return nil
}

func (repository *SQLiteProjects) SetAgentBridgeNativeSession(
	ctx context.Context,
	runID domain.RunID,
	turnID domain.TurnID,
	sessionID string,
) error {
	if runID.IsZero() || turnID.IsZero() || sessionID == "" || len(sessionID) > 256 {
		return application.ErrAgentBridgeInvalid
	}
	result, err := repository.db.ExecContext(ctx, `
UPDATE agent_turns SET native_session_id = ?
WHERE id = ? AND run_id = ? AND status IN ('starting', 'active')
  AND EXISTS (SELECT 1 FROM agent_bridge_turns WHERE turn_id = agent_turns.id)`,
		sessionID, turnID.String(), runID.String())
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return application.ErrAgentBridgeStaleTurn
	}
	return nil
}

func (repository *SQLiteProjects) PrepareAgentBridgeInvocation(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
) (application.AgentBridgeInvocation, error) {
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() {
		return application.AgentBridgeInvocation{}, application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	defer tx.Rollback()
	run, _, _, err := loadAgentBridgeTx(ctx, tx, projectID, runID)
	if err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	if run.CurrentTurn.ID != turnID || run.CurrentTurn.Status != application.AgentTurnStarting {
		return application.AgentBridgeInvocation{}, application.ErrAgentBridgeStaleTurn
	}
	var totalMessages uint64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM agent_conversation_messages
WHERE project_id = ? AND run_id = ? AND role IN ('creator', 'agent')`,
		projectID.String(), runID.String()).Scan(&totalMessages); err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	first, err := firstAgentRecoveryMessage(ctx, tx, projectID, runID)
	if err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, project_id, run_id, turn_id, ordinal, role, text, created_at
FROM agent_conversation_messages
WHERE project_id = ? AND run_id = ? AND role IN ('creator', 'agent')
ORDER BY ordinal DESC LIMIT ?`, projectID.String(), runID.String(), application.MaximumAgentRecoveryMessages-1)
	if err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	defer rows.Close()
	newest := make([]application.AgentConversationMessage, 0, application.MaximumAgentRecoveryMessages)
	for rows.Next() {
		message, scanErr := scanAgentConversationMessage(rows)
		if scanErr != nil {
			return application.AgentBridgeInvocation{}, scanErr
		}
		newest = append(newest, message)
	}
	if err := rows.Err(); err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	if err := rows.Close(); err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	for index := range newest {
		if newest[index].Role != application.AgentConversationCreator {
			continue
		}
		newest[index].Attachments, err = loadAgentContextAttachments(ctx, tx, newest[index].TurnID)
		if err != nil {
			return application.AgentBridgeInvocation{}, err
		}
	}
	if len(newest) == 0 || newest[0].TurnID != turnID || newest[0].Role != application.AgentConversationCreator {
		return application.AgentBridgeInvocation{}, application.ErrAgentBridgeStaleTurn
	}
	messages := boundedAgentRecoveryMessages(first, newest)
	omitted := totalMessages - uint64(len(messages))
	receipts, omittedReceipts, err := loadAgentRecoveryReceipts(ctx, tx, projectID, runID)
	if err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	var nativeSession sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT native_session_id
FROM agent_turns
WHERE run_id = ? AND generation < ? AND native_session_id IS NOT NULL
ORDER BY generation DESC LIMIT 1`, runID.String(), run.CurrentTurn.Generation.Value()).Scan(&nativeSession)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return application.AgentBridgeInvocation{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AgentBridgeInvocation{}, err
	}
	return application.AgentBridgeInvocation{
		ProjectID: projectID, RunID: runID, TurnID: turnID, SequenceID: run.CurrentTurn.SequenceID,
		Messages: messages, OmittedMessageCount: omitted,
		Receipts: receipts, OmittedReceiptCount: omittedReceipts, NativeSessionID: nativeSession.String,
	}, nil
}

func loadAgentRecoveryReceipts(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	runID domain.RunID,
) ([]application.CommandReceipt, uint64, error) {
	var total uint64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM agent_command_receipts WHERE project_id = ? AND run_id = ?`,
		projectID.String(), runID.String()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT receipt.id, receipt.project_id, receipt.run_id, receipt.turn_id, receipt.ordinal,
       receipt.class, receipt.command, receipt.command_fingerprint, receipt.input_digest,
       receipt.request_id, receipt.result_digest, receipt.status, receipt.project_revision,
       receipt.activity_cursor, receipt.created_at
FROM agent_command_receipts AS receipt
JOIN agent_turns AS turn ON turn.id = receipt.turn_id
WHERE receipt.project_id = ? AND receipt.run_id = ?
ORDER BY turn.generation DESC, receipt.ordinal DESC
LIMIT ?`, projectID.String(), runID.String(), application.MaximumAgentRecoveryReceipts)
	if err != nil {
		return nil, 0, err
	}
	newest := make([]application.CommandReceipt, 0, application.MaximumAgentRecoveryReceipts)
	for rows.Next() {
		receipt, scanErr := scanCommandReceipt(rows)
		if scanErr != nil {
			rows.Close()
			return nil, 0, scanErr
		}
		newest = append(newest, receipt)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for index := range newest {
		newest[index].ResultRefs, err = loadCommandReceiptRefs(ctx, tx, newest[index].ID)
		if err != nil {
			return nil, 0, err
		}
	}
	selectedNewest := make([]application.CommandReceipt, 0, len(newest))
	bytes := 0
	for _, receipt := range newest {
		encoded, encodeErr := json.Marshal(receipt)
		if encodeErr != nil {
			return nil, 0, encodeErr
		}
		if bytes+len(encoded) > application.MaximumAgentRecoveryReceiptBytes {
			break
		}
		bytes += len(encoded)
		selectedNewest = append(selectedNewest, receipt)
	}
	receipts := make([]application.CommandReceipt, len(selectedNewest))
	for index := range selectedNewest {
		receipts[len(selectedNewest)-1-index] = selectedNewest[index]
	}
	return receipts, total - uint64(len(receipts)), nil
}

func firstAgentRecoveryMessage(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	runID domain.RunID,
) (application.AgentConversationMessage, error) {
	row := tx.QueryRowContext(ctx, `
SELECT id, project_id, run_id, turn_id, ordinal, role, text, created_at
FROM agent_conversation_messages
WHERE project_id = ? AND run_id = ? AND role = 'creator'
ORDER BY ordinal ASC LIMIT 1`, projectID.String(), runID.String())
	message, err := scanAgentConversationMessage(row)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	message.Attachments, err = loadAgentContextAttachments(ctx, tx, message.TurnID)
	return message, err
}

func boundedAgentRecoveryMessages(
	first application.AgentConversationMessage,
	newest []application.AgentConversationMessage,
) []application.AgentConversationMessage {
	selectedNewest := make([]application.AgentConversationMessage, 0, len(newest))
	total := agentRecoveryMessageSize(first)
	for _, message := range newest {
		if message.ID == first.ID {
			continue
		}
		size := agentRecoveryMessageSize(message)
		if total+size > application.MaximumAgentRecoveryBytes {
			break
		}
		total += size
		selectedNewest = append(selectedNewest, message)
	}
	messages := make([]application.AgentConversationMessage, 0, len(selectedNewest)+1)
	messages = append(messages, first)
	for index := len(selectedNewest) - 1; index >= 0; index-- {
		messages = append(messages, selectedNewest[index])
	}
	return messages
}

func agentRecoveryMessageSize(message application.AgentConversationMessage) int {
	encoded, err := json.Marshal(message)
	if err != nil {
		return application.MaximumAgentRecoveryBytes + 1
	}
	return len(encoded)
}

func (repository *SQLiteProjects) FinishAgentBridgeTurn(
	ctx context.Context,
	record application.AgentBridgeRuntimeRecord,
) error {
	if record.ProjectID.IsZero() || record.RunID.IsZero() || record.TurnID.IsZero() || record.OccurredAt.IsZero() ||
		(record.Outcome != application.AgentBridgeRuntimeCompleted &&
			record.Outcome != application.AgentBridgeRuntimeDetached &&
			record.Outcome != application.AgentBridgeRuntimeFailed &&
			record.Outcome != application.AgentBridgeRuntimeResourceLimit) {
		return application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	run, _, _, err := loadAgentBridgeTx(ctx, tx, record.ProjectID, record.RunID)
	if err != nil {
		return err
	}
	if run.CurrentTurn.ID != record.TurnID {
		return application.ErrAgentBridgeStaleTurn
	}
	if terminalAgentRun(run.Status) {
		return tx.Commit()
	}
	if run.CurrentTurn.Status == application.AgentTurnCancelled || run.CurrentTurn.Status == application.AgentTurnSuperseded {
		return tx.Commit()
	}
	if run.CurrentTurn.Status != application.AgentTurnStarting && run.CurrentTurn.Status != application.AgentTurnActive {
		return application.ErrAgentBridgeStaleTurn
	}
	turnStatus, waitingReason := application.AgentTurnCompleted, "awaiting-creator"
	switch record.Outcome {
	case application.AgentBridgeRuntimeDetached:
		turnStatus, waitingReason = application.AgentTurnDetached, "agent-detached"
	case application.AgentBridgeRuntimeFailed:
		turnStatus, waitingReason = application.AgentTurnFailed, "adapter-failed"
	case application.AgentBridgeRuntimeResourceLimit:
		turnStatus, waitingReason = application.AgentTurnFailed, "resource-limit"
	}
	now := formatInstant(record.OccurredAt)
	if _, err := tx.ExecContext(ctx, `UPDATE agent_turns SET status = ?, ended_at = ? WHERE id = ?`,
		string(turnStatus), now, record.TurnID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs SET status = 'paused', waiting_reason = ?, updated_at = ? WHERE id = ?`,
		waitingReason, now, record.RunID.String()); err != nil {
		return err
	}
	return tx.Commit()
}

func (repository *SQLiteProjects) RecoverAgentBridgeTurns(ctx context.Context, at time.Time) error {
	if at.IsZero() {
		return application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatInstant(at)
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_turns
SET status = 'detached', ended_at = ?
WHERE status IN ('starting', 'active')
  AND EXISTS (SELECT 1 FROM agent_bridge_turns WHERE turn_id = agent_turns.id)`, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs
SET status = 'paused', waiting_reason = 'agent-detached', updated_at = ?
WHERE status NOT IN ('completed', 'failed', 'cancelled')
  AND EXISTS (SELECT 1 FROM agent_bridge_runs WHERE run_id = agent_runs.id)
  AND EXISTS (
    SELECT 1 FROM agent_turns
    WHERE agent_turns.id = agent_runs.current_turn_id AND agent_turns.status = 'detached'
  )`, now); err != nil {
		return err
	}
	return tx.Commit()
}

func insertAgentConversationMessage(
	ctx context.Context,
	tx *sql.Tx,
	message application.AgentConversationMessage,
) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO agent_conversation_messages (
  id, project_id, run_id, turn_id, ordinal, role, text, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID.String(), message.ProjectID.String(), message.RunID.String(), message.TurnID.String(),
		message.Ordinal.Value(), string(message.Role), message.Text, formatInstant(message.CreatedAt),
	)
	return err
}

func nextConversationOrdinal(ctx context.Context, tx *sql.Tx, runID domain.RunID) (domain.Cursor, error) {
	var current uint64
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(ordinal), 0) FROM agent_conversation_messages WHERE run_id = ?`, runID.String()).Scan(&current); err != nil {
		return 0, err
	}
	return domain.NewCursor(current + 1)
}

func loadAgentConversationMessage(
	ctx context.Context,
	tx *sql.Tx,
	id domain.ConversationMessageID,
) (application.AgentConversationMessage, error) {
	row := tx.QueryRowContext(ctx, `
SELECT id, project_id, run_id, turn_id, ordinal, role, text, created_at
FROM agent_conversation_messages WHERE id = ?`, id.String())
	message, err := scanAgentConversationMessage(row)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	message.Attachments, err = loadAgentContextAttachments(ctx, tx, message.TurnID)
	return message, err
}

type agentConversationScanner interface {
	Scan(...any) error
}

func scanAgentConversationMessage(scanner agentConversationScanner) (application.AgentConversationMessage, error) {
	var id, projectID, runID, turnID, role, text, createdAt string
	var ordinal uint64
	if err := scanner.Scan(&id, &projectID, &runID, &turnID, &ordinal, &role, &text, &createdAt); err != nil {
		return application.AgentConversationMessage{}, err
	}
	parsedID, err := domain.ParseConversationMessageID(id)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	parsedProject, err := domain.ParseProjectID(projectID)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	parsedRun, err := domain.ParseRunID(runID)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	parsedTurn, err := domain.ParseTurnID(turnID)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	parsedOrdinal, err := domain.NewCursor(ordinal)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	instant, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return application.AgentConversationMessage{}, err
	}
	message := application.AgentConversationMessage{
		ID: parsedID, ProjectID: parsedProject, RunID: parsedRun, TurnID: parsedTurn,
		Ordinal: parsedOrdinal, Role: application.AgentConversationRole(role), Text: text,
		Attachments: []application.AgentContextAttachment{}, CreatedAt: instant,
	}
	if message.Role != application.AgentConversationCreator && message.Role != application.AgentConversationAgent &&
		message.Role != application.AgentConversationNotice {
		return application.AgentConversationMessage{}, fmt.Errorf("invalid Agent conversation role")
	}
	return message, nil
}
