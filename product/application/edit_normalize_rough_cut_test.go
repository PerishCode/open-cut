package application

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestNormalizeRoughCutRechecksAndExpandsExactPrimitives(t *testing.T) {
	fixture := newRoughCutNormalizationFixture(t)
	operation, err := BuildRoughCutOperation(
		fixture.state, testRational(t, 10, 1), mustCaptionLocal(t, "rough"), fixture.items,
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, _, err := NormalizeEditProposal(fixture.normalizeInput(t, operation))
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Operations) != 6 || proposal.Operations[0].Type != domain.NormalizedPutLinkGroup ||
		proposal.Operations[1].Type != domain.NormalizedPutClip ||
		proposal.Operations[2].Type != domain.NormalizedPutClip ||
		proposal.Operations[3].Type != domain.NormalizedPutAlignment ||
		proposal.Operations[4].Type != domain.NormalizedPutClip ||
		proposal.Operations[5].Type != domain.NormalizedPutAlignment {
		t.Fatalf("operations=%+v", proposal.Operations)
	}
	if len(proposal.Operations[3].Alignment.Targets) != 2 ||
		proposal.Operations[1].Clip.LinkGroupID == nil || proposal.Operations[2].Clip.LinkGroupID == nil ||
		*proposal.Operations[1].Clip.LinkGroupID != *proposal.Operations[2].Clip.LinkGroupID ||
		proposal.Operations[4].Clip.LinkGroupID != nil {
		t.Fatalf("linked output=%+v", proposal.Operations)
	}
	firstEnd, _ := proposal.Operations[1].Clip.TimelineRange.End()
	if !sameRational(firstEnd, proposal.Operations[4].Clip.TimelineRange.Start) {
		t.Fatalf("rough cut is not zero-gap: %+v", proposal.Operations)
	}
}

func TestNormalizeRoughCutRejectsOutputTamperingEvenWithUpdatedDigest(t *testing.T) {
	fixture := newRoughCutNormalizationFixture(t)
	operation, err := BuildRoughCutOperation(
		fixture.state, testRational(t, 10, 1), mustCaptionLocal(t, "rough"), fixture.items,
	)
	if err != nil {
		t.Fatal(err)
	}
	operation.DerivedRoughCut[0].TimelineRange.Start = testRational(t, 11, 1)
	digest, err := roughCutOperationDigest(operation)
	if err != nil {
		t.Fatal(err)
	}
	operation.RoughCutOutputDigest = &digest
	if _, _, err := NormalizeEditProposal(fixture.normalizeInput(t, operation)); !errors.Is(err, ErrEditInvalid) {
		t.Fatalf("tamper error=%v", err)
	}
}

func TestBuildRoughCutRejectsStaleExcerptAndCommittedOverlap(t *testing.T) {
	fixture := newRoughCutNormalizationFixture(t)
	fixture.state.SourceExcerptEvidence[fixture.excerpts[0].ID.String()] = domain.SourceExcerptEvidenceStale
	if _, err := BuildRoughCutOperation(
		fixture.state, testRational(t, 0, 1), mustCaptionLocal(t, "rough"), fixture.items,
	); !errors.Is(err, ErrEditInvalid) {
		t.Fatalf("stale error=%v", err)
	}

	fixture = newRoughCutNormalizationFixture(t)
	operation, err := BuildRoughCutOperation(
		fixture.state, testRational(t, 10, 1), mustCaptionLocal(t, "rough"), fixture.items,
	)
	if err != nil {
		t.Fatal(err)
	}
	existingID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000060", domain.ParseClipID)
	fixture.state.Clips[existingID.String()] = domain.ClipState{
		ID: existingID, Revision: 1, SequenceID: fixture.sequenceID, TrackID: fixture.videoTrack.ID,
		AssetID: fixture.asset, SourceStreamID: fixture.videoStream.ID,
		SourceRange: testTimeRange(t, 0, 1, 1, 1), TimelineRange: testTimeRange(t, 10, 1, 1, 1), Enabled: true,
	}
	if _, _, err := NormalizeEditProposal(fixture.normalizeInput(t, operation)); !errors.Is(err, ErrEditInvalid) {
		t.Fatalf("overlap error=%v", err)
	}
}

