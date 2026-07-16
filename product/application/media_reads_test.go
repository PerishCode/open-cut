package application

import (
	"encoding/json"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSafeAssetViewProjectsRequiredCollectionsAsArrays(t *testing.T) {
	assetID, _ := domain.ParseAssetID("018f0000-0000-7000-8000-000000000001")
	projectID, _ := domain.ParseProjectID("018f0000-0000-7000-8000-000000000002")
	streamID, _ := domain.ParseSourceStreamID("018f0000-0000-7000-8000-000000000003")
	timeBase, _ := domain.NewRationalTime(1, 48_000)
	view := safeAssetView(domain.AssetDetail{
		Asset: domain.AssetState{ID: assetID, ProjectID: projectID},
		Facts: &domain.MediaFacts{
			Container: "wav",
			Streams: []domain.SourceStream{{
				ID:         streamID,
				Descriptor: domain.SourceStreamDescriptor{MediaType: domain.MediaAudio, TimeBase: timeBase},
			}},
		},
	})
	payload, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	root := decodeJSONObject(t, payload)
	assertJSONEmptyArray(t, root["artifacts"])
	assertJSONEmptyArray(t, root["jobs"])

	facts := decodeJSONObject(t, root["facts"])
	assertJSONEmptyArray(t, facts["containerAliases"])
	var streams []json.RawMessage
	if err := json.Unmarshal(facts["streams"], &streams); err != nil || len(streams) != 1 {
		t.Fatalf("streams = %s, error = %v", facts["streams"], err)
	}
	descriptor := decodeJSONObject(t, decodeJSONObject(t, streams[0])["descriptor"])
	assertJSONEmptyArray(t, descriptor["dispositions"])
}

func TestSafeAssetViewProjectsJobPrerequisitesAsArray(t *testing.T) {
	assetID, _ := domain.ParseAssetID("018f0000-0000-7000-8000-000000000001")
	projectID, _ := domain.ParseProjectID("018f0000-0000-7000-8000-000000000002")
	jobID, _ := domain.ParseMediaJobID("018f0000-0000-7000-8000-000000000003")
	view := safeAssetView(domain.AssetDetail{
		Asset: domain.AssetState{ID: assetID, ProjectID: projectID},
		Jobs:  []domain.MediaJobSummary{{ID: jobID, Kind: domain.MediaJobIdentify}},
	})
	payload, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	root := decodeJSONObject(t, payload)
	var jobs []json.RawMessage
	if err := json.Unmarshal(root["jobs"], &jobs); err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %s, error = %v", root["jobs"], err)
	}
	assertJSONEmptyArray(t, decodeJSONObject(t, jobs[0])["prerequisites"])
}

func decodeJSONObject(t *testing.T, payload []byte) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}
	return object
}

func assertJSONEmptyArray(t *testing.T, payload json.RawMessage) {
	t.Helper()
	if string(payload) != "[]" {
		t.Fatalf("value = %s, want []", payload)
	}
}
