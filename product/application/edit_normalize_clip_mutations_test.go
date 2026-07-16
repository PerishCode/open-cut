package application

import (
	"errors"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestNormalizeLinkedMovePreservesExactAlignmentThroughExplicitRemap(t *testing.T) {
	fixture := newClipMutationFixture(t)
	start, _ := domain.NewRationalTime(10, 1)
	scope := domain.ClipScopeLinked
	input := fixture.input(t, []EditOperationInput{
		{Type: domain.EditMoveClip, Clip: fixture.ref(fixture.video.ID.String()), Scope: &scope,
			TrackID: &fixture.video.TrackID, TimelineStart: &start},
		{Type: domain.EditRemapAlignment, AlignmentID: &fixture.alignment.ID,
			AlignmentTargets: fixture.existingAlignmentInputs(t)},
	}, nil)

	proposal, _, err := NormalizeEditProposal(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Operations) != 3 || proposal.Operations[0].Clip == nil || proposal.Operations[1].Clip == nil ||
		proposal.Operations[2].Alignment == nil || proposal.Operations[2].Alignment.Status != domain.AlignmentExact {
		t.Fatalf("operations=%+v", proposal.Operations)
	}
	for _, operation := range proposal.Operations[:2] {
		if operation.Clip.Revision.Value() != 2 || !sameRational(operation.Clip.TimelineRange.Start, start) {
			t.Fatalf("moved clip=%+v", operation.Clip)
		}
	}
	for _, target := range proposal.Operations[2].Alignment.Targets {
		if target.Clip == nil || target.Clip.ClipRevision.Value() != 2 {
			t.Fatalf("target=%+v", target)
		}
	}
}

func TestNormalizeTrimRejectsFalseExactCoverageAndAcceptsStale(t *testing.T) {
	fixture := newClipMutationFixture(t)
	scope := domain.ClipScopeSingle
	trimmed := testTimeRange(t, 1, 1, 4, 1)
	falseExact := fixture.input(t, []EditOperationInput{
		{Type: domain.EditTrimClip, Clip: fixture.ref(fixture.video.ID.String()), Scope: &scope,
			SourceRange: &trimmed, TimelineRange: &trimmed},
		{Type: domain.EditRemapAlignment, AlignmentID: &fixture.alignment.ID, AlignmentTargets: []AlignmentTargetInput{{
			Type: domain.AlignmentTargetClip, Clip: fixture.ref(fixture.video.ID.String()),
			LocalRange: clipRangePointer(testTimeRange(t, 0, 1, 4, 1)),
		}}},
	}, nil)
	if _, _, err := NormalizeEditProposal(falseExact); !errors.Is(err, ErrEditInvalid) {
		t.Fatalf("false exact error=%v", err)
	}

	stale := fixture.input(t, []EditOperationInput{
		{Type: domain.EditTrimClip, Clip: fixture.ref(fixture.video.ID.String()), Scope: &scope,
			SourceRange: &trimmed, TimelineRange: &trimmed},
		{Type: domain.EditMarkAlignmentStale, AlignmentID: &fixture.alignment.ID},
	}, nil)
	proposal, _, err := NormalizeEditProposal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Operations[len(proposal.Operations)-1].Alignment.Status != domain.AlignmentStale {
		t.Fatalf("operations=%+v", proposal.Operations)
	}
}

func TestNormalizeLinkedSplitUsesExplicitIdentitiesAndCoverageRemap(t *testing.T) {
	fixture := newClipMutationFixture(t)
	scope := domain.ClipScopeLinked
	splitAt, _ := domain.NewRationalTime(2, 1)
	videoLeft, videoRight := mustCaptionLocal(t, "video_left"), mustCaptionLocal(t, "video_right")
	audioLeft, audioRight := mustCaptionLocal(t, "audio_left"), mustCaptionLocal(t, "audio_right")
	leftGroup, rightGroup := mustCaptionLocal(t, "left_group"), mustCaptionLocal(t, "right_group")
	outputs := []ClipSplitOutputInput{
		{Clip: *fixture.ref(fixture.video.ID.String()), LeftAs: videoLeft, RightAs: videoRight},
		{Clip: *fixture.ref(fixture.audio.ID.String()), LeftAs: audioLeft, RightAs: audioRight},
	}
	operations := []EditOperationInput{
		{Type: domain.EditSplitClip, Clip: fixture.ref(fixture.video.ID.String()), Scope: &scope, SplitAt: &splitAt,
			SplitOutputs: outputs, LeftLinkGroupAs: &leftGroup, RightLinkGroupAs: &rightGroup},
		{Type: domain.EditRemapAlignment, AlignmentID: &fixture.alignment.ID,
			AlignmentTargets: []AlignmentTargetInput{
				fixture.localClipTarget(t, videoLeft, 0, 2), fixture.localClipTarget(t, videoRight, 0, 3),
				fixture.localClipTarget(t, audioLeft, 0, 2), fixture.localClipTarget(t, audioRight, 0, 3),
			}},
	}
	allocation := []domain.LocalAllocation{
		fixture.allocation(videoLeft, domain.EntityClip, "018f0000-0000-7000-8000-000000000031"),
		fixture.allocation(videoRight, domain.EntityClip, "018f0000-0000-7000-8000-000000000032"),
		fixture.allocation(audioLeft, domain.EntityClip, "018f0000-0000-7000-8000-000000000033"),
		fixture.allocation(audioRight, domain.EntityClip, "018f0000-0000-7000-8000-000000000034"),
		fixture.allocation(leftGroup, domain.EntityLinkGroup, "018f0000-0000-7000-8000-000000000035"),
		fixture.allocation(rightGroup, domain.EntityLinkGroup, "018f0000-0000-7000-8000-000000000036"),
	}
	proposal, _, err := NormalizeEditProposal(fixture.input(t, operations, allocation))
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Operations) != 10 || proposal.Operations[0].LinkGroup == nil ||
		proposal.Operations[1].LinkGroup == nil || proposal.Operations[9].Alignment == nil ||
		len(proposal.Operations[9].Alignment.Targets) != 4 {
		t.Fatalf("operations=%+v", proposal.Operations)
	}
}

