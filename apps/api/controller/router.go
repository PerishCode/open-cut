package controller

import (
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func NewRouter(health service.Health, projects service.Projects) (*http.ServeMux, huma.API) {
	mux := http.NewServeMux()
	config := huma.DefaultConfig("Open Cut API", "1.0.0")
	config.OpenAPIPath = ""
	config.DocsPath = ""
	config.SchemasPath = ""
	config.Servers = nil
	config.Transformers = nil
	config.CreateHooks = nil
	api := humago.New(mux, config)
	RegisterHealth(api, health)
	RegisterProjects(api, projects)
	return mux, api
}
