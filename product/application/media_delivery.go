package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

type MediaLeasePurpose string

const (
	MediaLeaseSourcePreview   MediaLeasePurpose = "source-preview"
	MediaLeaseSequencePreview MediaLeasePurpose = "sequence-preview"
)

var (
	ErrSourceProxyIntegrity     = errors.New("source proxy artifact failed integrity verification")
	ErrSequencePreviewIntegrity = errors.New("sequence preview artifact failed integrity verification")
)

type MediaPreparationStatus string

const (
	MediaPreparationReady     MediaPreparationStatus = "ready"
	MediaPreparationPreparing MediaPreparationStatus = "preparing"
	MediaPreparationFailed    MediaPreparationStatus = "failed"
)

type MediaPreparationStage string

const (
	MediaPreparationProxy     MediaPreparationStage = "proxy"
	MediaPreparationIntegrity MediaPreparationStage = "integrity"
	MediaPreparationRender    MediaPreparationStage = "render"
)

type MediaDiagnosticCode string

const (
	MediaDiagnosticProxyIntegrityRejected    MediaDiagnosticCode = "source-proxy-integrity-rejected"
	MediaDiagnosticProxyJobFailed            MediaDiagnosticCode = "source-proxy-job-failed"
	MediaDiagnosticProxyJobCancelled         MediaDiagnosticCode = "source-proxy-job-cancelled"
	MediaDiagnosticSequenceIntegrityRejected MediaDiagnosticCode = "sequence-preview-integrity-rejected"
	MediaDiagnosticSequenceJobFailed         MediaDiagnosticCode = "sequence-preview-job-failed"
	MediaDiagnosticSequenceJobCancelled      MediaDiagnosticCode = "sequence-preview-job-cancelled"
)

type MediaDiagnosticSeverity string

const (
	MediaDiagnosticDegraded MediaDiagnosticSeverity = "degraded"
	MediaDiagnosticBlocking MediaDiagnosticSeverity = "blocking"
)

type MediaDiagnosticSubjectKind string

const (
	MediaDiagnosticAsset    MediaDiagnosticSubjectKind = "asset"
	MediaDiagnosticJob      MediaDiagnosticSubjectKind = "media-job"
	MediaDiagnosticWorkJob  MediaDiagnosticSubjectKind = "work-job"
	MediaDiagnosticArtifact MediaDiagnosticSubjectKind = "artifact"
)

type MediaRecoveryAction string

const (
	MediaRecoveryAutomaticRetry  MediaRecoveryAction = "automatic-retry"
	MediaRecoveryRetryJob        MediaRecoveryAction = "retry-job"
	MediaRecoveryRelinkSource    MediaRecoveryAction = "relink-source"
	MediaRecoveryAcquireResource MediaRecoveryAction = "acquire-resource"
	MediaRecoveryAdoptRevision   MediaRecoveryAction = "adopt-revision"
	MediaRecoveryUpdateRuntime   MediaRecoveryAction = "update-runtime"
	MediaRecoveryNone            MediaRecoveryAction = "none"
)

type MediaDiagnostic struct {
	Code        MediaDiagnosticCode        `json:"code" enum:"source-proxy-integrity-rejected,source-proxy-job-failed,source-proxy-job-cancelled,sequence-preview-integrity-rejected,sequence-preview-job-failed,sequence-preview-job-cancelled"`
	Severity    MediaDiagnosticSeverity    `json:"severity" enum:"degraded,blocking"`
	SubjectKind MediaDiagnosticSubjectKind `json:"subjectKind" enum:"asset,media-job,work-job,artifact"`
	SubjectID   string                     `json:"subjectId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Recovery    MediaRecoveryAction        `json:"recovery" enum:"automatic-retry,retry-job,relink-source,acquire-resource,adopt-revision,update-runtime,none"`
}

type SourcePreviewResolution struct {
	ProjectID          domain.ProjectID
	AssetID            domain.AssetID
	AssetRevision      domain.Revision
	Fingerprint        domain.Digest
	Video              *domain.SourceStream
	Audio              *domain.SourceStream
	Artifact           *domain.ArtifactSummary
	Job                domain.MediaJobSummary
	RejectedArtifactID *domain.ArtifactID
}

type SourcePreviewSelectionInput struct {
	AssetRevision domain.Revision        `json:"assetRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Fingerprint   domain.Digest          `json:"fingerprint" format:"sha256-digest"`
	VideoStreamID *domain.SourceStreamID `json:"videoStreamId,omitempty"`
	AudioStreamID *domain.SourceStreamID `json:"audioStreamId,omitempty"`
}

type SourcePreviewSelectionSnapshot struct {
	ProjectID     domain.ProjectID
	AssetID       domain.AssetID
	AssetRevision domain.Revision
	Fingerprint   domain.Digest
	Video         *domain.SourceStream
	Audio         *domain.SourceStream
}