func TestNormalizeSingleRemoveDissolvesDegenerateGroup(t *testing.T) {
	fixture := newClipMutationFixture(t)
	scope := domain.ClipScopeSingle
	proposal, _, err := NormalizeEditProposal(fixture.input(t, []EditOperationInput{
		{Type: domain.EditRemoveClip, Clip: fixture.ref(fixture.video.ID.String()), Scope: &scope},
		{Type: domain.EditMarkAlignmentStale, AlignmentID: &fixture.alignment.ID},
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Operations) != 4 || proposal.Operations[0].Clip == nil ||
		!proposal.Operations[0].Clip.Tombstoned || proposal.Operations[1].Clip == nil ||
		proposal.Operations[1].Clip.LinkGroupID != nil || proposal.Operations[2].LinkGroup == nil ||
		!proposal.Operations[2].LinkGroup.Tombstoned || proposal.Operations[3].Alignment == nil ||
		proposal.Operations[3].Alignment.Status != domain.AlignmentStale {
		t.Fatalf("operations=%+v", proposal.Operations)
	}
}

func TestNormalizeExplicitLinkAndUnlinkCloseMembership(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		fixture := newClipMutationFixture(t)
		fixture.video.LinkGroupID = nil
		fixture.audio.LinkGroupID = nil
		groupLocal := mustCaptionLocal(t, "linked_group")
		allocation := []domain.LocalAllocation{fixture.allocation(
			groupLocal, domain.EntityLinkGroup, "018f0000-0000-7000-8000-000000000040",
		)}
		proposal, _, err := NormalizeEditProposal(fixture.input(t, []EditOperationInput{
			{Type: domain.EditLinkClips, CreateLinkGroupAs: &groupLocal,
				Clips: []EditReference{*fixture.ref(fixture.video.ID.String()), *fixture.ref(fixture.audio.ID.String())}},
			{Type: domain.EditRemapAlignment, AlignmentID: &fixture.alignment.ID,
				AlignmentTargets: fixture.existingAlignmentInputs(t)},
		}, allocation))
		if err != nil || len(proposal.Operations) != 4 || proposal.Operations[0].LinkGroup == nil ||
			proposal.Operations[1].Clip == nil || proposal.Operations[1].Clip.LinkGroupID == nil ||
			proposal.Operations[2].Clip == nil || proposal.Operations[2].Clip.LinkGroupID == nil {
			t.Fatalf("operations=%+v err=%v", proposal.Operations, err)
		}
	})

	t.Run("unlink", func(t *testing.T) {
		fixture := newClipMutationFixture(t)
		proposal, _, err := NormalizeEditProposal(fixture.input(t, []EditOperationInput{
			{Type: domain.EditUnlinkClips, LinkGroup: fixture.ref(fixture.group.ID.String())},
			{Type: domain.EditMarkAlignmentStale, AlignmentID: &fixture.alignment.ID},
		}, nil))
		if err != nil || len(proposal.Operations) != 4 || proposal.Operations[0].Clip.LinkGroupID != nil ||
			proposal.Operations[1].Clip.LinkGroupID != nil || proposal.Operations[2].LinkGroup == nil ||
			!proposal.Operations[2].LinkGroup.Tombstoned {
			t.Fatalf("operations=%+v err=%v", proposal.Operations, err)
		}
	})
}

