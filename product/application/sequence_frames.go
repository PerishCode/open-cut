package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	SequenceFrameSetProfile          = "sequence-frame-srgb-png-v1"
	SequenceFrameGridPolicy          = "sequence-frame-grid-floor-v1"
	SequenceFrameParametersSchema    = "open-cut/sequence-frame-set-parameters/v1"
	SequenceFrameArtifactSchema      = "open-cut/sequence-frame-set-artifact/v1"
	MaximumSequenceFrameSamples      = 8
	MaximumSequenceFrameArtifactSize = 32 << 20
)

var (
	ErrSequenceFramesInvalid  = errors.New("sequence frame request is invalid")
	ErrSequenceFramesNotFound = errors.New("sequence frame job was not found")
	ErrSequenceFramesRecovery = errors.New("sequence frame job cannot use the requested recovery")
)

type SequenceFramesOperation string

const (
	SequenceFramesPrepare  SequenceFramesOperation = "prepare"
	SequenceFramesContinue SequenceFramesOperation = "continue"
	SequenceFramesRetry    SequenceFramesOperation = "retry"
)

type SequenceFramesInput struct {
	Operation        SequenceFramesOperation `json:"operation" enum:"prepare,continue,retry"`
	SequenceRevision *domain.Revision        `json:"sequenceRevision,omitempty" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Times            []domain.RationalTime   `json:"times,omitempty" maxItems:"8"`
	JobID            *domain.WorkJobID       `json:"jobId,omitempty"`
}

func (input SequenceFramesInput) Validate() error {
	switch input.Operation {
	case SequenceFramesPrepare:
		if input.SequenceRevision == nil || input.SequenceRevision.Value() == 0 || input.JobID != nil ||
			len(input.Times) == 0 || len(input.Times) > MaximumSequenceFrameSamples {
			return ErrSequenceFramesInvalid
		}
		for index, instant := range input.Times {
			if instant.Validate() != nil || instant.IsNegative() {
				return ErrSequenceFramesInvalid
			}
			if index > 0 {
				comparison, err := input.Times[index-1].Compare(instant)
				if err != nil || comparison >= 0 {
					return ErrSequenceFramesInvalid
				}
			}
		}
	case SequenceFramesContinue, SequenceFramesRetry:
		if input.SequenceRevision != nil || len(input.Times) != 0 || input.JobID == nil || input.JobID.IsZero() {
			return ErrSequenceFramesInvalid
		}
	default:
		return ErrSequenceFramesInvalid
	}
	return nil
}

type SequenceFrameCoordinate struct {
	RequestedTime domain.RationalTime `json:"requestedTime"`
	SequenceTime  domain.RationalTime `json:"sequenceTime"`
	FrameIndex    domain.UInt64       `json:"frameIndex" format:"uint64-decimal"`
}

type SequenceFrameSetParameters struct {
	ProjectID        domain.ProjectID          `json:"projectId"`
	SequenceID       domain.SequenceID         `json:"sequenceId"`
	SequenceRevision domain.Revision           `json:"sequenceRevision"`
	PreviewJobID     domain.WorkJobID          `json:"previewJobId"`
	FrameRate        domain.RationalTime       `json:"frameRate"`
	GridPolicy       string                    `json:"gridPolicy"`
	Profile          string                    `json:"profile"`
	ExecutorVersion  string                    `json:"executorVersion"`
	Samples          []SequenceFrameCoordinate `json:"samples"`
}

func NewSequenceFrameSetParameters(
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	previewJobID domain.WorkJobID,
	frameRate domain.RationalTime,
	times []domain.RationalTime,
	executorVersion string,
) (SequenceFrameSetParameters, error) {
	parameters := SequenceFrameSetParameters{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: sequenceRevision,
		PreviewJobID: previewJobID, FrameRate: frameRate,
		GridPolicy: SequenceFrameGridPolicy, Profile: SequenceFrameSetProfile, ExecutorVersion: executorVersion,
		Samples: make([]SequenceFrameCoordinate, 0, len(times)),
	}
	for _, instant := range times {
		coordinate, err := sequenceFrameCoordinate(instant, frameRate)
		if err != nil {
			return SequenceFrameSetParameters{}, err
		}
		parameters.Samples = append(parameters.Samples, coordinate)
	}
	if err := parameters.Validate(); err != nil {
		return SequenceFrameSetParameters{}, err
	}
	return parameters, nil
}

func (parameters SequenceFrameSetParameters) Validate() error {
	if parameters.ProjectID.IsZero() || parameters.SequenceID.IsZero() ||
		parameters.SequenceRevision.Value() == 0 || parameters.PreviewJobID.IsZero() ||
		parameters.FrameRate.Validate() != nil || !parameters.FrameRate.IsPositive() ||
		parameters.GridPolicy != SequenceFrameGridPolicy || parameters.Profile != SequenceFrameSetProfile ||
		parameters.ExecutorVersion == "" || len(parameters.ExecutorVersion) > 1024 ||
		len(parameters.Samples) == 0 || len(parameters.Samples) > MaximumSequenceFrameSamples {
		return ErrSequenceFramesInvalid
	}
	for index, sample := range parameters.Samples {
		expected, err := sequenceFrameCoordinate(sample.RequestedTime, parameters.FrameRate)
		if err != nil || expected != sample {
			return ErrSequenceFramesInvalid
		}
		if index > 0 {
			comparison, err := parameters.Samples[index-1].RequestedTime.Compare(sample.RequestedTime)
			if err != nil || comparison >= 0 {
				return ErrSequenceFramesInvalid
			}
		}
	}
	return nil
}

func sequenceFrameCoordinate(
	instant domain.RationalTime,
	frameRate domain.RationalTime,
) (SequenceFrameCoordinate, error) {
	if instant.Validate() != nil || instant.IsNegative() || frameRate.Validate() != nil || !frameRate.IsPositive() {
		return SequenceFrameCoordinate{}, ErrSequenceFramesInvalid
	}
	numerator := new(big.Int).Mul(big.NewInt(instant.Value.Value()), big.NewInt(frameRate.Value.Value()))
	denominator := new(big.Int).Mul(big.NewInt(int64(instant.Scale)), big.NewInt(int64(frameRate.Scale)))
	index := new(big.Int).Quo(numerator, denominator)
	if !index.IsUint64() || index.Uint64() > math.MaxInt64 || frameRate.Value.Value() > math.MaxInt32 {
		return SequenceFrameCoordinate{}, ErrSequenceFramesInvalid
	}
	frameIndex, err := domain.NewUInt64(index.Uint64())
	if err != nil {
		return SequenceFrameCoordinate{}, err
	}
	gridNumerator := new(big.Int).Mul(index, big.NewInt(int64(frameRate.Scale)))
	if !gridNumerator.IsInt64() {
		return SequenceFrameCoordinate{}, ErrSequenceFramesInvalid
	}
	sequenceTime, err := domain.NewRationalTime(gridNumerator.Int64(), int32(frameRate.Value.Value()))
	if err != nil {
		return SequenceFrameCoordinate{}, err
	}
	return SequenceFrameCoordinate{
		RequestedTime: instant, SequenceTime: sequenceTime, FrameIndex: frameIndex,
	}, nil
}

func SequenceFrameOutputDimensions(canvasWidth, canvasHeight uint32) (uint32, uint32, error) {
	if canvasWidth == 0 || canvasHeight == 0 {
		return 0, 0, ErrSequenceFramesInvalid
	}
	width := new(big.Rat).SetInt64(int64(canvasWidth))
	height := new(big.Rat).SetInt64(int64(canvasHeight))
	long := width
	if height.Cmp(width) > 0 {
		long = height
	}
	maximum := new(big.Rat).SetInt64(MaximumFrameLongEdge)
	if long.Cmp(maximum) > 0 {
		scale := new(big.Rat).Quo(maximum, long)
		width.Mul(width, scale)
		height.Mul(height, scale)
	}
	resultWidth, ok := roundedSequenceFrameDimension(width)
	if !ok {
		return 0, 0, ErrSequenceFramesInvalid
	}
	resultHeight, ok := roundedSequenceFrameDimension(height)
	if !ok {
		return 0, 0, ErrSequenceFramesInvalid
	}
	return resultWidth, resultHeight, nil
}

func roundedSequenceFrameDimension(value *big.Rat) (uint32, bool) {
	numerator := new(big.Int).Mul(value.Num(), big.NewInt(2))
	numerator.Add(numerator, value.Denom())
	denominator := new(big.Int).Mul(value.Denom(), big.NewInt(2))
	rounded := new(big.Int).Quo(numerator, denominator)
	if rounded.Sign() <= 0 {
		return 1, true
	}
	if !rounded.IsUint64() || rounded.Uint64() > MaximumFrameLongEdge {
		return 0, false
	}
	return uint32(rounded.Uint64()), true
}

func CanonicalSequenceFrameSetParameters(
	parameters SequenceFrameSetParameters,
) ([]byte, domain.Digest, error) {
	if err := parameters.Validate(); err != nil {
		return nil, "", err
	}
	return domain.CanonicalDigest(
		"open-cut/sequence-frame-set-parameters", SequenceFrameParametersSchema, parameters,
	)
}

func DecodeSequenceFrameSetParameters(data []byte) (SequenceFrameSetParameters, error) {
	var envelope struct {
		Domain  string                     `json:"domain"`
		Payload SequenceFrameSetParameters `json:"payload"`
		Schema  string                     `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/sequence-frame-set-parameters" ||
		envelope.Schema != SequenceFrameParametersSchema || envelope.Payload.Validate() != nil {
		return SequenceFrameSetParameters{}, ErrSequenceFramesInvalid
	}
	return envelope.Payload, nil
}

