package domain

import "unicode"

const (
	RenderPlanSchema                 = "open-cut/render-plan/v4"
	RenderPlanCompilerV4             = "sequence-render-plan-v4"
	SequencePreviewProfileV1         = "webm-vp9-opus-sequence-preview-v1"
	SequenceExportProfileV1          = "webm-vp9-opus-v1"
	SequencePreviewArtifactSchema    = "open-cut/sequence-preview-artifact/v1"
	SequenceExportArtifactSchema     = "open-cut/sequence-export-artifact/v1"
	SequencePreviewMaximumLongEdge   = 1280
	SequencePreviewAudioSampleRate   = 48000
	SequencePreviewOpacityBasisPoint = 10_000
	RenderCoordinatePolicyV1         = "oriented-crop-fit-anchor-canvas-v1"
	RenderColorPipelineV1            = "rec709-left-linear-rgba16-integer-v1"
	RenderScalePolicyV1              = "pixel-center-mitchell-fixed-v1"
	RenderBlendPolicyV1              = "source-over-half-even-v1"
	RenderAudioGainPolicyV1          = "millidb-q31-integer-v1"
	RenderAudioMixPolicyV1           = "int64-sum-final-s16-hard-limit-v1"
	RenderCaptionLayoutPolicyV1      = "explicit-lines-bottom-box-v1"
	RenderCaptionRasterPolicyV1      = "harfbuzz-freetype-fribidi-gray-v1"
	RenderKeyframePolicyV1           = "frame-zero-two-second-grid-v1"
	RenderMuxPolicyV1                = "webm-bitexact-no-segmentuid-v1"
	RenderOpusTrimPolicyV1           = "exact-sample-count-discard-padding-v1"
	RenderDeterminismPolicyV1        = "same-build-target-byte-stable-v1"
	RenderGainMinimumMilliDB         = -96_000
	RenderGainMaximumMilliDB         = 24_000
)

type RenderPlanPurpose string

const (
	RenderPurposeSequencePreview RenderPlanPurpose = "sequence-preview"
	RenderPurposeExport          RenderPlanPurpose = "export"
)

// IsRenderCaptionRune closes the text controls understood by the first
// caption evaluator. Directional formatting controls are deliberately absent:
// bidi direction is a renderer policy, not hidden authored state. ZWNJ and ZWJ
// remain available for script shaping.
func IsRenderCaptionRune(value rune) bool {
	if value == '\n' || value == '\t' || value == '\u200c' || value == '\u200d' {
		return true
	}
	return !unicode.IsControl(value) && !unicode.In(value, unicode.Cf, unicode.Zl, unicode.Zp)
}

type RenderPlanInput struct {
	ArtifactID      ArtifactID        `json:"artifactId"`
	ArtifactDigest  Digest            `json:"artifactDigest"`
	ProducerVersion string            `json:"producerVersion"`
	Profile         string            `json:"profile"`
	AssetID         AssetID           `json:"assetId"`
	AssetRevision   Revision          `json:"assetRevision"`
	Fingerprint     Digest            `json:"fingerprint"`
	SourceEpoch     RationalTime      `json:"sourceEpoch"`
	MediaDigest     Digest            `json:"mediaDigest"`
	Video           *RenderVideoInput `json:"video,omitempty"`
	Audio           *RenderAudioInput `json:"audio,omitempty"`
}

type RenderVideoInput struct {
	SourceStreamID   SourceStreamID `json:"sourceStreamId"`
	SourceStart      RationalTime   `json:"sourceStart"`
	MaterialStart    RationalTime   `json:"materialStart"`
	SourceTimeBase   RationalTime   `json:"sourceTimeBase"`
	MaterialTimeBase RationalTime   `json:"materialTimeBase"`
	TimeMapDigest    Digest         `json:"timeMapDigest"`
	Width            uint32         `json:"width"`
	Height           uint32         `json:"height"`
}