type clipMutationFixture struct {
	videoTrack, audioTrack   EditTrackState
	videoStream, audioStream EditSourceStreamState
	video, audio             domain.ClipState
	group                    domain.LinkGroupState
	alignment                domain.AlignmentState
	projectID                domain.ProjectID
	sequenceID               domain.SequenceID
	documentID               domain.NarrativeDocumentID
	nodeID                   domain.NarrativeNodeID
}

func newClipMutationFixture(t *testing.T) clipMutationFixture {
	projectID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	documentID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000002", domain.ParseNarrativeDocumentID)
	sequenceID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000003", domain.ParseSequenceID)
	videoTrackID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000004", domain.ParseTrackID)
	audioTrackID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000005", domain.ParseTrackID)
	assetID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000006", domain.ParseAssetID)
	videoStreamID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000007", domain.ParseSourceStreamID)
	audioStreamID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000008", domain.ParseSourceStreamID)
	videoID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000009", domain.ParseClipID)
	audioID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000010", domain.ParseClipID)
	groupID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000011", domain.ParseLinkGroupID)
	nodeID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000012", domain.ParseNarrativeNodeID)
	alignmentID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000013", domain.ParseAlignmentID)
	rangeValue := testTimeRange(t, 0, 1, 5, 1)
	group := domain.LinkGroupState{ID: groupID, Revision: 1, SequenceID: sequenceID}
	video := domain.ClipState{ID: videoID, Revision: 1, SequenceID: sequenceID, TrackID: videoTrackID,
		AssetID: assetID, SourceStreamID: videoStreamID, SourceRange: rangeValue, TimelineRange: rangeValue,
		Enabled: true, LinkGroupID: &groupID}
	audio := domain.ClipState{ID: audioID, Revision: 1, SequenceID: sequenceID, TrackID: audioTrackID,
		AssetID: assetID, SourceStreamID: audioStreamID, SourceRange: rangeValue, TimelineRange: rangeValue,
		Enabled: true, LinkGroupID: &groupID}
	alignment := domain.AlignmentState{ID: alignmentID, Revision: 1, NarrativeNodeID: nodeID,
		NarrativeNodeRevision: 1, SequenceID: sequenceID, Status: domain.AlignmentExact,
		Targets: []domain.AlignmentTarget{
			{Type: domain.AlignmentTargetClip, Clip: &domain.ClipAlignmentTarget{ClipID: videoID, ClipRevision: 1, LocalRange: rangeValue}},
			{Type: domain.AlignmentTargetClip, Clip: &domain.ClipAlignmentTarget{ClipID: audioID, ClipRevision: 1, LocalRange: rangeValue}},
		}}
	return clipMutationFixture{
		projectID: projectID, sequenceID: sequenceID, documentID: documentID, nodeID: nodeID,
		videoTrack: EditTrackState{ID: videoTrackID, SequenceID: sequenceID, Revision: 1, Type: domain.TrackVideo},
		audioTrack: EditTrackState{ID: audioTrackID, SequenceID: sequenceID, Revision: 1, Type: domain.TrackAudio},
		videoStream: EditSourceStreamState{ID: videoStreamID, AssetID: assetID, AssetRevision: 1,
			Descriptor: domain.SourceStreamDescriptor{MediaType: domain.MediaVideo}},
		audioStream: EditSourceStreamState{ID: audioStreamID, AssetID: assetID, AssetRevision: 1,
			Descriptor: domain.SourceStreamDescriptor{MediaType: domain.MediaAudio}},
		video: video, audio: audio, group: group, alignment: alignment,
	}
}

