package tests

import (
	"errors"
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSQLiteCreatorManualCaptionCreatePreviewCommitAndUndo(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	creator := creatorContext(t)
	captionLocal, _ := domain.ParseLocalID("creator_manual_caption")
	captionRange := domain.TimeRange{Start: mustRational(t, 2, 1), Duration: mustRational(t, 1, 1)}
	language := domain.CaptionLanguage("und")
	text := "A manually authored title."
	proposalsBefore, transactionsBefore := editJournalCounts(t, fixture.store.Path())
	preview, err := fixture.editReads.CaptionGestureForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionGesturePreviewInput{
			Kind: application.CreatorCaptionCreate, CaptionAs: &captionLocal,
			TrackID: fixture.captionTrack.ID, TrackRevision: fixture.captionTrack.Revision,
			Range: &captionRange, Language: &language, Text: &text,
		},
	)
	if err != nil || preview.BaseProjectRevision.Value() != 4 || len(preview.Operations) != 1 ||
		preview.Operations[0].Type != domain.EditAddCaption || preview.Subject.CaptionAs == nil ||
		*preview.Subject.CaptionAs != captionLocal || preview.Subject.Provenance != domain.CaptionProvenanceManual ||
		len(preview.AlignmentEffects) != 0 || preview.OutputDigest == "" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	sequenceRevision, _ := domain.NewRevision(2)
	for _, condition := range []domain.EntityPrecondition{
		{Kind: domain.EntitySequence, ID: fixture.project.Project.Project.MainSequenceID.String(), Revision: sequenceRevision},
		{Kind: domain.EntityTrack, ID: fixture.captionTrack.ID.String(), Revision: fixture.captionTrack.Revision},
	} {
		if !hasExactPrecondition(preview.Preconditions, condition) {
			t.Fatalf("missing precondition=%+v in %+v", condition, preview.Preconditions)
		}
	}
	proposalsAfter, transactionsAfter := editJournalCounts(t, fixture.store.Path())
	if proposalsBefore != proposalsAfter || transactionsBefore != transactionsAfter {
		t.Fatalf("preview wrote journal state: proposals %d→%d transactions %d→%d",
			proposalsBefore, proposalsAfter, transactionsBefore, transactionsAfter)
	}
	committed, err := fixture.edits.CommitForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "gesture:creator-manual-caption-create"), Intent: "Create a manual Caption",
			BaseProjectRevision: preview.BaseProjectRevision,
			Preconditions:       preview.Preconditions, Operations: preview.Operations,
		},
	)
	if err != nil || committed.Transaction.CommittedProjectRevision.Value() != 5 {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	captionID, err := domain.ParseCaptionID(allocationID(t, committed.Proposal, captionLocal))
	if err != nil {
		t.Fatal(err)
	}
	caption := readCaptionEntity(t, fixture, captionID)
	if caption.Provenance.Kind != domain.CaptionProvenanceManual || caption.Text != text ||
		caption.Language != language || !sameTestRange(caption.Range, captionRange) {
		t.Fatalf("caption=%+v", caption)
	}
	overlapLocal, _ := domain.ParseLocalID("creator_manual_overlap")
	nextTrackRevision, _ := domain.NewRevision(fixture.captionTrack.Revision.Value() + 1)
	_, err = fixture.editReads.CaptionGestureForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionGesturePreviewInput{
			Kind: application.CreatorCaptionCreate, CaptionAs: &overlapLocal,
			TrackID: fixture.captionTrack.ID, TrackRevision: nextTrackRevision,
			Range: &captionRange, Language: &language, Text: &text,
		},
	)
	if !errors.Is(err, application.ErrEditInvalid) {
		t.Fatalf("same-Track overlap error=%v", err)
	}
	undone, err := fixture.edits.UndoForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		committed.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "gesture:creator-manual-caption-create-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 6 ||
		!readCaptionEntity(t, fixture, captionID).Tombstoned {
		t.Fatalf("undone=%+v err=%v", undone, err)
	}
}

