package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"reflect"
	"regexp"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrNoMediaWork      = errors.New("no eligible media work")
	ErrMediaLeaseLost   = errors.New("media job attempt lease was lost")
	ErrMediaSourceRead  = errors.New("media source could not be read")
	ErrMediaSourceMoved = errors.New("media source observation changed")
)

var mediaFailureCode = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

type MediaJobClaim struct {
	JobID               domain.MediaJobID
	AttemptID           domain.JobAttemptID
	ProjectID           domain.ProjectID
	AssetID             domain.AssetID
	SourceGrantID       domain.SourceGrantID
	Kind                domain.MediaJobKind
	ExecutorVersion     string
	ExecutorTarget      string
	Generation          uint64
	LeaseOwner          string
	LeaseExpiresAt      time.Time
	ExpectedObservation domain.SourceObservation
	AcceptedFingerprint *domain.Digest
	ParametersDigest    domain.Digest
	ParametersJSON      []byte
	SourceStream        *domain.SourceStream
	SourceStreams       []domain.SourceStream
	TranscriptBinding   *domain.TranscriptBinding
	TranscriptNoAudio   bool
}

type MediaExecutorRegistration struct {
	Kind    domain.MediaJobKind
	Version string
	Target  string
}

type ClaimMediaJobInput struct {
	AttemptID     domain.JobAttemptID
	Executors     []MediaExecutorRegistration
	Resources     []ProductResourceRegistration
	OnlyJobID     *domain.WorkJobID
	LeaseOwner    string
	Now           time.Time
	LeaseDuration time.Duration
}

type CompleteMediaIdentification struct {
	Claim       MediaJobClaim
	Fingerprint domain.Digest
	Observation domain.SourceObservation
	EventID     domain.ActivityEventID
	CompletedAt time.Time
}

type CompleteMediaProbe struct {
	Claim       MediaJobClaim
	ArtifactID  domain.ArtifactID
	Facts       domain.MediaFacts
	EventID     domain.ActivityEventID
	CompletedAt time.Time
}

type CompleteMediaTranscript struct {
	Claim             MediaJobClaim
	Artifact          domain.TranscriptArtifact
	ArtifactCanonical []byte
	ContentDigest     domain.Digest
	ByteSize          domain.UInt64
	EventID           domain.ActivityEventID
	CompletedAt       time.Time
}

type CompleteMediaTranscriptNoAudio struct {
	Claim       MediaJobClaim
	EventID     domain.ActivityEventID
	CompletedAt time.Time
}

type ReblockMediaTranscriptResource struct {
	Claim           MediaJobClaim
	ResourceID      domain.ResourceID
	EventID         domain.ActivityEventID
	ResourceEventID domain.ActivityEventID
	ReblockedAt     time.Time
}

type FailMediaJobInput struct {
	Claim        MediaJobClaim
	Code         string
	Detail       string
	Availability *domain.AssetAvailability
	EventID      domain.ActivityEventID
	FailedAt     time.Time
}

type MediaWorkRepository interface {
	CompleteMediaIdentification(context.Context, CompleteMediaIdentification) error
	CompleteMediaProbe(context.Context, CompleteMediaProbe) error
	CompleteMediaFrameSet(context.Context, CompleteMediaFrameSet) error
	CompleteMediaProxy(context.Context, CompleteMediaProxy) error
	CompleteMediaRenderInput(context.Context, CompleteMediaRenderInput) error
	CompleteMediaTranscript(context.Context, CompleteMediaTranscript) error
	CompleteMediaTranscriptNoAudio(context.Context, CompleteMediaTranscriptNoAudio) error
	ReblockMediaTranscriptResource(context.Context, ReblockMediaTranscriptResource) error
	FailMediaJob(context.Context, FailMediaJobInput) error
}

type MediaIdentification struct {
	Fingerprint domain.Digest
	Observation domain.SourceObservation
}

type MediaProbe struct {
	Container        string
	ContainerAliases []string
	StartTime        *domain.RationalTime
	Duration         *domain.RationalTime
	BitRate          *domain.UInt64
	Streams          []domain.SourceStreamDescriptor
}

