package editmigration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/PerishCode/open-cut/product/domain"
)

// RewriteEditJournalSchemaV2ToV3 advances canonical domains after the Clip
// mutation union was closed. Operation payload shape is retained exactly.
func RewriteEditJournalSchemaV2ToV3(ctx context.Context, tx *sql.Tx) error {
	if err := requireClosedLinkGroups(ctx, tx); err != nil {
		return err
	}
	if err := rewriteProposalSchemaV2ToV3(ctx, tx); err != nil {
		return err
	}
	return rewriteTransactionSchemaV2ToV3(ctx, tx)
}

func requireClosedLinkGroups(ctx context.Context, tx *sql.Tx) error {
	var invalid int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM (
  SELECT g.id, g.tombstoned, COUNT(c.id) AS live_members
  FROM clip_link_groups g
  LEFT JOIN clips c ON c.link_group_id = g.id AND c.tombstoned = 0
  GROUP BY g.id, g.tombstoned
  HAVING (g.tombstoned = 0 AND COUNT(c.id) NOT BETWEEN 2 AND 64)
     OR (g.tombstoned = 1 AND COUNT(c.id) <> 0)
)`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("clip mutation migration found %d invalid LinkGroups", invalid)
	}
	return nil
}

func rewriteProposalSchemaV2ToV3(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, canonical_json FROM edit_proposals
WHERE schema_version = ? ORDER BY id`, unifiedEditProposalSchema)
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
		canonical, digest, err := domain.CanonicalDigest("open-cut/edit-proposal", clipEditProposalSchema, payload)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE edit_proposals SET schema_version = ?, digest = ?, canonical_json = ?
WHERE id = ? AND schema_version = ?`, clipEditProposalSchema, digest.String(), string(canonical),
			current.id, unifiedEditProposalSchema); err != nil {
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

func rewriteTransactionSchemaV2ToV3(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, proposal_id, project_id, actor_kind, actor_id, intent, operation_json,
       inverse_json, changes_json, project_revision, undoes_transaction_id
FROM edit_transactions WHERE schema_version = ? ORDER BY project_revision, id`, unifiedEditTransactionSchema)
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
		if err := json.Unmarshal([]byte(current.operations), &operations); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(current.inverse), &inverse); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(current.changes), &changes); err != nil {
			return err
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
		_, digest, err := domain.CanonicalDigest("open-cut/edit-transaction", clipEditTransactionSchema, struct {
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
WHERE id = ? AND schema_version = ?`, clipEditTransactionSchema, digest.String(),
			current.id, unifiedEditTransactionSchema); err != nil {
			return err
		}
	}
	return nil
}
