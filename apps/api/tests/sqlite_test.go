package tests

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSQLiteProjectGenesisIsAtomicExactAndIdempotentAcrossRestart(t *testing.T) {
	ctx := creatorContext(t)
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dataDir, "database", "open-cut.db"); store.Path() != want {
		t.Fatalf("database path = %q, want %q", store.Path(), want)
	}
	projects, reads, activity, _ := testProjectApplications(t, store)
	requestID, _ := domain.ParseRequestID("gesture:create-project:sqlite-001")
	first, err := projects.Create(ctx, application.CreateProjectInput{RequestID: requestID, Name: "SQLite story"})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	replayed, err := projects.Create(ctx, application.CreateProjectInput{RequestID: requestID, Name: "SQLite story"})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Project.Project.ID != first.Project.Project.ID ||
		replayed.TransactionID != first.TransactionID || replayed.ProjectActivityCursor.String() != "1" {
		store.Close()
		t.Fatalf("first=%+v replayed=%+v", first, replayed)
	}
	if _, err := projects.Create(ctx, application.CreateProjectInput{RequestID: requestID, Name: "Different"}); !errors.Is(err, application.ErrRequestIdentityReused) {
		store.Close()
		t.Fatalf("request identity reuse error = %v", err)
	}
	workspaceActivity, err := activity.List(ctx, application.ListActivityInput{})
	if err != nil || len(workspaceActivity.Events) != 1 ||
		workspaceActivity.Events[0].Kind != "workspace.project-created" || workspaceActivity.Cursor.String() != "1" {
		store.Close()
		t.Fatalf("workspace activity=%+v err=%v", workspaceActivity, err)
	}
	projectActivity, err := activity.List(ctx, application.ListActivityInput{ProjectID: &first.Project.Project.ID})
	if err != nil || len(projectActivity.Events) != 1 || projectActivity.Events[0].Kind != "project.created" ||
		len(projectActivity.Events[0].ChangedEntityRefs) != 7 || projectActivity.Cursor.String() != "1" {
		store.Close()
		t.Fatalf("project activity=%+v err=%v", projectActivity, err)
	}
	page, err := reads.List(ctx, application.ListProjectsInput{})
	if err != nil || len(page.Projects) != 1 || page.ActivityCursor.String() != "1" {
		store.Close()
		t.Fatalf("page=%+v err=%v", page, err)
	}
	databasePath := store.Path()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedReads, err := application.NewProjectReads(reopened)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := reopenedReads.Show(ctx, first.Project.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Project.Revision.String() != "1" || overview.ActivityCursor.String() != "1" ||
		overview.Format != domain.DefaultSequenceFormat() || len(overview.Tracks) != 3 {
		t.Fatalf("overview=%+v", overview)
	}
	if databasePath != reopened.Path() {
		t.Fatalf("database path changed across restart: %q -> %q", databasePath, reopened.Path())
	}
}

func TestSQLiteLocalCreatorIsSingletonAndAuthorizationAuditSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	clock := &mutableClock{now: time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)}
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	firstSessions, firstKey := newTestUISessions(t, store, clock, false)
	first := authorizeTestUI(t, firstSessions, firstKey, "electron-sqlite-1")
	databasePath := store.Path()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	clock.now = clock.now.Add(time.Minute)
	reopened, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	secondSessions, secondKey := newTestUISessions(t, reopened, clock, false)
	second := authorizeTestUI(t, secondSessions, secondKey, "electron-sqlite-2")
	if first.Actor.IDString() != second.Actor.IDString() {
		reopened.Close()
		t.Fatalf("local creator changed across restart: %s -> %s", first.Actor.IDString(), second.Actor.IDString())
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var creators, audits int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_creators`).Scan(&creators); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_audit`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if creators != 1 || audits != 4 {
		t.Fatalf("creators=%d audits=%d", creators, audits)
	}
}