type RenderAudioInput struct {
	SourceStreamID     SourceStreamID `json:"sourceStreamId"`
	SourceStart        RationalTime   `json:"sourceStart"`
	MaterialStart      RationalTime   `json:"materialStart"`
	SourceTimeBase     RationalTime   `json:"sourceTimeBase"`
	MaterialTimeBase   RationalTime   `json:"materialTimeBase"`
	SampleRate         uint32         `json:"sampleRate"`
	ChannelLayout      string         `json:"channelLayout"`
	DecodedSampleCount UInt64         `json:"decodedSampleCount"`
}

type RenderPlacement struct {
	CropXBasisPoints      uint16        `json:"cropXBasisPoints"`
	CropYBasisPoints      uint16        `json:"cropYBasisPoints"`
	CropWidthBasisPoints  uint16        `json:"cropWidthBasisPoints"`
	CropHeightBasisPoints uint16        `json:"cropHeightBasisPoints"`
	ScaleX                ExactRational `json:"scaleX"`
	ScaleY                ExactRational `json:"scaleY"`
	TranslateX            ExactRational `json:"translateX"`
	TranslateY            ExactRational `json:"translateY"`
	AnchorXBasisPoints    uint16        `json:"anchorXBasisPoints"`
	AnchorYBasisPoints    uint16        `json:"anchorYBasisPoints"`
	OpacityBasisPoints    uint16        `json:"opacityBasisPoints"`
	FitPolicy             string        `json:"fitPolicy"`
}

type RenderVideoInstruction struct {
	ClipID          ClipID          `json:"clipId"`
	ClipRevision    Revision        `json:"clipRevision"`
	TrackID         TrackID         `json:"trackId"`
	TrackRevision   Revision        `json:"trackRevision"`
	Layer           uint16          `json:"layer"`
	InputArtifactID ArtifactID      `json:"inputArtifactId"`
	SourceStreamID  SourceStreamID  `json:"sourceStreamId"`
	SourceRange     TimeRange       `json:"sourceRange"`
	TimelineRange   TimeRange       `json:"timelineRange"`
	Orientation     string          `json:"orientation"`
	Placement       RenderPlacement `json:"placement"`
}

type RenderAudioInstruction struct {
	ClipID          ClipID         `json:"clipId"`
	ClipRevision    Revision       `json:"clipRevision"`
	TrackID         TrackID        `json:"trackId"`
	TrackRevision   Revision       `json:"trackRevision"`
	Layer           uint16         `json:"layer"`
	InputArtifactID ArtifactID     `json:"inputArtifactId"`
	SourceStreamID  SourceStreamID `json:"sourceStreamId"`
	SourceRange     TimeRange      `json:"sourceRange"`
	TimelineRange   TimeRange      `json:"timelineRange"`
	ChannelMapping  string         `json:"channelMapping"`
	GainMilliDB     int32          `json:"gainMilliDb"`
}

type RenderFontResource struct {
	ResourceID string `json:"resourceId"`
	Version    string `json:"version"`
	SHA256     Digest `json:"sha256"`
}

type RenderCaptionStyle struct {
	FontResourceID        string `json:"fontResourceId"`
	FontSizeBasisPoint    uint16 `json:"fontSizeBasisPoints"`
	TextColorRGBA         string `json:"textColorRgba"`
	OutlineColorRGBA      string `json:"outlineColorRgba"`
	OutlineBasisPoints    uint16 `json:"outlineBasisPoints"`
	LineHeightBasisPoints uint16 `json:"lineHeightBasisPoints"`
	Alignment             string `json:"alignment"`
	PositionYBasisPoint   uint16 `json:"positionYBasisPoints"`
	SafeWidthBasisPoint   uint16 `json:"safeWidthBasisPoints"`
	WrapPolicy            string `json:"wrapPolicy"`
}

