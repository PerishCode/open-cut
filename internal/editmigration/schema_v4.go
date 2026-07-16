package editmigration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/PerishCode/open-cut/product/domain"
)

// RewriteEditJournalSchemaV3ToV4 advances the creative input domain for the
// deterministic rough-cut operation. Normalized primitive payloads are kept
// byte-for-byte, but their canonical envelope and dependent digests advance.
func RewriteEditJournalSchemaV3ToV4(ctx context.Context, tx *sql.Tx) error {
	if err := rewriteProposalSchemaV3ToV4(ctx, tx); err != nil {
		return err
	}
	return rewriteTransactionSchemaV3ToV4(ctx, tx)
}

func rewriteProposalSchemaV3ToV4(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, canonical_json FROM edit_proposals
WHERE schema_version = ? ORDER BY id`, clipEditProposalSchema)
	if err != nil {
		return err
	}
	type value struct{ id, canonical string }
	values := make([]value, 0)
	for rows.Next() {
		var current value
		if err := rows.Scan(&current.id, &current.canonical); err != nil {
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
			return fmt.Errorf("unexpected proposal canonical envelope")
		}
		payload, exists := record["payload"]
		if !exists {
			return fmt.Errorf("proposal canonical payload is missing")
		}
		canonical, digest, err := domain.CanonicalDigest("open-cut/edit-proposal", roughCutEditProposalSchema, payload)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE edit_proposals SET schema_version = ?, digest = ?, canonical_json = ?
WHERE id = ? AND schema_version = ?`, roughCutEditProposalSchema, digest.String(), string(canonical),
			current.id, clipEditProposalSchema); err != nil {
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

func rewriteTransactionSchemaV3ToV4(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, proposal_id, project_id, actor_kind, actor_id, intent, operation_json,
       inverse_json, changes_json, project_revision, undoes_transaction_id
FROM edit_transactions WHERE schema_version = ? ORDER BY project_revision, id`, clipEditTransactionSchema)
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
		var operations, inverse []domain.NormalizedEditOperation
		var changes []domain.EntityRevisionChange
		if json.Unmarshal([]byte(current.operations), &operations) != nil ||
			json.Unmarshal([]byte(current.inverse), &inverse) != nil ||
			json.Unmarshal([]byte(current.changes), &changes) != nil {
			return fmt.Errorf("invalid v3 edit transaction payload")
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
		if err := tx.QueryRowContext(ctx, `SELECT digest FROM edit_proposals WHERE id = ?`, current.proposalID).Scan(&proposalDigestValue); err != nil {
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
		_, digest, err := domain.CanonicalDigest("open-cut/edit-transaction", roughCutEditTransactionSchema, struct {
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
UPDATE edit_transactions SET schema_version = ?, digest = ?
WHERE id = ? AND schema_version = ?`, roughCutEditTransactionSchema, digest.String(),
			current.id, clipEditTransactionSchema); err != nil {
			return err
		}
	}
	return nil
}
