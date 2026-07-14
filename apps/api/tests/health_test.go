package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
)

func TestHealthContract(t *testing.T) {
	mux, api := controller.NewRouter(service.NewHealth(repository.StaticHealth{}))
	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		OK      bool   `json:"ok"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Service != "api" {
		t.Fatalf("body=%+v", body)
	}
	document, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(document, []byte(`"$schema"`)) {
		t.Fatal("runtime schema links escaped into the product OpenAPI contract")
	}
}

func TestHealthServiceUsesRepository(t *testing.T) {
	healthService := service.NewHealth(repository.StaticHealth{})
	status, err := healthService.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.OK || status.Service != "api" {
		t.Fatalf("status=%+v", status)
	}
}
