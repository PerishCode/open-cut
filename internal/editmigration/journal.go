package editmigration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	legacyEditProposalSchema      = "open-cut/edit-proposal/v1"
	legacyEditTransactionSchema   = "open-cut/edit-transaction/v1"
	unifiedEditProposalSchema     = "open-cut/edit-proposal/v2"
	unifiedEditTransactionSchema  = "open-cut/edit-transaction/v2"
	clipEditProposalSchema        = "open-cut/edit-proposal/v3"
	clipEditTransactionSchema     = "open-cut/edit-transaction/v3"
	roughCutEditProposalSchema    = "open-cut/edit-proposal/v4"
	roughCutEditTransactionSchema = "open-cut/edit-transaction/v4"
)

// RewriteUnifiedAlignmentJournals performs the forward-only journal rewrite
// paired with SQLite migration 0024.
func RewriteUnifiedAlignmentJournals(ctx context.Context, tx *sql.Tx) error {
	if err := rewriteAlignmentProposals(ctx, tx); err != nil {
		return err
	}
	return rewriteAlignmentTransactions(ctx, tx)
}

func rewriteAlignmentProposals(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, canonical_json, operations_json, inverse_preview_json
FROM edit_proposals WHERE schema_version = ? ORDER BY id`, legacyEditProposalSchema)
	if err != nil {
		return err
	}
	type proposalRow struct{ id, canonical, operations, inverse string }
	values := make([]proposalRow, 0)
	for rows.Next() {
		var value proposalRow
		if err := rows.Scan(&value.id, &value.canonical, &value.operations, &value.inverse); err != nil {
			rows.Close()
			return err
		}
		values = append(values, value)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, value := range values {
		canonical, digest, err := rewriteProposalCanonical(value.canonical)
		if err != nil {
			return fmt.Errorf("migrate proposal %s canonical payload: %w", value.id, err)
		}
		operations, err := rewriteAlignmentJSON(value.operations)
		if err != nil {
			return fmt.Errorf("migrate proposal %s operations: %w", value.id, err)
		}
		inverse, err := rewriteAlignmentJSON(value.inverse)
		if err != nil {
			return fmt.Errorf("migrate proposal %s inverse: %w", value.id, err)
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE edit_proposals SET schema_version = ?, digest = ?, canonical_json = ?,
  operations_json = ?, inverse_preview_json = ? WHERE id = ? AND schema_version = ?`,
			unifiedEditProposalSchema, digest.String(), string(canonical), string(operations), string(inverse),
			value.id, legacyEditProposalSchema); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE activity_outbox SET payload_json = json_set(payload_json, '$.proposalDigest', ?)
