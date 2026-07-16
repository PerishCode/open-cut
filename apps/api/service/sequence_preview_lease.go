package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type SequencePreviewLeaseRequest struct {
	Purpose                  application.MediaLeasePurpose `json:"purpose" enum:"sequence-preview"`
	Operation                SequencePreviewLeaseOperation `json:"operation" enum:"prepare,continue,retry"`
	ExpectedSequenceRevision domain.Revision               `json:"expectedSequenceRevision"`
	Continuation             *SequencePreviewContinuation  `json:"continuation,omitempty"`
}

type SequencePreviewLeaseOperation string

const (
	SequencePreviewPrepare  SequencePreviewLeaseOperation = "prepare"
	SequencePreviewContinue SequencePreviewLeaseOperation = "continue"
	SequencePreviewRetry    SequencePreviewLeaseOperation = "retry"
)

type SequencePreviewContinuation struct {
	JobID            domain.WorkJobID `json:"jobId"`
	RenderPlanDigest *domain.Digest   `json:"renderPlanDigest,omitempty"`
}

type SequencePreviewJobResult struct {
	ID                  domain.WorkJobID    `json:"id"`
	Kind                domain.WorkJobKind  `json:"kind" enum:"sequence-preview"`
	State               domain.WorkJobState `json:"state" enum:"blocked,queued,running,succeeded,failed,cancelled"`
	ProgressBasisPoints uint16              `json:"progressBasisPoints" minimum:"0" maximum:"10000"`
	TerminalErrorCode   *string             `json:"terminalErrorCode,omitempty" maxLength:"256"`
	RenderPlanDigest    *domain.Digest      `json:"renderPlanDigest,omitempty"`
	ResultArtifactID    *domain.ArtifactID  `json:"resultArtifactId,omitempty"`
	CreatedAt           time.Time           `json:"createdAt"`
	UpdatedAt           time.Time           `json:"updatedAt"`
}

type SequencePreviewLease struct {
	Schema           string                           `json:"schema" enum:"open-cut/media-lease/v1"`
	ResourceID       domain.ResourceID                `json:"resourceId" format:"uuid"`
	Purpose          application.MediaLeasePurpose    `json:"purpose" enum:"sequence-preview"`
	ProjectID        domain.ProjectID                 `json:"projectId"`
	SequenceID       domain.SequenceID                `json:"sequenceId"`
	SequenceRevision domain.Revision                  `json:"sequenceRevision"`
	RenderPlanDigest domain.Digest                    `json:"renderPlanDigest"`
	ArtifactID       domain.ArtifactID                `json:"artifactId"`
	ArtifactDigest   domain.Digest                    `json:"artifactDigest"`
	Facts            domain.SequencePreviewMediaFacts `json:"facts"`
	MimeType         string                           `json:"mimeType" enum:"video/webm"`
	ByteLength       domain.UInt64                    `json:"byteLength"`
	ETag             string                           `json:"etag"`
	SameOriginURL    string                           `json:"sameOriginUrl"`
	ExpiresAt        time.Time                        `json:"expiresAt"`
}

type SequencePreviewLeaseResult struct {
	Status           application.SequencePreviewPreparationStatus `json:"status" enum:"empty,ready,preparing,failed"`
	Purpose          application.MediaLeasePurpose                `json:"purpose" enum:"sequence-preview"`
	ProjectID        domain.ProjectID                             `json:"projectId"`
	SequenceID       domain.SequenceID                            `json:"sequenceId"`
	SequenceRevision domain.Revision                              `json:"sequenceRevision"`
	Job              *SequencePreviewJobResult                    `json:"job,omitempty"`
	Continuation     *SequencePreviewContinuation                 `json:"continuation,omitempty"`
	Stage            *application.MediaPreparationStage           `json:"stage,omitempty" enum:"render,integrity"`
	Diagnostics      []application.MediaDiagnostic                `json:"diagnostics" maxItems:"32" nullable:"false"`
	Lease            *SequencePreviewLease                        `json:"lease,omitempty"`
}

