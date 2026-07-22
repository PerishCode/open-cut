package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestCreatorCaptionPreviewCommitsOneAtomicInsertOnlyTransactionAndUndo(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	creator := creatorContext(t)
	localPrefix, _ := domain.ParseLocalID("creator_captions")
	staleRevision, _ := domain.NewRevision(2)
	_, err := fixture.editReads.CaptionDerivationForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionDerivationPreviewInput{
			SourceExcerptID: fixture.excerptID, SourceExcerptRevision: 1,
			ClipID: fixture.clipID, ClipRevision: staleRevision,
			TrackID: fixture.captionTrack.ID, TrackRevision: fixture.captionTrack.Revision,
			LocalPrefix: localPrefix,
		},
	)
	if !errors.Is(err, application.ErrEditConflict) {
		t.Fatalf("stale Creator selection error=%v", err)
	}
	proposalsBefore, transactionsBefore := editJournalCounts(t, fixture.store.Path())
	preview, err := fixture.editReads.CaptionDerivationForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionDerivationPreviewInput{
			SourceExcerptID: fixture.excerptID, SourceExcerptRevision: 1,
			ClipID: fixture.clipID, ClipRevision: 1,
			TrackID: fixture.captionTrack.ID, TrackRevision: fixture.captionTrack.Revision,
			LocalPrefix: localPrefix,
		},
	)
	if err != nil || preview.BaseProjectRevision.Value() != 4 || preview.Language != domain.CaptionLanguage("en") ||
		len(preview.Operation.DerivedCaptions) != 1 || preview.Operation.DerivedCaptions[0].Text != "hi world" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	proposalsAfter, transactionsAfter := editJournalCounts(t, fixture.store.Path())
	if proposalsBefore != proposalsAfter || transactionsBefore != transactionsAfter {
		t.Fatalf("preview wrote journal state: proposals %d→%d transactions %d→%d",
			proposalsBefore, proposalsAfter, transactionsBefore, transactionsAfter)
	}
	committed, err := fixture.edits.CommitForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "gesture:creator-caption-apply"), Intent: "Create reviewed readable captions",
			BaseProjectRevision: preview.BaseProjectRevision, Preconditions: preview.Preconditions,
			Operations: []application.EditOperationInput{preview.Operation},
		},
	)
	if err != nil || committed.Transaction.CommittedProjectRevision.Value() != 5 ||
		committed.Proposal.RunID != nil || committed.Proposal.TurnID != nil ||
		committed.Transaction.Actor.Kind != domain.ActorCreator {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	captionID, err := domain.ParseCaptionID(allocationID(
		t, committed.Proposal, preview.Operation.DerivedCaptions[0].CaptionAs,
	))
	if err != nil {
		t.Fatal(err)
	}
	caption := readCaptionEntity(t, fixture, captionID)
	if caption.Text != "hi world" || caption.Language != domain.CaptionLanguage("en") ||
		caption.Provenance.Kind != domain.CaptionProvenanceTranscriptDerivation ||
		caption.ProvenanceStatus == nil || caption.ProvenanceStatus.Content != domain.CaptionContentExact ||
		caption.ProvenanceStatus.Evidence != domain.CaptionEvidenceExact {
		t.Fatalf("caption=%+v", caption)
	}
	trackRevision, _ := domain.NewRevision(2)
	_, err = fixture.editReads.CaptionDerivationForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionDerivationPreviewInput{
			SourceExcerptID: fixture.excerptID, SourceExcerptRevision: 1,
			ClipID: fixture.clipID, ClipRevision: 1,
			TrackID: fixture.captionTrack.ID, TrackRevision: trackRevision,
			LocalPrefix: localPrefix,
		},
	)
	if !errors.Is(err, application.ErrEditInvalid) {
		t.Fatalf("insert-only collision error=%v", err)
	}
	undone, err := fixture.edits.UndoForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		committed.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "gesture:creator-caption-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 6 ||
		undone.Transaction.UndoesTransactionID == nil || *undone.Transaction.UndoesTransactionID != committed.Transaction.ID {
		t.Fatalf("undone=%+v err=%v", undone, err)
	}
	if caption := readCaptionEntity(t, fixture, captionID); !caption.Tombstoned {
		t.Fatalf("undone caption=%+v", caption)
	}
}