type RenderEvaluationPolicy struct {
	CoordinatePolicy    string `json:"coordinatePolicy"`
	ColorPipeline       string `json:"colorPipeline"`
	ScalePolicy         string `json:"scalePolicy"`
	BlendPolicy         string `json:"blendPolicy"`
	AudioGainPolicy     string `json:"audioGainPolicy"`
	AudioMixPolicy      string `json:"audioMixPolicy"`
	CaptionLayoutPolicy string `json:"captionLayoutPolicy"`
	CaptionRasterPolicy string `json:"captionRasterPolicy"`
	DeterminismPolicy   string `json:"determinismPolicy"`
}

type RenderMuxPolicy struct {
	MuxPolicy      string `json:"muxPolicy"`
	KeyframePolicy string `json:"keyframePolicy"`
	OpusTrimPolicy string `json:"opusTrimPolicy"`
}

type RenderCaptionInstruction struct {
	CaptionID       CaptionID          `json:"captionId"`
	CaptionRevision Revision           `json:"captionRevision"`
	TrackID         TrackID            `json:"trackId"`
	TrackRevision   Revision           `json:"trackRevision"`
	Layer           uint16             `json:"layer"`
	Range           TimeRange          `json:"range"`
	Language        CaptionLanguage    `json:"language"`
	Text            string             `json:"text"`
	Style           RenderCaptionStyle `json:"style"`
}

type RenderVideoOutputPolicy struct {
	Codec          string `json:"codec"`
	Encoder        string `json:"encoder"`
	EncoderProfile uint8  `json:"encoderProfile"`
	PixelFormat    string `json:"pixelFormat"`
	ColorRange     string `json:"colorRange"`
	ColorSpace     string `json:"colorSpace"`
	ColorTransfer  string `json:"colorTransfer"`
	ColorPrimaries string `json:"colorPrimaries"`
	ChromaLocation string `json:"chromaLocation"`
	RateControl    string `json:"rateControl"`
	CRF            uint8  `json:"crf"`
	Deadline       string `json:"deadline"`
	CPUUsed        uint8  `json:"cpuUsed"`
	ThreadCount    uint8  `json:"threadCount"`
}

type RenderAudioOutputPolicy struct {
	Codec            string `json:"codec"`
	Encoder          string `json:"encoder"`
	SampleRate       uint32 `json:"sampleRate"`
	ChannelLayout    string `json:"channelLayout"`
	BitRate          uint32 `json:"bitRate"`
	VariableBitRate  bool   `json:"variableBitRate"`
	FrameDurationMS  uint8  `json:"frameDurationMs"`
	CompressionLevel uint8  `json:"compressionLevel"`
	ClippingPolicy   string `json:"clippingPolicy"`
	PCMFormat        string `json:"pcmFormat"`
	DitherPolicy     string `json:"ditherPolicy"`
}

type RenderOutputPolicy struct {
	Profile             string                  `json:"profile"`
	Container           string                  `json:"container"`
	CanvasWidth         uint32                  `json:"canvasWidth"`
	CanvasHeight        uint32                  `json:"canvasHeight"`
	PixelAspect         RationalTime            `json:"pixelAspect"`
	FrameRate           RationalTime            `json:"frameRate"`
	VideoFrameCount     UInt64                  `json:"videoFrameCount" format:"uint64-decimal"`
	AudioSampleCount    UInt64                  `json:"audioSampleCount" format:"uint64-decimal"`
	StreamPolicy        string                  `json:"streamPolicy"`
	VideoSamplingPolicy string                  `json:"videoSamplingPolicy"`
	AudioSamplingPolicy string                  `json:"audioSamplingPolicy"`
	TailPolicy          string                  `json:"tailPolicy"`
	BackgroundRGBA      string                  `json:"backgroundRgba"`
	Evaluation          RenderEvaluationPolicy  `json:"evaluation"`
	Mux                 RenderMuxPolicy         `json:"mux"`
	Video               RenderVideoOutputPolicy `json:"video"`
	Audio               RenderAudioOutputPolicy `json:"audio"`
}

