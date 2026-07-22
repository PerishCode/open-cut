package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/internal/businessacceptance"
	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/whispertoolchain"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

// TestRealTranscriptCommandProducesTranscriptFromBothClosures exercises the one
// pipeline that spans two toolchain closures: the media closure normalizes real
// speech to canonical 16 kHz mono S16, and the whisper closure transcribes it.
//
// Transcription had no real-binary integration coverage before the closures
// were split — its only end-to-end proof was the conformance suite, which runs
// against a dummy test model and therefore never produces words. This asserts
// the product path with the production model instead: that the executor version
// is derived from the whisper closure rather than the media one, that the
// binding records the engine that actually ran, and that real speech becomes
// real tokens.
func TestRealTranscriptCommandProducesTranscriptFromBothClosures(t *testing.T) {
	serialAPITest(t, "uses shared media/model closures and a native transcription process")
	modelPath := os.Getenv("OPEN_CUT_TRANSCRIPTION_MODEL")
	if modelPath == "" {
		t.Skip("set OPEN_CUT_TRANSCRIPTION_MODEL to the pinned multilingual model")
	}
	modelPath, err := filepath.Abs(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	closureRoot := filepath.Join(repositoryRoot, "apps", "api", "dist", "sidecar")
	verified, err := mediatoolchain.Load(closureRoot, target.Host())
	if err != nil {
		t.Skipf("built media toolchain unavailable: %v", err)
	}
	whisper, err := whispertoolchain.Load(closureRoot, target.Host())
	if err != nil {
		t.Skipf("built whisper toolchain unavailable: %v", err)
	}
	transcriptCapability, exists := whisper.Capabilities[whispertoolchain.CapabilityLocalTranscriptionV1]
	if !exists {
		t.Skip("whisper closure does not carry the transcription capability")
	}
	apiExecutable := filepath.Join(closureRoot, "api-sidecar.exe")
	if info, statErr := os.Stat(apiExecutable); statErr != nil || !info.Mode().IsRegular() {
		t.Skip("built API executable is unavailable")
	}

	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, _, _, _ := testProjectApplications(t, store)
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: mustRequestID(t, "gesture:real-transcript-project"), Name: "Real transcript pipeline",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := application.ClockFunc(time.Now)
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	sourceAccess, err := service.NewSourceAccess(media, store)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "speech.webm")
	if err := businessacceptance.WriteSpeechFixture(sourcePath); err != nil {
		t.Fatal(err)
	}
	sourcePath, err = filepath.EvalSymlinks(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := sourceAccess.RegisterSelection(creatorContext(t), service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "picker:real-transcript-source"), Path: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(
		creatorContext(t), created.Project.Project.ID, application.RegisterAssetInput{
			RequestID: mustRequestID(t, "gesture:real-transcript-asset"), SourceGrantID: grant.Grant.ID,
			ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	// The model is served from disk rather than embedded: it is the real pinned
	// artifact, and acquiring it the ordinary way is what clears the job's
	// model-required prerequisite.
	entry := productionModelCatalogEntry(t, modelPath)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.ServeFile(writer, request, modelPath)
	}))
	defer server.Close()
	entry = rebindCatalogEntryOrigin(t, entry, server.URL+"/model.bin")
	resources, err := application.NewProductResources(
		store, []application.ProductResourceCatalogEntry{entry},
		application.UUIDv7IdentityGenerator{}, clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resources.Acquire(creatorContext(t), entry.Name, application.AcquireProductResourceInput{
		RequestID: mustRequestID(t, "ui:resource:real-transcript-model"),
	}); err != nil {
		t.Fatal(err)
	}
	downloader, err := service.NewProductResourceDownloader(
		server.Client(), filepath.Join(dataDir, "work", "product-resource-downloads"),
	)
	if err != nil {
		t.Fatal(err)
	}
	resourceExecutor, err := application.NewProductResourceWorkExecutor(
		store, downloader, application.UUIDv7IdentityGenerator{}, clock,
	)
	if err != nil {
		t.Fatal(err)
	}

	probeTool := verified.Capabilities[mediatoolchain.CapabilityProbeV1].Entry
	frameTool := verified.Capabilities[mediatoolchain.CapabilityFrameRGBV1].Entry
	attemptRoot := filepath.Join(dataDir, "work", "media-attempts")
	identify, err := service.NewExternalMediaIdentifyExecutor(
		sourceAccess, apiExecutable, attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := service.NewExternalMediaProbeExecutor(
		sourceAccess, probeTool.Path, verified.Manifest.Version+"@"+probeTool.SHA256,
		attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	models, err := service.NewTranscriptResourceAccess(store, dataDir, clock)
	if err != nil {
		t.Fatal(err)
	}
	// The executor version names the whisper closure only. That is the split:
	// the media closure supplies a normalizer, not the identity of the engine
	// whose output is being recorded.
	transcriptVersion := whisper.Manifest.Version + "/" + application.TranscriptProfile + "@" +
		transcriptCapability.ClosureSHA256 + "@" + whisper.Manifest.Build.RecipeSHA256
	if strings.Contains(transcriptVersion, "ffmpeg") {
		t.Fatalf("transcript executor version still names the media closure: %s", transcriptVersion)
	}
	transcript, err := service.NewExternalMediaTranscriptExecutor(
		sourceAccess, models, probeTool.Path, frameTool.Path, transcriptCapability.Entry.Path,
		transcriptVersion, whisper.Manifest.Target.String(),
		filepath.Join(dataDir, "work", "transcript-attempts"), lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}

	scheduler := newTestWorkSchedulerWithResources(t,
		store, []application.MediaJobExecutor{identify, probe, transcript},
		[]application.WorkJobExecutor{resourceExecutor},
		resources.RuntimeRegistrations(), clock, "api:real-transcript-test")
	if err := scheduler.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	// identify, probe, model acquisition and transcription, in whatever order
	// the scheduler picks them up.
	deadline := time.Now().Add(20 * time.Minute)
	for {
		executed, runErr := scheduler.RunOne(ctx)
		if runErr != nil {
			t.Fatalf("scheduler error: %v", runErr)
		}
		if !executed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transcript pipeline did not settle")
		}
	}

	reads, _ := application.NewAssetReads(store)
	asset, _, err := reads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	var transcriptArtifact *domain.ArtifactID
	for index, artifact := range asset.Artifacts {
		if artifact.Kind == domain.ArtifactTranscript && artifact.State == domain.ArtifactReady {
			transcriptArtifact = &asset.Artifacts[index].ID
		}
	}
	if transcriptArtifact == nil {
		t.Fatalf("no ready transcript artifact; jobs=%+v artifacts=%+v", asset.Jobs, asset.Artifacts)
	}

	transcriptReads, err := application.NewTranscriptReads(store)
	if err != nil {
		t.Fatal(err)
	}
	document, err := transcriptReads.Read(creatorContext(t), application.TranscriptReadQuery{
		ProjectID: created.Project.Project.ID, AssetID: registered.Asset.Asset.ID,
		ArtifactID: transcriptArtifact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Segments) == 0 {
		t.Fatal("real speech produced no transcript segments")
	}
	words := 0
	for _, segment := range document.Segments {
		words += len(segment.Tokens)
		if strings.TrimSpace(segment.Text) == "" {
			t.Fatalf("segment %s carries no text", segment.ID)
		}
	}
	if words == 0 {
		t.Fatal("real speech produced no transcript tokens")
	}
	t.Logf("segments=%d tokens=%d engine=%s", len(document.Segments), words, transcriptVersion)
}

func productionModelCatalogEntry(
	t *testing.T, path string,
) application.ProductResourceCatalogEntry {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written <= 0 {
		t.Fatalf("hash model: %v", err)
	}
	size, err := domain.NewUInt64(uint64(written))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := application.NewProductResourceCatalogEntry(
		application.TranscriptProfile, domain.ProductResourceTranscriptionModel,
		"pinned-small-v1", application.TranscriptProfile, "https://placeholder.invalid/model.bin",
		size, domain.Digest("sha256:"+hex.EncodeToString(digest.Sum(nil))),
		domain.ProductResourceRetentionOffline,
	)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func rebindCatalogEntryOrigin(
	t *testing.T, entry application.ProductResourceCatalogEntry, origin string,
) application.ProductResourceCatalogEntry {
	t.Helper()
	rebound, err := application.NewProductResourceCatalogEntry(
		entry.Name, entry.Kind, entry.Version, entry.Profile, origin,
		entry.ByteSize, entry.SHA256, entry.Retention,
	)
	if err != nil {
		t.Fatal(err)
	}
	return rebound
}