func TestCaptionDerivationPreviewApplyUndoAndProvenanceStatus(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()

	preview, err := fixture.editReads.CaptionDerivation(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CaptionDerivationPreviewInput{
			SourceExcerptID: fixture.excerptID, ClipID: fixture.clipID,
			TrackID: fixture.captionTrack.ID, LocalPrefix: "opening",
		},
	)
	if err != nil || preview.BaseProjectRevision.Value() != 4 || len(preview.Preconditions) != 5 ||
		preview.Operation.Type != domain.EditDeriveCaptions || len(preview.Operation.DerivedCaptions) != 1 ||
		preview.Operation.DerivedCaptions[0].Text != "hi world" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	tampered := preview.Operation
	tampered.DerivedCaptions = append([]application.DerivedCaptionOutputInput(nil), tampered.DerivedCaptions...)
	tampered.DerivedCaptions[0].Text = "tampered"
	if _, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:caption-tamper"), Intent: "attempt to alter deterministic output",
			BaseProjectRevision: preview.BaseProjectRevision, Preconditions: preview.Preconditions,
			Operations: []application.EditOperationInput{tampered},
		},
	); !errors.Is(err, application.ErrEditInvalid) {
		t.Fatalf("tampered proposal error=%v", err)
	}

	proposed, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:caption-propose"), Intent: "derive readable captions",
			BaseProjectRevision: preview.BaseProjectRevision, Preconditions: preview.Preconditions,
			Operations: []application.EditOperationInput{preview.Operation},
		},
	)
	if err != nil || len(proposed.Proposal.Operations) != 2 || len(proposed.Proposal.Allocation) != 2 {
		t.Fatalf("proposal=%+v err=%v", proposed, err)
	}
	applied, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, proposed.Proposal.ID,
		application.EditApplyInput{
			RequestID: mustRequestID(t, "agent:caption-apply"), ProposalDigest: proposed.Proposal.Digest,
		},
	)
	if err != nil || applied.Transaction.CommittedProjectRevision.Value() != 5 {
		t.Fatalf("applied=%+v err=%v", applied, err)
	}
	captionID, err := domain.ParseCaptionID(allocationID(t, proposed.Proposal, preview.Operation.DerivedCaptions[0].CaptionAs))
	if err != nil {
		t.Fatal(err)
	}
	alignmentID, err := domain.ParseAlignmentID(allocationID(
		t, proposed.Proposal, preview.Operation.DerivedCaptions[0].AlignmentAs,
	))
	if err != nil {
		t.Fatal(err)
	}
	caption := readCaptionEntity(t, fixture, captionID)
	if caption.Provenance.Kind != domain.CaptionProvenanceTranscriptDerivation ||
		caption.Provenance.Derivation == nil || caption.Provenance.Derivation.SourceExcerptID != fixture.excerptID ||
		caption.Provenance.Derivation.ClipID != fixture.clipID ||
		caption.ProvenanceStatus == nil || caption.ProvenanceStatus.Content != domain.CaptionContentExact ||
		caption.ProvenanceStatus.Evidence != domain.CaptionEvidenceExact {
		t.Fatalf("caption=%+v", caption)
	}

	undone, err := fixture.edits.Undo(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, applied.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "agent:caption-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 6 {
		t.Fatalf("undone=%+v err=%v", undone, err)
	}
	if caption := readCaptionEntity(t, fixture, captionID); !caption.Tombstoned {
		t.Fatalf("undone caption=%+v", caption)
	}
	redone, err := fixture.edits.Undo(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, undone.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "agent:caption-redo")},
	)
	if err != nil || redone.Transaction.CommittedProjectRevision.Value() != 7 {
		t.Fatalf("redone=%+v err=%v", redone, err)
	}
	if caption := readCaptionEntity(t, fixture, captionID); caption.Tombstoned ||
		caption.ProvenanceStatus == nil || caption.ProvenanceStatus.Content != domain.CaptionContentExact {
		t.Fatalf("redone caption=%+v", caption)
	}

	modifiedText := "Creator-polished caption"
	currentCaption := readCaptionEntity(t, fixture, captionID)
	currentAlignment := readAlignmentEntity(t, fixture, alignmentID)
	updated, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:caption-update"), Intent: "polish derived caption",
			BaseProjectRevision: redone.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityCaption, ID: captionID.String(), Revision: currentCaption.Revision},
				{Kind: domain.EntityAlignment, ID: alignmentID.String(), Revision: currentAlignment.Revision},
				{Kind: domain.EntityTrack, ID: fixture.captionTrack.ID.String(), Revision: 4},
			},
			Operations: []application.EditOperationInput{
				{
					Type: domain.EditUpdateCaption, CaptionID: &captionID, Range: &currentCaption.Range,
					Language: &currentCaption.Language, Text: &modifiedText,
				},
				{Type: domain.EditMarkAlignmentStale, AlignmentID: &alignmentID},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	updatedCommit, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, updated.Proposal.ID,
		application.EditApplyInput{RequestID: mustRequestID(t, "agent:caption-update-apply"), ProposalDigest: updated.Proposal.Digest},
	)
	if err != nil || updatedCommit.Transaction.CommittedProjectRevision.Value() != 8 {
		t.Fatalf("updated=%+v err=%v", updatedCommit, err)
	}
	if caption := readCaptionEntity(t, fixture, captionID); caption.ProvenanceStatus == nil ||
		caption.ProvenanceStatus.Content != domain.CaptionContentModified ||
		caption.ProvenanceStatus.Evidence != domain.CaptionEvidenceExact {
		t.Fatalf("modified caption=%+v", caption)
	}

	correction := readCorrectionEntity(t, fixture, fixture.correctionID)
	removed, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:caption-evidence-drift"), Intent: "remove cited correction",
			BaseProjectRevision: updatedCommit.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityTranscriptCorrection, ID: fixture.correctionID.String(), Revision: correction.Revision,
			}},
			Operations: []application.EditOperationInput{{
				Type: domain.EditRemoveTranscriptCorrection, TranscriptCorrectionID: &fixture.correctionID,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	removedCommit, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, removed.Proposal.ID,
		application.EditApplyInput{RequestID: mustRequestID(t, "agent:caption-evidence-drift-apply"), ProposalDigest: removed.Proposal.Digest},
	)
	if err != nil || removedCommit.Transaction.CommittedProjectRevision.Value() != 9 {
		t.Fatalf("removed=%+v err=%v", removedCommit, err)
	}
	if caption := readCaptionEntity(t, fixture, captionID); caption.ProvenanceStatus == nil ||
		caption.ProvenanceStatus.Content != domain.CaptionContentModified ||
		caption.ProvenanceStatus.Evidence != domain.CaptionEvidenceStale {
		t.Fatalf("stale caption=%+v", caption)
	}
}

