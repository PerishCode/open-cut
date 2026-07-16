package application

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	MaximumSequencePreviewManifestSize = 1 << 20
	MaximumSequencePreviewVideoFrames  = 10_000_000
	MaximumSequencePreviewAudioSamples = 2_000_000_000
	MaximumSequencePreviewArtifactSize = 16 << 30
)

type SequencePreviewArtifactFile struct {
	Path     string        `json:"path"`
	MimeType string        `json:"mimeType"`
	ByteSize domain.UInt64 `json:"byteSize" format:"uint64-decimal"`
	SHA256   domain.Digest `json:"sha256"`
}

type SequencePreviewArtifactManifest struct {
	ProjectID        domain.ProjectID                 `json:"projectId"`
	SequenceID       domain.SequenceID                `json:"sequenceId"`
	SequenceRevision domain.Revision                  `json:"sequenceRevision"`
	RenderPlanDigest domain.Digest                    `json:"renderPlanDigest"`
	RendererVersion  string                           `json:"rendererVersion"`
	RendererTarget   string                           `json:"rendererTarget"`
	Profile          string                           `json:"profile"`
	Facts            domain.SequencePreviewMediaFacts `json:"facts"`
	Media            SequencePreviewArtifactFile      `json:"media"`
}

type SequencePreviewRenderExecution struct {
	Media     SequencePreviewArtifactFile
	Workspace PreparedMediaWorkspace
}

type CompleteSequencePreview struct {
	Claim             WorkJobClaim
	ArtifactID        domain.ArtifactID
	Plan              PublishedRenderPlan
	Manifest          SequencePreviewArtifactManifest
	ManifestCanonical []byte
	ContentDigest     domain.Digest
	ByteSize          domain.UInt64
	Workspace         PreparedMediaWorkspace
	EventID           domain.ActivityEventID
	CompletedAt       time.Time
}

func (manifest SequencePreviewArtifactManifest) Validate() error {
	if manifest.ProjectID.IsZero() || manifest.SequenceID.IsZero() ||
		manifest.SequenceRevision.Value() == 0 || manifest.RenderPlanDigest == "" ||
		manifest.RendererVersion == "" || len(manifest.RendererVersion) > 1024 ||
		!validPreviewTarget(manifest.RendererTarget) || manifest.Profile != domain.SequencePreviewProfileV1 ||
		ValidateSequencePreviewFacts(manifest.Facts) != nil ||
		manifest.Media.Path != "preview.webm" || manifest.Media.MimeType != "video/webm" ||
		manifest.Media.ByteSize.Value() == 0 || manifest.Media.ByteSize.Value() > MaximumSequencePreviewArtifactSize {
		return ErrSequencePreviewInvalid
	}
	if _, err := domain.ParseDigest(manifest.RenderPlanDigest.String()); err != nil {
		return ErrSequencePreviewInvalid
	}
	if _, err := domain.ParseDigest(manifest.Media.SHA256.String()); err != nil {
		return ErrSequencePreviewInvalid
	}
	return nil
}

