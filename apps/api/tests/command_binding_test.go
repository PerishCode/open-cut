package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/command"
)

func TestAgentAPIEndpointsAreBoundToRegistryFingerprints(t *testing.T) {
	parallelAPITest(t)
	store := repository.NewMemoryProjects()
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	_, api := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, nil, nil, nil, nil, nil, service.RejectAuthorizer{},
	)
	document, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var openapi struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(document, &openapi); err != nil {
		t.Fatal(err)
	}
	registry := command.InitialRegistry()
	bindings := agentEndpointBindings()
	expected := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		operation := openapi.Paths[binding.Path][binding.Method]
		name := binding.Command[0] + " " + binding.Command[1]
		fingerprint, err := registry.Fingerprint(binding.Command)
		if err != nil {
			t.Fatal(err)
		}
		descriptor, err := registry.Lookup(binding.Command)
		if err != nil {
			t.Fatal(err)
		}
		if operation["x-open-cut-command"] != name ||
			operation["x-open-cut-command-fingerprint"] != fingerprint ||
			operation["x-open-cut-receipt"] != string(descriptor.Receipt) ||
			operation["x-open-cut-surface"] != "creator,agent" {
			t.Fatalf("binding=%v operation=%+v", binding, operation)
		}
		expected[name] = binding.Method + " " + binding.Path
	}
	root, err := registry.Discover(nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range root.Children {
		discovery, discoverErr := registry.Discover([]string{group.Name}, "test")
		if discoverErr != nil {
			t.Fatal(discoverErr)
		}
		for _, leaf := range discovery.Children {
			name := group.Name + " " + leaf.Name
			if _, exists := expected[name]; !exists {
				t.Fatalf("registered Agent command has no asserted HTTP binding: %s", name)
			}
		}
	}
	for path, methods := range openapi.Paths {
		for method, operation := range methods {
			name, exposed := operation["x-open-cut-command"].(string)
			if !exposed {
				continue
			}
			if expected[name] != method+" "+path {
				t.Fatalf("unexpected Agent HTTP binding: command=%s method=%s path=%s", name, method, path)
			}
		}
	}
	create := openapi.Paths["/v1/projects"]["post"]
	if _, exposed := create["x-open-cut-command"]; exposed {
		t.Fatalf("creator-only project create escaped into the Agent command surface: %+v", create)
	}
	for _, path := range []string{
		"/v1/projects/{projectId}/assets/{assetId}/media-leases",
		"/v1/projects/{projectId}/sequences/{sequenceId}/media-leases",
	} {
		operation := openapi.Paths[path]["post"]
		if _, exposed := operation["x-open-cut-command"]; exposed ||
			operation["x-open-cut-surface"] != "first-party-creator" {
			t.Fatalf("Creator Viewer delivery escaped into the Agent command surface: %s %+v", path, operation)
		}
	}
}

// TestChallengeBindingCoversEveryAgentEndpoint proves the CLI challenge's
// command-to-path binding recognizes every registered Agent endpoint: a
// command missing from that binding table fails closed at challenge time and
// silently amputates part of the Agent surface in installed products.
func TestChallengeBindingCoversEveryAgentEndpoint(t *testing.T) {
	parallelAPITest(t)
	replacements := map[string]string{
		"{id}":            "019f6f4f-88da-7f98-a91b-0bd7cf213013",
		"{projectId}":     "019f6f4f-88da-7f98-a91b-0bd7cf213013",
		"{assetId}":       "019f6f58-5e6b-7510-93de-44a490cd450d",
		"{runId}":         "019f6f5a-d537-70e1-a93e-bfe10a797a74",
		"{turnId}":        "019f6f5a-d537-73d6-8cfb-d84aaa46c62a",
		"{sequenceId}":    "019f6f4f-88da-72b8-8212-8acad6337584",
		"{documentId}":    "019f6f4f-88da-7445-9220-f89f1cba58c8",
		"{proposalId}":    "019f6f65-dc98-7bff-9b5e-3f3e692567f5",
		"{transactionId}": "019f70a7-ed65-7360-a82e-fed039bb85ed",
		"{jobId}":         "019f70aa-5d5f-77b2-b351-ce97f25604b0",
		"{kind}":          "clip",
	}
	for _, binding := range agentEndpointBindings() {
		requestPath := binding.Path
		for placeholder, value := range replacements {
			requestPath = strings.ReplaceAll(requestPath, placeholder, value)
		}
		if strings.ContainsRune(requestPath, '{') {
			t.Fatalf("unsubstituted placeholder in %s", requestPath)
		}
		if !service.ChallengeHTTPBinding(binding.Command, requestPath) {
			t.Fatalf(
				"the CLI challenge binding rejects registered Agent endpoint %q at %s",
				binding.Command[0]+" "+binding.Command[1], requestPath,
			)
		}
	}
}

