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

func TestMediaAttemptLeaseRecoveryRejectsLatePublisherAndAdvancesGeneration(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, _, _, _ := testProjectApplications(t, store)
	created, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: mustRequestID(t, "gesture:lease-recovery-project"), Name: "Lease recovery",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	clock := application.ClockFunc(func() time.Time { return now })
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	size, _ := domain.NewUInt64(2048)
	observation := domain.SourceObservation{
		ByteSize: size, ModifiedUnixNs: domain.NewInt64(12345), FileIdentity: "dev:10:inode:20",
	}
	grant, err := media.RegisterSourceGrant(creatorContext(t), application.RegisterSourceGrantInput{
		RequestID: mustRequestID(t, "picker:lease-recovery"), Platform: "mac", Kind: domain.SourceGrantLocalPath,
		DisplayName: "recovery.mov", Observation: observation,
		ProtectedMaterial: []byte(`{"schema":"open-cut/source-grant-material/local-path-v1","path":"/private/recovery.mov"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(
		creatorContext(t), created.Project.Project.ID,
		application.RegisterAssetInput{
			RequestID: mustRequestID(t, "gesture:lease-recovery-asset"), SourceGrantID: grant.Grant.ID,
			ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	attemptID := mustJobAttemptID(t, now)
	claim, err := store.ClaimMediaJob(ctx, application.ClaimMediaJobInput{
		AttemptID: attemptID, Executors: []application.MediaExecutorRegistration{{
			Kind: domain.MediaJobIdentify, Version: application.InitialMediaProducer,
		}}, LeaseOwner: "api:dead",
		Now: now, LeaseDuration: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	staleFingerprint, _ := domain.ParseDigest("sha256:" + strings.Repeat("a", 64))
	if err := store.CompleteMediaIdentification(ctx, application.CompleteMediaIdentification{
		Claim: claim, Fingerprint: staleFingerprint, Observation: observation,
		EventID: mustActivityEventID(t, now.Add(4*time.Second)), CompletedAt: now.Add(4 * time.Second),
	}); !errors.Is(err, application.ErrMediaLeaseLost) {
		t.Fatalf("expired publisher error=%v", err)
	}
	now = now.Add(4 * time.Second)
	if err := store.RecoverMediaJobs(ctx, []application.MediaExecutorRegistration{{
		Kind: domain.MediaJobIdentify, Version: application.InitialMediaProducer,
	}}, now); err != nil {
		t.Fatal(err)
	}
	selectedFingerprint, _ := domain.ParseDigest("sha256:" + strings.Repeat("b", 64))
	scheduler := newTestWorkScheduler(t, store,
		[]application.MediaJobExecutor{fixedIdentifyExecutor{result: application.MediaIdentification{
			Fingerprint: selectedFingerprint, Observation: observation,
		}}},
		clock, "api:recovered")
	if executed, err := scheduler.RunOne(ctx); err != nil || !executed {
		t.Fatalf("recovered execution=%v err=%v", executed, err)
	}
	if err := store.CompleteMediaIdentification(ctx, application.CompleteMediaIdentification{
		Claim: claim, Fingerprint: staleFingerprint, Observation: observation,
		EventID: mustActivityEventID(t, now.Add(time.Second)), CompletedAt: now.Add(time.Second),
	}); !errors.Is(err, application.ErrMediaLeaseLost) {
		t.Fatalf("late publisher error=%v", err)
	}
	assetReads, _ := application.NewAssetReads(store)
	asset, _, err := assetReads.Inspect(creatorContext(t), created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || asset.Fingerprint == nil || *asset.Fingerprint != selectedFingerprint {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	db, err := sql.Open("sqlite3", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
SELECT generation, state FROM work_job_attempts WHERE job_id = ? ORDER BY generation`, claim.JobID.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := []struct {
		generation int
		state      string
	}{{1, "abandoned"}, {2, "succeeded"}}
	for _, expected := range want {
		if !rows.Next() {
			t.Fatalf("missing attempt %+v", expected)
		}
		var generation int
		var state string
		if err := rows.Scan(&generation, &state); err != nil || generation != expected.generation || state != expected.state {
			t.Fatalf("attempt generation=%d state=%s err=%v", generation, state, err)
		}
	}
	if rows.Next() {
		t.Fatal("unexpected extra attempt")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	probeExecutors := []application.MediaExecutorRegistration{{
		Kind: domain.MediaJobProbe, Version: "ffprobe-fixture-v1",
	}}
	leaseNow := now.Add(2 * time.Second)
	var probeJobID domain.WorkJobID
	for generation := 1; generation <= 3; generation++ {
		probeClaim, claimErr := store.ClaimMediaJob(ctx, application.ClaimMediaJobInput{
			AttemptID: mustJobAttemptID(t, leaseNow), Executors: probeExecutors,
			LeaseOwner: "api:crash-loop", Now: leaseNow, LeaseDuration: 3 * time.Second,
		})
		if claimErr != nil || probeClaim.Kind != domain.MediaJobProbe || probeClaim.Generation != uint64(generation) {
			t.Fatalf("probe generation=%d claim=%+v err=%v", generation, probeClaim, claimErr)
		}
		probeJobID = probeClaim.JobID
		leaseNow = leaseNow.Add(4 * time.Second)
		if err := store.RecoverMediaJobs(ctx, probeExecutors, leaseNow); err != nil {
			t.Fatal(err)
		}
	}
	var probeState, terminalCode string
	if err := db.QueryRowContext(ctx, `
SELECT state, terminal_error_code FROM work_jobs WHERE id = ?`, probeJobID.String()).Scan(
		&probeState, &terminalCode,
	); err != nil {
		t.Fatal(err)
	}
	if probeState != "failed" || terminalCode != "attempt-limit-exceeded" {
		t.Fatalf("probe state=%s terminal=%s", probeState, terminalCode)
	}
}

func mustJobAttemptID(t *testing.T, at time.Time) domain.JobAttemptID {
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

func mustActivityEventID(t *testing.T, at time.Time) domain.ActivityEventID {
	t.Helper()
	value, err := domain.GenerateUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.ParseActivityEventID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
