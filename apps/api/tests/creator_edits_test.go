package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestCreatorEditsCommitConflictUndoRedoAndHTTPShareOneAtomicKernel(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, projectReads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	request, _ := domain.ParseRequestID("gesture:creator-writing-project")
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: request, Name: "Creator writing",
	})
	if err != nil {
		t.Fatal(err)
	}
	creatorCtx := creatorContext(t)
	local, _ := domain.ParseLocalID("opening")
	language := domain.CaptionLanguage("und")
	purpose := domain.AuthoredTextSpoken
	original := "Start with the human problem."
	insertRequest, _ := domain.ParseRequestID("gesture:creator-writing-insert")
	insertInput := application.EditProposeInput{
		RequestID: insertRequest, Intent: "Write the opening paragraph",
		BaseProjectRevision: created.Project.Project.Revision,
		Preconditions: []domain.EntityPrecondition{{
			Kind: domain.EntityNarrativeNode, ID: created.Project.NarrativeRootNodeID.String(), Revision: 1,
		}},
		Operations: []application.EditOperationInput{{
			Type: domain.EditInsertAuthoredText, CreateAs: &local,
			ParentID: &created.Project.NarrativeRootNodeID, AuthoredTextPurpose: &purpose,
			Language: &language, Text: &original,
		}},
	}
	inserted, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		insertInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	if inserted.Proposal.Actor.Kind != domain.ActorCreator || inserted.Proposal.RunID != nil ||
		inserted.Proposal.TurnID != nil || inserted.Transaction.CommittedProjectRevision.Value() != 2 {
		t.Fatalf("inserted=%+v", inserted)
	}
	replayed, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID, insertInput,
	)
	if err != nil || !replayed.Replayed || replayed.Transaction.ID != inserted.Transaction.ID {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	nodeValue := allocationID(t, inserted.Proposal, local)
	nodeID, _ := domain.ParseNarrativeNodeID(nodeValue)
	updatedText := "Start with the human problem, then reveal the product."
	updateRequest, _ := domain.ParseRequestID("gesture:creator-writing-update")
	updated, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		creatorAuthoredUpdate(updateRequest, 2, nodeID, 1, purpose, language, updatedText),
	)
	if err != nil || updated.Transaction.CommittedProjectRevision.Value() != 3 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}

	conflictRequest, _ := domain.ParseRequestID("gesture:creator-writing-conflict")
	_, err = edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		creatorAuthoredUpdate(conflictRequest, 2, nodeID, 1, purpose, language, "Stale replacement"),
	)
	if !errors.Is(err, application.ErrEditConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	assertCreatorEditJournalCounts(t, store.Path(), 2, 2)

	undoRequest, _ := domain.ParseRequestID("gesture:creator-writing-undo")
	undone, err := edits.UndoForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		updated.Transaction.ID, application.EditUndoInput{RequestID: undoRequest},
	)
	if err != nil || undone.Transaction.UndoesTransactionID == nil ||
		*undone.Transaction.UndoesTransactionID != updated.Transaction.ID {
		t.Fatalf("undone=%+v err=%v", undone, err)
	}
	redoRequest, _ := domain.ParseRequestID("gesture:creator-writing-redo")
	redone, err := edits.UndoForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		undone.Transaction.ID, application.EditUndoInput{RequestID: redoRequest},
	)
	if err != nil || redone.Transaction.CommittedProjectRevision.Value() != 5 {
		t.Fatalf("redone=%+v err=%v", redone, err)
	}
	recent, err := editReads.HistoryForCreator(creatorCtx, created.Project.Project.ID, 0, 2)
	if err != nil || len(recent.Transactions) != 2 ||
		recent.Transactions[0].ID != redone.Transaction.ID ||
		recent.Transactions[1].ID != undone.Transaction.ID ||
		recent.Transactions[0].Actor != domain.ActorCreator || recent.NextBefore.Value() != 4 {
		t.Fatalf("recent=%+v err=%v", recent, err)
	}
	older, err := editReads.HistoryForCreator(creatorCtx, created.Project.Project.ID, recent.NextBefore, 2)
	if err != nil || len(older.Transactions) != 2 || older.Transactions[0].ID != updated.Transaction.ID ||
		older.Transactions[1].ID != inserted.Transaction.ID || older.NextBefore.Value() != 0 {
		t.Fatalf("older=%+v err=%v", older, err)
	}
	page, err := editReads.NarrativeSubtree(
		creatorCtx, created.Project.Project.ID, created.Project.Project.NarrativeDocumentID,
		created.Project.NarrativeRootNodeID, "", 50,
	)
	if err != nil || len(page.Nodes) != 1 || page.Nodes[0].AuthoredText == nil ||
		page.Nodes[0].AuthoredText.Text != updatedText || page.Nodes[0].AuthoredText.Revision.Value() != 4 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	assertCreatorEditJournalCounts(t, store.Path(), 4, 4)

	media, assetReads, sourceAccess := testMediaApplications(t, store)
	authority, _ := application.AuthorityFromContext(creatorCtx)
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, projectReads, activity, runs,
		edits, editReads, media, assetReads, sourceAccess, nil, nil, nil, nil, nil,
		fixedAuthorizer{authority: authority},
	)
	server := httptest.NewServer(mux)
	defer server.Close()
	httpRequestID, _ := domain.ParseRequestID("gesture:creator-writing-http")
	httpInput := creatorAuthoredUpdate(httpRequestID, 5, nodeID, 4, purpose, language, "A direct HTTP checkpoint.")
	body, _ := json.Marshal(httpInput)
	response, err := server.Client().Post(
		server.URL+"/v1/projects/"+created.Project.Project.ID.String()+"/sequences/"+
			created.Project.Project.MainSequenceID.String()+"/edits",
		"application/json", strings.NewReader(string(body)),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var committed application.EditCommitResult
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&committed) != nil ||
		committed.Proposal.Actor.Kind != domain.ActorCreator || committed.Transaction.CommittedProjectRevision.Value() != 6 {
		t.Fatalf("HTTP status=%d committed=%+v", response.StatusCode, committed)
	}
}