WHERE kind = 'edit.proposed' AND outcome_kind = 'proposal' AND outcome_id = ?`,
			digest.String(), value.id); err != nil {
			return err
		}
	}
	return nil
}

func rewriteProposalCanonical(value string) ([]byte, domain.Digest, error) {
	envelope, err := decodeJSON(value)
	if err != nil {
		return nil, "", err
	}
	record, ok := envelope.(map[string]any)
	if !ok || record["domain"] != "open-cut/edit-proposal" {
		return nil, "", fmt.Errorf("unexpected proposal canonical envelope")
	}
	payload, exists := record["payload"]
	if !exists {
		return nil, "", fmt.Errorf("proposal canonical payload is missing")
	}
	if err := rewriteAlignmentValue(payload); err != nil {
		return nil, "", err
	}
	return domain.CanonicalDigest("open-cut/edit-proposal", unifiedEditProposalSchema, payload)
}

func rewriteAlignmentTransactions(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, proposal_id, project_id, actor_kind, actor_id, intent, operation_json,
       inverse_json, changes_json, project_revision, undoes_transaction_id
FROM edit_transactions WHERE schema_version = ? ORDER BY project_revision, id`, legacyEditTransactionSchema)
	if err != nil {
		return err
	}
	type transactionRow struct {
		id, proposalID, projectID, actorKind, actorID, intent string
		operations, inverse, changes                          string
		projectRevision                                       uint64
		undoes                                                sql.NullString
	}
	values := make([]transactionRow, 0)
	for rows.Next() {
		var value transactionRow
		if err := rows.Scan(
			&value.id, &value.proposalID, &value.projectID, &value.actorKind, &value.actorID, &value.intent,
			&value.operations, &value.inverse, &value.changes, &value.projectRevision, &value.undoes,
		); err != nil {
			rows.Close()
			return err
		}
		values = append(values, value)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, value := range values {
		operationsJSON, err := rewriteAlignmentJSON(value.operations)
		if err != nil {
			return err
		}
		inverseJSON, err := rewriteAlignmentJSON(value.inverse)
		if err != nil {
			return err
		}
		var operations, inverse []domain.NormalizedEditOperation
		var changes []domain.EntityRevisionChange
		if err := json.Unmarshal(operationsJSON, &operations); err != nil {
			return err
		}
		if err := json.Unmarshal(inverseJSON, &inverse); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(value.changes), &changes); err != nil {
			return err
		}
		actor, err := storedActor(value.actorKind, value.actorID)
		if err != nil {
			return err
		}
		projectID, err := domain.ParseProjectID(value.projectID)
		if err != nil {
			return err
		}
		projectRevision, err := domain.NewRevision(value.projectRevision)
		if err != nil {
			return err
		}
		var proposalDigestValue string
		if err := tx.QueryRowContext(ctx, `SELECT digest FROM edit_proposals WHERE id = ?`, value.proposalID).Scan(&proposalDigestValue); err != nil {
			return err
		}
		proposalDigest, err := domain.ParseDigest(proposalDigestValue)
		if err != nil {
			return err
		}
		var undoes *domain.TransactionID
		if value.undoes.Valid {
			parsed, err := domain.ParseTransactionID(value.undoes.String)
			if err != nil {
				return err
			}
			undoes = &parsed
		}
		_, digest, err := domain.CanonicalDigest("open-cut/edit-transaction", unifiedEditTransactionSchema, struct {
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
			Intent: value.intent, Inverse: inverse, Operations: operations,
			ProjectID: projectID, ProposalDigest: proposalDigest, Undoes: undoes,
		})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE edit_transactions SET schema_version = ?, operation_json = ?, inverse_json = ?, digest = ?
WHERE id = ? AND schema_version = ?`, unifiedEditTransactionSchema, string(operationsJSON),
			string(inverseJSON), digest.String(), value.id, legacyEditTransactionSchema); err != nil {
			return err
		}
	}
	return nil
}

func storedActor(kind, value string) (domain.ActorRef, error) {
	switch domain.CreativeActor(kind) {
	case domain.ActorCreator:
		id, err := domain.ParseCreatorID(value)
		if err != nil {
			return domain.ActorRef{}, err
		}
		return domain.CreatorActor(id), nil
	case domain.ActorAgent:
		id, err := domain.ParseAgentID(value)
		if err != nil {
			return domain.ActorRef{}, err
		}
		return domain.AgentActor(id), nil
	default:
		return domain.ActorRef{}, domain.ErrInvalidCreativeActor
	}
}

func rewriteAlignmentJSON(value string) ([]byte, error) {
	decoded, err := decodeJSON(value)
	if err != nil {
		return nil, err
	}
	if err := rewriteAlignmentValue(decoded); err != nil {
		return nil, err
	}
	return json.Marshal(decoded)
}

func decodeJSON(value string) (any, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func rewriteAlignmentValue(value any) error {
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			if err := rewriteAlignmentValue(item); err != nil {
				return err
			}
		}
	case map[string]any:
		if current["type"] == "put-caption-alignment" {
			alignment, ok := current["alignment"].(map[string]any)
			if !ok {
				return fmt.Errorf("legacy alignment payload is missing")
			}
			captionID, idOK := alignment["captionId"]
			captionRevision, revisionOK := alignment["captionRevision"]
			localRange, rangeOK := alignment["captionLocalRange"]
			if !idOK || !revisionOK || !rangeOK {
				return fmt.Errorf("legacy caption alignment target is incomplete")
			}
			alignment["targets"] = []any{map[string]any{
				"type": "caption",
				"caption": map[string]any{
					"captionId": captionID, "captionRevision": captionRevision, "localRange": localRange,
				},
			}}
			delete(alignment, "captionId")
			delete(alignment, "captionRevision")
			delete(alignment, "captionLocalRange")
			current["type"] = "put-alignment"
		}
		for _, item := range current {
			if err := rewriteAlignmentValue(item); err != nil {
				return err
			}
		}
	}
	return nil
}