func TestCaptionDerivationMapsOneExcerptIntoTwoExplicitClipInstances(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	secondLocal, _ := domain.ParseLocalID("caption_clip_second")
	enabled := true
	secondTimeline := domain.TimeRange{Start: mustRational(t, 2, 1), Duration: fixture.sourceRange.Duration}
	secondProposal, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:caption-second-clip"), Intent: "reuse excerpt in a second clip",
			BaseProjectRevision: 4,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityTrack, ID: fixture.audioTrack.ID.String(), Revision: 2},
				{Kind: domain.EntityAsset, ID: fixture.assetID.String(), Revision: fixture.assetRevision},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditAddClip, CreateAs: &secondLocal, TrackID: &fixture.audioTrack.ID,
				AssetID: &fixture.assetID, SourceStreamID: &fixture.sourceStream,
				SourceRange: &fixture.sourceRange, TimelineRange: &secondTimeline, Enabled: &enabled,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondCommit, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, secondProposal.Proposal.ID,
		application.EditApplyInput{RequestID: mustRequestID(t, "agent:caption-second-clip-apply"), ProposalDigest: secondProposal.Proposal.Digest},
	)
	if err != nil || secondCommit.Transaction.CommittedProjectRevision.Value() != 5 {
		t.Fatalf("second clip=%+v err=%v", secondCommit, err)
	}
	secondClipID, err := domain.ParseClipID(allocationID(t, secondProposal.Proposal, secondLocal))
	if err != nil {
		t.Fatal(err)
	}
	first, err := fixture.editReads.CaptionDerivation(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CaptionDerivationPreviewInput{
			SourceExcerptID: fixture.excerptID, ClipID: fixture.clipID,
			TrackID: fixture.captionTrack.ID, LocalPrefix: "first",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.editReads.CaptionDerivation(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CaptionDerivationPreviewInput{
			SourceExcerptID: fixture.excerptID, ClipID: secondClipID,
			TrackID: fixture.captionTrack.ID, LocalPrefix: "second",
		},
	)
	if err != nil || first.BaseProjectRevision != second.BaseProjectRevision ||
		len(first.Operation.DerivedCaptions) != 1 || len(second.Operation.DerivedCaptions) != 1 ||
		!sameTestRange(first.Operation.DerivedCaptions[0].SourceRange, second.Operation.DerivedCaptions[0].SourceRange) ||
		sameTestRange(first.Operation.DerivedCaptions[0].TimelineRange, second.Operation.DerivedCaptions[0].TimelineRange) {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	combined, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:caption-two-clips"), Intent: "caption both explicit clip instances",
			BaseProjectRevision: first.BaseProjectRevision,
			Preconditions:       mergeCaptionPreconditions(first.Preconditions, second.Preconditions),
			Operations:          []application.EditOperationInput{first.Operation, second.Operation},
		},
	)
	if err != nil || len(combined.Proposal.Operations) != 4 {
		t.Fatalf("combined=%+v err=%v", combined, err)
	}
	committed, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, combined.Proposal.ID,
		application.EditApplyInput{RequestID: mustRequestID(t, "agent:caption-two-clips-apply"), ProposalDigest: combined.Proposal.Digest},
	)
	if err != nil || committed.Transaction.CommittedProjectRevision.Value() != 6 {
		t.Fatalf("commit=%+v err=%v", committed, err)
	}
	window, err := fixture.editReads.SequenceWindow(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		&fixture.captionTrack.ID,
		domain.TimeRange{Start: mustRational(t, 0, 1), Duration: mustRational(t, 3, 1)}, "", 20,
	)
	if err != nil || len(window.Captions) != 2 || window.Captions[0].Text != "hi world" ||
		window.Captions[1].Text != "hi world" {
		t.Fatalf("window=%+v err=%v", window, err)
	}
}

