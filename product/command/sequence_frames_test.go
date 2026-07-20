package command

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func mustUUIDv7(t *testing.T) string {
	t.Helper()
	value, err := domain.GenerateUUIDv7(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// An accepted Sequence-frame job has scheduled work but delivered nothing, so
// `resources` is legitimately empty. The schema declares it nullable:"false",
// and the Agent decodes it as an array, so an empty collection must encode as
// [] rather than null.
func TestAcceptedSequenceFramesEncodeEmptyResourcesAsAnArray(t *testing.T) {
	projectID, err := domain.ParseProjectID(mustUUIDv7(t))
	if err != nil {
		t.Fatal(err)
	}
	sequenceID, err := domain.ParseSequenceID(mustUUIDv7(t))
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := domain.ParseWorkJobID(mustUUIDv7(t))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := domain.NewRevision(1)
	if err != nil {
		t.Fatal(err)
	}
	data := SequenceFramesDataFrom(application.SequenceFrameSetResult{
		Status: application.SequenceFrameSetAccepted, ProjectID: projectID,
		SequenceID: sequenceID, SequenceRevision: revision,
		Profile: "sequence-frame-srgb-png-v1",
		Job:     application.SequenceFrameJob{ID: jobID, State: domain.MediaJobQueued},
		// An accepted job has scheduled work and delivered nothing.
		Resources: []application.SequenceFrameResourceLease{},
	})
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resources", "samples"} {
		if _, isArray := decoded[field].([]any); !isArray {
			t.Fatalf("%s encoded as %v, want an array", field, decoded[field])
		}
	}
}
