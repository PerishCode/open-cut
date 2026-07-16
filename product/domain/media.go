package domain

import (
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AssetRegisterProposalSchema    = "open-cut/edit-proposal/asset-register/v1"
	AssetRegisterTransactionSchema = "open-cut/edit-transaction/asset-register/v1"
	MediaJobParametersSchema       = "open-cut/media-job-parameters/v1"
	MediaFactsSchema               = "open-cut/media-facts/v1"
)

type SourceGrantState string

const (
	SourceGrantActive      SourceGrantState = "active"
	SourceGrantRevoked     SourceGrantState = "revoked"
	SourceGrantUnavailable SourceGrantState = "unavailable"
)

type SourceGrantKind string

const (
	SourceGrantLocalPath   SourceGrantKind = "local-path-v1"
	SourceGrantMacBookmark SourceGrantKind = "mac-security-scoped-bookmark-v1"
)

type SourceObservation struct {
	ByteSize       UInt64 `json:"byteSize" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	ModifiedUnixNs Int64  `json:"modifiedUnixNs" format:"int64-decimal" pattern:"^(0|-?[1-9][0-9]*)$"`
	FileIdentity   string `json:"fileIdentity" minLength:"1" maxLength:"512"`
}

type SourceGrantSummary struct {
	ID          SourceGrantID     `json:"id"`
	Platform    string            `json:"platform" enum:"mac,win,linux"`
	Kind        SourceGrantKind   `json:"kind" enum:"local-path-v1,mac-security-scoped-bookmark-v1"`
	DisplayName string            `json:"displayName" minLength:"1" maxLength:"512"`
	Observation SourceObservation `json:"observation"`
	State       SourceGrantState  `json:"state" enum:"active,revoked,unavailable"`
	CreatedAt   time.Time         `json:"createdAt"`
}

type AssetImportMode string

const (
	AssetReferenced AssetImportMode = "referenced"
	AssetManaged    AssetImportMode = "managed"
)

type AssetAvailability string

const (
	AssetIdentifying  AssetAvailability = "identifying"
	AssetOnline       AssetAvailability = "online"
	AssetChanged      AssetAvailability = "changed"
	AssetMissing      AssetAvailability = "missing"
	AssetManagedState AssetAvailability = "managed"
	AssetUnreadable   AssetAvailability = "unreadable"
)

type AssetState struct {
	ID                  AssetID         `json:"id"`
	Revision            Revision        `json:"revision"`
	ProjectID           ProjectID       `json:"projectId"`
	SourceGrantID       SourceGrantID   `json:"sourceGrantId"`
	DisplayName         string          `json:"displayName" minLength:"1" maxLength:"512"`
	ImportMode          AssetImportMode `json:"importMode" enum:"referenced,managed"`
	AcceptedFingerprint *Digest         `json:"acceptedFingerprint,omitempty"`
	Tombstoned          bool            `json:"tombstoned"`
}

type MediaType string

const (
	MediaVideo      MediaType = "video"
	MediaAudio      MediaType = "audio"
	MediaSubtitle   MediaType = "subtitle"
	MediaData       MediaType = "data"
	MediaAttachment MediaType = "attachment"
	MediaOther      MediaType = "other"
)

var ErrInvalidMediaFacts = errors.New("invalid media facts")

type SourceStream struct {
	ID         SourceStreamID         `json:"id"`
	Descriptor SourceStreamDescriptor `json:"descriptor"`
}

type SourceStreamDescriptor struct {
	Index        uint32            `json:"index"`
	MediaType    MediaType         `json:"mediaType" enum:"video,audio,subtitle,data,attachment,other"`
	Codec        string            `json:"codec" minLength:"1" maxLength:"128"`
	CodecProfile string            `json:"codecProfile,omitempty" maxLength:"128"`
	CodecTag     string            `json:"codecTag,omitempty" maxLength:"64"`
	TimeBase     RationalTime      `json:"timeBase"`
	StartTime    *RationalTime     `json:"startTime,omitempty"`
	Duration     *RationalTime     `json:"duration,omitempty"`
	Language     string            `json:"language,omitempty" maxLength:"64"`
	Dispositions []string          `json:"dispositions" maxItems:"32" nullable:"false"`
	Video        *VideoStreamFacts `json:"video,omitempty"`
	Audio        *AudioStreamFacts `json:"audio,omitempty"`
}

type VideoStreamFacts struct {
	Width          uint32        `json:"width" minimum:"1" maximum:"32768"`
	Height         uint32        `json:"height" minimum:"1" maximum:"32768"`
	CodedWidth     uint32        `json:"codedWidth,omitempty" maximum:"32768"`
	CodedHeight    uint32        `json:"codedHeight,omitempty" maximum:"32768"`
	PixelAspect    *RationalTime `json:"pixelAspect,omitempty"`
	AverageRate    *RationalTime `json:"averageRate,omitempty"`
	NominalRate    *RationalTime `json:"nominalRate,omitempty"`
	Rotation       int16         `json:"rotation" enum:"0,90,180,270"`
	PixelFormat    string        `json:"pixelFormat,omitempty" maxLength:"64"`
	ColorRange     string        `json:"colorRange,omitempty" maxLength:"64"`
	ColorSpace     string        `json:"colorSpace,omitempty" maxLength:"64"`
	ColorTransfer  string        `json:"colorTransfer,omitempty" maxLength:"64"`
	ColorPrimaries string        `json:"colorPrimaries,omitempty" maxLength:"64"`
}

type AudioStreamFacts struct {
	SampleFormat  string `json:"sampleFormat,omitempty" maxLength:"64"`
	SampleRate    uint32 `json:"sampleRate" minimum:"1" maximum:"768000"`
	Channels      uint16 `json:"channels" minimum:"1" maximum:"64"`
	ChannelLayout string `json:"channelLayout,omitempty" maxLength:"128"`
}

type MediaFacts struct {
	Container        string         `json:"container" minLength:"1" maxLength:"128"`
	ContainerAliases []string       `json:"containerAliases" maxItems:"32" nullable:"false"`
	StartTime        *RationalTime  `json:"startTime,omitempty"`
	Duration         *RationalTime  `json:"duration,omitempty"`
	BitRate          *UInt64        `json:"bitRate,omitempty" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Streams          []SourceStream `json:"streams" maxItems:"64" nullable:"false"`
}

