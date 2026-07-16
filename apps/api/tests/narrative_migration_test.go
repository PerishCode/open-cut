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

func TestCommonNarrativeNodeSQLMigrationPreservesTypedTreeAndAlignment(t *testing.T) {
	ctx := context.Background()
	db := openMigrationDatabase(t, "narrative-projection.db")

	legacySchema := `
CREATE TABLE projects (id TEXT PRIMARY KEY);
CREATE TABLE edit_transactions (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, project_revision INTEGER NOT NULL);
CREATE TABLE assets (id TEXT PRIMARY KEY);
CREATE TABLE transcript_artifacts (artifact_id TEXT PRIMARY KEY);
CREATE TABLE source_streams (id TEXT PRIMARY KEY);
CREATE TABLE transcript_segments (id TEXT PRIMARY KEY);
CREATE TABLE transcript_corrections (id TEXT PRIMARY KEY);
CREATE TABLE sequences (id TEXT PRIMARY KEY);
CREATE TABLE captions (id TEXT PRIMARY KEY);
CREATE TABLE clips (id TEXT PRIMARY KEY);
CREATE TABLE narrative_documents (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, revision INTEGER NOT NULL,
  kind TEXT NOT NULL, root_node_id TEXT NOT NULL
);
CREATE TABLE narrative_nodes (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, document_id TEXT NOT NULL,
  parent_id TEXT, revision INTEGER NOT NULL, kind TEXT NOT NULL,
  title TEXT NOT NULL, order_key TEXT NOT NULL
);
CREATE TABLE narrative_leaf_nodes (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, document_id TEXT NOT NULL,
  parent_id TEXT NOT NULL, revision INTEGER NOT NULL, kind TEXT NOT NULL,
  order_index INTEGER NOT NULL, tombstoned INTEGER NOT NULL, last_transaction_id TEXT NOT NULL
);
CREATE TABLE narrative_authored_text_values (id TEXT PRIMARY KEY, text TEXT NOT NULL);
CREATE TABLE narrative_source_excerpt_values (
  id TEXT PRIMARY KEY, asset_id TEXT NOT NULL, accepted_fingerprint TEXT NOT NULL,
  source_start_value INTEGER NOT NULL, source_start_scale INTEGER NOT NULL,
  source_duration_value INTEGER NOT NULL, source_duration_scale INTEGER NOT NULL,
  language TEXT NOT NULL, effective_text TEXT NOT NULL,
  transcript_artifact_id TEXT NOT NULL, source_stream_id TEXT NOT NULL
);
CREATE TABLE narrative_source_excerpt_segments (
  node_id TEXT NOT NULL, ordinal INTEGER NOT NULL, segment_id TEXT NOT NULL,
  PRIMARY KEY (node_id, ordinal)
);
CREATE TABLE narrative_source_excerpt_corrections (
  node_id TEXT NOT NULL, ordinal INTEGER NOT NULL, correction_id TEXT NOT NULL,
  correction_revision INTEGER NOT NULL, PRIMARY KEY (node_id, ordinal)
);
CREATE TABLE alignments (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, narrative_node_id TEXT NOT NULL,
  narrative_node_revision INTEGER NOT NULL, sequence_id TEXT NOT NULL,
  revision INTEGER NOT NULL, status TEXT NOT NULL, last_transaction_id TEXT NOT NULL
);
CREATE TABLE alignment_targets (
  alignment_id TEXT NOT NULL, ordinal INTEGER NOT NULL, kind TEXT NOT NULL,
  caption_id TEXT, clip_id TEXT, entity_revision INTEGER,
  local_start_value INTEGER, local_start_scale INTEGER,
  local_duration_value INTEGER, local_duration_scale INTEGER,
  timeline_start_value INTEGER, timeline_start_scale INTEGER,
  timeline_duration_value INTEGER, timeline_duration_scale INTEGER,
  sequence_revision INTEGER, PRIMARY KEY (alignment_id, ordinal)
);`
	if _, err := db.ExecContext(ctx, legacySchema); err != nil {
		t.Fatal(err)
	}
	ids := []string{
		"018f0000-0000-7000-8000-000000000101", // project
		"018f0000-0000-7000-8000-000000000102", // transaction
		"018f0000-0000-7000-8000-000000000103", // document
		"018f0000-0000-7000-8000-000000000104", // root
		"018f0000-0000-7000-8000-000000000105", // authored
		"018f0000-0000-7000-8000-000000000106", // excerpt
		"018f0000-0000-7000-8000-000000000107", // asset
		"018f0000-0000-7000-8000-000000000108", // artifact
		"018f0000-0000-7000-8000-000000000109", // stream
		"018f0000-0000-7000-8000-00000000010a", // segment
		"018f0000-0000-7000-8000-00000000010b", // correction
		"018f0000-0000-7000-8000-00000000010c", // sequence
		"018f0000-0000-7000-8000-00000000010d", // caption
		"018f0000-0000-7000-8000-00000000010e", // alignment
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects VALUES (?)`, []any{ids[0]}},
		{`INSERT INTO edit_transactions VALUES (?, ?, 1)`, []any{ids[1], ids[0]}},
		{`INSERT INTO assets VALUES (?)`, []any{ids[6]}},
		{`INSERT INTO transcript_artifacts VALUES (?)`, []any{ids[7]}},
		{`INSERT INTO source_streams VALUES (?)`, []any{ids[8]}},
		{`INSERT INTO transcript_segments VALUES (?)`, []any{ids[9]}},
		{`INSERT INTO transcript_corrections VALUES (?)`, []any{ids[10]}},
		{`INSERT INTO sequences VALUES (?)`, []any{ids[11]}},
		{`INSERT INTO captions VALUES (?)`, []any{ids[12]}},
		{`INSERT INTO narrative_documents VALUES (?, ?, 3, 'paper-edit', ?)`, []any{ids[2], ids[0], ids[3]}},
		{`INSERT INTO narrative_nodes VALUES (?, ?, ?, NULL, 2, 'section', 'Story', 'root')`, []any{ids[3], ids[0], ids[2]}},
		{`INSERT INTO narrative_leaf_nodes VALUES (?, ?, ?, ?, 4, 'authored-text', 0, 0, ?)`, []any{ids[4], ids[0], ids[2], ids[3], ids[1]}},
		{`INSERT INTO narrative_authored_text_values VALUES (?, 'Opening promise.')`, []any{ids[4]}},
		{`INSERT INTO narrative_leaf_nodes VALUES (?, ?, ?, ?, 5, 'source-excerpt', 1, 0, ?)`, []any{ids[5], ids[0], ids[2], ids[3], ids[1]}},
		{`INSERT INTO narrative_source_excerpt_values VALUES (?, ?, ?, 0, 1, 2, 1, 'en', 'Source truth.', ?, ?)`, []any{ids[5], ids[6], "sha256:" + strings.Repeat("a", 64), ids[7], ids[8]}},
		{`INSERT INTO narrative_source_excerpt_segments VALUES (?, 0, ?)`, []any{ids[5], ids[9]}},
		{`INSERT INTO narrative_source_excerpt_corrections VALUES (?, 0, ?, 7)`, []any{ids[5], ids[10]}},
		{`INSERT INTO alignments VALUES (?, ?, ?, 5, ?, 6, 'exact', ?)`, []any{ids[13], ids[0], ids[5], ids[11], ids[1]}},
		{`INSERT INTO alignment_targets VALUES (?, 0, 'caption', ?, NULL, 8, 0, 1, 2, 1, NULL, NULL, NULL, NULL, NULL)`, []any{ids[13], ids[12]}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed %q: %v", statement.query, err)
		}
	}

	migrationSQL, err := os.ReadFile(filepath.Join("..", "repository", "migrations", "0027_common_narrative_nodes.sql"))
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

	var rootKind, rootTitle, rootLanguage string
	if err := db.QueryRowContext(ctx, `
SELECT n.kind, value.title, value.language
FROM narrative_nodes n JOIN narrative_section_values value ON value.id = n.id
WHERE n.id = ?`, ids[3]).Scan(&rootKind, &rootTitle, &rootLanguage); err != nil {
		t.Fatal(err)
	}
	var authoredKind, authoredPurpose, authoredLanguage, authoredText string
	if err := db.QueryRowContext(ctx, `
SELECT n.kind, value.purpose, value.language, value.text
FROM narrative_nodes n JOIN narrative_authored_text_values value ON value.id = n.id
WHERE n.id = ?`, ids[4]).Scan(&authoredKind, &authoredPurpose, &authoredLanguage, &authoredText); err != nil {
		t.Fatal(err)
	}
	var excerptKind, effectiveText, segmentID, correctionID string
	var correctionRevision uint64
	if err := db.QueryRowContext(ctx, `
SELECT n.kind, value.effective_text, segment.segment_id, correction.correction_id, correction.correction_revision
FROM narrative_nodes n
JOIN narrative_source_excerpt_values value ON value.id = n.id
JOIN narrative_source_excerpt_segments segment ON segment.node_id = n.id AND segment.ordinal = 0
JOIN narrative_source_excerpt_corrections correction ON correction.node_id = n.id AND correction.ordinal = 0
WHERE n.id = ?`, ids[5]).Scan(
		&excerptKind, &effectiveText, &segmentID, &correctionID, &correctionRevision,
	); err != nil {
		t.Fatal(err)
	}
	if rootKind != "section" || rootTitle != "Story" || rootLanguage != "und" ||
		authoredKind != "authored-text" || authoredPurpose != "spoken" || authoredLanguage != "und" ||
		authoredText != "Opening promise." || excerptKind != "source-excerpt" || effectiveText != "Source truth." ||
		segmentID != ids[9] || correctionID != ids[10] || correctionRevision != 7 {
		t.Fatalf("migrated narrative values were not preserved")
	}
	var alignedNode string
	if err := db.QueryRowContext(ctx, `SELECT narrative_node_id FROM alignments WHERE id = ?`, ids[13]).Scan(&alignedNode); err != nil || alignedNode != ids[5] {
		t.Fatalf("alignment node=%s err=%v", alignedNode, err)
	}
	var legacyTableCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = 'narrative_leaf_nodes'`).Scan(&legacyTableCount); err != nil || legacyTableCount != 0 {
		t.Fatalf("legacy leaf table count=%d err=%v", legacyTableCount, err)
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("common Narrative migration left a foreign-key violation")
	}
}

