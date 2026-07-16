package tests

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestTranscriptWorkBindsExactInputsAndPublishesTypedArtifact(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resources := acquireTranscriptFixtureResource(t, store, dataDir)
	projects, _, _, runs := testProjectApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	creator := creatorContext(t)
	created, err := projects.Create(creator, application.CreateProjectInput{
		RequestID: mustRequestID(t, "ui:transcript-project"), Name: "Transcript project",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes := []byte("stable source bytes for transcript")
	sourcePath := filepath.Join(t.TempDir(), "source.mov")
	if err := os.WriteFile(sourcePath, sourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	grant, err := sourceAccess.RegisterSelection(creator, service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "ui:transcript-source"), Path: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creator, created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "ui:transcript-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	fingerprintBytes := sha256.Sum256(sourceBytes)
	fingerprint, err := domain.ParseDigest("sha256:" + hex.EncodeToString(fingerprintBytes[:]))
	if err != nil {
		t.Fatal(err)
	}
	timeBase := mustRational(t, 1, 48_000)
	duration := mustRational(t, 1, 1)
	transcriptExecutor := &fixedTranscriptExecutor{result: transcriptRecognitionFixture(t)}
	mediaExecutors := []application.MediaJobExecutor{
		fixedIdentifyExecutor{result: application.MediaIdentification{
			Fingerprint: fingerprint, Observation: grant.Grant.Observation,
		}},
		fixedProbeExecutor{version: "ffprobe-transcript-fixture-v1", result: application.MediaProbe{
			Container: "mov", Duration: &duration,
			Streams: []domain.SourceStreamDescriptor{{
				Index: 0, MediaType: domain.MediaAudio, Codec: "aac", TimeBase: timeBase,
				Duration: &duration, Dispositions: []string{"default"},
				Audio: &domain.AudioStreamFacts{
					SampleFormat: "fltp", SampleRate: 48_000, Channels: 2, ChannelLayout: "stereo",
				},
			}},
		}},
		transcriptExecutor,
	}
	workExecutors, err := application.NewMediaWorkExecutors(
		store, mediaExecutors, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := application.NewWorkScheduler(
		store, workExecutors, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
		application.WorkSchedulerSettings{
			LeaseOwner: "test-transcript-worker", LeaseDuration: 30 * time.Second,
			PollInterval: 10 * time.Millisecond, Resources: resources.RuntimeRegistrations(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for index, kind := range []domain.MediaJobKind{
		domain.MediaJobIdentify, domain.MediaJobProbe, domain.MediaJobTranscript,
	} {
		executed, runErr := scheduler.RunOne(ctx)
		if runErr != nil || !executed {
			t.Fatalf("step %d (%s) executed=%v err=%v", index, kind, executed, runErr)
		}
	}
	if transcriptExecutor.claim == nil || transcriptExecutor.claim.TranscriptBinding == nil ||
		transcriptExecutor.claim.TranscriptBinding.EngineTarget != transcriptFixtureTarget ||
		transcriptExecutor.claim.TranscriptBinding.EngineVersion != transcriptFixtureVersion ||
		transcriptExecutor.claim.TranscriptBinding.ModelName != application.TranscriptProfile ||
		transcriptExecutor.claim.SourceStream == nil || transcriptExecutor.claim.TranscriptNoAudio {
		t.Fatalf("transcript claim=%+v", transcriptExecutor.claim)
	}
	detail, _, err := assetReads.Inspect(creator, created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	var transcriptJob domain.MediaJobSummary
	for _, job := range detail.Jobs {
		if job.Kind == domain.MediaJobTranscript {
			transcriptJob = job
		}
	}
	if transcriptJob.State != domain.MediaJobSucceeded || transcriptJob.ResultArtifactID == nil {
		t.Fatalf("transcript job=%+v", transcriptJob)
	}
	foundArtifact := false
	for _, artifact := range detail.Artifacts {
		if artifact.ID == *transcriptJob.ResultArtifactID {
			foundArtifact = artifact.Kind == domain.ArtifactTranscript && artifact.State == domain.ArtifactReady
		}
	}
	if !foundArtifact {
		t.Fatalf("transcript artifact missing: %+v", detail.Artifacts)
	}
	read, err := media.ReadTranscript(creator, application.TranscriptReadQuery{
		ProjectID: created.Project.Project.ID, AssetID: registered.Asset.Asset.ID, Limit: 20,
	})
	if err != nil || read.Schema != application.TranscriptReadSchema ||
		read.Artifact.ID != *transcriptJob.ResultArtifactID || !read.Artifact.IsDefault ||
		len(read.Segments) != 1 || read.Segments[0].Text != "hello world" ||
		len(read.Segments[0].Tokens) != 2 || read.NextAfter != "" {
		t.Fatalf("transcript read=%+v err=%v", read, err)
	}
	agent := createSQLiteAgentContext(t, store)
	run, err := runs.Begin(agent, created.Project.Project.ID, application.RunBeginInput{
		RequestID: mustRequestID(t, "agent:transcript-writing-run"),
		Intent:    "Correct the transcript and cite it in the paper edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := application.NewEdits(
		store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	correctionLocal, _ := domain.ParseLocalID("opening_correction")
	excerptLocal, _ := domain.ParseLocalID("opening_excerpt")
	language := domain.CaptionLanguage("en")
	segment := read.Segments[0]
	correctionRange := segment.Tokens[0].SourceRange
	correctionText := "hi"
	proposal, err := edits.Propose(
		agent, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID:           mustRequestID(t, "agent:transcript-writing-propose"),
			Intent:              "Correct the greeting and cite the exact evidence",
			BaseProjectRevision: registered.Transaction.CommittedProjectRevision,
			Preconditions: []domain.EntityPrecondition{{
				Kind: domain.EntityNarrativeNode, ID: created.Project.NarrativeRootNodeID.String(), Revision: 1,
			}},
			Operations: []application.EditOperationInput{
				{
					Type: domain.EditAddTranscriptCorrection, CreateAs: &correctionLocal,
					AssetID: &registered.Asset.Asset.ID, TranscriptArtifactID: transcriptJob.ResultArtifactID,
					TranscriptSegmentIDs: []domain.TranscriptSegmentID{segment.ID},
					SourceRange:          &correctionRange, Language: &language, Text: &correctionText,
				},
				{
					Type: domain.EditInsertSourceExcerpt, CreateAs: &excerptLocal,
					ParentID: &created.Project.NarrativeRootNodeID, AssetID: &registered.Asset.Asset.ID,
					AcceptedFingerprint: &fingerprint, TranscriptArtifactID: transcriptJob.ResultArtifactID,
					TranscriptSegmentIDs: []domain.TranscriptSegmentID{segment.ID},
					SourceRange:          &segment.SourceRange, Language: &language,
					CorrectionRevisions: []application.TranscriptCorrectionReferenceInput{{
						Correction: application.EditReference{Local: &correctionLocal},
					}},
				},
			},
		},
	)
	if err != nil || len(proposal.Proposal.Operations) != 2 || len(proposal.Proposal.Allocation) != 2 {
		t.Fatalf("transcript writing proposal=%+v err=%v", proposal, err)
	}
	committed, err := edits.Apply(
		agent, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, proposal.Proposal.ID,
		application.EditApplyInput{
			RequestID:      mustRequestID(t, "agent:transcript-writing-apply"),
			ProposalDigest: proposal.Proposal.Digest,
		},
	)
	if err != nil || committed.Transaction.CommittedProjectRevision.Value() != 3 {
		t.Fatalf("transcript writing commit=%+v err=%v", committed, err)
	}
	editReads, _ := application.NewEditReads(store)
	narrative, err := editReads.NarrativeSubtree(
		agent, created.Project.Project.ID, created.Project.Project.NarrativeDocumentID,
		created.Project.NarrativeRootNodeID, "", 50,
	)
	if err != nil || len(narrative.Nodes) != 1 || narrative.Nodes[0].SourceExcerpt == nil ||
		narrative.Nodes[0].SourceExcerpt.EffectiveText != "hi world" ||
		len(narrative.Nodes[0].SourceExcerpt.Evidence.CorrectionRevisions) != 1 ||
		narrative.Nodes[0].EvidenceStatus != domain.SourceExcerptEvidenceExact {
		t.Fatalf("source excerpt=%+v err=%v", narrative, err)
	}
	correctionID := allocationID(t, proposal.Proposal, correctionLocal)
	correction, err := editReads.Entity(
		agent, created.Project.Project.ID, domain.EntityTranscriptCorrection, correctionID,
	)
	if err != nil || correction.TranscriptCorrection == nil ||
		correction.TranscriptCorrection.ReplacementText != "hi" || correction.TranscriptCorrection.Tombstoned {
		t.Fatalf("correction=%+v err=%v", correction, err)
	}
	correctedRead, err := media.ReadTranscript(creator, application.TranscriptReadQuery{
		ProjectID: created.Project.Project.ID, AssetID: registered.Asset.Asset.ID,
		ArtifactID: transcriptJob.ResultArtifactID, Limit: 20,
	})
	if err != nil || len(correctedRead.Corrections) != 1 ||
		correctedRead.Corrections[0].OriginalText != "hello" ||
		correctedRead.Corrections[0].EffectiveText != "hi" ||
		correctedRead.Corrections[0].ID.String() != correctionID {
		t.Fatalf("corrected transcript read=%+v err=%v", correctedRead, err)
	}
	undone, err := edits.Undo(
		agent, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID, committed.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "agent:transcript-writing-undo")},
	)
	if err != nil || undone.Transaction.CommittedProjectRevision.Value() != 4 {
		t.Fatalf("transcript writing undo=%+v err=%v", undone, err)
	}
	narrative, err = editReads.NarrativeSubtree(
		agent, created.Project.Project.ID, created.Project.Project.NarrativeDocumentID,
		created.Project.NarrativeRootNodeID, "", 50,
	)
	if err != nil || len(narrative.Nodes) != 0 {
		t.Fatalf("undone source excerpt=%+v err=%v", narrative, err)
	}
	creatorExcerptRevision := commitAndUndoCreatorSourceExcerpt(t, store, creator, edits, editReads,
		created.Project.Project.ID, created.Project.Project.MainSequenceID, created.Project.Project.NarrativeDocumentID,
		created.Project.NarrativeRootNodeID, registered.Asset.Asset.ID, fingerprint, *transcriptJob.ResultArtifactID,
		segment, language, undone.Transaction.CommittedProjectRevision, narrative.Parent.Revision)
	firstOverlap, _ := domain.ParseLocalID("overlap_one")
	secondOverlap, _ := domain.ParseLocalID("overlap_two")
	_, err = edits.Propose(
		agent, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		run.Run.ID, run.Run.CurrentTurn.ID,
		application.EditProposeInput{
			RequestID:           mustRequestID(t, "agent:transcript-overlap-propose"),
			Intent:              "Attempt overlapping corrections",
			BaseProjectRevision: creatorExcerptRevision,
			Operations: []application.EditOperationInput{
				{
					Type: domain.EditAddTranscriptCorrection, CreateAs: &firstOverlap,
					AssetID: &registered.Asset.Asset.ID, TranscriptArtifactID: transcriptJob.ResultArtifactID,
					TranscriptSegmentIDs: []domain.TranscriptSegmentID{segment.ID},
					SourceRange:          &correctionRange, Language: &language, Text: &correctionText,
				},
				{
					Type: domain.EditAddTranscriptCorrection, CreateAs: &secondOverlap,
					AssetID: &registered.Asset.Asset.ID, TranscriptArtifactID: transcriptJob.ResultArtifactID,
					TranscriptSegmentIDs: []domain.TranscriptSegmentID{segment.ID},
					SourceRange:          &correctionRange, Language: &language, Text: &correctionText,
				},
			},
		},
	)
	if !errors.Is(err, application.ErrEditInvalid) {
		t.Fatalf("overlapping correction err=%v", err)
	}
	assertTypedTranscriptRows(t, store.Path(), registered.Asset.Asset.ID, *transcriptJob.ResultArtifactID)
	secondArtifact := cloneTranscriptArtifactFixture(t, store.Path(), *transcriptJob.ResultArtifactID)
	selection, err := media.SelectTranscriptDefault(creator, created.Project.Project.ID, registered.Asset.Asset.ID,
		application.SelectTranscriptDefaultInput{
			ArtifactID: secondArtifact, ExpectedDefaultArtifactID: *transcriptJob.ResultArtifactID,
		})
	if err != nil || selection.ArtifactID != secondArtifact ||
		selection.PreviousArtifactID != *transcriptJob.ResultArtifactID || selection.Replayed {
		t.Fatalf("transcript selection=%+v err=%v", selection, err)
	}
	selected, err := media.ReadTranscript(creator, application.TranscriptReadQuery{
		ProjectID: created.Project.Project.ID, AssetID: registered.Asset.Asset.ID, Limit: 20,
	})
	if err != nil || selected.Artifact.ID != secondArtifact || !selected.Artifact.IsDefault {
		t.Fatalf("selected transcript=%+v err=%v", selected, err)
	}
	original, err := media.ReadTranscript(creator, application.TranscriptReadQuery{
		ProjectID: created.Project.Project.ID, AssetID: registered.Asset.Asset.ID,
		ArtifactID: transcriptJob.ResultArtifactID, Limit: 20,
	})
	if err != nil || original.Artifact.IsDefault {
		t.Fatalf("original transcript=%+v err=%v", original, err)
	}
	replayed, err := media.SelectTranscriptDefault(creator, created.Project.Project.ID, registered.Asset.Asset.ID,
		application.SelectTranscriptDefaultInput{
			ArtifactID: secondArtifact, ExpectedDefaultArtifactID: *transcriptJob.ResultArtifactID,
		})
	if err != nil || !replayed.Replayed {
		t.Fatalf("replayed transcript selection=%+v err=%v", replayed, err)
	}
	_, err = media.SelectTranscriptDefault(creator, created.Project.Project.ID, registered.Asset.Asset.ID,
		application.SelectTranscriptDefaultInput{
			ArtifactID: *transcriptJob.ResultArtifactID, ExpectedDefaultArtifactID: *transcriptJob.ResultArtifactID,
		})
	if !errors.Is(err, application.ErrTranscriptSelectionConflict) {
		t.Fatalf("stale transcript selection err=%v", err)
	}
	_, err = media.SelectTranscriptDefault(agentContextForResource(t), created.Project.Project.ID, registered.Asset.Asset.ID,
		application.SelectTranscriptDefaultInput{ArtifactID: secondArtifact, ExpectedDefaultArtifactID: secondArtifact})
	if !errors.Is(err, application.ErrAuthorityScopeDenied) {
		t.Fatalf("Agent selected a Creator transcript default: %v", err)
	}
}

func TestTranscriptWorkCompletesNoAudioWithoutModelBindingOrArtifact(t *testing.T) {
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, _, _, _ := testProjectApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	creator := creatorContext(t)
	created, err := projects.Create(creator, application.CreateProjectInput{
		RequestID: mustRequestID(t, "ui:no-audio-project"), Name: "No audio project",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes := []byte("video-only source fixture")
	sourcePath := filepath.Join(t.TempDir(), "video-only.mov")
	if err := os.WriteFile(sourcePath, sourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	grant, err := sourceAccess.RegisterSelection(creator, service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "ui:no-audio-source"), Path: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creator, created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "ui:no-audio-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	fingerprintBytes := sha256.Sum256(sourceBytes)
	fingerprint, _ := domain.ParseDigest("sha256:" + hex.EncodeToString(fingerprintBytes[:]))
	duration := mustRational(t, 1, 1)
	timeBase := mustRational(t, 1, 30)
	noAudio := &fixedNoAudioTranscriptExecutor{}
	workExecutors, err := application.NewMediaWorkExecutors(
		store, []application.MediaJobExecutor{
			fixedIdentifyExecutor{result: application.MediaIdentification{
				Fingerprint: fingerprint, Observation: grant.Grant.Observation,
			}},
			fixedProbeExecutor{version: "ffprobe-no-audio-fixture-v1", result: application.MediaProbe{
				Container: "mov", Duration: &duration,
				Streams: []domain.SourceStreamDescriptor{{
					Index: 0, MediaType: domain.MediaVideo, Codec: "h264", TimeBase: timeBase,
					Duration: &duration, Dispositions: []string{"default"},
					Video: &domain.VideoStreamFacts{Width: 1920, Height: 1080},
				}},
			}},
			noAudio,
		},
		application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := application.NewWorkScheduler(
		store, workExecutors, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
		application.WorkSchedulerSettings{
			LeaseOwner: "test-no-audio-worker", LeaseDuration: 30 * time.Second,
			PollInterval: 10 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
			t.Fatalf("step %d executed=%v err=%v", index, executed, err)
		}
	}
	if noAudio.claim == nil || !noAudio.claim.TranscriptNoAudio ||
		noAudio.claim.TranscriptBinding != nil || noAudio.claim.SourceStream != nil {
		t.Fatalf("no-audio claim=%+v", noAudio.claim)
	}
	detail, _, err := assetReads.Inspect(creator, created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range detail.Jobs {
		if job.Kind == domain.MediaJobTranscript &&
			(job.State != domain.MediaJobSucceeded || job.ResultArtifactID != nil) {
			t.Fatalf("no-audio transcript job=%+v", job)
		}
	}
	database, err := sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var resultCode string
	var bindingCount, transcriptArtifactCount int
	if err := database.QueryRow(`
SELECT detail.result_code
FROM media_job_details detail JOIN work_jobs job ON job.id = detail.job_id
WHERE detail.asset_id = ? AND job.kind = 'transcript'`, registered.Asset.Asset.ID.String()).Scan(&resultCode); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM transcript_job_bindings`).Scan(&bindingCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM transcript_artifacts`).Scan(&transcriptArtifactCount); err != nil {
		t.Fatal(err)
	}
	if resultCode != "no-audio" || bindingCount != 0 || transcriptArtifactCount != 0 {
		t.Fatalf("result=%s binding=%d artifact=%d", resultCode, bindingCount, transcriptArtifactCount)
	}
}

func TestTranscriptResourceCorruptionInvalidatesAndReblocksBoundWork(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resources := acquireTranscriptFixtureResource(t, store, dataDir)
	projects, _, _, _ := testProjectApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	creator := creatorContext(t)
	created, err := projects.Create(creator, application.CreateProjectInput{
		RequestID: mustRequestID(t, "ui:corrupt-transcript-project"), Name: "Corrupt transcript project",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes := []byte("stable source bytes for corrupt transcript")
	sourcePath := filepath.Join(t.TempDir(), "source.mov")
	if err := os.WriteFile(sourcePath, sourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	grant, err := sourceAccess.RegisterSelection(creator, service.PlatformSourceSelection{
		RequestID: mustRequestID(t, "ui:corrupt-transcript-source"), Path: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creator, created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "ui:corrupt-transcript-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	fingerprintBytes := sha256.Sum256(sourceBytes)
	fingerprint, _ := domain.ParseDigest("sha256:" + hex.EncodeToString(fingerprintBytes[:]))
	duration := mustRational(t, 1, 1)
	timeBase := mustRational(t, 1, 48_000)
	modelAccess, err := service.NewTranscriptResourceAccess(
		store, dataDir, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	transcriptExecutor := &resourceCheckingTranscriptExecutor{
		models: modelAccess, result: transcriptRecognitionFixture(t),
	}
	workExecutors, err := application.NewMediaWorkExecutors(
		store, []application.MediaJobExecutor{
			fixedIdentifyExecutor{result: application.MediaIdentification{
				Fingerprint: fingerprint, Observation: grant.Grant.Observation,
			}},
			fixedProbeExecutor{version: "ffprobe-corrupt-transcript-fixture-v1", result: application.MediaProbe{
				Container: "mov", Duration: &duration,
				Streams: []domain.SourceStreamDescriptor{{
					Index: 0, MediaType: domain.MediaAudio, Codec: "aac", TimeBase: timeBase,
					Duration: &duration, Dispositions: []string{"default"},
					Audio: &domain.AudioStreamFacts{
						SampleFormat: "fltp", SampleRate: 48_000, Channels: 2, ChannelLayout: "stereo",
					},
				}},
			}},
			transcriptExecutor,
		},
		application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := application.NewWorkScheduler(
		store, workExecutors, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
		application.WorkSchedulerSettings{
			LeaseOwner: "test-corrupt-transcript-worker", LeaseDuration: 30 * time.Second,
			PollInterval: 10 * time.Millisecond, Resources: resources.RuntimeRegistrations(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("pre-corruption step %d executed=%v err=%v", index, executed, runErr)
		}
	}
	database, err := sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var resourceID string
	if err := database.QueryRow(`SELECT id FROM product_resources WHERE state = 'ready'`).Scan(&resourceID); err != nil {
		t.Fatal(err)
	}
	contentPath := filepath.Join(dataDir, "resources", "product", resourceID, "content.bin")
	if err := os.WriteFile(contentPath, []byte("tampered transcript model"), 0o600); err != nil {
		t.Fatal(err)
	}
	if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
		t.Fatalf("corrupt transcript executed=%v err=%v", executed, runErr)
	}
	if transcriptExecutor.claim == nil || transcriptExecutor.claim.TranscriptBinding == nil {
		t.Fatalf("corrupt transcript claim=%+v", transcriptExecutor.claim)
	}
	var resourceState, jobState, attemptState, attemptDiagnostics string
	var prerequisiteCount, invalidationActivityCount, transcriptActivityCount int
	if err := database.QueryRow(`SELECT state FROM product_resources WHERE id = ?`, resourceID).Scan(&resourceState); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
SELECT job.state FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
WHERE detail.asset_id = ? AND job.kind = 'transcript'`, registered.Asset.Asset.ID.String()).Scan(&jobState); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
SELECT state, diagnostics_json FROM work_job_attempts
WHERE job_id = ? ORDER BY generation DESC LIMIT 1`, transcriptExecutor.claim.JobID.String()).Scan(
		&attemptState, &attemptDiagnostics,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
SELECT COUNT(*) FROM work_job_prerequisites
WHERE job_id = ? AND kind = 'model-required' AND reference_kind = 'resource' AND reference_id = ?`,
		transcriptExecutor.claim.JobID.String(), application.TranscriptProfile,
	).Scan(&prerequisiteCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM activity_outbox WHERE kind = 'resource.invalidated'`).Scan(
		&invalidationActivityCount,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM activity_outbox WHERE kind = 'media.transcript-reblocked'`).Scan(
		&transcriptActivityCount,
	); err != nil {
		t.Fatal(err)
	}
	if resourceState != "invalid" || jobState != "blocked" || attemptState != "abandoned" ||
		attemptDiagnostics != `{"code":"resource-integrity-invalid"}` || prerequisiteCount != 1 ||
		invalidationActivityCount != 1 || transcriptActivityCount != 1 {
		t.Fatalf(
			"resource=%s job=%s attempt=%s diagnostics=%s prerequisite=%d activities=%d/%d",
			resourceState, jobState, attemptState, attemptDiagnostics, prerequisiteCount,
			invalidationActivityCount, transcriptActivityCount,
		)
	}
	detail, _, err := assetReads.Inspect(creator, created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range detail.Jobs {
		if job.Kind == domain.MediaJobTranscript && job.State != domain.MediaJobBlocked {
			t.Fatalf("reblocked transcript projection=%+v", job)
		}
	}
}

const (
	transcriptFixtureVersion = "whisper.cpp-fixture-v1"
	transcriptFixtureTarget  = "test-x64"
)

type fixedTranscriptExecutor struct {
	result application.TranscriptRecognition
	claim  *application.MediaJobClaim
}

type fixedNoAudioTranscriptExecutor struct {
	claim *application.MediaJobClaim
}

type resourceCheckingTranscriptExecutor struct {
	models service.TranscriptModelAccess
	result application.TranscriptRecognition
	claim  *application.MediaJobClaim
}

func (executor *resourceCheckingTranscriptExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{
		Kind: domain.MediaJobTranscript, Version: transcriptFixtureVersion, Target: transcriptFixtureTarget,
	}
}

func (executor *resourceCheckingTranscriptExecutor) Execute(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	copy := claim
	executor.claim = &copy
	if _, err := executor.models.Resolve(ctx, claim); err != nil {
		return application.MediaJobExecution{}, err
	}
	return application.MediaJobExecution{Transcript: &executor.result}, nil
}

func (executor *fixedNoAudioTranscriptExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{
		Kind: domain.MediaJobTranscript, Version: transcriptFixtureVersion, Target: transcriptFixtureTarget,
	}
}

func (executor *fixedNoAudioTranscriptExecutor) Execute(
	_ context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	if !claim.TranscriptNoAudio || claim.TranscriptBinding != nil || claim.SourceStream != nil {
		return application.MediaJobExecution{}, errors.New("no-audio fixture received an audio claim")
	}
	copy := claim
	executor.claim = &copy
	return application.MediaJobExecution{TranscriptNoAudio: true}, nil
}

func (executor *fixedTranscriptExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{
		Kind: domain.MediaJobTranscript, Version: transcriptFixtureVersion, Target: transcriptFixtureTarget,
	}
}

func (executor *fixedTranscriptExecutor) Execute(
	_ context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	if claim.TranscriptBinding == nil || claim.SourceStream == nil || claim.TranscriptNoAudio ||
		claim.TranscriptBinding.SourceStreamID != claim.SourceStream.ID ||
		claim.TranscriptBinding.EngineVersion != transcriptFixtureVersion ||
		claim.TranscriptBinding.EngineTarget != transcriptFixtureTarget {
		return application.MediaJobExecution{}, errors.New("transcript fixture received an unbound claim")
	}
	copy := claim
	executor.claim = &copy
	return application.MediaJobExecution{Transcript: &executor.result}, nil
}

func transcriptRecognitionFixture(t *testing.T) application.TranscriptRecognition {
	t.Helper()
	pcm := sha256.Sum256([]byte("canonical normalized pcm fixture"))
	samples, _ := domain.NewUInt64(16_000)
	bytes, _ := domain.NewUInt64(32_000)
	confidence, languageConfidence := uint16(9_800), uint16(9_900)
	return application.TranscriptRecognition{
		DetectedLanguage: "en", LanguageConfidenceBasisPoints: &languageConfidence,
		Normalization: domain.TranscriptNormalizationProof{
			SourceStartTime: mustRational(t, 0, 1), SampleRate: 16_000, Channels: 1,
			SampleFormat: "s16le", SampleCount: samples, PCMByteSize: bytes,
			PCMDigest:     domain.Digest("sha256:" + hex.EncodeToString(pcm[:])),
			ChannelPolicy: "stereo-equal-v1", TimingPolicy: "audio-frame-pts-gap-fill-v1",
		},
		Segments: []application.TranscriptSegmentRecognition{{
			SourceRange: domain.TimeRange{Start: mustRational(t, 0, 1), Duration: mustRational(t, 1, 1)},
			Text:        "hello world",
			Tokens: []application.TranscriptTokenRecognition{
				{SourceRange: domain.TimeRange{Start: mustRational(t, 0, 1), Duration: mustRational(t, 1, 2)}, Text: "hello", ConfidenceBasisPoints: &confidence},
				{SourceRange: domain.TimeRange{Start: mustRational(t, 1, 2), Duration: mustRational(t, 1, 2)}, Text: " world", ConfidenceBasisPoints: &confidence},
			},
		}},
	}
}

func acquireTranscriptFixtureResource(
	t *testing.T,
	store *repository.SQLiteProjects,
	dataDir string,
) *application.ProductResources {
	t.Helper()
	content := []byte("qualified transcript model fixture")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", domain.NewInt64(int64(len(content))).String())
		_, _ = writer.Write(content)
	}))
	t.Cleanup(server.Close)
	entry := resourceCatalogEntry(t, server.URL+"/model.bin", content)
	resources, err := application.NewProductResources(
		store, []application.ProductResourceCatalogEntry{entry},
		application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resources.Acquire(creatorContext(t), entry.Name, application.AcquireProductResourceInput{
		RequestID: mustRequestID(t, "ui:transcript-resource"),
	}); err != nil {
		t.Fatal(err)
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
			LeaseOwner: "test-transcript-resource", LeaseDuration: 30 * time.Second,
			PollInterval: 10 * time.Millisecond, Resources: resources.RuntimeRegistrations(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if executed, err := scheduler.RunOne(context.Background()); err != nil || !executed {
		t.Fatalf("resource executed=%v err=%v", executed, err)
	}
	return resources
}

func assertTypedTranscriptRows(
	t *testing.T,
	databasePath string,
	assetID domain.AssetID,
	artifactID domain.ArtifactID,
) {
	t.Helper()
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var bindingCount, artifactCount, segmentCount, tokenCount, selectionCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM transcript_job_bindings`).Scan(&bindingCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM transcript_artifacts WHERE artifact_id = ?`, artifactID.String(),
	).Scan(&artifactCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM transcript_segments WHERE artifact_id = ?`, artifactID.String(),
	).Scan(&segmentCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
SELECT COUNT(*) FROM transcript_tokens token
JOIN transcript_segments segment ON segment.id = token.segment_id
WHERE segment.artifact_id = ?`, artifactID.String()).Scan(&tokenCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
SELECT COUNT(*) FROM asset_transcript_selection WHERE asset_id = ? AND artifact_id = ?`,
		assetID.String(), artifactID.String(),
	).Scan(&selectionCount); err != nil {
		t.Fatal(err)
	}
	if bindingCount != 1 || artifactCount != 1 || segmentCount != 1 || tokenCount != 2 || selectionCount != 1 {
		t.Fatalf(
			"typed transcript rows binding=%d artifact=%d segment=%d token=%d selection=%d",
			bindingCount, artifactCount, segmentCount, tokenCount, selectionCount,
		)
	}
}