func TestSQLiteCreatorCaptionUpdatePreservesOnlyProvableCaptionAlignment(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	creator := creatorContext(t)
	captionID, alignmentID, derivedCommit := commitFixtureDerivedCaption(t, fixture)
	caption := readCaptionEntity(t, fixture, captionID)
	trackRevision, _ := domain.NewRevision(fixture.captionTrack.Revision.Value() + 1)
	shiftedRange := domain.TimeRange{Start: mustRational(t, 2, 1), Duration: caption.Range.Duration}
	preserve := application.CreatorCaptionPreserveAlignment
	preview, err := fixture.editReads.CaptionGestureForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionGesturePreviewInput{
			Kind: application.CreatorCaptionUpdate, CaptionID: &captionID, CaptionRevision: &caption.Revision,
			TrackID: fixture.captionTrack.ID, TrackRevision: trackRevision,
			Range: &shiftedRange, Language: &caption.Language, Text: &caption.Text,
			AlignmentHandling: &preserve,
		},
	)
	if err != nil || preview.BaseProjectRevision != derivedCommit.Transaction.CommittedProjectRevision ||
		len(preview.Operations) != 2 || preview.Operations[0].Type != domain.EditUpdateCaption ||
		preview.Operations[1].Type != domain.EditRemapAlignment || len(preview.AlignmentEffects) != 1 ||
		preview.AlignmentEffects[0].AlignmentID != alignmentID {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	committed, err := fixture.edits.CommitForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "gesture:creator-caption-timing"), Intent: "Move one Caption in time",
			BaseProjectRevision: preview.BaseProjectRevision,
			Preconditions:       preview.Preconditions, Operations: preview.Operations,
		},
	)
	if err != nil || committed.Transaction.CommittedProjectRevision.Value() != 6 {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	caption = readCaptionEntity(t, fixture, captionID)
	alignment := readAlignmentEntity(t, fixture, alignmentID)
	if !sameTestRange(caption.Range, shiftedRange) || caption.Provenance.Kind != domain.CaptionProvenanceTranscriptDerivation ||
		caption.ProvenanceStatus == nil || caption.ProvenanceStatus.Content != domain.CaptionContentModified ||
		alignment.Status != domain.AlignmentExact || alignment.Targets[0].Caption.CaptionRevision != caption.Revision {
		t.Fatalf("caption=%+v alignment=%+v", caption, alignment)
	}

	changedText := "Creator-polished wording"
	trackRevision, _ = domain.NewRevision(trackRevision.Value() + 1)
	_, err = fixture.editReads.CaptionGestureForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionGesturePreviewInput{
			Kind: application.CreatorCaptionUpdate, CaptionID: &captionID, CaptionRevision: &caption.Revision,
			TrackID: fixture.captionTrack.ID, TrackRevision: trackRevision,
			Range: &caption.Range, Language: &caption.Language, Text: &changedText,
			AlignmentHandling: &preserve,
		},
	)
	if !errors.Is(err, application.ErrEditInvalid) {
		t.Fatalf("text preservation error=%v", err)
	}
	stale := application.CreatorCaptionStaleAlignment
	preview, err = fixture.editReads.CaptionGestureForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionGesturePreviewInput{
			Kind: application.CreatorCaptionUpdate, CaptionID: &captionID, CaptionRevision: &caption.Revision,
			TrackID: fixture.captionTrack.ID, TrackRevision: trackRevision,
			Range: &caption.Range, Language: &caption.Language, Text: &changedText,
			AlignmentHandling: &stale,
		},
	)
	if err != nil || len(preview.Operations) != 2 || preview.Operations[1].Type != domain.EditMarkAlignmentStale {
		t.Fatalf("stale preview=%+v err=%v", preview, err)
	}
	committed, err = fixture.edits.CommitForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "gesture:creator-caption-text"), Intent: "Polish one derived Caption",
			BaseProjectRevision: preview.BaseProjectRevision,
			Preconditions:       preview.Preconditions, Operations: preview.Operations,
		},
	)
	if err != nil || committed.Transaction.CommittedProjectRevision.Value() != 7 {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	caption = readCaptionEntity(t, fixture, captionID)
	alignment = readAlignmentEntity(t, fixture, alignmentID)
	if caption.Text != changedText || caption.ProvenanceStatus == nil ||
		caption.ProvenanceStatus.Content != domain.CaptionContentModified || alignment.Status != domain.AlignmentStale {
		t.Fatalf("caption=%+v alignment=%+v", caption, alignment)
	}
}

