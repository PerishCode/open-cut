package productcli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestCaptionDerivationCLIUsesOnlyStableBusinessIdentities(t *testing.T) {
	project := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	sequence := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000002", domain.ParseSequenceID)
	excerpt := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000003", domain.ParseNarrativeNodeID)
	clip := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000004", domain.ParseClipID)
	track := mustCaptionCLIIdentity(t, "018f0000-0000-7000-8000-000000000005", domain.ParseTrackID)
	t.Setenv(envProjectID, project.String())
	t.Setenv(envSequenceID, sequence.String())
	invocation, err := parseBusinessInvocation([]string{
		"edit", "derive-captions",
		"--source-excerpt-id", excerpt.String(),
		"--clip-id", clip.String(),
		"--track-id", track.String(),
		"--local-prefix", "opening",
	}, bytes.NewReader(nil), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	expectedQuery := url.Values{
		"sourceExcerptId": {excerpt.String()}, "clipId": {clip.String()},
		"trackId": {track.String()}, "localPrefix": {"opening"},
	}.Encode()
	if invocation.name != "edit derive-captions" || invocation.method != "GET" ||
		invocation.path != "/v1/projects/"+project.String()+"/sequences/"+sequence.String()+"/edit/caption-derivation" ||
		invocation.query != expectedQuery || len(invocation.body) != 0 ||
		invocation.context.ProjectID == nil || *invocation.context.ProjectID != project ||
		invocation.context.SequenceID == nil || *invocation.context.SequenceID != sequence {
		t.Fatalf("invocation=%+v", invocation)
	}

	captionLocal, _ := domain.ParseLocalID("opening_caption_001")
	alignmentLocal, _ := domain.ParseLocalID("opening_alignment_001")
	zero, _ := domain.NewRationalTime(0, 1)
	one, _ := domain.NewRationalTime(1, 1)
	rangeValue, _ := domain.NewTimeRange(zero, one)
	policy := domain.ReadableCaptionPolicyV1()
	result := command.CaptionDeriveData{
		BaseProjectRevision: 4,
		Operation: application.EditOperationInput{
			Type: domain.EditDeriveCaptions, NarrativeNode: &application.EditReference{ID: excerpt.String()},
			Clip: &application.EditReference{ID: clip.String()}, TrackID: &track, CaptionPolicy: &policy,
			DerivedCaptions: []application.DerivedCaptionOutputInput{{
				CaptionAs: captionLocal, AlignmentAs: alignmentLocal,
				SourceRange: rangeValue, TimelineRange: rangeValue, Text: "hello",
			}},
		},
		ActivityCursor: 7,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, revision, cursor, err := validateBusinessResponse("edit derive-captions", encoded); err != nil ||
		revision == nil || revision.Value() != 4 || cursor == nil || cursor.Value() != 7 {
		t.Fatalf("revision=%v cursor=%v err=%v", revision, cursor, err)
	}
}

func mustCaptionCLIIdentity[T any](t *testing.T, value string, parse func(string) (T, error)) T {
	t.Helper()
	result, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
