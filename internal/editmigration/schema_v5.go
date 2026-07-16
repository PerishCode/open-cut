package editmigration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/PerishCode/open-cut/product/domain"
)

// RewriteEditJournalSchemaV4ToV5 replaces the split Narrative leaf operation
// representation with one typed common-node union. Existing authored text is
// intentionally promoted to the closed spoken/und semantics; no runtime v4
// decoder remains after this forward-only migration.
func RewriteEditJournalSchemaV4ToV5(ctx context.Context, tx *sql.Tx) error {
	if err := rewriteProposalSchemaV4ToV5(ctx, tx); err != nil {
		return err
	}
	return rewriteTransactionSchemaV4ToV5(ctx, tx)
}

func rewriteProposalSchemaV4ToV5(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, canonical_json, operations_json, inverse_preview_json
FROM edit_proposals WHERE schema_version = ? ORDER BY id`, roughCutEditProposalSchema)
	if err != nil {
		return err
	}
	type value struct{ id, canonical, operations, inverse string }
	values := make([]value, 0)
	for rows.Next() {
		var current value
		if err := rows.Scan(&current.id, &current.canonical, &current.operations, &current.inverse); err != nil {
			rows.Close()
			return err
		}
		values = append(values, current)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, current := range values {
		envelope, err := decodeJSON(current.canonical)
		if err != nil {
			return err
		}
		record, ok := envelope.(map[string]any)
		if !ok || record["domain"] != "open-cut/edit-proposal" {
			return fmt.Errorf("unexpected v4 proposal canonical envelope")
		}
		payload, ok := record["payload"].(map[string]any)
		if !ok {
			return fmt.Errorf("v4 proposal canonical payload is missing")
		}
		if err := rewriteNarrativeOperationField(payload, "operations"); err != nil {
			return err
		}
		if err := rewriteNarrativeOperationField(payload, "inverse"); err != nil {
			return err
		}
		canonical, digest, err := domain.CanonicalDigest(
			"open-cut/edit-proposal", domain.EditProposalSchema, payload,
		)
		if err != nil {
			return err
		}
		operations, err := rewriteNarrativeOperationJSON(current.operations)
		if err != nil {
			return err
		}
		inverse, err := rewriteNarrativeOperationJSON(current.inverse)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE edit_proposals SET schema_version = ?, digest = ?, canonical_json = ?,
  operations_json = ?, inverse_preview_json = ?
WHERE id = ? AND schema_version = ?`, domain.EditProposalSchema, digest.String(), string(canonical),
			string(operations), string(inverse), current.id, roughCutEditProposalSchema); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE activity_outbox SET payload_json = json_set(payload_json, '$.proposalDigest', ?)
