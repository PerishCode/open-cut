package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	FrameSetProfile             = "frame-srgb-png-v1"
	FrameSetParametersSchema    = "open-cut/media-frame-set-parameters/v1"
	FrameSetArtifactSchema      = "open-cut/media-frame-set-artifact/v1"
	MaximumFrameSetSamples      = 8
	MaximumFrameLongEdge        = 1280
	MaximumFrameSetArtifactSize = 32 << 20
)

type MediaFrameSetParameters struct {
	AssetID        domain.AssetID        `json:"assetId"`
	Fingerprint    domain.Digest         `json:"fingerprint"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
	Times          []domain.RationalTime `json:"times"`
	Profile        string                `json:"profile"`
}

func (parameters MediaFrameSetParameters) Validate() error {
	if parameters.AssetID.IsZero() || parameters.SourceStreamID.IsZero() ||
		parameters.Profile != FrameSetProfile || len(parameters.Times) == 0 ||
		len(parameters.Times) > MaximumFrameSetSamples {
		return domain.ErrInvalidMediaFacts
	}
	if _, err := domain.ParseDigest(parameters.Fingerprint.String()); err != nil {
		return domain.ErrInvalidMediaFacts
	}
	for index, instant := range parameters.Times {
		if instant.Validate() != nil || instant.IsNegative() {
			return domain.ErrInvalidMediaFacts
		}
		if index > 0 {
			comparison, err := parameters.Times[index-1].Compare(instant)
			if err != nil || comparison >= 0 {
				return domain.ErrInvalidMediaFacts
			}
		}
	}
	return nil
}

func CanonicalFrameSetParameters(parameters MediaFrameSetParameters) ([]byte, domain.Digest, error) {
	if err := parameters.Validate(); err != nil {
		return nil, "", err
	}
	return domain.CanonicalDigest(
		"open-cut/media-frame-set-parameters", FrameSetParametersSchema, parameters,
	)
}

func DecodeFrameSetParameters(data []byte) (MediaFrameSetParameters, error) {
	var envelope struct {
		Domain  string                  `json:"domain"`
		Payload MediaFrameSetParameters `json:"payload"`
		Schema  string                  `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/media-frame-set-parameters" ||
		envelope.Schema != FrameSetParametersSchema || envelope.Payload.Validate() != nil {
		return MediaFrameSetParameters{}, domain.ErrInvalidMediaFacts
	}
	return envelope.Payload, nil
}

type MediaFrameSample struct {
	RequestedTime domain.RationalTime
	SourceTime    domain.RationalTime
	Width         uint32
	Height        uint32
	PNG           []byte
}

type MediaFrameSetExecution struct {
	SourceStreamID domain.SourceStreamID
	Profile        string
	Samples        []MediaFrameSample
}

func (frameSet MediaFrameSetExecution) Validate(parameters MediaFrameSetParameters) error {
	if frameSet.SourceStreamID != parameters.SourceStreamID || frameSet.Profile != parameters.Profile ||
		len(frameSet.Samples) != len(parameters.Times) {
		return domain.ErrInvalidMediaFacts
	}
	total := 0
	for index, sample := range frameSet.Samples {
		if sample.RequestedTime != parameters.Times[index] || sample.SourceTime.Validate() != nil ||
			sample.Width == 0 || sample.Height == 0 ||
			sample.Width > MaximumFrameLongEdge || sample.Height > MaximumFrameLongEdge || len(sample.PNG) == 0 {
			return domain.ErrInvalidMediaFacts
		}
		total += len(sample.PNG)
		if total > MaximumFrameSetArtifactSize {
			return fmt.Errorf("frame set exceeds the artifact limit")
		}
	}
	return nil
}

type FrameSetArtifactSample struct {
	RequestedTime domain.RationalTime `json:"requestedTime"`
	SourceTime    domain.RationalTime `json:"sourceTime"`
	Width         uint32              `json:"width"`
	Height        uint32              `json:"height"`
	Path          string              `json:"path"`
	ByteSize      domain.UInt64       `json:"byteSize"`
	SHA256        domain.Digest       `json:"sha256"`
}

type FrameSetArtifactManifest struct {
	AssetID        domain.AssetID           `json:"assetId"`
	Fingerprint    domain.Digest            `json:"fingerprint"`
	SourceStreamID domain.SourceStreamID    `json:"sourceStreamId"`
	Profile        string                   `json:"profile"`
	Producer       string                   `json:"producer"`
	Samples        []FrameSetArtifactSample `json:"samples"`
}

