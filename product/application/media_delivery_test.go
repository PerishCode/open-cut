package application

import (
	"context"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestPrepareSourcePreviewReconcilesConcurrentIntegrityRetry(t *testing.T) {
	projectID, _ := domain.ParseProjectID("018f0000-0000-7000-8000-000000000201")
	assetID, _ := domain.ParseAssetID("018f0000-0000-7000-8000-000000000202")
	originalJobID, _ := domain.ParseMediaJobID("018f0000-0000-7000-8000-000000000203")
	retryJobID, _ := domain.ParseMediaJobID("018f0000-0000-7000-8000-000000000204")
	artifactID, _ := domain.ParseArtifactID("018f0000-0000-7000-8000-000000000205")
	revision, _ := domain.NewRevision(2)
	fingerprint := domain.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	video := proxyVideoStream(t, "018f0000-0000-7000-8000-000000000206", 0, nil)
	repository := &integrityRetryMediaRepository{
		snapshot: SourcePreviewSelectionSnapshot{
			ProjectID: projectID, AssetID: assetID, AssetRevision: revision,
			Fingerprint: fingerprint, Video: &video,
		},
		resolutions: []SourcePreviewResolution{
			{
				ProjectID: projectID, AssetID: assetID, AssetRevision: revision,
				Fingerprint: fingerprint, Video: &video,
				Job: domain.MediaJobSummary{
					ID: originalJobID, Kind: domain.MediaJobProxy, State: domain.MediaJobSucceeded,
					ResultArtifactID: &artifactID,
				},
				Artifact: &domain.ArtifactSummary{
					ID: artifactID, Kind: domain.ArtifactProxy, State: domain.ArtifactEvicted,
					InputFingerprint: fingerprint,
				},
			},
			{
				ProjectID: projectID, AssetID: assetID, AssetRevision: revision,
				Fingerprint: fingerprint, Video: &video, RejectedArtifactID: &artifactID,
				Job: domain.MediaJobSummary{
					ID: retryJobID, Kind: domain.MediaJobProxy, State: domain.MediaJobBlocked,
				},
			},
		},
		jobIDs: []domain.MediaJobID{originalJobID, retryJobID},
	}
	viewer, err := NewViewerMedia(
		repository, &sequenceIdentities{}, ClockFunc(func() time.Time { return applicationInstant }),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := viewer.PrepareSourcePreview(
		applicationCreatorContext(t), projectID, assetID,
		SourcePreviewSelectionInput{
			AssetRevision: revision, Fingerprint: fingerprint, VideoStreamID: &video.ID,
		},
		MediaLeaseSourcePreview,
	)
	if err != nil || result.Status != MediaPreparationPreparing || result.Job.ID != retryJobID ||
		result.Stage == nil || *result.Stage != MediaPreparationProxy || len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code != MediaDiagnosticProxyIntegrityRejected {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if repository.ensureCalls != 2 || repository.resolveCalls != 2 {
		t.Fatalf("ensure=%d resolve=%d", repository.ensureCalls, repository.resolveCalls)
	}
}

type integrityRetryMediaRepository struct {
	snapshot     SourcePreviewSelectionSnapshot
	jobIDs       []domain.MediaJobID
	resolutions  []SourcePreviewResolution
	ensureCalls  int
	resolveCalls int
}

func (repository *integrityRetryMediaRepository) LoadSourcePreviewSelection(
	context.Context,
	domain.ProjectID,
	domain.AssetID,
	SourcePreviewSelectionInput,
) (SourcePreviewSelectionSnapshot, error) {
	return repository.snapshot, nil
}

func (repository *integrityRetryMediaRepository) EnsureExplicitSourceProxyJob(
	context.Context,
	EnsureExplicitSourceProxyJobRecord,
) (domain.WorkJobID, error) {
	jobID := repository.jobIDs[repository.ensureCalls]
	repository.ensureCalls++
	return jobID, nil
}

func (repository *integrityRetryMediaRepository) ResolveSourcePreview(
	context.Context,
	domain.ProjectID,
	domain.AssetID,
	domain.MediaJobID,
	domain.Digest,
) (SourcePreviewResolution, error) {
	resolution := repository.resolutions[repository.resolveCalls]
	repository.resolveCalls++
	return resolution, nil
}

func (*integrityRetryMediaRepository) RejectSourceProxyArtifact(
	context.Context,
	RejectSourceProxyArtifactRecord,
) (domain.MediaJobSummary, error) {
	return domain.MediaJobSummary{}, nil
}
