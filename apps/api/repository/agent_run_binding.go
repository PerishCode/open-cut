package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) AssociateAgentRunPairing(
	ctx context.Context,
	runID domain.RunID,
	turnID domain.TurnID,
	grantID string,
	grantRevision domain.Revision,
	at time.Time,
) error {
	if runID.IsZero() || turnID.IsZero() || grantID == "" || grantRevision.Value() < 1 || at.IsZero() {
		return application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var grantStatus string
	var storedGrantRevision uint64
	if err := tx.QueryRowContext(ctx, `
SELECT status, revision FROM installation_grants WHERE id = ?`, grantID).Scan(
		&grantStatus, &storedGrantRevision,
	); err != nil || grantStatus != string(application.CLIGrantPending) || storedGrantRevision != grantRevision.Value() {
		return application.ErrAgentBridgeBindingDenied
	}
	var currentTurn, authorizationState, status string
	if err := tx.QueryRowContext(ctx, `
SELECT current_turn_id, authorization_state, status
FROM agent_runs
WHERE id = ? AND EXISTS (SELECT 1 FROM agent_bridge_runs WHERE run_id = agent_runs.id)`, runID.String()).Scan(
		&currentTurn, &authorizationState, &status,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrAgentBridgeNotFound
		}
		return err
	}
	if currentTurn != turnID.String() || authorizationState != "pending" ||
		status == string(application.AgentRunFailed) || status == string(application.AgentRunCancelled) {
		return application.ErrAgentBridgeBindingDenied
	}
	var storedGrant string
	var storedRevision uint64
	err = tx.QueryRowContext(ctx, `
SELECT grant_id, grant_revision FROM agent_run_pairing_associations WHERE run_id = ?`, runID.String()).Scan(
		&storedGrant, &storedRevision,
	)
	if err == nil {
		if storedGrant != grantID || storedRevision != grantRevision.Value() {
			return application.ErrAgentBridgeBindingDenied
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_run_pairing_associations (run_id, originating_turn_id, grant_id, grant_revision, created_at)
VALUES (?, ?, ?, ?, ?)`, runID.String(), turnID.String(), grantID, grantRevision.Value(), formatInstant(at)); err != nil {
		return application.ErrAgentBridgeBindingDenied
	}
	return tx.Commit()
}

func (repository *SQLiteProjects) BindAuthorizedAgentRun(
	ctx context.Context,
	runID domain.RunID,
	turnID domain.TurnID,
	agentID domain.AgentID,
	grantID string,
	grantRevision domain.Revision,
	eventID domain.ActivityEventID,
	at time.Time,
) error {
	if runID.IsZero() || turnID.IsZero() || agentID.IsZero() || grantID == "" || grantRevision.Value() < 1 || eventID.IsZero() || at.IsZero() {
		return application.ErrAgentBridgeInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var grantStatus, grantAgent string
	var storedGrantRevision uint64
	if err := tx.QueryRowContext(ctx, `
SELECT status, agent_id, revision FROM installation_grants WHERE id = ?`, grantID).Scan(
		&grantStatus, &grantAgent, &storedGrantRevision,
	); err != nil || grantStatus != string(application.CLIGrantActive) || grantAgent != agentID.String() ||
		storedGrantRevision != grantRevision.Value() {
		return application.ErrAgentBridgeBindingDenied
	}
	var projectValue, currentTurn, authorizationState, status string
	var actorID sql.NullString
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `
SELECT run.project_id, run.current_turn_id, run.authorization_state, run.actor_id, run.status, project.revision
FROM agent_runs AS run
JOIN projects AS project ON project.id = run.project_id
WHERE run.id = ? AND EXISTS (SELECT 1 FROM agent_bridge_runs WHERE run_id = run.id)`, runID.String()).Scan(
		&projectValue, &currentTurn, &authorizationState, &actorID, &status, &projectRevision,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrAgentBridgeNotFound
		}
		return err
	}
	if currentTurn != turnID.String() || status == string(application.AgentRunFailed) ||
		status == string(application.AgentRunCancelled) || status == string(application.AgentRunCompleted) {
		return application.ErrAgentBridgeBindingDenied
	}
	if authorizationState == "bound" {
		if !actorID.Valid || actorID.String != agentID.String() {
			return application.ErrAgentBridgeBindingDenied
		}
		return tx.Commit()
	}
	var associatedGrant string
	var associatedRevision uint64
	err = tx.QueryRowContext(ctx, `
SELECT grant_id, grant_revision FROM agent_run_pairing_associations WHERE run_id = ?`, runID.String()).Scan(
		&associatedGrant, &associatedRevision,
	)
	if err == nil && (associatedGrant != grantID || associatedRevision != grantRevision.Value()) {
		return application.ErrAgentBridgeBindingDenied
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := formatInstant(at)
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs
SET actor_id = ?, authorization_state = 'bound', status = 'active', waiting_reason = NULL, updated_at = ?
WHERE id = ?`, agentID.String(), now, runID.String()); err != nil {
		return err
	}
	projectID, err := domain.ParseProjectID(projectValue)
	if err != nil {
		return err
	}
	revision, err := domain.NewRevision(projectRevision)
	if err != nil {
		return err
	}
	if _, err := appendRunActivity(ctx, tx, projectID, revision, domain.AgentActor(agentID), eventID,
		"run.authorized", runID, turnID, "run-authorized", at); err != nil {
		return err
	}
	return tx.Commit()
}