WHERE kind = 'edit.proposed' AND outcome_kind = 'proposal' AND outcome_id = ?`,
			digest.String(), current.id); err != nil {
			return err
		}
	}
	return nil
}

func rewriteTransactionSchemaV4ToV5(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, proposal_id, project_id, actor_kind, actor_id, intent, operation_json,
       inverse_json, changes_json, project_revision, undoes_transaction_id
FROM edit_transactions WHERE schema_version = ? ORDER BY project_revision, id`, roughCutEditTransactionSchema)
	if err != nil {
		return err
	}
	type value struct {
		id, proposalID, projectID, actorKind, actorID, intent string
		operations, inverse, changes                          string
		projectRevision                                       uint64
		undoes                                                sql.NullString
	}
	values := make([]value, 0)
	for rows.Next() {
		var current value
		if err := rows.Scan(
			&current.id, &current.proposalID, &current.projectID, &current.actorKind, &current.actorID,
			&current.intent, &current.operations, &current.inverse, &current.changes,
			&current.projectRevision, &current.undoes,
		); err != nil {
			rows.Close()
			return err
		}
		values = append(values, current)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, current := range values {
		operationsJSON, err := rewriteNarrativeOperationJSON(current.operations)
		if err != nil {
			return err
		}
		inverseJSON, err := rewriteNarrativeOperationJSON(current.inverse)
		if err != nil {
			return err
		}
		var operations, inverse []domain.NormalizedEditOperation
		var changes []domain.EntityRevisionChange
		if json.Unmarshal(operationsJSON, &operations) != nil || json.Unmarshal(inverseJSON, &inverse) != nil ||
			json.Unmarshal([]byte(current.changes), &changes) != nil {
			return fmt.Errorf("invalid v4 edit transaction payload")
		}
		actor, err := storedActor(current.actorKind, current.actorID)
		if err != nil {
			return err
		}
		projectID, err := domain.ParseProjectID(current.projectID)
		if err != nil {
			return err
		}
		projectRevision, err := domain.NewRevision(current.projectRevision)
		if err != nil {
			return err
		}
		var proposalDigestValue string
		if err := tx.QueryRowContext(ctx, `SELECT digest FROM edit_proposals WHERE id = ?`,
			current.proposalID).Scan(&proposalDigestValue); err != nil {
			return err
		}
		proposalDigest, err := domain.ParseDigest(proposalDigestValue)
		if err != nil {
			return err
		}
		var undoes *domain.TransactionID
		if current.undoes.Valid {
			parsed, err := domain.ParseTransactionID(current.undoes.String)
			if err != nil {
				return err
			}
			undoes = &parsed
		}
		_, digest, err := domain.CanonicalDigest("open-cut/edit-transaction", domain.EditTransactionSchema, struct {
			Actor                    domain.ActorRef                  `json:"actor"`
			Changes                  []domain.EntityRevisionChange    `json:"changes"`
			CommittedProjectRevision domain.Revision                  `json:"committedProjectRevision"`
			Intent                   string                           `json:"intent"`
			Inverse                  []domain.NormalizedEditOperation `json:"inverse"`
			Operations               []domain.NormalizedEditOperation `json:"operations"`
			ProjectID                domain.ProjectID                 `json:"projectId"`
			ProposalDigest           domain.Digest                    `json:"proposalDigest"`
			Undoes                   *domain.TransactionID            `json:"undoesTransactionId,omitempty"`
		}{
			Actor: actor, Changes: changes, CommittedProjectRevision: projectRevision,
			Intent: current.intent, Inverse: inverse, Operations: operations,
			ProjectID: projectID, ProposalDigest: proposalDigest, Undoes: undoes,
		})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE edit_transactions SET schema_version = ?, digest = ?, operation_json = ?, inverse_json = ?
WHERE id = ? AND schema_version = ?`, domain.EditTransactionSchema, digest.String(),
			string(operationsJSON), string(inverseJSON), current.id, roughCutEditTransactionSchema); err != nil {
			return err
		}
	}
	return nil
}

func rewriteNarrativeOperationJSON(value string) ([]byte, error) {
	decoded, err := decodeJSON(value)
	if err != nil {
		return nil, err
	}
	operations, ok := decoded.([]any)
	if !ok {
		return nil, fmt.Errorf("normalized operation list is invalid")
	}
	if err := rewriteNarrativeOperations(operations); err != nil {
		return nil, err
	}
	return json.Marshal(operations)
}

func rewriteNarrativeOperationField(payload map[string]any, field string) error {
	operations, ok := payload[field].([]any)
	if !ok {
		return fmt.Errorf("proposal %s operation list is invalid", field)
	}
	return rewriteNarrativeOperations(operations)
}

func rewriteNarrativeOperations(operations []any) error {
	for _, raw := range operations {
		operation, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("normalized operation is invalid")
		}
		switch operation["type"] {
		case "put-authored-text":
			value, ok := operation["authoredText"].(map[string]any)
			if !ok {
				return fmt.Errorf("v4 authored-text payload is invalid")
			}
			value["purpose"] = "spoken"
			value["language"] = "und"
			delete(operation, "authoredText")
			operation["type"] = "put-narrative-node"
			operation["narrativeNode"] = map[string]any{
				"kind": "authored-text", "authoredText": value,
			}
		case "put-source-excerpt":
			value, ok := operation["sourceExcerpt"].(map[string]any)
			if !ok {
				return fmt.Errorf("v4 source-excerpt payload is invalid")
			}
			delete(operation, "sourceExcerpt")
			operation["type"] = "put-narrative-node"
			operation["narrativeNode"] = map[string]any{
				"kind": "source-excerpt", "sourceExcerpt": value,
			}
		case "put-narrative-node":
			return fmt.Errorf("v4 journal unexpectedly contains a v5 Narrative operation")
		}
	}
	return nil
}
