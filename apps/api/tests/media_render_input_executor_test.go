package tests

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestRealRenderInputExecutorProducesStableExactStreamMaterial(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	closureRoot := filepath.Join(repositoryRoot, "apps", "api", "dist", "sidecar")
	verified, err := mediatoolchain.Load(closureRoot, target.Host())
	if err != nil {
		t.Skipf("built media toolchain unavailable: %v", err)
	}
	probeTool := verified.Capabilities[mediatoolchain.CapabilityProbeV1].Entry
	renderInputTool := verified.Capabilities[mediatoolchain.CapabilityRenderInputV1]
	if probeTool.Path == "" || renderInputTool.Entry.Path == "" {
		t.Skip("qualified render-input tools are unavailable")
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
	projects, projectReads, _, runs := testProjectApplications(t, store)
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: mustRequestID(t, "gesture:render-input-project"), Name: "Render input",
	})
	if err != nil {
		t.Fatal(err)
	}
	media, _, sourceAccess := testMediaApplications(t, store)
	sourcePath := buildAdmittedRenderInputSource(t, renderInputTool.Entry.Path)
	grant, err := sourceAccess.RegisterSelection(creatorContext(t), service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "picker:render-input-source"), Path: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creatorContext(t), created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "gesture:render-input-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	attemptRoot := filepath.Join(dataDir, "work", "media-attempts")
	identify, err := service.NewExternalMediaIdentifyExecutor(
		sourceAccess, apiExecutable, attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := service.NewExternalMediaProbeExecutor(
		sourceAccess, probeTool.Path, "render-input-probe-v1", attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	version := verified.Manifest.Version + "/" + application.RenderInputProfile + "@" +
		renderInputTool.ClosureSHA256 + "@" + verified.Manifest.Build.RecipeSHA256
	executor, err := service.NewExternalMediaRenderInputExecutor(
		sourceAccess, probeTool.Path, renderInputTool.Entry.Path, version,
		verified.Manifest.Target.String(), attemptRoot, lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 16, 1, 0, 0, 0, time.UTC)
	clock := application.ClockFunc(func() time.Time { return now })
	scheduler := newTestWorkScheduler(
		t, store, []application.MediaJobExecutor{identify, probe, executor}, clock, "api:render-input-test",
	)
	if err := scheduler.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("source prerequisite %d executed=%v err=%v", index, executed, runErr)
		}
		now = now.Add(time.Second)
	}
	reads, err := application.NewAssetReads(store)
	if err != nil {
		t.Fatal(err)
	}
	asset, _, err := reads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || asset.AcceptedFingerprint == nil || asset.Facts == nil {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	fingerprint := *asset.AcceptedFingerprint
	var stream *domain.SourceStream
	for index := range asset.Facts.Streams {
		if asset.Facts.Streams[index].Descriptor.MediaType == domain.MediaVideo {
			stream = &asset.Facts.Streams[index]
		}
	}
	if stream == nil || stream.Descriptor.Video == nil || stream.Descriptor.Video.ColorSpace != "bt709" ||
		stream.Descriptor.Video.PixelFormat != "yuv420p" {
		t.Fatalf("admitted video facts=%+v", stream)
	}
	streamID := stream.ID
	parameters := application.InitialMediaJobParameters{
		AssetID: registered.Asset.Asset.ID, Kind: domain.MediaJobRenderInput,
		Profile: application.RenderInputProfile,
		RenderInputSelection: &application.SourceProxySelection{
			Policy: application.SourceProxySelectionExplicit, VideoStreamID: &streamID,
		},
	}
	parametersJSON, parametersDigest, err := application.CanonicalInitialMediaJobParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	claim := application.MediaJobClaim{
		AssetID: registered.Asset.Asset.ID, Kind: domain.MediaJobRenderInput,
		ExecutorVersion: version, ExecutorTarget: verified.Manifest.Target.String(),
		ExpectedObservation: grant.Grant.Observation, AcceptedFingerprint: &fingerprint,
		ParametersDigest: parametersDigest, ParametersJSON: parametersJSON,
		SourceStreams: []domain.SourceStream{*stream},
	}
	first := executeRealRenderInput(t, ctx, executor, claim,
		mustRenderInputAttemptID(t, time.Date(2026, 7, 16, 1, 0, 2, 0, time.UTC)))
	defer first.Workspace.Release()
	second := executeRealRenderInput(t, ctx, executor, claim,
		mustRenderInputAttemptID(t, time.Date(2026, 7, 16, 1, 0, 3, 0, time.UTC)))
	defer second.Workspace.Release()
	if first.Video == nil || first.Audio != nil || first.Video.Source.ID != streamID ||
		first.Video.Codec != "ffv1" || first.Video.FrameCount.Value() == 0 {
		t.Fatalf("render-input execution=%+v", first)
	}
	firstMedia := readPreparedMedia(t, first.Workspace, first.Media.Path)
	secondMedia := readPreparedMedia(t, second.Workspace, second.Media.Path)
	if !bytes.Equal(firstMedia, secondMedia) || first.Media.SHA256 != second.Media.SHA256 {
		t.Fatal("render-input media was not byte stable")
	}
	firstMap := readPreparedMedia(t, first.Workspace, first.Video.TimeMap.Path)
	secondMap := readPreparedMedia(t, second.Workspace, second.Video.TimeMap.Path)
	if !bytes.Equal(firstMap, secondMap) ||
		application.ValidateSourceProxyTimeMap(firstMap, first.Video.FrameCount.Value()) != nil {
		t.Fatal("render-input time map was not stable and valid")
	}
	jobID := mustRenderInputWorkJobID(t, time.Date(2026, 7, 16, 1, 1, 0, 0, time.UTC))
	logicalKey := "media/v1/" + registered.Asset.Asset.ID.String() + "/render-input/" + parametersDigest.String()
	record := application.EnsureExplicitRenderInputJobRecord{
		JobID: jobID, ProjectID: created.Project.Project.ID, AssetID: registered.Asset.Asset.ID,
		Fingerprint: fingerprint, SourceStream: *stream, Parameters: parameters,
		Canonical: parametersJSON, Digest: parametersDigest, LogicalKey: logicalKey, CreatedAt: now,
	}
	ensured, err := store.EnsureExplicitRenderInputJob(ctx, record)
	if err != nil || ensured != jobID {
		t.Fatalf("ensured=%s err=%v", ensured, err)
	}
	record.JobID = mustRenderInputWorkJobID(t, time.Date(2026, 7, 16, 1, 1, 1, 0, time.UTC))
	reused, err := store.EnsureExplicitRenderInputJob(ctx, record)
	if err != nil || reused != jobID {
		t.Fatalf("reused=%s err=%v", reused, err)
	}
	now = now.Add(time.Second)
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("publication execution=%v err=%v", executed, err)
	}
	asset, _, err = reads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	var artifact *domain.ArtifactSummary
	for index := range asset.Artifacts {
		if asset.Artifacts[index].Kind == domain.ArtifactRenderInput {
			artifact = &asset.Artifacts[index]
		}
	}
	if artifact == nil || artifact.State != domain.ArtifactReady {
		t.Fatalf("render-input artifact=%+v", artifact)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(
		dataDir, "artifacts", "media", artifact.ID.String(), "manifest.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := application.DecodeRenderInputArtifactManifest(manifestBytes)
	if err != nil || manifest.Video == nil || manifest.Video.Source.ID != streamID || manifest.Audio != nil {
		t.Fatalf("published manifest=%+v err=%v", manifest, err)
	}
	if err := store.ReconcileMediaArtifactStorage(ctx); err != nil {
		t.Fatalf("render-input recovery failed: %v", err)
	}
	record.JobID = mustRenderInputWorkJobID(t, time.Date(2026, 7, 16, 1, 1, 2, 0, time.UTC))
	reused, err = store.EnsureExplicitRenderInputJob(ctx, record)
	if err != nil || reused != jobID {
		t.Fatalf("ready reuse=%s err=%v", reused, err)
	}

	overview, err := projectReads.Show(creatorContext(t), created.Project.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoTrack application.TrackSummary
	for _, track := range overview.Tracks {
		if track.Type == domain.TrackVideo {
			videoTrack = track
		}
	}
	sourceDuration := stream.Descriptor.Duration
	if sourceDuration == nil {
		sourceDuration = asset.Facts.Duration
	}
	if videoTrack.ID.IsZero() || sourceDuration == nil {
		t.Fatalf("video track=%+v stream=%+v", videoTrack, stream)
	}
	agentCtx := createSQLiteAgentContext(t, store)
	run, err := runs.Begin(agentCtx, overview.Project.ID, application.RunBeginInput{
		RequestID: mustRequestID(t, "agent:real-export-run"), Intent: "Render one real pinned export",
	})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := application.NewEdits(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	zero, _ := domain.NewRationalTime(0, 1)
	clipRange, err := domain.NewTimeRange(zero, *sourceDuration)
	if err != nil {
		t.Fatal(err)
	}
	local, _ := domain.ParseLocalID("real_export_video")
	enabled := true
	proposal, err := edits.Propose(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:real-export-propose"), Intent: "Add exact export clip",
			BaseProjectRevision: overview.Project.Revision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityTrack, ID: videoTrack.ID.String(), Revision: videoTrack.Revision},
				{Kind: domain.EntityAsset, ID: asset.ID.String(), Revision: asset.Revision},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditAddClip, CreateAs: &local, TrackID: &videoTrack.ID,
				AssetID: &asset.ID, SourceStreamID: &streamID,
				SourceRange: &clipRange, TimelineRange: &clipRange, Enabled: &enabled,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := edits.Apply(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		proposal.Proposal.ID, application.EditApplyInput{
			RequestID: mustRequestID(t, "agent:real-export-apply"), ProposalDigest: proposal.Proposal.Digest,
		},
	); err != nil {
		t.Fatal(err)
	}
	committed, err := projectReads.Show(creatorContext(t), overview.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	exportCapability := verified.Capabilities[mediatoolchain.CapabilitySequenceExportRendererV1]
	exportIdentity := application.RenderExecutorIdentity{
		Version: verified.Manifest.Version + "/" + application.SequenceExportRendererV1 + "@" +
			exportCapability.ClosureSHA256 + "@" + verified.Manifest.Build.RecipeSHA256,
		Target: verified.Manifest.Target.String(),
	}
	exports, err := application.NewSequenceExports(
		store, application.UUIDv7IdentityGenerator{}, clock,
		application.SequenceExportSettings{
			RendererVersion: exportIdentity.Version, RendererTarget: exportIdentity.Target,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	started, err := exports.Start(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.SequenceExportStartInput{
			RequestID:        mustRequestID(t, "agent:real-export-start"),
			SequenceRevision: committed.MainSequenceRevision, Preset: domain.SequenceExportProfileV1,
		},
	)
	if err != nil || started.Job.State != domain.MediaJobBlocked || started.Job.RootJobID != started.Job.ID {
		t.Fatalf("started=%+v err=%v", started, err)
	}
	creatorCtx := creatorContext(t)
	if observed, showErr := exports.ShowForCreator(
		creatorCtx, overview.Project.ID, application.SequenceExportShowInput{JobID: started.Job.ID},
	); showErr != nil || observed.Job.ID != started.Job.ID {
		t.Fatalf("Creator could not observe Agent-owned export: result=%+v err=%v", observed, showErr)
	}
	creatorStarted, err := exports.StartForCreator(
		creatorCtx, overview.Project.ID, overview.Project.MainSequenceID,
		application.SequenceExportStartInput{
			RequestID:        mustRequestID(t, "creator:real-export-start"),
			SequenceRevision: committed.MainSequenceRevision, Preset: domain.SequenceExportProfileV1,
		},
	)
	if err != nil || creatorStarted.Job.State != domain.MediaJobBlocked || creatorStarted.Job.ID == started.Job.ID {
		t.Fatalf("Creator export=%+v err=%v", creatorStarted, err)
	}
	if _, showErr := exports.Show(
		agentCtx, overview.Project.ID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.SequenceExportShowInput{JobID: creatorStarted.Job.ID},
	); !errors.Is(showErr, application.ErrRunStaleTurn) {
		t.Fatalf("AgentRun observed Creator-owned export: %v", showErr)
	}
	creatorCancelled, err := exports.CancelForCreator(
		creatorCtx, overview.Project.ID, application.SequenceExportCancelInput{
			RequestID: mustRequestID(t, "creator:real-export-cancel"), JobID: creatorStarted.Job.ID,
		},
	)
	if err != nil || creatorCancelled.Job.State != domain.MediaJobCancelled {
		t.Fatalf("Creator cancel=%+v err=%v", creatorCancelled, err)
	}
	resourceRoots := make(map[string]string, len(exportCapability.Resources))
	for _, resource := range exportCapability.Resources {
		resourceRoots[resource.ID] = resource.Root
	}
	ffmpeg := verified.Tools["ffmpeg"]
	exportRenderer, err := service.NewExternalSequenceExportRenderer(
		store, exportCapability.Entry.Path, exportIdentity,
		renderengine.ExecutionClosure{
			SHA256: domain.Digest(exportCapability.ClosureSHA256),
			Tools: map[string]renderengine.ExecutionToolPin{
				"ffmpeg": {Path: ffmpeg.Path, SHA256: domain.Digest(ffmpeg.SHA256)},
			},
		},
		resourceRoots, filepath.Join(dataDir, "work", "sequence-export-attempts"), lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	exportVerifier, err := service.NewExternalSequenceExportVerifier(
		probeTool.Path, filepath.Join(dataDir, "work", "sequence-export-verification"), lifecycle.ProfileHarness,
	)
	if err != nil {
		t.Fatal(err)
	}
	recordedRenderer := &recordingSequenceExportRenderer{inner: exportRenderer}
	recordedVerifier := &recordingSequenceExportVerifier{inner: exportVerifier}
	exportExecutor, err := application.NewSequenceExportWorkExecutor(
		store, recordedRenderer, recordedVerifier, application.UUIDv7IdentityGenerator{}, clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	executeExport := func(wantJob domain.WorkJobID, attemptAt time.Time) application.SequenceExportResult {
		t.Helper()
		registrations := []application.WorkExecutorRegistration{exportExecutor.Registration()}
		if err := store.RecoverWorkJobs(ctx, registrations, nil, now); err != nil {
			t.Fatal(err)
		}
		claim, err := store.ClaimWorkJob(ctx, application.ClaimWorkJobInput{
			AttemptID: mustRenderInputAttemptID(t, attemptAt), Executors: registrations,
			LeaseOwner: "api:real-export-test", Now: now, LeaseDuration: 30 * time.Second,
		})
		if err != nil || claim.JobID != wantJob || claim.SequenceExport == nil {
			t.Fatalf("export claim=%+v err=%v", claim, err)
		}
		if err := exportExecutor.Execute(ctx, claim); err != nil {
			t.Fatal(err)
		}
		result, err := exports.Show(agentCtx, overview.Project.ID, run.Run.ID, run.Run.CurrentTurn.ID,
			application.SequenceExportShowInput{JobID: wantJob})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	now = now.Add(time.Second)
	completed := executeExport(started.Job.ID, now.Add(time.Second))
	if completed.Job.State != domain.MediaJobSucceeded || completed.Job.Artifact == nil ||
		completed.Job.Artifact.State != domain.SequenceExportArtifactValid ||
		completed.Job.Artifact.Facts.VideoCodec != "vp9" || completed.Job.Artifact.Facts.AudioCodec != "opus" {
		terminal := ""
		if completed.Job.TerminalErrorCode != nil {
			terminal = *completed.Job.TerminalErrorCode
		}
		t.Fatalf("completed=%+v terminal=%q renderErr=%v verifyErr=%v",
			completed, terminal, recordedRenderer.err, recordedVerifier.err)
	}
	exportRoot := filepath.Join(dataDir, "artifacts", "sequence-export", completed.Job.Artifact.ID.String())
	if _, err := os.Stat(filepath.Join(exportRoot, "export.webm")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exportRoot, "export.webm"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := store.ReconcileProductStorage(ctx, now); err != nil {
		t.Fatalf("corrupt export blocked core readiness: %v", err)
	}
	invalid, err := exports.Show(agentCtx, overview.Project.ID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.SequenceExportShowInput{JobID: started.Job.ID})
	if err != nil || invalid.Job.Artifact == nil ||
		invalid.Job.Artifact.State != domain.SequenceExportArtifactInvalid ||
		invalid.Recovery != application.MediaRecoveryRetryJob {
		t.Fatalf("invalid=%+v err=%v", invalid, err)
	}
	if _, err := os.Stat(exportRoot); !os.IsNotExist(err) {
		t.Fatalf("invalid export remained canonical: %v", err)
	}

	db, err := sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE media_artifacts SET state = 'evicted' WHERE id = ? AND kind = 'render-input'`,
		artifact.ID.String()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dataDir, "artifacts", "media", artifact.ID.String())); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	retry, err := exports.Retry(agentCtx, overview.Project.ID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.SequenceExportRetryInput{JobID: started.Job.ID})
	if err != nil || retry.Job.ID == started.Job.ID || retry.Job.RetryOfJobID == nil ||
		*retry.Job.RetryOfJobID != started.Job.ID {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	if err := scheduler.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("render-input rematerialization executed=%v err=%v", executed, err)
	}
	now = now.Add(time.Second)
	recompleted := executeExport(retry.Job.ID, now.Add(time.Second))
	if recompleted.Job.State != domain.MediaJobSucceeded || recompleted.Job.Artifact == nil ||
		recompleted.Job.Artifact.ID == completed.Job.Artifact.ID ||
		recompleted.Job.Artifact.Facts != completed.Job.Artifact.Facts {
		t.Fatalf("recompleted=%+v completed=%+v", recompleted, completed)
	}
	firstHistory, err := exports.ListForCreator(
		creatorCtx, overview.Project.ID, application.ListSequenceExportHistoryInput{Limit: 1},
	)
	if err != nil || len(firstHistory.Lineages) != 1 || firstHistory.NextAfter == "" {
		t.Fatalf("first export history=%+v err=%v", firstHistory, err)
	}
	secondHistory, err := exports.ListForCreator(
		creatorCtx, overview.Project.ID,
		application.ListSequenceExportHistoryInput{After: firstHistory.NextAfter, Limit: 1},
	)
	if err != nil || len(secondHistory.Lineages) != 1 || secondHistory.NextAfter != "" ||
		secondHistory.Lineages[0].Export.Job.RootJobID == firstHistory.Lineages[0].Export.Job.RootJobID {
		t.Fatalf("second export history=%+v err=%v", secondHistory, err)
	}
	history := append(firstHistory.Lineages, secondHistory.Lineages...)
	var agentLineage *application.SequenceExportLineage
	for index := range history {
		if history[index].Export.Job.RootJobID == started.Job.ID {
			agentLineage = &history[index]
		}
	}
	if agentLineage == nil || agentLineage.Origin != application.SequenceExportOriginAgent ||
		agentLineage.AttemptCount.Value() != 2 ||
		agentLineage.ArtifactAvailability != application.SequenceExportArtifactReady ||
		agentLineage.Export.Job.ID != retry.Job.ID {
		t.Fatalf("Agent export lineage=%+v", agentLineage)
	}
	historyOnly, err := application.NewSequenceExports(
		store, application.UUIDv7IdentityGenerator{}, clock, application.SequenceExportSettings{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if page, err := historyOnly.ListForCreator(
		creatorCtx, overview.Project.ID, application.ListSequenceExportHistoryInput{Limit: 50},
	); err != nil || len(page.Lineages) != 2 {
		t.Fatalf("history-only export control page=%+v err=%v", page, err)
	}
	if _, err := historyOnly.StartForCreator(
		creatorCtx, overview.Project.ID, overview.Project.MainSequenceID,
		application.SequenceExportStartInput{
			RequestID:        mustRequestID(t, "creator:unavailable-export-start"),
			SequenceRevision: committed.MainSequenceRevision, Preset: domain.SequenceExportProfileV1,
		},
	); !errors.Is(err, application.ErrSequenceExportUnavailable) {
		t.Fatalf("unavailable export start error=%v", err)
	}
	deliveryFile, deliveryMedia, err := store.OpenSequenceExportDelivery(
		ctx, overview.Project.ID, recompleted.Job.Artifact.ID, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	deliveryBytes, readErr := io.ReadAll(deliveryFile)
	closeErr := deliveryFile.Close()
	if readErr != nil || closeErr != nil || uint64(len(deliveryBytes)) != deliveryMedia.ByteSize.Value() {
		t.Fatalf("delivery bytes=%d media=%+v readErr=%v closeErr=%v",
			len(deliveryBytes), deliveryMedia, readErr, closeErr)
	}
	deleteInput := application.SequenceExportDeleteArtifactInput{
		RequestID:  mustRequestID(t, "creator:real-export-delete"),
		ArtifactID: recompleted.Job.Artifact.ID,
	}
	now = now.Add(time.Second)
	deleted, err := exports.DeleteArtifactForCreator(
		creatorCtx, overview.Project.ID, recompleted.Job.ID, deleteInput,
	)
	if err != nil || deleted.Job.Artifact == nil ||
		deleted.Job.Artifact.State != domain.SequenceExportArtifactDeleted ||
		deleted.Recovery != application.MediaRecoveryRetryJob {
		t.Fatalf("deleted export=%+v err=%v", deleted, err)
	}
	replayedDelete, err := exports.DeleteArtifactForCreator(
		creatorCtx, overview.Project.ID, recompleted.Job.ID, deleteInput,
	)
	if err != nil || !replayedDelete.Replayed || replayedDelete.Job.Artifact == nil ||
		replayedDelete.Job.Artifact.State != domain.SequenceExportArtifactDeleted {
		t.Fatalf("replayed delete=%+v err=%v", replayedDelete, err)
	}
	if _, _, err := store.OpenSequenceExportDelivery(
		ctx, overview.Project.ID, recompleted.Job.Artifact.ID, now,
	); !errors.Is(err, application.ErrSequenceExportNotFound) {
		t.Fatalf("deleted export remained deliverable: %v", err)
	}
	deletedRoot := filepath.Join(
		dataDir, "artifacts", "sequence-export", recompleted.Job.Artifact.ID.String(),
	)
	if _, err := os.Stat(deletedRoot); !os.IsNotExist(err) {
		t.Fatalf("deleted export remained canonical: %v", err)
	}
	postCommitStage := filepath.Join(
		dataDir, "work", "sequence-export-deletions", recompleted.Job.Artifact.ID.String(),
	)
	if err := os.MkdirAll(postCommitStage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(postCommitStage, "orphan"), []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileProductStorage(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(postCommitStage); !os.IsNotExist(err) {
		t.Fatalf("post-commit deletion staging remained: %v", err)
	}
	deletedHistory, err := exports.ListForCreator(
		creatorCtx, overview.Project.ID, application.ListSequenceExportHistoryInput{Limit: 50},
	)
	if err != nil {
		t.Fatal(err)
	}
	for index := range deletedHistory.Lineages {
		lineage := deletedHistory.Lineages[index]
		if lineage.Export.Job.RootJobID == started.Job.ID &&
			(lineage.AttemptCount.Value() != 2 ||
				lineage.ArtifactAvailability != application.SequenceExportArtifactDeleted) {
			t.Fatalf("deleted history lineage=%+v", lineage)
		}
	}
	now = now.Add(time.Second)
	postDeleteRetry, err := exports.RetryForCreator(
		creatorCtx, overview.Project.ID, application.SequenceExportRetryInput{JobID: recompleted.Job.ID},
	)
	if err != nil || postDeleteRetry.Job.RetryOfJobID == nil ||
		*postDeleteRetry.Job.RetryOfJobID != recompleted.Job.ID {
		t.Fatalf("post-delete retry=%+v err=%v", postDeleteRetry, err)
	}
	now = now.Add(time.Second)
	postDeleteCompleted := executeExport(postDeleteRetry.Job.ID, now.Add(time.Second))
	if postDeleteCompleted.Job.Artifact == nil ||
		postDeleteCompleted.Job.Artifact.ID == recompleted.Job.Artifact.ID {
		t.Fatalf("post-delete completion=%+v", postDeleteCompleted)
	}
	retryRoot := filepath.Join(
		dataDir, "artifacts", "sequence-export", postDeleteCompleted.Job.Artifact.ID.String(),
	)
	preCommitStage := filepath.Join(
		dataDir, "work", "sequence-export-deletions", postDeleteCompleted.Job.Artifact.ID.String(),
	)
	if err := os.Rename(retryRoot, preCommitStage); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileProductStorage(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(retryRoot, "export.webm")); err != nil {
		t.Fatalf("pre-commit deletion staging was not restored: %v", err)
	}
	if err := os.WriteFile(filepath.Join(retryRoot, "export.webm"), []byte("delivery-corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, _, err := store.OpenSequenceExportDelivery(
		ctx, overview.Project.ID, postDeleteCompleted.Job.Artifact.ID, now,
	); !errors.Is(err, application.ErrSequenceExportIntegrity) {
		t.Fatalf("corrupt delivery error=%v", err)
	}
	deliveryInvalid, err := exports.ShowForCreator(
		creatorCtx, overview.Project.ID, application.SequenceExportShowInput{JobID: postDeleteRetry.Job.ID},
	)
	if err != nil || deliveryInvalid.Job.Artifact == nil ||
		deliveryInvalid.Job.Artifact.State != domain.SequenceExportArtifactInvalid ||
		deliveryInvalid.Recovery != application.MediaRecoveryRetryJob {
		t.Fatalf("delivery invalid=%+v err=%v", deliveryInvalid, err)
	}
}

type recordingSequenceExportRenderer struct {
	inner application.SequenceExportRenderer
	err   error
}

func (renderer *recordingSequenceExportRenderer) Identity() application.RenderExecutorIdentity {
	return renderer.inner.Identity()
}

func (renderer *recordingSequenceExportRenderer) Render(
	ctx context.Context,
	request application.SequenceExportRenderRequest,
) (application.SequenceExportRenderExecution, error) {
	execution, err := renderer.inner.Render(ctx, request)
	renderer.err = err
	return execution, err
}

type recordingSequenceExportVerifier struct {
	inner application.SequenceExportArtifactVerifier
	err   error
}

func (verifier *recordingSequenceExportVerifier) Verify(
	ctx context.Context,
	request application.SequenceExportVerificationRequest,
) (domain.RenderedMediaFacts, error) {
	facts, err := verifier.inner.Verify(ctx, request)
	verifier.err = err
	return facts, err
}

func buildAdmittedRenderInputSource(t *testing.T, ffmpeg string) string {
	t.Helper()
	root := t.TempDir()
	fixture := filepath.Join(root, "fixture.avi")
	if err := mediatoolchain.WriteCanonicalConformanceFixture(fixture); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "admitted.mkv")
	err := lifecycle.Run(context.Background(), lifecycle.ProcessSpec{
		Executable: ffmpeg,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
			"-protocol_whitelist", "file,pipe,fd", "-i", fixture, "-map", "0:v:0",
			"-vf", "format=yuv420p,setsar=1,setparams=range=limited:color_primaries=bt709:color_trc=bt709:colorspace=bt709",
			"-c:v", "ffv1", "-level", "3", "-coder", "1", "-context", "1", "-g", "1",
			"-slicecrc", "1", "-threads", "1", "-pix_fmt", "yuv420p", "-flags:v", "+bitexact",
			"-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709", "-color_range", "tv",
			"-map_metadata", "-1", "-map_chapters", "-1", "-fflags", "+bitexact", "-f", "matroska", output,
		},
		Directory: root, Env: []string{"LANG=C", "LC_ALL=C", "TZ=UTC"},
		Stdout: io.Discard, Stderr: os.Stderr, Profile: lifecycle.ProfileHarness,
		Presentation: lifecycle.PresentationHeadless, ContainProcessTree: true,
		TerminationGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(output)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func executeRealRenderInput(
	t *testing.T,
	ctx context.Context,
	executor *service.ExternalMediaRenderInputExecutor,
	claim application.MediaJobClaim,
	attemptID domain.JobAttemptID,
) application.MediaRenderInputExecution {
	t.Helper()
	claim.AttemptID = attemptID
	execution, err := executor.Execute(ctx, claim)
	if err != nil || execution.RenderInput == nil {
		t.Fatalf("render-input execution=%+v err=%v", execution, err)
	}
	return *execution.RenderInput
}

func readPreparedMedia(t *testing.T, workspace application.PreparedMediaWorkspace, path string) []byte {
	t.Helper()
	file, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustRenderInputAttemptID(t *testing.T, at time.Time) domain.JobAttemptID {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.ParseJobAttemptID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRenderInputWorkJobID(t *testing.T, at time.Time) domain.WorkJobID {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.ParseWorkJobID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