func TestCreatorParagraphSplitAndMergeCommitAsAtomicTransactions(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, _, _, _ := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	request, _ := domain.ParseRequestID("gesture:creator-structure-project")
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: request, Name: "Creator structure",
	})
	if err != nil {
		t.Fatal(err)
	}
	creatorCtx := creatorContext(t)
	language := domain.CaptionLanguage("und")
	purpose := domain.AuthoredTextSpoken
	original := "Start with the human problem."
	opening, _ := domain.ParseLocalID("opening")
	insertRequest, _ := domain.ParseRequestID("gesture:creator-structure-insert")
	inserted, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: insertRequest, Intent: "Write one paragraph",
			BaseProjectRevision: created.Project.Project.Revision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityNarrativeNode, ID: created.Project.NarrativeRootNodeID.String(), Revision: 1,
			}},
			Operations: []application.EditOperationInput{{
				Type: domain.EditInsertAuthoredText, CreateAs: &opening,
				ParentID: &created.Project.NarrativeRootNodeID, AuthoredTextPurpose: &purpose,
				Language: &language, Text: &original,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	leftID, _ := domain.ParseNarrativeNodeID(allocationID(t, inserted.Proposal, opening))
	beforeSplit, err := editReads.NarrativeSubtree(
		creatorCtx, created.Project.Project.ID, created.Project.Project.NarrativeDocumentID,
		created.Project.NarrativeRootNodeID, "", 50,
	)
	if err != nil {
		t.Fatal(err)
	}
	rightLocal, _ := domain.ParseLocalID("right_half")
	leftText, rightText := "Start with ", "the human problem."
	splitRequest, _ := domain.ParseRequestID("gesture:creator-structure-split")
	split, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: splitRequest, Intent: "Split the paragraph",
			BaseProjectRevision: inserted.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityNarrativeNode, ID: leftID.String(), Revision: 1},
				{Kind: domain.EntityNarrativeNode, ID: beforeSplit.Parent.ID.String(), Revision: beforeSplit.Parent.Revision},
			},
			Operations: []application.EditOperationInput{
				{
					Type: domain.EditUpdateAuthoredText, NodeID: &leftID, AuthoredTextPurpose: &purpose,
					Language: &language, Text: &leftText,
				},
				{
					Type: domain.EditInsertAuthoredText, CreateAs: &rightLocal,
					ParentID: &created.Project.NarrativeRootNodeID,
					After:    &application.EditReference{ID: leftID.String()}, AuthoredTextPurpose: &purpose,
					Language: &language, Text: &rightText,
				},
			},
		},
	)
	if err != nil || len(split.Transaction.Operations) != 2 || split.Transaction.CommittedProjectRevision.Value() != 3 {
		t.Fatalf("split=%+v err=%v", split, err)
	}
	rightID, _ := domain.ParseNarrativeNodeID(allocationID(t, split.Proposal, rightLocal))
	afterSplit, err := editReads.NarrativeSubtree(
		creatorCtx, created.Project.Project.ID, created.Project.Project.NarrativeDocumentID,
		created.Project.NarrativeRootNodeID, "", 50,
	)
	if err != nil || len(afterSplit.Nodes) != 2 || afterSplit.Nodes[0].AuthoredText.Text != leftText ||
		afterSplit.Nodes[1].AuthoredText.Text != rightText {
		t.Fatalf("afterSplit=%+v err=%v", afterSplit, err)
	}
	mergeRequest, _ := domain.ParseRequestID("gesture:creator-structure-merge")
	merged, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mergeRequest, Intent: "Merge the paragraphs",
			BaseProjectRevision: split.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityNarrativeNode, ID: leftID.String(), Revision: 2},
				{Kind: domain.EntityNarrativeNode, ID: rightID.String(), Revision: 1},
				{Kind: domain.EntityNarrativeNode, ID: afterSplit.Parent.ID.String(), Revision: afterSplit.Parent.Revision},
			},
			Operations: []application.EditOperationInput{
				{
					Type: domain.EditUpdateAuthoredText, NodeID: &leftID, AuthoredTextPurpose: &purpose,
					Language: &language, Text: &original,
				},
				{Type: domain.EditRemoveNarrativeNode, NodeID: &rightID},
			},
		},
	)
	if err != nil || len(merged.Transaction.Operations) != 2 || merged.Transaction.CommittedProjectRevision.Value() != 4 {
		t.Fatalf("merged=%+v err=%v", merged, err)
	}
	afterMerge, err := editReads.NarrativeSubtree(
		creatorCtx, created.Project.Project.ID, created.Project.Project.NarrativeDocumentID,
		created.Project.NarrativeRootNodeID, "", 50,
	)
	if err != nil || len(afterMerge.Nodes) != 1 || afterMerge.Nodes[0].AuthoredText == nil ||
		afterMerge.Nodes[0].AuthoredText.Text != original {
		t.Fatalf("afterMerge=%+v err=%v", afterMerge, err)
	}
	assertCreatorEditJournalCounts(t, store.Path(), 3, 3)
}

