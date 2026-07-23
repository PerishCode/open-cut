package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestRealFrameCommandPublishesArtifactAndTurnScopedLeases(t *testing.T) {
	serialAPITest(t, "uses the shared built media closure and native process budget")
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	closureRoot := filepath.Join(repositoryRoot, "apps", "api", "dist", "sidecar")
	verified, err := mediatoolchain.Load(closureRoot, target.Host())
	if err != nil {
		t.Skipf("built media toolchain unavailable: %v", err)
	}
	apiExecutable := filepath.Join(closureRoot, "api-sidecar.exe")
	if info, err := os.Stat(apiExecutable); err != nil || !info.Mode().IsRegular() {
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
		RequestID: mustRequestID(t, "gesture:real-frame-project"), Name: "Real frame pipeline",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 15, 0, 0, 0, time.UTC)
	clock := application.ClockFunc(func() time.Time { return now })
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	sourceAccess, err := service.NewSourceAccess(media, store)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "canonical.avi")
	if err := mediatoolchain.WriteCanonicalConformanceFixture(sourcePath); err != nil {
		t.Fatal(err)
	}
	sourcePath, err = filepath.EvalSymlinks(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := sourceAccess.RegisterSelection(creatorContext(t), service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "picker:real-frame-source"), Path: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creatorContext(t), created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "gesture:real-frame-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx := createSQLiteAgentContext(t, store)
	runs, err := application.NewAgentRuns(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	run, err := runs.Begin(agentCtx, created.Project.Project.ID, application.RunBeginInput{
		RequestID: mustRequestID(t, "agent:real-frame-run"), Intent: "Inspect exact source frames",
	})
	if err != nil {
		t.Fatal(err)
	}
	probeTool := verified.Capabilities[mediatoolchain.CapabilityProbeV1].Entry
	frameTool := verified.Capabilities[mediatoolchain.CapabilityFrameRGBV1].Entry
	proxyTool := verified.Capabilities[mediatoolchain.CapabilitySourceProxyV1].Entry
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
	frameVersion := verified.Manifest.Version + "@" + probeTool.SHA256 + "@" + frameTool.SHA256 +
		"/" + application.FrameSetProfile
	frame, err := service.NewExternalMediaFrameExecutor(
		sourceAccess, probeTool.Path, frameTool.Path, frameVersion, attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	proxyVersion := verified.Manifest.Version + "@" + probeTool.SHA256 + "@" + proxyTool.SHA256 +
		"/" + application.SourceProxyProfile
	proxy, err := service.NewExternalMediaProxyExecutor(
		sourceAccess, probeTool.Path, proxyTool.Path, proxyVersion, attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := newTestWorkScheduler(t,
		store, []application.MediaJobExecutor{identify, probe, frame, proxy},
		clock, "api:real-frame-test")
	if err := scheduler.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("prerequisite execution %d executed=%v err=%v", index, executed, runErr)
		}
	}
	reads, _ := application.NewAssetReads(store)
	asset, _, err := reads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || asset.Facts == nil || len(asset.Facts.Streams) != 2 {
		t.Fatalf("probed asset=%+v err=%v", asset, err)
	}
	var videoStream, audioStream domain.SourceStreamID
	for _, stream := range asset.Facts.Streams {
		if stream.Descriptor.MediaType == domain.MediaVideo {
			videoStream = stream.ID
		} else if stream.Descriptor.MediaType == domain.MediaAudio {
			audioStream = stream.ID
		}
	}
	zero, _ := domain.NewRationalTime(0, 1)
	quarter, _ := domain.NewRationalTime(1, 4)
	threeQuarters, _ := domain.NewRationalTime(3, 4)
	frameInput := application.RequestMediaFramesInput{
		SourceStreamID: videoStream, Times: []domain.RationalTime{zero, quarter, threeQuarters},
	}
	accepted, err := media.RequestFrames(
		agentCtx, created.Project.Project.ID, registered.Asset.Asset.ID,
		run.Run.ID, run.Run.CurrentTurn.ID, frameInput,
	)
	if err != nil || accepted.Status != application.MediaFrameSetAccepted || len(accepted.Resources) != 0 {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("frame execution=%v err=%v", executed, err)
	}
	ready, err := media.RequestFrames(
		agentCtx, created.Project.Project.ID, registered.Asset.Asset.ID,
		run.Run.ID, run.Run.CurrentTurn.ID, frameInput,
	)
	if err != nil || ready.Status != application.MediaFrameSetReady || ready.ArtifactID == nil ||
		ready.Job.ID != accepted.Job.ID || len(ready.Resources) != 3 {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("proxy execution=%v err=%v", executed, err)
	}
	proxiedAsset, _, err := reads.Inspect(
		creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	var proxyArtifactID domain.ArtifactID
	for _, artifact := range proxiedAsset.Artifacts {
		if artifact.Kind == domain.ArtifactProxy {
			proxyArtifactID = artifact.ID
		}
	}
	if proxyArtifactID.IsZero() {
		t.Fatalf("proxy artifact missing: %+v", proxiedAsset.Artifacts)
	}
	proxyRoot := filepath.Join(dataDir, "artifacts", "media", proxyArtifactID.String())
	proxyManifestBytes, err := os.ReadFile(filepath.Join(proxyRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	proxyManifest, err := application.DecodeSourceProxyArtifactManifest(proxyManifestBytes)
	if err != nil || proxyManifest.Video == nil || proxyManifest.Audio == nil ||
		proxyManifest.Video.FrameCount.Value() != 2 || proxyManifest.Profile != application.SourceProxyProfile {
		t.Fatalf("proxy manifest=%+v err=%v", proxyManifest, err)
	}
	mapBytes, err := os.ReadFile(filepath.Join(proxyRoot, proxyManifest.Video.TimeMap.Path))
	if err != nil || application.ValidateSourceProxyTimeMap(mapBytes, 2) != nil {
		t.Fatalf("proxy time map bytes=%d err=%v", len(mapBytes), err)
	}
	proxyMediaPath := filepath.Join(proxyRoot, proxyManifest.Media.Path)
	proxyMediaBytes, err := os.ReadFile(proxyMediaPath)
	if err != nil {
		t.Fatal(err)
	}
	proxyMediaFile, deliveredManifest, err := store.OpenSourceProxyMedia(
		ctx, created.Project.Project.ID, registered.Asset.Asset.ID, proxyArtifactID,
	)
	if err != nil || deliveredManifest.Media != proxyManifest.Media {
		t.Fatalf("open source proxy media=%+v err=%v", deliveredManifest.Media, err)
	}
	deliveredBytes, readErr := io.ReadAll(proxyMediaFile)
	closeErr := proxyMediaFile.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(deliveredBytes, proxyMediaBytes) {
		t.Fatalf("delivered proxy bytes=%d read=%v close=%v", len(deliveredBytes), readErr, closeErr)
	}
	corruptProxyMedia := append([]byte(nil), proxyMediaBytes...)
	corruptProxyMedia[len(corruptProxyMedia)/2] ^= 0xff
	if err := os.WriteFile(proxyMediaPath, corruptProxyMedia, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(proxyMediaPath, now.Add(time.Second), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileMediaArtifactStorage(ctx); err != nil {
		t.Fatalf("large proxy digest leaked into API readiness: %v", err)
	}
	if file, _, err := store.OpenSourceProxyMedia(
		ctx, created.Project.Project.ID, registered.Asset.Asset.ID, proxyArtifactID,
	); err == nil {
		file.Close()
		t.Fatal("tampered proxy media reused its integrity cache")
	}
	if err := os.WriteFile(proxyMediaPath, proxyMediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(proxyMediaPath, now.Add(2*time.Second), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if file, _, err := store.OpenSourceProxyMedia(
		ctx, created.Project.Project.ID, registered.Asset.Asset.ID, proxyArtifactID,
	); err != nil {
		t.Fatalf("restored proxy media failed integrity verification: %v", err)
	} else if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	viewerMedia, err := application.NewViewerMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	mediaLeases, err := service.NewMediaLeaseService(
		viewerMedia, store, application.UUIDv7IdentityGenerator{}, clock, strings.NewReader(strings.Repeat("m", 256)),
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions, privateKey := newTestUISessions(t, store, clock, false)
	_, projectReads, activityReads, _ := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	assetReads, err := application.NewAssetReads(store)
	if err != nil {
		t.Fatal(err)
	}
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, projectReads, activityReads, runs, edits, editReads,
		media, assetReads, sourceAccess, mediaLeases, nil, nil, nil, nil, sessions,
	)
	server := httptest.NewServer(mux)
	defer server.Close()
	uiSession := issueTestUISession(t, sessions, privateKey, "electron-media-1")
	if proxiedAsset.AcceptedFingerprint == nil {
		t.Fatal("proxied asset has no accepted fingerprint")
	}
	sourcePreviewRequest := service.MediaLeaseRequest{
		Purpose: application.MediaLeaseSourcePreview, AssetRevision: proxiedAsset.Revision,
		Fingerprint:   *proxiedAsset.AcceptedFingerprint,
		VideoStreamID: &videoStream, AudioStreamID: &audioStream,
	}
	leaseResponse := postJSON(
		t, server,
		"/v1/projects/"+created.Project.Project.ID.String()+"/assets/"+registered.Asset.Asset.ID.String()+"/media-leases",
		sourcePreviewRequest, uiSession,
	)
	if leaseResponse.Code != http.StatusOK {
		t.Fatalf("media lease status=%d body=%s", leaseResponse.Code, leaseResponse.Body.String())
	}
	var leaseResult service.MediaLeaseResult
	if err := json.NewDecoder(leaseResponse.Body).Decode(&leaseResult); err != nil ||
		leaseResult.Status != application.MediaPreparationPreparing || leaseResult.Stage == nil ||
		*leaseResult.Stage != application.MediaPreparationProxy || leaseResult.Lease != nil {
		t.Fatalf("initial media lease=%+v err=%v", leaseResult, err)
	}
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("explicit source proxy execution=%v err=%v", executed, err)
	}
	leaseResult = awaitSourcePreviewLease(
		t, server, created.Project.Project.ID, registered.Asset.Asset.ID, sourcePreviewRequest, uiSession,
	)
	if leaseResult.Lease == nil {
		t.Fatalf("media lease=%+v err=%v", leaseResult, err)
	}
	positionResponse := postJSON(
		t, server,
		"/v1/projects/"+created.Project.Project.ID.String()+"/assets/"+registered.Asset.Asset.ID.String()+"/source-position",
		service.SourcePositionRequest{
			ResourceID: leaseResult.Lease.ResourceID, Operation: service.SourcePositionSettle, Target: zero,
		},
		uiSession,
	)
	var position service.SourcePositionResult
	if positionResponse.Code != http.StatusOK || json.NewDecoder(positionResponse.Body).Decode(&position) != nil ||
		position.ResourceID != leaseResult.Lease.ResourceID || position.ProjectID != created.Project.Project.ID ||
		position.AssetID != registered.Asset.Asset.ID || position.AssetRevision != sourcePreviewRequest.AssetRevision ||
		position.Fingerprint != sourcePreviewRequest.Fingerprint || position.VideoStreamID == nil ||
		*position.VideoStreamID != videoStream || position.AudioStreamID == nil ||
		*position.AudioStreamID != audioStream || position.Operation != service.SourcePositionSettle ||
		position.Boundary != service.SourcePositionVideoPresentation {
		t.Fatalf("source position status=%d result=%+v", positionResponse.Code, position)
	}
	now = now.Add(time.Second)
	renewedUISession := issueTestUISession(t, sessions, privateKey, "electron-media-1")
	renewedPosition := postJSON(
		t, server,
		"/v1/projects/"+created.Project.Project.ID.String()+"/assets/"+registered.Asset.Asset.ID.String()+"/source-position",
		service.SourcePositionRequest{
			ResourceID: leaseResult.Lease.ResourceID, Operation: service.SourcePositionSettle, Target: zero,
		},
		renewedUISession,
	)
	if renewedPosition.Code != http.StatusOK {
		t.Fatalf("rotated UI session lost its media lease: %s", renewedPosition.Body.String())
	}
	crossSessionPosition := postJSON(
		t, server,
		"/v1/projects/"+created.Project.Project.ID.String()+"/assets/"+registered.Asset.Asset.ID.String()+"/source-position",
		service.SourcePositionRequest{
			ResourceID: leaseResult.Lease.ResourceID, Operation: service.SourcePositionSettle, Target: zero,
		},
		issueTestUISession(t, sessions, privateKey, "electron-media-position-copy"),
	)
	if crossSessionPosition.Code == http.StatusOK {
		t.Fatalf("cross-session source position was accepted: %s", crossSessionPosition.Body.String())
	}
	proxyArtifactID = leaseResult.Lease.ArtifactID
	proxyRoot = filepath.Join(dataDir, "artifacts", "media", proxyArtifactID.String())
	proxyManifestBytes, err = os.ReadFile(filepath.Join(proxyRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	proxyManifest, err = application.DecodeSourceProxyArtifactManifest(proxyManifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	proxyMediaPath = filepath.Join(proxyRoot, proxyManifest.Media.Path)
	proxyMediaBytes, err = os.ReadFile(proxyMediaPath)
	if err != nil {
		t.Fatal(err)
	}
	contentPath := strings.TrimPrefix(leaseResult.Lease.SameOriginURL, "/api")
	contentRequest := httptest.NewRequest(http.MethodGet, server.URL+contentPath, nil)
	contentRequest.Header.Set("X-Open-Cut-UI-Session", uiSession)
	contentRequest.Header.Set("Range", "bytes=0-31")
	contentResponse := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(contentResponse, contentRequest)
	if contentResponse.Code != http.StatusPartialContent || contentResponse.Body.Len() != 32 ||
		contentResponse.Header().Get("Content-Range") == "" || contentResponse.Header().Get("ETag") != leaseResult.Lease.ETag {
		t.Fatalf("media range status=%d headers=%v bytes=%d", contentResponse.Code, contentResponse.Header(), contentResponse.Body.Len())
	}
	invalidRangeRequest := httptest.NewRequest(http.MethodGet, server.URL+contentPath, nil)
	invalidRangeRequest.Header.Set("X-Open-Cut-UI-Session", uiSession)
	invalidRangeRequest.Header.Set("Range", "bytes=0-1,4-5")
	invalidRangeResponse := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(invalidRangeResponse, invalidRangeRequest)
	if invalidRangeResponse.Code != http.StatusRequestedRangeNotSatisfiable ||
		invalidRangeResponse.Header().Get("Content-Range") == "" {
		t.Fatalf("invalid media range status=%d headers=%v", invalidRangeResponse.Code, invalidRangeResponse.Header())
	}
	headRequest := httptest.NewRequest(http.MethodHead, server.URL+contentPath, nil)
	headRequest.Header.Set("X-Open-Cut-UI-Session", uiSession)
	headResponse := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(headResponse, headRequest)
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 ||
		headResponse.Header().Get("Content-Length") != proxyManifest.Media.ByteSize.String() {
		t.Fatalf("media HEAD status=%d headers=%v bytes=%d", headResponse.Code, headResponse.Header(), headResponse.Body.Len())
	}
	copiedRequest := httptest.NewRequest(http.MethodGet, server.URL+contentPath, nil)
	copiedRequest.Header.Set("X-Open-Cut-UI-Session", issueTestUISession(t, sessions, privateKey, "electron-media-2"))
	copiedResponse := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(copiedResponse, copiedRequest)
	if copiedResponse.Code != http.StatusNotFound {
		t.Fatalf("copied media lease status=%d body=%s", copiedResponse.Code, copiedResponse.Body.String())
	}
	corruptProxyMedia = append([]byte(nil), proxyMediaBytes...)
	corruptProxyMedia[len(corruptProxyMedia)/3] ^= 0xff
	if err := os.WriteFile(proxyMediaPath, corruptProxyMedia, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(proxyMediaPath, now.Add(3*time.Second), now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	rejectedResponse := postJSON(
		t, server,
		"/v1/projects/"+created.Project.Project.ID.String()+"/assets/"+registered.Asset.Asset.ID.String()+"/media-leases",
		sourcePreviewRequest, uiSession,
	)
	var rejectedPreparation service.MediaLeaseResult
	if rejectedResponse.Code != http.StatusOK ||
		json.NewDecoder(rejectedResponse.Body).Decode(&rejectedPreparation) != nil ||
		rejectedPreparation.Status != application.MediaPreparationPreparing || rejectedPreparation.Stage == nil ||
		*rejectedPreparation.Stage != application.MediaPreparationIntegrity || rejectedPreparation.Lease != nil {
		t.Fatalf("integrity rejection start status=%d result=%+v", rejectedResponse.Code, rejectedPreparation)
	}
	retryPreparation := awaitSourcePreviewRetry(
		t, server, created.Project.Project.ID, registered.Asset.Asset.ID,
		sourcePreviewRequest, uiSession, leaseResult.Job.ID,
	)
	if retryPreparation.Job.State != domain.MediaJobBlocked && retryPreparation.Job.State != domain.MediaJobQueued {
		t.Fatalf("integrity retry was not schedulable: %+v", retryPreparation)
	}
	if _, err := os.Stat(proxyRoot); !os.IsNotExist(err) {
		t.Fatalf("rejected proxy bytes were not quarantined: %v", err)
	}
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("proxy repair execution=%v err=%v", executed, err)
	}
	repairedCandidate, err := os.ReadFile(proxyMediaPath)
	if err != nil || !bytes.Equal(repairedCandidate, proxyMediaBytes) {
		t.Fatalf("proxy repair candidate bytes=%d err=%v", len(repairedCandidate), err)
	}
	repairedLease := awaitSourcePreviewLease(
		t, server, created.Project.Project.ID, registered.Asset.Asset.ID, sourcePreviewRequest, uiSession,
	)
	if repairedLease.Job.ID != retryPreparation.Job.ID || repairedLease.Lease == nil ||
		repairedLease.Lease.ArtifactID != proxyArtifactID {
		t.Fatalf("proxy repair changed semantic identity: retry=%+v repaired=%+v", retryPreparation, repairedLease)
	}
	repairedBytes, err := os.ReadFile(proxyMediaPath)
	if err != nil || !bytes.Equal(repairedBytes, proxyMediaBytes) {
		t.Fatalf("proxy repair bytes=%d err=%v", len(repairedBytes), err)
	}
	currentProject, err := projectReads.Show(agentCtx, created.Project.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoTrack, audioTrack application.TrackSummary
	for _, track := range currentProject.Tracks {
		switch track.Type {
		case domain.TrackVideo:
			videoTrack = track
		case domain.TrackAudio:
			audioTrack = track
		}
	}
	if videoTrack.ID.IsZero() || audioTrack.ID.IsZero() || videoStream.IsZero() || audioStream.IsZero() {
		t.Fatalf("clip inputs are incomplete: tracks=%+v streams=%s/%s", currentProject.Tracks, videoStream, audioStream)
	}
	one, _ := domain.NewRationalTime(1, 1)
	clipRange, _ := domain.NewTimeRange(zero, one)
	videoLocal, _ := domain.ParseLocalID("rough_cut_video")
	audioLocal, _ := domain.ParseLocalID("rough_cut_audio")
	groupLocal, _ := domain.ParseLocalID("rough_cut_av")
	enabled := true
	clipProposal, err := edits.Propose(
		agentCtx, currentProject.Project.ID, currentProject.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID:           mustRequestID(t, "agent:real-frame:add-clips"),
			Intent:              "Assemble the explicit linked source video and audio rough cut",
			BaseProjectRevision: currentProject.Project.Revision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityTrack, ID: videoTrack.ID.String(), Revision: videoTrack.Revision},
				{Kind: domain.EntityTrack, ID: audioTrack.ID.String(), Revision: audioTrack.Revision},
				{Kind: domain.EntityAsset, ID: registered.Asset.Asset.ID.String(), Revision: registered.Asset.Asset.Revision},
			},
			Operations: []application.EditOperationInput{
				{
					Type: domain.EditAddClip, CreateAs: &videoLocal, CreateLinkGroupAs: &groupLocal,
					TrackID: &videoTrack.ID, AssetID: &registered.Asset.Asset.ID, SourceStreamID: &videoStream,
					SourceRange: &clipRange, TimelineRange: &clipRange, Enabled: &enabled,
				},
				{
					Type: domain.EditAddClip, CreateAs: &audioLocal,
					TrackID: &audioTrack.ID, AssetID: &registered.Asset.Asset.ID, SourceStreamID: &audioStream,
					SourceRange: &clipRange, TimelineRange: &clipRange, Enabled: &enabled,
					LinkGroup: &application.EditReference{Local: &groupLocal},
				},
			},
		},
	)
	if err != nil || len(clipProposal.Proposal.Allocation) != 3 || len(clipProposal.Proposal.Operations) != 3 {
		t.Fatalf("clip proposal=%+v err=%v", clipProposal, err)
	}
	clipCommit, err := edits.Apply(
		agentCtx, currentProject.Project.ID, currentProject.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, clipProposal.Proposal.ID,
		application.EditApplyInput{
			RequestID:      mustRequestID(t, "agent:real-frame:apply-clips"),
			ProposalDigest: clipProposal.Proposal.Digest,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	sequenceWindow, err := editReads.SequenceWindow(
		agentCtx, currentProject.Project.ID, currentProject.Project.MainSequenceID,
		nil, clipRange, "", 100,
	)
	if err != nil || len(sequenceWindow.Clips) != 2 || len(sequenceWindow.LinkGroups) != 1 ||
		sequenceWindow.Clips[0].LinkGroupID == nil || sequenceWindow.Clips[1].LinkGroupID == nil ||
		*sequenceWindow.Clips[0].LinkGroupID != *sequenceWindow.Clips[1].LinkGroupID {
		t.Fatalf("sequence clip window=%+v err=%v", sequenceWindow, err)
	}
	firstClipPage, err := editReads.SequenceWindow(
		agentCtx, currentProject.Project.ID, currentProject.Project.MainSequenceID,
		nil, clipRange, "", 1,
	)
	if err != nil || len(firstClipPage.Clips) != 1 || firstClipPage.NextAfter == "" {
		t.Fatalf("first clip page=%+v err=%v", firstClipPage, err)
	}
	secondClipPage, err := editReads.SequenceWindow(
		agentCtx, currentProject.Project.ID, currentProject.Project.MainSequenceID,
		nil, clipRange, firstClipPage.NextAfter, 1,
	)
	if err != nil || len(secondClipPage.Clips) != 1 ||
		secondClipPage.Clips[0].ID == firstClipPage.Clips[0].ID {
		t.Fatalf("second clip page=%+v err=%v", secondClipPage, err)
	}
	clipID := allocationID(t, clipProposal.Proposal, videoLocal)
	clipEntity, err := editReads.Entity(agentCtx, currentProject.Project.ID, domain.EntityClip, clipID)
	if err != nil || clipEntity.Clip == nil || clipEntity.Clip.SourceStreamID != videoStream {
		t.Fatalf("clip entity=%+v err=%v", clipEntity, err)
	}
	if len(clipCommit.Transaction.InverseOperations) != 3 ||
		clipCommit.Transaction.InverseOperations[2].Type != domain.NormalizedPutLinkGroup {
		t.Fatalf("clip inverse is not dependency-safe: %+v", clipCommit.Transaction.InverseOperations)
	}
	renderPlans, err := application.NewRenderPlans(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	compiledPreview, err := renderPlans.CompileSequencePreview(
		ctx, currentProject.Project.ID, currentProject.Project.MainSequenceID, sequenceWindow.SequenceRevision,
	)
	if err != nil || compiledPreview.Replayed || len(compiledPreview.Plan.Payload.Inputs) != 1 ||
		len(compiledPreview.Plan.Payload.Video) != 1 || len(compiledPreview.Plan.Payload.Audio) != 1 ||
		compiledPreview.Plan.Payload.Duration != one ||
		compiledPreview.Plan.Payload.Output.Profile != domain.SequencePreviewProfileV1 {
		t.Fatalf("compiled sequence preview=%+v err=%v", compiledPreview, err)
	}
	replayedPreview, err := renderPlans.CompileSequencePreview(
		ctx, currentProject.Project.ID, currentProject.Project.MainSequenceID, sequenceWindow.SequenceRevision,
	)
	if err != nil || !replayedPreview.Replayed || replayedPreview.Plan.Digest != compiledPreview.Plan.Digest {
		t.Fatalf("replayed sequence preview=%+v err=%v", replayedPreview, err)
	}
	wantSourceTimes := []domain.RationalTime{zero, zero, mustRational(t, 1, 2)}
	for index, resource := range ready.Resources {
		bytes, err := os.ReadFile(resource.ReadOnlyPath)
		if err != nil || !pathInside(dataDir, resource.ReadOnlyPath) || uint64(len(bytes)) != resource.ByteSize.Value() {
			t.Fatalf("resource=%+v bytes=%d err=%v", resource, len(bytes), err)
		}
		if resource.RequestedTime != frameInput.Times[index] || resource.SourceTime != wantSourceTimes[index] {
			t.Fatalf("resource[%d] requested=%v source=%v", index, resource.RequestedTime, resource.SourceTime)
		}
		digest := sha256.Sum256(bytes)
		if resource.SHA256.String() != "sha256:"+hex.EncodeToString(digest[:]) {
			t.Fatalf("resource digest=%s", resource.SHA256)
		}
	}
	artifactManifest := filepath.Join(dataDir, "artifacts", "media", ready.ArtifactID.String(), "manifest.json")
	if info, err := os.Stat(artifactManifest); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("artifact manifest unavailable: %v", err)
	}
	repeated, err := media.RequestFrames(
		agentCtx, created.Project.Project.ID, registered.Asset.Asset.ID,
		run.Run.ID, run.Run.CurrentTurn.ID, frameInput,
	)
	if err != nil || repeated.Job.ID != ready.Job.ID || len(repeated.Resources) != len(ready.Resources) ||
		repeated.Resources[0].ResourceID == ready.Resources[0].ResourceID {
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
	expiredPaths := make([]string, 0, len(ready.Resources)+len(repeated.Resources))
	for _, resource := range append(ready.Resources, repeated.Resources...) {
		expiredPaths = append(expiredPaths, resource.ReadOnlyPath)
	}
	if err := store.ReconcileMediaScratchLeases(ctx, now.Add(5*time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, leasePath := range expiredPaths {
		if _, err := os.Stat(leasePath); !os.IsNotExist(err) {
			t.Fatalf("expired scratch lease remained readable %s: %v", leasePath, err)
		}
	}
	now = now.Add(6 * time.Minute)
	fresh, err := media.RequestFrames(
		agentCtx, created.Project.Project.ID, registered.Asset.Asset.ID,
		run.Run.ID, run.Run.CurrentTurn.ID, frameInput,
	)
	if err != nil || len(fresh.Resources) != 3 {
		t.Fatalf("fresh leases=%+v err=%v", fresh, err)
	}
	leasePaths := make([]string, len(fresh.Resources))
	for index, resource := range fresh.Resources {
		leasePaths[index] = resource.ReadOnlyPath
	}
	agentScratch := filepath.Join(
		dataDir, "scratch", "runs", run.Run.ID.String(), "turns", run.Run.CurrentTurn.ID.String(), "agent",
	)
	if err := os.MkdirAll(agentScratch, 0o700); err != nil {
		t.Fatal(err)
	}
	agentSentinel := filepath.Join(agentScratch, "native-process-still-running")
	if err := os.WriteFile(agentSentinel, []byte("private runtime"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = runs.Complete(
		agentCtx, created.Project.Project.ID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.RunCompleteInput{
			RequestID:          mustRequestID(t, "agent:real-frame-complete"),
			ExpectedGeneration: run.Run.CurrentTurn.Generation,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, leasePath := range leasePaths {
		if _, err := os.Stat(leasePath); !os.IsNotExist(err) {
			t.Fatalf("terminal Turn retained scratch lease %s: %v", leasePath, err)
		}
	}
	if info, err := os.Stat(agentSentinel); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("Run completion removed adapter-owned scratch: %v", err)
	}
	if info, err := os.Stat(artifactManifest); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("terminal Turn removed canonical artifact: %v", err)
	}
	orphanAttemptValue, err := domain.GenerateUUIDv7(now)
	if err != nil {
		t.Fatal(err)
	}
	orphanAttemptRoot := filepath.Join(dataDir, "work", "media-attempts", orphanAttemptValue)
	if err := os.Mkdir(orphanAttemptRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileProductStorage(ctx, now); err != nil {
		t.Fatalf("valid canonical artifact failed restart recovery: %v", err)
	}
	if _, err := os.Stat(orphanAttemptRoot); !os.IsNotExist(err) {
		t.Fatalf("orphan attempt workspace survived recovery: %v", err)
	}
	proxyMapPath := filepath.Join(proxyRoot, proxyManifest.Video.TimeMap.Path)
	if err := os.WriteFile(proxyMapPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileMediaArtifactStorage(ctx); err == nil {
		t.Fatal("corrupt proxy artifact did not fail closed during restart recovery")
	}
	if err := os.WriteFile(proxyMapPath, mapBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileMediaArtifactStorage(ctx); err != nil {
		t.Fatalf("restored proxy artifact failed recovery: %v", err)
	}
	if err := os.WriteFile(artifactManifest, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileMediaArtifactStorage(ctx); err == nil {
		t.Fatal("corrupt durable artifact did not fail closed during restart recovery")
	}
}

func awaitSourcePreviewLease(
	t *testing.T,
	server *httptest.Server,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	request service.MediaLeaseRequest,
	session string,
) service.MediaLeaseResult {
	t.Helper()
	path := "/v1/projects/" + projectID.String() + "/assets/" + assetID.String() + "/media-leases"
	deadline := time.Now().Add(5 * time.Second)
	for {
		response := postJSON(
			t, server, path, request, session,
		)
		if response.Code != http.StatusOK {
			t.Fatalf("media lease status=%d body=%s", response.Code, response.Body.String())
		}
		var result service.MediaLeaseResult
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Status == application.MediaPreparationReady && result.Lease != nil {
			return result
		}
		if time.Now().After(deadline) {
			t.Fatalf("source preview did not become ready: %+v", result)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func awaitSourcePreviewRetry(
	t *testing.T,
	server *httptest.Server,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	request service.MediaLeaseRequest,
	session string,
	rejectedJobID domain.MediaJobID,
) service.MediaLeaseResult {
	t.Helper()
	path := "/v1/projects/" + projectID.String() + "/assets/" + assetID.String() + "/media-leases"
	deadline := time.Now().Add(5 * time.Second)
	for {
		response := postJSON(
			t, server, path, request, session,
		)
		if response.Code != http.StatusOK {
			t.Fatalf("media retry status=%d body=%s", response.Code, response.Body.String())
		}
		var result service.MediaLeaseResult
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Job.ID != rejectedJobID && result.Status == application.MediaPreparationPreparing &&
			result.Stage != nil && *result.Stage == application.MediaPreparationProxy && len(result.Diagnostics) == 1 &&
			result.Diagnostics[0].Code == application.MediaDiagnosticProxyIntegrityRejected &&
			result.Diagnostics[0].SubjectKind == application.MediaDiagnosticArtifact &&
			result.Diagnostics[0].Recovery == application.MediaRecoveryAutomaticRetry {
			return result
		}
		if time.Now().After(deadline) {
			t.Fatalf("source preview did not enter retry preparation: %+v", result)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mustRational(t *testing.T, value int64, scale int32) domain.RationalTime {
	t.Helper()
	result, err := domain.NewRationalTime(value, scale)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}