func (fixture clipMutationFixture) input(
	t *testing.T,
	operations []EditOperationInput,
	allocation []domain.LocalAllocation,
) NormalizeEditInput {
	proposalID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000020", domain.ParseProposalID)
	runID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000021", domain.ParseRunID)
	turnID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000022", domain.ParseTurnID)
	agentID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000023", domain.ParseAgentID)
	preconditions := []domain.EntityPrecondition{
		{Kind: domain.EntityClip, ID: fixture.video.ID.String(), Revision: fixture.video.Revision},
		{Kind: domain.EntityClip, ID: fixture.audio.ID.String(), Revision: fixture.audio.Revision},
		{Kind: domain.EntityLinkGroup, ID: fixture.group.ID.String(), Revision: fixture.group.Revision},
		{Kind: domain.EntityTrack, ID: fixture.videoTrack.ID.String(), Revision: fixture.videoTrack.Revision},
		{Kind: domain.EntityTrack, ID: fixture.audioTrack.ID.String(), Revision: fixture.audioTrack.Revision},
		{Kind: domain.EntityAlignment, ID: fixture.alignment.ID.String(), Revision: fixture.alignment.Revision},
	}
	return NormalizeEditInput{
		ProposalID: proposalID, ProjectID: fixture.projectID, SequenceID: fixture.sequenceID,
		RunID: runID, TurnID: turnID, Actor: domain.AgentActor(agentID), Allocation: allocation,
		Input: EditProposeInput{RequestID: mustCaptionRequest(t, "clip-mutation-1"), Intent: "Edit linked clips",
			BaseProjectRevision: 1, Preconditions: preconditions, Operations: operations},
		CreatedAt: time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC),
		State: EditNormalizationState{
			ProjectID: fixture.projectID, ProjectRevision: 1, DocumentID: fixture.documentID, DocumentRevision: 1,
			SequenceID: fixture.sequenceID, SequenceRevision: 1,
			Tracks: map[string]EditTrackState{
				fixture.videoTrack.ID.String(): fixture.videoTrack, fixture.audioTrack.ID.String(): fixture.audioTrack,
			},
			Clips: map[string]domain.ClipState{
				fixture.video.ID.String(): fixture.video, fixture.audio.ID.String(): fixture.audio,
			},
			LinkGroups: map[string]domain.LinkGroupState{fixture.group.ID.String(): fixture.group},
			LinkGroupClips: map[string][]domain.ClipID{
				fixture.group.ID.String(): {fixture.video.ID, fixture.audio.ID},
			},
			SourceStreams: map[string]EditSourceStreamState{
				fixture.videoStream.ID.String(): fixture.videoStream, fixture.audioStream.ID.String(): fixture.audioStream,
			},
			Alignments: map[string]domain.AlignmentState{fixture.alignment.ID.String(): fixture.alignment},
			ClipAlignments: map[string][]domain.AlignmentID{
				fixture.video.ID.String(): {fixture.alignment.ID}, fixture.audio.ID.String(): {fixture.alignment.ID},
			},
			Sections: map[string]domain.NarrativeSectionState{}, AuthoredTexts: map[string]domain.AuthoredTextState{},
			SourceExcerpts:        map[string]domain.SourceExcerptState{},
			TranscriptCorrections: map[string]domain.TranscriptCorrectionState{},
			TranscriptArtifacts:   map[string]EditTranscriptArtifactState{}, Captions: map[string]domain.CaptionState{},
			NodeAlignments: map[string][]domain.AlignmentID{}, CaptionAlignments: map[string][]domain.AlignmentID{},
		},
	}
}

func (fixture clipMutationFixture) ref(value string) *EditReference {
	return &EditReference{ID: value}
}

func (fixture clipMutationFixture) existingAlignmentInputs(t *testing.T) []AlignmentTargetInput {
	rangeValue := clipRangePointer(testTimeRange(t, 0, 1, 5, 1))
	return []AlignmentTargetInput{
		{Type: domain.AlignmentTargetClip, Clip: fixture.ref(fixture.video.ID.String()), LocalRange: rangeValue},
		{Type: domain.AlignmentTargetClip, Clip: fixture.ref(fixture.audio.ID.String()), LocalRange: rangeValue},
	}
}

func (fixture clipMutationFixture) localClipTarget(
	t *testing.T,
	local domain.LocalID,
	start, duration int64,
) AlignmentTargetInput {
	return AlignmentTargetInput{Type: domain.AlignmentTargetClip, Clip: &EditReference{Local: &local},
		LocalRange: clipRangePointer(testTimeRange(t, start, 1, duration, 1))}
}

func (fixture clipMutationFixture) allocation(
	local domain.LocalID,
	kind domain.EditEntityKind,
	value string,
) domain.LocalAllocation {
	return domain.LocalAllocation{Local: local, Kind: kind, ID: value}
}

func clipRangePointer(value domain.TimeRange) *domain.TimeRange { return &value }

func sameRational(left, right domain.RationalTime) bool {
	comparison, err := left.Compare(right)
	return err == nil && comparison == 0
}