type MediaJobExecution struct {
	Identification    *MediaIdentification
	Probe             *MediaProbe
	FrameSet          *MediaFrameSetExecution
	Proxy             *MediaProxyExecution
	RenderInput       *MediaRenderInputExecution
	Transcript        *TranscriptRecognition
	TranscriptNoAudio bool
}

type TranscriptTokenRecognition struct {
	SourceRange           domain.TimeRange
	Text                  string
	ConfidenceBasisPoints *uint16
}

type TranscriptSegmentRecognition struct {
	SourceRange domain.TimeRange
	Text        string
	Tokens      []TranscriptTokenRecognition
}

type TranscriptRecognition struct {
	DetectedLanguage              string
	LanguageConfidenceBasisPoints *uint16
	Normalization                 domain.TranscriptNormalizationProof
	Segments                      []TranscriptSegmentRecognition
}

type MediaJobExecutor interface {
	Registration() MediaExecutorRegistration
	Execute(context.Context, MediaJobClaim) (MediaJobExecution, error)
}

type MediaExecutionError struct {
	Code         string
	Availability *domain.AssetAvailability
	Cause        error
}

func (failure MediaExecutionError) Error() string {
	if failure.Cause == nil {
		return failure.Code
	}
	return failure.Code + ": " + failure.Cause.Error()
}

func (failure MediaExecutionError) Unwrap() error { return failure.Cause }

type MediaResourceInvalidError struct {
	ResourceID domain.ResourceID
	Cause      error
}

func (failure MediaResourceInvalidError) Error() string {
	if failure.Cause == nil {
		return "bound product resource is invalid"
	}
	return "bound product resource is invalid: " + failure.Cause.Error()
}

func (failure MediaResourceInvalidError) Unwrap() error { return failure.Cause }

func NewMediaResourceInvalidError(resourceID domain.ResourceID, cause error) error {
	if resourceID.IsZero() {
		return ErrProductResourceInvalid
	}
	return MediaResourceInvalidError{ResourceID: resourceID, Cause: cause}
}

func NewMediaExecutionError(code string, cause error) error {
	if !mediaFailureCode.MatchString(code) {
		return fmt.Errorf("invalid media execution failure")
	}
	return MediaExecutionError{Code: code, Cause: cause}
}

func NewMediaSourceExecutionError(
	code string,
	availability domain.AssetAvailability,
	cause error,
) error {
	if !mediaFailureCode.MatchString(code) || !validFailureAvailability(availability) {
		return fmt.Errorf("invalid media source execution failure")
	}
	return MediaExecutionError{Code: code, Availability: &availability, Cause: cause}
}

type mediaWorkDispatcher struct {
	repository MediaWorkRepository
	executors  map[domain.MediaJobKind]MediaJobExecutor
	identities IdentityGenerator
	clock      Clock
}

func newMediaWorkDispatcher(
	repository MediaWorkRepository,
	executors []MediaJobExecutor,
	identities IdentityGenerator,
	clock Clock,
) (*mediaWorkDispatcher, []MediaExecutorRegistration, error) {
	if repository == nil || len(executors) == 0 || identities == nil || clock == nil {
		return nil, nil, fmt.Errorf("media work dependencies are invalid")
	}
	registry := make(map[domain.MediaJobKind]MediaJobExecutor, len(executors))
	claims := make([]MediaExecutorRegistration, 0, len(executors))
	for _, executor := range executors {
		if executor == nil {
			return nil, nil, fmt.Errorf("media work executor is invalid")
		}
		registration := executor.Registration()
		if (registration.Kind != domain.MediaJobIdentify && registration.Kind != domain.MediaJobProbe &&
			registration.Kind != domain.MediaJobFrameSet && registration.Kind != domain.MediaJobProxy &&
			registration.Kind != domain.MediaJobRenderInput &&
			registration.Kind != domain.MediaJobTranscript) || registration.Version == "" ||
			len(registration.Version) > 1024 || len(registration.Target) > 128 ||
			(registration.Kind == domain.MediaJobTranscript || registration.Kind == domain.MediaJobRenderInput) !=
				(registration.Target != "") {
			return nil, nil, fmt.Errorf("media work executor registration is invalid")
		}
		if _, duplicate := registry[registration.Kind]; duplicate {
			return nil, nil, fmt.Errorf("media work repeats an executor kind")
		}
		registry[registration.Kind] = executor
		claims = append(claims, registration)
	}
	return &mediaWorkDispatcher{
		repository: repository, executors: registry, identities: identities, clock: clock,
	}, claims, nil
}

