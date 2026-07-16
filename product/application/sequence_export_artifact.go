package application

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	MaximumSequenceExportManifestSize = 1 << 20
	MaximumSequenceExportArtifactSize = 1 << 40
)

type SequenceExportArtifactFile struct {
	Path     string        `json:"path"`
	MimeType string        `json:"mimeType"`
	ByteSize domain.UInt64 `json:"byteSize" format:"uint64-decimal"`
	SHA256   domain.Digest `json:"sha256"`
}

type SequenceExportArtifactManifest struct {
	ProducerJobID    domain.WorkJobID           `json:"producerJobId"`
	ProjectID        domain.ProjectID           `json:"projectId"`
	SequenceID       domain.SequenceID          `json:"sequenceId"`
	SequenceRevision domain.Revision            `json:"sequenceRevision"`
	RenderPlanDigest domain.Digest              `json:"renderPlanDigest"`
	RendererVersion  string                     `json:"rendererVersion"`
	RendererTarget   string                     `json:"rendererTarget"`
	Profile          string                     `json:"profile"`
	Facts            domain.RenderedMediaFacts  `json:"facts"`
	Media            SequenceExportArtifactFile `json:"media"`
}

type SequenceExportRenderExecution struct {
	Media     SequenceExportArtifactFile
	Workspace PreparedMediaWorkspace
}

type CompleteSequenceExport struct {
	Claim             WorkJobClaim
	ArtifactID        domain.ArtifactID
	Plan              PublishedRenderPlan
	Manifest          SequenceExportArtifactManifest
	ManifestCanonical []byte
	ContentDigest     domain.Digest
	ByteSize          domain.UInt64
	Workspace         PreparedMediaWorkspace
	EventID           domain.ActivityEventID
	CompletedAt       time.Time
}

func (manifest SequenceExportArtifactManifest) Validate() error {
	if manifest.ProducerJobID.IsZero() || manifest.ProjectID.IsZero() || manifest.SequenceID.IsZero() ||
		manifest.SequenceRevision.Value() == 0 || manifest.RenderPlanDigest == "" ||
		manifest.RendererVersion == "" || len(manifest.RendererVersion) > 1024 ||
		!validPreviewTarget(manifest.RendererTarget) || manifest.Profile != domain.SequenceExportProfileV1 ||
		ValidateSequenceExportFacts(manifest.Facts) != nil || manifest.Media.Path != "export.webm" ||
		manifest.Media.MimeType != "video/webm" || manifest.Media.ByteSize.Value() == 0 ||
		manifest.Media.ByteSize.Value() > MaximumSequenceExportArtifactSize {
		return ErrSequenceExportInvalid
	}
	if _, err := domain.ParseDigest(manifest.RenderPlanDigest.String()); err != nil {
		return ErrSequenceExportInvalid
	}
	if _, err := domain.ParseDigest(manifest.Media.SHA256.String()); err != nil {
		return ErrSequenceExportInvalid
	}
	return nil
}

func DecodeSequenceExportArtifactManifest(data []byte) (SequenceExportArtifactManifest, error) {
	if len(data) == 0 || len(data) > MaximumSequenceExportManifestSize {
		return SequenceExportArtifactManifest{}, ErrSequenceExportInvalid
	}
	var envelope struct {
		Domain  string                         `json:"domain"`
		Payload SequenceExportArtifactManifest `json:"payload"`
		Schema  string                         `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/sequence-export-artifact" ||
		envelope.Schema != domain.SequenceExportArtifactSchema || envelope.Payload.Validate() != nil {
		return SequenceExportArtifactManifest{}, ErrSequenceExportInvalid
	}
	return envelope.Payload, nil
}

func SequenceExportFactsForPlan(plan domain.RenderPlanPayload) (domain.RenderedMediaFacts, error) {
	if ValidateSequenceExportRenderPlanPayload(plan) != nil || plan.Purpose != domain.RenderPurposeExport ||
		plan.Duration.Validate() != nil || !plan.Duration.IsPositive() ||
		plan.Output.Profile != domain.SequenceExportProfileV1 || plan.Output.VideoFrameCount.Value() == 0 ||
		plan.Output.VideoFrameCount.Value() > MaximumSequencePreviewVideoFrames ||
		plan.Output.AudioSampleCount.Value() == 0 ||
		plan.Output.AudioSampleCount.Value() > MaximumSequencePreviewAudioSamples {
		return domain.RenderedMediaFacts{}, ErrRenderPlanInvalid
	}
	presentation, err := sequencePreviewPresentationDuration(plan)
	if err != nil {
		return domain.RenderedMediaFacts{}, err
	}
	facts := domain.RenderedMediaFacts{
		SemanticDuration: plan.Duration, PresentationDuration: presentation,
		CanvasWidth: plan.Output.CanvasWidth, CanvasHeight: plan.Output.CanvasHeight,
		FrameRate: plan.Output.FrameRate, VideoFrameCount: plan.Output.VideoFrameCount,
		AudioSampleRate: domain.SequencePreviewAudioSampleRate, AudioSampleCount: plan.Output.AudioSampleCount,
		VideoCodec: "vp9", AudioCodec: "opus", PixelFormat: "yuv420p", ChannelLayout: "stereo",
	}
	if ValidateSequenceExportFacts(facts) != nil {
		return domain.RenderedMediaFacts{}, ErrRenderPlanInvalid
	}
	return facts, nil
}

// RenderedMediaFactsForPlan projects the common renderer output facts while
// preserving purpose-specific plan validation at the application boundary.
func RenderedMediaFactsForPlan(plan domain.RenderPlanPayload) (domain.RenderedMediaFacts, error) {
	switch plan.Purpose {
	case domain.RenderPurposeSequencePreview:
		return SequencePreviewFactsForPlan(plan)
	case domain.RenderPurposeExport:
		return SequenceExportFactsForPlan(plan)
	default:
		return domain.RenderedMediaFacts{}, ErrRenderPlanInvalid
	}
}

func ValidateSequenceExportFacts(facts domain.RenderedMediaFacts) error {
	if facts.SemanticDuration.Validate() != nil || !facts.SemanticDuration.IsPositive() ||
		facts.PresentationDuration.Validate() != nil || !facts.PresentationDuration.IsPositive() ||
		facts.CanvasWidth < 2 || facts.CanvasHeight < 2 || facts.CanvasWidth%2 != 0 || facts.CanvasHeight%2 != 0 ||
		facts.CanvasWidth > MaximumRenderMaterialDimension || facts.CanvasHeight > MaximumRenderMaterialDimension ||
		facts.FrameRate.Validate() != nil || !facts.FrameRate.IsPositive() ||
		facts.VideoFrameCount.Value() == 0 || facts.VideoFrameCount.Value() > MaximumSequencePreviewVideoFrames ||
		facts.AudioSampleRate != domain.SequencePreviewAudioSampleRate || facts.AudioSampleCount.Value() == 0 ||
		facts.AudioSampleCount.Value() > MaximumSequencePreviewAudioSamples || facts.VideoCodec != "vp9" ||
		facts.AudioCodec != "opus" || facts.PixelFormat != "yuv420p" || facts.ChannelLayout != "stereo" {
		return ErrSequenceExportInvalid
	}
	comparison, err := facts.PresentationDuration.Compare(facts.SemanticDuration)
	if err != nil || comparison < 0 {
		return ErrSequenceExportInvalid
	}
	return nil
}