func TestNarrativeJournalV5MigrationWrapsLegacyPayloadsAndRecomputesDigests(t *testing.T) {
	ctx := context.Background()
	db := openMigrationDatabase(t, "narrative-journal.db")
	if _, err := db.ExecContext(ctx, `
CREATE TABLE edit_proposals (
  id TEXT PRIMARY KEY, schema_version TEXT NOT NULL, digest TEXT NOT NULL,
  canonical_json TEXT NOT NULL, operations_json TEXT NOT NULL, inverse_preview_json TEXT NOT NULL
);
CREATE TABLE activity_outbox (kind TEXT NOT NULL, outcome_kind TEXT, outcome_id TEXT, payload_json TEXT NOT NULL);
CREATE TABLE edit_transactions (
  id TEXT PRIMARY KEY, proposal_id TEXT NOT NULL, project_id TEXT NOT NULL,
  actor_kind TEXT NOT NULL, actor_id TEXT NOT NULL, intent TEXT NOT NULL,
  operation_json TEXT NOT NULL, inverse_json TEXT NOT NULL, changes_json TEXT NOT NULL,
  project_revision INTEGER NOT NULL, undoes_transaction_id TEXT,
  schema_version TEXT NOT NULL, digest TEXT NOT NULL
);`); err != nil {
		t.Fatal(err)
	}
	ids := []string{
		"018f0000-0000-7000-8000-000000000201", "018f0000-0000-7000-8000-000000000202",
		"018f0000-0000-7000-8000-000000000203", "018f0000-0000-7000-8000-000000000204",
		"018f0000-0000-7000-8000-000000000205", "018f0000-0000-7000-8000-000000000206",
		"018f0000-0000-7000-8000-000000000207", "018f0000-0000-7000-8000-000000000208",
	}
	authored := map[string]any{
		"id": ids[4], "revision": "3", "documentId": ids[6], "parentId": ids[7],
		"text": "Legacy prose.", "tombstoned": false,
	}
	source := map[string]any{
		"id": ids[5], "revision": "4", "documentId": ids[6], "parentId": ids[7],
		"assetId": ids[4], "acceptedFingerprint": "sha256:" + strings.Repeat("b", 64),
		"sourceRange": map[string]any{
			"start": map[string]any{"value": "0", "scale": 1}, "duration": map[string]any{"value": "1", "scale": 1},
		},
		"language": "en", "effectiveText": "Evidence.",
		"evidence": map[string]any{
			"artifactId": ids[0], "sourceStreamId": ids[1], "segmentIds": []any{ids[2]}, "correctionRevisions": []any{},
		},
		"tombstoned": false,
	}
	operations := []any{
		map[string]any{"type": "put-authored-text", "authoredText": authored},
		map[string]any{"type": "put-source-excerpt", "sourceExcerpt": source},
	}
	operationsJSON, _ := json.Marshal(operations)
	payload := map[string]any{
		"actor": map[string]any{"kind": "agent", "agentId": ids[3]}, "operations": operations, "inverse": operations,
	}
	canonical, oldDigest, err := domain.CanonicalDigest("open-cut/edit-proposal", "open-cut/edit-proposal/v4", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO edit_proposals VALUES (?, 'open-cut/edit-proposal/v4', ?, ?, ?, ?)`,
		ids[0], oldDigest.String(), string(canonical), string(operationsJSON), string(operationsJSON)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO activity_outbox VALUES ('edit.proposed', 'proposal', ?, json_object('proposalDigest', ?))`, ids[0], oldDigest.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO edit_transactions VALUES (?, ?, ?, 'agent', ?, 'legacy Narrative', ?, ?, '[]', 5, NULL,
  'open-cut/edit-transaction/v4', ?)`, ids[1], ids[0], ids[2], ids[3], string(operationsJSON),
		string(operationsJSON), "sha256:"+strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := editmigration.RewriteEditJournalSchemaV4ToV5(ctx, tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var schema, digest, migratedJSON, activityJSON string
	if err := db.QueryRowContext(ctx, `SELECT schema_version, digest, operations_json FROM edit_proposals WHERE id = ?`, ids[0]).Scan(&schema, &digest, &migratedJSON); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT payload_json FROM activity_outbox WHERE outcome_id = ?`, ids[0]).Scan(&activityJSON); err != nil {
		t.Fatal(err)
	}
	var migrated []domain.NormalizedEditOperation
	if err := json.Unmarshal([]byte(migratedJSON), &migrated); err != nil {
		t.Fatal(err)
	}
	if schema != domain.EditProposalSchema || digest == oldDigest.String() || !strings.Contains(activityJSON, digest) ||
		len(migrated) != 2 || migrated[0].Type != domain.NormalizedPutNarrativeNode ||
		migrated[0].NarrativeNode == nil || migrated[0].NarrativeNode.AuthoredText == nil ||
		migrated[0].NarrativeNode.AuthoredText.Purpose != domain.AuthoredTextSpoken ||
		migrated[0].NarrativeNode.AuthoredText.Language.String() != "und" ||
		migrated[1].NarrativeNode == nil || migrated[1].NarrativeNode.SourceExcerpt == nil {
		t.Fatalf("schema=%s digest=%s migrated=%+v activity=%s", schema, digest, migrated, activityJSON)
	}
	var transactionSchema, transactionDigest string
	if err := db.QueryRowContext(ctx, `SELECT schema_version, digest FROM edit_transactions WHERE id = ?`, ids[1]).Scan(&transactionSchema, &transactionDigest); err != nil {
		t.Fatal(err)
	}
	if transactionSchema != domain.EditTransactionSchema || transactionDigest == "sha256:"+strings.Repeat("0", 64) {
		t.Fatalf("transaction schema=%s digest=%s", transactionSchema, transactionDigest)
	}
}

func openMigrationDatabase(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := sqliteDriver.Open(filepath.Join(t.TempDir(), name), func(connection *sqlite3.Conn) error {
		return connection.Exec(`PRAGMA foreign_keys = ON;`)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
