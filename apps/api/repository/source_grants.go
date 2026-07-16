package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) RegisterSourceGrant(
	ctx context.Context,
	record application.RegisterSourceGrantRecord,
) (application.SourceGrantResult, error) {
	if err := validateSourceGrantRecord(record); err != nil {
		return application.SourceGrantResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SourceGrantResult{}, err
	}
	defer tx.Rollback()
	var existingID, existingDigest string
	err = tx.QueryRowContext(ctx, `
SELECT source_grant_id, input_digest FROM source_grant_requests
WHERE creator_id = ? AND request_id = ?`, record.CreatorID.String(), record.RequestID.String()).Scan(
		&existingID, &existingDigest,
	)
	if err == nil {
		if existingDigest != record.InputDigest.String() {
			return application.SourceGrantResult{}, application.ErrSourceGrantReused
		}
		id, parseErr := domain.ParseSourceGrantID(existingID)
		if parseErr != nil {
			return application.SourceGrantResult{}, parseErr
		}
		grant, loadErr := loadSourceGrant(ctx, tx, record.InstallationID, id)
		if loadErr != nil {
			return application.SourceGrantResult{}, loadErr
		}
		if err := tx.Commit(); err != nil {
			return application.SourceGrantResult{}, err
		}
		return application.SourceGrantResult{Grant: grant, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return application.SourceGrantResult{}, err
	}
	createdAt := formatInstant(record.CreatedAt)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO source_grants (
  id, installation_id, platform, grant_kind, schema_version, protected_material,
  display_name, observed_byte_size, observed_modified_unix_ns, observed_file_identity,
  state, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?)`,
		record.ID.String(), record.InstallationID, record.Platform, record.Kind,
		application.SourceGrantRegisterSchema, record.ProtectedMaterial, record.DisplayName,
		record.Observation.ByteSize.Value(), record.Observation.ModifiedUnixNs.Value(),
		record.Observation.FileIdentity, createdAt,
	); err != nil {
		return application.SourceGrantResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO source_grant_requests (creator_id, request_id, input_digest, source_grant_id, created_at)
VALUES (?, ?, ?, ?, ?)`, record.CreatorID.String(), record.RequestID.String(),
		record.InputDigest.String(), record.ID.String(), createdAt,
	); err != nil {
		return application.SourceGrantResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SourceGrantResult{}, err
	}
	return application.SourceGrantResult{Grant: domain.SourceGrantSummary{
		ID: record.ID, Platform: record.Platform, Kind: record.Kind, DisplayName: record.DisplayName,
		Observation: record.Observation, State: domain.SourceGrantActive, CreatedAt: record.CreatedAt.UTC(),
	}}, nil
}

func (repository *SQLiteProjects) ReadSourceGrant(
	ctx context.Context,
	installationID string,
	id domain.SourceGrantID,
) (domain.SourceGrantSummary, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.SourceGrantSummary{}, err
	}
	defer tx.Rollback()
	grant, err := loadSourceGrant(ctx, tx, installationID, id)
	if err != nil {
		return domain.SourceGrantSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.SourceGrantSummary{}, err
	}
	return grant, nil
}

func loadSourceGrant(
	ctx context.Context,
	tx *sql.Tx,
	installationID string,
	id domain.SourceGrantID,
) (domain.SourceGrantSummary, error) {
	var idValue, platform, kind, displayName, fileIdentity, state, createdAt string
	var byteSize uint64
	var modifiedUnixNs int64
	err := tx.QueryRowContext(ctx, `
SELECT id, platform, grant_kind, display_name, observed_byte_size,
       observed_modified_unix_ns, observed_file_identity, state, created_at
FROM source_grants WHERE id = ? AND installation_id = ?`, id.String(), installationID).Scan(
		&idValue, &platform, &kind, &displayName, &byteSize,
		&modifiedUnixNs, &fileIdentity, &state, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SourceGrantSummary{}, application.ErrSourceGrantNotFound
	}
	if err != nil {
		return domain.SourceGrantSummary{}, err
	}
	parsedID, err := domain.ParseSourceGrantID(idValue)
	if err != nil {
		return domain.SourceGrantSummary{}, err
	}
	parsedSize, err := domain.NewUInt64(byteSize)
	if err != nil {
		return domain.SourceGrantSummary{}, err
	}
	instant, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.SourceGrantSummary{}, err
	}
	result := domain.SourceGrantSummary{
		ID: parsedID, Platform: platform, Kind: domain.SourceGrantKind(kind), DisplayName: displayName,
		Observation: domain.SourceObservation{
			ByteSize: parsedSize, ModifiedUnixNs: domain.NewInt64(modifiedUnixNs), FileIdentity: fileIdentity,
		},
		State: domain.SourceGrantState(state), CreatedAt: instant.UTC(),
	}
	if (result.Kind != domain.SourceGrantLocalPath && result.Kind != domain.SourceGrantMacBookmark) ||
		(result.State != domain.SourceGrantActive && result.State != domain.SourceGrantRevoked &&
			result.State != domain.SourceGrantUnavailable) {
		return domain.SourceGrantSummary{}, application.ErrSourceGrantInvalid
	}
	return result, nil
}

func validateSourceGrantRecord(record application.RegisterSourceGrantRecord) error {
	if record.ID.IsZero() || record.CreatorID.IsZero() || record.InstallationID == "" ||
		record.CreatedAt.IsZero() || len(record.ProtectedMaterial) == 0 ||
		len(record.ProtectedMaterial) > 64<<10 {
		return application.ErrSourceGrantInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrSourceGrantInvalid
	}
	if _, err := domain.ParseDigest(record.InputDigest.String()); err != nil {
		return application.ErrSourceGrantInvalid
	}
	return nil
}