func (manifest FrameSetArtifactManifest) Validate() error {
	if manifest.AssetID.IsZero() || manifest.SourceStreamID.IsZero() ||
		manifest.Profile != FrameSetProfile || manifest.Producer == "" || len(manifest.Producer) > 256 ||
		len(manifest.Samples) == 0 || len(manifest.Samples) > MaximumFrameSetSamples {
		return domain.ErrInvalidMediaFacts
	}
	if _, err := domain.ParseDigest(manifest.Fingerprint.String()); err != nil {
		return domain.ErrInvalidMediaFacts
	}
	for index, sample := range manifest.Samples {
		if sample.RequestedTime.Validate() != nil || sample.RequestedTime.IsNegative() ||
			sample.SourceTime.Validate() != nil || sample.Width == 0 || sample.Height == 0 ||
			sample.Width > MaximumFrameLongEdge || sample.Height > MaximumFrameLongEdge ||
			sample.Path != fmt.Sprintf("frames/%03d.png", index) || sample.ByteSize.Value() == 0 ||
			sample.ByteSize.Value() > MaximumFrameSetArtifactSize {
			return domain.ErrInvalidMediaFacts
		}
		if _, err := domain.ParseDigest(sample.SHA256.String()); err != nil {
			return domain.ErrInvalidMediaFacts
		}
		if index > 0 {
			comparison, err := manifest.Samples[index-1].RequestedTime.Compare(sample.RequestedTime)
			if err != nil || comparison >= 0 {
				return domain.ErrInvalidMediaFacts
			}
		}
	}
	return nil
}

