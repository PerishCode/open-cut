package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/model"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
)

func TestProjectHTTPAndSSEContract(t *testing.T) {
	projects := service.NewProjects(repository.NewMemoryProjects())
	mux, _ := controller.NewRouter(service.NewHealth(repository.StaticHealth{}), projects)
	server := httptest.NewServer(mux)
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK || stream.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream status=%d content-type=%q", stream.StatusCode, stream.Header.Get("Content-Type"))
	}
	scanner := bufio.NewScanner(stream.Body)
	snapshotEvent, snapshotData := readSSE(t, scanner)
	if snapshotEvent != "project.snapshot" {
		t.Fatalf("snapshot event=%q data=%s", snapshotEvent, snapshotData)
	}
	var snapshot model.ProjectSnapshot
	if err := json.Unmarshal([]byte(snapshotData), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 0 || len(snapshot.Projects) != 0 {
		t.Fatalf("snapshot=%+v", snapshot)
	}

	body := strings.NewReader(`{"name":"Day 0","description":"Cold-start project"}`)
	putRequest, _ := http.NewRequest(http.MethodPut, server.URL+"/v1/projects/project-1", body)
	putRequest.Header.Set("Content-Type", "application/json")
	putResponse, err := server.Client().Do(putRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer putResponse.Body.Close()
	if putResponse.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", putResponse.StatusCode)
	}
	var written model.ProjectUpserted
	if err := json.NewDecoder(putResponse.Body).Decode(&written); err != nil {
		t.Fatal(err)
	}
	if written.Revision != 1 || written.Project.ID != "project-1" {
		t.Fatalf("written=%+v", written)
	}

	upsertEvent, upsertData := readSSE(t, scanner)
	if upsertEvent != "project.upserted" {
		t.Fatalf("upsert event=%q data=%s", upsertEvent, upsertData)
	}
	var upserted model.ProjectUpserted
	if err := json.Unmarshal([]byte(upsertData), &upserted); err != nil {
		t.Fatal(err)
	}
	if upserted != written {
		t.Fatalf("event=%+v response=%+v", upserted, written)
	}

	listResponse, err := server.Client().Get(server.URL + "/v1/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer listResponse.Body.Close()
	var listed model.ProjectSnapshot
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if listed.Revision != 1 || len(listed.Projects) != 1 || listed.Projects[0] != written.Project {
		t.Fatalf("listed=%+v", listed)
	}
}

func readSSE(t *testing.T, scanner *bufio.Scanner) (string, string) {
	t.Helper()
	event, data := "", ""
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			return event, data
		}
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("SSE stream ended: %v", scanner.Err())
	return "", ""
}