func (facts MediaFacts) Validate() error {
	if !validMediaText(facts.Container, 128, true) || len(facts.ContainerAliases) > 32 ||
		len(facts.Streams) == 0 || len(facts.Streams) > 64 {
		return ErrInvalidMediaFacts
	}
	if facts.StartTime != nil && facts.StartTime.Validate() != nil {
		return ErrInvalidMediaFacts
	}
	if facts.Duration != nil && (facts.Duration.Validate() != nil || facts.Duration.IsNegative()) {
		return ErrInvalidMediaFacts
	}
	aliases := make(map[string]struct{}, len(facts.ContainerAliases))
	for _, alias := range facts.ContainerAliases {
		if !validMediaText(alias, 128, true) {
			return ErrInvalidMediaFacts
		}
		if _, duplicate := aliases[alias]; duplicate {
			return ErrInvalidMediaFacts
		}
		aliases[alias] = struct{}{}
	}
	streamIDs := make(map[string]struct{}, len(facts.Streams))
	indexes := make(map[uint32]struct{}, len(facts.Streams))
	for _, stream := range facts.Streams {
		if stream.ID.IsZero() || stream.Descriptor.Validate() != nil {
			return ErrInvalidMediaFacts
		}
		if _, duplicate := streamIDs[stream.ID.String()]; duplicate {
			return ErrInvalidMediaFacts
		}
		if _, duplicate := indexes[stream.Descriptor.Index]; duplicate {
			return ErrInvalidMediaFacts
		}
		streamIDs[stream.ID.String()] = struct{}{}
		indexes[stream.Descriptor.Index] = struct{}{}
	}
	return nil
}