type sequencePreviewMediaOpener interface {
	OpenSequencePreviewMedia(
		context.Context,
		domain.ProjectID,
		domain.SequenceID,
		domain.Revision,
		domain.Digest,
		domain.ArtifactID,
	) (*os.File, application.SequencePreviewArtifactFile, error)
	IsSequencePreviewMediaVerificationCurrent(
		context.Context,
		domain.ProjectID,
		domain.SequenceID,
		domain.Revision,
		domain.Digest,
		domain.ArtifactID,
	) (bool, error)
}

type sequencePreviewLeaseRecord struct {
	resourceID       domain.ResourceID
	sessionHash      string
	apiInstance      string
	projectID        domain.ProjectID
	sequenceID       domain.SequenceID
	sequenceRevision domain.Revision
	planDigest       domain.Digest
	jobID            domain.WorkJobID
	artifactID       domain.ArtifactID
	artifactDigest   domain.Digest
	media            application.SequencePreviewArtifactFile
	expiresAt        time.Time
}

type sequencePreviewVerification struct {
	state       sourceProxyVerificationState
	media       application.SequencePreviewArtifactFile
	replacement *application.SequencePreviewJobProjection
	err         error
	updatedAt   time.Time
}

type SequencePreviewLeaseService struct {
	mu            sync.Mutex
	previews      *application.SequencePreviews
	opener        sequencePreviewMediaOpener
	identities    application.IdentityGenerator
	clock         application.Clock
	random        io.Reader
	leases        map[string]sequencePreviewLeaseRecord
	verifications map[string]sequencePreviewVerification
}

func NewSequencePreviewLeaseService(
	previews *application.SequencePreviews,
	opener sequencePreviewMediaOpener,
	identities application.IdentityGenerator,
	clock application.Clock,
	random io.Reader,
) (*SequencePreviewLeaseService, error) {
	if previews == nil || opener == nil || identities == nil || clock == nil || random == nil {
		return nil, fmt.Errorf("sequence preview lease dependencies are required")
	}
	return &SequencePreviewLeaseService{
		previews: previews, opener: opener, identities: identities, clock: clock, random: random,
		leases:        make(map[string]sequencePreviewLeaseRecord),
		verifications: make(map[string]sequencePreviewVerification),
	}, nil
}

