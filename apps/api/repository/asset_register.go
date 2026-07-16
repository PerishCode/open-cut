package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) RegisterAsset(
	ctx context.Context,
	record application.RegisterAssetRecord,
) (application.AssetRegisterResult, error) {
	if err := validateAssetRegisterRecord(record); err != nil {
		return application.AssetRegisterResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	defer tx.Rollback()
	if replayed, err := loadAssetRegisterReplay(ctx, tx, record); err == nil {
		if err := tx.Commit(); err != nil {
			return application.AssetRegisterResult{}, err
		}
		replayed.Replayed = true
		return replayed, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return application.AssetRegisterResult{}, err
	}
	if err := validateAssetRegisterState(ctx, tx, record); err != nil {
		return application.AssetRegisterResult{}, err
	}
	if err := insertAssetRegisterProposal(ctx, tx, record); err != nil {
		return application.AssetRegisterResult{}, err
	}
	if err := insertAssetRegisterTransaction(ctx, tx, record); err != nil {
		return application.AssetRegisterResult{}, err
	}
	if err := insertAssetProjection(ctx, tx, record); err != nil {
		return application.AssetRegisterResult{}, err
	}
	if err := insertProposalApplication(
		ctx, tx, record.ApplicationID, record.Proposal, record.Actor,
		record.Input.RequestID, record.InputDigest, record.Transaction.ID, record.OccurredAt,
	); err != nil {
		return application.AssetRegisterResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE edit_proposals
SET status = 'applied', applied_transaction_id = ?, updated_at = ?
WHERE id = ? AND status = 'pending'`, record.Transaction.ID.String(),
		formatInstant(record.OccurredAt), record.Proposal.ID.String(),
	); err != nil {
		return application.AssetRegisterResult{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE projects SET revision = ? WHERE id = ? AND revision = ? AND status = 'active'`,
		record.Transaction.CommittedProjectRevision.Value(), record.Asset.ProjectID.String(),
		record.Input.ExpectedProjectRevision.Value(),
	)
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.AssetRegisterResult{}, application.ErrEditConflict
	}
	if err := insertInitialMediaJobs(ctx, tx, record); err != nil {
		return application.AssetRegisterResult{}, err
	}
	projectCursor, err := appendAssetRegisteredActivity(ctx, tx, record, "project")
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	if _, err := appendAssetRegisteredActivity(ctx, tx, record, "installation"); err != nil {
		return application.AssetRegisterResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO request_identities (
  actor_kind, actor_id, request_id, schema_version, input_digest, input_json,
  installation_id, project_id, proposal_id, transaction_id,
  project_activity_event_id, installation_activity_event_id, status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'committed', ?)`,
		record.Actor.Kind, record.Actor.IDString(), record.Input.RequestID.String(),
		application.AssetRegisterSchema, record.InputDigest.String(), string(record.InputCanonical),
		record.InstallationID, record.Asset.ProjectID.String(), record.Proposal.ID.String(),
		record.Transaction.ID.String(), record.ProjectActivityEventID.String(),
		record.InstallationActivityEventID.String(), formatInstant(record.OccurredAt),
	); err != nil {
		return application.AssetRegisterResult{}, err
	}
	detail, err := loadAssetDetail(ctx, tx, record.Asset.ProjectID, record.Asset.ID)
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.AssetRegisterResult{}, err
	}
	return application.AssetRegisterResult{
		Asset: detail, Transaction: record.Transaction, ActivityCursor: projectCursor,
	}, nil
}

func validateAssetRegisterState(
	ctx context.Context,
	tx *sql.Tx,
	record application.RegisterAssetRecord,
) error {
	var revision uint64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT revision, status FROM projects WHERE id = ?`,
		record.Asset.ProjectID.String()).Scan(&revision, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrProjectNotFound
		}
		return err
	}
	if status != string(domain.ProjectActive) || revision != record.Input.ExpectedProjectRevision.Value() {
		return application.ErrEditConflict
	}
	var displayName, state string
	if err := tx.QueryRowContext(ctx, `
SELECT display_name, state FROM source_grants
WHERE id = ? AND installation_id = ?`, record.Input.SourceGrantID.String(), record.InstallationID).Scan(
		&displayName, &state,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrSourceGrantNotFound
		}
		return err
	}
	if state != string(domain.SourceGrantActive) || displayName != record.Asset.DisplayName {
		return application.ErrSourceGrantNotFound
	}
	var existing int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM assets WHERE project_id = ? AND source_grant_id = ?`,
		record.Asset.ProjectID.String(), record.Input.SourceGrantID.String()).Scan(&existing)
	if err == nil {
		return application.ErrAssetAlreadyImported
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

func insertAssetRegisterProposal(
	ctx context.Context,
	tx *sql.Tx,
	record application.RegisterAssetRecord,
) error {
	preconditions, _ := json.Marshal(record.Proposal.Preconditions)
	allocation, _ := json.Marshal(record.Proposal.Allocation)
	operations, _ := json.Marshal(record.Proposal.Operations)
	inverse, _ := json.Marshal(record.Proposal.InversePreview)
	changes, _ := json.Marshal(record.Proposal.Changes)
	impact, _ := json.Marshal(record.Proposal.Impact)
	at := formatInstant(record.OccurredAt)
	_, err := tx.ExecContext(ctx, `