func TestUnifiedAlignmentPersistsTwoClipTargetsAndWindowDeduplicatesIt(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	secondLocal, _ := domain.ParseLocalID("alignment_second_clip")
	enabled := true
	secondTimeline := domain.TimeRange{Start: mustRational(t, 2, 1), Duration: fixture.sourceRange.Duration}
	secondProposal, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:alignment-second-clip"), Intent: "create another exact clip realization",
			BaseProjectRevision: 4,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityTrack, ID: fixture.audioTrack.ID.String(), Revision: 2},
				{Kind: domain.EntityAsset, ID: fixture.assetID.String(), Revision: fixture.assetRevision},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditAddClip, CreateAs: &secondLocal, TrackID: &fixture.audioTrack.ID,
				AssetID: &fixture.assetID, SourceStreamID: &fixture.sourceStream,
				SourceRange: &fixture.sourceRange, TimelineRange: &secondTimeline, Enabled: &enabled,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondCommit, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, secondProposal.Proposal.ID,
		application.EditApplyInput{
			RequestID: mustRequestID(t, "agent:alignment-second-clip-apply"), ProposalDigest: secondProposal.Proposal.Digest,
		},
	)
	if err != nil || secondCommit.Transaction.CommittedProjectRevision.Value() != 5 {
		t.Fatalf("second clip=%+v err=%v", secondCommit, err)
	}
	secondClipID, err := domain.ParseClipID(allocationID(t, secondProposal.Proposal, secondLocal))
	if err != nil {
		t.Fatal(err)
	}
	alignmentLocal, _ := domain.ParseLocalID("two_clip_alignment")
	localRange := domain.TimeRange{Start: mustRational(t, 0, 1), Duration: fixture.sourceRange.Duration}
	bound, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:alignment-bind-clips"), Intent: "bind one excerpt to both clip realizations",
			BaseProjectRevision: secondCommit.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityNarrativeNode, ID: fixture.excerptID.String(), Revision: 1},
				{Kind: domain.EntityClip, ID: fixture.clipID.String(), Revision: 1},
				{Kind: domain.EntityClip, ID: secondClipID.String(), Revision: 1},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditBindAlignment, CreateAs: &alignmentLocal,
				NarrativeNode: &application.EditReference{ID: fixture.excerptID.String()},
				AlignmentTargets: []application.AlignmentTargetInput{
					{Type: domain.AlignmentTargetClip, Clip: &application.EditReference{ID: fixture.clipID.String()}, LocalRange: &localRange},
					{Type: domain.AlignmentTargetClip, Clip: &application.EditReference{ID: secondClipID.String()}, LocalRange: &localRange},
				},
			}},
		},
	)
	if err != nil || len(bound.Proposal.Operations) != 1 {
		t.Fatalf("bound=%+v err=%v", bound, err)
	}
	boundCommit, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, bound.Proposal.ID,
		application.EditApplyInput{
			RequestID: mustRequestID(t, "agent:alignment-bind-clips-apply"), ProposalDigest: bound.Proposal.Digest,
		},
	)
	if err != nil || boundCommit.Transaction.CommittedProjectRevision.Value() != 6 {
		t.Fatalf("commit=%+v err=%v", boundCommit, err)
	}
	alignmentID, err := domain.ParseAlignmentID(allocationID(t, bound.Proposal, alignmentLocal))
	if err != nil {
		t.Fatal(err)
	}
	alignment := readAlignmentEntity(t, fixture, alignmentID)
	if alignment.Status != domain.AlignmentExact || len(alignment.Targets) != 2 ||
		alignment.Targets[0].Type != domain.AlignmentTargetClip || alignment.Targets[1].Type != domain.AlignmentTargetClip {
		t.Fatalf("alignment=%+v", alignment)
	}
	window, err := fixture.editReads.SequenceWindow(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		&fixture.audioTrack.ID, domain.TimeRange{Start: mustRational(t, 0, 1), Duration: mustRational(t, 3, 1)}, "", 20,
	)
	if err != nil || len(window.Clips) != 2 || len(window.Alignments) != 1 || window.Alignments[0].ID != alignmentID {
		t.Fatalf("window=%+v err=%v", window, err)
	}
	undone, err := fixture.edits.Undo(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, boundCommit.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "agent:alignment-bind-clips-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 7 {
		t.Fatalf("undo=%+v err=%v", undone, err)
	}
	alignment = readAlignmentEntity(t, fixture, alignmentID)
	if alignment.Status != domain.AlignmentUnbound || len(alignment.Targets) != 2 {
		t.Fatalf("undone alignment=%+v", alignment)
	}
}