type SequenceFrameJob struct {
	ID                  domain.WorkJobID    `json:"id"`
	Kind                domain.WorkJobKind  `json:"kind" enum:"sequence-frame-set"`
	State               domain.WorkJobState `json:"state" enum:"blocked,queued,running,succeeded,failed,cancelled"`
	ProgressBasisPoints uint16              `json:"progressBasisPoints" minimum:"0" maximum:"10000"`
	PreviewJobID        domain.WorkJobID    `json:"previewJobId"`
	TerminalErrorCode   *string             `json:"terminalErrorCode,omitempty" minLength:"1" maxLength:"256"`
	ResultArtifactID    *domain.ArtifactID  `json:"resultArtifactId,omitempty"`
	CreatedAt           time.Time           `json:"createdAt"`
	UpdatedAt           time.Time           `json:"updatedAt"`
}

type SequenceFrameSetStatus string

const (
	SequenceFrameSetAccepted SequenceFrameSetStatus = "accepted"
	SequenceFrameSetReady    SequenceFrameSetStatus = "ready"
	SequenceFrameSetFailed   SequenceFrameSetStatus = "failed"
)

type SequenceFrameResourceLease struct {
	ResourceID    domain.ResourceID   `json:"resourceId" format:"uuid"`
	MIMEType      string              `json:"mimeType" enum:"image/png"`
	ByteSize      domain.UInt64       `json:"byteSize" format:"uint64-decimal"`
	SHA256        domain.Digest       `json:"sha256" format:"sha256-digest"`
	RequestedTime domain.RationalTime `json:"requestedTime"`
	SequenceTime  domain.RationalTime `json:"sequenceTime"`
	FrameIndex    domain.UInt64       `json:"frameIndex" format:"uint64-decimal"`
	ReadOnlyPath  string              `json:"readOnlyPath"`
	ExpiresAt     time.Time           `json:"expiresAt"`
}

