package controller

import (
	"net/http"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func NewRouter(
	health service.Health,
	productStatus *application.ProductStatus,
	productResources *application.ProductResources,
	projects *application.Projects,
	reads *application.ProjectReads,
	activity *application.ActivityReads,
	runs *application.AgentRuns,
	edits *application.Edits,
	editReads *application.EditReads,
	media *application.Media,
	assetReads *application.AssetReads,
	sourceAccess *service.SourceAccess,
	mediaLeases *service.MediaLeaseService,
	sequencePreviewLeases *service.SequencePreviewLeaseService,
	sequenceFrames *application.SequenceFrames,
	sequenceExports *application.SequenceExports,
	sequenceExportDelivery *service.SequenceExportDeliveryService,
	authorizer service.Authorizer,
) (*http.ServeMux, huma.API) {
	return NewRouterWithAgentBridge(
		health, productStatus, productResources, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, mediaLeases, sequencePreviewLeases, sequenceFrames,
		sequenceExports, sequenceExportDelivery, nil, authorizer,
		nil,
	)
}

func NewRouterWithAgentBridge(
	health service.Health,
	productStatus *application.ProductStatus,
	productResources *application.ProductResources,
	projects *application.Projects,
	reads *application.ProjectReads,
	activity *application.ActivityReads,
	runs *application.AgentRuns,
	edits *application.Edits,
	editReads *application.EditReads,
	media *application.Media,
	assetReads *application.AssetReads,
	sourceAccess *service.SourceAccess,
	mediaLeases *service.MediaLeaseService,
	sequencePreviewLeases *service.SequencePreviewLeaseService,
	sequenceFrames *application.SequenceFrames,
	sequenceExports *application.SequenceExports,
	sequenceExportDelivery *service.SequenceExportDeliveryService,
	agentBridge *service.AgentBridgeService,
	authorizer service.Authorizer,
	projectVersions *application.ProjectVersions,
) (*http.ServeMux, huma.API) {
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
	RegisterProductStatus(api, productStatus, runs, authorizer)
	RegisterProductResources(api, productResources, authorizer)
	RegisterUISessions(api, authorizer)
	RegisterCLIAuthorization(api, authorizer)
	RegisterProjects(api, projects, reads, runs, authorizer)
	RegisterActivity(api, activity, runs, authorizer)
	RegisterRuns(api, runs, authorizer)
	RegisterEditReads(api, editReads, runs, authorizer)
	RegisterEditCommands(api, edits, runs, authorizer)
	RegisterCreatorEdits(api, edits, authorizer)
	RegisterCreatorRoughCutPreview(api, editReads, authorizer)
	RegisterCreatorTimelineGesturePreview(api, editReads, authorizer)
	RegisterCreatorCaptionPreview(api, editReads, authorizer)
	RegisterCreatorCaptionGesturePreview(api, editReads, authorizer)
	RegisterCreatorClipPlacementPreview(api, editReads, authorizer)
	RegisterCreatorEditHistory(api, editReads, authorizer)
	RegisterProjectVersions(api, projectVersions, authorizer)
	RegisterMedia(api, media, assetReads, sourceAccess, runs, authorizer)
	RegisterSequenceFrames(api, sequenceFrames, runs, authorizer)
	RegisterSequenceExports(api, sequenceExports, runs, authorizer)
	RegisterAgentBridge(api, agentBridge, runs, authorizer)
	RegisterSequenceExportDelivery(mux, api, sequenceExportDelivery, authorizer)
	RegisterMediaDelivery(mux, api, mediaLeases, sequencePreviewLeases, authorizer)
	return mux, api
}