type captionDerivationIntegrationFixture struct {
	store         *repository.SQLiteProjects
	project       application.CreateProjectResult
	agent         context.Context
	run           application.RunCommandResult
	edits         *application.Edits
	editReads     *application.EditReads
	captionTrack  application.TrackSummary
	audioTrack    application.TrackSummary
	assetID       domain.AssetID
	assetRevision domain.Revision
	sourceStream  domain.SourceStreamID
	sourceRange   domain.TimeRange
	excerptID     domain.NarrativeNodeID
	correctionID  domain.TranscriptCorrectionID
	clipID        domain.ClipID
}

func newCaptionDerivationIntegrationFixture(t *testing.T) captionDerivationIntegrationFixture {
	t.Helper()
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	resources := acquireTranscriptFixtureResource(t, store, dataDir)
	projects, _, _, runs := testProjectApplications(t, store)
	media, _, sourceAccess := testMediaApplications(t, store)
	creator := creatorContext(t)
	created, err := projects.Create(creator, application.CreateProjectInput{
		RequestID: mustRequestID(t, "ui:caption-project"), Name: "Caption derivation",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes := []byte("stable source bytes for caption derivation")
	sourcePath := filepath.Join(t.TempDir(), "caption-source.mov")
	if err := os.WriteFile(sourcePath, sourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	grant, err := sourceAccess.RegisterSelection(creator, service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "ui:caption-source"), Path: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creator, created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "ui:caption-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	digestBytes := sha256.Sum256(sourceBytes)
	fingerprint, err := domain.ParseDigest("sha256:" + hex.EncodeToString(digestBytes[:]))
	if err != nil {
		t.Fatal(err)
	}
	duration := mustRational(t, 1, 1)
	transcriptExecutor := &fixedTranscriptExecutor{result: transcriptRecognitionFixture(t)}
	executors := []application.MediaJobExecutor{
		fixedIdentifyExecutor{result: application.MediaIdentification{Fingerprint: fingerprint, Observation: grant.Grant.Observation}},
		fixedProbeExecutor{version: "ffprobe-caption-fixture-v1", result: application.MediaProbe{
			Container: "mov", Duration: &duration,
			Streams: []domain.SourceStreamDescriptor{{
				Index: 0, MediaType: domain.MediaAudio, Codec: "aac", TimeBase: mustRational(t, 1, 48_000),
				Duration: &duration, Dispositions: []string{"default"},
				Audio: &domain.AudioStreamFacts{SampleFormat: "fltp", SampleRate: 48_000, Channels: 2, ChannelLayout: "stereo"},
			}},
		}},
		transcriptExecutor,
	}
	workExecutors, err := application.NewMediaWorkExecutors(
		store, executors, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := application.NewWorkScheduler(
		store, workExecutors, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
		application.WorkSchedulerSettings{
			LeaseOwner: "caption-derivation-worker", LeaseDuration: 30 * time.Second,
			PollInterval: 10 * time.Millisecond, Resources: resources.RuntimeRegistrations(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for step := 0; step < 3; step++ {
		if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("media step %d executed=%v err=%v", step, executed, runErr)
		}
	}
	transcript, err := media.ReadTranscript(creator, application.TranscriptReadQuery{
		ProjectID: created.Project.Project.ID, AssetID: registered.Asset.Asset.ID, Limit: 20,
	})
	if err != nil || len(transcript.Segments) != 1 {
		t.Fatalf("transcript=%+v err=%v", transcript, err)
	}
	agent := createSQLiteAgentContext(t, store)
	run, err := runs.Begin(agent, created.Project.Project.ID, application.RunBeginInput{
		RequestID: mustRequestID(t, "agent:caption-run"), Intent: "derive captions from corrected paper edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := application.NewEdits(store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	correctionLocal, _ := domain.ParseLocalID("caption_correction")
	excerptLocal, _ := domain.ParseLocalID("caption_excerpt")
	language := domain.CaptionLanguage("en")
	segment := transcript.Segments[0]
	correctionRange := segment.Tokens[0].SourceRange
	correctionText := "hi"
	writingProposal, err := edits.Propose(
		agent, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:caption-writing-propose"),
			Intent:    "correct and cite transcript", BaseProjectRevision: registered.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityNarrativeNode, ID: created.Project.NarrativeRootNodeID.String(), Revision: 1,
			}},
			Operations: []application.EditOperationInput{
				{
					Type: domain.EditAddTranscriptCorrection, CreateAs: &correctionLocal,
					AssetID: &registered.Asset.Asset.ID, TranscriptArtifactID: &transcript.Artifact.ID,
					TranscriptSegmentIDs: []domain.TranscriptSegmentID{segment.ID},
					SourceRange:          &correctionRange, Language: &language, Text: &correctionText,
				},
				{
					Type: domain.EditInsertSourceExcerpt, CreateAs: &excerptLocal,
					ParentID: &created.Project.NarrativeRootNodeID, AssetID: &registered.Asset.Asset.ID,
					AcceptedFingerprint: &fingerprint, TranscriptArtifactID: &transcript.Artifact.ID,
					TranscriptSegmentIDs: []domain.TranscriptSegmentID{segment.ID},
					SourceRange:          &segment.SourceRange, Language: &language,
					CorrectionRevisions: []application.TranscriptCorrectionReferenceInput{{
						Correction: application.EditReference{Local: &correctionLocal},
					}},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	writingCommit, err := edits.Apply(
		agent, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, writingProposal.Proposal.ID,
		application.EditApplyInput{RequestID: mustRequestID(t, "agent:caption-writing-apply"), ProposalDigest: writingProposal.Proposal.Digest},
	)
	if err != nil {
		t.Fatal(err)
	}
	excerptID, err := domain.ParseNarrativeNodeID(allocationID(t, writingProposal.Proposal, excerptLocal))
	if err != nil {
		t.Fatal(err)
	}
	correctionID, err := domain.ParseTranscriptCorrectionID(allocationID(t, writingProposal.Proposal, correctionLocal))
	if err != nil {
		t.Fatal(err)
	}
	var audioTrack application.TrackSummary
	for _, track := range created.Project.Tracks {
		if track.Type == domain.TrackAudio {
			audioTrack = track
		}
	}
	if audioTrack.ID.IsZero() {
		t.Fatal("genesis has no audio track")
	}
	clipLocal, _ := domain.ParseLocalID("caption_clip")
	enabled := true
	timelineRange := segment.SourceRange
	clipProposal, err := edits.Propose(
		agent, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:caption-clip-propose"), Intent: "materialize source clip",
			BaseProjectRevision: writingCommit.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityTrack, ID: audioTrack.ID.String(), Revision: audioTrack.Revision},
				{Kind: domain.EntityAsset, ID: registered.Asset.Asset.ID.String(), Revision: registered.Asset.Asset.Revision},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditAddClip, CreateAs: &clipLocal, TrackID: &audioTrack.ID,
				AssetID: &registered.Asset.Asset.ID, SourceStreamID: &transcript.Artifact.SourceStreamID,
				SourceRange: &segment.SourceRange, TimelineRange: &timelineRange, Enabled: &enabled,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	clipCommit, err := edits.Apply(
		agent, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, clipProposal.Proposal.ID,
		application.EditApplyInput{RequestID: mustRequestID(t, "agent:caption-clip-apply"), ProposalDigest: clipProposal.Proposal.Digest},
	)
	if err != nil || clipCommit.Transaction.CommittedProjectRevision.Value() != 4 {
		t.Fatalf("clip commit=%+v err=%v", clipCommit, err)
	}
	clipID, err := domain.ParseClipID(allocationID(t, clipProposal.Proposal, clipLocal))
	if err != nil {
		t.Fatal(err)
	}
	editReads, err := application.NewEditReads(store)
	if err != nil {
		t.Fatal(err)
	}
	return captionDerivationIntegrationFixture{
		store: store, project: created, agent: agent, run: run, edits: edits, editReads: editReads,
		captionTrack: captionTrackFromOverview(t, created.Project), audioTrack: audioTrack,
		assetID: registered.Asset.Asset.ID, assetRevision: registered.Asset.Asset.Revision,
		sourceStream: transcript.Artifact.SourceStreamID, sourceRange: segment.SourceRange, excerptID: excerptID,
		correctionID: correctionID, clipID: clipID,
	}
}

func mergeCaptionPreconditions(groups ...[]domain.EntityPrecondition) []domain.EntityPrecondition {
	result := make([]domain.EntityPrecondition, 0)
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, condition := range group {
			key := string(condition.Kind) + "\x00" + condition.ID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, condition)
		}
	}
	return result
}

func sameTestRange(left, right domain.TimeRange) bool {
	start, startErr := left.Start.Compare(right.Start)
	duration, durationErr := left.Duration.Compare(right.Duration)
	return startErr == nil && durationErr == nil && start == 0 && duration == 0
}

func readCaptionEntity(
	t *testing.T,
	fixture captionDerivationIntegrationFixture,
	id domain.CaptionID,
) domain.CaptionState {
	t.Helper()
	result, err := fixture.editReads.Entity(fixture.agent, fixture.project.Project.Project.ID, domain.EntityCaption, id.String())
	if err != nil || result.Caption == nil {
		t.Fatalf("caption entity=%+v err=%v", result, err)
	}
	return *result.Caption
}

func readCorrectionEntity(
	t *testing.T,
	fixture captionDerivationIntegrationFixture,
	id domain.TranscriptCorrectionID,
) domain.TranscriptCorrectionState {
	t.Helper()
	result, err := fixture.editReads.Entity(
		fixture.agent, fixture.project.Project.Project.ID, domain.EntityTranscriptCorrection, id.String(),
	)
	if err != nil || result.TranscriptCorrection == nil {
		t.Fatalf("correction entity=%+v err=%v", result, err)
	}
	return *result.TranscriptCorrection
}

func readAlignmentEntity(
	t *testing.T,
	fixture captionDerivationIntegrationFixture,
	id domain.AlignmentID,
) domain.AlignmentState {
	t.Helper()
	result, err := fixture.editReads.Entity(
		fixture.agent, fixture.project.Project.Project.ID, domain.EntityAlignment, id.String(),
	)
	if err != nil || result.Alignment == nil {
		t.Fatalf("alignment entity=%+v err=%v", result, err)
	}
	return *result.Alignment
}
