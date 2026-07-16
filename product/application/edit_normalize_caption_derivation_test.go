package application

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestDeriveCaptionCuesUsesExactEvidenceAndForwardPadding(t *testing.T) {
	fixture := newCaptionDerivationFixture(t)
	cues, err := DeriveCaptionCues(
		fixture.artifact, fixture.excerpt, fixture.corrections, fixture.clip, domain.ReadableCaptionPolicyV1(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("cues = %+v", cues)
	}
	if cues[0].Text != "Hi" || !equalTimeRange(cues[0].SourceRange, testTimeRange(t, 0, 1, 1, 1)) ||
		!equalTimeRange(cues[0].TimelineRange, testTimeRange(t, 10, 1, 1, 1)) ||
		!equalTimeRange(cues[0].EvidenceSourceRange, testTimeRange(t, 0, 1, 2, 5)) {
		t.Fatalf("first cue = %+v", cues[0])
	}
	if cues[1].Text != "Better take." ||
		!equalTimeRange(cues[1].SourceRange, testTimeRange(t, 7, 5, 11, 10)) ||
		!equalTimeRange(cues[1].TimelineRange, testTimeRange(t, 57, 5, 11, 10)) ||
		len(cues[1].CorrectionRevisions) != 1 ||
		cues[1].CorrectionRevisions[0].ID != fixture.correction.ID ||
		cues[1].CorrectionRevisions[0].Revision != fixture.correction.Revision {
		t.Fatalf("second cue = %+v", cues[1])
	}
}

func TestCaptionWrappingCountsUnicodeGraphemeClusters(t *testing.T) {
	policy := domain.ReadableCaptionPolicyV1()
	combined, err := wrapCaptionUnits([]captionEvidenceUnit{
		{text: strings.Repeat("a", 30)},
		{text: " " + strings.Repeat("b", 20)},
	}, policy)
	if err != nil || combined != strings.Repeat("a", 30)+"\n"+strings.Repeat("b", 20) {
		t.Fatalf("combined = %q err=%v", combined, err)
	}
	clusters := strings.Repeat("e\u0301", 42)
	if value, err := wrapCaptionUnits([]captionEvidenceUnit{{text: clusters}}, policy); err != nil || value != clusters {
		t.Fatalf("42 EGC value = %q err=%v", value, err)
	}
	if _, err := wrapCaptionUnits([]captionEvidenceUnit{{text: clusters + "x"}}, policy); !errors.Is(err, ErrEditInvalid) {
		t.Fatalf("43 EGC error = %v", err)
	}
}

func TestDeriveCaptionCuesDoesNotSplitCorrectionReplacement(t *testing.T) {
	fixture := newCaptionDerivationFixture(t)
	tooLong := strings.Repeat("x", 43)
	fixture.correction.ReplacementText = tooLong
	fixture.corrections[fixture.correction.ID.String()] = fixture.correction
	fixture.excerpt.EffectiveText = "Hi " + tooLong
	if _, err := DeriveCaptionCues(
		fixture.artifact, fixture.excerpt, fixture.corrections, fixture.clip, domain.ReadableCaptionPolicyV1(),
	); !errors.Is(err, ErrEditInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeCaptionDerivationExpandsPairsAndRejectsTampering(t *testing.T) {
	fixture := newCaptionDerivationFixture(t)
	input := fixture.normalizeInput(t)
	proposal, _, err := NormalizeEditProposal(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Operations) != 4 ||
		proposal.Operations[0].Type != domain.NormalizedPutCaption ||
		proposal.Operations[1].Type != domain.NormalizedPutAlignment ||
		proposal.Operations[2].Type != domain.NormalizedPutCaption ||
		proposal.Operations[3].Type != domain.NormalizedPutAlignment {
		t.Fatalf("operations = %+v", proposal.Operations)
	}
	first := proposal.Operations[0].Caption
	if first == nil || first.Provenance.Kind != domain.CaptionProvenanceTranscriptDerivation ||
		first.Provenance.Derivation == nil || first.Provenance.Derivation.SourceExcerptID != fixture.excerpt.ID ||
		first.Provenance.Derivation.ClipID != fixture.clip.ID ||
		first.Provenance.Derivation.Policy.Validate() != nil {
		t.Fatalf("caption = %+v", first)
	}

	tampered := fixture.normalizeInput(t)
	tampered.Input.Operations[0].DerivedCaptions[0].Text += "!"
	if _, _, err := NormalizeEditProposal(tampered); !errors.Is(err, ErrEditInvalid) {
		t.Fatalf("tampered error = %v", err)
	}
}

type captionDerivationFixture struct {
	project     domain.ProjectID
	document    domain.NarrativeDocumentID
	sequence    domain.SequenceID
	track       EditTrackState
	asset       domain.AssetID
	stream      domain.SourceStreamID
	artifact    EditTranscriptArtifactState
	excerpt     domain.SourceExcerptState
	correction  domain.TranscriptCorrectionState
	corrections map[string]domain.TranscriptCorrectionState
	clip        domain.ClipState
}

func newCaptionDerivationFixture(t *testing.T) captionDerivationFixture {
	t.Helper()
	project := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	document := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000002", domain.ParseNarrativeDocumentID)
	sequence := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000003", domain.ParseSequenceID)
	trackID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000004", domain.ParseTrackID)
	asset := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000005", domain.ParseAssetID)
	stream := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000006", domain.ParseSourceStreamID)
	artifactID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000007", domain.ParseArtifactID)
	segmentOne := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000008", domain.ParseTranscriptSegmentID)
	segmentTwo := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000009", domain.ParseTranscriptSegmentID)
	tokenOne := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000010", domain.ParseTranscriptTokenID)
	tokenTwo := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000011", domain.ParseTranscriptTokenID)
	tokenThree := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000012", domain.ParseTranscriptTokenID)
	excerptID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000013", domain.ParseNarrativeNodeID)
	correctionID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000014", domain.ParseTranscriptCorrectionID)
	clipID := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000015", domain.ParseClipID)
	language, err := domain.ParseCaptionLanguage("en")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := domain.ParseDigest("sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	segmentOneState := EditTranscriptSegmentState{
		ID: segmentOne, Ordinal: 0, SourceRange: testTimeRange(t, 0, 1, 2, 5), Text: "Hi",
		Tokens: []domain.TranscriptToken{{
			ID: tokenOne, Ordinal: 0, SourceRange: testTimeRange(t, 0, 1, 2, 5), Text: "Hi",
		}},
	}
	segmentTwoState := EditTranscriptSegmentState{
		ID: segmentTwo, Ordinal: 1, SourceRange: testTimeRange(t, 7, 5, 11, 10), Text: " Bad take.",
		Tokens: []domain.TranscriptToken{
			{ID: tokenTwo, Ordinal: 0, SourceRange: testTimeRange(t, 7, 5, 1, 2), Text: " Bad"},
			{ID: tokenThree, Ordinal: 1, SourceRange: testTimeRange(t, 19, 10, 3, 5), Text: " take."},
		},
	}
	artifact := EditTranscriptArtifactState{
		ID: artifactID, AssetID: asset, Fingerprint: fingerprint, SourceStreamID: stream, Language: language,
		Segments: map[string]EditTranscriptSegmentState{
			segmentOne.String(): segmentOneState,
			segmentTwo.String(): segmentTwoState,
		},
	}
	correction := domain.TranscriptCorrectionState{
		ID: correctionID, Revision: 6, AssetID: asset, ArtifactID: artifactID,
		SegmentIDs: []domain.TranscriptSegmentID{segmentTwo}, SourceRange: segmentTwoState.SourceRange,
		ReplacementText: "Better take.", Language: language,
	}
	excerpt := domain.SourceExcerptState{
		ID: excerptID, Revision: 5, DocumentID: document,
		AssetID: asset, AcceptedFingerprint: fingerprint, SourceRange: testTimeRange(t, 0, 1, 5, 2),
		Language: language, EffectiveText: "Hi Better take.",
		Evidence: domain.SourceExcerptTranscriptEvidence{
			ArtifactID: artifactID, SourceStreamID: stream,
			SegmentIDs:          []domain.TranscriptSegmentID{segmentOne, segmentTwo},
			CorrectionRevisions: []domain.TranscriptCorrectionRevisionRef{{ID: correctionID, Revision: 6}},
		},
	}
	clip := domain.ClipState{
		ID: clipID, Revision: 4, SequenceID: sequence, TrackID: trackID,
		AssetID: asset, SourceStreamID: stream, SourceRange: testTimeRange(t, 0, 1, 3, 1),
		TimelineRange: testTimeRange(t, 10, 1, 3, 1), Enabled: true,
	}
	return captionDerivationFixture{
		project: project, document: document, sequence: sequence,
		track: EditTrackState{ID: trackID, SequenceID: sequence, Revision: 2, Type: domain.TrackCaption},
		asset: asset, stream: stream, artifact: artifact, excerpt: excerpt, correction: correction,
		corrections: map[string]domain.TranscriptCorrectionState{correctionID.String(): correction}, clip: clip,
	}
}

func (fixture captionDerivationFixture) normalizeInput(t *testing.T) NormalizeEditInput {
	t.Helper()
	cues, err := DeriveCaptionCues(
		fixture.artifact, fixture.excerpt, fixture.corrections, fixture.clip, domain.ReadableCaptionPolicyV1(),
	)
	if err != nil {
		t.Fatal(err)
	}
	captionOne := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000021", domain.ParseCaptionID)
	alignmentOne := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000022", domain.ParseAlignmentID)
	captionTwo := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000023", domain.ParseCaptionID)
	alignmentTwo := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000024", domain.ParseAlignmentID)
	proposal := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000025", domain.ParseProposalID)
	run := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000026", domain.ParseRunID)
	turn := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000027", domain.ParseTurnID)
	agent := mustCaptionTestID(t, "018f0000-0000-7000-8000-000000000028", domain.ParseAgentID)
	captionLocalOne := mustCaptionLocal(t, "derived_caption_001")
	alignmentLocalOne := mustCaptionLocal(t, "derived_alignment_001")
	captionLocalTwo := mustCaptionLocal(t, "derived_caption_002")
	alignmentLocalTwo := mustCaptionLocal(t, "derived_alignment_002")
	policy := domain.ReadableCaptionPolicyV1()
	outputs := []DerivedCaptionOutputInput{
		{CaptionAs: captionLocalOne, AlignmentAs: alignmentLocalOne, SourceRange: cues[0].SourceRange, TimelineRange: cues[0].TimelineRange, Text: cues[0].Text},
		{CaptionAs: captionLocalTwo, AlignmentAs: alignmentLocalTwo, SourceRange: cues[1].SourceRange, TimelineRange: cues[1].TimelineRange, Text: cues[1].Text},
	}
	return NormalizeEditInput{
		ProposalID: proposal, ProjectID: fixture.project, SequenceID: fixture.sequence,
		RunID: run, TurnID: turn, Actor: domain.AgentActor(agent), CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		Allocation: []domain.LocalAllocation{
			{Local: captionLocalOne, Kind: domain.EntityCaption, ID: captionOne.String()},
			{Local: alignmentLocalOne, Kind: domain.EntityAlignment, ID: alignmentOne.String()},
			{Local: captionLocalTwo, Kind: domain.EntityCaption, ID: captionTwo.String()},
			{Local: alignmentLocalTwo, Kind: domain.EntityAlignment, ID: alignmentTwo.String()},
		},
		Input: EditProposeInput{
			RequestID: mustCaptionRequest(t, "derive-captions-test"), Intent: "derive exact captions",
			BaseProjectRevision: 10,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityNarrativeNode, ID: fixture.excerpt.ID.String(), Revision: fixture.excerpt.Revision},
				{Kind: domain.EntityClip, ID: fixture.clip.ID.String(), Revision: fixture.clip.Revision},
				{Kind: domain.EntityTrack, ID: fixture.track.ID.String(), Revision: fixture.track.Revision},
				{Kind: domain.EntityAsset, ID: fixture.asset.String(), Revision: 3},
				{Kind: domain.EntityTranscriptCorrection, ID: fixture.correction.ID.String(), Revision: fixture.correction.Revision},
			},
			Operations: []EditOperationInput{{
				Type:          domain.EditDeriveCaptions,
				NarrativeNode: &EditReference{ID: fixture.excerpt.ID.String()},
				Clip:          &EditReference{ID: fixture.clip.ID.String()}, TrackID: &fixture.track.ID,
				CaptionPolicy: &policy, DerivedCaptions: outputs,
			}},
		},
		State: EditNormalizationState{
			ProjectID: fixture.project, ProjectRevision: 10, DocumentID: fixture.document, DocumentRevision: 4,
			SequenceID: fixture.sequence, SequenceRevision: 7,
			Tracks:                map[string]EditTrackState{fixture.track.ID.String(): fixture.track},
			SourceExcerpts:        map[string]domain.SourceExcerptState{fixture.excerpt.ID.String(): fixture.excerpt},
			TranscriptCorrections: fixture.corrections,
			TranscriptArtifacts:   map[string]EditTranscriptArtifactState{fixture.artifact.ID.String(): fixture.artifact},
			Clips:                 map[string]domain.ClipState{fixture.clip.ID.String(): fixture.clip},
			SourceStreams: map[string]EditSourceStreamState{fixture.stream.String(): {
				ID: fixture.stream, AssetID: fixture.asset, AssetRevision: 3,
			}},
			Captions: map[string]domain.CaptionState{}, Alignments: map[string]domain.AlignmentState{},
			AuthoredTexts: map[string]domain.AuthoredTextState{}, LinkGroups: map[string]domain.LinkGroupState{},
		},
	}
}

func testTimeRange(t *testing.T, startValue int64, startScale int32, durationValue int64, durationScale int32) domain.TimeRange {
	t.Helper()
	start, err := domain.NewRationalTime(startValue, startScale)
	if err != nil {
		t.Fatal(err)
	}
	duration, err := domain.NewRationalTime(durationValue, durationScale)
	if err != nil {
		t.Fatal(err)
	}
	result, err := domain.NewTimeRange(start, duration)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustCaptionTestID[T any](t *testing.T, value string, parse func(string) (T, error)) T {
	t.Helper()
	result, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustCaptionLocal(t *testing.T, value string) domain.LocalID {
	t.Helper()
	result, err := domain.ParseLocalID(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustCaptionRequest(t *testing.T, value string) domain.RequestID {
	t.Helper()
	result, err := domain.ParseRequestID(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
