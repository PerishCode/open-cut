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

func (repository *SQLiteProjects) RecordCommandReceipt(
	ctx context.Context,
	record application.RecordCommandReceipt,
) (application.CommandReceipt, error) {
	if err := validateCommandReceiptRecord(record); err != nil {
		return application.CommandReceipt{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.CommandReceipt{}, err
	}
	defer tx.Rollback()
	receipt, exists, err := loadCommandReceiptByID(ctx, tx, record.Receipt.ID)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	if exists {
		if !sameCommandReceiptInvocation(receipt, record.Receipt) {
			return application.CommandReceipt{}, application.ErrCommandReceiptInvalid
		}
		if err := tx.Commit(); err != nil {
			return application.CommandReceipt{}, err
		}
		return receipt, nil
	}
	if record.Receipt.RequestID != nil {
		receipt, exists, err = loadCommandReceiptByRequest(ctx, tx, record.Actor, *record.Receipt.RequestID)
		if err != nil {
			return application.CommandReceipt{}, err
		}
		if exists {
			if !sameLogicalCommandReceipt(receipt, record.Receipt) {
				return application.CommandReceipt{}, application.ErrCommandReceiptInvalid
			}
			if err := tx.Commit(); err != nil {
				return application.CommandReceipt{}, err
			}
			return receipt, nil
		}
	}
	if err := validateCommandReceiptOwner(ctx, tx, record); err != nil {
		return application.CommandReceipt{}, err
	}
	receipt = record.Receipt
	receipt.Ordinal, err = nextCommandReceiptOrdinal(ctx, tx, receipt.TurnID)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	if err := insertCommandReceipt(ctx, tx, receipt, record.Actor); err != nil {
		return application.CommandReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CommandReceipt{}, err
	}
	return receipt, nil
}

func (repository *SQLiteProjects) ListCommandReceipts(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	after domain.Cursor,
	limit uint32,
) (application.TurnReceiptPage, error) {
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() || limit == 0 || limit > application.MaximumCommandReceiptPage {
		return application.TurnReceiptPage{}, application.ErrCommandReceiptInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.TurnReceiptPage{}, err
	}
	defer tx.Rollback()
	if _, _, _, err := loadAgentBridgeTx(ctx, tx, projectID, runID); err != nil {
		return application.TurnReceiptPage{}, err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `
SELECT 1 FROM agent_turns WHERE id = ? AND run_id = ? AND project_id = ?`,
		turnID.String(), runID.String(), projectID.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.TurnReceiptPage{}, application.ErrCommandReceiptNotFound
		}
		return application.TurnReceiptPage{}, err
	}
	rows, err := tx.QueryContext(ctx, commandReceiptSelect+`
WHERE project_id = ? AND run_id = ? AND turn_id = ? AND ordinal > ?
ORDER BY ordinal LIMIT ?`, projectID.String(), runID.String(), turnID.String(), after.Value(), limit+1)
	if err != nil {
		return application.TurnReceiptPage{}, err
	}
	receipts := make([]application.CommandReceipt, 0, limit+1)
	for rows.Next() {
		receipt, scanErr := scanCommandReceipt(rows)
		if scanErr != nil {
			rows.Close()
			return application.TurnReceiptPage{}, scanErr
		}
		receipts = append(receipts, receipt)
	}
	if err := rows.Close(); err != nil {
		return application.TurnReceiptPage{}, err
	}
	if err := rows.Err(); err != nil {
		return application.TurnReceiptPage{}, err
	}
	for index := range receipts {
		receipts[index].ResultRefs, err = loadCommandReceiptRefs(ctx, tx, receipts[index].ID)
		if err != nil {
			return application.TurnReceiptPage{}, err
		}
	}
	var next *domain.Cursor
	if len(receipts) > int(limit) {
		receipts = receipts[:limit]
		cursor := receipts[len(receipts)-1].Ordinal
		next = &cursor
	}
	if err := tx.Commit(); err != nil {
		return application.TurnReceiptPage{}, err
	}
	return application.TurnReceiptPage{
		ProjectID: projectID, RunID: runID, TurnID: turnID, Receipts: receipts, NextAfter: next,
	}, nil
}