func (scheduler *mediaWorkDispatcher) executeClaim(
	ctx context.Context,
	claim MediaJobClaim,
) (bool, error) {
	executor, exists := scheduler.executors[claim.Kind]
	if !exists || executor.Registration().Version != claim.ExecutorVersion ||
		executor.Registration().Target != claim.ExecutorTarget {
		return true, ErrMediaLeaseLost
	}
	execution, executionErr := executor.Execute(ctx, claim)
	if execution.Proxy != nil && execution.Proxy.Workspace != nil {
		defer execution.Proxy.Workspace.Release()
	}
	if execution.RenderInput != nil && execution.RenderInput.Workspace != nil {
		defer execution.RenderInput.Workspace.Release()
	}
	eventID, err := scheduler.newActivityEventID(ctx, scheduler.clock.Now().UTC())
	if err != nil {
		return true, err
	}
	if executionErr != nil {
		var resourceInvalid MediaResourceInvalidError
		if errors.As(executionErr, &resourceInvalid) {
			if claim.Kind != domain.MediaJobTranscript || claim.TranscriptBinding == nil ||
				claim.TranscriptBinding.ModelResourceID != resourceInvalid.ResourceID {
				return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
					Claim: claim, Code: "executor-output-invalid", EventID: eventID,
					FailedAt: scheduler.clock.Now().UTC(),
				})
			}
			resourceEventID, identityErr := scheduler.newActivityEventID(ctx, scheduler.clock.Now().UTC())
			if identityErr != nil {
				return true, identityErr
			}
			return true, scheduler.repository.ReblockMediaTranscriptResource(
				ctx, ReblockMediaTranscriptResource{
					Claim: claim, ResourceID: resourceInvalid.ResourceID,
					EventID: eventID, ResourceEventID: resourceEventID,
					ReblockedAt: scheduler.clock.Now().UTC(),
				},
			)
		}
		failure := classifyMediaExecutionError(executionErr)
		detail := ""
		if failure.Cause != nil {
			detail = BoundedDiagnosticDetail(failure.Cause.Error())
		}
		return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
			Claim: claim, Code: failure.Code, Detail: detail, Availability: failure.Availability,
			EventID: eventID, FailedAt: scheduler.clock.Now().UTC(),
		})
	}
	completedAt := scheduler.clock.Now().UTC()
	switch claim.Kind {
	case domain.MediaJobIdentify:
		if execution.Identification == nil || execution.Probe != nil || execution.FrameSet != nil ||
			execution.Proxy != nil || execution.RenderInput != nil || execution.Transcript != nil || execution.TranscriptNoAudio ||
			execution.Identification.Observation != claim.ExpectedObservation {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid",
				EventID: eventID, FailedAt: completedAt,
			})
		}
		return true, scheduler.repository.CompleteMediaIdentification(ctx, CompleteMediaIdentification{
			Claim: claim, Fingerprint: execution.Identification.Fingerprint,
			Observation: execution.Identification.Observation, EventID: eventID, CompletedAt: completedAt,
		})
	case domain.MediaJobProbe:
		if execution.Probe == nil || execution.Identification != nil || execution.FrameSet != nil ||
			execution.Proxy != nil || execution.RenderInput != nil || execution.Transcript != nil || execution.TranscriptNoAudio ||
			claim.AcceptedFingerprint == nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid",
				EventID: eventID, FailedAt: completedAt,
			})
		}
		artifactID, facts, buildErr := scheduler.materializeProbe(ctx, *execution.Probe, completedAt)
		if buildErr != nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid",
				EventID: eventID, FailedAt: completedAt,
			})
		}
		return true, scheduler.repository.CompleteMediaProbe(ctx, CompleteMediaProbe{
			Claim: claim, ArtifactID: artifactID, Facts: facts, EventID: eventID, CompletedAt: completedAt,
		})
	case domain.MediaJobFrameSet:
		if execution.FrameSet == nil || execution.Identification != nil || execution.Probe != nil ||
			execution.Proxy != nil || execution.RenderInput != nil || execution.Transcript != nil || execution.TranscriptNoAudio ||
			claim.AcceptedFingerprint == nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid", EventID: eventID, FailedAt: completedAt,
			})
		}
		publication, buildErr := scheduler.materializeFrameSet(ctx, claim, *execution.FrameSet, eventID, completedAt)
		if buildErr != nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid", EventID: eventID, FailedAt: completedAt,
			})
		}
		return true, scheduler.repository.CompleteMediaFrameSet(ctx, publication)
	case domain.MediaJobProxy:
		if execution.Proxy == nil || execution.Identification != nil || execution.Probe != nil ||
			execution.FrameSet != nil || execution.RenderInput != nil || execution.Transcript != nil || execution.TranscriptNoAudio ||
			claim.AcceptedFingerprint == nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid", EventID: eventID, FailedAt: completedAt,
			})
		}
		publication, buildErr := scheduler.materializeProxy(ctx, claim, *execution.Proxy, eventID, completedAt)
		if buildErr != nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid", EventID: eventID, FailedAt: completedAt,
			})
		}
		return true, scheduler.repository.CompleteMediaProxy(ctx, publication)
	case domain.MediaJobRenderInput:
		if execution.RenderInput == nil || execution.Identification != nil || execution.Probe != nil ||
			execution.FrameSet != nil || execution.Proxy != nil || execution.Transcript != nil ||
			execution.TranscriptNoAudio || claim.AcceptedFingerprint == nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid", EventID: eventID, FailedAt: completedAt,
			})
		}
		publication, buildErr := scheduler.materializeRenderInput(
			ctx, claim, *execution.RenderInput, eventID, completedAt,
		)
		if buildErr != nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid", EventID: eventID, FailedAt: completedAt,
			})
		}
		return true, scheduler.repository.CompleteMediaRenderInput(ctx, publication)
	case domain.MediaJobTranscript:
		if execution.Identification != nil || execution.Probe != nil ||
			execution.FrameSet != nil || execution.Proxy != nil || execution.RenderInput != nil ||
			claim.AcceptedFingerprint == nil ||
			(execution.Transcript != nil) == execution.TranscriptNoAudio ||
			claim.TranscriptNoAudio != execution.TranscriptNoAudio ||
			(claim.TranscriptNoAudio != (claim.TranscriptBinding == nil)) {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid", EventID: eventID, FailedAt: completedAt,
			})
		}
		if execution.TranscriptNoAudio {
			return true, scheduler.repository.CompleteMediaTranscriptNoAudio(
				ctx, CompleteMediaTranscriptNoAudio{Claim: claim, EventID: eventID, CompletedAt: completedAt},
			)
		}
		publication, buildErr := scheduler.materializeTranscript(ctx, claim, *execution.Transcript, eventID, completedAt)
		if buildErr != nil {
			return true, scheduler.repository.FailMediaJob(ctx, FailMediaJobInput{
				Claim: claim, Code: "executor-output-invalid", EventID: eventID, FailedAt: completedAt,
			})
		}
		return true, scheduler.repository.CompleteMediaTranscript(ctx, publication)
	default:
		return true, ErrMediaLeaseLost
	}
}