type RenderPlanPayload struct {
	CompilerVersion  string                     `json:"compilerVersion"`
	Purpose          RenderPlanPurpose          `json:"purpose"`
	ProjectID        ProjectID                  `json:"projectId"`
	SequenceID       SequenceID                 `json:"sequenceId"`
	SequenceRevision Revision                   `json:"sequenceRevision"`
	SequenceFormat   SequenceFormat             `json:"sequenceFormat"`
	Duration         RationalTime               `json:"duration"`
	Inputs           []RenderPlanInput          `json:"inputs"`
	Video            []RenderVideoInstruction   `json:"video"`
	Audio            []RenderAudioInstruction   `json:"audio"`
	Captions         []RenderCaptionInstruction `json:"captions"`
	FontResources    []RenderFontResource       `json:"fontResources"`
	Output           RenderOutputPolicy         `json:"output"`
}

type RenderPlan struct {
	Payload                 RenderPlanPayload `json:"payload"`
	Digest                  Digest            `json:"digest"`
	ObservedProjectRevision Revision          `json:"observedProjectRevision"`
}

type SequencePreviewArtifactState string

const (
	SequencePreviewArtifactReady   SequencePreviewArtifactState = "ready"
	SequencePreviewArtifactEvicted SequencePreviewArtifactState = "evicted"
)

type SequencePreviewMediaFacts struct {
	SemanticDuration     RationalTime `json:"semanticDuration"`
	PresentationDuration RationalTime `json:"presentationDuration"`
	CanvasWidth          uint32       `json:"canvasWidth"`
	CanvasHeight         uint32       `json:"canvasHeight"`
	FrameRate            RationalTime `json:"frameRate"`
	VideoFrameCount      UInt64       `json:"videoFrameCount" format:"uint64-decimal"`
	AudioSampleRate      uint32       `json:"audioSampleRate"`
	AudioSampleCount     UInt64       `json:"audioSampleCount" format:"uint64-decimal"`
	VideoCodec           string       `json:"videoCodec"`
	AudioCodec           string       `json:"audioCodec"`
	PixelFormat          string       `json:"pixelFormat"`
	ChannelLayout        string       `json:"channelLayout"`
}

type RenderedMediaFacts = SequencePreviewMediaFacts

type SequencePreviewArtifactSummary struct {
	ID               ArtifactID                   `json:"id"`
	ProjectID        ProjectID                    `json:"projectId"`
	SequenceID       SequenceID                   `json:"sequenceId"`
	SequenceRevision Revision                     `json:"sequenceRevision"`
	RenderPlanDigest Digest                       `json:"renderPlanDigest"`
	RendererVersion  string                       `json:"rendererVersion"`
	RendererTarget   string                       `json:"rendererTarget"`
	Profile          string                       `json:"profile"`
	State            SequencePreviewArtifactState `json:"state"`
	Facts            SequencePreviewMediaFacts    `json:"facts"`
	ByteSize         UInt64                       `json:"byteSize" format:"uint64-decimal"`
	ContentDigest    Digest                       `json:"contentDigest"`
}

type SequenceExportArtifactState string

const (
	SequenceExportArtifactValid   SequenceExportArtifactState = "valid"
	SequenceExportArtifactInvalid SequenceExportArtifactState = "invalid"
	SequenceExportArtifactDeleted SequenceExportArtifactState = "deleted"
)

type SequenceExportArtifactSummary struct {
	ID               ArtifactID                  `json:"id"`
	ProducerJobID    WorkJobID                   `json:"producerJobId"`
	ProjectID        ProjectID                   `json:"projectId"`
	SequenceID       SequenceID                  `json:"sequenceId"`
	SequenceRevision Revision                    `json:"sequenceRevision"`
	RenderPlanDigest Digest                      `json:"renderPlanDigest"`
	RendererVersion  string                      `json:"rendererVersion"`
	RendererTarget   string                      `json:"rendererTarget"`
	Profile          string                      `json:"profile"`
	State            SequenceExportArtifactState `json:"state" enum:"valid,invalid,deleted"`
	Facts            RenderedMediaFacts          `json:"facts"`
	ByteSize         UInt64                      `json:"byteSize" format:"uint64-decimal"`
	ContentDigest    Digest                      `json:"contentDigest"`
}
