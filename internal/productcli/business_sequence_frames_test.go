package productcli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequenceFramesCLIClosesPrepareContinueAndRetry(t *testing.T) {
	project := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	sequence := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000002", domain.ParseSequenceID)
	run := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000003", domain.ParseRunID)
	turn := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000004", domain.ParseTurnID)
	job := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000005", domain.ParseWorkJobID)
	t.Setenv(envProjectID, project.String())
	t.Setenv(envSequenceID, sequence.String())
	t.Setenv(envRunID, run.String())
	t.Setenv(envTurnID, turn.String())
	wantPath := "/v1/projects/" + project.String() + "/runs/" + run.String() + "/turns/" +
		turn.String() + "/sequences/" + sequence.String() + "/frames"
	fixtures := []struct {
		args  []string
		check func(command.SequenceFramesInput) bool
	}{
		{args: []string{"sequence", "frames", "--sequence-revision", "7", "--time", "0/1", "--time", "1/2"},
			check: func(input command.SequenceFramesInput) bool {
				return input.SequenceRevision != nil && input.SequenceRevision.Value() == 7 && len(input.Times) == 2
			}},
		{args: []string{"sequence", "frames", "--job-id", job.String()},
			check: func(input command.SequenceFramesInput) bool { return input.JobID != nil && *input.JobID == job }},
		{args: []string{"sequence", "frames", "--retry-job-id", job.String()},
			check: func(input command.SequenceFramesInput) bool {
				return input.RetryJobID != nil && *input.RetryJobID == job
			}},
	}
	for _, fixture := range fixtures {
		invocation, err := parseBusinessInvocation(fixture.args, bytes.NewReader(nil), &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		var input command.SequenceFramesInput
		if json.Unmarshal(invocation.body, &input) != nil || !fixture.check(input) ||
			invocation.name != "sequence frames" || invocation.method != "POST" ||
			invocation.path != wantPath || invocation.query != "" {
			t.Fatalf("invocation=%+v input=%+v", invocation, input)
		}
	}
	if _, err := parseBusinessInvocation([]string{
		"sequence", "frames", "--job-id", job.String(), "--retry-job-id", job.String(),
	}, bytes.NewReader(nil), &bytes.Buffer{}); err == nil {
		t.Fatal("mixed continue/retry invocation was accepted")
	}
}

func TestSequenceFramesCLIValidatesTerminalDataWithoutInternalProvenance(t *testing.T) {
	job := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000005", domain.ParseWorkJobID)
	project := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	sequence := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000002", domain.ParseSequenceID)
	revision, _ := domain.NewRevision(7)
	zero, _ := domain.NewRationalTime(0, 1)
	index, _ := domain.NewUInt64(0)
	code := "sequence-time-out-of-range"
	result := command.SequenceFramesData{
		Status: application.SequenceFrameSetFailed, ProjectID: project, SequenceID: sequence,
		SequenceRevision: revision, Profile: application.SequenceFrameSetProfile,
		Samples: []application.SequenceFrameCoordinate{{RequestedTime: zero, SequenceTime: zero, FrameIndex: index}},
		Job: command.SequenceFrameJobData{
			ID: job, State: domain.MediaJobFailed, TerminalErrorCode: &code,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		Recovery: application.MediaRecoveryNone, Resources: []application.SequenceFrameResourceLease{},
		ActivityCursor: 9,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, cursor, err := validateBusinessResponse("sequence frames", encoded); err != nil ||
		cursor == nil || cursor.Value() != 9 {
		t.Fatalf("cursor=%v err=%v", cursor, err)
	}
	if bytes.Contains(encoded, []byte("previewJobId")) || bytes.Contains(encoded, []byte("artifactId")) ||
		bytes.Contains(encoded, []byte("renderPlanDigest")) {
		t.Fatalf("internal provenance escaped into stable CLI data: %s", encoded)
	}
}

func mustSequenceCLIIdentity[T any](t *testing.T, value string, parse func(string) (T, error)) T {
	t.Helper()
	result, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
