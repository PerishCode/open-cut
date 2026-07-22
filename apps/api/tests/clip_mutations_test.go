package tests

import (
	"database/sql"
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSQLiteCreatorTimelineGesturePlansCompleteAlignmentClosureAndCommitsAtomically(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	alignmentID := bindFixtureClipAlignment(t, fixture, "creator_timeline_alignment")
	proposalsBefore, transactionsBefore := editJournalCounts(t, fixture.store.Path())
	creator := creatorContext(t)
	start := mustRational(t, 2, 1)
	trackRevision, _ := domain.NewRevision(2)
	result, err := fixture.editReads.TimelineGestureForCreator(
		creator,
		fixture.project.Project.Project.ID,
		fixture.project.Project.Project.MainSequenceID,
		application.CreatorTimelineGesturePreviewInput{
			Kind: application.CreatorTimelineMove, ClipID: fixture.clipID, ClipRevision: 1,
			Scope: domain.ClipScopeSingle, AlignmentHandling: application.CreatorTimelinePreserveAlignment,
			TrackID: &fixture.audioTrack.ID, TrackRevision: &trackRevision, TimelineStart: &start,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != application.CreatorTimelinePreviewReady || result.Ready == nil || result.Blocked != nil {
		t.Fatalf("result=%+v", result)
	}
	preview := *result.Ready
	if preview.BaseProjectRevision.Value() != 5 || preview.Kind != application.CreatorTimelineMove ||
		preview.SeedClipID != fixture.clipID || len(preview.AffectedClipIDs) != 1 ||
		len(preview.Operations) != 2 || preview.Operations[0].Type != domain.EditMoveClip ||
		preview.Operations[1].Type != domain.EditRemapAlignment || len(preview.AlignmentEffects) != 1 ||
		preview.AlignmentEffects[0].AlignmentID != alignmentID || preview.OutputDigest == "" ||
		len(preview.ClipEffects) != 1 || preview.ClipEffects[0].Outcome != application.CreatorTimelineClipUpdated ||
		preview.ClipEffects[0].After == nil || !sameTestRational(preview.ClipEffects[0].After.TimelineRange.Start, start) {
		t.Fatalf("preview=%+v", preview)
	}
	sequenceRevision, _ := domain.NewRevision(2)
	for _, condition := range []domain.EntityPrecondition{
		{Kind: domain.EntitySequence, ID: fixture.project.Project.Project.MainSequenceID.String(), Revision: sequenceRevision},
		{Kind: domain.EntityClip, ID: fixture.clipID.String(), Revision: 1},
		{Kind: domain.EntityTrack, ID: fixture.audioTrack.ID.String(), Revision: trackRevision},
		{Kind: domain.EntityAlignment, ID: alignmentID.String(), Revision: 1},
	} {
		if !hasExactPrecondition(preview.Preconditions, condition) {
			t.Fatalf("missing precondition=%+v in %+v", condition, preview.Preconditions)
		}
	}
	proposalsAfter, transactionsAfter := editJournalCounts(t, fixture.store.Path())
	if proposalsAfter != proposalsBefore || transactionsAfter != transactionsBefore {
		t.Fatalf("preview wrote durable state: proposals %d→%d transactions %d→%d",
			proposalsBefore, proposalsAfter, transactionsBefore, transactionsAfter)
	}
	committed, err := fixture.edits.CommitForCreator(
		creator,
		fixture.project.Project.Project.ID,
		fixture.project.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "gesture:creator-timeline-move"), Intent: "Move the selected clip",
			BaseProjectRevision: preview.BaseProjectRevision,
			Preconditions:       preview.Preconditions, Operations: preview.Operations,
		},
	)
	if err != nil || committed.Transaction.CommittedProjectRevision.Value() != 6 ||
		committed.Proposal.RunID != nil || committed.Proposal.TurnID != nil {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	clip := readClipEntity(t, fixture, fixture.clipID)
	alignment := readAlignmentEntity(t, fixture, alignmentID)
	if !sameTestRational(clip.TimelineRange.Start, start) || clip.Revision.Value() != 2 ||
		alignment.Status != domain.AlignmentExact || alignment.Revision.Value() != 2 ||
		len(alignment.Targets) != 1 || alignment.Targets[0].Clip.ClipRevision.Value() != 2 {
		t.Fatalf("clip=%+v alignment=%+v", clip, alignment)
	}
	undone, err := fixture.edits.UndoForCreator(
		creator,
		fixture.project.Project.Project.ID,
		fixture.project.Project.Project.MainSequenceID,
		committed.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "gesture:creator-timeline-move-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 7 {
		t.Fatalf("undone=%+v err=%v", undone, err)
	}
	clip = readClipEntity(t, fixture, fixture.clipID)
	alignment = readAlignmentEntity(t, fixture, alignmentID)
	if !sameTestRational(clip.TimelineRange.Start, mustRational(t, 0, 1)) ||
		alignment.Status != domain.AlignmentExact || alignment.Targets[0].Clip.ClipRevision.Value() != 3 {
		t.Fatalf("undone clip=%+v alignment=%+v", clip, alignment)
	}
}

func TestSQLiteCreatorTimelineGestureRejectsUnprovableAlignmentPreservation(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	bindFixtureClipAlignment(t, fixture, "creator_trim_alignment")
	half := mustRational(t, 1, 2)
	trimmedDuration, err := fixture.sourceRange.Duration.Subtract(half)
	if err != nil || !trimmedDuration.IsPositive() {
		t.Fatalf("fixture source range cannot be trimmed: %+v", fixture.sourceRange)
	}
	sourceStart, err := fixture.sourceRange.Start.Add(half)
	if err != nil {
		t.Fatal(err)
	}
	sourceRange, err := domain.NewTimeRange(sourceStart, trimmedDuration)
	if err != nil {
		t.Fatal(err)
	}
	timelineRange, err := domain.NewTimeRange(half, trimmedDuration)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := fixture.editReads.TimelineGestureForCreator(
		creatorContext(t), fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorTimelineGesturePreviewInput{
			Kind: application.CreatorTimelineTrim, ClipID: fixture.clipID, ClipRevision: 1,
			Scope: domain.ClipScopeSingle, AlignmentHandling: application.CreatorTimelinePreserveAlignment,
			SourceRange: &sourceRange, TimelineRange: &timelineRange,
		},
	)
	if err != nil || blocked.Status != application.CreatorTimelinePreviewBlocked || blocked.Blocked == nil ||
		blocked.Ready != nil || blocked.Blocked.Reason != application.CreatorTimelinePreserveUnprovable ||
		len(blocked.Blocked.Recoveries) != 2 {
		t.Fatalf("expected typed unprovable preservation block, got result=%+v err=%v", blocked, err)
	}
	result, err := fixture.editReads.TimelineGestureForCreator(
		creatorContext(t), fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorTimelineGesturePreviewInput{
			Kind: application.CreatorTimelineTrim, ClipID: fixture.clipID, ClipRevision: 1,
			Scope: domain.ClipScopeSingle, AlignmentHandling: application.CreatorTimelineStaleAlignment,
			SourceRange: &sourceRange, TimelineRange: &timelineRange,
		},
	)
	if err != nil || result.Status != application.CreatorTimelinePreviewReady || result.Ready == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	preview := *result.Ready
	if len(preview.Operations) != 2 ||
		preview.Operations[0].Type != domain.EditTrimClip ||
		preview.Operations[1].Type != domain.EditMarkAlignmentStale {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestSQLiteCreatorTimelineGestureReturnsTypedNoChangeWithoutWriting(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	beforeProposals, beforeTransactions := editJournalCounts(t, fixture.store.Path())
	zero := mustRational(t, 0, 1)
	trackRevision, _ := domain.NewRevision(2)
	result, err := fixture.editReads.TimelineGestureForCreator(
		creatorContext(t), fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorTimelineGesturePreviewInput{
			Kind: application.CreatorTimelineMove, ClipID: fixture.clipID, ClipRevision: 1,
			Scope: domain.ClipScopeSingle, AlignmentHandling: application.CreatorTimelinePreserveAlignment,
			TrackID: &fixture.audioTrack.ID, TrackRevision: &trackRevision, TimelineStart: &zero,
		},
	)
	if err != nil || result.Status != application.CreatorTimelinePreviewBlocked || result.Blocked == nil ||
		result.Ready != nil || result.Blocked.Reason != application.CreatorTimelineNoChange ||
		len(result.Blocked.Recoveries) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	afterProposals, afterTransactions := editJournalCounts(t, fixture.store.Path())
	if beforeProposals != afterProposals || beforeTransactions != afterTransactions {
		t.Fatalf("no-change preview wrote journal state: %d/%d -> %d/%d",
			beforeProposals, beforeTransactions, afterProposals, afterTransactions)
	}
}

func TestSQLiteCreatorTimelineGestureReturnsTypedCollisionClosure(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	local, _ := domain.ParseLocalID("timeline_collision_clip")
	enabled := true
	timelineRange := domain.TimeRange{Start: mustRational(t, 2, 1), Duration: fixture.sourceRange.Duration}
	proposal, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:timeline-collision"), Intent: "create a collision target",
			BaseProjectRevision: 4,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityTrack, ID: fixture.audioTrack.ID.String(), Revision: 2},
				{Kind: domain.EntityAsset, ID: fixture.assetID.String(), Revision: fixture.assetRevision},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditAddClip, CreateAs: &local, TrackID: &fixture.audioTrack.ID,
				AssetID: &fixture.assetID, SourceStreamID: &fixture.sourceStream,
				SourceRange: &fixture.sourceRange, TimelineRange: &timelineRange, Enabled: &enabled,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, proposal.Proposal.ID,
		application.EditApplyInput{
			RequestID: mustRequestID(t, "agent:timeline-collision-apply"), ProposalDigest: proposal.Proposal.Digest,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := domain.ParseClipID(allocationID(t, proposal.Proposal, local))
	if err != nil {
		t.Fatal(err)
	}
	trackRevision, _ := domain.NewRevision(3)
	start := mustRational(t, 2, 1)
	result, err := fixture.editReads.TimelineGestureForCreator(
		creatorContext(t), fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorTimelineGesturePreviewInput{
			Kind: application.CreatorTimelineMove, ClipID: fixture.clipID, ClipRevision: 1,
			Scope: domain.ClipScopeSingle, AlignmentHandling: application.CreatorTimelinePreserveAlignment,
			TrackID: &fixture.audioTrack.ID, TrackRevision: &trackRevision, TimelineStart: &start,
		},
	)
	if err != nil || result.Status != application.CreatorTimelinePreviewBlocked || result.Blocked == nil ||
		result.Blocked.Reason != application.CreatorTimelineTrackCollision ||
		len(result.Blocked.SubjectClipIDs) != 2 ||
		!hasClipID(result.Blocked.SubjectClipIDs, fixture.clipID) ||
		!hasClipID(result.Blocked.SubjectClipIDs, secondID) ||
		result.Blocked.BaseProjectRevision != committed.Transaction.CommittedProjectRevision {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestSQLiteCreatorTimelineSplitEffectsHideAllocatedIdentities(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	half := mustRational(t, 1, 2)
	prefix, _ := domain.ParseLocalID("timeline_split")
	result, err := fixture.editReads.TimelineGestureForCreator(
		creatorContext(t), fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		application.CreatorTimelineGesturePreviewInput{
			Kind: application.CreatorTimelineSplit, ClipID: fixture.clipID, ClipRevision: 1,
			Scope: domain.ClipScopeSingle, AlignmentHandling: application.CreatorTimelinePreserveAlignment,
			SplitAt: &half, LocalPrefix: &prefix,
		},
	)
	if err != nil || result.Status != application.CreatorTimelinePreviewReady || result.Ready == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	preview := result.Ready
	if len(preview.ClipEffects) != 1 || preview.ClipEffects[0].Outcome != application.CreatorTimelineClipSplit ||
		preview.ClipEffects[0].Left == nil || preview.ClipEffects[0].Right == nil ||
		preview.ClipEffects[0].Left.Revision.Value() != 1 || preview.ClipEffects[0].Right.Revision.Value() != 1 ||
		!sameTestRational(preview.ClipEffects[0].Left.TimelineRange.Duration, half) ||
		!sameTestRational(preview.ClipEffects[0].Right.TimelineRange.Start, half) {
		t.Fatalf("preview=%+v", preview)
	}
}

func hasClipID(values []domain.ClipID, expected domain.ClipID) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func bindFixtureClipAlignment(
	t *testing.T,
	fixture captionDerivationIntegrationFixture,
	localName string,
) domain.AlignmentID {
	t.Helper()
	alignmentLocal, err := domain.ParseLocalID(localName)
	if err != nil {
		t.Fatal(err)
	}
	localRange := domain.TimeRange{Start: mustRational(t, 0, 1), Duration: fixture.sourceRange.Duration}
	bound, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:"+localName), Intent: "Bind excerpt to the fixture clip",
			BaseProjectRevision: 4,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityNarrativeNode, ID: fixture.excerptID.String(), Revision: 1},
				{Kind: domain.EntityClip, ID: fixture.clipID.String(), Revision: 1},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditBindAlignment, CreateAs: &alignmentLocal,
				NarrativeNode: &application.EditReference{ID: fixture.excerptID.String()},
				AlignmentTargets: []application.AlignmentTargetInput{{
					Type: domain.AlignmentTargetClip, Clip: &application.EditReference{ID: fixture.clipID.String()},
					LocalRange: &localRange,
				}},
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, bound.Proposal.ID,
		application.EditApplyInput{
			RequestID: mustRequestID(t, "agent:"+localName+"-apply"), ProposalDigest: bound.Proposal.Digest,
		},
	)
	if err != nil || committed.Transaction.CommittedProjectRevision.Value() != 5 {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	alignmentID, err := domain.ParseAlignmentID(allocationID(t, bound.Proposal, alignmentLocal))
	if err != nil {
		t.Fatal(err)
	}
	return alignmentID
}

func hasExactPrecondition(values []domain.EntityPrecondition, expected domain.EntityPrecondition) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func editJournalCounts(t *testing.T, databasePath string) (int, int) {
	t.Helper()
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var proposals, transactions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edit_proposals`).Scan(&proposals); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM edit_transactions`).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	return proposals, transactions
}

func TestSQLiteClipMoveAlignmentRemapApplyReadAndUndo(t *testing.T) {
	parallelAPITest(t)
	fixture := newCaptionDerivationIntegrationFixture(t)
	defer fixture.store.Close()
	alignmentLocal, _ := domain.ParseLocalID("move_alignment")
	localRange := domain.TimeRange{Start: mustRational(t, 0, 1), Duration: fixture.sourceRange.Duration}
	bound, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:move-bind"), Intent: "bind excerpt to movable clip",
			BaseProjectRevision: 4,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityNarrativeNode, ID: fixture.excerptID.String(), Revision: 1},
				{Kind: domain.EntityClip, ID: fixture.clipID.String(), Revision: 1},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditBindAlignment, CreateAs: &alignmentLocal,
				NarrativeNode: &application.EditReference{ID: fixture.excerptID.String()},
				AlignmentTargets: []application.AlignmentTargetInput{{
					Type: domain.AlignmentTargetClip, Clip: &application.EditReference{ID: fixture.clipID.String()},
					LocalRange: &localRange,
				}},
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	boundCommit, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, bound.Proposal.ID,
		application.EditApplyInput{RequestID: mustRequestID(t, "agent:move-bind-apply"), ProposalDigest: bound.Proposal.Digest},
	)
	if err != nil || boundCommit.Transaction.CommittedProjectRevision.Value() != 5 {
		t.Fatalf("bound=%+v err=%v", boundCommit, err)
	}
	alignmentID, err := domain.ParseAlignmentID(allocationID(t, bound.Proposal, alignmentLocal))
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.ClipScopeSingle
	start := mustRational(t, 2, 1)
	moved, err := fixture.edits.Propose(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:move-propose"), Intent: "move clip without changing its meaning",
			BaseProjectRevision: boundCommit.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityClip, ID: fixture.clipID.String(), Revision: 1},
				{Kind: domain.EntityTrack, ID: fixture.audioTrack.ID.String(), Revision: 2},
				{Kind: domain.EntityAlignment, ID: alignmentID.String(), Revision: 1},
			},
			Operations: []application.EditOperationInput{
				{Type: domain.EditMoveClip, Clip: &application.EditReference{ID: fixture.clipID.String()},
					Scope: &scope, TrackID: &fixture.audioTrack.ID, TimelineStart: &start},
				{Type: domain.EditRemapAlignment, AlignmentID: &alignmentID,
					AlignmentTargets: []application.AlignmentTargetInput{{
						Type: domain.AlignmentTargetClip, Clip: &application.EditReference{ID: fixture.clipID.String()},
						LocalRange: &localRange,
					}},
				},
			},
		},
	)
	if err != nil || len(moved.Proposal.Operations) != 2 {
		t.Fatalf("moved=%+v err=%v", moved, err)
	}
	movedCommit, err := fixture.edits.Apply(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, moved.Proposal.ID,
		application.EditApplyInput{RequestID: mustRequestID(t, "agent:move-apply"), ProposalDigest: moved.Proposal.Digest},
	)
	if err != nil || movedCommit.Transaction.CommittedProjectRevision.Value() != 6 {
		t.Fatalf("commit=%+v err=%v", movedCommit, err)
	}
	clip := readClipEntity(t, fixture, fixture.clipID)
	alignment := readAlignmentEntity(t, fixture, alignmentID)
	if !sameTestRational(clip.TimelineRange.Start, start) || clip.Revision.Value() != 2 ||
		alignment.Status != domain.AlignmentExact || alignment.Revision.Value() != 2 ||
		len(alignment.Targets) != 1 || alignment.Targets[0].Clip.ClipRevision.Value() != 2 {
		t.Fatalf("clip=%+v alignment=%+v", clip, alignment)
	}
	undone, err := fixture.edits.Undo(
		fixture.agent, fixture.project.Project.Project.ID, fixture.project.Project.Project.MainSequenceID,
		fixture.run.Run.ID, fixture.run.Run.CurrentTurn.ID, movedCommit.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "agent:move-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 7 {
		t.Fatalf("undo=%+v err=%v", undone, err)
	}
	clip = readClipEntity(t, fixture, fixture.clipID)
	alignment = readAlignmentEntity(t, fixture, alignmentID)
	if !sameTestRational(clip.TimelineRange.Start, mustRational(t, 0, 1)) || clip.Revision.Value() != 3 ||
		alignment.Status != domain.AlignmentExact || alignment.Revision.Value() != 3 ||
		alignment.Targets[0].Clip.ClipRevision.Value() != 3 {
		t.Fatalf("undone clip=%+v alignment=%+v", clip, alignment)
	}
}

func readClipEntity(
	t *testing.T,
	fixture captionDerivationIntegrationFixture,
	id domain.ClipID,
) domain.ClipState {
	t.Helper()
	result, err := fixture.editReads.Entity(
		fixture.agent, fixture.project.Project.Project.ID, domain.EntityClip, id.String(),
	)
	if err != nil || result.Clip == nil {
		t.Fatalf("clip entity=%+v err=%v", result, err)
	}
	return *result.Clip
}

func sameTestRational(left, right domain.RationalTime) bool {
	comparison, err := left.Compare(right)
	return err == nil && comparison == 0
}