func (descriptor SourceStreamDescriptor) Validate() error {
	if !validMediaText(descriptor.Codec, 128, true) ||
		!validMediaText(descriptor.CodecProfile, 128, false) ||
		!validMediaText(descriptor.CodecTag, 64, false) ||
		!validMediaText(descriptor.Language, 64, false) ||
		descriptor.TimeBase.Validate() != nil || !descriptor.TimeBase.IsPositive() ||
		len(descriptor.Dispositions) > 32 {
		return ErrInvalidMediaFacts
	}
	if descriptor.StartTime != nil && descriptor.StartTime.Validate() != nil {
		return ErrInvalidMediaFacts
	}
	if descriptor.Duration != nil && (descriptor.Duration.Validate() != nil || descriptor.Duration.IsNegative()) {
		return ErrInvalidMediaFacts
	}
	if !slices.IsSorted(descriptor.Dispositions) {
		return ErrInvalidMediaFacts
	}
	previous := ""
	for _, disposition := range descriptor.Dispositions {
		if !validMediaText(disposition, 64, true) || disposition == previous {
			return ErrInvalidMediaFacts
		}
		previous = disposition
	}
	switch descriptor.MediaType {
	case MediaVideo:
		if descriptor.Video == nil || descriptor.Audio != nil || descriptor.Video.Validate() != nil {
			return ErrInvalidMediaFacts
		}
	case MediaAudio:
		if descriptor.Audio == nil || descriptor.Video != nil || descriptor.Audio.Validate() != nil {
			return ErrInvalidMediaFacts
		}
	case MediaSubtitle, MediaData, MediaAttachment, MediaOther:
		if descriptor.Video != nil || descriptor.Audio != nil {
			return ErrInvalidMediaFacts
		}
	default:
		return ErrInvalidMediaFacts
	}
	return nil
}

func (facts VideoStreamFacts) Validate() error {
	if facts.Width < 1 || facts.Width > 32768 || facts.Height < 1 || facts.Height > 32768 ||
		facts.CodedWidth > 32768 || facts.CodedHeight > 32768 ||
		(facts.Rotation != 0 && facts.Rotation != 90 && facts.Rotation != 180 && facts.Rotation != 270) ||
		!validMediaText(facts.PixelFormat, 64, false) || !validMediaText(facts.ColorRange, 64, false) ||
		!validMediaText(facts.ColorSpace, 64, false) || !validMediaText(facts.ColorTransfer, 64, false) ||
		!validMediaText(facts.ColorPrimaries, 64, false) {
		return ErrInvalidMediaFacts
	}
	for _, rational := range []*RationalTime{facts.PixelAspect, facts.AverageRate, facts.NominalRate} {
		if rational != nil && (rational.Validate() != nil || !rational.IsPositive()) {
			return ErrInvalidMediaFacts
		}
	}
	return nil
}

func (facts AudioStreamFacts) Validate() error {
	if facts.SampleRate < 1 || facts.SampleRate > 768000 || facts.Channels < 1 || facts.Channels > 64 ||
		!validMediaText(facts.SampleFormat, 64, false) || !validMediaText(facts.ChannelLayout, 128, false) {
		return ErrInvalidMediaFacts
	}
	return nil
}

func validMediaText(value string, maximum int, required bool) bool {
	if !utf8.ValidString(value) || len([]byte(value)) > maximum || strings.TrimSpace(value) != value ||
		(required && value == "") {
		return false
	}
	for _, current := range value {
		if current < 0x20 || current == 0x7f {
			return false
		}
	}
	return true
}

type ArtifactKind string

const (
	ArtifactMediaFacts  ArtifactKind = "media-facts"
	ArtifactFrameSet    ArtifactKind = "frame-sample-set"
	ArtifactProxy       ArtifactKind = "proxy"
	ArtifactRenderInput ArtifactKind = "render-input"
	ArtifactWaveform    ArtifactKind = "waveform"
	ArtifactTranscript  ArtifactKind = "transcript"
)

type ArtifactState string

const (
	ArtifactReady   ArtifactState = "ready"
	ArtifactEvicted ArtifactState = "evicted"
)

type ArtifactSummary struct {
	ID               ArtifactID    `json:"id"`
	Kind             ArtifactKind  `json:"kind" enum:"media-facts,frame-sample-set,proxy,render-input,waveform,transcript"`
	ProducerVersion  string        `json:"producerVersion"`
	InputFingerprint Digest        `json:"inputFingerprint"`
	State            ArtifactState `json:"state" enum:"ready,evicted"`
	ByteSize         UInt64        `json:"byteSize"`
	ContentDigest    Digest        `json:"contentDigest"`
	CreatedAt        time.Time     `json:"createdAt"`
}

type WorkJobKind string
type MediaJobKind = WorkJobKind