func (scheduler *mediaWorkDispatcher) materializeTranscript(
	ctx context.Context,
	claim MediaJobClaim,
	recognition TranscriptRecognition,
	eventID domain.ActivityEventID,
	at time.Time,
) (CompleteMediaTranscript, error) {
	if claim.TranscriptBinding == nil || claim.TranscriptBinding.Validate() != nil ||
		claim.AcceptedFingerprint == nil || claim.TranscriptBinding.Fingerprint != *claim.AcceptedFingerprint ||
		len(recognition.Segments) > domain.MaximumTranscriptSegments {
		return CompleteMediaTranscript{}, domain.ErrInvalidTranscript
	}
	artifactValue, err := scheduler.identities.NewID(ctx, at)
	if err != nil {
		return CompleteMediaTranscript{}, err
	}
	artifactID, err := domain.ParseArtifactID(artifactValue)
	if err != nil {
		return CompleteMediaTranscript{}, err
	}
	_, bindingDigest, err := domain.CanonicalDigest(
		"open-cut/transcript-binding", domain.TranscriptBindingSchema, *claim.TranscriptBinding,
	)
	if err != nil {
		return CompleteMediaTranscript{}, err
	}
	artifact := domain.TranscriptArtifact{
		Schema: domain.TranscriptArtifactSchema, ID: artifactID, ProjectID: claim.ProjectID,
		Binding: *claim.TranscriptBinding, BindingDigest: bindingDigest,
		DetectedLanguage:              recognition.DetectedLanguage,
		LanguageConfidenceBasisPoints: recognition.LanguageConfidenceBasisPoints,
		Normalization:                 recognition.Normalization,
		Segments:                      make([]domain.TranscriptSegment, len(recognition.Segments)),
	}
	for segmentIndex, segment := range recognition.Segments {
		segmentValue, idErr := scheduler.identities.NewID(ctx, at)
		if idErr != nil {
			return CompleteMediaTranscript{}, idErr
		}
		segmentID, idErr := domain.ParseTranscriptSegmentID(segmentValue)
		if idErr != nil {
			return CompleteMediaTranscript{}, idErr
		}
		materialized := domain.TranscriptSegment{
			ID: segmentID, Ordinal: uint32(segmentIndex), SourceRange: segment.SourceRange,
			Text: segment.Text, Tokens: make([]domain.TranscriptToken, len(segment.Tokens)),
		}
		for tokenIndex, token := range segment.Tokens {
			tokenValue, tokenErr := scheduler.identities.NewID(ctx, at)
			if tokenErr != nil {
				return CompleteMediaTranscript{}, tokenErr
			}
			tokenID, tokenErr := domain.ParseTranscriptTokenID(tokenValue)
			if tokenErr != nil {
				return CompleteMediaTranscript{}, tokenErr
			}
			materialized.Tokens[tokenIndex] = domain.TranscriptToken{
				ID: tokenID, Ordinal: uint32(tokenIndex), SourceRange: token.SourceRange,
				Text: token.Text, ConfidenceBasisPoints: token.ConfidenceBasisPoints,
			}
		}
		artifact.Segments[segmentIndex] = materialized
	}
	if err := artifact.Validate(); err != nil {
		return CompleteMediaTranscript{}, err
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/transcript-artifact", domain.TranscriptArtifactSchema, artifact,
	)
	if err != nil {
		return CompleteMediaTranscript{}, err
	}
	size, err := domain.NewUInt64(uint64(len(canonical)))
	if err != nil {
		return CompleteMediaTranscript{}, err
	}
	return CompleteMediaTranscript{
		Claim: claim, Artifact: artifact, ArtifactCanonical: canonical,
		ContentDigest: digest, ByteSize: size, EventID: eventID, CompletedAt: at,
	}, nil
}

