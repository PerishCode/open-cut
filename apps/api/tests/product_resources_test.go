package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestProductResourceHTTPIsCreatorOnlyAndHidesAcquisitionInternals(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := resourceCatalogEntry(t, "https://catalog.invalid/whisper-small.bin", []byte("model"))
	resources, err := application.NewProductResources(
		store, []application.ProductResourceCatalogEntry{entry},
		application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	creatorMux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, resources,
		projects, reads, activity, runs, edits, editReads, media, assetReads, sourceAccess,
		nil, nil, nil, nil, nil, creatorAuthorizer{},
	)
	request := httptest.NewRequest(http.MethodGet, "/v1/product/resources", nil)
	response := httptest.NewRecorder()
	creatorMux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	encoded := response.Body.String()
	for _, forbidden := range []string{"catalog.invalid", "origin", "sha256", "byteReference", "datadir", "/resources/"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("product resource response exposed %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(encoded, entry.Name) || !strings.Contains(encoded, `"state":"not-acquired"`) {
		t.Fatalf("product resource response=%s", encoded)
	}

	authority, err := application.AuthorityFromContext(agentContextForResource(t))
	if err != nil {
		t.Fatal(err)
	}
	agentMux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, resources,
		projects, reads, activity, runs, edits, editReads, media, assetReads, sourceAccess,
		nil, nil, nil, nil, nil, fixedAuthorizer{authority: authority},
	)
	request = httptest.NewRequest(
		http.MethodPost, "/v1/product/resources/"+entry.Name+"/acquisition",
		strings.NewReader(`{"requestId":"agent-must-not-acquire"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	agentMux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("Agent acquisition status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProductResourceAcquisitionIsCreatorAuthorizedDurableAndUnblocksModelRequirement(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	content := []byte("authenticated local transcription model")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/whisper-small.bin" {
			t.Fatalf("resource path=%s", request.URL.Path)
		}
		writer.Header().Set("Content-Length", domain.NewInt64(int64(len(content))).String())
		writer.Header().Set("ETag", `"fixture-model-v1"`)
		_, _ = writer.Write(content)
	}))
	defer server.Close()
	entry := resourceCatalogEntry(t, server.URL+"/whisper-small.bin", content)
	resources, err := application.NewProductResources(
		store, []application.ProductResourceCatalogEntry{entry},
		application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resources.List(agentContextForResource(t)); !isAuthorityDenied(err) {
		t.Fatalf("Agent resource list error=%v", err)
	}
	creator := creatorContext(t)
	requested, err := resources.Acquire(creator, entry.Name, application.AcquireProductResourceInput{
		RequestID: mustRequestID(t, "ui:resource:whisper-small:v1"),
	})
	if err != nil || requested.Resource.State != application.ProductResourceQueued || requested.Resource.JobID == nil {
		t.Fatalf("requested=%+v err=%v", requested, err)
	}
	replayed, err := resources.Acquire(creator, entry.Name, application.AcquireProductResourceInput{
		RequestID: mustRequestID(t, "ui:resource:whisper-small:v1"),
	})
	if err != nil || !replayed.Replayed || replayed.Resource.JobID == nil ||
		*replayed.Resource.JobID != *requested.Resource.JobID {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	downloader, err := service.NewProductResourceDownloader(
		server.Client(), filepath.Join(dataDir, "work", "product-resource-downloads"),
	)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := application.NewProductResourceWorkExecutor(
		store, downloader, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := application.NewWorkScheduler(
		store, []application.WorkJobExecutor{executor},
		application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
		application.WorkSchedulerSettings{
			LeaseOwner: "test-resource-worker", LeaseDuration: 30 * time.Second,
			PollInterval: 10 * time.Millisecond, Resources: resources.RuntimeRegistrations(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	executed, err := scheduler.RunOne(ctx)
	if err != nil || !executed {
		t.Fatalf("executed=%v err=%v", executed, err)
	}
	snapshot, err := resources.List(creator)
	if err != nil || len(snapshot.Resources) != 1 || snapshot.Resources[0].State != application.ProductResourceReady ||
		snapshot.Resources[0].ResourceID == nil || snapshot.Resources[0].JobID == nil {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	resourceID := snapshot.Resources[0].ResourceID.String()
	stored, err := os.ReadFile(filepath.Join(dataDir, "resources", "product", resourceID, "content.bin"))
	if err != nil || string(stored) != string(content) {
		t.Fatalf("stored=%q err=%v", stored, err)
	}

	projects, _, _, _ := testProjectApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	created, err := projects.Create(creator, application.CreateProjectInput{
		RequestID: mustRequestID(t, "ui:resource-test-project"), Name: "Transcript resource project",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "source.wav")
	if err := os.WriteFile(sourcePath, []byte("fixture source"), 0o600); err != nil {
		t.Fatal(err)
	}
	grant, err := sourceAccess.RegisterSelection(creator, service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "ui:resource-test-source"), Path: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creator, created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "ui:resource-test-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	registrations := []application.WorkExecutorRegistration{
		{Kind: domain.MediaJobIdentify, Version: application.InitialMediaProducer},
		{Kind: application.WorkJobResourceAcquire, Version: application.ProductResourceDownloaderV1},
	}
	if err := store.RecoverWorkJobs(ctx, registrations, resources.RuntimeRegistrations(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	detail, _, err := assetReads.Inspect(creator, created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range detail.Jobs {
		if job.Kind != domain.MediaJobTranscript {
			continue
		}
		for _, prerequisite := range job.Prerequisites {
			if prerequisite.Kind == domain.MediaPrerequisiteModel {
				t.Fatalf("ready authenticated model still blocks transcript: %+v", job)
			}
		}
		return
	}
	t.Fatal("transcript job was not registered")
}

func resourceCatalogEntry(
	t *testing.T,
	origin string,
	content []byte,
) application.ProductResourceCatalogEntry {
	t.Helper()
	hash := sha256.Sum256(content)
	size, _ := domain.NewUInt64(uint64(len(content)))
	entry, err := application.NewProductResourceCatalogEntry(
		application.TranscriptProfile, domain.ProductResourceTranscriptionModel,
		"fixture-small-v1", application.TranscriptProfile, origin, size,
		domain.Digest("sha256:"+hex.EncodeToString(hash[:])), domain.ProductResourceRetentionOffline,
	)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func agentContextForResource(t *testing.T) context.Context {
	t.Helper()
	agent, _ := domain.ParseAgentID("018f0000-0000-7000-8000-000000000301")
	ctx, err := application.ContextWithAuthority(context.Background(), application.Authority{
		Surface: application.AuthorityProductCLI, InstallationID: "installation-test",
		GrantID: "grant-test", Actor: domain.AgentActor(agent), Invocation: testAgentInvocation(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func isAuthorityDenied(err error) bool {
	return err == application.ErrAuthorityScopeDenied
}
