package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSQLiteRenderPlanCompilesExactAVSnapshotAndReplaysAcrossRestart(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	projects, projectReads, _, runs := testProjectApplications(t, store)
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: mustRequestID(t, "gesture:render-plan-project"), Name: "Immutable preview plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 16, 0, 0, 0, time.UTC)
	clock := application.ClockFunc(func() time.Time { return now })
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	size, _ := domain.NewUInt64(4096)
	observation := domain.SourceObservation{
		ByteSize: size, ModifiedUnixNs: domain.NewInt64(1234), FileIdentity: "fixture:render-plan",
	}
	grant, err := media.RegisterSourceGrant(creatorContext(t), application.RegisterSourceGrantInput{
		RequestID: mustRequestID(t, "picker:render-plan"), Platform: "mac", Kind: domain.SourceGrantLocalPath,
		DisplayName: "render-plan.mov", Observation: observation,
		ProtectedMaterial: []byte(`{"schema":"open-cut/source-grant-material/local-path/v1","path":"/fixture/render-plan.mov"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creatorContext(t), created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "gesture:render-plan-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := testRenderDigest("a")
	one, _ := domain.NewRationalTime(1, 1)
	videoTimeBase, _ := domain.NewRationalTime(1, 1000)
	audioTimeBase, _ := domain.NewRationalTime(1, 48000)
	probe := application.MediaProbe{
		Container: "matroska", Duration: &one,
		Streams: []domain.SourceStreamDescriptor{
			{Index: 0, MediaType: domain.MediaVideo, Codec: "vp9", TimeBase: videoTimeBase,
				Duration: &one, Dispositions: []string{"default"},
				Video: &domain.VideoStreamFacts{Width: 1920, Height: 1080, Rotation: 0}},
			{Index: 1, MediaType: domain.MediaAudio, Codec: "opus", TimeBase: audioTimeBase,
				Duration: &one, Dispositions: []string{"default"},
				Audio: &domain.AudioStreamFacts{SampleRate: 48000, Channels: 2, ChannelLayout: "stereo"}},
			{Index: 2, MediaType: domain.MediaAudio, Codec: "opus", TimeBase: audioTimeBase,
				Duration: &one, Dispositions: []string{},
				Audio: &domain.AudioStreamFacts{SampleRate: 48000, Channels: 2, ChannelLayout: "stereo"}},
		},
	}
	scheduler := newTestWorkScheduler(t, store,
		[]application.MediaJobExecutor{
			fixedIdentifyExecutor{result: application.MediaIdentification{Fingerprint: fingerprint, Observation: observation}},
			fixedProbeExecutor{result: probe}, fixedSourceProxyExecutor{},
		}, clock, "api:render-plan-test")
	if err := scheduler.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("media execution %d executed=%v err=%v", index, executed, runErr)
		}
	}
	assetReads, _ := application.NewAssetReads(store)
	asset, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || asset.Facts == nil || len(asset.Facts.Streams) != 3 {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	var videoStream, audioStream domain.SourceStreamID
	for _, stream := range asset.Facts.Streams {
		if stream.Descriptor.MediaType == domain.MediaVideo {
			videoStream = stream.ID
		} else if stream.Descriptor.MediaType == domain.MediaAudio {
			audioStream = stream.ID
		}
	}
	overview, err := projectReads.Show(creatorContext(t), created.Project.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoTrack, audioTrack, captionTrack application.TrackSummary
	for _, track := range overview.Tracks {
		if track.Type == domain.TrackVideo {
			videoTrack = track
		} else if track.Type == domain.TrackAudio {
			audioTrack = track
		} else if track.Type == domain.TrackCaption {
			captionTrack = track
		}
	}
	agentCtx := createSQLiteAgentContext(t, store)
	run, err := runs.Begin(agentCtx, overview.Project.ID, application.RunBeginInput{
		RequestID: mustRequestID(t, "agent:render-plan-run"), Intent: "Compile one immutable linked A/V preview",
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
	videoLocal, _ := domain.ParseLocalID("preview_video")
	audioLocal, _ := domain.ParseLocalID("preview_audio")
	groupLocal, _ := domain.ParseLocalID("preview_av")
	enabled := true
	proposal, err := edits.Propose(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "agent:render-plan-propose"), Intent: "Add linked A/V",
			BaseProjectRevision: overview.Project.Revision,
			Preconditions: []domain.EntityPrecondition{
				{Kind: domain.EntityTrack, ID: videoTrack.ID.String(), Revision: videoTrack.Revision},
				{Kind: domain.EntityTrack, ID: audioTrack.ID.String(), Revision: audioTrack.Revision},
				{Kind: domain.EntityAsset, ID: asset.ID.String(), Revision: asset.Revision},
			},
			Operations: []application.EditOperationInput{
				{Type: domain.EditAddClip, CreateAs: &videoLocal, CreateLinkGroupAs: &groupLocal,
					TrackID: &videoTrack.ID, AssetID: &asset.ID, SourceStreamID: &videoStream,
					SourceRange: &clipRange, TimelineRange: &clipRange, Enabled: &enabled},
				{Type: domain.EditAddClip, CreateAs: &audioLocal, TrackID: &audioTrack.ID,
					AssetID: &asset.ID, SourceStreamID: &audioStream,
					SourceRange: &clipRange, TimelineRange: &clipRange, Enabled: &enabled,
					LinkGroup: &application.EditReference{Local: &groupLocal}},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := edits.Apply(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		proposal.Proposal.ID, application.EditApplyInput{
			RequestID: mustRequestID(t, "agent:render-plan-apply"), ProposalDigest: proposal.Proposal.Digest,
		},
	); err != nil {
		t.Fatal(err)
	}
	committed, err := projectReads.Show(creatorContext(t), overview.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	previews, err := application.NewSequencePreviews(
		store, application.UUIDv7IdentityGenerator{}, clock,
		application.SequencePreviewSettings{
			RendererVersion: application.SequencePreviewRendererV1 + "@fixture",
			RendererTarget:  "mac-arm64",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := previews.Prepare(
		creatorContext(t), overview.Project.ID, overview.Project.MainSequenceID, committed.MainSequenceRevision,
	)
	if err != nil || prepared.Status != application.SequencePreviewPreparing || prepared.Job == nil ||
		prepared.Job.State != domain.MediaJobBlocked {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	db, err := sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var inputCount, producerCount, prerequisiteCount int
	if err := db.QueryRow(`
SELECT COUNT(*), COUNT(DISTINCT producer_job_id)
FROM sequence_preview_job_inputs WHERE job_id = ?`, prepared.Job.ID.String()).Scan(
		&inputCount, &producerCount,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.QueryRow(`
SELECT COUNT(*) FROM work_job_prerequisites WHERE job_id = ?`, prepared.Job.ID.String()).Scan(
		&prerequisiteCount,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if inputCount != 2 || producerCount != 2 || prerequisiteCount != 3 {
		t.Fatalf("inputs=%d producers=%d prerequisites=%d", inputCount, producerCount, prerequisiteCount)
	}
	if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
		t.Fatalf("explicit alternate-stream proxy executed=%v err=%v", executed, runErr)
	}
	previewVersion := application.SequencePreviewRendererV1 + "@fixture"
	wrongRegistrations := []application.WorkExecutorRegistration{{
		Kind: domain.WorkJobSequencePreview, Version: previewVersion + "-wrong",
	}}
	if err := store.RecoverWorkJobs(ctx, wrongRegistrations, nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWorkJob(ctx, application.ClaimWorkJobInput{
		AttemptID: mustJobAttemptID(t, now.Add(time.Second)), Executors: wrongRegistrations,
		LeaseOwner: "api:sequence-preview-test", Now: now, LeaseDuration: 30 * time.Second,
	}); !errors.Is(err, application.ErrNoWork) {
		t.Fatalf("mismatched renderer claim err=%v", err)
	}
	registrations := []application.WorkExecutorRegistration{{
		Kind: domain.WorkJobSequencePreview, Version: previewVersion,
	}}
	if err := store.RecoverWorkJobs(ctx, registrations, nil, now); err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimWorkJob(ctx, application.ClaimWorkJobInput{
		AttemptID: mustJobAttemptID(t, now.Add(2*time.Second)), Executors: registrations,
		LeaseOwner: "api:sequence-preview-test", Now: now, LeaseDuration: 30 * time.Second,
	})
	if err != nil || claim.JobID != prepared.Job.ID || claim.Kind != domain.WorkJobSequencePreview ||
		claim.ExecutorVersion != previewVersion || claim.SequencePreview == nil || claim.Media != nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	if claim.SequencePreview.ProjectID != overview.Project.ID ||
		claim.SequencePreview.SequenceID != overview.Project.MainSequenceID ||
		claim.SequencePreview.SequenceRevision != committed.MainSequenceRevision ||
		claim.SequencePreview.Parameters.RendererVersion != previewVersion ||
		len(claim.SequencePreview.Parameters.Inputs) != 2 {
		t.Fatalf("sequence claim=%+v", claim.SequencePreview)
	}
	captionLocal, _ := domain.ParseLocalID("preview_after_prepare")
	captionText := "This newer caption must not enter the already prepared preview."
	captionLanguage := domain.CaptionLanguage("en")
	newerProposal, err := edits.Propose(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID:           mustRequestID(t, "agent:render-plan-newer-propose"),
			Intent:              "Advance the Sequence after preview preparation",
			BaseProjectRevision: committed.Project.Revision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityTrack, ID: captionTrack.ID.String(), Revision: captionTrack.Revision,
			}},
			Operations: []application.EditOperationInput{{
				Type: domain.EditAddCaption, CreateAs: &captionLocal,
				TrackID: &captionTrack.ID, Range: &clipRange, Language: &captionLanguage, Text: &captionText,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := edits.Apply(
		agentCtx, overview.Project.ID, overview.Project.MainSequenceID, run.Run.ID, run.Run.CurrentTurn.ID,
		newerProposal.Proposal.ID, application.EditApplyInput{
			RequestID:      mustRequestID(t, "agent:render-plan-newer-apply"),
			ProposalDigest: newerProposal.Proposal.Digest,
		},
	); err != nil {
		t.Fatal(err)
	}
	newerHead, err := projectReads.Show(creatorContext(t), overview.Project.ID)
	if err != nil || newerHead.MainSequenceRevision == committed.MainSequenceRevision {
		t.Fatalf("newer head=%+v err=%v", newerHead, err)
	}
	attemptPlans, err := application.NewSequencePreviewAttemptPlans(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	first, err := attemptPlans.Bind(ctx, claim)
	if err != nil || first.Replayed || len(first.Plan.Payload.Inputs) != 2 ||
		len(first.Plan.Payload.Video) != 1 || len(first.Plan.Payload.Audio) != 1 ||
		len(first.Plan.Payload.Captions) != 0 ||
		first.Plan.Payload.SequenceRevision != committed.MainSequenceRevision {
		t.Fatalf("first bound plan=%+v err=%v", first, err)
	}
	rebound, err := attemptPlans.Bind(ctx, claim)
	if err != nil || !rebound.Replayed || rebound.Plan.Digest != first.Plan.Digest ||
		rebound.CreatedAt != first.CreatedAt {
		t.Fatalf("rebound plan=%+v err=%v", rebound, err)
	}
	db, err = sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var boundDigest string
	if err := db.QueryRow(`
SELECT render_plan_digest FROM sequence_preview_job_details WHERE job_id = ?`, claim.JobID.String()).Scan(
		&boundDigest,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if boundDigest != first.Plan.Digest.String() {
		t.Fatalf("bound digest=%s plan=%s", boundDigest, first.Plan.Digest)
	}
	materialRoots, err := store.ResolveSequencePreviewArtifactRoots(
		ctx, claim, first.Plan.Digest, first.Plan.Payload.Inputs, now,
	)
	if err != nil || len(materialRoots) != len(first.Plan.Payload.Inputs) {
		t.Fatalf("material roots=%+v err=%v", materialRoots, err)
	}
	for _, input := range first.Plan.Payload.Inputs {
		root := materialRoots[input.ArtifactID.String()]
		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			t.Fatalf("material root %s is invalid: %v", root, statErr)
		}
	}
	db, err = sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var liveMaterialLeases int
	if err := db.QueryRow(`SELECT COUNT(*) FROM render_material_leases WHERE attempt_id = ?`,
		claim.AttemptID.String()).Scan(&liveMaterialLeases); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if liveMaterialLeases != len(first.Plan.Payload.Inputs) {
		t.Fatalf("live material leases=%d inputs=%d", liveMaterialLeases, len(first.Plan.Payload.Inputs))
	}
	mutatedInputs := append([]domain.RenderPlanInput(nil), first.Plan.Payload.Inputs...)
	mutatedInputs[0].ArtifactDigest = testRenderDigest("f")
	if _, err := store.ResolveSequencePreviewArtifactRoots(
		ctx, claim, first.Plan.Digest, mutatedInputs, now,
	); !errors.Is(err, application.ErrRenderPlanInvalid) {
		t.Fatalf("mutated render input resolved: %v", err)
	}
	previewExecutor, err := application.NewSequencePreviewWorkExecutor(
		store,
		fixedSequencePreviewRenderer{version: previewVersion, target: "mac-arm64"},
		fixedSequencePreviewVerifier{},
		application.UUIDv7IdentityGenerator{}, clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := previewExecutor.Execute(ctx, claim); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var previewState, artifactValue, artifactState, byteReference string
	if err := db.QueryRow(`
SELECT job.state, detail.result_artifact_id, artifact.state, artifact.byte_reference
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
JOIN sequence_preview_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE job.id = ?`, claim.JobID.String()).Scan(
		&previewState, &artifactValue, &artifactState, &byteReference,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM render_material_leases WHERE attempt_id = ?`,
		claim.AttemptID.String()).Scan(&liveMaterialLeases); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if previewState != "succeeded" || artifactState != "ready" ||
		byteReference != "artifact:sequence-preview/"+artifactValue {
		t.Fatalf("preview state=%s artifact=%s/%s reference=%s", previewState, artifactValue, artifactState, byteReference)
	}
	if liveMaterialLeases != 0 {
		t.Fatalf("terminal preview retained %d material leases", liveMaterialLeases)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(
		dataDir, "artifacts", "sequence-preview", artifactValue, "manifest.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := application.DecodeSequencePreviewArtifactManifest(manifestBytes)
	if err != nil || manifest.RenderPlanDigest != first.Plan.Digest ||
		manifest.RendererVersion != previewVersion || manifest.Media.Path != "preview.webm" {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
	duplicateValue, err := domain.GenerateUUIDv7(now.Add(4 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	duplicateJobID, err := domain.ParseWorkJobID(duplicateValue)
	if err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	at := now.Format(time.RFC3339Nano)
	if _, err := db.Exec(`
INSERT INTO work_jobs (
  id, scope_kind, project_id, installation_id, kind, state, pool, priority_class,
  logical_key, parameters_digest, parameters_json, producer_version,
  progress_basis_points, cancellation_requested, retry_of_job_id, created_at,
  updated_at, terminal_error_code
)
SELECT ?, scope_kind, project_id, installation_id, kind, 'blocked', pool, priority_class,
       logical_key || '/equivalent-lineage', parameters_digest, parameters_json,
       producer_version, 0, 0, NULL, ?, ?, NULL
FROM work_jobs WHERE id = ?`,
		duplicateJobID.String(), at, at, prepared.Job.ID.String(),
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO sequence_preview_job_details (
  job_id, sequence_id, sequence_revision, resolver_version, compiler_version,
  renderer_version, renderer_target, output_profile, render_intent_schema,
  render_intent_digest, render_intent_json, render_plan_digest
)
SELECT ?, sequence_id, sequence_revision, resolver_version, compiler_version,
       renderer_version, renderer_target, output_profile, render_intent_schema,
       render_intent_digest, render_intent_json, render_plan_digest
FROM sequence_preview_job_details WHERE job_id = ?`,
		duplicateJobID.String(), prepared.Job.ID.String(),
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO sequence_preview_job_inputs
		 SELECT ?, ordinal, clip_id, source_stream_id, producer_job_id
		 FROM sequence_preview_job_inputs WHERE job_id = ?`,
		`INSERT INTO sequence_preview_job_resources
		 SELECT ?, ordinal, resource_kind, resource_id, resource_version, resource_digest
		 FROM sequence_preview_job_resources WHERE job_id = ?`,
		`INSERT INTO work_job_prerequisites
		 SELECT ?, kind, reference_kind, reference_id, created_at
		 FROM work_job_prerequisites WHERE job_id = ?`,
		`INSERT INTO work_job_owners
		 SELECT ?, owner_kind, owner_id, created_at
		 FROM work_job_owners WHERE job_id = ?`,
	} {
		if _, err := db.Exec(statement, duplicateJobID.String(), prepared.Job.ID.String()); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverWorkJobs(ctx, registrations, nil, now); err != nil {
		t.Fatal(err)
	}
	duplicateClaim, err := store.ClaimWorkJob(ctx, application.ClaimWorkJobInput{
		AttemptID: mustJobAttemptID(t, now.Add(5*time.Second)), Executors: registrations,
		LeaseOwner: "api:sequence-preview-dedup-test", Now: now, LeaseDuration: 30 * time.Second,
	})
	if err != nil || duplicateClaim.JobID != duplicateJobID {
		t.Fatalf("duplicate claim=%+v err=%v", duplicateClaim, err)
	}
	if err := previewExecutor.Execute(ctx, duplicateClaim); err != nil {
		t.Fatalf("equivalent preview publication did not reuse its ready artifact: %v", err)
	}
	db, err = sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var duplicateState, duplicateArtifact string
	var equivalentArtifactCount int
	if err := db.QueryRow(`
SELECT job.state, detail.result_artifact_id
FROM work_jobs job
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE job.id = ?`, duplicateJobID.String()).Scan(&duplicateState, &duplicateArtifact); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.QueryRow(`
SELECT COUNT(*) FROM sequence_preview_artifacts
WHERE render_plan_digest = ? AND renderer_version = ? AND renderer_target = ?
  AND output_profile = ?`,
		first.Plan.Digest.String(), previewVersion, "mac-arm64", domain.SequencePreviewProfileV1,
	).Scan(&equivalentArtifactCount); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if duplicateState != "succeeded" || duplicateArtifact != artifactValue || equivalentArtifactCount != 1 {
		t.Fatalf(
			"equivalent preview state=%s artifact=%s want=%s artifacts=%d",
			duplicateState, duplicateArtifact, artifactValue, equivalentArtifactCount,
		)
	}
	failingVersion := application.SequencePreviewRendererV1 + "@failing-fixture"
	failingPreviews, err := application.NewSequencePreviews(
		store, application.UUIDv7IdentityGenerator{}, clock,
		application.SequencePreviewSettings{
			RendererVersion: failingVersion, RendererTarget: "mac-arm64",
			FontResource: &domain.RenderFontResource{
				ResourceID: "font:fixture-caption", Version: "fixture-v1", SHA256: testRenderDigest("c"),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	failingPrepared, err := failingPreviews.Prepare(
		creatorContext(t), overview.Project.ID, overview.Project.MainSequenceID, newerHead.MainSequenceRevision,
	)
	if err != nil || failingPrepared.Job == nil || failingPrepared.Status != application.SequencePreviewPreparing {
		t.Fatalf("failing prepared=%+v err=%v", failingPrepared, err)
	}
	failingRegistrations := []application.WorkExecutorRegistration{{
		Kind: domain.WorkJobSequencePreview, Version: failingVersion,
	}}
	if err := store.RecoverWorkJobs(ctx, failingRegistrations, nil, now); err != nil {
		t.Fatal(err)
	}
	failingClaim, err := store.ClaimWorkJob(ctx, application.ClaimWorkJobInput{
		AttemptID: mustJobAttemptID(t, now.Add(3*time.Second)), Executors: failingRegistrations,
		LeaseOwner: "api:sequence-preview-failure-test", Now: now, LeaseDuration: 30 * time.Second,
	})
	if err != nil || failingClaim.JobID != failingPrepared.Job.ID {
		t.Fatalf("failing claim=%+v err=%v", failingClaim, err)
	}
	failingExecutor, err := application.NewSequencePreviewWorkExecutor(
		store,
		fixedSequencePreviewRenderer{
			version: failingVersion, target: "mac-arm64",
			err: application.NewSequencePreviewExecutionError(
				"renderer-failed", errors.New("fixture render failure"),
			),
		},
		fixedSequencePreviewVerifier{}, application.UUIDv7IdentityGenerator{}, clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := failingExecutor.Execute(ctx, failingClaim); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var failedState, failureCode string
	if err := db.QueryRow(`
SELECT state, terminal_error_code FROM work_jobs WHERE id = ?`, failingClaim.JobID.String()).Scan(
		&failedState, &failureCode,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if failedState != "failed" || failureCode != "renderer-failed" {
		t.Fatalf("failed preview state=%s code=%s", failedState, failureCode)
	}
	failingPreparedAgain, err := failingPreviews.Prepare(
		creatorContext(t), overview.Project.ID, overview.Project.MainSequenceID, newerHead.MainSequenceRevision,
	)
	if err != nil || failingPreparedAgain.Status != application.SequencePreviewFailed ||
		failingPreparedAgain.Job == nil || failingPreparedAgain.Job.ID != failingClaim.JobID {
		t.Fatalf("terminal prepare did not converge: %+v err=%v", failingPreparedAgain, err)
	}
	retryPrepared, err := failingPreviews.Retry(
		creatorContext(t), overview.Project.ID, overview.Project.MainSequenceID,
		newerHead.MainSequenceRevision, failingClaim.JobID, failingPreparedAgain.Job.RenderPlanDigest,
	)
	if err != nil || retryPrepared.Status != application.SequencePreviewPreparing || retryPrepared.Job == nil ||
		retryPrepared.Job.ID == failingClaim.JobID || retryPrepared.Job.RenderPlanDigest == nil ||
		failingPreparedAgain.Job.RenderPlanDigest == nil ||
		*retryPrepared.Job.RenderPlanDigest != *failingPreparedAgain.Job.RenderPlanDigest {
		t.Fatalf("explicit retry=%+v err=%v", retryPrepared, err)
	}
	replayedRetry, err := failingPreviews.Retry(
		creatorContext(t), overview.Project.ID, overview.Project.MainSequenceID,
		newerHead.MainSequenceRevision, failingClaim.JobID, failingPreparedAgain.Job.RenderPlanDigest,
	)
	if err != nil || replayedRetry.Job == nil || replayedRetry.Job.ID != retryPrepared.Job.ID {
		t.Fatalf("retry replay=%+v err=%v", replayedRetry, err)
	}
	db, err = sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var retryOf string
	if err := db.QueryRow(`SELECT retry_of_job_id FROM work_jobs WHERE id = ?`,
		retryPrepared.Job.ID.String()).Scan(&retryOf); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if retryOf != failingClaim.JobID.String() {
		t.Fatalf("retry predecessor=%s", retryOf)
	}
	orphanValue, err := domain.GenerateUUIDv7(now.Add(4 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	orphanID, err := domain.ParseArtifactID(orphanValue)
	if err != nil {
		t.Fatal(err)
	}
	orphanRoot := filepath.Join(dataDir, "artifacts", "sequence-preview", orphanID.String())
	if err := os.MkdirAll(orphanRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	orphanAttemptID := mustJobAttemptID(t, now.Add(5*time.Second))
	orphanAttemptRoot := filepath.Join(dataDir, "work", "sequence-preview-attempts", orphanAttemptID.String())
	if err := os.MkdirAll(orphanAttemptRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileProductStorage(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphanRoot); !os.IsNotExist(err) {
		t.Fatalf("sequence preview orphan survived reconciliation: %v", err)
	}
	if _, err := os.Stat(orphanAttemptRoot); !os.IsNotExist(err) {
		t.Fatalf("sequence preview attempt survived reconciliation: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedPreviews, err := application.NewSequencePreviews(
		reopened, application.UUIDv7IdentityGenerator{}, clock,
		application.SequencePreviewSettings{
			RendererVersion: application.SequencePreviewRendererV1 + "@fixture",
			RendererTarget:  "mac-arm64",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	reprepared, err := reopenedPreviews.Continue(
		creatorContext(t), overview.Project.ID, overview.Project.MainSequenceID,
		committed.MainSequenceRevision, prepared.Job.ID, &first.Plan.Digest,
	)
	if err != nil || reprepared.Job == nil || reprepared.Job.ID != prepared.Job.ID ||
		reprepared.Status != application.SequencePreviewReady || reprepared.Job.Artifact == nil ||
		reprepared.Job.Artifact.ID.String() != artifactValue {
		t.Fatalf("reprepared=%+v err=%v", reprepared, err)
	}
}

type fixedSequencePreviewRenderer struct {
	version string
	target  string
	err     error
}

func (renderer fixedSequencePreviewRenderer) Identity() application.SequencePreviewRendererIdentity {
	return application.SequencePreviewRendererIdentity{Version: renderer.version, Target: renderer.target}
}

func (renderer fixedSequencePreviewRenderer) Render(
	_ context.Context,
	_ application.SequencePreviewRenderRequest,
) (application.SequencePreviewRenderExecution, error) {
	if renderer.err != nil {
		return application.SequencePreviewRenderExecution{}, renderer.err
	}
	media := []byte("fixture-sequence-preview-webm")
	size, _ := domain.NewUInt64(uint64(len(media)))
	return application.SequencePreviewRenderExecution{
		Media: application.SequencePreviewArtifactFile{
			Path: "preview.webm", MimeType: "video/webm", ByteSize: size, SHA256: bytesDigest(media),
		},
		Workspace: memoryProxyWorkspace{"preview.webm": media},
	}, nil
}

type fixedSequencePreviewVerifier struct{}

func (fixedSequencePreviewVerifier) Verify(
	_ context.Context,
	request application.SequencePreviewVerificationRequest,
) (domain.SequencePreviewMediaFacts, error) {
	return application.SequencePreviewFactsForPlan(request.Plan.Plan.Payload)
}

type fixedSourceProxyExecutor struct{}

func (fixedSourceProxyExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{Kind: domain.MediaJobProxy, Version: "source-proxy-fixture-v1"}
}

func (fixedSourceProxyExecutor) Execute(
	_ context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	parameters, err := application.DecodeInitialMediaJobParameters(claim.ParametersJSON)
	if err != nil || parameters.ProxySelection == nil {
		return application.MediaJobExecution{}, application.ErrMediaSourceRead
	}
	video, audio, err := application.SelectSourceProxyStreams(claim.SourceStreams, *parameters.ProxySelection)
	if err != nil || (video == nil && audio == nil) {
		return application.MediaJobExecution{}, application.ErrMediaSourceRead
	}
	zero, _ := domain.NewRationalTime(0, 1)
	videoTimeBase, _ := domain.NewRationalTime(1, 1000)
	audioTimeBase, _ := domain.NewRationalTime(1, 48000)
	mediaBytes := []byte("fixture-seekable-webm")
	mapBytes, _ := application.EncodeSourceProxyTimeMap([]int64{0}, []int64{0})
	mediaSize, _ := domain.NewUInt64(uint64(len(mediaBytes)))
	mapSize, _ := domain.NewUInt64(uint64(len(mapBytes)))
	frameCount, _ := domain.NewUInt64(1)
	audioSampleCount, _ := domain.NewUInt64(240_000)
	execution := application.MediaProxyExecution{
		SourceEpoch: zero,
		Media: application.SourceProxyArtifactFile{
			Path: "proxy.webm", MimeType: proxyFixtureMIME(video != nil), ByteSize: mediaSize, SHA256: bytesDigest(mediaBytes),
		},
		Workspace: memoryProxyWorkspace{"proxy.webm": mediaBytes},
	}
	if video != nil {
		execution.Video = &application.SourceProxyVideoTrack{
			Source: *video, SourceStartTime: zero, ProxyStartTime: zero, TimeBase: videoTimeBase,
			Codec: "vp9", Width: 1920, Height: 1080, PixelFormat: "yuv420p",
			ColorRange: "tv", ColorSpace: "bt709", ColorTransfer: "bt709", ColorPrimaries: "bt709",
			ColorInterpretation: "assumed-bt709", FrameCount: frameCount,
			TimeMap: application.SourceProxyArtifactFile{
				Path: "video-time-map.bin", MimeType: "application/vnd.open-cut.pts-map",
				ByteSize: mapSize, SHA256: bytesDigest(mapBytes),
			},
		}
		execution.Workspace.(memoryProxyWorkspace)["video-time-map.bin"] = mapBytes
	}
	if audio != nil {
		execution.Audio = &application.SourceProxyAudioTrack{
			Source: *audio, SourceStartTime: zero, ProxyStartTime: zero, TimeBase: audioTimeBase,
			Codec: "opus", SampleRate: 48000, Channels: 2, ChannelLayout: "stereo",
			ChannelProjection: "stereo-pass-v1", DecodedSampleCount: audioSampleCount,
		}
	}
	return application.MediaJobExecution{Proxy: &execution}, nil
}

func proxyFixtureMIME(hasVideo bool) string {
	if hasVideo {
		return "video/webm"
	}
	return "audio/webm"
}

type memoryProxyWorkspace map[string][]byte

func (workspace memoryProxyWorkspace) Open(relativePath string) (io.ReadCloser, error) {
	value, exists := workspace[relativePath]
	if !exists {
		return nil, application.ErrMediaSourceRead
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (memoryProxyWorkspace) Release() error { return nil }
func bytesDigest(value []byte) domain.Digest {
	digest := sha256.Sum256(value)
	return domain.Digest("sha256:" + hex.EncodeToString(digest[:]))
}

func testRenderDigest(character string) domain.Digest {
	return domain.Digest("sha256:" + strings.Repeat(character, 64))
}