func (scheduler *mediaWorkDispatcher) materializeProbe(
	ctx context.Context,
	probe MediaProbe,
	at time.Time,
) (domain.ArtifactID, domain.MediaFacts, error) {
	artifactValue, err := scheduler.identities.NewID(ctx, at)
	if err != nil {
		return domain.ArtifactID{}, domain.MediaFacts{}, err
	}
	artifactID, err := domain.ParseArtifactID(artifactValue)
	if err != nil {
		return domain.ArtifactID{}, domain.MediaFacts{}, err
	}
	facts := domain.MediaFacts{
		Container: probe.Container, ContainerAliases: append([]string(nil), probe.ContainerAliases...),
		StartTime: probe.StartTime, Duration: probe.Duration, BitRate: probe.BitRate,
		Streams: make([]domain.SourceStream, 0, len(probe.Streams)),
	}
	for _, descriptor := range probe.Streams {
		value, idErr := scheduler.identities.NewID(ctx, at)
		if idErr != nil {
			return domain.ArtifactID{}, domain.MediaFacts{}, idErr
		}
		id, idErr := domain.ParseSourceStreamID(value)
		if idErr != nil {
			return domain.ArtifactID{}, domain.MediaFacts{}, idErr
		}
		facts.Streams = append(facts.Streams, domain.SourceStream{ID: id, Descriptor: descriptor})
	}
	if err := facts.Validate(); err != nil {
		return domain.ArtifactID{}, domain.MediaFacts{}, err
	}
	return artifactID, facts, nil
}