type SourcePreviewPreparation struct {
	Status        MediaPreparationStatus  `json:"status" enum:"ready,preparing,failed"`
	Purpose       MediaLeasePurpose       `json:"purpose" enum:"source-preview"`
	ProjectID     domain.ProjectID        `json:"projectId"`
	AssetID       domain.AssetID          `json:"assetId"`
	AssetRevision domain.Revision         `json:"assetRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Fingerprint   domain.Digest           `json:"fingerprint"`
	VideoStreamID *domain.SourceStreamID  `json:"videoStreamId,omitempty"`
	AudioStreamID *domain.SourceStreamID  `json:"audioStreamId,omitempty"`
	Artifact      *domain.ArtifactSummary `json:"artifact,omitempty"`
	Job           domain.MediaJobSummary  `json:"job"`
	Stage         *MediaPreparationStage  `json:"stage,omitempty" enum:"proxy,integrity,render"`
	Diagnostics   []MediaDiagnostic       `json:"diagnostics" maxItems:"32" nullable:"false"`
}

type RejectSourceProxyArtifactRecord struct {
	ProjectID  domain.ProjectID
	AssetID    domain.AssetID
	ArtifactID domain.ArtifactID
	JobID      domain.MediaJobID
	RetryJobID domain.MediaJobID
	EventID    domain.ActivityEventID
	Code       MediaDiagnosticCode
	RejectedAt time.Time
}

type MediaDeliveryRepository interface {
	LoadSourcePreviewSelection(
		context.Context, domain.ProjectID, domain.AssetID, SourcePreviewSelectionInput,
	) (SourcePreviewSelectionSnapshot, error)
	EnsureExplicitSourceProxyJob(context.Context, EnsureExplicitSourceProxyJobRecord) (domain.WorkJobID, error)
	ResolveSourcePreview(
		context.Context, domain.ProjectID, domain.AssetID, domain.MediaJobID, domain.Digest,
	) (SourcePreviewResolution, error)
	RejectSourceProxyArtifact(context.Context, RejectSourceProxyArtifactRecord) (domain.MediaJobSummary, error)
}

type ViewerMedia struct {
	repository MediaDeliveryRepository
	identities IdentityGenerator
	clock      Clock
}

func NewViewerMedia(repository MediaDeliveryRepository, identities IdentityGenerator, clock Clock) (*ViewerMedia, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("viewer media repository is required")
	}
	return &ViewerMedia{repository: repository, identities: identities, clock: clock}, nil
}

func (media *ViewerMedia) PrepareSourcePreview(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	selectionInput SourcePreviewSelectionInput,
	purpose MediaLeasePurpose,
) (SourcePreviewPreparation, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return SourcePreviewPreparation{}, err
	}
	if projectID.IsZero() || assetID.IsZero() || purpose != MediaLeaseSourcePreview ||
		!validSourcePreviewSelection(selectionInput) {
		return SourcePreviewPreparation{}, ErrAssetInvalid
	}
	snapshot, err := media.repository.LoadSourcePreviewSelection(ctx, projectID, assetID, selectionInput)
	if err != nil {
		return SourcePreviewPreparation{}, err
	}
	selection := SourceProxySelection{Policy: SourceProxySelectionExplicit}
	streams := make([]domain.SourceStream, 0, 2)
	if snapshot.Video != nil {
		selection.VideoStreamID = &snapshot.Video.ID
		streams = append(streams, *snapshot.Video)
	}
	if snapshot.Audio != nil {
		selection.AudioStreamID = &snapshot.Audio.ID
		streams = append(streams, *snapshot.Audio)
	}
	parameters := InitialMediaJobParameters{
		AssetID: assetID, Kind: domain.MediaJobProxy, Profile: SourceProxyProfile, ProxySelection: &selection,
	}
	canonical, digest, err := CanonicalInitialMediaJobParameters(parameters)
	if err != nil {
		return SourcePreviewPreparation{}, ErrAssetInvalid
	}
	at := media.clock.Now().UTC()
	var resolution SourcePreviewResolution
	for attempt := 0; attempt < 2; attempt++ {
		jobValue, identityErr := media.identities.NewID(ctx, at)
		if identityErr != nil {
			return SourcePreviewPreparation{}, identityErr
		}
		jobID, parseErr := domain.ParseWorkJobID(jobValue)
		if parseErr != nil {
			return SourcePreviewPreparation{}, parseErr
		}
		jobID, err = media.repository.EnsureExplicitSourceProxyJob(ctx, EnsureExplicitSourceProxyJobRecord{
			JobID: jobID, ProjectID: projectID, AssetID: assetID, Fingerprint: snapshot.Fingerprint,
			SourceStreams: streams, Parameters: parameters, Canonical: canonical, Digest: digest,
			LogicalKey: "media/v1/" + assetID.String() + "/proxy/" + digest.String(), CreatedAt: at,
		})
		if err != nil {
			return SourcePreviewPreparation{}, err
		}
		resolution, err = media.repository.ResolveSourcePreview(ctx, projectID, assetID, jobID, digest)
		if err != nil {
			return SourcePreviewPreparation{}, err
		}
		if attempt == 0 && succeededSourceProxyWasEvicted(resolution) {
			continue
		}
		break
	}
	if resolution.ProjectID != projectID || resolution.AssetID != assetID ||
		resolution.AssetRevision != snapshot.AssetRevision || resolution.Fingerprint != snapshot.Fingerprint ||
		!sameOptionalSourceStream(resolution.Video, snapshot.Video) ||
		!sameOptionalSourceStream(resolution.Audio, snapshot.Audio) ||
		resolution.Job.ID.IsZero() || resolution.Job.Kind != domain.MediaJobProxy {
		return SourcePreviewPreparation{}, ErrAssetInvalid
	}
	result := SourcePreviewPreparation{
		Purpose: purpose, ProjectID: projectID, AssetID: assetID, AssetRevision: resolution.AssetRevision,
		Fingerprint: resolution.Fingerprint, Artifact: resolution.Artifact, Job: resolution.Job,
		Diagnostics: []MediaDiagnostic{},
	}
	if resolution.Video != nil {
		id := resolution.Video.ID
		result.VideoStreamID = &id
	}
	if resolution.Audio != nil {
		id := resolution.Audio.ID
		result.AudioStreamID = &id
	}
	switch resolution.Job.State {
	case domain.MediaJobBlocked, domain.MediaJobQueued, domain.MediaJobRunning:
		if resolution.Artifact != nil || resolution.Job.ResultArtifactID != nil {
			return SourcePreviewPreparation{}, ErrAssetInvalid
		}
		result.Status = MediaPreparationPreparing
		stage := MediaPreparationProxy
		result.Stage = &stage
		if resolution.RejectedArtifactID != nil {
			result.Diagnostics = append(result.Diagnostics, MediaDiagnostic{
				Code: MediaDiagnosticProxyIntegrityRejected, Severity: MediaDiagnosticBlocking,
				SubjectKind: MediaDiagnosticArtifact, SubjectID: resolution.RejectedArtifactID.String(),
				Recovery: MediaRecoveryAutomaticRetry,
			})
		}
	case domain.MediaJobSucceeded:
		if resolution.Artifact == nil || resolution.Job.ResultArtifactID == nil ||
			resolution.Artifact.ID != *resolution.Job.ResultArtifactID ||
			resolution.Artifact.Kind != domain.ArtifactProxy ||
			resolution.Artifact.State != domain.ArtifactReady ||
			resolution.Artifact.InputFingerprint != resolution.Fingerprint {
			return SourcePreviewPreparation{}, ErrAssetInvalid
		}
		result.Status = MediaPreparationReady
	case domain.MediaJobFailed, domain.MediaJobCancelled:
		if resolution.Artifact != nil {
			return SourcePreviewPreparation{}, ErrAssetInvalid
		}
		result.Status = MediaPreparationFailed
		stage := MediaPreparationProxy
		result.Stage = &stage
		code := MediaDiagnosticProxyJobFailed
		if resolution.Job.State == domain.MediaJobCancelled {
			code = MediaDiagnosticProxyJobCancelled
		}
		result.Diagnostics = append(result.Diagnostics, MediaDiagnostic{
			Code: code, Severity: MediaDiagnosticBlocking, SubjectKind: MediaDiagnosticJob,
			SubjectID: resolution.Job.ID.String(), Recovery: MediaRecoveryRelinkSource,
		})
	default:
		return SourcePreviewPreparation{}, ErrAssetInvalid
	}
	return result, nil
}

func succeededSourceProxyWasEvicted(resolution SourcePreviewResolution) bool {
	return resolution.Job.State == domain.MediaJobSucceeded && resolution.Artifact != nil &&
		resolution.Artifact.State == domain.ArtifactEvicted
}

func validSourcePreviewSelection(input SourcePreviewSelectionInput) bool {
	if input.AssetRevision.Value() == 0 || (input.VideoStreamID == nil && input.AudioStreamID == nil) ||
		(input.VideoStreamID != nil && input.VideoStreamID.IsZero()) ||
		(input.AudioStreamID != nil && input.AudioStreamID.IsZero()) {
		return false
	}
	_, err := domain.ParseDigest(input.Fingerprint.String())
	return err == nil
}

func sameOptionalSourceStream(left, right *domain.SourceStream) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.ID == right.ID && left.Descriptor.Validate() == nil && right.Descriptor.Validate() == nil
}

func (media *ViewerMedia) RejectSourcePreviewArtifact(
	ctx context.Context,
	record RejectSourceProxyArtifactRecord,
) (domain.MediaJobSummary, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return domain.MediaJobSummary{}, err
	}
	if record.ProjectID.IsZero() || record.AssetID.IsZero() || record.ArtifactID.IsZero() ||
		record.JobID.IsZero() || record.RetryJobID.IsZero() || record.EventID.IsZero() ||
		record.JobID == record.RetryJobID || record.Code != MediaDiagnosticProxyIntegrityRejected ||
		record.RejectedAt.IsZero() {
		return domain.MediaJobSummary{}, ErrAssetInvalid
	}
	return media.repository.RejectSourceProxyArtifact(ctx, record)
}