func (service *SequencePreviewLeaseService) Create(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	request SequencePreviewLeaseRequest,
) (SequencePreviewLeaseResult, error) {
	binding, err := uiSessionBindingFromContext(ctx)
	if err != nil {
		return SequencePreviewLeaseResult{}, err
	}
	if request.Purpose != application.MediaLeaseSequencePreview ||
		request.ExpectedSequenceRevision.Value() == 0 {
		return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
	}
	var preparation application.SequencePreviewPreparation
	switch request.Operation {
	case SequencePreviewPrepare:
		if request.Continuation != nil {
			return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
		}
		preparation, err = service.previews.Prepare(
			ctx, projectID, sequenceID, request.ExpectedSequenceRevision,
		)
	case SequencePreviewContinue, SequencePreviewRetry:
		if request.Continuation == nil {
			return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
		}
		if request.Continuation.JobID.IsZero() ||
			(request.Continuation.RenderPlanDigest != nil && *request.Continuation.RenderPlanDigest == "") {
			return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
		}
		if request.Operation == SequencePreviewContinue {
			preparation, err = service.previews.Continue(
				ctx, projectID, sequenceID, request.ExpectedSequenceRevision,
				request.Continuation.JobID, request.Continuation.RenderPlanDigest,
			)
		} else {
			preparation, err = service.previews.Retry(
				ctx, projectID, sequenceID, request.ExpectedSequenceRevision,
				request.Continuation.JobID, request.Continuation.RenderPlanDigest,
			)
		}
	default:
		return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
	}
	if err != nil {
		return SequencePreviewLeaseResult{}, err
	}
	result := SequencePreviewLeaseResult{
		Status: preparation.Status, Purpose: request.Purpose,
		ProjectID: projectID, SequenceID: sequenceID,
		SequenceRevision: request.ExpectedSequenceRevision,
		Diagnostics:      []application.MediaDiagnostic{},
	}
	if preparation.Status == application.SequencePreviewEmpty {
		if preparation.Job != nil {
			return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
		}
		return result, nil
	}
	if preparation.Job == nil {
		return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
	}
	job := sequencePreviewJobResult(*preparation.Job)
	result.Job = &job
	result.Continuation = sequencePreviewContinuation(*preparation.Job)
	switch preparation.Status {
	case application.SequencePreviewPreparing:
		stage := application.MediaPreparationRender
		result.Stage = &stage
		return result, nil
	case application.SequencePreviewFailed:
		stage := application.MediaPreparationRender
		result.Stage = &stage
		code := application.MediaDiagnosticSequenceJobFailed
		if preparation.Job.State == domain.MediaJobCancelled {
			code = application.MediaDiagnosticSequenceJobCancelled
		}
		result.Diagnostics = append(result.Diagnostics, application.MediaDiagnostic{
			Code: code, Severity: application.MediaDiagnosticBlocking,
			SubjectKind: application.MediaDiagnosticWorkJob,
			SubjectID:   preparation.Job.ID.String(),
			Recovery:    application.SequencePreviewRecoveryAction(*preparation.Job),
		})
		return result, nil
	case application.SequencePreviewReady:
	default:
		return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
	}
	artifact := preparation.Job.Artifact
	planDigest := preparation.Job.RenderPlanDigest
	if artifact == nil || planDigest == nil || artifact.State != domain.SequencePreviewArtifactReady ||
		artifact.ProjectID != projectID || artifact.SequenceID != sequenceID ||
		artifact.SequenceRevision != request.ExpectedSequenceRevision ||
		artifact.RenderPlanDigest != *planDigest || artifact.Profile != domain.SequencePreviewProfileV1 {
		return SequencePreviewLeaseResult{}, ErrMediaLeaseInvalid
	}
	verified, media, replacement, err := service.ensureVerified(
		ctx, projectID, sequenceID, request.ExpectedSequenceRevision,
		*preparation.Job, *planDigest, *artifact,
	)
	if err != nil {
		return SequencePreviewLeaseResult{}, err
	}
	if !verified {
		result.Status = application.SequencePreviewPreparing
		stage := application.MediaPreparationIntegrity
		result.Stage = &stage
		if replacement != nil {
			replacementJob := sequencePreviewJobResult(*replacement)
			result.Job = &replacementJob
			result.Continuation = sequencePreviewContinuation(*replacement)
			result.Diagnostics = append(result.Diagnostics, application.MediaDiagnostic{
				Code:        application.MediaDiagnosticSequenceIntegrityRejected,
				Severity:    application.MediaDiagnosticBlocking,
				SubjectKind: application.MediaDiagnosticArtifact,
				SubjectID:   artifact.ID.String(),
				Recovery:    application.MediaRecoveryAutomaticRetry,
			})
		}
		return result, nil
	}
	now := service.clock.Now().UTC()
	expiresAt := now.Add(MediaLeaseTTL)
	if binding.expiresAt.Before(expiresAt) {
		expiresAt = binding.expiresAt
	}
	if !now.Before(expiresAt) {
		return SequencePreviewLeaseResult{}, ErrMediaLeaseExpired
	}
	identityValue, err := service.identities.NewID(ctx, now)
	if err != nil {
		return SequencePreviewLeaseResult{}, err
	}
	resourceID, err := domain.ParseResourceID(identityValue)
	if err != nil {
		return SequencePreviewLeaseResult{}, err
	}
	token, err := randomToken(service.random, "oc_sequence_")
	if err != nil {
		return SequencePreviewLeaseResult{}, err
	}
	record := sequencePreviewLeaseRecord{
		resourceID: resourceID, sessionHash: binding.sessionHash, apiInstance: binding.apiInstance,
		projectID: projectID, sequenceID: sequenceID, sequenceRevision: request.ExpectedSequenceRevision,
		planDigest: *planDigest, jobID: preparation.Job.ID,
		artifactID: artifact.ID, artifactDigest: artifact.ContentDigest,
		media: media, expiresAt: expiresAt,
	}
	service.mu.Lock()
	service.cleanupLocked(now)
	if len(service.leases) >= mediaLeaseLimit {
		service.mu.Unlock()
		return SequencePreviewLeaseResult{}, ErrUIRateLimited
	}
	service.leases[tokenHash(token)] = record
	service.mu.Unlock()
	result.Lease = &SequencePreviewLease{
		Schema: MediaLeaseSchema, ResourceID: resourceID, Purpose: request.Purpose,
		ProjectID: projectID, SequenceID: sequenceID,
		SequenceRevision: request.ExpectedSequenceRevision, RenderPlanDigest: *planDigest,
		ArtifactID: artifact.ID, ArtifactDigest: artifact.ContentDigest, Facts: artifact.Facts,
		MimeType: media.MimeType, ByteLength: media.ByteSize, ETag: strongMediaETag(media.SHA256),
		SameOriginURL: "/api/v1/media/content/" + token, ExpiresAt: expiresAt,
	}
	return result, nil
}

