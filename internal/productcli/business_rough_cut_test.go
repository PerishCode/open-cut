package productcli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestRoughCutDerivationCLIUsesStrictStdinAndStableBusinessRoute(t *testing.T) {
	project := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	sequence := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000002", domain.ParseSequenceID)
	excerpt := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000003", domain.ParseNarrativeNodeID)
	track := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000004", domain.ParseTrackID)
	stream := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000005", domain.ParseSourceStreamID)
	prefix, _ := domain.ParseLocalID("rough")
	start, _ := domain.NewRationalTime(3, 1)
	input := command.RoughCutDeriveInput{
		TimelineStart: start, LocalPrefix: prefix,
		Items: []application.RoughCutDerivationPreviewItemInput{{
			SourceExcerptID: excerpt, SourceExcerptRevision: 2,
			Audio: &application.RoughCutDerivationPreviewLaneInput{
				TrackID: track, TrackRevision: 4, SourceStreamID: stream,
			},
		}},
	}
	encodedInput, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(envProjectID, project.String())
	t.Setenv(envSequenceID, sequence.String())
	invocation, err := parseBusinessInvocation(
		[]string{"edit", "derive-rough-cut", "--input", "-"}, bytes.NewReader(encodedInput), &bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.name != "edit derive-rough-cut" || invocation.method != "POST" || invocation.query != "" ||
		invocation.path != "/v1/projects/"+project.String()+"/sequences/"+sequence.String()+"/edit/rough-cut-derivation" ||
		!bytes.Equal(invocation.body, encodedInput) {
		t.Fatalf("invocation=%+v body=%s", invocation, invocation.body)
	}
	if _, err := parseBusinessInvocation(
		[]string{"edit", "derive-rough-cut"}, bytes.NewReader(encodedInput), &bytes.Buffer{},
	); err == nil {
		t.Fatal("derive-rough-cut accepted an implicit input channel")
	}

	digest, _ := domain.ParseDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	one, _ := domain.NewRationalTime(1, 1)
	rangeValue, _ := domain.NewTimeRange(start, one)
	alignmentAs, _ := domain.ParseLocalID("rough_alignment_001")
	clipAs, _ := domain.ParseLocalID("rough_audio_001")
	operation := application.EditOperationInput{
		Type: domain.EditDeriveRoughCut, RoughCutOutputDigest: &digest,
		DerivedRoughCut: []application.DerivedRoughCutOutputInput{{
			SourceExcerptID: excerpt, SourceRange: rangeValue, TimelineRange: rangeValue,
			AlignmentAs: alignmentAs, Audio: &application.DerivedRoughCutLaneOutputInput{
				ClipAs: clipAs, TrackID: track, SourceStreamID: stream,
			},
		}},
	}
	response, err := json.Marshal(command.RoughCutDeriveData{
		BaseProjectRevision: 5, Operation: operation, OutputDigest: digest, ActivityCursor: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, revision, cursor, err := validateBusinessResponse("edit derive-rough-cut", response); err != nil ||
		revision == nil || revision.Value() != 5 || cursor == nil || cursor.Value() != 8 {
		t.Fatalf("revision=%v cursor=%v err=%v", revision, cursor, err)
	}
}
