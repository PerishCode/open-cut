package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestProjectVersionsCheckpointAndAtomicallyRestoreCreativeHead(t *testing.T) {
	parallelAPITest(t)
	store, err := repository.OpenSQLiteProjects(context.Background(), filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, reads, _, _ := testProjectApplications(t, store)
	edits, _ := testEditingApplications(t, store)
	versions, err := application.NewProjectVersions(store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	creator := creatorContext(t)
	createRequest, _ := domain.ParseRequestID("gesture:version-project")
	created, err := projects.Create(creator, application.CreateProjectInput{RequestID: createRequest, Name: "Versioned story"})
	if err != nil {
		t.Fatal(err)
	}

	local, _ := domain.ParseLocalID("opening")
	language := domain.CaptionLanguage("und")
	purpose := domain.AuthoredTextSpoken
	original := "Keep the recoverable opening."
	insertRequest, _ := domain.ParseRequestID("gesture:version-insert")
	inserted, err := edits.CommitForCreator(creator, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: insertRequest, Intent: "Write recoverable opening", BaseProjectRevision: 1,
			Preconditions: []domain.EntityPrecondition{{Kind: domain.EntityNarrativeNode,
				ID: created.Project.NarrativeRootNodeID.String(), Revision: 1}},
			Operations: []application.EditOperationInput{{Type: domain.EditInsertAuthoredText, CreateAs: &local,
				ParentID: &created.Project.NarrativeRootNodeID, AuthoredTextPurpose: &purpose,
				Language: &language, Text: &original}},
		})
	if err != nil {
		t.Fatal(err)
	}
	nodeID, _ := domain.ParseNarrativeNodeID(allocationID(t, inserted.Proposal, local))
	checkpointRequest, _ := domain.ParseRequestID("gesture:version-save")
	checkpoint, err := versions.Create(creator, created.Project.Project.ID,
		application.CreateProjectVersionInput{RequestID: checkpointRequest, Name: "Opening approved"})
	if err != nil || checkpoint.Version.CapturedProjectRevision.Value() != 2 || checkpoint.Version.Source != application.ProjectVersionManual {
		t.Fatalf("checkpoint=%+v err=%v", checkpoint, err)
	}

	updateRequest, _ := domain.ParseRequestID("gesture:version-damage")
	damaged, err := edits.CommitForCreator(creator, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		creatorAuthoredUpdate(updateRequest, 2, nodeID, 1, purpose, language, "An unwanted destructive rewrite."))
	if err != nil {
		t.Fatal(err)
	}
	restoreRequest, _ := domain.ParseRequestID("gesture:version-restore")
	restored, err := versions.Restore(creator, created.Project.Project.ID, checkpoint.Version.ID,
		application.RestoreProjectVersionInput{RequestID: restoreRequest,
			ExpectedProjectRevision: damaged.Transaction.CommittedProjectRevision})
	if err != nil {
		t.Fatal(err)
	}
	if restored.CommittedProjectRevision.Value() != 4 || restored.SafetyVersion.CapturedProjectRevision.Value() != 3 ||
		restored.TransactionID.IsZero() || restored.SafetyVersion.Source != application.ProjectVersionPreRestore {
		t.Fatalf("restored=%+v", restored)
	}
	overview, err := reads.Show(creator, created.Project.Project.ID)
	if err != nil || overview.Project.Revision.Value() != 4 {
		t.Fatalf("overview=%+v err=%v", overview, err)
	}
	db, err := sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var text string
	var revision uint64
	if err := db.QueryRow(`
SELECT value.text, node.revision FROM narrative_nodes node
JOIN narrative_authored_text_values value ON value.id = node.id WHERE node.id = ?`, nodeID.String()).Scan(&text, &revision); err != nil {
		t.Fatal(err)
	}
	if text != original || revision != 3 {
		t.Fatalf("restored text=%q revision=%d", text, revision)
	}
	var restoreOperations, restoreChanges string
	if err := db.QueryRow(`SELECT operation_json, changes_json FROM edit_transactions WHERE id = ?`, restored.TransactionID.String()).Scan(&restoreOperations, &restoreChanges); err != nil {
		t.Fatal(err)
	}
	if len(restoreOperations) > 512 || !containsJSONToken(restoreOperations, "restore-project-version") {
		t.Fatalf("restore journal=%s", restoreOperations)
	}
	var changes []domain.EntityRevisionChange
	if err := json.Unmarshal([]byte(restoreChanges), &changes); err != nil || len(changes) != 2 {
		t.Fatalf("restore journal must contain only aggregate changes: %s err=%v", restoreChanges, err)
	}
	page, err := versions.List(creator, created.Project.Project.ID, domain.ProjectVersionID{}, 2)
	if err != nil || len(page.Versions) != 2 || page.NextBefore == nil {
		t.Fatalf("versions=%+v err=%v", page, err)
	}
	older, err := versions.List(creator, created.Project.Project.ID, *page.NextBefore, 2)
	if err != nil || len(older.Versions) != 1 || older.NextBefore != nil ||
		older.Versions[0].ID == page.Versions[0].ID || older.Versions[0].ID == page.Versions[1].ID ||
		older.Versions[0].Source != application.ProjectVersionGenesis ||
		older.Versions[0].Retention != application.ProjectVersionPinned {
		t.Fatalf("older versions=%+v err=%v", older, err)
	}
}

func containsJSONToken(value, token string) bool {
	for index := 0; index+len(token) <= len(value); index++ {
		if value[index:index+len(token)] == token {
			return true
		}
	}
	return false
}