type roughCutNormalizationFixture struct {
	project     domain.ProjectID
	document    domain.NarrativeDocumentID
	sequenceID  domain.SequenceID
	asset       domain.AssetID
	videoTrack  EditTrackState
	audioTrack  EditTrackState
	videoStream EditSourceStreamState
	audioStream EditSourceStreamState
	excerpts    []domain.SourceExcerptState
	items       []RoughCutDerivationItemInput
	state       EditNormalizationState
}

func newRoughCutNormalizationFixture(t *testing.T) roughCutNormalizationFixture {
	project := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	document := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000002", domain.ParseNarrativeDocumentID)
	sequence := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000003", domain.ParseSequenceID)
	videoTrackID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000004", domain.ParseTrackID)
	audioTrackID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000005", domain.ParseTrackID)
	asset := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000006", domain.ParseAssetID)
	videoStreamID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000007", domain.ParseSourceStreamID)
	audioStreamID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000008", domain.ParseSourceStreamID)
	excerptOneID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000009", domain.ParseNarrativeNodeID)
	excerptTwoID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000010", domain.ParseNarrativeNodeID)
	fingerprint, _ := domain.ParseDigest("sha256:" + strings.Repeat("a", 64))
	language, _ := domain.ParseCaptionLanguage("en")
	excerpts := []domain.SourceExcerptState{
		{ID: excerptOneID, Revision: 2, DocumentID: document, AssetID: asset,
			AcceptedFingerprint: fingerprint, SourceRange: testTimeRange(t, -2, 1, 3, 1),
			Language: language, EffectiveText: "first", Evidence: domain.SourceExcerptTranscriptEvidence{}},
		{ID: excerptTwoID, Revision: 3, DocumentID: document, AssetID: asset,
			AcceptedFingerprint: fingerprint, SourceRange: testTimeRange(t, 2, 1, 2, 1),
			Language: language, EffectiveText: "second", Evidence: domain.SourceExcerptTranscriptEvidence{}},
	}
	startTime := testRational(t, -2, 1)
	duration := testRational(t, 7, 1)
	videoTrack := EditTrackState{ID: videoTrackID, SequenceID: sequence, Revision: 4, Type: domain.TrackVideo}
	audioTrack := EditTrackState{ID: audioTrackID, SequenceID: sequence, Revision: 5, Type: domain.TrackAudio}
	videoStream := EditSourceStreamState{ID: videoStreamID, AssetID: asset, AssetRevision: 6,
		Descriptor: domain.SourceStreamDescriptor{MediaType: domain.MediaVideo, StartTime: &startTime, Duration: &duration}}
	audioStream := EditSourceStreamState{ID: audioStreamID, AssetID: asset, AssetRevision: 6,
		Descriptor: domain.SourceStreamDescriptor{MediaType: domain.MediaAudio, StartTime: &startTime, Duration: &duration}}
	state := EditNormalizationState{
		ProjectID: project, ProjectRevision: 7, DocumentID: document, DocumentRevision: 8,
		SequenceID: sequence, SequenceRevision: 9,
		Tracks: map[string]EditTrackState{videoTrackID.String(): videoTrack, audioTrackID.String(): audioTrack},
		SourceExcerpts: map[string]domain.SourceExcerptState{
			excerptOneID.String(): excerpts[0], excerptTwoID.String(): excerpts[1],
		},
		SourceExcerptEvidence: map[string]domain.SourceExcerptEvidenceStatus{
			excerptOneID.String(): domain.SourceExcerptEvidenceExact,
			excerptTwoID.String(): domain.SourceExcerptEvidenceExact,
		},
		SourceStreams: map[string]EditSourceStreamState{
			videoStreamID.String(): videoStream, audioStreamID.String(): audioStream,
		},
		Sections: map[string]domain.NarrativeSectionState{}, AuthoredTexts: map[string]domain.AuthoredTextState{},
		TranscriptCorrections: map[string]domain.TranscriptCorrectionState{},
		TranscriptArtifacts:   map[string]EditTranscriptArtifactState{}, Captions: map[string]domain.CaptionState{},
		Clips: map[string]domain.ClipState{}, LinkGroups: map[string]domain.LinkGroupState{},
		LinkGroupClips: map[string][]domain.ClipID{}, Alignments: map[string]domain.AlignmentState{},
		NodeAlignments: map[string][]domain.AlignmentID{}, CaptionAlignments: map[string][]domain.AlignmentID{},
		ClipAlignments: map[string][]domain.AlignmentID{},
	}
	items := []RoughCutDerivationItemInput{
		{SourceExcerptID: excerptOneID,
			Video: &RoughCutLaneBindingInput{TrackID: videoTrackID, SourceStreamID: videoStreamID},
			Audio: &RoughCutLaneBindingInput{TrackID: audioTrackID, SourceStreamID: audioStreamID}},
		{SourceExcerptID: excerptTwoID,
			Video: &RoughCutLaneBindingInput{TrackID: videoTrackID, SourceStreamID: videoStreamID}},
	}
	return roughCutNormalizationFixture{
		project: project, document: document, sequenceID: sequence, asset: asset,
		videoTrack: videoTrack, audioTrack: audioTrack, videoStream: videoStream, audioStream: audioStream,
		excerpts: excerpts, items: items, state: state,
	}
}