type agentEndpointBinding struct {
	Path    string
	Method  string
	Command []string
}

func agentEndpointBindings() []agentEndpointBinding {
	return []agentEndpointBinding{
		{Path: "/v1/product/status", Method: "get", Command: []string{"product", "status"}},
		{Path: "/v1/activity", Method: "get", Command: []string{"activity", "list"}},
		{Path: "/v1/projects", Method: "get", Command: []string{"project", "list"}},
		{Path: "/v1/projects/{id}", Method: "get", Command: []string{"project", "show"}},
		{Path: "/v1/projects/{projectId}/assets", Method: "get", Command: []string{"asset", "list"}},
		{Path: "/v1/projects/{projectId}/assets/{assetId}", Method: "get", Command: []string{"asset", "inspect"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/assets/{assetId}/frames", Method: "post", Command: []string{"asset", "frames"}},
		{Path: "/v1/projects/{projectId}/assets/{assetId}/transcript", Method: "get", Command: []string{"transcript", "read"}},
		{Path: "/v1/projects/{projectId}/runs", Method: "post", Command: []string{"run", "begin"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}", Method: "get", Command: []string{"run", "show"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/wait", Method: "get", Command: []string{"run", "wait"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/resume", Method: "post", Command: []string{"run", "resume"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/complete", Method: "post", Command: []string{"run", "complete"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/cancel", Method: "post", Command: []string{"run", "cancel"}},
		{Path: "/v1/projects/{projectId}/narratives/{documentId}/subtree", Method: "get", Command: []string{"narrative", "show"}},
		{Path: "/v1/projects/{projectId}/sequences/{sequenceId}/window", Method: "get", Command: []string{"sequence", "show"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/sequences/{sequenceId}/frames", Method: "post", Command: []string{"sequence", "frames"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/sequences/{sequenceId}/exports", Method: "post", Command: []string{"export", "start"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/exports/{jobId}", Method: "get", Command: []string{"export", "show"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/exports/{jobId}/retry", Method: "post", Command: []string{"export", "retry"}},
		{Path: "/v1/projects/{projectId}/runs/{runId}/turns/{turnId}/exports/{jobId}/cancel", Method: "post", Command: []string{"export", "cancel"}},
		{Path: "/v1/projects/{projectId}/entities/{kind}/{id}", Method: "get", Command: []string{"entity", "show"}},
		{Path: "/v1/projects/{projectId}/edit/proposals/{proposalId}", Method: "get", Command: []string{"edit", "show"}},
		{Path: "/v1/projects/{projectId}/edit/transactions", Method: "get", Command: []string{"edit", "history"}},
		{Path: "/v1/projects/{projectId}/sequences/{sequenceId}/edit/caption-derivation", Method: "get", Command: []string{"edit", "derive-captions"}},
		{Path: "/v1/projects/{projectId}/sequences/{sequenceId}/edit/rough-cut-derivation", Method: "post", Command: []string{"edit", "derive-rough-cut"}},
		{Path: "/v1/projects/{projectId}/sequences/{sequenceId}/runs/{runId}/turns/{turnId}/edit/proposals", Method: "post", Command: []string{"edit", "propose"}},
		{Path: "/v1/projects/{projectId}/sequences/{sequenceId}/runs/{runId}/turns/{turnId}/edit/proposals/{proposalId}/apply", Method: "post", Command: []string{"edit", "apply"}},
		{Path: "/v1/projects/{projectId}/sequences/{sequenceId}/runs/{runId}/turns/{turnId}/edit/transactions/{transactionId}/undo", Method: "post", Command: []string{"edit", "undo"}},
	}
}
