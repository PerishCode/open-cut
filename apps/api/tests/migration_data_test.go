package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/internal/editmigration"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/ncruces/go-sqlite3"
	sqliteDriver "github.com/ncruces/go-sqlite3/driver"
)

func TestUnifiedAlignmentDataMigrationRewritesJournalsAndDigests(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	db, err := sqliteDriver.Open(t.TempDir()+"/migration.db", func(connection *sqlite3.Conn) error {
		return connection.Exec(`PRAGMA foreign_keys = ON;`)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	statements := []string{`
CREATE TABLE edit_proposals (
  id TEXT PRIMARY KEY, schema_version TEXT NOT NULL, digest TEXT NOT NULL,
  canonical_json TEXT NOT NULL, operations_json TEXT NOT NULL,
  inverse_preview_json TEXT NOT NULL
);`, `CREATE TABLE activity_outbox (
  kind TEXT NOT NULL, outcome_kind TEXT, outcome_id TEXT, payload_json TEXT NOT NULL
);`, `CREATE TABLE edit_transactions (
  id TEXT PRIMARY KEY, proposal_id TEXT NOT NULL, project_id TEXT NOT NULL,
  actor_kind TEXT NOT NULL, actor_id TEXT NOT NULL, intent TEXT NOT NULL,
  operation_json TEXT NOT NULL, inverse_json TEXT NOT NULL, changes_json TEXT NOT NULL,
  project_revision INTEGER NOT NULL, undoes_transaction_id TEXT,
  schema_version TEXT NOT NULL, digest TEXT NOT NULL
);`, `CREATE TABLE clip_link_groups (id TEXT PRIMARY KEY, tombstoned INTEGER NOT NULL);`,
		`CREATE TABLE clips (id TEXT PRIMARY KEY, link_group_id TEXT, tombstoned INTEGER NOT NULL);`}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	proposalID := "018f0000-0000-7000-8000-000000000001"
	transactionID := "018f0000-0000-7000-8000-000000000002"
	projectID := "018f0000-0000-7000-8000-000000000003"
	agentID := "018f0000-0000-7000-8000-000000000004"
	legacyOperation := map[string]any{
		"type": "put-caption-alignment",
		"alignment": map[string]any{
			"id": "018f0000-0000-7000-8000-000000000005", "revision": "1",
			"narrativeNodeId":       "018f0000-0000-7000-8000-000000000006",
			"narrativeNodeRevision": "1", "sequenceId": "018f0000-0000-7000-8000-000000000007",
			"captionId": "018f0000-0000-7000-8000-000000000008", "captionRevision": "1",
			"captionLocalRange": map[string]any{
				"start":    map[string]any{"value": "0", "scale": 1},
				"duration": map[string]any{"value": "2", "scale": 1},
			},
			"status": "exact",
		},
	}
	operations, _ := json.Marshal([]any{legacyOperation})
	payload := map[string]any{
		"actor":      map[string]any{"kind": "agent", "agentId": agentID},
		"operations": []any{legacyOperation}, "inverse": []any{legacyOperation},
	}
	canonical, legacyDigest, err := domain.CanonicalDigest("open-cut/edit-proposal", "open-cut/edit-proposal/v1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO edit_proposals VALUES (?, ?, ?, ?, ?, ?)`,
		proposalID, "open-cut/edit-proposal/v1", legacyDigest.String(), string(canonical), string(operations), string(operations),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO activity_outbox VALUES ('edit.proposed', 'proposal', ?, json_object('proposalDigest', ?))`,
		proposalID, legacyDigest.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO edit_transactions VALUES (?, ?, ?, ?, ?, 'legacy edit', ?, ?, '[]', 2, NULL, ?, ?)`,
		transactionID, proposalID, projectID, "agent", agentID, string(operations), string(operations),
		"open-cut/edit-transaction/v1", strings.Repeat("0", 71)); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := editmigration.RewriteUnifiedAlignmentJournals(ctx, tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := editmigration.RewriteEditJournalSchemaV2ToV3(ctx, tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	var pinnedV3Proposal, pinnedV3Transaction string
	if err := tx.QueryRowContext(ctx, `SELECT schema_version FROM edit_proposals WHERE id = ?`, proposalID).Scan(&pinnedV3Proposal); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT schema_version FROM edit_transactions WHERE id = ?`, transactionID).Scan(&pinnedV3Transaction); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if pinnedV3Proposal != "open-cut/edit-proposal/v3" || pinnedV3Transaction != "open-cut/edit-transaction/v3" {
		tx.Rollback()
		t.Fatalf("v25 drifted: proposal=%s transaction=%s", pinnedV3Proposal, pinnedV3Transaction)
	}
	if err := editmigration.RewriteEditJournalSchemaV3ToV4(ctx, tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	var pinnedV4Proposal, pinnedV4Transaction string
	if err := tx.QueryRowContext(ctx, `SELECT schema_version FROM edit_proposals WHERE id = ?`, proposalID).Scan(&pinnedV4Proposal); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT schema_version FROM edit_transactions WHERE id = ?`, transactionID).Scan(&pinnedV4Transaction); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if pinnedV4Proposal != "open-cut/edit-proposal/v4" || pinnedV4Transaction != "open-cut/edit-transaction/v4" {
		tx.Rollback()
		t.Fatalf("v26 drifted: proposal=%s transaction=%s", pinnedV4Proposal, pinnedV4Transaction)
	}
	if err := editmigration.RewriteEditJournalSchemaV4ToV5(ctx, tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var proposalSchema, proposalDigest, proposalOperations, proposalCanonical string
	if err := db.QueryRowContext(ctx, `
SELECT schema_version, digest, operations_json, canonical_json FROM edit_proposals WHERE id = ?`, proposalID).Scan(
		&proposalSchema, &proposalDigest, &proposalOperations, &proposalCanonical,
	); err != nil {
		t.Fatal(err)
	}
	if proposalSchema != domain.EditProposalSchema || proposalDigest == legacyDigest.String() ||
		strings.Contains(proposalOperations, "caption-alignment") || strings.Contains(proposalCanonical, "captionLocalRange") {
		t.Fatalf("proposal schema=%s digest=%s operations=%s canonical=%s", proposalSchema, proposalDigest, proposalOperations, proposalCanonical)
	}
	var normalized []domain.NormalizedEditOperation
	if err := json.Unmarshal([]byte(proposalOperations), &normalized); err != nil {
		t.Fatal(err)
	}
	if len(normalized) != 1 || normalized[0].Type != domain.NormalizedPutAlignment ||
		normalized[0].Alignment == nil || len(normalized[0].Alignment.Targets) != 1 ||
		normalized[0].Alignment.Targets[0].Caption == nil {
		t.Fatalf("normalized=%+v", normalized)
	}

	var transactionSchema, transactionDigest, transactionOperations, activityPayload string
	if err := db.QueryRowContext(ctx, `
SELECT schema_version, digest, operation_json FROM edit_transactions WHERE id = ?`, transactionID).Scan(
		&transactionSchema, &transactionDigest, &transactionOperations,
	); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT payload_json FROM activity_outbox WHERE outcome_id = ?`, proposalID).Scan(&activityPayload); err != nil {
		t.Fatal(err)
	}
	if transactionSchema != domain.EditTransactionSchema || strings.Contains(transactionOperations, "caption-alignment") ||
		transactionDigest == strings.Repeat("0", 71) || !strings.Contains(activityPayload, proposalDigest) {
		t.Fatalf("transaction schema=%s digest=%s operations=%s activity=%s", transactionSchema, transactionDigest, transactionOperations, activityPayload)
	}
}

func TestUnifiedAlignmentSQLMigrationRewritesProjection(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	db, err := sqliteDriver.Open(t.TempDir()+"/projection.db", func(connection *sqlite3.Conn) error {
		return connection.Exec(`PRAGMA foreign_keys = ON;`)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		`CREATE TABLE projects (id TEXT PRIMARY KEY)`,
		`CREATE TABLE narrative_leaf_nodes (id TEXT PRIMARY KEY)`,
		`CREATE TABLE sequences (id TEXT PRIMARY KEY)`,
		`CREATE TABLE captions (id TEXT PRIMARY KEY)`,
		`CREATE TABLE clips (id TEXT PRIMARY KEY)`,
		`CREATE TABLE edit_transactions (id TEXT PRIMARY KEY)`,
		`CREATE TABLE caption_alignments (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, narrative_node_id TEXT NOT NULL,
  narrative_node_revision INTEGER NOT NULL, sequence_id TEXT NOT NULL,
  caption_id TEXT NOT NULL, caption_revision INTEGER NOT NULL,
  local_start_value INTEGER NOT NULL, local_start_scale INTEGER NOT NULL,
  local_duration_value INTEGER NOT NULL, local_duration_scale INTEGER NOT NULL,
  revision INTEGER NOT NULL, status TEXT NOT NULL, last_transaction_id TEXT NOT NULL
)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{
		"018f0000-0000-7000-8000-000000000011",
		"018f0000-0000-7000-8000-000000000012",
		"018f0000-0000-7000-8000-000000000013",
		"018f0000-0000-7000-8000-000000000014",
		"018f0000-0000-7000-8000-000000000015",
		"018f0000-0000-7000-8000-000000000016",
		"018f0000-0000-7000-8000-000000000017",
	}
	for index, table := range []string{"projects", "narrative_leaf_nodes", "sequences", "captions", "clips", "edit_transactions"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO `+table+` (id) VALUES (?)`, ids[index]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO caption_alignments VALUES (?, ?, ?, 3, ?, ?, 4, 0, 1, 2, 1, 5, 'stale', ?)`,
		ids[6], ids[0], ids[1], ids[2], ids[3], ids[5]); err != nil {
		t.Fatal(err)
	}
	migrationSQL, err := os.ReadFile(filepath.Join("..", "repository", "migrations", "0024_unified_alignments.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, string(migrationSQL)); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var revision, nodeRevision, targetRevision uint64
	var status, kind, captionID string
	if err := db.QueryRowContext(ctx, `
SELECT a.revision, a.narrative_node_revision, a.status, t.kind, t.caption_id, t.entity_revision
FROM alignments a JOIN alignment_targets t ON t.alignment_id = a.id WHERE a.id = ?`, ids[6]).Scan(
		&revision, &nodeRevision, &status, &kind, &captionID, &targetRevision,
	); err != nil {
		t.Fatal(err)
	}
	if revision != 5 || nodeRevision != 3 || status != "stale" || kind != "caption" ||
		captionID != ids[3] || targetRevision != 4 {
		t.Fatalf("alignment=%d/%d/%s target=%s/%s/%d", revision, nodeRevision, status, kind, captionID, targetRevision)
	}
}

func TestClipMutationMigrationRejectsDegenerateLiveLinkGroup(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	db, err := sqliteDriver.Open(t.TempDir()+"/invalid-group.db", func(connection *sqlite3.Conn) error {
		return connection.Exec(`PRAGMA foreign_keys = ON;`)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
CREATE TABLE clip_link_groups (id TEXT PRIMARY KEY, tombstoned INTEGER NOT NULL);
CREATE TABLE clips (id TEXT PRIMARY KEY, link_group_id TEXT, tombstoned INTEGER NOT NULL);
INSERT INTO clip_link_groups VALUES ('group', 0);
INSERT INTO clips VALUES ('only-member', 'group', 0);`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := editmigration.RewriteEditJournalSchemaV2ToV3(ctx, tx); err == nil {
		tx.Rollback()
		t.Fatal("degenerate live LinkGroup was accepted")
	}
	_ = tx.Rollback()
}
