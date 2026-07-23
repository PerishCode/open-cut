package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	MediaLeaseSchema = "open-cut/media-lease/v1"
	MediaLeaseTTL    = 5 * time.Minute
	mediaLeaseLimit  = 2048
)

var (
	ErrMediaLeaseInvalid = errors.New("media lease is invalid")
	ErrMediaLeaseExpired = errors.New("media lease expired")
	ErrMediaRangeInvalid = errors.New("media byte range is invalid")
)

type MediaLeaseRequest struct {
	Purpose       application.MediaLeasePurpose `json:"purpose" enum:"source-preview"`
	AssetRevision domain.Revision               `json:"assetRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Fingerprint   domain.Digest                 `json:"fingerprint" format:"sha256-digest"`
	VideoStreamID *domain.SourceStreamID        `json:"videoStreamId,omitempty"`
	AudioStreamID *domain.SourceStreamID        `json:"audioStreamId,omitempty"`
}

type SourcePreviewTrackTiming struct {
	SourceStreamID   domain.SourceStreamID `json:"sourceStreamId"`
	CoverageStart    domain.RationalTime   `json:"coverageStart"`
	CoverageDuration *domain.RationalTime  `json:"coverageDuration,omitempty"`
	SourceStartTime  domain.RationalTime   `json:"sourceStartTime"`
	ProxyStartTime   domain.RationalTime   `json:"proxyStartTime"`
	SourceTimeBase   domain.RationalTime   `json:"sourceTimeBase"`
	ProxyTimeBase    domain.RationalTime   `json:"proxyTimeBase"`
}

type MediaLease struct {
	Schema         string                        `json:"schema" enum:"open-cut/media-lease/v1"`
	ResourceID     domain.ResourceID             `json:"resourceId" format:"uuid"`
	Purpose        application.MediaLeasePurpose `json:"purpose" enum:"source-preview"`
	ProjectID      domain.ProjectID              `json:"projectId"`
	AssetID        domain.AssetID                `json:"assetId"`
	AssetRevision  domain.Revision               `json:"assetRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Fingerprint    domain.Digest                 `json:"fingerprint" format:"sha256-digest"`
	ArtifactID     domain.ArtifactID             `json:"artifactId"`
	ArtifactDigest domain.Digest                 `json:"artifactDigest"`
	MimeType       string                        `json:"mimeType" enum:"video/webm,audio/webm"`
	ByteLength     domain.UInt64                 `json:"byteLength"`
	ETag           string                        `json:"etag"`
	SameOriginURL  string                        `json:"sameOriginUrl"`
	ExpiresAt      time.Time                     `json:"expiresAt"`
	SourceEpoch    domain.RationalTime           `json:"sourceEpoch"`
	Video          *SourcePreviewTrackTiming     `json:"video,omitempty"`
	Audio          *SourcePreviewTrackTiming     `json:"audio,omitempty"`
}

