package tests

import (
	"context"
	"database/sql"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func commitAndUndoCreatorSourceExcerpt(
	t *testing.T,
	store *repository.SQLiteProjects,
	creator context.Context,
	edits *application.Edits,
	editReads *application.EditReads,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	documentID domain.NarrativeDocumentID,
	rootID domain.NarrativeNodeID,
	assetID domain.AssetID,
	fingerprint domain.Digest,
	artifactID domain.ArtifactID,
	segment application.TranscriptSegmentView,
	language domain.CaptionLanguage,
	baseRevision domain.Revision,
	parentRevision domain.Revision,
) domain.Revision {
	t.Helper()
	local, _ := domain.ParseLocalID("creator_excerpt")
	requestID := mustRequestID(t, "ui:transcript-creator-excerpt")
	committed, err := edits.CommitForCreator(
		creator, projectID, sequenceID,
		application.EditProposeInput{
			RequestID: requestID, Intent: "Insert exact transcript evidence directly",
			BaseProjectRevision: baseRevision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityNarrativeNode, ID: rootID.String(), Revision: parentRevision,
			}},
			Operations: []application.EditOperationInput{{
				Type: domain.EditInsertSourceExcerpt, CreateAs: &local,
				ParentID: &rootID, AssetID: &assetID, AcceptedFingerprint: &fingerprint,
				TranscriptArtifactID: &artifactID, TranscriptSegmentIDs: []domain.TranscriptSegmentID{segment.ID},
				SourceRange: &segment.SourceRange, Language: &language,
			}},
		},
	)
	if err != nil || committed.Proposal.Actor.Kind != domain.ActorCreator ||
		committed.Proposal.RunID != nil || committed.Proposal.TurnID != nil ||
		committed.Transaction.CommittedProjectRevision.Value() != baseRevision.Value()+1 {
		t.Fatalf("creator source excerpt=%+v err=%v", committed, err)
	}
	narrative, err := editReads.NarrativeSubtree(creator, projectID, documentID, rootID, "", 50)
	if err != nil || len(narrative.Nodes) != 1 || narrative.Nodes[0].SourceExcerpt == nil ||
		narrative.Nodes[0].SourceExcerpt.EffectiveText != "hello world" ||
		len(narrative.Nodes[0].SourceExcerpt.Evidence.CorrectionRevisions) != 0 ||
		narrative.Nodes[0].EvidenceStatus != domain.SourceExcerptEvidenceExact {
		t.Fatalf("creator source excerpt projection=%+v err=%v", narrative, err)
	}
	database, err := sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var actorKind string
	var runID, turnID sql.NullString
	if err := database.QueryRow(`
SELECT actor_kind, run_id, turn_id
FROM edit_request_identities
WHERE request_id = ?`, requestID.String()).Scan(&actorKind, &runID, &turnID); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if actorKind != string(domain.ActorCreator) || runID.Valid || turnID.Valid {
		t.Fatalf("creator source excerpt authority actor=%s run=%+v turn=%+v", actorKind, runID, turnID)
	}
	undone, err := edits.UndoForCreator(
		creator, projectID, sequenceID, committed.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "ui:transcript-creator-excerpt-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != baseRevision.Value()+2 {
		t.Fatalf("creator source excerpt undo=%+v err=%v", undone, err)
	}
	narrative, err = editReads.NarrativeSubtree(creator, projectID, documentID, rootID, "", 50)
	if err != nil || len(narrative.Nodes) != 0 {
		t.Fatalf("creator source excerpt undo projection=%+v err=%v", narrative, err)
	}
	return undone.Transaction.CommittedProjectRevision
}
