package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
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

func TestMediaHTTPTrustedSelectionRegistersPathSafeAssetAndBoundedReads(t *testing.T) {
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: mustRequestID(t, "gesture:http-media-project"), Name: "HTTP media",
	})
	if err != nil {
		t.Fatal(err)
	}
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, nil, nil, nil, nil, nil, creatorAuthorizer{},
	)
	server := httptest.NewServer(mux)
	defer server.Close()

	sourcePath := filepath.Join(t.TempDir(), "private footage.mov")
	if err := os.WriteFile(sourcePath, []byte("fixture-media"), 0o600); err != nil {
		t.Fatal(err)
	}
	selectionBody, _ := json.Marshal(service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "ui:source-grant:http"), Path: sourcePath,
	})
	selectionResponse := doJSON(t, server, http.MethodPost, "/v1/internal/platform/source-grants", selectionBody)
	selectionRaw, err := io.ReadAll(selectionResponse.Body)
	selectionResponse.Body.Close()
	if err != nil || selectionResponse.StatusCode != http.StatusOK || strings.Contains(string(selectionRaw), sourcePath) {
		t.Fatalf("selection status=%d body=%s err=%v", selectionResponse.StatusCode, selectionRaw, err)
	}
	var grant application.SourceGrantResult
	if err := json.Unmarshal(selectionRaw, &grant); err != nil || grant.Grant.DisplayName != filepath.Base(sourcePath) {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}
	executorInput, _ := json.Marshal(struct {
		Path                string                   `json:"path"`
		ExpectedObservation domain.SourceObservation `json:"expectedObservation"`
	}{Path: sourcePath, ExpectedObservation: grant.Grant.Observation})
	var executorOutput bytes.Buffer
	if err := service.RunMediaExecutor(
		[]string{"identify-v1"}, bytes.NewReader(executorInput), &executorOutput,
	); err != nil {
		t.Fatal(err)
	}
	var identified application.MediaIdentification
	if err := json.Unmarshal(executorOutput.Bytes(), &identified); err != nil {
		t.Fatal(err)
	}
	wantFingerprint := sha256.Sum256([]byte("fixture-media"))
	if identified.Fingerprint.String() != "sha256:"+hex.EncodeToString(wantFingerprint[:]) ||
		identified.Observation != grant.Grant.Observation || strings.Contains(executorOutput.String(), sourcePath) {
		t.Fatalf("identified=%+v output=%s", identified, executorOutput.String())
	}

	assetBody, _ := json.Marshal(application.RegisterAssetInput{
		RequestID: mustRequestID(t, "ui:asset-register:http"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	assetResponse := doJSON(
		t, server, http.MethodPost, "/v1/projects/"+created.Project.Project.ID.String()+"/assets", assetBody,
	)
	assetRaw, err := io.ReadAll(assetResponse.Body)
	assetResponse.Body.Close()
	if err != nil || assetResponse.StatusCode != http.StatusOK || strings.Contains(string(assetRaw), sourcePath) {
		t.Fatalf("asset status=%d body=%s err=%v", assetResponse.StatusCode, assetRaw, err)
	}
	var registered application.AssetRegisterResult
	if err := json.Unmarshal(assetRaw, &registered); err != nil || len(registered.Asset.Jobs) != 5 {
		t.Fatalf("registered=%+v err=%v", registered, err)
	}

	listResponse, err := server.Client().Get(
		server.URL + "/v1/projects/" + created.Project.Project.ID.String() + "/assets?limit=50",
	)
	if err != nil {
		t.Fatal(err)
	}
	listRaw, err := io.ReadAll(listResponse.Body)
	listResponse.Body.Close()
	if err != nil || listResponse.StatusCode != http.StatusOK || strings.Contains(string(listRaw), sourcePath) ||
		strings.Contains(string(listRaw), "sourceGrantId") {
		t.Fatalf("list status=%d body=%s err=%v", listResponse.StatusCode, listRaw, err)
	}
	var page application.AssetPage
	if err := json.Unmarshal(listRaw, &page); err != nil || len(page.Assets) != 1 ||
		page.Assets[0].ID != registered.Asset.Asset.ID {
		t.Fatalf("page=%+v err=%v", page, err)
	}

	inspectResponse, err := server.Client().Get(
		server.URL + "/v1/projects/" + created.Project.Project.ID.String() + "/assets/" +
			registered.Asset.Asset.ID.String(),
	)
	if err != nil {
		t.Fatal(err)
	}
	inspectRaw, err := io.ReadAll(inspectResponse.Body)
	inspectResponse.Body.Close()
	if err != nil || inspectResponse.StatusCode != http.StatusOK || strings.Contains(string(inspectRaw), sourcePath) ||
		strings.Contains(string(inspectRaw), "sourceGrantId") {
		t.Fatalf("inspect status=%d body=%s err=%v", inspectResponse.StatusCode, inspectRaw, err)
	}
}

func TestFFProbeOutputNormalizationPreservesExactStreamFacts(t *testing.T) {
	raw := []byte(`{
  "streams": [
    {
      "index": 1,
      "codec_name": "aac",
      "codec_type": "audio",
      "profile": "LC",
      "codec_tag_string": "mp4a",
      "time_base": "1/48000",
      "duration_ts": 48000,
      "sample_fmt": "fltp",
      "sample_rate": "48000",
      "channels": 2,
      "channel_layout": "stereo",
      "tags": {"language": "eng"},
      "disposition": {"default": 1, "dub": 0}
    },
    {
      "index": 0,
      "codec_name": "h264",
      "codec_type": "video",
      "profile": "High",
      "codec_tag_string": "avc1",
      "time_base": "1/90000",
      "duration_ts": 90000,
      "width": 1920,
      "height": 1080,
      "coded_width": 1920,
      "coded_height": 1088,
      "sample_aspect_ratio": "1:1",
      "avg_frame_rate": "30000/1001",
      "r_frame_rate": "30000/1001",
      "pix_fmt": "yuv420p",
      "color_range": "tv",
      "color_space": "bt709",
      "color_transfer": "bt709",
      "color_primaries": "bt709",
      "disposition": {"default": 1},
      "side_data_list": [{"rotation": -90}]
    }
  ],
  "format": {
    "format_name": "mov,mp4,m4a,3gp,3g2,mj2",
    "start_time": "0.000000",
    "duration": "1.000000",
    "bit_rate": "128000"
  }
}`)
	probe, err := service.DecodeFFProbeOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Container != "mov" || len(probe.ContainerAliases) != 5 || len(probe.Streams) != 2 ||
		probe.Duration == nil || probe.Duration.Value.Value() != 1 || probe.Duration.Scale != 1 ||
		probe.BitRate == nil || probe.BitRate.Value() != 128000 {
		t.Fatalf("probe=%+v", probe)
	}
	video := probe.Streams[0]
	audio := probe.Streams[1]
	if video.MediaType != domain.MediaVideo || video.Video == nil || video.Video.Rotation != 270 ||
		video.Video.AverageRate == nil || video.Video.AverageRate.Value.Value() != 30000 ||
		video.Video.AverageRate.Scale != 1001 || video.Duration == nil || video.Duration.Value.Value() != 1 ||
		audio.MediaType != domain.MediaAudio || audio.Audio == nil || audio.Audio.SampleRate != 48000 ||
		len(audio.Dispositions) != 1 || audio.Dispositions[0] != "default" {
		t.Fatalf("video=%+v audio=%+v", video, audio)
	}
}

func mustRequestID(t *testing.T, value string) domain.RequestID {
	t.Helper()
	request, err := domain.ParseRequestID(value)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func doJSON(t *testing.T, server *httptest.Server, method, path string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestSourceGrantAssetRegistrationJobsUndoAndReplayAreDurableAndPathSafe(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	projects, _, activity, runs := testProjectApplications(t, store)
	projectRequest, _ := domain.ParseRequestID("gesture:media-project")
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: projectRequest, Name: "Footage ingest",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := application.ClockFunc(func() time.Time {
		return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	})
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := "/Users/creator/private-launch-demo.mp4"
	byteSize, _ := domain.NewUInt64(1024)
	grantRequest, _ := domain.ParseRequestID("picker:source:001")
	grantInput := application.RegisterSourceGrantInput{
		RequestID: grantRequest, Platform: "mac", Kind: domain.SourceGrantLocalPath,
		DisplayName: "private-launch-demo.mp4",
		Observation: domain.SourceObservation{
			ByteSize: byteSize, ModifiedUnixNs: domain.NewInt64(123456789), FileIdentity: "dev:42:inode:81",
		},
		ProtectedMaterial: []byte(`{"schema":"open-cut/source-grant-material/local-path/v1","path":"` + sourcePath + `"}`),
	}
	grant, err := media.RegisterSourceGrant(creatorContext(t), grantInput)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Grant.DisplayName != "private-launch-demo.mp4" || grant.Grant.State != domain.SourceGrantActive {
		t.Fatalf("grant=%+v", grant)
	}
	assetRequest, _ := domain.ParseRequestID("gesture:asset-register:001")
	assetInput := application.RegisterAssetInput{
		RequestID: assetRequest, SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	}
	registered, err := media.RegisterAsset(creatorContext(t), created.Project.Project.ID, assetInput)
	if err != nil {
		t.Fatal(err)
	}
	if registered.Asset.Asset.DisplayName != grant.Grant.DisplayName ||
		registered.Asset.Availability != domain.AssetIdentifying || len(registered.Asset.Jobs) != 5 ||
		registered.Transaction.CommittedProjectRevision.Value() != 2 ||
		registered.Transaction.Operations[0].Type != domain.NormalizedPutAsset {
		t.Fatalf("registered=%+v", registered)
	}
	assertInitialMediaJobStates(t, registered.Asset.Jobs)
	serialized, err := json.Marshal(struct {
		Asset       domain.AssetDetail
		Transaction domain.EditTransaction
	}{registered.Asset, registered.Transaction})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), sourcePath) {
		t.Fatal("source path escaped through Asset or EditTransaction output")
	}
	page, err := activity.List(creatorContext(t), application.ListActivityInput{
		ProjectID: &created.Project.Project.ID, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	activityJSON, _ := json.Marshal(page)
	if strings.Contains(string(activityJSON), sourcePath) || len(page.Events) != 2 ||
		page.Events[1].Kind != "asset.registered" {
		t.Fatalf("activity=%s", activityJSON)
	}
	fingerprint, _ := domain.ParseDigest("sha256:" + strings.Repeat("e", 64))
	oneSecond, _ := domain.NewRationalTime(1, 1)
	audioTimeBase, _ := domain.NewRationalTime(1, 48000)
	scheduler := newTestWorkScheduler(t, store,
		[]application.MediaJobExecutor{
			fixedIdentifyExecutor{result: application.MediaIdentification{
				Fingerprint: fingerprint, Observation: grantInput.Observation,
			}},
			fixedProbeExecutor{result: application.MediaProbe{
				Container: "mov", ContainerAliases: []string{"mp4"}, Duration: &oneSecond,
				Streams: []domain.SourceStreamDescriptor{{
					Index: 0, MediaType: domain.MediaAudio, Codec: "aac", TimeBase: audioTimeBase,
					Duration: &oneSecond, Dispositions: []string{"default"},
					Audio: &domain.AudioStreamFacts{
						SampleFormat: "fltp", SampleRate: 48000, Channels: 2, ChannelLayout: "stereo",
					},
				}},
			}},
		}, clock, "api:test")
	if err := scheduler.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("scheduler executed=%v err=%v", executed, err)
	}
	assetReads, err := application.NewAssetReads(store)
	if err != nil {
		t.Fatal(err)
	}
	identifiedAsset, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if identifiedAsset.Availability != domain.AssetOnline || identifiedAsset.Fingerprint == nil ||
		*identifiedAsset.Fingerprint != fingerprint || identifiedAsset.AcceptedFingerprint == nil ||
		*identifiedAsset.AcceptedFingerprint != fingerprint {
		t.Fatalf("identified asset=%+v", identifiedAsset)
	}
	assertIdentifiedMediaJobStates(t, identifiedAsset.Jobs)
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("probe scheduler execution=%v err=%v", executed, err)
	}
	probedAsset, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || probedAsset.Facts == nil || probedAsset.Facts.Container != "mov" ||
		len(probedAsset.Facts.Streams) != 1 || probedAsset.Facts.Streams[0].Descriptor.Codec != "aac" ||
		len(probedAsset.Artifacts) != 1 || probedAsset.Artifacts[0].Kind != domain.ArtifactMediaFacts {
		t.Fatalf("probed asset=%+v err=%v", probedAsset, err)
	}
	assertProbedMediaJobStates(t, probedAsset.Jobs)
	if executed, err := scheduler.RunOne(ctx); err != nil || executed {
		t.Fatalf("unexpected third scheduler execution=%v err=%v", executed, err)
	}
	mediaActivity, err := activity.List(creatorContext(t), application.ListActivityInput{
		ProjectID: &created.Project.Project.ID, After: page.Cursor, Limit: 10,
	})
	if err != nil || len(mediaActivity.Events) != 2 || mediaActivity.Events[0].Kind != "media.identified" ||
		mediaActivity.Events[1].Kind != "media.probed" || mediaActivity.Events[0].Actor != nil ||
		mediaActivity.Events[1].Actor != nil {
		t.Fatalf("media activity=%+v err=%v", mediaActivity, err)
	}

	agentCtx := createSQLiteAgentContext(t, store)
	runRequest, _ := domain.ParseRequestID("agent:media:undo-run")
	run, err := runs.Begin(agentCtx, created.Project.Project.ID, application.RunBeginInput{
		RequestID: runRequest, Intent: "Verify reversible footage registration",
	})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := application.NewEdits(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	undoRequest, _ := domain.ParseRequestID("agent:media:undo:001")
	undone, err := edits.Undo(
		agentCtx, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, registered.Transaction.ID,
		application.EditUndoInput{RequestID: undoRequest, Intent: "Remove the imported footage from creative state"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(undone.Transaction.Operations) != 1 || undone.Transaction.Operations[0].Asset == nil ||
		!undone.Transaction.Operations[0].Asset.Tombstoned ||
		undone.Transaction.CommittedProjectRevision.Value() != 3 {
		t.Fatalf("undone=%+v", undone)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedMedia, err := application.NewMedia(reopened, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	replayedGrant, err := reopenedMedia.RegisterSourceGrant(creatorContext(t), grantInput)
	if err != nil || !replayedGrant.Replayed || replayedGrant.Grant.ID != grant.Grant.ID {
		t.Fatalf("grant replay=%+v err=%v", replayedGrant, err)
	}
	replayedAsset, err := reopenedMedia.RegisterAsset(creatorContext(t), created.Project.Project.ID, assetInput)
	if err != nil || !replayedAsset.Replayed || replayedAsset.Asset.Asset.ID != registered.Asset.Asset.ID ||
		!replayedAsset.Asset.Asset.Tombstoned {
		t.Fatalf("asset replay=%+v err=%v", replayedAsset, err)
	}
	db, err := sql.Open("sqlite3", reopened.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var protectedMaterial []byte
	if err := db.QueryRowContext(ctx, `SELECT protected_material FROM source_grants WHERE id = ?`,
		grant.Grant.ID.String()).Scan(&protectedMaterial); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(protectedMaterial), sourcePath) {
		t.Fatal("SourceGrant material was not preserved inside its private repository boundary")
	}
	for _, table := range []string{"assets", "edit_proposals", "edit_transactions", "activity_outbox", "work_jobs"} {
		var leaked int
		query := `SELECT COUNT(*) FROM ` + table + ` WHERE CAST(` + mediaSearchColumn(table) + ` AS TEXT) LIKE ?`
		if err := db.QueryRowContext(ctx, query, "%"+sourcePath+"%").Scan(&leaked); err != nil {
			t.Fatal(err)
		}
		if leaked != 0 {
			t.Fatalf("source path leaked into %s", table)
		}
	}
}

func assertInitialMediaJobStates(t *testing.T, jobs []domain.MediaJobSummary) {
	t.Helper()
	states := make(map[domain.MediaJobKind]domain.MediaJobSummary, len(jobs))
	for _, job := range jobs {
		states[job.Kind] = job
	}
	if states[domain.MediaJobIdentify].State != domain.MediaJobBlocked ||
		states[domain.MediaJobProbe].State != domain.MediaJobBlocked ||
		states[domain.MediaJobProxy].State != domain.MediaJobBlocked ||
		states[domain.MediaJobWaveform].State != domain.MediaJobBlocked ||
		states[domain.MediaJobTranscript].State != domain.MediaJobBlocked ||
		!hasPrerequisiteKinds(states[domain.MediaJobIdentify], domain.MediaPrerequisiteExecutor) ||
		!hasPrerequisiteKinds(states[domain.MediaJobProbe], domain.MediaPrerequisiteExecutor, domain.MediaPrerequisiteFingerprint) ||
		!hasPrerequisiteKinds(states[domain.MediaJobTranscript], domain.MediaPrerequisiteExecutor, domain.MediaPrerequisiteFacts, domain.MediaPrerequisiteModel) {
		t.Fatalf("job states=%v", states)
	}
}

func assertIdentifiedMediaJobStates(t *testing.T, jobs []domain.MediaJobSummary) {
	t.Helper()
	states := make(map[domain.MediaJobKind]domain.MediaJobSummary, len(jobs))
	for _, job := range jobs {
		states[job.Kind] = job
	}
	if states[domain.MediaJobIdentify].State != domain.MediaJobSucceeded ||
		states[domain.MediaJobProbe].State != domain.MediaJobQueued ||
		len(states[domain.MediaJobProbe].Prerequisites) != 0 ||
		states[domain.MediaJobProxy].State != domain.MediaJobBlocked ||
		states[domain.MediaJobWaveform].State != domain.MediaJobBlocked ||
		states[domain.MediaJobTranscript].State != domain.MediaJobBlocked ||
		!hasPrerequisiteKinds(states[domain.MediaJobTranscript], domain.MediaPrerequisiteExecutor, domain.MediaPrerequisiteFacts, domain.MediaPrerequisiteModel) {
		t.Fatalf("identified job states=%v", states)
	}
}

func assertProbedMediaJobStates(t *testing.T, jobs []domain.MediaJobSummary) {
	t.Helper()
	states := make(map[domain.MediaJobKind]domain.MediaJobSummary, len(jobs))
	for _, job := range jobs {
		states[job.Kind] = job
	}
	if states[domain.MediaJobIdentify].State != domain.MediaJobSucceeded ||
		states[domain.MediaJobProbe].State != domain.MediaJobSucceeded ||
		states[domain.MediaJobProxy].State != domain.MediaJobBlocked ||
		states[domain.MediaJobWaveform].State != domain.MediaJobBlocked ||
		states[domain.MediaJobTranscript].State != domain.MediaJobBlocked ||
		!hasPrerequisiteKinds(states[domain.MediaJobTranscript], domain.MediaPrerequisiteExecutor, domain.MediaPrerequisiteModel) {
		t.Fatalf("probed job states=%v", states)
	}
}

func hasPrerequisiteKinds(job domain.MediaJobSummary, expected ...domain.MediaJobPrerequisiteKind) bool {
	if len(job.Prerequisites) != len(expected) {
		return false
	}
	actual := make(map[domain.MediaJobPrerequisiteKind]struct{}, len(job.Prerequisites))
	for _, prerequisite := range job.Prerequisites {
		actual[prerequisite.Kind] = struct{}{}
	}
	for _, kind := range expected {
		if _, exists := actual[kind]; !exists {
			return false
		}
	}
	return true
}

type fixedIdentifyExecutor struct {
	result application.MediaIdentification
	err    error
}

func (executor fixedIdentifyExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{
		Kind: domain.MediaJobIdentify, Version: application.InitialMediaProducer,
	}
}

func (executor fixedIdentifyExecutor) Execute(
	context.Context,
	application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	if executor.err != nil {
		return application.MediaJobExecution{}, executor.err
	}
	return application.MediaJobExecution{Identification: &executor.result}, nil
}

type fixedProbeExecutor struct {
	result  application.MediaProbe
	err     error
	version string
}

func (executor fixedProbeExecutor) Registration() application.MediaExecutorRegistration {
	version := executor.version
	if version == "" {
		version = "ffprobe-fixture-v1"
	}
	return application.MediaExecutorRegistration{Kind: domain.MediaJobProbe, Version: version}
}

func (executor fixedProbeExecutor) Execute(
	context.Context,
	application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	if executor.err != nil {
		return application.MediaJobExecution{}, executor.err
	}
	return application.MediaJobExecution{Probe: &executor.result}, nil
}

func mediaSearchColumn(table string) string {
	switch table {
	case "assets":
		return "display_name"
	case "edit_proposals":
		return "canonical_json"
	case "edit_transactions":
		return "operation_json"
	case "activity_outbox":
		return "payload_json"
	case "work_jobs":
		return "parameters_json"
	default:
		panic("unexpected media table")
	}
}

func newTestWorkScheduler(
	t *testing.T,
	store *repository.SQLiteProjects,
	executors []application.MediaJobExecutor,
	clock application.Clock,
	leaseOwner string,
) *application.WorkScheduler {
	t.Helper()
	workExecutors, err := application.NewMediaWorkExecutors(
		store, executors, application.UUIDv7IdentityGenerator{}, clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := application.NewWorkScheduler(
		store, workExecutors, application.UUIDv7IdentityGenerator{}, clock,
		application.WorkSchedulerSettings{
			LeaseOwner: leaseOwner, LeaseDuration: 30 * time.Second, PollInterval: 10 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

// newTestWorkSchedulerWithResources runs media executors alongside non-media
// work in one scheduler, which is what the transcript pipeline needs: model
// acquisition and transcription are separate job kinds that must settle in the
// same queue.
func newTestWorkSchedulerWithResources(
	t *testing.T,
	store *repository.SQLiteProjects,
	executors []application.MediaJobExecutor,
	additional []application.WorkJobExecutor,
	resources []application.ProductResourceRegistration,
	clock application.Clock,
	leaseOwner string,
) *application.WorkScheduler {
	t.Helper()
	workExecutors, err := application.NewMediaWorkExecutors(
		store, executors, application.UUIDv7IdentityGenerator{}, clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	workExecutors = append(workExecutors, additional...)
	scheduler, err := application.NewWorkScheduler(
		store, workExecutors, application.UUIDv7IdentityGenerator{}, clock,
		application.WorkSchedulerSettings{
			LeaseOwner: leaseOwner, LeaseDuration: 30 * time.Second,
			PollInterval: 10 * time.Millisecond, Resources: resources,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}