func TestSQLiteCreatorCaptionRemoveRequiresExplicitUnbindAndUndoesAtomically(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	creator := creatorContext(t)
	captionID, alignmentID, _ := commitFixtureDerivedCaption(t, fixture)
	caption := readCaptionEntity(t, fixture, captionID)
	trackRevision, _ := domain.NewRevision(fixture.captionTrack.Revision.Value() + 1)
	preserve := application.CreatorCaptionPreserveAlignment
	_, err := fixture.editReads.CaptionGestureForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionGesturePreviewInput{
			Kind: application.CreatorCaptionRemove, CaptionID: &captionID, CaptionRevision: &caption.Revision,
			TrackID: fixture.captionTrack.ID, TrackRevision: trackRevision, AlignmentHandling: &preserve,
		},
	)
	if !errors.Is(err, application.ErrEditInvalid) {
		t.Fatalf("remove preserve error=%v", err)
	}
	unbind := application.CreatorCaptionUnbindAlignment
	preview, err := fixture.editReads.CaptionGestureForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionGesturePreviewInput{
			Kind: application.CreatorCaptionRemove, CaptionID: &captionID, CaptionRevision: &caption.Revision,
			TrackID: fixture.captionTrack.ID, TrackRevision: trackRevision, AlignmentHandling: &unbind,
		},
	)
	if err != nil || len(preview.Operations) != 2 || preview.Operations[0].Type != domain.EditRemoveCaption ||
		preview.Operations[1].Type != domain.EditUnbindAlignment || preview.Subject.CaptionID == nil ||
		*preview.Subject.CaptionID != captionID {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	committed, err := fixture.edits.CommitForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "gesture:creator-caption-remove"), Intent: "Remove one Caption",
			BaseProjectRevision: preview.BaseProjectRevision,
			Preconditions:       preview.Preconditions, Operations: preview.Operations,
		},
	)
	if err != nil || !readCaptionEntity(t, fixture, captionID).Tombstoned ||
		readAlignmentEntity(t, fixture, alignmentID).Status != domain.AlignmentUnbound {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	undone, err := fixture.edits.UndoForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		committed.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "gesture:creator-caption-remove-undo")},
	)
	caption = readCaptionEntity(t, fixture, captionID)
	alignment := readAlignmentEntity(t, fixture, alignmentID)
	if err != nil || caption.Tombstoned || alignment.Status != domain.AlignmentExact ||
		alignment.Targets[0].Caption.CaptionRevision != caption.Revision {
		t.Fatalf("undone=%+v caption=%+v alignment=%+v err=%v", undone, caption, alignment, err)
	}
}

func commitFixtureDerivedCaption(
	t *testing.T,
	fixture captionDerivationIntegrationFixture,
) (domain.CaptionID, domain.AlignmentID, application.EditCommitResult) {
	t.Helper()
	creator := creatorContext(t)
	localPrefix, _ := domain.ParseLocalID("manual_caption_source")
	preview, err := fixture.editReads.CaptionDerivationForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorCaptionDerivationPreviewInput{
			SourceExcerptID: fixture.excerptID, SourceExcerptRevision: 1,
			ClipID: fixture.clipID, ClipRevision: 1,
			TrackID: fixture.captionTrack.ID, TrackRevision: fixture.captionTrack.Revision,
			LocalPrefix: localPrefix,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := fixture.edits.CommitForCreator(
		creator, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "gesture:manual-caption-source"), Intent: "Create derived Caption fixture",
			BaseProjectRevision: preview.BaseProjectRevision,
			Preconditions:       preview.Preconditions, Operations: []application.EditOperationInput{preview.Operation},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	captionID, err := domain.ParseCaptionID(allocationID(t, committed.Proposal, preview.Operation.DerivedCaptions[0].CaptionAs))
	if err != nil {
		t.Fatal(err)
	}
	alignmentID, err := domain.ParseAlignmentID(allocationID(t, committed.Proposal, preview.Operation.DerivedCaptions[0].AlignmentAs))
	if err != nil {
		t.Fatal(err)
	}
	return captionID, alignmentID, committed
}
