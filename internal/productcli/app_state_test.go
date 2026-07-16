package productcli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/authwire"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestAppStateArgvOverridesLaunchEnvironmentWithoutPersistingContext(t *testing.T) {
	environmentProject, _ := domain.ParseProjectID(mustUUIDv7(t, time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)))
	argvProject, _ := domain.ParseProjectID(mustUUIDv7(t, time.Date(2026, 7, 15, 1, 0, 1, 0, time.UTC)))
	t.Setenv(envProjectID, environmentProject.String())
	t.Setenv(envOutput, string(application.OutputHuman))
	t.Setenv(envWaitMS, "1200")

	invocation, err := parseBusinessInvocation([]string{
		"project", "show", "--project-id", argvProject.String(),
		"--output", "json", "--wait-ms", "2300",
	}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.context.ProjectID == nil || *invocation.context.ProjectID != argvProject {
		t.Fatalf("context=%+v", invocation.context)
	}
	if invocation.policyOverride.Output == nil || *invocation.policyOverride.Output != application.OutputJSON ||
		invocation.policyOverride.WaitMilliseconds == nil || *invocation.policyOverride.WaitMilliseconds != 2300 {
		t.Fatalf("policy override=%+v", invocation.policyOverride)
	}
}

func TestAppStateRejectsUnboundedWaitBeforeProductStartup(t *testing.T) {
	_, err := parseBusinessInvocation([]string{
		"project", "list", "--wait-ms", "30001",
	}, nil, io.Discard)
	if err == nil {
		t.Fatal("unbounded wait was accepted")
	}
}

func TestRunInvocationBuildsSignedBodyFromCLIAndLaunchScopedContext(t *testing.T) {
	project, _ := domain.ParseProjectID(mustUUIDv7(t, time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)))
	run, _ := domain.ParseRunID(mustUUIDv7(t, time.Date(2026, 7, 15, 2, 0, 1, 0, time.UTC)))
	turn, _ := domain.ParseTurnID(mustUUIDv7(t, time.Date(2026, 7, 15, 2, 0, 2, 0, time.UTC)))
	t.Setenv(envProjectID, project.String())
	t.Setenv(envRunID, run.String())
	t.Setenv(envTurnID, turn.String())

	invocation, err := parseBusinessInvocation([]string{
		"run", "resume", "--request-id", "agent:resume:001", "--expected-generation", "7",
	}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := "/v1/projects/" + project.String() + "/runs/" + run.String() + "/turns/" + turn.String() + "/resume"
	if invocation.method != http.MethodPost || invocation.path != wantPath || len(invocation.body) == 0 ||
		invocation.context.ProjectID == nil || invocation.context.RunID == nil || invocation.context.TurnID == nil {
		t.Fatalf("invocation=%+v", invocation)
	}
	var input application.RunResumeInput
	if err := json.Unmarshal(invocation.body, &input); err != nil || input.RequestID.String() != "agent:resume:001" ||
		input.ExpectedGeneration.Value() != 7 {
		t.Fatalf("input=%+v err=%v", input, err)
	}
	digest, err := authwire.CommandBodyDigest("run resume", invocation.body)
	if err != nil || invocation.bodyDigest != digest.String() {
		t.Fatalf("digest=%q want=%q err=%v", invocation.bodyDigest, digest, err)
	}
}