func (scheduler *mediaWorkDispatcher) materializeFrameSet(
	ctx context.Context,
	claim MediaJobClaim,
	frameSet MediaFrameSetExecution,
	eventID domain.ActivityEventID,
	at time.Time,
) (CompleteMediaFrameSet, error) {
	parameters, err := DecodeFrameSetParameters(claim.ParametersJSON)
	if err != nil || parameters.AssetID != claim.AssetID || claim.AcceptedFingerprint == nil ||
		parameters.Fingerprint != *claim.AcceptedFingerprint || frameSet.Validate(parameters) != nil {
		return CompleteMediaFrameSet{}, domain.ErrInvalidMediaFacts
	}
	canonicalParameters, parametersDigest, err := CanonicalFrameSetParameters(parameters)
	if err != nil || !bytes.Equal(canonicalParameters, claim.ParametersJSON) ||
		parametersDigest != claim.ParametersDigest {
		return CompleteMediaFrameSet{}, domain.ErrInvalidMediaFacts
	}
	artifactValue, err := scheduler.identities.NewID(ctx, at)
	if err != nil {
		return CompleteMediaFrameSet{}, err
	}
	artifactID, err := domain.ParseArtifactID(artifactValue)
	if err != nil {
		return CompleteMediaFrameSet{}, err
	}
	manifest := FrameSetArtifactManifest{
		AssetID: claim.AssetID, Fingerprint: *claim.AcceptedFingerprint,
		SourceStreamID: parameters.SourceStreamID, Profile: parameters.Profile,
		Producer: claim.ExecutorVersion,
		Samples:  make([]FrameSetArtifactSample, 0, len(frameSet.Samples)),
	}
	pngs := make([][]byte, 0, len(frameSet.Samples))
	totalBytes := uint64(0)
	for index, sample := range frameSet.Samples {
		decoded, decodeErr := png.Decode(bytes.NewReader(sample.PNG))
		if decodeErr != nil || decoded.Bounds().Dx() != int(sample.Width) || decoded.Bounds().Dy() != int(sample.Height) {
			return CompleteMediaFrameSet{}, domain.ErrInvalidMediaFacts
		}
		digest := sha256.Sum256(sample.PNG)
		size, sizeErr := domain.NewUInt64(uint64(len(sample.PNG)))
		if sizeErr != nil {
			return CompleteMediaFrameSet{}, sizeErr
		}
		manifest.Samples = append(manifest.Samples, FrameSetArtifactSample{
			RequestedTime: sample.RequestedTime, SourceTime: sample.SourceTime,
			Width: sample.Width, Height: sample.Height,
			Path: fmt.Sprintf("frames/%03d.png", index), ByteSize: size,
			SHA256: domain.Digest("sha256:" + hex.EncodeToString(digest[:])),
		})
		pngs = append(pngs, append([]byte(nil), sample.PNG...))
		totalBytes += uint64(len(sample.PNG))
	}
	manifestCanonical, contentDigest, err := domain.CanonicalDigest(
		"open-cut/media-frame-set-artifact", FrameSetArtifactSchema, manifest,
	)
	if err != nil {
		return CompleteMediaFrameSet{}, err
	}
	totalBytes += uint64(len(manifestCanonical))
	if totalBytes > MaximumFrameSetArtifactSize {
		return CompleteMediaFrameSet{}, domain.ErrInvalidMediaFacts
	}
	byteSize, err := domain.NewUInt64(totalBytes)
	if err != nil {
		return CompleteMediaFrameSet{}, err
	}
	return CompleteMediaFrameSet{
		Claim: claim, ArtifactID: artifactID, Parameters: parameters, Manifest: manifest,
		ManifestCanonical: manifestCanonical, ContentDigest: contentDigest, PNGs: pngs,
		ByteSize: byteSize, EventID: eventID, CompletedAt: at,
	}, nil
}

