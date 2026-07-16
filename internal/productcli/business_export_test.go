package productcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestExportCLIExposesOnlyClosedDurableLeaves(t *testing.T) {
	project := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	sequence := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000002", domain.ParseSequenceID)
	run := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000003", domain.ParseRunID)
	turn := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000004", domain.ParseTurnID)
	job := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000005", domain.ParseWorkJobID)
	t.Setenv(envProjectID, project.String())
	t.Setenv(envSequenceID, sequence.String())
	t.Setenv(envRunID, run.String())
	t.Setenv(envTurnID, turn.String())
	prefix := "/v1/projects/" + project.String() + "/runs/" + run.String() + "/turns/" + turn.String()
	fixtures := []struct {
		args   []string
		method string
		path   string
		body   bool
	}{
		{[]string{"export", "start", "--request-id", "agent:export:001", "--sequence-revision", "7", "--preset", domain.SequenceExportProfileV1}, http.MethodPost, prefix + "/sequences/" + sequence.String() + "/exports", true},
		{[]string{"export", "show", "--job-id", job.String()}, http.MethodGet, prefix + "/exports/" + job.String(), false},
		{[]string{"export", "retry", "--job-id", job.String()}, http.MethodPost, prefix + "/exports/" + job.String() + "/retry", true},
		{[]string{"export", "cancel", "--job-id", job.String(), "--request-id", "agent:export:cancel:001"}, http.MethodPost, prefix + "/exports/" + job.String() + "/cancel", true},
	}
	for _, fixture := range fixtures {
		invocation, err := parseBusinessInvocation(fixture.args, bytes.NewReader(nil), &bytes.Buffer{})
		if err != nil {
			t.Fatalf("%v: %v", fixture.args, err)
		}
		if invocation.method != fixture.method || invocation.path != fixture.path ||
			(len(invocation.body) > 0) != fixture.body || invocation.query != "" {
			t.Fatalf("args=%v invocation=%+v", fixture.args, invocation)
		}
		if bytes.Contains(invocation.body, []byte("path")) || bytes.Contains(invocation.body, []byte("renderer")) {
			t.Fatalf("internal or destination input escaped: %s", invocation.body)
		}
	}
}

func TestExportCLIValidatesStableJobProjection(t *testing.T) {
	project := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000001", domain.ParseProjectID)
	sequence := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000002", domain.ParseSequenceID)
	job := mustSequenceCLIIdentity(t, "018f0000-0000-7000-8000-000000000005", domain.ParseWorkJobID)
	revision, _ := domain.NewRevision(7)
	result := command.ExportData{
		ProjectID: project, SequenceID: sequence, SequenceRevision: revision,
		Preset: domain.SequenceExportProfileV1,
		Job: command.ExportJobData{
			ID: job, RootJobID: job, State: domain.MediaJobRunning,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		Recovery: application.MediaRecoveryNone, ActivityCursor: 11,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, cursor, err := validateBusinessResponse("export show", encoded); err != nil ||
		cursor == nil || cursor.Value() != 11 {
		t.Fatalf("cursor=%v err=%v", cursor, err)
	}
	for _, forbidden := range []string{"renderPlanDigest", "rendererVersion", "producerJobId", "byteReference", "datadir"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("internal export provenance escaped: %s", encoded)
		}
	}
}