func TestSQLiteCLIGrantKeepsAgentAndExactDecisionAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	createdAt := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	grantID, err := domain.GenerateUUIDv7(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	agentValue, err := domain.GenerateUUIDv7(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	agentID, err := domain.ParseAgentID(agentValue)
	if err != nil {
		t.Fatal(err)
	}
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.EnsurePendingCLIGrant(ctx, application.PendingCLIGrant{
		ID: grantID, InstallationID: "installation-cli-sqlite", AgentID: agentID,
		PublicKey: "fixture-public-key", Fingerprint: "sha256:" + strings.Repeat("a", 64),
		Scopes: []string{"project:read", "activity:read"}, CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(10 * time.Minute),
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	approved, err := store.DecideCLIGrant(ctx, pending.ID, true, createdAt.Add(time.Minute))
	if err != nil || approved.Status != application.CLIGrantActive {
		store.Close()
		t.Fatalf("approved=%+v err=%v", approved, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopened.FindCLIGrant(ctx, "installation-cli-sqlite", "fixture-public-key")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != grantID || stored.AgentID != agentID || stored.Status != application.CLIGrantActive ||
		len(stored.Scopes) != 2 || stored.DecidedAt == nil || stored.Revision.Value() != 1 || stored.ScopeDigest == "" {
		t.Fatalf("stored=%+v", stored)
	}
	repeated, err := reopened.EnsurePendingCLIGrant(ctx, application.PendingCLIGrant{
		ID: grantID, InstallationID: "installation-cli-sqlite", AgentID: agentID,
		PublicKey: "fixture-public-key", Fingerprint: "sha256:" + strings.Repeat("a", 64),
		Scopes: []string{"project:read"}, CreatedAt: createdAt.Add(2 * time.Minute),
		ExpiresAt: createdAt.Add(12 * time.Minute),
	})
	if err != nil || repeated.Status != application.CLIGrantActive || len(repeated.Scopes) != 2 {
		reopened.Close()
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
	upgradeID, err := domain.GenerateUUIDv7(createdAt.Add(3 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	upgrade, err := reopened.EnsurePendingCLIGrantScopeUpgrade(ctx, application.PendingCLIGrantScopeUpgrade{
		ID: upgradeID, GrantID: grantID, FromRevision: stored.Revision,
		RequestedScopes: []string{"run:read", "project:read", "activity:read"},
		CreatedAt:       createdAt.Add(3 * time.Minute), ExpiresAt: createdAt.Add(13 * time.Minute),
	})
	if err != nil || upgrade.Status != application.CLIGrantScopeUpgradePending ||
		upgrade.RequestedScopeDigest == "" {
		reopened.Close()
		t.Fatalf("upgrade=%+v err=%v", upgrade, err)
	}
	decidedUpgrade, upgradedGrant, err := reopened.DecideCLIGrantScopeUpgrade(
		ctx, upgrade.ID, true, createdAt.Add(4*time.Minute),
	)
	if err != nil || decidedUpgrade.Status != application.CLIGrantScopeUpgradeApproved ||
		upgradedGrant.Revision.Value() != 2 || len(upgradedGrant.Scopes) != 3 || upgradedGrant.AgentID != agentID {
		reopened.Close()
		t.Fatalf("decided upgrade=%+v grant=%+v err=%v", decidedUpgrade, upgradedGrant, err)
	}
	revoked, err := reopened.RevokeCLIGrant(ctx, grantID, createdAt.Add(5*time.Minute))
	if err != nil || revoked.Status != application.CLIGrantRevoked || revoked.RevokedAt == nil {
		reopened.Close()
		t.Fatalf("revoked=%+v err=%v", revoked, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	final, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Close()
	stored, err = final.FindCLIGrant(ctx, "installation-cli-sqlite", "fixture-public-key")
	if err != nil || stored.Status != application.CLIGrantRevoked || stored.RevokedAt == nil {
		t.Fatalf("stored revoked=%+v err=%v", stored, err)
	}
}

func TestSQLiteMigrationCutoverRemovesPlaceholderShape(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := store.Path()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var migrations int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations < 16 {
		t.Fatalf("migration count = %d", migrations)
	}
	policy, err := storePolicyFromDatabase(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Revision.Value() != 1 || policy.Policy != application.DefaultInvocationPolicy() {
		t.Fatalf("invocation policy=%+v", policy)
	}
	var placeholder int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'project_state'
`).Scan(&placeholder); err != nil {
		t.Fatal(err)
	}
	if placeholder != 0 {
		t.Fatal("placeholder project_state table survived migration 0002")
	}
	var descriptionColumns int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name = 'description'
`).Scan(&descriptionColumns); err != nil {
		t.Fatal(err)
	}
	if descriptionColumns != 0 {
		t.Fatal("placeholder project description column survived migration 0002")
	}
	var legacyMediaJobTables int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_master
WHERE type = 'table' AND name IN (
  'media_jobs', 'media_job_attempts', 'media_job_owners', 'media_job_prerequisites'
)`).Scan(&legacyMediaJobTables); err != nil {
		t.Fatal(err)
	}
	if legacyMediaJobTables != 0 {
		t.Fatal("media-only scheduler tables survived generic WorkJob cutover")
	}
	var genericWorkTables int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_master
WHERE type = 'table' AND name IN (
  'work_jobs', 'work_job_attempts', 'work_job_owners',
  'work_job_prerequisites', 'media_job_details'
)`).Scan(&genericWorkTables); err != nil {
		t.Fatal(err)
	}
	if genericWorkTables != 5 {
		t.Fatalf("generic WorkJob table count = %d", genericWorkTables)
	}
	var languageNotNull int
	var languageDefault sql.NullString
	if err := db.QueryRowContext(ctx, `
SELECT "notnull", dflt_value FROM pragma_table_info('captions') WHERE name = 'language'
`).Scan(&languageNotNull, &languageDefault); err != nil {
		t.Fatal(err)
	}
	if languageNotNull != 1 || languageDefault.Valid {
		t.Fatalf("caption language column notnull=%d default=%+v", languageNotNull, languageDefault)
	}
	var renderV4Tables int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_master
WHERE type = 'table' AND (
  (name = 'render_plans' AND sql LIKE '%open-cut/render-plan/v4%') OR
  (name = 'sequence_preview_job_details' AND sql LIKE '%open-cut/sequence-render-intent/v1%')
)
`).Scan(&renderV4Tables); err != nil {
		t.Fatal(err)
	}
	if renderV4Tables != 2 {
		t.Fatalf("render v4 table count = %d", renderV4Tables)
	}
	var renderMaterialTables int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_master
WHERE type = 'table' AND (
  (name = 'media_artifacts' AND sql LIKE '%render-input%') OR
  name = 'render_material_leases'
)
`).Scan(&renderMaterialTables); err != nil {
		t.Fatal(err)
	}
	if renderMaterialTables != 2 {
		t.Fatalf("render material table count = %d", renderMaterialTables)
	}
	var exportTables int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_master
WHERE type = 'table' AND name IN (
  'sequence_export_artifacts', 'sequence_export_job_details',
  'sequence_export_job_inputs', 'sequence_export_job_resources', 'sequence_export_requests'
)
`).Scan(&exportTables); err != nil {
		t.Fatal(err)
	}
	if exportTables != 5 {
		t.Fatalf("sequence export table count = %d", exportTables)
	}
	var exportRequestSchema string
	if err := db.QueryRowContext(ctx, `
SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'sequence_export_requests'
`).Scan(&exportRequestSchema); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exportRequestSchema, "owner_kind IN ('run', 'creator')") ||
		!strings.Contains(exportRequestSchema, "owner_kind = 'creator' AND run_id IS NULL AND turn_id IS NULL") ||
		!strings.Contains(exportRequestSchema, "'delete-artifact'") {
		t.Fatalf("sequence export ownership schema = %s", exportRequestSchema)
	}
	var exportArtifactSchema string
	if err := db.QueryRowContext(ctx, `
SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'sequence_export_artifacts'
`).Scan(&exportArtifactSchema); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exportArtifactSchema, "'valid', 'invalid', 'deleted'") {
		t.Fatalf("sequence export artifact schema = %s", exportArtifactSchema)
	}
}

func TestSQLiteProjectsRejectRewrittenMigrationHistory(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	projects, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := projects.Path()
	if err := projects.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET checksum = 'rewritten' WHERE version = 1`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := repository.OpenSQLiteProjects(ctx, dataDir); err == nil {
		reopened.Close()
		t.Fatal("rewritten migration history was accepted")
	}
}

func TestSQLiteProjectsRejectNewerMigrationHistory(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	projects, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := projects.Path()
	if err := projects.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	var latestVersion int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&latestVersion); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, checksum) VALUES (?, 'future', 'future')`, latestVersion+1); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := repository.OpenSQLiteProjects(ctx, dataDir); err == nil {
		reopened.Close()
		t.Fatal("newer migration history was accepted by the older binary")
	}
}

func storePolicyFromDatabase(ctx context.Context, db *sql.DB) (application.InvocationPolicySettings, error) {
	var revision uint64
	var output string
	var wait uint32
	if err := db.QueryRowContext(ctx, `
SELECT revision, output_mode, wait_milliseconds FROM cli_invocation_settings WHERE singleton = 1
`).Scan(&revision, &output, &wait); err != nil {
		return application.InvocationPolicySettings{}, err
	}
	parsed, err := domain.NewRevision(revision)
	if err != nil {
		return application.InvocationPolicySettings{}, err
	}
	return application.InvocationPolicySettings{
		Revision: parsed,
		Policy:   application.InvocationPolicy{Output: application.OutputMode(output), WaitMilliseconds: wait},
	}, nil
}