func TestRunWaitUsesBoundedPolicyAndSignedActivityCursor(t *testing.T) {
	project, _ := domain.ParseProjectID(mustUUIDv7(t, time.Date(2026, 7, 15, 2, 30, 0, 0, time.UTC)))
	run, _ := domain.ParseRunID(mustUUIDv7(t, time.Date(2026, 7, 15, 2, 30, 1, 0, time.UTC)))
	invocation, err := parseBusinessInvocation([]string{
		"run", "wait", "--project-id", project.String(), "--run-id", run.String(),
		"--after", "17", "--wait-ms", "750",
	}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.method != http.MethodGet ||
		invocation.path != "/v1/projects/"+project.String()+"/runs/"+run.String()+"/wait" ||
		invocation.query != "after=17" || invocation.policyOverride.WaitMilliseconds == nil ||
		*invocation.policyOverride.WaitMilliseconds != 750 {
		t.Fatalf("invocation=%+v", invocation)
	}
}

func TestEditProposalInvocationUsesOnlyBoundedStdinAndInjectedAppState(t *testing.T) {
	base := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	project, _ := domain.ParseProjectID(mustUUIDv7(t, base))
	sequence, _ := domain.ParseSequenceID(mustUUIDv7(t, base.Add(time.Millisecond)))
	run, _ := domain.ParseRunID(mustUUIDv7(t, base.Add(2*time.Millisecond)))
	turn, _ := domain.ParseTurnID(mustUUIDv7(t, base.Add(3*time.Millisecond)))
	nodeID := mustUUIDv7(t, base.Add(4*time.Millisecond))
	proposalJSON := `{"requestId":"agent:proposal:001","intent":"Rewrite opening",` +
		`"baseProjectRevision":"1","preconditions":[],"operations":[` +
		`{"type":"update-authored-text","nodeId":"` + nodeID +
		`","authoredTextPurpose":"spoken","language":"en","text":"Hello cut"}]}`
	invocation, err := parseBusinessInvocation([]string{
		"edit", "propose", "--input", "-",
		"--project-id", project.String(), "--sequence-id", sequence.String(),
		"--run-id", run.String(), "--turn-id", turn.String(),
	}, strings.NewReader(proposalJSON), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := "/v1/projects/" + project.String() + "/sequences/" + sequence.String() +
		"/runs/" + run.String() + "/turns/" + turn.String() + "/edit/proposals"
	if invocation.method != http.MethodPost || invocation.path != wantPath || len(invocation.body) == 0 {
		t.Fatalf("invocation=%+v", invocation)
	}
	var input application.EditProposeInput
	if err := json.Unmarshal(invocation.body, &input); err != nil || input.Intent != "Rewrite opening" ||
		len(input.Operations) != 1 {
		t.Fatalf("input=%+v err=%v", input, err)
	}
}

func TestSequenceReadInvocationCarriesExactBoundedWindow(t *testing.T) {
	base := time.Date(2026, 7, 15, 3, 30, 0, 0, time.UTC)
	project, _ := domain.ParseProjectID(mustUUIDv7(t, base))
	sequence, _ := domain.ParseSequenceID(mustUUIDv7(t, base.Add(time.Millisecond)))
	invocation, err := parseBusinessInvocation([]string{
		"sequence", "show", "--start", "0/1", "--duration", "5/2", "--limit", "20",
		"--project-id", project.String(), "--sequence-id", sequence.String(),
	}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.method != http.MethodGet ||
		invocation.path != "/v1/projects/"+project.String()+"/sequences/"+sequence.String()+"/window" ||
		invocation.query != "durationScale=2&durationValue=5&limit=20&startScale=1&startValue=0" {
		t.Fatalf("invocation=%+v", invocation)
	}
}

func TestAssetReadsBindOnlyProjectContextAndOpaqueBounds(t *testing.T) {
	base := time.Date(2026, 7, 15, 4, 0, 0, 0, time.UTC)
	project, _ := domain.ParseProjectID(mustUUIDv7(t, base))
	asset, _ := domain.ParseAssetID(mustUUIDv7(t, base.Add(time.Millisecond)))
	listed, err := parseBusinessInvocation([]string{
		"asset", "list", "--after", "asset.v1.opaque", "--limit", "25", "--project-id", project.String(),
	}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if listed.method != http.MethodGet || listed.path != "/v1/projects/"+project.String()+"/assets" ||
		listed.query != "after=asset.v1.opaque&limit=25" {
		t.Fatalf("list invocation=%+v", listed)
	}
	inspected, err := parseBusinessInvocation([]string{
		"asset", "inspect", "--asset-id", asset.String(), "--project-id", project.String(),
	}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.method != http.MethodGet ||
		inspected.path != "/v1/projects/"+project.String()+"/assets/"+asset.String() || inspected.query != "" {
		t.Fatalf("inspect invocation=%+v", inspected)
	}
}

func TestAssetFramesBindsExactStreamTimesAndActiveTurnContext(t *testing.T) {
	base := time.Date(2026, 7, 15, 4, 30, 0, 0, time.UTC)
	project, _ := domain.ParseProjectID(mustUUIDv7(t, base))
	run, _ := domain.ParseRunID(mustUUIDv7(t, base.Add(time.Millisecond)))
	turn, _ := domain.ParseTurnID(mustUUIDv7(t, base.Add(2*time.Millisecond)))
	asset, _ := domain.ParseAssetID(mustUUIDv7(t, base.Add(3*time.Millisecond)))
	stream, _ := domain.ParseSourceStreamID(mustUUIDv7(t, base.Add(4*time.Millisecond)))
	invocation, err := parseBusinessInvocation([]string{
		"asset", "frames", "--asset-id", asset.String(), "--source-stream-id", stream.String(),
		"--time", "0/1", "--time", "1/4", "--time", "3/4",
		"--project-id", project.String(), "--run-id", run.String(), "--turn-id", turn.String(),
	}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := "/v1/projects/" + project.String() + "/runs/" + run.String() +
		"/turns/" + turn.String() + "/assets/" + asset.String() + "/frames"
	if invocation.method != http.MethodPost || invocation.path != wantPath {
		t.Fatalf("invocation=%+v", invocation)
	}
	var input command.AssetFramesInput
	if err := json.Unmarshal(invocation.body, &input); err != nil || input.AssetID != asset ||
		input.SourceStreamID != stream || len(input.Times) != 3 {
		t.Fatalf("input=%+v err=%v", input, err)
	}
	if _, err := parseBusinessInvocation([]string{
		"asset", "frames", "--asset-id", asset.String(), "--source-stream-id", stream.String(),
		"--time", "1/2", "--time", "1/2", "--project-id", project.String(),
		"--run-id", run.String(), "--turn-id", turn.String(),
	}, nil, io.Discard); err == nil {
		t.Fatal("duplicate frame times were accepted")
	}
}

func mustUUIDv7(t *testing.T, at time.Time) string {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
