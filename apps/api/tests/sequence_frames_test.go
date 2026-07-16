package tests

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSQLiteSequenceFramesPublishLeaseEvictAndExplicitlyRetry(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, projectReads, _, runs := testProjectApplications(t, store)
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: mustRequestID(t, "gesture:sequence-frame-project"), Name: "Sequence frame inspection",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	clock := application.ClockFunc(func() time.Time { return now })
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	size, _ := domain.NewUInt64(4096)
	observation := domain.SourceObservation{
		ByteSize: size, ModifiedUnixNs: domain.NewInt64(1234), FileIdentity: "fixture:sequence-frames",
	}
	grant, err := media.RegisterSourceGrant(creatorContext(t), application.RegisterSourceGrantInput{
		RequestID: mustRequestID(t, "picker:sequence-frame-source"), Platform: "mac",
		Kind: domain.SourceGrantLocalPath, DisplayName: "sequence-frame.mov", Observation: observation,
		ProtectedMaterial: []byte(`{"schema":"open-cut/source-grant-material/local-path/v1","path":"/fixture/sequence-frame.mov"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creatorContext(t), created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "gesture:sequence-frame-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	one, _ := domain.NewRationalTime(1, 1)
	videoTimeBase, _ := domain.NewRationalTime(1, 1000)
	probe := application.MediaProbe{
		Container: "matroska", Duration: &one,
		Streams: []domain.SourceStreamDescriptor{{
			Index: 0, MediaType: domain.MediaVideo, Codec: "vp9", TimeBase: videoTimeBase,
			Duration: &one, Dispositions: []string{"default"},
			Video: &domain.VideoStreamFacts{Width: 1920, Height: 1080, Rotation: 0},
		}},
	}
	mediaScheduler := newTestWorkScheduler(t, store, []application.MediaJobExecutor{
		fixedIdentifyExecutor{result: application.MediaIdentification{
			Fingerprint: testRenderDigest("a"), Observation: observation,
		}},
		fixedProbeExecutor{result: probe}, fixedSourceProxyExecutor{},
	}, clock, "api:sequence-frame-media")
	if err := mediaScheduler.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if executed, runErr := mediaScheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("media work %d executed=%v err=%v", index, executed, runErr)
		}
	}
	assetReads, _ := application.NewAssetReads(store)
	asset, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || asset.Facts == nil || len(asset.Facts.Streams) != 1 {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	videoStream := asset.Facts.Streams[0].ID
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
	agentCtx := createSQLiteAgentContext(t, store)
	run, err := runs.Begin(agentCtx, overview.Project.ID, application.RunBeginInput{
		RequestID: mustRequestID(t, "agent:sequence-frame-run"), Intent: "Inspect committed Sequence frames",
	})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := application.NewEdits(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	zero, _ := domain.NewRationalTime(0, 1)
	clipRange, _ := domain.NewTimeRange(zero, one)
	clipLocal, _ := domain.ParseLocalID("sequence_frame_clip")
	enabled := true
	proposal, err := edits.Propose(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:sequence-frame-propose"), Intent: "Add the inspected Clip",
			BaseProjectRevision: overview.Project.Revision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityTrack, ID: videoTrack.ID.String(), Revision: videoTrack.Revision},
				{Kind: domain.EntityAsset, ID: asset.ID.String(), Revision: asset.Revision},
			},
			Operations: []application.EditOperationInput{{
				Type: domain.EditAddClip, CreateAs: &clipLocal, TrackID: &videoTrack.ID,
				AssetID: &asset.ID, SourceStreamID: &videoStream,
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
			RequestID: mustRequestID(t, "agent:sequence-frame-apply"), ProposalDigest: proposal.Proposal.Digest,
		},
	); err != nil {
		t.Fatal(err)
	}
	committed, err := projectReads.Show(creatorContext(t), overview.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	previewVersion := application.SequencePreviewRendererV1 + "@sequence-frame-fixture"
	previews, err := application.NewSequencePreviews(
		store, application.UUIDv7IdentityGenerator{}, clock,
		application.SequencePreviewSettings{RendererVersion: previewVersion, RendererTarget: "mac-arm64"},
	)
	if err != nil {
		t.Fatal(err)
	}
	frameVersion := "sequence-frame-fixture-v1"
	frames, err := application.NewSequenceFrames(
		store, previews, application.UUIDv7IdentityGenerator{}, clock,
		application.SequenceFrameSettings{ExecutorVersion: frameVersion},
	)
	if err != nil {
		t.Fatal(err)
	}
	half, _ := domain.NewRationalTime(1, 2)
	prepared, err := frames.Execute(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.SequenceFramesInput{
			Operation:        application.SequenceFramesPrepare,
			SequenceRevision: &committed.MainSequenceRevision,
			Times:            []domain.RationalTime{zero, half},
		},
	)
	if err != nil || prepared.Status != application.SequenceFrameSetAccepted || len(prepared.Resources) != 0 {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	previewExecutor, err := application.NewSequencePreviewWorkExecutor(
		store, fixedSequencePreviewRenderer{version: previewVersion, target: "mac-arm64"},
		fixedSequencePreviewVerifier{}, application.UUIDv7IdentityGenerator{}, clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	frameExecutor := fixedSequenceFrameWorkExecutor{repository: store, version: frameVersion, clock: clock}
	workScheduler, err := application.NewWorkScheduler(
		store, []application.WorkJobExecutor{previewExecutor, frameExecutor},
		application.UUIDv7IdentityGenerator{}, clock,
		application.WorkSchedulerSettings{
			LeaseOwner: "api:sequence-frame-work", LeaseDuration: 30 * time.Second,
			PollInterval: 10 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if executed, runErr := workScheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("sequence work %d executed=%v err=%v", index, executed, runErr)
		}
	}
	continuation := application.SequenceFramesInput{Operation: application.SequenceFramesContinue, JobID: &prepared.Job.ID}
	ready, err := frames.Execute(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		continuation,
	)
	if err != nil || ready.Status != application.SequenceFrameSetReady || len(ready.Resources) != 2 ||
		ready.Job.ID != prepared.Job.ID || ready.ArtifactID == nil {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	firstResources := append([]application.SequenceFrameResourceLease(nil), ready.Resources...)
	for _, resource := range firstResources {
		if _, err := os.Stat(resource.ReadOnlyPath); err != nil ||
			!pathWithinTest(filepath.Join(dataDir, "scratch", "runs", run.Run.ID.String(), "turns", run.Run.CurrentTurn.ID.String()), resource.ReadOnlyPath) {
			t.Fatalf("resource=%+v err=%v", resource, err)
		}
	}
	replayed, err := frames.Execute(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		continuation,
	)
	if err != nil || len(replayed.Resources) != 2 ||
		replayed.Resources[0].ResourceID != firstResources[0].ResourceID ||
		replayed.Resources[1].ResourceID != firstResources[1].ResourceID {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	now = now.Add(6 * time.Minute)
	if err := store.ReconcileProductScratchLeases(ctx, now); err != nil {
		t.Fatal(err)
	}
	for _, resource := range firstResources {
		if _, err := os.Stat(resource.ReadOnlyPath); !os.IsNotExist(err) {
			t.Fatalf("expired resource survived: %s err=%v", resource.ReadOnlyPath, err)
		}
	}
	rematerialized, err := frames.Execute(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		continuation,
	)
	if err != nil || rematerialized.Resources[0].ResourceID == firstResources[0].ResourceID {
		t.Fatalf("rematerialized=%+v err=%v", rematerialized, err)
	}
	artifactRoot := filepath.Join(dataDir, "artifacts", "sequence-frames", ready.ArtifactID.String())
	if err := os.WriteFile(filepath.Join(artifactRoot, "frames", "000.png"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileSequenceFrameStorage(ctx, now); err != nil {
		t.Fatal(err)
	}
	failed, err := frames.Execute(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		continuation,
	)
	if err != nil || failed.Status != application.SequenceFrameSetFailed ||
		failed.Recovery != application.MediaRecoveryRetryJob || failed.Job.TerminalErrorCode == nil ||
		*failed.Job.TerminalErrorCode != "frame-artifact-unavailable" {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
	retry, err := frames.Execute(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.SequenceFramesInput{Operation: application.SequenceFramesRetry, JobID: &failed.Job.ID},
	)
	if err != nil || retry.Status != application.SequenceFrameSetAccepted || retry.Job.ID == failed.Job.ID {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	if executed, runErr := workScheduler.RunOne(ctx); runErr != nil || !executed {
		t.Fatalf("retry work executed=%v err=%v", executed, runErr)
	}
	recovered, err := frames.Execute(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		continuation,
	)
	if err != nil || recovered.Status != application.SequenceFrameSetReady ||
		recovered.Job.ID != retry.Job.ID || recovered.ArtifactID == nil || *recovered.ArtifactID == *ready.ArtifactID {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
}

type fixedSequenceFrameWorkExecutor struct {
	repository *repository.SQLiteProjects
	version    string
	clock      application.Clock
}

func (executor fixedSequenceFrameWorkExecutor) Registration() application.WorkExecutorRegistration {
	return application.WorkExecutorRegistration{Kind: domain.WorkJobSequenceFrames, Version: executor.version}
}

func (executor fixedSequenceFrameWorkExecutor) Execute(ctx context.Context, claim application.WorkJobClaim) error {
	frame := claim.SequenceFrames
	width, height, err := application.SequenceFrameOutputDimensions(
		frame.PreviewArtifact.Facts.CanvasWidth, frame.PreviewArtifact.Facts.CanvasHeight,
	)
	if err != nil {
		return err
	}
	manifest := application.SequenceFrameArtifactManifest{
		ProjectID: frame.ProjectID, SequenceID: frame.SequenceID, SequenceRevision: frame.SequenceRevision,
		PreviewJobID: frame.Parameters.PreviewJobID, PreviewArtifactID: frame.PreviewArtifact.ID,
		PreviewArtifactDigest: frame.PreviewArtifact.ContentDigest,
		RenderPlanDigest:      frame.PreviewArtifact.RenderPlanDigest,
		Profile:               frame.Parameters.Profile, GridPolicy: frame.Parameters.GridPolicy,
		Producer: executor.version,
		Samples:  make([]application.SequenceFrameArtifactSample, 0, len(frame.Parameters.Samples)),
	}
	pngs := make([][]byte, 0, len(frame.Parameters.Samples))
	for index, coordinate := range frame.Parameters.Samples {
		pixels := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
		for y := 0; y < int(height); y++ {
			for x := 0; x < int(width); x++ {
				pixels.SetRGBA(x, y, color.RGBA{R: uint8(40 + index), G: 80, B: 120, A: 255})
			}
		}
		buffer := new(bytes.Buffer)
		if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(buffer, pixels); err != nil {
			return err
		}
		data := buffer.Bytes()
		size, _ := domain.NewUInt64(uint64(len(data)))
		manifest.Samples = append(manifest.Samples, application.SequenceFrameArtifactSample{
			SequenceFrameCoordinate: coordinate, Width: width, Height: height,
			Path: framePath(index), ByteSize: size, SHA256: bytesDigest(data),
		})
		pngs = append(pngs, append([]byte(nil), data...))
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-frame-set-artifact", application.SequenceFrameArtifactSchema, manifest,
	)
	if err != nil {
		return err
	}
	total := uint64(len(canonical))
	for _, data := range pngs {
		total += uint64(len(data))
	}
	byteSize, _ := domain.NewUInt64(total)
	now := executor.clock.Now().UTC()
	artifactValue, _ := domain.GenerateUUIDv7(now)
	artifactID, _ := domain.ParseArtifactID(artifactValue)
	eventValue, _ := domain.GenerateUUIDv7(now)
	eventID, _ := domain.ParseActivityEventID(eventValue)
	return executor.repository.CompleteSequenceFrameSet(ctx, application.CompleteSequenceFrameSet{
		Claim: claim, ArtifactID: artifactID, Manifest: manifest, ManifestCanonical: canonical,
		ContentDigest: digest, PNGs: pngs, ByteSize: byteSize, EventID: eventID, CompletedAt: now,
	})
}

func framePath(index int) string {
	return "frames/00" + string(rune('0'+index)) + ".png"
}

func pathWithinTest(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) &&
		len(relative) > 0 && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
