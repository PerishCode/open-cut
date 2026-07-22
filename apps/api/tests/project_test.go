package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
)

func TestProjectHTTPUsesCreatorGenesisAndExactReadModels(t *testing.T) {
	parallelAPITest(t)
	store := repository.NewMemoryProjects()
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, nil, nil, nil, nil, nil, creatorAuthorizer{},
	)
	server := httptest.NewServer(mux)
	defer server.Close()

	createRequest, _ := http.NewRequest(
		http.MethodPost, server.URL+"/v1/projects",
		strings.NewReader(`{"requestId":"gesture:create-project:001","name":"Launch story"}`),
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse, err := server.Client().Do(createRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer createResponse.Body.Close()
	if createResponse.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createResponse.StatusCode, readBody(t, createResponse))
	}
	var created application.CreateProjectResult
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Project.Project.Revision.String() != "1" || created.Project.ActivityCursor.String() != "1" ||
		len(created.Project.Tracks) != 3 || created.Project.Format.CanvasWidth != 1920 {
		t.Fatalf("created=%+v", created)
	}

	listResponse, err := server.Client().Get(server.URL + "/v1/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer listResponse.Body.Close()
	var listed application.ListProjectsResult
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Projects) != 1 || listed.Projects[0].ID != created.Project.Project.ID || listed.ActivityCursor.String() != "1" {
		t.Fatalf("listed=%+v", listed)
	}

	showResponse, err := server.Client().Get(server.URL + "/v1/projects/" + created.Project.Project.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	defer showResponse.Body.Close()
	var shown application.ProjectOverview
	if err := json.NewDecoder(showResponse.Body).Decode(&shown); err != nil {
		t.Fatal(err)
	}
	if shown.Project.ID != created.Project.Project.ID || shown.ActivityCursor.String() != "1" {
		t.Fatalf("shown=%+v", shown)
	}
}

func TestProjectRoutesFailClosedWithoutAuthority(t *testing.T) {
	parallelAPITest(t)
	store := repository.NewMemoryProjects()
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, nil, nil, nil, nil, nil, service.RejectAuthorizer{},
	)
	request := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	var body any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return err.Error()
	}
	encoded, _ := json.Marshal(body)
	return string(encoded)
}