func (repository *SQLiteProjects) FindCommandReceipt(
	ctx context.Context,
	actor domain.ActorRef,
	requestID domain.RequestID,
) (application.CommandReceipt, bool, error) {
	if actor.Validate() != nil || actor.Kind != domain.ActorAgent {
		return application.CommandReceipt{}, false, application.ErrCommandReceiptInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.CommandReceipt{}, false, err
	}
	defer tx.Rollback()
	receipt, exists, err := loadCommandReceiptByRequest(ctx, tx, actor, requestID)
	if err != nil {
		return application.CommandReceipt{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.CommandReceipt{}, false, err
	}
	return receipt, exists, nil
}

func insertCommandReceipt(
	ctx context.Context,
	tx *sql.Tx,
	receipt application.CommandReceipt,
	actor domain.ActorRef,
) error {
	var requestID, projectRevision, activityCursor any
	if receipt.RequestID != nil {
		requestID = receipt.RequestID.String()
	}
	if receipt.ProjectRevision != nil {
		projectRevision = int64(receipt.ProjectRevision.Value())
	}
	if receipt.ActivityCursor != nil {
		activityCursor = int64(receipt.ActivityCursor.Value())
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_command_receipts (
  id, project_id, run_id, turn_id, ordinal, actor_id, class, command,
  command_fingerprint, input_digest, request_id, result_digest, status,
  project_revision, activity_cursor, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		receipt.ID.String(), receipt.ProjectID.String(), receipt.RunID.String(), receipt.TurnID.String(),
		receipt.Ordinal.Value(), actor.IDString(), string(receipt.Class), receipt.Command,
		receipt.CommandFingerprint.String(), receipt.InputDigest.String(), requestID, receipt.ResultDigest.String(),
		string(receipt.Status), projectRevision, activityCursor, formatInstant(receipt.CreatedAt)); err != nil {
		return err
	}
	for ordinal, ref := range receipt.ResultRefs {
		var revision any
		if ref.Revision != nil {
			revision = int64(ref.Revision.Value())
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_command_receipt_refs (receipt_id, ordinal, kind, entity_id, entity_revision)
VALUES (?, ?, ?, ?, ?)`, receipt.ID.String(), ordinal, ref.Kind, ref.ID, revision); err != nil {
			return err
		}
	}
	return nil
}

func validateCommandReceiptOwner(
	ctx context.Context,
	tx *sql.Tx,
	record application.RecordCommandReceipt,
) error {
	var actorID, runProject, turnRun, turnProject string
	err := tx.QueryRowContext(ctx, `
SELECT run.actor_id, run.project_id, turn.run_id, turn.project_id
FROM agent_runs AS run
JOIN agent_turns AS turn ON turn.id = ?
WHERE run.id = ?`, record.Receipt.TurnID.String(), record.Receipt.RunID.String()).Scan(
		&actorID, &runProject, &turnRun, &turnProject)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrCommandReceiptNotFound
	}
	if err != nil {
		return err
	}
	if actorID != record.Actor.IDString() || runProject != record.Receipt.ProjectID.String() ||
		turnRun != record.Receipt.RunID.String() || turnProject != record.Receipt.ProjectID.String() {
		return application.ErrCommandReceiptInvalid
	}
	return nil
}

func validateCommandReceiptRecord(record application.RecordCommandReceipt) error {
	receipt := record.Receipt
	if receipt.Schema != application.CommandReceiptSchema || receipt.ID.IsZero() || receipt.ProjectID.IsZero() ||
		receipt.RunID.IsZero() || receipt.TurnID.IsZero() || receipt.Ordinal.Value() != 0 ||
		(receipt.Class != application.CommandReceiptEvidence && receipt.Class != application.CommandReceiptOutcome) ||
		receipt.Command == "" || len(receipt.Command) > 128 ||
		receipt.CommandFingerprint == "" || receipt.InputDigest == "" || receipt.ResultDigest == "" ||
		receipt.CreatedAt.IsZero() || record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorAgent ||
		len(receipt.ResultRefs) > application.MaximumCommandReceiptRefs || !validStoredReceiptStatus(receipt.Status) {
		return application.ErrCommandReceiptInvalid
	}
	for _, ref := range receipt.ResultRefs {
		if ref.Kind == "" || len(ref.Kind) > 64 || ref.ID == "" || len(ref.ID) > 128 ||
			(ref.Revision != nil && ref.Revision.Value() < 1) {
			return application.ErrCommandReceiptInvalid
		}
	}
	return nil
}

func validStoredReceiptStatus(status application.CommandReceiptStatus) bool {
	switch status {
	case application.CommandReceiptSucceeded, application.CommandReceiptAccepted, application.CommandReceiptWaiting,
		application.CommandReceiptApprovalRequired, application.CommandReceiptConflict,
		application.CommandReceiptNotFound, application.CommandReceiptUnavailable,
		application.CommandReceiptIncompatible, application.CommandReceiptInvalid, application.CommandReceiptFailed:
		return true
	default:
		return false
	}
}

func nextCommandReceiptOrdinal(ctx context.Context, tx *sql.Tx, turnID domain.TurnID) (domain.Cursor, error) {
	var current uint64
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(ordinal), 0) FROM agent_command_receipts WHERE turn_id = ?`, turnID.String()).Scan(&current); err != nil {
		return 0, err
	}
	return domain.NewCursor(current + 1)
}

const commandReceiptSelect = `
SELECT id, project_id, run_id, turn_id, ordinal, class, command, command_fingerprint,
       input_digest, request_id, result_digest, status, project_revision, activity_cursor, created_at
FROM agent_command_receipts `

func loadCommandReceiptByID(
	ctx context.Context,
	tx *sql.Tx,
	id domain.CommandReceiptID,
) (application.CommandReceipt, bool, error) {
	receipt, err := scanCommandReceipt(tx.QueryRowContext(ctx, commandReceiptSelect+` WHERE id = ?`, id.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return application.CommandReceipt{}, false, nil
	}
	if err != nil {
		return application.CommandReceipt{}, false, err
	}
	receipt.ResultRefs, err = loadCommandReceiptRefs(ctx, tx, id)
	return receipt, true, err
}

func loadCommandReceiptByRequest(
	ctx context.Context,
	tx *sql.Tx,
	actor domain.ActorRef,
	requestID domain.RequestID,
) (application.CommandReceipt, bool, error) {
	receipt, err := scanCommandReceipt(tx.QueryRowContext(ctx, commandReceiptSelect+
		` WHERE actor_id = ? AND request_id = ?`, actor.IDString(), requestID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return application.CommandReceipt{}, false, nil
	}
	if err != nil {
		return application.CommandReceipt{}, false, err
	}
	receipt.ResultRefs, err = loadCommandReceiptRefs(ctx, tx, receipt.ID)
	return receipt, true, err
}

type commandReceiptScanner interface{ Scan(...any) error }

func scanCommandReceipt(scanner commandReceiptScanner) (application.CommandReceipt, error) {
	var id, projectID, runID, turnID, class, commandName, fingerprint, inputDigest, resultDigest, status, createdAt string
	var ordinal uint64
	var requestID sql.NullString
	var projectRevision, activityCursor sql.NullInt64
	if err := scanner.Scan(&id, &projectID, &runID, &turnID, &ordinal, &class, &commandName, &fingerprint,
		&inputDigest, &requestID, &resultDigest, &status, &projectRevision, &activityCursor, &createdAt); err != nil {
		return application.CommandReceipt{}, err
	}
	parsedID, err := domain.ParseCommandReceiptID(id)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	parsedProject, err := domain.ParseProjectID(projectID)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	parsedRun, err := domain.ParseRunID(runID)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	parsedTurn, err := domain.ParseTurnID(turnID)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	parsedOrdinal, err := domain.NewCursor(ordinal)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	parsedFingerprint, err := domain.ParseDigest(fingerprint)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	parsedInput, err := domain.ParseDigest(inputDigest)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	parsedResult, err := domain.ParseDigest(resultDigest)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	instant, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return application.CommandReceipt{}, err
	}
	receipt := application.CommandReceipt{
		Schema: application.CommandReceiptSchema, ID: parsedID, ProjectID: parsedProject, RunID: parsedRun,
		TurnID: parsedTurn, Ordinal: parsedOrdinal, Class: application.CommandReceiptClass(class), Command: commandName,
		CommandFingerprint: parsedFingerprint, InputDigest: parsedInput, ResultDigest: parsedResult,
		Status: application.CommandReceiptStatus(status), ResultRefs: []application.CommandReceiptRef{}, CreatedAt: instant.UTC(),
	}
	if requestID.Valid {
		parsed, parseErr := domain.ParseRequestID(requestID.String)
		if parseErr != nil {
			return application.CommandReceipt{}, parseErr
		}
		receipt.RequestID = &parsed
	}
	if projectRevision.Valid {
		parsed, parseErr := domain.NewRevision(uint64(projectRevision.Int64))
		if parseErr != nil {
			return application.CommandReceipt{}, parseErr
		}
		receipt.ProjectRevision = &parsed
	}
	if activityCursor.Valid {
		parsed, parseErr := domain.NewCursor(uint64(activityCursor.Int64))
		if parseErr != nil {
			return application.CommandReceipt{}, parseErr
		}
		receipt.ActivityCursor = &parsed
	}
	return receipt, nil
}

func loadCommandReceiptRefs(
	ctx context.Context,
	tx *sql.Tx,
	receiptID domain.CommandReceiptID,
) ([]application.CommandReceiptRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT kind, entity_id, entity_revision
FROM agent_command_receipt_refs WHERE receipt_id = ? ORDER BY ordinal`, receiptID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := make([]application.CommandReceiptRef, 0)
	for rows.Next() {
		var kind, id string
		var revision sql.NullInt64
		if err := rows.Scan(&kind, &id, &revision); err != nil {
			return nil, err
		}
		ref := application.CommandReceiptRef{Kind: kind, ID: id}
		if revision.Valid {
			parsed, parseErr := domain.NewRevision(uint64(revision.Int64))
			if parseErr != nil {
				return nil, parseErr
			}
			ref.Revision = &parsed
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func sameCommandReceiptInvocation(left, right application.CommandReceipt) bool {
	return left.ID == right.ID && left.ProjectID == right.ProjectID && left.RunID == right.RunID &&
		left.TurnID == right.TurnID && left.Class == right.Class && left.Command == right.Command &&
		left.CommandFingerprint == right.CommandFingerprint && left.InputDigest == right.InputDigest
}

func sameLogicalCommandReceipt(left, right application.CommandReceipt) bool {
	return left.ProjectID == right.ProjectID && left.RunID == right.RunID && left.TurnID == right.TurnID &&
		left.Class == right.Class &&
		left.Command == right.Command && left.CommandFingerprint == right.CommandFingerprint &&
		left.InputDigest == right.InputDigest && left.RequestID != nil && right.RequestID != nil &&
		*left.RequestID == *right.RequestID
}

func commandOutcomeReceiptFromActivity(
	ctx context.Context,
	tx *sql.Tx,
	record activityRecord,
	cursor domain.Cursor,
	runID domain.RunID,
	turnID domain.TurnID,
) error {
	authority, err := application.AuthorityFromContext(ctx)
	if err != nil || authority.Surface != application.AuthorityProductCLI || authority.Invocation == nil ||
		authority.Invocation.Class != application.CommandReceiptOutcome || record.ScopeKind != "project" {
		return nil
	}
	if runID.IsZero() && authority.Invocation.Context.RunID != nil {
		runID = *authority.Invocation.Context.RunID
	}
	if turnID.IsZero() && authority.Invocation.Context.TurnID != nil {
		turnID = *authority.Invocation.Context.TurnID
	}
	if runID.IsZero() || turnID.IsZero() || authority.Invocation.Context.ProjectID == nil ||
		authority.Invocation.Context.ProjectID.String() != record.ScopeID {
		return nil
	}
	if _, exists, err := loadCommandReceiptByID(ctx, tx, authority.Invocation.ID); err != nil || exists {
		return err
	}
	projectID, err := domain.ParseProjectID(record.ScopeID)
	if err != nil {
		return err
	}
	resultPayload := struct {
		ActivityKind string          `json:"activityKind"`
		Cursor       domain.Cursor   `json:"cursor"`
		OutcomeKind  string          `json:"outcomeKind"`
		OutcomeID    string          `json:"outcomeId"`
		Payload      json.RawMessage `json:"payload"`
	}{record.Kind, cursor, record.OutcomeKind, record.OutcomeID, json.RawMessage(record.Payload)}
	_, resultDigest, err := domain.CanonicalDigest("open-cut/command-outcome", application.CommandReceiptSchema, resultPayload)
	if err != nil {
		return err
	}
	refs := []application.CommandReceiptRef{}
	if record.OutcomeKind != "" && record.OutcomeID != "" {
		refs = append(refs, application.CommandReceiptRef{Kind: record.OutcomeKind, ID: record.OutcomeID})
	}
	var decoded struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
	}
	if json.Unmarshal(record.Payload, &decoded) == nil {
		for _, changed := range decoded.ChangedEntityRefs {
			revision := changed.Revision
			refs = append(refs, application.CommandReceiptRef{Kind: changed.Kind, ID: changed.ID, Revision: &revision})
		}
	}
	if len(refs) > application.MaximumCommandReceiptRefs {
		return application.ErrCommandReceiptInvalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, record.OccurredAt)
	if err != nil {
		return err
	}
	projectRevision, err := activityProjectRevision(record.ProjectRevision)
	if err != nil {
		return err
	}
	receipt := application.CommandReceipt{
		Schema: application.CommandReceiptSchema, ID: authority.Invocation.ID, ProjectID: projectID,
		RunID: runID, TurnID: turnID, Class: application.CommandReceiptOutcome,
		Command: authority.Invocation.Command, CommandFingerprint: authority.Invocation.Fingerprint,
		InputDigest: authority.Invocation.InputDigest, RequestID: authority.Invocation.RequestID,
		ResultDigest: resultDigest, Status: commandOutcomeReceiptStatus(authority.Invocation.Command), ResultRefs: refs,
		ProjectRevision: projectRevision, ActivityCursor: &cursor, CreatedAt: createdAt.UTC(),
	}
	if receipt.RequestID != nil {
		existing, exists, loadErr := loadCommandReceiptByRequest(ctx, tx, authority.Actor, *receipt.RequestID)
		if loadErr != nil {
			return loadErr
		}
		if exists {
			if !sameLogicalCommandReceipt(existing, receipt) {
				return application.ErrCommandReceiptInvalid
			}
			return nil
		}
	}
	if err := validateCommandReceiptOwner(ctx, tx, application.RecordCommandReceipt{Receipt: receipt, Actor: authority.Actor}); err != nil {
		return err
	}
	receipt.Ordinal, err = nextCommandReceiptOrdinal(ctx, tx, turnID)
	if err != nil {
		return err
	}
	return insertCommandReceipt(ctx, tx, receipt, authority.Actor)
}

func commandOutcomeReceiptStatus(command string) application.CommandReceiptStatus {
	if command == "export start" || command == "export retry" {
		return application.CommandReceiptAccepted
	}
	return application.CommandReceiptSucceeded
}

func activityProjectRevision(value any) (*domain.Revision, error) {
	if value == nil {
		return nil, nil
	}
	var raw uint64
	switch typed := value.(type) {
	case int64:
		if typed < 1 {
			return nil, application.ErrCommandReceiptInvalid
		}
		raw = uint64(typed)
	case uint64:
		raw = typed
	default:
		return nil, fmt.Errorf("unsupported activity revision type %T", value)
	}
	revision, err := domain.NewRevision(raw)
	return &revision, err
}