type SequenceFrameSetResult struct {
	Status           SequenceFrameSetStatus       `json:"status" enum:"accepted,ready,failed"`
	ProjectID        domain.ProjectID             `json:"projectId"`
	SequenceID       domain.SequenceID            `json:"sequenceId"`
	SequenceRevision domain.Revision              `json:"sequenceRevision"`
	Profile          string                       `json:"profile" enum:"sequence-frame-srgb-png-v1"`
	Samples          []SequenceFrameCoordinate    `json:"samples" maxItems:"8" nullable:"false"`
	Job              SequenceFrameJob             `json:"job"`
	Recovery         MediaRecoveryAction          `json:"recovery" enum:"retry-job,relink-source,acquire-resource,adopt-revision,update-runtime,none"`
	ArtifactID       *domain.ArtifactID           `json:"artifactId,omitempty"`
	Resources        []SequenceFrameResourceLease `json:"resources" maxItems:"8" nullable:"false"`
	ActivityCursor   domain.Cursor                `json:"activityCursor"`
}

type RequestSequenceFrameSetRecord struct {
	JobID            domain.WorkJobID
	ProjectID        domain.ProjectID
	SequenceID       domain.SequenceID
	RunID            domain.RunID
	TurnID           domain.TurnID
	Actor            domain.ActorRef
	Parameters       SequenceFrameSetParameters
	ParametersJSON   []byte
	ParametersDigest domain.Digest
	LogicalKey       string
	ActivityEventID  domain.ActivityEventID
	RequestedAt      time.Time
}

