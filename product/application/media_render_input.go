package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

const (
	MaximumRenderInputManifestSize = 1 << 20
	MaximumRenderInputFrames       = MaximumSourceProxyFrames
	MaximumRenderInputAudioSamples = MaximumSourceProxyAudioSamples
	MaximumRenderInputArtifactSize = 1 << 40
	MaximumRenderMaterialDimension = rendercontract.MaximumRenderDimension
)

type RenderInputArtifactFile struct {
	Path     string        `json:"path"`
	MimeType string        `json:"mimeType"`
	ByteSize domain.UInt64 `json:"byteSize"`
	SHA256   domain.Digest `json:"sha256"`
}

type RenderInputVideoTrack struct {
	Source              domain.SourceStream     `json:"source"`
	SourceStartTime     domain.RationalTime     `json:"sourceStartTime"`
	MaterialStartTime   domain.RationalTime     `json:"materialStartTime"`
	TimeBase            domain.RationalTime     `json:"timeBase"`
	Codec               string                  `json:"codec"`
	Width               uint32                  `json:"width"`
	Height              uint32                  `json:"height"`
	PixelFormat         string                  `json:"pixelFormat"`
	ColorRange          string                  `json:"colorRange"`
	ColorSpace          string                  `json:"colorSpace"`
	ColorTransfer       string                  `json:"colorTransfer"`
	ColorPrimaries      string                  `json:"colorPrimaries"`
	ColorInterpretation string                  `json:"colorInterpretation"`
	FrameCount          domain.UInt64           `json:"frameCount"`
	TimeMap             RenderInputArtifactFile `json:"timeMap"`
}

type RenderInputAudioTrack struct {
	Source             domain.SourceStream `json:"source"`
	SourceStartTime    domain.RationalTime `json:"sourceStartTime"`
	MaterialStartTime  domain.RationalTime `json:"materialStartTime"`
	TimeBase           domain.RationalTime `json:"timeBase"`
	Codec              string              `json:"codec"`
	SampleFormat       string              `json:"sampleFormat"`
	SampleRate         uint32              `json:"sampleRate"`
	Channels           uint16              `json:"channels"`
	ChannelLayout      string              `json:"channelLayout"`
	ChannelProjection  string              `json:"channelProjection"`
	DecodedSampleCount domain.UInt64       `json:"decodedSampleCount"`
}

type RenderInputArtifactManifest struct {
	AssetID     domain.AssetID          `json:"assetId"`
	Fingerprint domain.Digest           `json:"fingerprint"`
	Profile     string                  `json:"profile"`
	Producer    string                  `json:"producer"`
	SourceEpoch domain.RationalTime     `json:"sourceEpoch"`
	Media       RenderInputArtifactFile `json:"media"`
	Video       *RenderInputVideoTrack  `json:"video,omitempty"`
	Audio       *RenderInputAudioTrack  `json:"audio,omitempty"`
}

type MediaRenderInputExecution struct {
	SourceEpoch domain.RationalTime
	Media       RenderInputArtifactFile
	Video       *RenderInputVideoTrack
	Audio       *RenderInputAudioTrack
	Workspace   PreparedMediaWorkspace
}

type CompleteMediaRenderInput struct {
	Claim             MediaJobClaim
	ArtifactID        domain.ArtifactID
	Parameters        InitialMediaJobParameters
	Manifest          RenderInputArtifactManifest
	ManifestCanonical []byte
	ContentDigest     domain.Digest
	ByteSize          domain.UInt64
	Workspace         PreparedMediaWorkspace
	EventID           domain.ActivityEventID
	CompletedAt       time.Time
}

type EnsureExplicitRenderInputJobRecord struct {
	JobID        domain.WorkJobID
	ProjectID    domain.ProjectID
	AssetID      domain.AssetID
	Fingerprint  domain.Digest
	SourceStream domain.SourceStream
	Parameters   InitialMediaJobParameters
	Canonical    []byte
	Digest       domain.Digest
	LogicalKey   string
	CreatedAt    time.Time
}

