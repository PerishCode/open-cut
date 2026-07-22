package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSQLiteEditProposalApplyUndoAndRedoAreAtomicDurableAndIdempotent(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	projects, projectReads, activity, runs := testProjectApplications(t, store)
	projectRequest, _ := domain.ParseRequestID("gesture:edit-kernel-project")
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: projectRequest, Name: "Atomic paper edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx := createSQLiteAgentContext(t, store)
	runRequest, _ := domain.ParseRequestID("agent:edit:run")
	run, err := runs.Begin(agentCtx, created.Project.Project.ID, application.RunBeginInput{
		RequestID: runRequest, Intent: "Write and caption the opening line",
	})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := application.NewEdits(
		store, application.UUIDv7IdentityGenerator{},
		application.ClockFunc(func() time.Time { return time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	captionTrack := captionTrackFromOverview(t, created.Project)
	zero, _ := domain.NewRationalTime(0, 1)
	two, _ := domain.NewRationalTime(2, 1)
	captionRange, _ := domain.NewTimeRange(zero, two)
	narrativeLocal, _ := domain.ParseLocalID("opening_narrative")
	captionLocal, _ := domain.ParseLocalID("opening_caption")
	alignmentLocal, _ := domain.ParseLocalID("opening_alignment")
	text := "Build the story before polishing the cut."
	language := domain.CaptionLanguage("en")
	authoredPurpose := domain.AuthoredTextSpoken
	proposeRequest, _ := domain.ParseRequestID("agent:edit:propose:001")
	input := application.EditProposeInput{
		RequestID: proposeRequest, Intent: "Add the opening authored line and executable caption",
		BaseProjectRevision: created.Project.Project.Revision,
		Preconditions: []domain.EntityPrecondition{
			{Kind: domain.EntityNarrativeNode, ID: created.Project.NarrativeRootNodeID.String(), Revision: 1},
			{Kind: domain.EntityTrack, ID: captionTrack.ID.String(), Revision: captionTrack.Revision},
		},
		Operations: []application.EditOperationInput{
			{Type: domain.EditInsertAuthoredText, CreateAs: &narrativeLocal,
				ParentID: &created.Project.NarrativeRootNodeID, AuthoredTextPurpose: &authoredPurpose,
				Language: &language, Text: &text},
			{Type: domain.EditAddCaption, CreateAs: &captionLocal,
				TrackID: &captionTrack.ID, Range: &captionRange, Language: &language, Text: &text},
			{Type: domain.EditBindAlignment, CreateAs: &alignmentLocal,
				NarrativeNode: &application.EditReference{Local: &narrativeLocal},
				AlignmentTargets: []application.AlignmentTargetInput{{
					Type: domain.AlignmentTargetCaption, Caption: &application.EditReference{Local: &captionLocal},
					LocalRange: &captionRange,
				}}},
		},
	}
	proposed, err := edits.Propose(
		agentCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if proposed.Proposal.Status != domain.ProposalOpen || len(proposed.Proposal.Allocation) != 3 ||
		len(proposed.Proposal.Operations) != 3 || proposed.ActivityCursor.Value() != 3 {
		t.Fatalf("proposed=%+v", proposed)
	}
	applyRequest, _ := domain.ParseRequestID("agent:edit:apply:001")
	applied, err := edits.Apply(
		agentCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, proposed.Proposal.ID,
		application.EditApplyInput{RequestID: applyRequest, ProposalDigest: proposed.Proposal.Digest},
	)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Transaction.CommittedProjectRevision.Value() != 2 ||
		applied.Proposal.Status != domain.ProposalApplied || applied.ActivityCursor.Value() != 4 {
		t.Fatalf("applied=%+v", applied)
	}
	editReads, err := application.NewEditReads(store)
	if err != nil {
		t.Fatal(err)
	}
	narrativePage, err := editReads.NarrativeSubtree(
		agentCtx, created.Project.Project.ID, created.Project.Project.NarrativeDocumentID,
		created.Project.NarrativeRootNodeID, "", 50,
	)
	if err != nil || len(narrativePage.Nodes) != 1 || narrativePage.Nodes[0].AuthoredText == nil ||
		narrativePage.Nodes[0].AuthoredText.Text != text ||
		narrativePage.DocumentRevision.Value() != 2 || narrativePage.ActivityCursor.Value() != 4 {
		t.Fatalf("narrative page=%+v err=%v", narrativePage, err)
	}
	sequencePage, err := editReads.SequenceWindow(
		agentCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		&captionTrack.ID, captionRange, "", 100,
	)
	if err != nil || len(sequencePage.Captions) != 1 || len(sequencePage.Alignments) != 1 ||
		sequencePage.Captions[0].Text != text || sequencePage.SequenceRevision.Value() != 2 {
		t.Fatalf("sequence page=%+v err=%v", sequencePage, err)
	}
	captionID := allocationID(t, proposed.Proposal, captionLocal)
	entity, err := editReads.Entity(agentCtx, created.Project.Project.ID, domain.EntityCaption, captionID)
	if err != nil || entity.Caption == nil || entity.Caption.Text != text || entity.ActivityCursor.Value() != 4 {
		t.Fatalf("entity=%+v err=%v", entity, err)
	}
	storedProposal, proposalCursor, err := editReads.Proposal(
		agentCtx, created.Project.Project.ID, proposed.Proposal.ID,
	)
	if err != nil || storedProposal.Status != domain.ProposalApplied || proposalCursor.Value() != 4 {
		t.Fatalf("stored proposal=%+v cursor=%v err=%v", storedProposal, proposalCursor, err)
	}
	history, err := editReads.History(agentCtx, created.Project.Project.ID, 0, 50)
	if err != nil || len(history.Transactions) != 1 || history.Transactions[0].ID != applied.Transaction.ID {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	authority, err := application.AuthorityFromContext(agentCtx)
	if err != nil {
		t.Fatal(err)
	}
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, projectReads, activity, runs,
		edits, editReads, media, assetReads, sourceAccess, nil, nil, nil, nil, nil, fixedAuthorizer{authority: authority},
	)
	server := httptest.NewServer(mux)
	defer server.Close()
	readURL := server.URL + "/v1/projects/" + created.Project.Project.ID.String() +
		"/narratives/" + created.Project.Project.NarrativeDocumentID.String() +
		"/subtree?parentId=" + url.QueryEscape(created.Project.NarrativeRootNodeID.String())
	response, err := server.Client().Get(readURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(response.Body)
	var HTTPPage application.NarrativeSubtreePage
	if response.StatusCode != http.StatusOK || readErr != nil || json.Unmarshal(responseBody, &HTTPPage) != nil ||
		len(HTTPPage.Nodes) != 1 || HTTPPage.Nodes[0].AuthoredText == nil || HTTPPage.Nodes[0].AuthoredText.Text != text {
		t.Fatalf("HTTP Narrative status=%d page=%+v body=%s", response.StatusCode, HTTPPage, responseBody)
	}
	historyURL := server.URL + "/v1/projects/" + created.Project.Project.ID.String() +
		"/edit/transactions?after=1&limit=10"
	historyResponse, err := server.Client().Get(historyURL)
	if err != nil {
		t.Fatal(err)
	}
	defer historyResponse.Body.Close()
	var HTTPHistory application.TransactionHistoryPage
	if historyResponse.StatusCode != http.StatusOK ||
		json.NewDecoder(historyResponse.Body).Decode(&HTTPHistory) != nil ||
		len(HTTPHistory.Transactions) != 1 || HTTPHistory.Transactions[0].ID != applied.Transaction.ID {
		t.Fatalf("HTTP history status=%d page=%+v", historyResponse.StatusCode, HTTPHistory)
	}
	replayedProposal, err := edits.Propose(
		agentCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, input,
	)
	if err != nil || !replayedProposal.Replayed || replayedProposal.Proposal.ID != proposed.Proposal.ID {
		t.Fatalf("proposal replay=%+v err=%v", replayedProposal, err)
	}
	replayedApply, err := edits.Apply(
		agentCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, proposed.Proposal.ID,
		application.EditApplyInput{RequestID: applyRequest, ProposalDigest: proposed.Proposal.Digest},
	)
	if err != nil || !replayedApply.Replayed || replayedApply.Transaction.ID != applied.Transaction.ID {
		t.Fatalf("apply replay=%+v err=%v", replayedApply, err)
	}
	undoRequest, _ := domain.ParseRequestID("agent:edit:undo:001")
	undone, err := edits.Undo(
		agentCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, applied.Transaction.ID,
		application.EditUndoInput{RequestID: undoRequest},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 3 ||
		undone.Transaction.UndoesTransactionID == nil || *undone.Transaction.UndoesTransactionID != applied.Transaction.ID {
		t.Fatalf("undone=%+v err=%v", undone, err)
	}
	redoRequest, _ := domain.ParseRequestID("agent:edit:redo:001")
	redone, err := edits.Undo(
		agentCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, undone.Transaction.ID,
		application.EditUndoInput{RequestID: redoRequest, Intent: "Restore the opening line"},
	)
	if err != nil || redone.Transaction.CommittedProjectRevision.Value() != 4 {
		t.Fatalf("redone=%+v err=%v", redone, err)
	}
	page, err := activity.List(creatorContext(t), application.ListActivityInput{ProjectID: &created.Project.Project.ID})
	if err != nil || len(page.Events) != 6 || page.Events[2].Kind != "edit.proposed" ||
		page.Events[3].Kind != "edit.committed" || page.Events[5].Kind != "edit.committed" {
		t.Fatalf("activity=%+v err=%v", page, err)
	}
	databasePath := store.Path()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	assertPersistedEditProjection(t, databasePath, created.Project.Project.ID, 4)
}

func allocationID(t *testing.T, proposal domain.EditProposal, local domain.LocalID) string {
	t.Helper()
	for _, allocation := range proposal.Allocation {
		if allocation.Local == local {
			return allocation.ID
		}
	}
	t.Fatalf("proposal has no allocation for %s", local)
	return ""
}

func createSQLiteAgentContext(t *testing.T, store *repository.SQLiteProjects) context.Context {
	t.Helper()
	at := time.Date(2026, 7, 15, 4, 0, 0, 0, time.UTC)
	agentValue, _ := domain.GenerateUUIDv7(at)
	agentID, _ := domain.ParseAgentID(agentValue)
	grantID, _ := domain.GenerateUUIDv7(at.Add(time.Millisecond))
	grant, err := store.EnsurePendingCLIGrant(context.Background(), application.PendingCLIGrant{
		ID: grantID, InstallationID: "installation-edit-test", AgentID: agentID,
		PublicKey: "edit-test-public-key", Fingerprint: "sha256:" + strings.Repeat("e", 64),
		Scopes:    []string{"edit:read", "edit:write", "run:read", "run:write"},
		CreatedAt: at, ExpiresAt: at.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideCLIGrant(context.Background(), grant.ID, true, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, err := application.ContextWithAuthority(context.Background(), application.Authority{
		Surface: application.AuthorityProductCLI, InstallationID: "installation-edit-test",
		GrantID: grant.ID, Actor: domain.AgentActor(agentID), Invocation: testAgentInvocation(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func captionTrackFromOverview(t *testing.T, overview application.ProjectOverview) application.TrackSummary {
	t.Helper()
	for _, track := range overview.Tracks {
		if track.Type == domain.TrackCaption {
			return track
		}
	}
	t.Fatal("genesis has no caption track")
	return application.TrackSummary{}
}

func assertPersistedEditProjection(
	t *testing.T,
	databasePath string,
	projectID domain.ProjectID,
	wantRevision uint64,
) {
	t.Helper()
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var projectRevision, documentRevision, sequenceRevision uint64
	if err := db.QueryRow(`
SELECT p.revision, d.revision, s.revision
FROM projects p
JOIN narrative_documents d ON d.id = p.narrative_document_id
JOIN sequences s ON s.id = p.main_sequence_id
WHERE p.id = ?`, projectID.String()).Scan(&projectRevision, &documentRevision, &sequenceRevision); err != nil {
		t.Fatal(err)
	}
	if projectRevision != wantRevision || documentRevision != wantRevision || sequenceRevision != wantRevision {
		t.Fatalf("revisions project=%d narrative=%d sequence=%d", projectRevision, documentRevision, sequenceRevision)
	}
	var textRevision, captionRevision, alignmentRevision uint64
	var captionLanguage string
	var textTombstoned, captionTombstoned bool
	var alignmentStatus string
	if err := db.QueryRow(`
SELECT node.revision, node.tombstoned
FROM narrative_nodes node
JOIN narrative_authored_text_values value ON value.id = node.id`).Scan(&textRevision, &textTombstoned); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT revision, language, tombstoned FROM captions`).Scan(
		&captionRevision, &captionLanguage, &captionTombstoned,
	); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT revision, status FROM alignments`).Scan(&alignmentRevision, &alignmentStatus); err != nil {
		t.Fatal(err)
	}
	var targetKind, targetCaptionID string
	var targetRevision uint64
	if err := db.QueryRow(`SELECT kind, caption_id, entity_revision FROM alignment_targets`).Scan(
		&targetKind, &targetCaptionID, &targetRevision,
	); err != nil {
		t.Fatal(err)
	}
	if textRevision != 3 || captionRevision != 3 || captionLanguage != "en" || alignmentRevision != 3 ||
		textTombstoned || captionTombstoned || alignmentStatus != string(domain.AlignmentExact) ||
		targetKind != string(domain.AlignmentTargetCaption) || targetCaptionID == "" || targetRevision != 3 {
		t.Fatalf("projection text=%d/%v caption=%d/%s/%v alignment=%d/%s",
			textRevision, textTombstoned, captionRevision, captionLanguage,
			captionTombstoned, alignmentRevision, alignmentStatus)
	}
	var requests, transactions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edit_request_identities`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM edit_transactions WHERE digest IS NOT NULL`).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if requests != 4 || transactions != 3 {
		t.Fatalf("requests=%d transactions=%d", requests, transactions)
	}
}