func TestCreatorSectionWritingUsesExactParentRevisionsAndRejectsNonEmptyRemoval(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, _, _, _ := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	request, _ := domain.ParseRequestID("gesture:creator-section-project")
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: request, Name: "Creator sections",
	})
	if err != nil {
		t.Fatal(err)
	}
	creatorCtx := creatorContext(t)
	language := domain.CaptionLanguage("en-US")
	sectionLocal, _ := domain.ParseLocalID("problem")
	sectionTitle := "The human problem"
	insertRequest, _ := domain.ParseRequestID("gesture:creator-section-insert")
	inserted, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: insertRequest, Intent: "Create an explicit section",
			BaseProjectRevision: created.Project.Project.Revision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityNarrativeNode, ID: created.Project.NarrativeRootNodeID.String(), Revision: 1,
			}},
			Operations: []application.EditOperationInput{{
				Type: domain.EditInsertSection, CreateAs: &sectionLocal,
				ParentID: &created.Project.NarrativeRootNodeID, Title: &sectionTitle, Language: &language,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	sectionID, _ := domain.ParseNarrativeNodeID(allocationID(t, inserted.Proposal, sectionLocal))
	renamedTitle := "Why this matters"
	renameRequest, _ := domain.ParseRequestID("gesture:creator-section-rename")
	renamed, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: renameRequest, Intent: "Rename the section",
			BaseProjectRevision: inserted.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityNarrativeNode, ID: sectionID.String(), Revision: 1,
			}},
			Operations: []application.EditOperationInput{{
				Type: domain.EditUpdateSection, NodeID: &sectionID, Title: &renamedTitle, Language: &language,
			}},
		},
	)
	if err != nil || renamed.Transaction.CommittedProjectRevision.Value() != 3 {
		t.Fatalf("renamed=%+v err=%v", renamed, err)
	}
	childLocal, _ := domain.ParseLocalID("first_paragraph")
	childText := "Begin with evidence."
	purpose := domain.AuthoredTextSpoken
	childRequest, _ := domain.ParseRequestID("gesture:creator-section-child")
	child, err := edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: childRequest, Intent: "Write inside the section",
			BaseProjectRevision: renamed.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityNarrativeNode, ID: sectionID.String(), Revision: 2,
			}},
			Operations: []application.EditOperationInput{{
				Type: domain.EditInsertAuthoredText, CreateAs: &childLocal, ParentID: &sectionID,
				AuthoredTextPurpose: &purpose, Language: &language, Text: &childText,
			}},
		},
	)
	if err != nil || child.Transaction.CommittedProjectRevision.Value() != 4 {
		t.Fatalf("child=%+v err=%v", child, err)
	}
	childPage, err := editReads.NarrativeSubtree(
		creatorCtx, created.Project.Project.ID, created.Project.Project.NarrativeDocumentID, sectionID, "", 50,
	)
	if err != nil || childPage.Parent.Title != renamedTitle || childPage.Parent.Revision.Value() != 3 ||
		len(childPage.Nodes) != 1 || childPage.Nodes[0].AuthoredText == nil ||
		childPage.Nodes[0].AuthoredText.Text != childText {
		t.Fatalf("childPage=%+v err=%v", childPage, err)
	}
	removeRequest, _ := domain.ParseRequestID("gesture:creator-section-remove-nonempty")
	_, err = edits.CommitForCreator(
		creatorCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: removeRequest, Intent: "Remove a non-empty section",
			BaseProjectRevision: child.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityNarrativeNode, ID: sectionID.String(), Revision: 3},
				{Kind: domain.EntityNarrativeNode, ID: created.Project.NarrativeRootNodeID.String(), Revision: 2},
			},
			Operations: []application.EditOperationInput{{Type: domain.EditRemoveNarrativeNode, NodeID: &sectionID}},
		},
	)
	if !errors.Is(err, application.ErrEditInvalid) {
		t.Fatalf("non-empty Section removal err=%v", err)
	}
	assertCreatorEditJournalCounts(t, store.Path(), 3, 3)
}