func (manifest RenderInputArtifactManifest) Validate() error {
	if manifest.AssetID.IsZero() || manifest.Fingerprint == "" ||
		manifest.Profile != RenderInputProfile || manifest.Producer == "" || len(manifest.Producer) > 1024 ||
		manifest.SourceEpoch.Validate() != nil || (manifest.Video == nil) == (manifest.Audio == nil) ||
		validateRenderInputFile(manifest.Media, "render-input.mkv", "video/x-matroska") != nil {
		return domain.ErrInvalidMediaFacts
	}
	if _, err := domain.ParseDigest(manifest.Fingerprint.String()); err != nil {
		return domain.ErrInvalidMediaFacts
	}
	if manifest.Video != nil && validateRenderInputVideo(*manifest.Video, manifest.SourceEpoch) != nil {
		return domain.ErrInvalidMediaFacts
	}
	if manifest.Audio != nil && validateRenderInputAudio(*manifest.Audio, manifest.SourceEpoch) != nil {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func validateRenderInputVideo(track RenderInputVideoTrack, epoch domain.RationalTime) error {
	if track.Source.ID.IsZero() || track.Source.Descriptor.Validate() != nil ||
		track.Source.Descriptor.MediaType != domain.MediaVideo || track.Source.Descriptor.Video == nil ||
		track.SourceStartTime.Validate() != nil || track.MaterialStartTime.Validate() != nil ||
		track.MaterialStartTime.IsNegative() || track.TimeBase.Validate() != nil || !track.TimeBase.IsPositive() ||
		track.Codec != "ffv1" || track.PixelFormat != "yuv420p" || track.ColorRange != "tv" ||
		track.ColorSpace != "bt709" || track.ColorTransfer != "bt709" || track.ColorPrimaries != "bt709" ||
		(track.ColorInterpretation != "source-metadata" && track.ColorInterpretation != "assumed-bt709") ||
		track.Width < 2 || track.Height < 2 ||
		track.Width > MaximumRenderMaterialDimension || track.Height > MaximumRenderMaterialDimension ||
		track.Width%2 != 0 || track.Height%2 != 0 ||
		track.FrameCount.Value() == 0 || track.FrameCount.Value() > MaximumRenderInputFrames ||
		validateRenderInputFile(track.TimeMap, "video-time-map.bin", "application/vnd.open-cut.pts-map") != nil {
		return domain.ErrInvalidMediaFacts
	}
	expectedSize := uint64(sourceProxyTimeMapHeaderSize) +
		track.FrameCount.Value()*sourceProxyTimeMapRecordSize
	if track.TimeMap.ByteSize.Value() != expectedSize {
		return domain.ErrInvalidMediaFacts
	}
	return validateSourceProxyTrackStart(track.SourceStartTime, track.MaterialStartTime, epoch)
}

func validateRenderInputAudio(track RenderInputAudioTrack, epoch domain.RationalTime) error {
	if track.Source.ID.IsZero() || track.Source.Descriptor.Validate() != nil ||
		track.Source.Descriptor.MediaType != domain.MediaAudio || track.Source.Descriptor.Audio == nil ||
		track.SourceStartTime.Validate() != nil || track.MaterialStartTime.Validate() != nil ||
		track.MaterialStartTime.IsNegative() || track.TimeBase.Validate() != nil || !track.TimeBase.IsPositive() ||
		track.Codec != "pcm_s16le" || track.SampleFormat != "s16" || track.SampleRate != 48000 ||
		track.Channels != 2 || track.ChannelLayout != "stereo" || track.DecodedSampleCount.Value() == 0 ||
		track.DecodedSampleCount.Value() > MaximumRenderInputAudioSamples ||
		(track.ChannelProjection != "mono-duplicate-v1" && track.ChannelProjection != "stereo-pass-v1") {
		return domain.ErrInvalidMediaFacts
	}
	return validateSourceProxyTrackStart(track.SourceStartTime, track.MaterialStartTime, epoch)
}

func validateRenderInputFile(file RenderInputArtifactFile, path, mime string) error {
	if file.Path != path || file.MimeType != mime || file.ByteSize.Value() == 0 ||
		file.ByteSize.Value() > MaximumRenderInputArtifactSize {
		return domain.ErrInvalidMediaFacts
	}
	_, err := domain.ParseDigest(file.SHA256.String())
	return err
}

func CanonicalRenderInputArtifactManifest(
	manifest RenderInputArtifactManifest,
) ([]byte, domain.Digest, error) {
	if manifest.Validate() != nil {
		return nil, "", domain.ErrInvalidMediaFacts
	}
	return domain.CanonicalDigest("open-cut/render-input-artifact", RenderInputArtifactSchema, manifest)
}

func DecodeRenderInputArtifactManifest(data []byte) (RenderInputArtifactManifest, error) {
	var envelope struct {
		Domain  string                      `json:"domain"`
		Payload RenderInputArtifactManifest `json:"payload"`
		Schema  string                      `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/render-input-artifact" || envelope.Schema != RenderInputArtifactSchema ||
		envelope.Payload.Validate() != nil {
		return RenderInputArtifactManifest{}, fmt.Errorf("render-input manifest is invalid")
	}
	return envelope.Payload, nil
}