func sequencePreviewJobResult(job application.SequencePreviewJobProjection) SequencePreviewJobResult {
	result := SequencePreviewJobResult{
		ID: job.ID, Kind: domain.WorkJobSequencePreview, State: job.State,
		ProgressBasisPoints: job.ProgressBasisPoints, TerminalErrorCode: job.TerminalErrorCode,
		RenderPlanDigest: job.RenderPlanDigest, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
	if job.Artifact != nil {
		artifactID := job.Artifact.ID
		result.ResultArtifactID = &artifactID
	}
	return result
}

func sequencePreviewContinuation(
	job application.SequencePreviewJobProjection,
) *SequencePreviewContinuation {
	return &SequencePreviewContinuation{JobID: job.ID, RenderPlanDigest: job.RenderPlanDigest}
}

func (service *SequencePreviewLeaseService) ensureVerified(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	job application.SequencePreviewJobProjection,
	planDigest domain.Digest,
	artifact domain.SequencePreviewArtifactSummary,
) (bool, application.SequencePreviewArtifactFile, *application.SequencePreviewJobProjection, error) {
	key := job.ID.String() + "\x00" + artifact.ID.String() + "\x00" + artifact.ContentDigest.String()
	now := service.clock.Now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	verification, exists := service.verifications[key]
	if exists {
		service.mu.Unlock()
		switch verification.state {
		case sourceProxyVerified:
			current, err := service.opener.IsSequencePreviewMediaVerificationCurrent(
				ctx, projectID, sequenceID, sequenceRevision, planDigest, artifact.ID,
			)
			if err != nil {
				return false, application.SequencePreviewArtifactFile{}, nil, err
			}
			if current {
				return true, verification.media, nil, nil
			}
			service.mu.Lock()
			if latest, found := service.verifications[key]; found && latest.state == sourceProxyVerified {
				delete(service.verifications, key)
			}
			service.mu.Unlock()
			return service.ensureVerified(
				ctx, projectID, sequenceID, sequenceRevision, job, planDigest, artifact,
			)
		case sourceProxyVerifying:
			return false, application.SequencePreviewArtifactFile{}, nil, nil
		case sourceProxyRejected:
			return false, application.SequencePreviewArtifactFile{}, verification.replacement, nil
		case sourceProxyErrored:
			return false, application.SequencePreviewArtifactFile{}, nil, verification.err
		default:
			return false, application.SequencePreviewArtifactFile{}, nil, ErrMediaLeaseInvalid
		}
	}
	if len(service.verifications) >= mediaLeaseLimit {
		service.mu.Unlock()
		return false, application.SequencePreviewArtifactFile{}, nil, ErrUIRateLimited
	}
	service.verifications[key] = sequencePreviewVerification{state: sourceProxyVerifying, updatedAt: now}
	service.mu.Unlock()
	go service.verify(
		context.WithoutCancel(ctx), key, projectID, sequenceID, sequenceRevision,
		job.ID, planDigest, artifact.ID,
	)
	return false, application.SequencePreviewArtifactFile{}, nil, nil
}

func (service *SequencePreviewLeaseService) verify(
	ctx context.Context,
	key string,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
	planDigest domain.Digest,
	artifactID domain.ArtifactID,
) {
	file, media, err := service.opener.OpenSequencePreviewMedia(
		ctx, projectID, sequenceID, sequenceRevision, planDigest, artifactID,
	)
	if err == nil {
		err = file.Close()
	}
	state := sourceProxyVerified
	var replacement *application.SequencePreviewJobProjection
	if err != nil {
		state = sourceProxyErrored
		if errors.Is(err, application.ErrSequencePreviewIntegrity) {
			if retry, rejectErr := service.rejectArtifact(
				ctx, projectID, sequenceID, sequenceRevision, jobID, artifactID,
			); rejectErr == nil {
				state = sourceProxyRejected
				replacement = &retry
				err = nil
			} else {
				err = rejectErr
			}
		}
	}
	service.mu.Lock()
	service.verifications[key] = sequencePreviewVerification{
		state: state, media: media, replacement: replacement,
		err: err, updatedAt: service.clock.Now().UTC(),
	}
	service.mu.Unlock()
}

func (service *SequencePreviewLeaseService) rejectArtifact(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
	artifactID domain.ArtifactID,
) (application.SequencePreviewJobProjection, error) {
	now := service.clock.Now().UTC()
	retryValue, err := service.identities.NewID(ctx, now)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	eventValue, err := service.identities.NewID(ctx, now)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	retryID, err := domain.ParseWorkJobID(retryValue)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	eventID, err := domain.ParseActivityEventID(eventValue)
	if err != nil {
		return application.SequencePreviewJobProjection{}, err
	}
	retry, err := service.previews.RejectArtifact(ctx, application.RejectSequencePreviewArtifactRecord{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: sequenceRevision,
		ArtifactID: artifactID, JobID: jobID, RetryJobID: retryID, EventID: eventID,
		Code: application.MediaDiagnosticSequenceIntegrityRejected, RejectedAt: now,
	})
	return retry, err
}

func (service *SequencePreviewLeaseService) ServeContent(
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
	if !exists || record.sessionHash != binding.sessionHash || record.apiInstance != binding.apiInstance {
		return ErrMediaLeaseInvalid
	}
	if !now.Before(record.expiresAt) {
		return ErrMediaLeaseExpired
	}
	file, media, err := service.opener.OpenSequencePreviewMedia(
		ctx, record.projectID, record.sequenceID, record.sequenceRevision,
		record.planDigest, record.artifactID,
	)
	if err != nil {
		if errors.Is(err, application.ErrSequencePreviewIntegrity) {
			_, _ = service.rejectArtifact(
				context.WithoutCancel(ctx), record.projectID, record.sequenceID,
				record.sequenceRevision, record.jobID, record.artifactID,
			)
		}
		return err
	}
	defer file.Close()
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
	setSequencePreviewMediaHeaders(w.Header(), media, record.expiresAt)
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

func (service *SequencePreviewLeaseService) cleanupLocked(now time.Time) {
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

func setSequencePreviewMediaHeaders(
	header http.Header,
	media application.SequencePreviewArtifactFile,
	expiresAt time.Time,
) {
	header.Set("Accept-Ranges", "bytes")
	header.Set("Content-Type", media.MimeType)
	header.Set("ETag", strongMediaETag(media.SHA256))
	header.Set("Cache-Control", "private, no-store, max-age=0")
	header.Set("Expires", expiresAt.UTC().Format(http.TimeFormat))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
}