func (scheduler *mediaWorkDispatcher) materializeProxy(
	ctx context.Context,
	claim MediaJobClaim,
	proxy MediaProxyExecution,
	eventID domain.ActivityEventID,
	at time.Time,
) (CompleteMediaProxy, error) {
	parameters, err := DecodeInitialMediaJobParameters(claim.ParametersJSON)
	if err != nil || parameters.AssetID != claim.AssetID || parameters.Kind != domain.MediaJobProxy ||
		parameters.Profile != SourceProxyProfile || claim.AcceptedFingerprint == nil || proxy.Workspace == nil {
		return CompleteMediaProxy{}, domain.ErrInvalidMediaFacts
	}
	canonicalParameters, parametersDigest, err := CanonicalInitialMediaJobParameters(parameters)
	if err != nil || !bytes.Equal(canonicalParameters, claim.ParametersJSON) ||
		parametersDigest != claim.ParametersDigest {
		return CompleteMediaProxy{}, domain.ErrInvalidMediaFacts
	}
	expectedVideo, expectedAudio, err := SelectSourceProxyStreams(claim.SourceStreams, *parameters.ProxySelection)
	if err != nil || !matchingSourceProxyTrack(expectedVideo, proxy.Video) ||
		!matchingSourceProxyTrack(expectedAudio, proxy.Audio) {
		return CompleteMediaProxy{}, domain.ErrInvalidMediaFacts
	}
	artifactValue, err := scheduler.identities.NewID(ctx, at)
	if err != nil {
		return CompleteMediaProxy{}, err
	}
	artifactID, err := domain.ParseArtifactID(artifactValue)
	if err != nil {
		return CompleteMediaProxy{}, err
	}
	manifest := SourceProxyArtifactManifest{
		AssetID: claim.AssetID, Fingerprint: *claim.AcceptedFingerprint,
		Profile: parameters.Profile, Producer: claim.ExecutorVersion,
		SourceEpoch: proxy.SourceEpoch, Media: proxy.Media, Video: proxy.Video, Audio: proxy.Audio,
	}
	if err := manifest.Validate(); err != nil {
		return CompleteMediaProxy{}, err
	}
	manifestCanonical, contentDigest, err := domain.CanonicalDigest(
		"open-cut/source-proxy-artifact", SourceProxyArtifactSchema, manifest,
	)
	if err != nil || len(manifestCanonical) > MaximumSourceProxyManifestSize {
		return CompleteMediaProxy{}, domain.ErrInvalidMediaFacts
	}
	total := uint64(len(manifestCanonical))
	for _, size := range []uint64{manifest.Media.ByteSize.Value(), sourceProxyTimeMapSize(manifest.Video)} {
		if size > MaximumSourceProxyArtifactSize-total {
			return CompleteMediaProxy{}, domain.ErrInvalidMediaFacts
		}
		total += size
	}
	byteSize, err := domain.NewUInt64(total)
	if err != nil {
		return CompleteMediaProxy{}, err
	}
	return CompleteMediaProxy{
		Claim: claim, ArtifactID: artifactID, Parameters: parameters, Manifest: manifest,
		ManifestCanonical: manifestCanonical, ContentDigest: contentDigest, ByteSize: byteSize,
		Workspace: proxy.Workspace, EventID: eventID, CompletedAt: at,
	}, nil
}

func matchingSourceProxyTrack[T SourceProxyVideoTrack | SourceProxyAudioTrack](
	expected *domain.SourceStream,
	actual *T,
) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	var source domain.SourceStream
	switch typed := any(actual).(type) {
	case *SourceProxyVideoTrack:
		source = typed.Source
	case *SourceProxyAudioTrack:
		source = typed.Source
	default:
		return false
	}
	return reflect.DeepEqual(*expected, source)
}

func sourceProxyTimeMapSize(video *SourceProxyVideoTrack) uint64 {
	if video == nil {
		return 0
	}
	return video.TimeMap.ByteSize.Value()
}

func (scheduler *mediaWorkDispatcher) newActivityEventID(
	ctx context.Context,
	at time.Time,
) (domain.ActivityEventID, error) {
	value, err := scheduler.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(value)
}

func classifyMediaExecutionError(err error) MediaExecutionError {
	var failure MediaExecutionError
	if errors.As(err, &failure) && mediaFailureCode.MatchString(failure.Code) &&
		(failure.Availability == nil || validFailureAvailability(*failure.Availability)) {
		return failure
	}
	return MediaExecutionError{Code: "executor-failed", Cause: err}
}

func validFailureAvailability(value domain.AssetAvailability) bool {
	return value == domain.AssetChanged || value == domain.AssetMissing || value == domain.AssetUnreadable
}
