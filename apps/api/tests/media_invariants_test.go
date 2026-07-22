package tests

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestMediaProbePreservesStreamIdentityAndIsolatesNonSourceFailures(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	projects, _, _, _ := testProjectApplications(t, store)
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: mustRequestID(t, "gesture:media-invariants-project"), Name: "Media invariants",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	clock := application.ClockFunc(func() time.Time { return now })
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	size, _ := domain.NewUInt64(4096)
	observation := domain.SourceObservation{
		ByteSize: size, ModifiedUnixNs: domain.NewInt64(987654321), FileIdentity: "dev:20:inode:40",
	}
	grant, err := media.RegisterSourceGrant(creatorContext(t), application.RegisterSourceGrantInput{
		RequestID: mustRequestID(t, "picker:media-invariants"), Platform: "mac", Kind: domain.SourceGrantLocalPath,
		DisplayName: "invariants.mov", Observation: observation,
		ProtectedMaterial: []byte(`{"schema":"open-cut/source-grant-material/local-path-v1","path":"/private/invariants.mov"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creatorContext(t), created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "gesture:media-invariants-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, _ := domain.ParseDigest("sha256:" + strings.Repeat("a", 64))
	oneSecond, _ := domain.NewRationalTime(1, 1)
	timeBase, _ := domain.NewRationalTime(1, 48000)
	probe := application.MediaProbe{
		Container: "mov", ContainerAliases: []string{"mp4"}, Duration: &oneSecond,
		Streams: []domain.SourceStreamDescriptor{{
			Index: 0, MediaType: domain.MediaAudio, Codec: "aac", TimeBase: timeBase, Duration: &oneSecond,
			Dispositions: []string{"default"}, Audio: &domain.AudioStreamFacts{
				SampleFormat: "fltp", SampleRate: 48000, Channels: 2, ChannelLayout: "stereo",
			},
		}},
	}
	scheduler := newInvariantMediaScheduler(t, store, clock,
		fixedIdentifyExecutor{result: application.MediaIdentification{Fingerprint: fingerprint, Observation: observation}},
		fixedProbeExecutor{result: probe, version: "ffprobe-fixture-v1"},
	)
	if err := scheduler.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("initial media execution %d executed=%v err=%v", index, executed, runErr)
		}
	}
	assetReads, _ := application.NewAssetReads(store)
	first, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || first.Facts == nil || len(first.Facts.Streams) != 1 {
		t.Fatalf("first probe facts=%+v err=%v", first.Facts, err)
	}
	stableStreamID := first.Facts.Streams[0].ID

	now = now.Add(time.Second)
	insertQueuedProbeJob(t, store.Path(), created.Project.Project.ID, registered.Asset.Asset.ID, now, "b")
	richerProbe := probe
	richerProbe.Streams = append([]domain.SourceStreamDescriptor(nil), probe.Streams...)
	richerProbe.Streams[0].CodecProfile = "LC"
	reprobe := newInvariantMediaScheduler(t, store, clock,
		fixedProbeExecutor{result: richerProbe, version: "ffprobe-fixture-v2"},
	)
	if err := reprobe.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if executed, runErr := reprobe.RunOne(ctx); runErr != nil || !executed {
		t.Fatalf("reprobe executed=%v err=%v", executed, runErr)
	}
	second, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || second.Facts == nil || len(second.Facts.Streams) != 1 ||
		second.Facts.Streams[0].ID != stableStreamID || second.Facts.Streams[0].Descriptor.CodecProfile != "LC" {
		t.Fatalf("reprobe facts=%+v err=%v", second.Facts, err)
	}
	if count := sourceStreamCount(t, store.Path(), registered.Asset.Asset.ID); count != 1 {
		t.Fatalf("source stream count=%d, want 1", count)
	}

	now = now.Add(time.Second)
	jobOnlyID := insertQueuedProbeJob(t, store.Path(), created.Project.Project.ID, registered.Asset.Asset.ID, now, "c")
	unsupported := newInvariantMediaScheduler(t, store, clock, fixedProbeExecutor{
		version: "ffprobe-fixture-v3",
		err:     application.NewMediaExecutionError("unsupported-media", errors.New("fixture is not a supported container")),
	})
	if err := unsupported.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if executed, runErr := unsupported.RunOne(ctx); runErr != nil || !executed {
		t.Fatalf("unsupported probe executed=%v err=%v", executed, runErr)
	}
	jobOnlyAsset, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	jobOnly, jobOnlyFound := mediaJobSummary(jobOnlyAsset.Jobs, jobOnlyID)
	if err != nil || jobOnlyAsset.Availability != domain.AssetOnline || !jobOnlyFound ||
		jobOnly.State != domain.MediaJobFailed || jobOnly.TerminalErrorCode == nil ||
		*jobOnly.TerminalErrorCode != "unsupported-media" {
		t.Fatalf("job-only failure availability=%s jobs=%+v err=%v", jobOnlyAsset.Availability, jobOnlyAsset.Jobs, err)
	}

	now = now.Add(time.Second)
	sourceFailureID := insertQueuedProbeJob(t, store.Path(), created.Project.Project.ID, registered.Asset.Asset.ID, now, "d")
	sourceMissing := newInvariantMediaScheduler(t, store, clock, fixedProbeExecutor{
		version: "ffprobe-fixture-v4",
		err: application.NewMediaSourceExecutionError(
			"source-missing", domain.AssetMissing, application.ErrMediaSourceRead,
		),
	})
	if err := sourceMissing.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if executed, runErr := sourceMissing.RunOne(ctx); runErr != nil || !executed {
		t.Fatalf("missing-source probe executed=%v err=%v", executed, runErr)
	}
	missingAsset, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	missingJob, missingJobFound := mediaJobSummary(missingAsset.Jobs, sourceFailureID)
	if err != nil || missingAsset.Availability != domain.AssetMissing || !missingJobFound ||
		missingJob.State != domain.MediaJobFailed || missingJob.TerminalErrorCode == nil ||
		*missingJob.TerminalErrorCode != "source-missing" {
		t.Fatalf("source failure availability=%s jobs=%+v err=%v", missingAsset.Availability, missingAsset.Jobs, err)
	}
}

func newInvariantMediaScheduler(
	t *testing.T,
	store *repository.SQLiteProjects,
	clock application.Clock,
	executors ...application.MediaJobExecutor,
) *application.WorkScheduler {
	t.Helper()
	return newTestWorkScheduler(t, store, executors, clock, "api:media-invariants")
}

func insertQueuedProbeJob(
	t *testing.T,
	databasePath string,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	at time.Time,
	digestDigit string,
) domain.MediaJobID {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := domain.ParseMediaJobID(value)
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	digest := "sha256:" + strings.Repeat(digestDigit, 64)
	_, err = database.Exec(`
INSERT INTO work_jobs (
  id, scope_kind, project_id, kind, state, pool, priority_class, logical_key,
  parameters_digest, parameters_json, producer_version, created_at, updated_at
) VALUES (?, 'project', ?, 'probe', 'queued', 'interactive-cpu', 'foreground', ?, ?, '{}', ?, ?, ?)`,
		jobID.String(), projectID.String(), "test/reprobe/"+jobID.String(), digest,
		"fixture-"+digestDigit, at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO media_job_details (job_id, asset_id) VALUES (?, ?)`,
		jobID.String(), assetID.String()); err != nil {
		t.Fatal(err)
	}
	return jobID
}

func sourceStreamCount(t *testing.T, databasePath string, assetID domain.AssetID) int {
	t.Helper()
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM source_streams WHERE asset_id = ?`, assetID.String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func mediaJobState(jobs []domain.MediaJobSummary, id domain.MediaJobID) domain.MediaJobState {
	job, found := mediaJobSummary(jobs, id)
	if !found {
		return ""
	}
	return job.State
}

func mediaJobSummary(jobs []domain.MediaJobSummary, id domain.MediaJobID) (domain.MediaJobSummary, bool) {
	for _, job := range jobs {
		if job.ID == id {
			return job, true
		}
	}
	return domain.MediaJobSummary{}, false
}