func (fixture roughCutNormalizationFixture) normalizeInput(
	t *testing.T,
	operation EditOperationInput,
) NormalizeEditInput {
	allocations := []domain.LocalAllocation{
		{Local: operation.DerivedRoughCut[0].AlignmentAs, Kind: domain.EntityAlignment, ID: "018f0000-0000-7000-8000-000000000020"},
		{Local: operation.DerivedRoughCut[0].Video.ClipAs, Kind: domain.EntityClip, ID: "018f0000-0000-7000-8000-000000000021"},
		{Local: operation.DerivedRoughCut[0].Audio.ClipAs, Kind: domain.EntityClip, ID: "018f0000-0000-7000-8000-000000000022"},
		{Local: *operation.DerivedRoughCut[0].LinkGroupAs, Kind: domain.EntityLinkGroup, ID: "018f0000-0000-7000-8000-000000000023"},
		{Local: operation.DerivedRoughCut[1].AlignmentAs, Kind: domain.EntityAlignment, ID: "018f0000-0000-7000-8000-000000000024"},
		{Local: operation.DerivedRoughCut[1].Video.ClipAs, Kind: domain.EntityClip, ID: "018f0000-0000-7000-8000-000000000025"},
	}
	preconditions := []domain.EntityPrecondition{
		{Kind: domain.EntitySequence, ID: fixture.sequenceID.String(), Revision: fixture.state.SequenceRevision},
		{Kind: domain.EntityNarrativeNode, ID: fixture.excerpts[0].ID.String(), Revision: fixture.excerpts[0].Revision},
		{Kind: domain.EntityNarrativeNode, ID: fixture.excerpts[1].ID.String(), Revision: fixture.excerpts[1].Revision},
		{Kind: domain.EntityTrack, ID: fixture.videoTrack.ID.String(), Revision: fixture.videoTrack.Revision},
		{Kind: domain.EntityTrack, ID: fixture.audioTrack.ID.String(), Revision: fixture.audioTrack.Revision},
		{Kind: domain.EntityAsset, ID: fixture.asset.String(), Revision: fixture.videoStream.AssetRevision},
	}
	return NormalizeEditInput{
		ProposalID: mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000030", domain.ParseProposalID),
		ProjectID:  fixture.project, SequenceID: fixture.sequenceID,
		RunID:      mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000031", domain.ParseRunID),
		TurnID:     mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000032", domain.ParseTurnID),
		Actor:      domain.AgentActor(mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000033", domain.ParseAgentID)),
		Allocation: allocations, CreatedAt: time.Unix(1_700_000_000, 0).UTC(), State: fixture.state,
		Input: EditProposeInput{
			RequestID: mustCaptionRequest(t, "derive-rough-cut-test"), Intent: "derive exact rough cut",
			BaseProjectRevision: fixture.state.ProjectRevision, Preconditions: preconditions,
			Operations: []EditOperationInput{operation},
		},
	}
}

func testRational(t *testing.T, value int64, scale int32) domain.RationalTime {
	t.Helper()
	result, err := domain.NewRationalTime(value, scale)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