func creatorAuthoredUpdate(
	requestID domain.RequestID,
	projectRevision uint64,
	nodeID domain.NarrativeNodeID,
	nodeRevision uint64,
	purpose domain.AuthoredTextPurpose,
	language domain.CaptionLanguage,
	text string,
) application.EditProposeInput {
	return application.EditProposeInput{
		RequestID: requestID, Intent: "Update the opening paragraph",
		BaseProjectRevision: domain.Revision(projectRevision),
		Preconditions: []domain.EntityPrecondition{{
			Kind: domain.EntityNarrativeNode, ID: nodeID.String(), Revision: domain.Revision(nodeRevision),
		}},
		Operations: []application.EditOperationInput{{
			Type: domain.EditUpdateAuthoredText, NodeID: &nodeID, AuthoredTextPurpose: &purpose,
			Language: &language, Text: &text,
		}},
	}
}

func assertCreatorEditJournalCounts(
	t *testing.T,
	databasePath string,
	wantRequests int,
	wantTransactions int,
) {
	t.Helper()
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var requests, transactions, agentTransactions, nonNullRunTurns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edit_request_identities WHERE actor_kind = 'creator'`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
SELECT COUNT(*) FROM edit_transactions
WHERE id IN (
  SELECT transaction_id FROM edit_request_identities
  WHERE actor_kind = 'creator' AND transaction_id IS NOT NULL
)`).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_run_transactions`).Scan(&agentTransactions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
SELECT COUNT(*) FROM edit_request_identities
WHERE actor_kind = 'creator' AND (run_id IS NOT NULL OR turn_id IS NOT NULL)`).Scan(&nonNullRunTurns); err != nil {
		t.Fatal(err)
	}
	if requests != wantRequests || transactions != wantTransactions || agentTransactions != 0 || nonNullRunTurns != 0 {
		t.Fatalf("requests=%d transactions=%d agent=%d creatorRunTurns=%d",
			requests, transactions, agentTransactions, nonNullRunTurns)
	}
}
