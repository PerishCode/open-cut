package tests

import (
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestRoughCutPreviewApplyUndoAndDeterministicReplay(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	prefix, err := domain.ParseLocalID("rough")
	if err != nil {
		t.Fatal(err)
	}
	input := application.RoughCutDerivationPreviewInput{
		TimelineStart: mustRational(t, 10, 1), LocalPrefix: prefix,
		Items: []application.RoughCutDerivationPreviewItemInput{{
			SourceExcerptID: fixture.excerptID, SourceExcerptRevision: 1,
			Audio: &application.RoughCutDerivationPreviewLaneInput{
				TrackID: fixture.audioTrack.ID, TrackRevision: 2, SourceStreamID: fixture.sourceStream,
			},
		}},
	}
	first, err := fixture.editReads.RoughCutDerivation(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID, input,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.editReads.RoughCutDerivation(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID, input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.BaseProjectRevision.Value() != 4 || first.OutputDigest != second.OutputDigest ||
		first.ActivityCursor != second.ActivityCursor || len(first.Operation.DerivedRoughCut) != 1 ||
		first.Operation.DerivedRoughCut[0].Audio == nil || first.Operation.DerivedRoughCut[0].Video != nil ||
		first.Operation.DerivedRoughCut[0].LinkGroupAs != nil {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	proposal, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:rough-cut-propose"), Intent: "materialize selected paper edit",
			BaseProjectRevision: first.BaseProjectRevision, Preconditions: first.Preconditions,
			Operations: []application.EditOperationInput{first.Operation},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Proposal.Operations) != 2 || proposal.Proposal.Operations[0].Clip == nil ||
		proposal.Proposal.Operations[1].Alignment == nil ||
		len(proposal.Proposal.Operations[1].Alignment.Targets) != 1 {
		t.Fatalf("proposal=%+v", proposal.Proposal)
	}
	commit, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, proposal.Proposal.ID,
		application.EditApplyInput{
			RequestID: mustRequestID(t, "agent:rough-cut-apply"), ProposalDigest: proposal.Proposal.Digest,
		},
	)
	if err != nil || commit.Transaction.CommittedProjectRevision.Value() != 5 {
		t.Fatalf("commit=%+v err=%v", commit, err)
	}
	output := first.Operation.DerivedRoughCut[0]
	clipID, err := domain.ParseClipID(allocationID(t, proposal.Proposal, output.Audio.ClipAs))
	if err != nil {
		t.Fatal(err)
	}
	alignmentID, err := domain.ParseAlignmentID(allocationID(t, proposal.Proposal, output.AlignmentAs))
	if err != nil {
		t.Fatal(err)
	}
	clipDetail, err := fixture.editReads.Entity(
		fixture.agent, fixture.project.Project.Project.ID, domain.EntityClip, clipID.String(),
	)
	if err != nil || clipDetail.Clip == nil || clipDetail.Clip.Tombstoned ||
		!sameTestRange(clipDetail.Clip.SourceRange, fixture.sourceRange) ||
		!sameTestRange(clipDetail.Clip.TimelineRange, output.TimelineRange) {
		t.Fatalf("clip=%+v err=%v", clipDetail, err)
	}
	alignment := readAlignmentEntity(t, fixture, alignmentID)
	if alignment.Status != domain.AlignmentExact || alignment.NarrativeNodeID != fixture.excerptID ||
		len(alignment.Targets) != 1 || alignment.Targets[0].Clip == nil ||
		alignment.Targets[0].Clip.ClipID != clipID {
		t.Fatalf("alignment=%+v", alignment)
	}
	undone, err := fixture.edits.Undo(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, commit.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "agent:rough-cut-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 6 {
		t.Fatalf("undo=%+v err=%v", undone, err)
	}
	clipDetail, err = fixture.editReads.Entity(
		fixture.agent, fixture.project.Project.Project.ID, domain.EntityClip, clipID.String(),
	)
	if err != nil || clipDetail.Clip == nil || !clipDetail.Clip.Tombstoned {
		t.Fatalf("undone clip=%+v err=%v", clipDetail, err)
	}
	alignment = readAlignmentEntity(t, fixture, alignmentID)
	if alignment.Status != domain.AlignmentUnbound {
		t.Fatalf("undone alignment=%+v", alignment)
	}
}

func TestCreatorRoughCutPreviewAndDirectApplyShareTheAgentKernel(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	prefix, err := domain.ParseLocalID("creator_rough")
	if err != nil {
		t.Fatal(err)
	}
	input := application.RoughCutDerivationPreviewInput{
		TimelineStart: mustRational(t, 10, 1), LocalPrefix: prefix,
		Items: []application.RoughCutDerivationPreviewItemInput{{
			SourceExcerptID: fixture.excerptID, SourceExcerptRevision: 1,
			Audio: &application.RoughCutDerivationPreviewLaneInput{
				TrackID: fixture.audioTrack.ID, TrackRevision: 2,
				SourceStreamID: fixture.sourceStream,
			},
		}},
	}
	creator := creatorContext(t)
	preview, err := fixture.editReads.RoughCutDerivationForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID, input,
	)
	if err != nil {
		t.Fatal(err)
	}
	agentPreview, err := fixture.editReads.RoughCutDerivation(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID, input,
	)
	if err != nil || preview.OutputDigest != agentPreview.OutputDigest ||
		preview.BaseProjectRevision != agentPreview.BaseProjectRevision || len(preview.Operation.DerivedRoughCut) != 1 {
		t.Fatalf("creator=%+v agent=%+v err=%v", preview, agentPreview, err)
	}
	committed, err := fixture.edits.CommitForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "ui:creator-rough-cut-apply"), Intent: "Apply reviewed rough cut",
			BaseProjectRevision: preview.BaseProjectRevision, Preconditions: preview.Preconditions,
			Operations: []application.EditOperationInput{preview.Operation},
		},
	)
	if err != nil || committed.Proposal.Actor.Kind != domain.ActorCreator || committed.Proposal.RunID != nil ||
		committed.Proposal.TurnID != nil || len(committed.Transaction.Operations) != 2 ||
		committed.Transaction.CommittedProjectRevision.Value() != preview.BaseProjectRevision.Value()+1 {
		t.Fatalf("creator rough-cut commit=%+v err=%v", committed, err)
	}
	output := preview.Operation.DerivedRoughCut[0]
	clipID, err := domain.ParseClipID(allocationID(t, committed.Proposal, output.Audio.ClipAs))
	if err != nil {
		t.Fatal(err)
	}
	clip, err := fixture.editReads.Entity(creator, fixture.project.Project.Project.ID, domain.EntityClip, clipID.String())
	if err != nil || clip.Clip == nil || clip.Clip.Tombstoned ||
		!sameTestRange(clip.Clip.TimelineRange, output.TimelineRange) {
		t.Fatalf("creator rough-cut clip=%+v err=%v", clip, err)
	}
	undone, err := fixture.edits.UndoForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		committed.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "ui:creator-rough-cut-undo")},
	)
	if err != nil || undone.Transaction.UndoesTransactionID == nil ||
		*undone.Transaction.UndoesTransactionID != committed.Transaction.ID {
		t.Fatalf("creator rough-cut undo=%+v err=%v", undone, err)
	}
}