func DecodeSequencePreviewArtifactManifest(data []byte) (SequencePreviewArtifactManifest, error) {
	if len(data) == 0 || len(data) > MaximumSequencePreviewManifestSize {
		return SequencePreviewArtifactManifest{}, ErrSequencePreviewInvalid
	}
	var envelope struct {
		Domain  string                          `json:"domain"`
		Payload SequencePreviewArtifactManifest `json:"payload"`
		Schema  string                          `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/sequence-preview-artifact" ||
		envelope.Schema != domain.SequencePreviewArtifactSchema || envelope.Payload.Validate() != nil {
		return SequencePreviewArtifactManifest{}, ErrSequencePreviewInvalid
	}
	return envelope.Payload, nil
}

func SequencePreviewFactsForPlan(
	plan domain.RenderPlanPayload,
) (domain.SequencePreviewMediaFacts, error) {
	if ValidateSequencePreviewRenderPlanPayload(plan) != nil ||
		plan.Purpose != domain.RenderPurposeSequencePreview || plan.Duration.Validate() != nil ||
		!plan.Duration.IsPositive() || plan.Output.Profile != domain.SequencePreviewProfileV1 ||
		plan.Output.VideoFrameCount.Value() == 0 ||
		plan.Output.VideoFrameCount.Value() > MaximumSequencePreviewVideoFrames ||
		plan.Output.AudioSampleCount.Value() == 0 ||
		plan.Output.AudioSampleCount.Value() > MaximumSequencePreviewAudioSamples {
		return domain.SequencePreviewMediaFacts{}, ErrRenderPlanInvalid
	}
	presentation, err := sequencePreviewPresentationDuration(plan)
	if err != nil {
		return domain.SequencePreviewMediaFacts{}, err
	}
	facts := domain.SequencePreviewMediaFacts{
		SemanticDuration: plan.Duration, PresentationDuration: presentation,
		CanvasWidth: plan.Output.CanvasWidth, CanvasHeight: plan.Output.CanvasHeight,
		FrameRate: plan.Output.FrameRate, VideoFrameCount: plan.Output.VideoFrameCount,
		AudioSampleRate:  domain.SequencePreviewAudioSampleRate,
		AudioSampleCount: plan.Output.AudioSampleCount,
		VideoCodec:       "vp9", AudioCodec: "opus", PixelFormat: "yuv420p", ChannelLayout: "stereo",
	}
	if ValidateSequencePreviewFacts(facts) != nil {
		return domain.SequencePreviewMediaFacts{}, ErrRenderPlanInvalid
	}
	return facts, nil
}

func ValidateSequencePreviewFacts(facts domain.SequencePreviewMediaFacts) error {
	if facts.SemanticDuration.Validate() != nil || !facts.SemanticDuration.IsPositive() ||
		facts.PresentationDuration.Validate() != nil || !facts.PresentationDuration.IsPositive() ||
		facts.CanvasWidth < 2 || facts.CanvasHeight < 2 || facts.CanvasWidth%2 != 0 || facts.CanvasHeight%2 != 0 ||
		facts.CanvasWidth > domain.SequencePreviewMaximumLongEdge ||
		facts.CanvasHeight > domain.SequencePreviewMaximumLongEdge ||
		facts.FrameRate.Validate() != nil || !facts.FrameRate.IsPositive() ||
		facts.VideoFrameCount.Value() == 0 || facts.VideoFrameCount.Value() > MaximumSequencePreviewVideoFrames ||
		facts.AudioSampleRate != domain.SequencePreviewAudioSampleRate ||
		facts.AudioSampleCount.Value() == 0 || facts.AudioSampleCount.Value() > MaximumSequencePreviewAudioSamples ||
		facts.VideoCodec != "vp9" || facts.AudioCodec != "opus" || facts.PixelFormat != "yuv420p" ||
		facts.ChannelLayout != "stereo" {
		return ErrSequencePreviewInvalid
	}
	comparison, err := facts.PresentationDuration.Compare(facts.SemanticDuration)
	if err != nil || comparison < 0 {
		return ErrSequencePreviewInvalid
	}
	return nil
}

func sequencePreviewPresentationDuration(plan domain.RenderPlanPayload) (domain.RationalTime, error) {
	video := new(big.Rat).SetFrac(
		new(big.Int).Mul(
			new(big.Int).SetUint64(plan.Output.VideoFrameCount.Value()),
			big.NewInt(int64(plan.Output.FrameRate.Scale)),
		),
		big.NewInt(plan.Output.FrameRate.Value.Value()),
	)
	audio := new(big.Rat).SetFrac(
		new(big.Int).SetUint64(plan.Output.AudioSampleCount.Value()),
		big.NewInt(domain.SequencePreviewAudioSampleRate),
	)
	presentation := video
	if audio.Cmp(video) > 0 {
		presentation = audio
	}
	numerator, denominator := presentation.Num(), presentation.Denom()
	if !numerator.IsInt64() || !denominator.IsInt64() || denominator.Int64() > math.MaxInt32 {
		return domain.RationalTime{}, ErrRenderPlanInvalid
	}
	return domain.NewRationalTime(numerator.Int64(), int32(denominator.Int64()))
}