type MediaLeaseResult struct {
	Status        application.MediaPreparationStatus `json:"status" enum:"ready,preparing,failed"`
	Purpose       application.MediaLeasePurpose      `json:"purpose" enum:"source-preview"`
	ProjectID     domain.ProjectID                   `json:"projectId"`
	AssetID       domain.AssetID                     `json:"assetId"`
	AssetRevision domain.Revision                    `json:"assetRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Fingerprint   domain.Digest                      `json:"fingerprint" format:"sha256-digest"`
	VideoStreamID *domain.SourceStreamID             `json:"videoStreamId,omitempty"`
	AudioStreamID *domain.SourceStreamID             `json:"audioStreamId,omitempty"`
	Job           domain.MediaJobSummary             `json:"job"`
	Stage         *application.MediaPreparationStage `json:"stage,omitempty" enum:"proxy,integrity,render"`
	Diagnostics   []application.MediaDiagnostic      `json:"diagnostics" maxItems:"32" nullable:"false"`
	Lease         *MediaLease                        `json:"lease,omitempty"`
}

type sourceProxyMediaOpener interface {
	OpenSourceProxyMedia(
		context.Context,
		domain.ProjectID,
		domain.AssetID,
		domain.ArtifactID,
	) (*os.File, application.SourceProxyArtifactManifest, error)
	OpenSourceProxyVideoTimeMap(
		context.Context,
		domain.ProjectID,
		domain.AssetID,
		domain.ArtifactID,
	) (*os.File, application.SourceProxyArtifactManifest, error)
	IsSourceProxyMediaVerificationCurrent(
		context.Context,
		domain.ProjectID,
		domain.AssetID,
		domain.ArtifactID,
	) (bool, error)
}

type mediaLeaseRecord struct {
	resourceID     domain.ResourceID
	binding        uiLeaseBinding
	projectID      domain.ProjectID
	assetID        domain.AssetID
	assetRevision  domain.Revision
	artifactID     domain.ArtifactID
	artifactDigest domain.Digest
	media          application.SourceProxyArtifactFile
	manifest       application.SourceProxyArtifactManifest
	expiresAt      time.Time
}

type sourceProxyVerificationState string

const (
	sourceProxyVerifying sourceProxyVerificationState = "verifying"
	sourceProxyVerified  sourceProxyVerificationState = "verified"
	sourceProxyRejected  sourceProxyVerificationState = "rejected"
	sourceProxyErrored   sourceProxyVerificationState = "errored"
)

type sourceProxyVerification struct {
	state     sourceProxyVerificationState
	manifest  application.SourceProxyArtifactManifest
	err       error
	updatedAt time.Time
}

type MediaLeaseService struct {
	mu            sync.Mutex
	viewer        *application.ViewerMedia
	opener        sourceProxyMediaOpener
	identities    application.IdentityGenerator
	clock         application.Clock
	random        io.Reader
	leases        map[string]mediaLeaseRecord
	verifications map[string]sourceProxyVerification
}

func NewMediaLeaseService(
	viewer *application.ViewerMedia,
	opener sourceProxyMediaOpener,
	identities application.IdentityGenerator,
	clock application.Clock,
	random io.Reader,
) (*MediaLeaseService, error) {
	if viewer == nil || opener == nil || identities == nil || clock == nil || random == nil {
		return nil, fmt.Errorf("media lease dependencies are required")
	}
	return &MediaLeaseService{
		viewer: viewer, opener: opener, identities: identities, clock: clock, random: random,
		leases: make(map[string]mediaLeaseRecord), verifications: make(map[string]sourceProxyVerification),
	}, nil
}

func (service *MediaLeaseService) CreateSourcePreview(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	request MediaLeaseRequest,
) (MediaLeaseResult, error) {
	binding, err := uiSessionBindingFromContext(ctx)
	if err != nil {
		return MediaLeaseResult{}, err
	}
	preparation, err := service.viewer.PrepareSourcePreview(ctx, projectID, assetID, application.SourcePreviewSelectionInput{
		AssetRevision: request.AssetRevision, Fingerprint: request.Fingerprint,
		VideoStreamID: request.VideoStreamID, AudioStreamID: request.AudioStreamID,
	}, request.Purpose)
	if err != nil {
		return MediaLeaseResult{}, err
	}
	result := MediaLeaseResult{
		Status: preparation.Status, Purpose: preparation.Purpose,
		ProjectID: preparation.ProjectID, AssetID: preparation.AssetID,
		AssetRevision: preparation.AssetRevision, Fingerprint: preparation.Fingerprint,
		VideoStreamID: preparation.VideoStreamID, AudioStreamID: preparation.AudioStreamID, Job: preparation.Job,
		Stage: preparation.Stage, Diagnostics: preparation.Diagnostics,
	}
	if preparation.Status != application.MediaPreparationReady {
		return result, nil
	}
	if preparation.Artifact == nil {
		return MediaLeaseResult{}, ErrMediaLeaseInvalid
	}
	verified, manifest, rejected, err := service.ensureSourceProxyVerified(
		ctx, projectID, assetID, preparation.Job.ID, *preparation.Artifact,
	)
	if err != nil {
		return MediaLeaseResult{}, err
	}
	if !verified {
		result.Status = application.MediaPreparationPreparing
		stage := application.MediaPreparationIntegrity
		result.Stage = &stage
		if rejected {
			result.Diagnostics = append(result.Diagnostics, application.MediaDiagnostic{
				Code:        application.MediaDiagnosticProxyIntegrityRejected,
				Severity:    application.MediaDiagnosticBlocking,
				SubjectKind: application.MediaDiagnosticArtifact,
				SubjectID:   preparation.Artifact.ID.String(),
				Recovery:    application.MediaRecoveryAutomaticRetry,
			})
		}
		return result, nil
	}
	if manifest.AssetID != assetID || manifest.Fingerprint != preparation.Fingerprint ||
		!matchingPreviewManifestStreams(preparation, manifest) {
		return MediaLeaseResult{}, ErrMediaLeaseInvalid
	}
	media := manifest.Media
	now := service.clock.Now().UTC()
	expiresAt := now.Add(MediaLeaseTTL)
	if binding.expiresAt.Before(expiresAt) {
		expiresAt = binding.expiresAt
	}
	if !now.Before(expiresAt) {
		return MediaLeaseResult{}, ErrMediaLeaseExpired
	}
	identityValue, err := service.identities.NewID(ctx, now)
	if err != nil {
		return MediaLeaseResult{}, err
	}
	resourceID, err := domain.ParseResourceID(identityValue)
	if err != nil {
		return MediaLeaseResult{}, err
	}
	token, err := randomToken(service.random, "oc_media_")
	if err != nil {
		return MediaLeaseResult{}, err
	}
	record := mediaLeaseRecord{
		resourceID: resourceID, binding: binding.leaseBinding(),
		projectID: projectID, assetID: assetID, assetRevision: preparation.AssetRevision,
		artifactID:     preparation.Artifact.ID,
		artifactDigest: preparation.Artifact.ContentDigest, media: media, manifest: manifest, expiresAt: expiresAt,
	}
	service.mu.Lock()
	service.cleanupLocked(now)
	if len(service.leases) >= mediaLeaseLimit {
		service.mu.Unlock()
		return MediaLeaseResult{}, ErrUIRateLimited
	}
	service.leases[tokenHash(token)] = record
	service.mu.Unlock()
	etag := strongMediaETag(media.SHA256)
	result.Lease = &MediaLease{
		Schema: MediaLeaseSchema, ResourceID: resourceID, Purpose: request.Purpose,
		ProjectID: projectID, AssetID: assetID, AssetRevision: preparation.AssetRevision,
		Fingerprint: preparation.Fingerprint,
		ArtifactID:  preparation.Artifact.ID, ArtifactDigest: preparation.Artifact.ContentDigest,
		MimeType: media.MimeType, ByteLength: media.ByteSize, ETag: etag,
		SameOriginURL: "/api/v1/media/content/" + token, ExpiresAt: expiresAt,
		SourceEpoch: manifest.SourceEpoch,
	}
	if manifest.Video != nil {
		result.Lease.Video = sourcePreviewVideoTiming(*manifest.Video)
	}
	if manifest.Audio != nil {
		result.Lease.Audio = sourcePreviewAudioTiming(*manifest.Audio)
	}
	return result, nil
}

func matchingPreviewManifestStreams(
	preparation application.SourcePreviewPreparation,
	manifest application.SourceProxyArtifactManifest,
) bool {
	if (preparation.VideoStreamID == nil) != (manifest.Video == nil) ||
		(preparation.AudioStreamID == nil) != (manifest.Audio == nil) {
		return false
	}
	if manifest.Video != nil && manifest.Video.Source.ID != *preparation.VideoStreamID {
		return false
	}
	if manifest.Audio != nil && manifest.Audio.Source.ID != *preparation.AudioStreamID {
		return false
	}
	return true
}

func sourcePreviewVideoTiming(track application.SourceProxyVideoTrack) *SourcePreviewTrackTiming {
	return sourcePreviewTrackTiming(
		track.Source,
		track.SourceStartTime,
		track.ProxyStartTime,
		track.TimeBase,
	)
}

func sourcePreviewAudioTiming(track application.SourceProxyAudioTrack) *SourcePreviewTrackTiming {
	return sourcePreviewTrackTiming(
		track.Source,
		track.SourceStartTime,
		track.ProxyStartTime,
		track.TimeBase,
	)
}

func sourcePreviewTrackTiming(
	stream domain.SourceStream,
	sourceStartTime domain.RationalTime,
	proxyStartTime domain.RationalTime,
	proxyTimeBase domain.RationalTime,
) *SourcePreviewTrackTiming {
	coverageStart, _ := domain.NewRationalTime(0, 1)
	if stream.Descriptor.StartTime != nil {
		coverageStart = *stream.Descriptor.StartTime
	}
	var coverageDuration *domain.RationalTime
	if stream.Descriptor.Duration != nil {
		duration := *stream.Descriptor.Duration
		coverageDuration = &duration
	}
	return &SourcePreviewTrackTiming{
		SourceStreamID: stream.ID, CoverageStart: coverageStart, CoverageDuration: coverageDuration,
		SourceStartTime: sourceStartTime, ProxyStartTime: proxyStartTime,
		SourceTimeBase: stream.Descriptor.TimeBase, ProxyTimeBase: proxyTimeBase,
	}
}

func (service *MediaLeaseService) ensureSourceProxyVerified(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	jobID domain.MediaJobID,
	artifact domain.ArtifactSummary,
) (bool, application.SourceProxyArtifactManifest, bool, error) {
	key := jobID.String() + "\x00" + artifact.ID.String() + "\x00" + artifact.ContentDigest.String()
	now := service.clock.Now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	verification, exists := service.verifications[key]
	if exists {
		service.mu.Unlock()
		switch verification.state {
		case sourceProxyVerified:
			current, err := service.opener.IsSourceProxyMediaVerificationCurrent(
				ctx, projectID, assetID, artifact.ID,
			)
			if err != nil {
				return false, application.SourceProxyArtifactManifest{}, false, err
			}
			if current {
				return true, verification.manifest, false, nil
			}
			service.mu.Lock()
			if latest, found := service.verifications[key]; found && latest.state == sourceProxyVerified {
				delete(service.verifications, key)
			}
			service.mu.Unlock()
			return service.ensureSourceProxyVerified(ctx, projectID, assetID, jobID, artifact)
		case sourceProxyVerifying:
			return false, application.SourceProxyArtifactManifest{}, false, nil
		case sourceProxyRejected:
			return false, application.SourceProxyArtifactManifest{}, true, nil
		case sourceProxyErrored:
			return false, application.SourceProxyArtifactManifest{}, false, verification.err
		default:
			return false, application.SourceProxyArtifactManifest{}, false, ErrMediaLeaseInvalid
		}
	}
	if len(service.verifications) >= mediaLeaseLimit {
		service.mu.Unlock()
		return false, application.SourceProxyArtifactManifest{}, false, ErrUIRateLimited
	}
	service.verifications[key] = sourceProxyVerification{state: sourceProxyVerifying, updatedAt: now}
	service.mu.Unlock()
	verificationContext := context.WithoutCancel(ctx)
	go service.verifySourceProxy(
		verificationContext, key, projectID, assetID, jobID, artifact.ID,
	)
	return false, application.SourceProxyArtifactManifest{}, false, nil
}

func (service *MediaLeaseService) verifySourceProxy(
	ctx context.Context,
	key string,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	jobID domain.MediaJobID,
	artifactID domain.ArtifactID,
) {
	file, manifest, err := service.opener.OpenSourceProxyMedia(ctx, projectID, assetID, artifactID)
	if err == nil {
		err = file.Close()
	}
	state := sourceProxyVerified
	if err != nil {
		state = sourceProxyErrored
		if errors.Is(err, application.ErrSourceProxyIntegrity) {
			now := service.clock.Now().UTC()
			retryValue, retryErr := service.identities.NewID(ctx, now)
			eventValue, eventErr := service.identities.NewID(ctx, now)
			retryID, retryParseErr := domain.ParseMediaJobID(retryValue)
			eventID, eventParseErr := domain.ParseActivityEventID(eventValue)
			if retryErr == nil && eventErr == nil && retryParseErr == nil && eventParseErr == nil {
				_, retryErr = service.viewer.RejectSourcePreviewArtifact(ctx, application.RejectSourceProxyArtifactRecord{
					ProjectID: projectID, AssetID: assetID, ArtifactID: artifactID, JobID: jobID,
					RetryJobID: retryID, EventID: eventID,
					Code: application.MediaDiagnosticProxyIntegrityRejected, RejectedAt: now,
				})
			}
			if retryErr == nil && eventErr == nil && retryParseErr == nil && eventParseErr == nil {
				state = sourceProxyRejected
				err = nil
			} else if retryErr != nil {
				err = retryErr
			} else if eventErr != nil {
				err = eventErr
			} else if retryParseErr != nil {
				err = retryParseErr
			} else {
				err = eventParseErr
			}
		}
	}
	service.mu.Lock()
	service.verifications[key] = sourceProxyVerification{
		state: state, manifest: manifest, err: err, updatedAt: service.clock.Now().UTC(),
	}
	service.mu.Unlock()
}

func (service *MediaLeaseService) ServeContent(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	token string,
) error {
	binding, err := uiSessionBindingFromContext(ctx)
	if err != nil || token == "" {
		return ErrMediaLeaseInvalid
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	record, exists := service.leases[tokenHash(token)]
	service.mu.Unlock()
	if !exists || !record.binding.matches(binding) {
		return ErrMediaLeaseInvalid
	}
	if !now.Before(record.expiresAt) {
		return ErrMediaLeaseExpired
	}
	file, manifest, err := service.opener.OpenSourceProxyMedia(
		ctx, record.projectID, record.assetID, record.artifactID,
	)
	if err != nil {
		return err
	}
	defer file.Close()
	media := manifest.Media
	if media != record.media {
		return ErrMediaLeaseInvalid
	}
	start, length, partial, err := resolveMediaRange(r.Header.Values("Range"), media.ByteSize.Value())
	if err != nil {
		w.Header().Set("Content-Range", "bytes */"+media.ByteSize.String())
		return err
	}
	if ifRange := r.Header.Get("If-Range"); partial && ifRange != "" && ifRange != strongMediaETag(media.SHA256) {
		start, length, partial = 0, media.ByteSize.Value(), false
	}
	setMediaHeaders(w.Header(), media, record.expiresAt)
	if !partial && r.Header.Get("If-None-Match") == strongMediaETag(media.SHA256) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, media.ByteSize.Value()))
		w.Header().Set("Content-Length", strconv.FormatUint(length, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", media.ByteSize.String())
		w.WriteHeader(http.StatusOK)
	}
	if r.Method == http.MethodHead {
		return nil
	}
	if _, err := file.Seek(int64(start), io.SeekStart); err != nil {
		return err
	}
	_, err = io.CopyN(w, file, int64(length))
	return err
}

func (service *MediaLeaseService) cleanupLocked(now time.Time) {
	for token, lease := range service.leases {
		if !now.Before(lease.expiresAt) {
			delete(service.leases, token)
		}
	}
	for key, verification := range service.verifications {
		if verification.state != sourceProxyVerifying && now.Sub(verification.updatedAt) >= MediaLeaseTTL {
			delete(service.verifications, key)
		}
	}
}

func resolveMediaRange(values []string, size uint64) (uint64, uint64, bool, error) {
	if size == 0 || len(values) > 1 {
		return 0, 0, false, ErrMediaRangeInvalid
	}
	if len(values) == 0 || values[0] == "" {
		return 0, size, false, nil
	}
	value := values[0]
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, false, ErrMediaRangeInvalid
	}
	startText, endText, found := strings.Cut(strings.TrimPrefix(value, "bytes="), "-")
	if !found || (startText == "" && endText == "") {
		return 0, 0, false, ErrMediaRangeInvalid
	}
	if startText == "" {
		suffix, err := strconv.ParseUint(endText, 10, 64)
		if err != nil || suffix == 0 {
			return 0, 0, false, ErrMediaRangeInvalid
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, suffix, true, nil
	}
	start, err := strconv.ParseUint(startText, 10, 64)
	if err != nil || start >= size {
		return 0, 0, false, ErrMediaRangeInvalid
	}
	end := size - 1
	if endText != "" {
		end, err = strconv.ParseUint(endText, 10, 64)
		if err != nil || end < start {
			return 0, 0, false, ErrMediaRangeInvalid
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end - start + 1, true, nil
}

func setMediaHeaders(header http.Header, media application.SourceProxyArtifactFile, expiresAt time.Time) {
	header.Set("Accept-Ranges", "bytes")
	header.Set("Content-Type", media.MimeType)
	header.Set("ETag", strongMediaETag(media.SHA256))
	header.Set("Cache-Control", "private, no-store, max-age=0")
	header.Set("Expires", expiresAt.UTC().Format(http.TimeFormat))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
}

func strongMediaETag(digest domain.Digest) string {
	return `"sha256-` + strings.TrimPrefix(digest.String(), "sha256:") + `"`
}