INSERT INTO edit_proposals (
  id, project_id, schema_version, digest, canonical_json, actor_kind, actor_id, status, created_at,
  request_id, base_project_revision, intent, preconditions_json, allocation_json,
  operations_json, inverse_preview_json, changes_json, impact_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Proposal.ID.String(), record.Asset.ProjectID.String(), domain.AssetRegisterProposalSchema,
		record.Proposal.Digest.String(), string(record.ProposalCanonical), record.Actor.Kind,
		record.Actor.IDString(), at, record.Input.RequestID.String(),
		record.Input.ExpectedProjectRevision.Value(), record.Proposal.Intent,
		string(preconditions), string(allocation), string(operations), string(inverse),
		string(changes), string(impact), at,
	)
	return err
}

func insertAssetRegisterTransaction(
	ctx context.Context,
	tx *sql.Tx,
	record application.RegisterAssetRecord,
) error {
	operations, _ := json.Marshal(record.Transaction.Operations)
	inverse, _ := json.Marshal(record.Transaction.InverseOperations)
	changes, _ := json.Marshal(record.Transaction.Changes)
	_, err := tx.ExecContext(ctx, `
INSERT INTO edit_transactions (
  id, project_id, proposal_id, project_revision, schema_version, operation_json,
  inverse_json, actor_kind, actor_id, committed_at, intent, digest, changes_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Transaction.ID.String(), record.Asset.ProjectID.String(), record.Proposal.ID.String(),
		record.Transaction.CommittedProjectRevision.Value(), domain.AssetRegisterTransactionSchema,
		string(operations), string(inverse), record.Actor.Kind, record.Actor.IDString(),
		formatInstant(record.OccurredAt), record.Transaction.Intent, record.Transaction.Digest.String(),
		string(changes),
	)
	return err
}

func insertAssetProjection(
	ctx context.Context,
	tx *sql.Tx,
	record application.RegisterAssetRecord,
) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO assets (
  id, project_id, revision, source_grant_id, display_name, import_mode,
  tombstoned, last_transaction_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		record.Asset.ID.String(), record.Asset.ProjectID.String(), record.Asset.Revision.Value(),
		record.Asset.SourceGrantID.String(), record.Asset.DisplayName, record.Asset.ImportMode,
		record.Transaction.ID.String(), formatInstant(record.OccurredAt),
	); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO asset_media_state (asset_id, availability, updated_at)
VALUES (?, 'identifying', ?)`, record.Asset.ID.String(), formatInstant(record.OccurredAt))
	return err
}
