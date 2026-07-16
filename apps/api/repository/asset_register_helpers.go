package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func insertInitialMediaJobs(
	ctx context.Context,
	tx *sql.Tx,
	record application.RegisterAssetRecord,
) error {
	at := formatInstant(record.OccurredAt)
	for _, job := range record.Jobs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, kind, state, pool, priority_class, logical_key,
  parameters_digest, parameters_json, producer_version, created_at, updated_at
) VALUES (?, 'project', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			job.ID.String(), record.Asset.ProjectID.String(), job.Kind,
			job.State, job.Pool, job.PriorityClass, job.LogicalKey, job.ParametersDigest.String(),
			string(job.ParametersJSON), job.ProducerVersion, at, at,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO media_job_details (job_id, asset_id) VALUES (?, ?)`,
			job.ID.String(), record.Asset.ID.String()); err != nil {
			return err
		}
		for _, prerequisite := range job.Prerequisites {
			referenceKind, referenceID := mediaPrerequisiteReference(prerequisite)
			if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, ?, ?, ?, ?)`, job.ID.String(), prerequisite.Kind, referenceKind, referenceID, at); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
VALUES (?, 'asset', ?, ?)`, job.ID.String(), record.Asset.ID.String(), at); err != nil {
			return err
		}
	}
	return nil
}

func mediaPrerequisiteReference(prerequisite domain.MediaJobPrerequisite) (string, string) {
	if prerequisite.JobID != nil {
		return "job", prerequisite.JobID.String()
	}
	if prerequisite.ResourceID != "" {
		return "resource", prerequisite.ResourceID
	}
	return "capability", prerequisite.Capability
}

func appendAssetRegisteredActivity(
	ctx context.Context,
	tx *sql.Tx,
	record application.RegisterAssetRecord,
	scope string,
) (domain.Cursor, error) {
	jobIDs := make([]domain.MediaJobID, 0, len(record.Jobs))
	for _, job := range record.Jobs {
		jobIDs = append(jobIDs, job.ID)
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		JobIDs            []domain.MediaJobID            `json:"jobIds"`
		TransactionID     domain.TransactionID           `json:"transactionId"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{{
			Kind: string(domain.EntityAsset), ID: record.Asset.ID.String(), Revision: record.Asset.Revision,
		}},
		JobIDs: jobIDs, TransactionID: record.Transaction.ID,
	})
	if err != nil {
		return 0, err
	}
	eventID := record.ProjectActivityEventID
	scopeID := record.Asset.ProjectID.String()
	kind := "asset.registered"
	if scope == "installation" {
		eventID = record.InstallationActivityEventID
		scopeID = record.InstallationID
		kind = "workspace.asset-registered"
	}
	return appendActivity(ctx, tx, activityRecord{
		ScopeKind: scope, ScopeID: scopeID, EventID: eventID.String(), Kind: kind,
		OccurredAt: formatInstant(record.OccurredAt), ActorKind: string(record.Actor.Kind),
		ActorID: record.Actor.IDString(), ProjectID: record.Asset.ProjectID.String(),
		ProjectRevision: int64(record.Transaction.CommittedProjectRevision.Value()),
		OutcomeKind:     "transaction", OutcomeID: record.Transaction.ID.String(),
		SummaryCode: "asset-registered", Payload: payload,
	})
}

func loadAssetRegisterReplay(
	ctx context.Context,
	tx *sql.Tx,
	record application.RegisterAssetRecord,
) (application.AssetRegisterResult, error) {
	var schema, digestValue, projectValue, transactionValue, eventValue string
	err := tx.QueryRowContext(ctx, `
SELECT schema_version, input_digest, project_id, transaction_id, project_activity_event_id
FROM request_identities
WHERE actor_kind = ? AND actor_id = ? AND request_id = ?`,
		record.Actor.Kind, record.Actor.IDString(), record.Input.RequestID.String()).Scan(
		&schema, &digestValue, &projectValue, &transactionValue, &eventValue,
	)
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	if schema != application.AssetRegisterSchema || digestValue != record.InputDigest.String() ||
		projectValue != record.Asset.ProjectID.String() {
		return application.AssetRegisterResult{}, application.ErrAssetRequestReused
	}
	transactionID, err := domain.ParseTransactionID(transactionValue)
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	transaction, err := loadEditTransaction(ctx, tx, record.Asset.ProjectID, transactionID)
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	if len(transaction.Operations) != 1 || transaction.Operations[0].Asset == nil {
		return application.AssetRegisterResult{}, application.ErrAssetInvalid
	}
	assetID := transaction.Operations[0].Asset.ID
	detail, err := loadAssetDetail(ctx, tx, record.Asset.ProjectID, assetID)
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	cursor, err := activityCursorForEvent(ctx, tx, eventValue)
	if err != nil {
		return application.AssetRegisterResult{}, err
	}
	return application.AssetRegisterResult{Asset: detail, Transaction: transaction, ActivityCursor: cursor}, nil
}

func validateAssetRegisterRecord(record application.RegisterAssetRecord) error {
	if record.InstallationID == "" || record.Actor.Validate() != nil ||
		record.Actor.Kind != domain.ActorCreator || record.Asset.ID.IsZero() ||
		record.Asset.ProjectID.IsZero() || record.Asset.SourceGrantID.IsZero() ||
		record.Proposal.ID.IsZero() || record.ApplicationID.IsZero() || record.Transaction.ID.IsZero() ||
		record.ProjectActivityEventID.IsZero() || record.InstallationActivityEventID.IsZero() ||
		len(record.Jobs) != 5 || record.OccurredAt.IsZero() || !json.Valid(record.InputCanonical) ||
		!json.Valid(record.ProposalCanonical) {
		return application.ErrAssetInvalid
	}
	if _, err := domain.ParseDigest(record.InputDigest.String()); err != nil {
		return application.ErrAssetInvalid
	}
	if record.Transaction.CommittedProjectRevision.Value() != record.Input.ExpectedProjectRevision.Value()+1 {
		return application.ErrAssetInvalid
	}
	seen := make(map[string]struct{}, len(record.Jobs))
	for _, job := range record.Jobs {
		if job.ID.IsZero() || !json.Valid(job.ParametersJSON) || len(job.Prerequisites) == 0 || len(job.Prerequisites) > 8 {
			return application.ErrAssetInvalid
		}
		prerequisites := make(map[string]struct{}, len(job.Prerequisites))
		for _, prerequisite := range job.Prerequisites {
			if prerequisite.Validate() != nil {
				return application.ErrAssetInvalid
			}
			referenceKind, referenceID := mediaPrerequisiteReference(prerequisite)
			key := string(prerequisite.Kind) + "/" + referenceKind + "/" + referenceID
			if _, duplicate := prerequisites[key]; duplicate {
				return application.ErrAssetInvalid
			}
			prerequisites[key] = struct{}{}
		}
		if _, duplicate := seen[string(job.Kind)]; duplicate {
			return application.ErrAssetInvalid
		}
		seen[string(job.Kind)] = struct{}{}
	}
	return nil
}