type ReadSequenceFrameSetRecord struct {
	ProjectID  domain.ProjectID
	SequenceID domain.SequenceID
	RunID      domain.RunID
	TurnID     domain.TurnID
	Actor      domain.ActorRef
	JobID      domain.WorkJobID
}

type MaterializeSequenceFrameLeasesRecord struct {
	ReadSequenceFrameSetRecord
	SequenceRevision domain.Revision
	ArtifactID       domain.ArtifactID
	LeaseSetID       domain.ResourceID
	Resources        []domain.ResourceID
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

type SequenceFrameRetrySeed struct {
	Result     SequenceFrameSetResult
	Parameters SequenceFrameSetParameters
}

type RetrySequenceFrameSetRecord struct {
	PredecessorJobID domain.WorkJobID
	Job              RequestSequenceFrameSetRecord
}

type SequenceFrameRepository interface {
	ReadSequenceFrameRate(context.Context, domain.ProjectID, domain.SequenceID, domain.Revision) (domain.RationalTime, error)
	RequestSequenceFrameSet(context.Context, RequestSequenceFrameSetRecord) (SequenceFrameSetResult, error)
	ReadSequenceFrameSet(context.Context, ReadSequenceFrameSetRecord) (SequenceFrameSetResult, error)
	LoadSequenceFrameRetrySeed(context.Context, ReadSequenceFrameSetRecord) (SequenceFrameRetrySeed, error)
	RetrySequenceFrameSet(context.Context, RetrySequenceFrameSetRecord) (SequenceFrameSetResult, error)
	MaterializeSequenceFrameLeases(context.Context, MaterializeSequenceFrameLeasesRecord) ([]SequenceFrameResourceLease, error)
}

type SequenceFrames struct {
	repository SequenceFrameRepository
	previews   *SequencePreviews
	identities IdentityGenerator
	clock      Clock
	settings   SequenceFrameSettings
}

type SequenceFrameSettings struct {
	ExecutorVersion string
}

func NewSequenceFrames(
	repository SequenceFrameRepository,
	previews *SequencePreviews,
	identities IdentityGenerator,
	clock Clock,
	settings SequenceFrameSettings,
) (*SequenceFrames, error) {
	if repository == nil || previews == nil || identities == nil || clock == nil ||
		settings.ExecutorVersion == "" || len(settings.ExecutorVersion) > 1024 {
		return nil, fmt.Errorf("sequence frame dependencies are required")
	}
	return &SequenceFrames{
		repository: repository, previews: previews, identities: identities, clock: clock, settings: settings,
	}, nil
}

func (frames *SequenceFrames) Execute(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	input SequenceFramesInput,
) (SequenceFrameSetResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || runID.IsZero() || turnID.IsZero() || input.Validate() != nil {
		return SequenceFrameSetResult{}, ErrSequenceFramesInvalid
	}
	var result SequenceFrameSetResult
	switch input.Operation {
	case SequenceFramesPrepare:
		result, err = frames.prepare(ctx, authority, projectID, sequenceID, runID, turnID, input)
	case SequenceFramesContinue:
		result, err = frames.repository.ReadSequenceFrameSet(ctx, ReadSequenceFrameSetRecord{
			ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID,
			Actor: authority.Actor, JobID: *input.JobID,
		})
	case SequenceFramesRetry:
		result, err = frames.retry(ctx, authority, projectID, sequenceID, runID, turnID, *input.JobID)
	}
	if err != nil {
		return result, err
	}
	return frames.materialize(ctx, authority, runID, turnID, result)
}

func (frames *SequenceFrames) retry(
	ctx context.Context,
	authority Authority,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	jobID domain.WorkJobID,
) (SequenceFrameSetResult, error) {
	seed, err := frames.repository.LoadSequenceFrameRetrySeed(ctx, ReadSequenceFrameSetRecord{
		ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID,
		Actor: authority.Actor, JobID: jobID,
	})
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	if seed.Result.Recovery != MediaRecoveryRetryJob {
		return SequenceFrameSetResult{}, ErrSequenceFramesRecovery
	}
	preview, err := frames.previews.ContinueForAgentOperationalRead(
		ctx, projectID, sequenceID, seed.Parameters.SequenceRevision, seed.Parameters.PreviewJobID,
	)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	if preview.Status == SequencePreviewFailed {
		if preview.Job == nil || SequencePreviewRecoveryAction(*preview.Job) != MediaRecoveryRetryJob {
			return SequenceFrameSetResult{}, ErrSequenceFramesRecovery
		}
		preview, err = frames.previews.RetryForAgentOperationalRead(
			ctx, projectID, sequenceID, seed.Parameters.SequenceRevision, preview.Job.ID,
		)
		if err != nil {
			return SequenceFrameSetResult{}, err
		}
	}
	if preview.Status == SequencePreviewEmpty || preview.Status == SequencePreviewFailed || preview.Job == nil {
		return SequenceFrameSetResult{}, ErrSequenceFramesRecovery
	}
	parameters := seed.Parameters
	parameters.PreviewJobID = preview.Job.ID
	parameters.ExecutorVersion = frames.settings.ExecutorVersion
	canonical, digest, err := CanonicalSequenceFrameSetParameters(parameters)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	now := frames.clock.Now().UTC()
	newJobID, err := frames.newWorkJobID(ctx, now)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	eventID, err := frames.newActivityEventID(ctx, now)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	return frames.repository.RetrySequenceFrameSet(ctx, RetrySequenceFrameSetRecord{
		PredecessorJobID: seed.Result.Job.ID,
		Job: RequestSequenceFrameSetRecord{
			JobID: newJobID, ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID,
			Actor: authority.Actor, Parameters: parameters, ParametersJSON: canonical, ParametersDigest: digest,
			LogicalKey: "sequence-frames/v1/" + preview.Job.ID.String() + "/" + digest.String() +
				"/retry/" + newJobID.String(),
			ActivityEventID: eventID, RequestedAt: now,
		},
	})
}

func (frames *SequenceFrames) prepare(
	ctx context.Context,
	authority Authority,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	input SequenceFramesInput,
) (SequenceFrameSetResult, error) {
	preview, err := frames.previews.PrepareForAgentOperationalRead(
		ctx, projectID, sequenceID, *input.SequenceRevision,
	)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	if preview.Status == SequencePreviewEmpty || preview.Job == nil {
		return SequenceFrameSetResult{}, ErrSequenceFramesInvalid
	}
	frameRate, err := frames.repository.ReadSequenceFrameRate(ctx, projectID, sequenceID, *input.SequenceRevision)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	parameters, err := NewSequenceFrameSetParameters(
		projectID, sequenceID, *input.SequenceRevision, preview.Job.ID, frameRate, input.Times,
		frames.settings.ExecutorVersion,
	)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	canonical, digest, err := CanonicalSequenceFrameSetParameters(parameters)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	now := frames.clock.Now().UTC()
	jobID, err := frames.newWorkJobID(ctx, now)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	eventID, err := frames.newActivityEventID(ctx, now)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	return frames.repository.RequestSequenceFrameSet(ctx, RequestSequenceFrameSetRecord{
		JobID: jobID, ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID,
		Actor: authority.Actor, Parameters: parameters, ParametersJSON: canonical, ParametersDigest: digest,
		LogicalKey:      "sequence-frames/v1/" + preview.Job.ID.String() + "/" + digest.String(),
		ActivityEventID: eventID, RequestedAt: now,
	})
}

func (frames *SequenceFrames) newWorkJobID(ctx context.Context, at time.Time) (domain.WorkJobID, error) {
	value, err := frames.identities.NewID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	return domain.ParseWorkJobID(value)
}

func (frames *SequenceFrames) newActivityEventID(
	ctx context.Context,
	at time.Time,
) (domain.ActivityEventID, error) {
	value, err := frames.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(value)
}

func SequenceFrameRecoveryAction(job SequenceFrameJob) MediaRecoveryAction {
	if job.State == domain.MediaJobCancelled {
		return MediaRecoveryRetryJob
	}
	if job.State != domain.MediaJobFailed || job.TerminalErrorCode == nil {
		return MediaRecoveryNone
	}
	switch *job.TerminalErrorCode {
	case "frame-decode-failed", "frame-decode-timeout", "frame-output-limit",
		"attempt-limit-exceeded", "frame-artifact-unavailable":
		return MediaRecoveryRetryJob
	case "input-job-failed", "input-artifact-unavailable":
		return MediaRecoveryRelinkSource
	case "sequence-revision-conflict":
		return MediaRecoveryAdoptRevision
	case "sequence-time-out-of-range":
		return MediaRecoveryNone
	default:
		return MediaRecoveryUpdateRuntime
	}
}

func (frames *SequenceFrames) materialize(
	ctx context.Context,
	authority Authority,
	runID domain.RunID,
	turnID domain.TurnID,
	result SequenceFrameSetResult,
) (SequenceFrameSetResult, error) {
	if result.Resources == nil {
		result.Resources = []SequenceFrameResourceLease{}
	}
	if result.Status != SequenceFrameSetReady || result.ArtifactID == nil || len(result.Resources) != 0 {
		return result, nil
	}
	resources := make([]domain.ResourceID, 0, len(result.Samples))
	now := frames.clock.Now().UTC()
	leaseSetValue, err := frames.identities.NewID(ctx, now)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	leaseSetID, err := domain.ParseResourceID(leaseSetValue)
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	for range result.Samples {
		value, err := frames.identities.NewID(ctx, now)
		if err != nil {
			return SequenceFrameSetResult{}, err
		}
		resource, err := domain.ParseResourceID(value)
		if err != nil {
			return SequenceFrameSetResult{}, err
		}
		resources = append(resources, resource)
	}
	leases, err := frames.repository.MaterializeSequenceFrameLeases(ctx, MaterializeSequenceFrameLeasesRecord{
		ReadSequenceFrameSetRecord: ReadSequenceFrameSetRecord{
			ProjectID: result.ProjectID, SequenceID: result.SequenceID, RunID: runID, TurnID: turnID,
			Actor: authority.Actor, JobID: result.Job.ID,
		},
		SequenceRevision: result.SequenceRevision,
		ArtifactID:       *result.ArtifactID, LeaseSetID: leaseSetID, Resources: resources,
		CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	})
	if err != nil {
		return SequenceFrameSetResult{}, err
	}
	result.Resources = leases
	return result, nil
}

type SequenceFrameExecutionSample struct {
	Coordinate SequenceFrameCoordinate
	Width      uint32
	Height     uint32
	PNG        []byte
}

type SequenceFrameExecution struct {
	Samples []SequenceFrameExecutionSample
}

type SequenceFrameArtifactSample struct {
	SequenceFrameCoordinate
	Width    uint32        `json:"width"`
	Height   uint32        `json:"height"`
	Path     string        `json:"path"`
	ByteSize domain.UInt64 `json:"byteSize" format:"uint64-decimal"`
	SHA256   domain.Digest `json:"sha256" format:"sha256-digest"`
}

type SequenceFrameArtifactManifest struct {
	ProjectID             domain.ProjectID              `json:"projectId"`
	SequenceID            domain.SequenceID             `json:"sequenceId"`
	SequenceRevision      domain.Revision               `json:"sequenceRevision"`
	PreviewJobID          domain.WorkJobID              `json:"previewJobId"`
	PreviewArtifactID     domain.ArtifactID             `json:"previewArtifactId"`
	PreviewArtifactDigest domain.Digest                 `json:"previewArtifactDigest"`
	RenderPlanDigest      domain.Digest                 `json:"renderPlanDigest"`
	Profile               string                        `json:"profile"`
	GridPolicy            string                        `json:"gridPolicy"`
	Producer              string                        `json:"producer"`
	Samples               []SequenceFrameArtifactSample `json:"samples"`
}

func (manifest SequenceFrameArtifactManifest) Validate() error {
	if manifest.ProjectID.IsZero() || manifest.SequenceID.IsZero() || manifest.SequenceRevision.Value() == 0 ||
		manifest.PreviewJobID.IsZero() || manifest.PreviewArtifactID.IsZero() ||
		manifest.PreviewArtifactDigest == "" || manifest.RenderPlanDigest == "" ||
		manifest.Profile != SequenceFrameSetProfile || manifest.GridPolicy != SequenceFrameGridPolicy ||
		manifest.Producer == "" || len(manifest.Producer) > 1024 ||
		len(manifest.Samples) == 0 || len(manifest.Samples) > MaximumSequenceFrameSamples {
		return ErrSequenceFramesInvalid
	}
	if _, err := domain.ParseDigest(manifest.PreviewArtifactDigest.String()); err != nil {
		return ErrSequenceFramesInvalid
	}
	if _, err := domain.ParseDigest(manifest.RenderPlanDigest.String()); err != nil {
		return ErrSequenceFramesInvalid
	}
	for index, sample := range manifest.Samples {
		if sample.RequestedTime.Validate() != nil || sample.SequenceTime.Validate() != nil ||
			sample.Width == 0 || sample.Height == 0 || sample.Width > MaximumFrameLongEdge ||
			sample.Height > MaximumFrameLongEdge || sample.Path != fmt.Sprintf("frames/%03d.png", index) ||
			sample.ByteSize.Value() == 0 || sample.ByteSize.Value() > MaximumSequenceFrameArtifactSize || sample.SHA256 == "" {
			return ErrSequenceFramesInvalid
		}
		if _, err := domain.ParseDigest(sample.SHA256.String()); err != nil {
			return ErrSequenceFramesInvalid
		}
	}
	return nil
}

func DecodeSequenceFrameArtifactManifest(data []byte) (SequenceFrameArtifactManifest, error) {
	var envelope struct {
		Domain  string                        `json:"domain"`
		Payload SequenceFrameArtifactManifest `json:"payload"`
		Schema  string                        `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/sequence-frame-set-artifact" ||
		envelope.Schema != SequenceFrameArtifactSchema || envelope.Payload.Validate() != nil {
		return SequenceFrameArtifactManifest{}, ErrSequenceFramesInvalid
	}
	return envelope.Payload, nil
}

type SequenceFrameJobClaim struct {
	ProjectID        domain.ProjectID
	SequenceID       domain.SequenceID
	SequenceRevision domain.Revision
	Parameters       SequenceFrameSetParameters
	ParametersDigest domain.Digest
	ParametersJSON   []byte
	PreviewArtifact  domain.SequencePreviewArtifactSummary
}

type CompleteSequenceFrameSet struct {
	Claim             WorkJobClaim
	ArtifactID        domain.ArtifactID
	Manifest          SequenceFrameArtifactManifest
	ManifestCanonical []byte
	ContentDigest     domain.Digest
	PNGs              [][]byte
	ByteSize          domain.UInt64
	EventID           domain.ActivityEventID
	CompletedAt       time.Time
}

type FailSequenceFrameSet struct {
	Claim    WorkJobClaim
	Code     string
	EventID  domain.ActivityEventID
	FailedAt time.Time
}