const (
	MediaJobIdentify       MediaJobKind = "identify"
	MediaJobProbe          MediaJobKind = "probe"
	MediaJobFrameSet       MediaJobKind = "frame-sample-set"
	MediaJobProxy          MediaJobKind = "proxy"
	MediaJobRenderInput    MediaJobKind = "render-input"
	MediaJobWaveform       MediaJobKind = "waveform"
	MediaJobTranscript     MediaJobKind = "transcript"
	WorkJobSequencePreview WorkJobKind  = "sequence-preview"
	WorkJobSequenceFrames  WorkJobKind  = "sequence-frame-set"
	WorkJobSequenceExport  WorkJobKind  = "sequence-export"
)

type WorkJobState string
type MediaJobState = WorkJobState

const (
	MediaJobBlocked   MediaJobState = "blocked"
	MediaJobQueued    MediaJobState = "queued"
	MediaJobRunning   MediaJobState = "running"
	MediaJobSucceeded MediaJobState = "succeeded"
	MediaJobFailed    MediaJobState = "failed"
	MediaJobCancelled MediaJobState = "cancelled"
)

type MediaJobPrerequisiteKind string

const (
	MediaPrerequisiteFingerprint MediaJobPrerequisiteKind = "fingerprint-required"
	MediaPrerequisiteFacts       MediaJobPrerequisiteKind = "facts-required"
	MediaPrerequisiteModel       MediaJobPrerequisiteKind = "model-required"
	MediaPrerequisiteExecutor    MediaJobPrerequisiteKind = "executor-required"
)

type MediaJobPrerequisite struct {
	Kind       MediaJobPrerequisiteKind `json:"kind" enum:"fingerprint-required,facts-required,model-required,executor-required"`
	JobID      *MediaJobID              `json:"jobId,omitempty"`
	ResourceID string                   `json:"resourceId,omitempty" maxLength:"256"`
	Capability string                   `json:"capability,omitempty" maxLength:"256"`
}

func (prerequisite MediaJobPrerequisite) Validate() error {
	jobReference := prerequisite.JobID != nil && !prerequisite.JobID.IsZero()
	resourceReference := validMediaText(prerequisite.ResourceID, 256, true)
	capabilityReference := validMediaText(prerequisite.Capability, 256, true)
	switch prerequisite.Kind {
	case MediaPrerequisiteFingerprint, MediaPrerequisiteFacts:
		if !jobReference || prerequisite.ResourceID != "" || prerequisite.Capability != "" {
			return ErrInvalidMediaFacts
		}
	case MediaPrerequisiteModel:
		if jobReference || !resourceReference || prerequisite.Capability != "" {
			return ErrInvalidMediaFacts
		}
	case MediaPrerequisiteExecutor:
		if jobReference || prerequisite.ResourceID != "" || !capabilityReference {
			return ErrInvalidMediaFacts
		}
	default:
		return ErrInvalidMediaFacts
	}
	return nil
}

type MediaJobSummary struct {
	ID                  MediaJobID             `json:"id"`
	Kind                MediaJobKind           `json:"kind" enum:"identify,probe,frame-sample-set,proxy,render-input,waveform,transcript"`
	State               MediaJobState          `json:"state" enum:"blocked,queued,running,succeeded,failed,cancelled"`
	ProgressBasisPoints uint16                 `json:"progressBasisPoints" minimum:"0" maximum:"10000"`
	Prerequisites       []MediaJobPrerequisite `json:"prerequisites" maxItems:"8" nullable:"false"`
	TerminalErrorCode   *string                `json:"terminalErrorCode,omitempty" minLength:"1" maxLength:"256"`
	ResultArtifactID    *ArtifactID            `json:"resultArtifactId,omitempty"`
	CreatedAt           time.Time              `json:"createdAt"`
	UpdatedAt           time.Time              `json:"updatedAt"`
}

type AssetDetail struct {
	Asset        AssetState        `json:"asset"`
	Availability AssetAvailability `json:"availability" enum:"identifying,online,changed,missing,managed,unreadable"`
	Fingerprint  *Digest           `json:"fingerprint,omitempty"`
	Facts        *MediaFacts       `json:"facts,omitempty"`
	Artifacts    []ArtifactSummary `json:"artifacts" maxItems:"32" nullable:"false"`
	Jobs         []MediaJobSummary `json:"jobs" maxItems:"32" nullable:"false"`
}
