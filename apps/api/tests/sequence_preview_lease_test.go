package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequencePreviewLeaseUsesClosedPrepareContinueRetryOperation(t *testing.T) {
	parallelAPITest(t)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	projectID, _ := domain.ParseProjectID("018f0a60-7b80-7a01-8000-000000000001")
	sequenceID, _ := domain.ParseSequenceID("018f0a60-7b80-7a01-8000-000000000002")
	jobID, _ := domain.ParseWorkJobID("018f0a60-7b80-7a01-8000-000000000003")
	revision, _ := domain.NewRevision(2)
	repository := &leaseSequencePreviewRepository{projection: application.SequencePreviewJobProjection{
		ID: jobID, State: domain.MediaJobRunning, ProgressBasisPoints: 2500, CreatedAt: now, UpdatedAt: now,
	}}
	previews, err := application.NewSequencePreviews(
		repository, application.UUIDv7IdentityGenerator{}, application.ClockFunc(func() time.Time { return now }),
		application.SequencePreviewSettings{
			RendererVersion: application.SequencePreviewRendererV1 + "@fixture", RendererTarget: "mac-arm64",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	leaseService, err := service.NewSequencePreviewLeaseService(
		previews, leaseSequencePreviewOpener{}, application.UUIDv7IdentityGenerator{},
		application.ClockFunc(func() time.Time { return now }), strings.NewReader(strings.Repeat("a", 128)),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := leaseCreatorContext(t, now, "electron-sequence-operation")
	continuation := &service.SequencePreviewContinuation{JobID: jobID}
	result, err := leaseService.Create(ctx, projectID, sequenceID, service.SequencePreviewLeaseRequest{
		Purpose: application.MediaLeaseSequencePreview, Operation: service.SequencePreviewContinue,
		ExpectedSequenceRevision: revision, Continuation: continuation,
	})
	if err != nil || result.Status != application.SequencePreviewPreparing ||
		result.Continuation == nil || result.Continuation.JobID != jobID || repository.continues != 1 {
		t.Fatalf("continue result=%+v calls=%d err=%v", result, repository.continues, err)
	}
	for _, request := range []service.SequencePreviewLeaseRequest{
		{Purpose: application.MediaLeaseSequencePreview, Operation: service.SequencePreviewPrepare,
			ExpectedSequenceRevision: revision, Continuation: continuation},
		{Purpose: application.MediaLeaseSequencePreview, Operation: service.SequencePreviewContinue,
			ExpectedSequenceRevision: revision},
		{Purpose: application.MediaLeaseSequencePreview, Operation: service.SequencePreviewRetry,
			ExpectedSequenceRevision: revision},
	} {
		if _, err := leaseService.Create(ctx, projectID, sequenceID, request); !errors.Is(err, service.ErrMediaLeaseInvalid) {
			t.Fatalf("invalid operation request=%+v err=%v", request, err)
		}
	}
}

func TestSequencePreviewLeaseServesOnlyPinnedRangeToIssuingClient(t *testing.T) {
	parallelAPITest(t)
	now := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)
	projectID, _ := domain.ParseProjectID("018f0a60-7b80-7a01-8000-000000000011")
	sequenceID, _ := domain.ParseSequenceID("018f0a60-7b80-7a01-8000-000000000012")
	jobID, _ := domain.ParseWorkJobID("018f0a60-7b80-7a01-8000-000000000013")
	artifactID, _ := domain.ParseArtifactID("018f0a60-7b80-7a01-8000-000000000014")
	revision, _ := domain.NewRevision(3)
	planDigest, _ := domain.ParseDigest("sha256:" + strings.Repeat("a", 64))
	contentDigest, _ := domain.ParseDigest("sha256:" + strings.Repeat("b", 64))
	mediaDigest, _ := domain.ParseDigest("sha256:" + strings.Repeat("c", 64))
	duration, _ := domain.NewRationalTime(1, 1)
	frameRate, _ := domain.NewRationalTime(30, 1)
	videoFrames, _ := domain.NewUInt64(30)
	audioSamples, _ := domain.NewUInt64(48_000)
	mediaSize, _ := domain.NewUInt64(10)
	artifactSize, _ := domain.NewUInt64(100)
	artifact := domain.SequencePreviewArtifactSummary{
		ID: artifactID, ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: revision,
		RenderPlanDigest: planDigest, RendererVersion: application.SequencePreviewRendererV1 + "@fixture",
		RendererTarget: "mac-arm64", Profile: domain.SequencePreviewProfileV1,
		State: domain.SequencePreviewArtifactReady, ByteSize: artifactSize, ContentDigest: contentDigest,
		Facts: domain.SequencePreviewMediaFacts{
			SemanticDuration: duration, PresentationDuration: duration,
			CanvasWidth: 1280, CanvasHeight: 720, FrameRate: frameRate,
			VideoFrameCount: videoFrames, AudioSampleRate: 48_000, AudioSampleCount: audioSamples,
			VideoCodec: "vp9", AudioCodec: "opus", PixelFormat: "yuv420p", ChannelLayout: "stereo",
		},
	}
	repository := &leaseSequencePreviewRepository{projection: application.SequencePreviewJobProjection{
		ID: jobID, State: domain.MediaJobSucceeded, ProgressBasisPoints: 10_000,
		RenderPlanDigest: &planDigest, Artifact: &artifact, CreatedAt: now, UpdatedAt: now,
	}}
	previews, err := application.NewSequencePreviews(
		repository, application.UUIDv7IdentityGenerator{}, application.ClockFunc(func() time.Time { return now }),
		application.SequencePreviewSettings{
			RendererVersion: application.SequencePreviewRendererV1 + "@fixture", RendererTarget: "mac-arm64",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "preview.webm")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	opener := leaseSequencePreviewOpener{
		path: path, current: true,
		media: application.SequencePreviewArtifactFile{
			Path: "preview.webm", MimeType: "video/webm", ByteSize: mediaSize, SHA256: mediaDigest,
		},
	}
	leaseService, err := service.NewSequencePreviewLeaseService(
		previews, opener, application.UUIDv7IdentityGenerator{}, application.ClockFunc(func() time.Time { return now }),
		strings.NewReader(strings.Repeat("a", 256)),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, renewed, copied := leaseCreatorRotationContexts(t, now, "issuing-session")
	request := service.SequencePreviewLeaseRequest{
		Purpose: application.MediaLeaseSequencePreview, Operation: service.SequencePreviewContinue,
		ExpectedSequenceRevision: revision, Continuation: &service.SequencePreviewContinuation{
			JobID: jobID, RenderPlanDigest: &planDigest,
		},
	}
	var lease *service.SequencePreviewLease
	deadline := time.Now().Add(time.Second)
	for lease == nil {
		result, createErr := leaseService.Create(ctx, projectID, sequenceID, request)
		if createErr != nil {
			t.Fatal(createErr)
		}
		lease = result.Lease
		if lease == nil {
			if time.Now().After(deadline) {
				t.Fatalf("verification did not publish a lease: %+v", result)
			}
			time.Sleep(time.Millisecond)
		}
	}
	token := strings.TrimPrefix(lease.SameOriginURL, "/api/v1/media/content/")
	requestHTTP := httptest.NewRequest(http.MethodGet, lease.SameOriginURL, nil)
	requestHTTP.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	if err := leaseService.ServeContent(ctx, response, requestHTTP, token); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusPartialContent || response.Body.String() != "2345" ||
		response.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	renewedResponse := httptest.NewRecorder()
	if err := leaseService.ServeContent(renewed, renewedResponse, requestHTTP, token); err != nil ||
		renewedResponse.Code != http.StatusPartialContent || renewedResponse.Body.String() != "2345" {
		t.Fatalf("rotated session range status=%d body=%q err=%v", renewedResponse.Code, renewedResponse.Body.String(), err)
	}
	if err := leaseService.ServeContent(copied, httptest.NewRecorder(), requestHTTP, token); !errors.Is(err, service.ErrMediaLeaseInvalid) {
		t.Fatalf("copied session error=%v", err)
	}
	now = now.Add(service.MediaLeaseTTL + time.Second)
	if err := leaseService.ServeContent(ctx, httptest.NewRecorder(), requestHTTP, token); !errors.Is(err, service.ErrMediaLeaseInvalid) && !errors.Is(err, service.ErrMediaLeaseExpired) {
		t.Fatalf("expired lease error=%v", err)
	}
}

func leaseCreatorContext(t *testing.T, now time.Time, clientInstance string) context.Context {
	t.Helper()
	store, err := repository.OpenSQLiteProjects(context.Background(), filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clock := application.ClockFunc(func() time.Time { return now })
	sessions, privateKey := newTestUISessions(t, store, clock, false)
	token := issueTestUISession(t, sessions, privateKey, clientInstance)
	authority, err := sessions.Authorize(context.Background(), service.AuthorizationRequest{
		Method: http.MethodPost, Route: "/v1/projects/{projectId}/sequences/{sequenceId}/media-leases",
		UISession: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := application.ContextWithAuthority(context.Background(), authority)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := sessions.BindUISession(authorized, token)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}

func leaseCreatorRotationContexts(
	t *testing.T,
	now time.Time,
	clientInstance string,
) (context.Context, context.Context, context.Context) {
	t.Helper()
	store, err := repository.OpenSQLiteProjects(context.Background(), filepath.Join(t.TempDir(), "api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clock := &mutableClock{now: now}
	sessions, privateKey := newTestUISessions(t, store, clock, false)
	issuing := bindLeaseCreatorContext(t, sessions, issueTestUISession(t, sessions, privateKey, clientInstance))
	clock.now = clock.now.Add(time.Second)
	renewed := bindLeaseCreatorContext(t, sessions, issueTestUISession(t, sessions, privateKey, clientInstance))
	clock.now = clock.now.Add(time.Second)
	copied := bindLeaseCreatorContext(t, sessions, issueTestUISession(t, sessions, privateKey, clientInstance+"-copy"))
	return issuing, renewed, copied
}

func bindLeaseCreatorContext(
	t *testing.T,
	sessions *service.UISessionService,
	token string,
) context.Context {
	t.Helper()
	authority, err := sessions.Authorize(context.Background(), service.AuthorizationRequest{
		Method: http.MethodPost, Route: "/v1/projects/{projectId}/sequences/{sequenceId}/media-leases",
		UISession: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := application.ContextWithAuthority(context.Background(), authority)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := sessions.BindUISession(authorized, token)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}

type leaseSequencePreviewRepository struct {
	projection application.SequencePreviewJobProjection
	continues  int
}

func (*leaseSequencePreviewRepository) LoadSequencePreviewPreparation(
	context.Context, domain.ProjectID, domain.SequenceID, domain.Revision,
) (application.SequencePreviewPreparationSnapshot, error) {
	return application.SequencePreviewPreparationSnapshot{}, application.ErrSequencePreviewInvalid
}

func (*leaseSequencePreviewRepository) EnsureExplicitSourceProxyJob(
	context.Context, application.EnsureExplicitSourceProxyJobRecord,
) (domain.WorkJobID, error) {
	return domain.WorkJobID{}, application.ErrSequencePreviewInvalid
}

func (*leaseSequencePreviewRepository) EnsureSequencePreviewJob(
	context.Context, application.EnsureSequencePreviewJobRecord,
) (application.SequencePreviewJobProjection, error) {
	return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
}

func (*leaseSequencePreviewRepository) RejectSequencePreviewArtifact(
	context.Context, application.RejectSequencePreviewArtifactRecord,
) (application.SequencePreviewJobProjection, error) {
	return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewInvalid
}

func (repository *leaseSequencePreviewRepository) LoadSequencePreviewContinuation(
	context.Context, domain.ProjectID, domain.SequenceID, domain.Revision, domain.WorkJobID,
) (application.SequencePreviewJobProjection, error) {
	repository.continues++
	return repository.projection, nil
}

func (*leaseSequencePreviewRepository) LoadSequencePreviewRetrySeed(
	context.Context, domain.ProjectID, domain.SequenceID, domain.Revision, domain.WorkJobID,
) (application.SequencePreviewRetrySeed, error) {
	return application.SequencePreviewRetrySeed{}, application.ErrSequencePreviewRecovery
}

func (*leaseSequencePreviewRepository) RetrySequencePreviewJob(
	context.Context, application.RetrySequencePreviewJobRecord,
) (application.SequencePreviewJobProjection, error) {
	return application.SequencePreviewJobProjection{}, application.ErrSequencePreviewRecovery
}

type leaseSequencePreviewOpener struct {
	path    string
	media   application.SequencePreviewArtifactFile
	current bool
}

func (opener leaseSequencePreviewOpener) OpenSequencePreviewMedia(
	context.Context, domain.ProjectID, domain.SequenceID, domain.Revision, domain.Digest, domain.ArtifactID,
) (*os.File, application.SequencePreviewArtifactFile, error) {
	if opener.path == "" {
		return nil, application.SequencePreviewArtifactFile{}, application.ErrSequencePreviewInvalid
	}
	file, err := os.Open(opener.path)
	return file, opener.media, err
}

func (opener leaseSequencePreviewOpener) IsSequencePreviewMediaVerificationCurrent(
	context.Context, domain.ProjectID, domain.SequenceID, domain.Revision, domain.Digest, domain.ArtifactID,
) (bool, error) {
	return opener.current, nil
}