func DecodeFrameSetArtifactManifest(data []byte) (FrameSetArtifactManifest, error) {
	var envelope struct {
		Domain  string                   `json:"domain"`
		Payload FrameSetArtifactManifest `json:"payload"`
		Schema  string                   `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/media-frame-set-artifact" || envelope.Schema != FrameSetArtifactSchema ||
		envelope.Payload.Validate() != nil {
		return FrameSetArtifactManifest{}, domain.ErrInvalidMediaFacts
	}
	return envelope.Payload, nil
}

type CompleteMediaFrameSet struct {
	Claim             MediaJobClaim
	ArtifactID        domain.ArtifactID
	Parameters        MediaFrameSetParameters
	Manifest          FrameSetArtifactManifest
	ManifestCanonical []byte
	ContentDigest     domain.Digest
	PNGs              [][]byte
	ByteSize          domain.UInt64
	EventID           domain.ActivityEventID
	CompletedAt       time.Time
}

type RequestMediaFramesInput struct {
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
	Times          []domain.RationalTime `json:"times" minItems:"1" maxItems:"8"`
}

type MediaFrameSetRequestStatus string

const (
	MediaFrameSetAccepted MediaFrameSetRequestStatus = "accepted"
	MediaFrameSetReady    MediaFrameSetRequestStatus = "ready"
)

type MediaFrameSetRequestResult struct {
	Status         MediaFrameSetRequestStatus `json:"status" enum:"accepted,ready"`
	Job            domain.MediaJobSummary     `json:"job"`
	ArtifactID     *domain.ArtifactID         `json:"artifactId,omitempty"`
	Resources      []FrameResourceLease       `json:"resources" maxItems:"8" nullable:"false"`
	ActivityCursor domain.Cursor              `json:"activityCursor"`
}

type FrameResourceLease struct {
	ResourceID    domain.ResourceID   `json:"resourceId" format:"uuid"`
	MIMEType      string              `json:"mimeType" enum:"image/png"`
	ByteSize      domain.UInt64       `json:"byteSize" format:"uint64-decimal"`
	SHA256        domain.Digest       `json:"sha256" format:"sha256-digest"`
	RequestedTime domain.RationalTime `json:"requestedTime"`
	SourceTime    domain.RationalTime `json:"sourceTime"`
	ReadOnlyPath  string              `json:"readOnlyPath"`
	ExpiresAt     time.Time           `json:"expiresAt"`
}

type MaterializeMediaFrameLeasesRecord struct {
	ProjectID  domain.ProjectID
	AssetID    domain.AssetID
	RunID      domain.RunID
	TurnID     domain.TurnID
	Actor      domain.ActorRef
	JobID      domain.MediaJobID
	ArtifactID domain.ArtifactID
	Resources  []domain.ResourceID
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

type RequestMediaFrameSetRecord struct {
	JobID            domain.MediaJobID
	ProjectID        domain.ProjectID
	AssetID          domain.AssetID
	RunID            domain.RunID
	TurnID           domain.TurnID
	Actor            domain.ActorRef
	Parameters       MediaFrameSetParameters
	ParametersJSON   []byte
	ParametersDigest domain.Digest
	LogicalKey       string
	ActivityEventID  domain.ActivityEventID
	RequestedAt      time.Time
}

type MediaFrameSetRepository interface {
	ReadAssetDetail(context.Context, domain.ProjectID, domain.AssetID) (domain.AssetDetail, domain.Cursor, error)
	RequestMediaFrameSet(context.Context, RequestMediaFrameSetRecord) (MediaFrameSetRequestResult, error)
	MaterializeMediaFrameLeases(context.Context, MaterializeMediaFrameLeasesRecord) ([]FrameResourceLease, error)
}

type MediaFrames struct {
	repository MediaFrameSetRepository
	identities IdentityGenerator
	clock      Clock
}

func NewMediaFrames(
	repository MediaFrameSetRepository,
	identities IdentityGenerator,
	clock Clock,
) (*MediaFrames, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("media frame application dependencies are required")
	}
	return &MediaFrames{repository: repository, identities: identities, clock: clock}, nil
}

func (frames *MediaFrames) Request(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	runID domain.RunID,
	turnID domain.TurnID,
	input RequestMediaFramesInput,
) (MediaFrameSetRequestResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return MediaFrameSetRequestResult{}, err
	}
	if projectID.IsZero() || assetID.IsZero() || runID.IsZero() || turnID.IsZero() ||
		input.SourceStreamID.IsZero() || len(input.Times) == 0 || len(input.Times) > MaximumFrameSetSamples {
		return MediaFrameSetRequestResult{}, ErrAssetInvalid
	}
	detail, _, err := frames.repository.ReadAssetDetail(ctx, projectID, assetID)
	if err != nil {
		return MediaFrameSetRequestResult{}, err
	}
	if detail.Asset.AcceptedFingerprint == nil || detail.Fingerprint == nil ||
		*detail.Asset.AcceptedFingerprint != *detail.Fingerprint || detail.Facts == nil ||
		(detail.Availability != domain.AssetOnline && detail.Availability != domain.AssetManagedState) {
		return MediaFrameSetRequestResult{}, ErrAssetInvalid
	}
	streamFound := false
	for _, stream := range detail.Facts.Streams {
		if stream.ID == input.SourceStreamID && stream.Descriptor.MediaType == domain.MediaVideo {
			streamFound = true
			break
		}
	}
	parameters := MediaFrameSetParameters{
		AssetID: assetID, Fingerprint: *detail.Asset.AcceptedFingerprint,
		SourceStreamID: input.SourceStreamID, Times: append([]domain.RationalTime(nil), input.Times...),
		Profile: FrameSetProfile,
	}
	parametersJSON, parametersDigest, err := CanonicalFrameSetParameters(parameters)
	if err != nil || !streamFound {
		return MediaFrameSetRequestResult{}, ErrAssetInvalid
	}
	now := frames.clock.Now().UTC()
	jobValue, err := frames.identities.NewID(ctx, now)
	if err != nil {
		return MediaFrameSetRequestResult{}, err
	}
	jobID, err := domain.ParseMediaJobID(jobValue)
	if err != nil {
		return MediaFrameSetRequestResult{}, err
	}
	eventValue, err := frames.identities.NewID(ctx, now)
	if err != nil {
		return MediaFrameSetRequestResult{}, err
	}
	eventID, err := domain.ParseActivityEventID(eventValue)
	if err != nil {
		return MediaFrameSetRequestResult{}, err
	}
	result, err := frames.repository.RequestMediaFrameSet(ctx, RequestMediaFrameSetRecord{
		JobID: jobID, ProjectID: projectID, AssetID: assetID, RunID: runID, TurnID: turnID,
		Actor: authority.Actor, Parameters: parameters, ParametersJSON: parametersJSON,
		ParametersDigest: parametersDigest,
		LogicalKey:       "media/v1/" + assetID.String() + "/" + string(domain.MediaJobFrameSet) + "/" + parametersDigest.String(),
		ActivityEventID:  eventID, RequestedAt: now,
	})
	if result.Resources == nil {
		result.Resources = []FrameResourceLease{}
	}
	if err != nil || result.Status != MediaFrameSetReady || result.ArtifactID == nil {
		return result, err
	}
	resources := make([]domain.ResourceID, len(parameters.Times))
	for index := range resources {
		value, identityErr := frames.identities.NewID(ctx, now)
		if identityErr != nil {
			return MediaFrameSetRequestResult{}, identityErr
		}
		resources[index], identityErr = domain.ParseResourceID(value)
		if identityErr != nil {
			return MediaFrameSetRequestResult{}, identityErr
		}
	}
	result.Resources, err = frames.repository.MaterializeMediaFrameLeases(ctx, MaterializeMediaFrameLeasesRecord{
		ProjectID: projectID, AssetID: assetID, RunID: runID, TurnID: turnID, Actor: authority.Actor,
		JobID: result.Job.ID, ArtifactID: *result.ArtifactID, Resources: resources,
		CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	})
	return result, err
}
