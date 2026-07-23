package rendercontract

import (
	"math/big"

	"github.com/PerishCode/open-cut/product/domain"
)

// SequencePreviewOutput derives the complete deterministic preview output
// policy from the creative sequence format and duration.
func SequencePreviewOutput(
	format domain.SequenceFormat,
	duration domain.RationalTime,
) (domain.RenderOutputPolicy, error) {
	width, height, err := sequencePreviewDimensions(format)
	if err != nil {
		return domain.RenderOutputPolicy{}, err
	}
	videoFrameCount, err := ceilRenderCount(duration, format.FrameRate)
	if err != nil {
		return domain.RenderOutputPolicy{}, err
	}
	audioRate, _ := domain.NewRationalTime(domain.SequencePreviewAudioSampleRate, 1)
	audioSampleCount, err := ceilRenderCount(duration, audioRate)
	if err != nil {
		return domain.RenderOutputPolicy{}, err
	}
	if videoFrameCount.Value() > MaximumPreviewVideoFrames ||
		audioSampleCount.Value() > MaximumPreviewAudioSamples {
		return domain.RenderOutputPolicy{}, ErrRenderPlanInvalid
	}
	one, _ := domain.NewRationalTime(1, 1)
	return domain.RenderOutputPolicy{
		Profile: domain.SequencePreviewProfileV1, Container: "webm",
		CanvasWidth: width, CanvasHeight: height, PixelAspect: one, FrameRate: format.FrameRate,
		VideoFrameCount: videoFrameCount, AudioSampleCount: audioSampleCount,
		StreamPolicy:        "single-video-single-audio-v1",
		VideoSamplingPolicy: "source-map-floor-first-fallback-v1",
		AudioSamplingPolicy: "render-material-sample-floor-silence-v1",
		TailPolicy:          "ceil-pad-black-silence-v1",
		BackgroundRGBA:      "#000000ff",
		Evaluation: domain.RenderEvaluationPolicy{
			CoordinatePolicy:    domain.RenderCoordinatePolicyV1,
			ColorPipeline:       domain.RenderColorPipelineV1,
			ScalePolicy:         domain.RenderScalePolicyV1,
			BlendPolicy:         domain.RenderBlendPolicyV1,
			AudioGainPolicy:     domain.RenderAudioGainPolicyV1,
			AudioMixPolicy:      domain.RenderAudioMixPolicyV1,
			CaptionLayoutPolicy: domain.RenderCaptionLayoutPolicyV1,
			CaptionRasterPolicy: domain.RenderCaptionRasterPolicyV1,
			DeterminismPolicy:   domain.RenderDeterminismPolicyV1,
		},
		Mux: domain.RenderMuxPolicy{
			MuxPolicy:      domain.RenderMuxPolicyV1,
			KeyframePolicy: domain.RenderKeyframePolicyV1,
			OpusTrimPolicy: domain.RenderOpusTrimPolicyV1,
		},
		Video: domain.RenderVideoOutputPolicy{
			Codec: "vp9", Encoder: "libvpx-vp9", EncoderProfile: 0, PixelFormat: "yuv420p",
			ColorRange: "tv", ColorSpace: "bt709", ColorTransfer: "bt709", ColorPrimaries: "bt709",
			ChromaLocation: "left",
			RateControl:    "constant-quality", CRF: 34, Deadline: "good", CPUUsed: 4, ThreadCount: 1,
		},
		Audio: domain.RenderAudioOutputPolicy{
			Codec: "opus", Encoder: "libopus", SampleRate: domain.SequencePreviewAudioSampleRate,
			ChannelLayout: "stereo", BitRate: 128_000, VariableBitRate: false,
			FrameDurationMS: 20, CompressionLevel: 10, ClippingPolicy: "hard-limit-v1",
			PCMFormat: "s16", DitherPolicy: "none-v1",
		},
	}, nil
}

// SequenceExportOutput derives the complete deterministic first-slice export
// policy. Preview and export intentionally share evaluation semantics while
// retaining their independently versioned output profiles.
func SequenceExportOutput(
	format domain.SequenceFormat,
	duration domain.RationalTime,
) (domain.RenderOutputPolicy, error) {
	if format.Validate() != nil || format.CanvasWidth%2 != 0 || format.CanvasHeight%2 != 0 {
		return domain.RenderOutputPolicy{}, ErrRenderPlanInvalid
	}
	one, _ := domain.NewRationalTime(1, 1)
	if equal, err := format.PixelAspect.Compare(one); err != nil || equal != 0 {
		return domain.RenderOutputPolicy{}, ErrRenderPlanInvalid
	}
	videoFrameCount, err := ceilRenderCount(duration, format.FrameRate)
	if err != nil {
		return domain.RenderOutputPolicy{}, err
	}
	audioRate, _ := domain.NewRationalTime(domain.SequencePreviewAudioSampleRate, 1)
	audioSampleCount, err := ceilRenderCount(duration, audioRate)
	if err != nil || videoFrameCount.Value() > MaximumPreviewVideoFrames ||
		audioSampleCount.Value() > MaximumPreviewAudioSamples {
		return domain.RenderOutputPolicy{}, ErrRenderPlanInvalid
	}
	return domain.RenderOutputPolicy{
		Profile: domain.SequenceExportProfileV1, Container: "webm",
		CanvasWidth: format.CanvasWidth, CanvasHeight: format.CanvasHeight,
		PixelAspect: one, FrameRate: format.FrameRate,
		VideoFrameCount: videoFrameCount, AudioSampleCount: audioSampleCount,
		StreamPolicy:        "single-video-single-audio-v1",
		VideoSamplingPolicy: "source-map-floor-first-fallback-v1",
		AudioSamplingPolicy: "render-material-sample-floor-silence-v1",
		TailPolicy:          "ceil-pad-black-silence-v1",
		BackgroundRGBA:      "#000000ff",
		Evaluation: domain.RenderEvaluationPolicy{
			CoordinatePolicy:    domain.RenderCoordinatePolicyV1,
			ColorPipeline:       domain.RenderColorPipelineV1,
			ScalePolicy:         domain.RenderScalePolicyV1,
			BlendPolicy:         domain.RenderBlendPolicyV1,
			AudioGainPolicy:     domain.RenderAudioGainPolicyV1,
			AudioMixPolicy:      domain.RenderAudioMixPolicyV1,
			CaptionLayoutPolicy: domain.RenderCaptionLayoutPolicyV1,
			CaptionRasterPolicy: domain.RenderCaptionRasterPolicyV1,
			DeterminismPolicy:   domain.RenderDeterminismPolicyV1,
		},
		Mux: domain.RenderMuxPolicy{
			MuxPolicy: domain.RenderMuxPolicyV1, KeyframePolicy: domain.RenderKeyframePolicyV1,
			OpusTrimPolicy: domain.RenderOpusTrimPolicyV1,
		},
		Video: domain.RenderVideoOutputPolicy{
			Codec: "vp9", Encoder: "libvpx-vp9", EncoderProfile: 0, PixelFormat: "yuv420p",
			ColorRange: "tv", ColorSpace: "bt709", ColorTransfer: "bt709", ColorPrimaries: "bt709",
			ChromaLocation: "left", RateControl: "constant-quality", CRF: 24,
			Deadline: "good", CPUUsed: 2, ThreadCount: 1,
		},
		Audio: domain.RenderAudioOutputPolicy{
			Codec: "opus", Encoder: "libopus", SampleRate: domain.SequencePreviewAudioSampleRate,
			ChannelLayout: "stereo", BitRate: 192_000, VariableBitRate: false,
			FrameDurationMS: 20, CompressionLevel: 10, ClippingPolicy: "hard-limit-v1",
			PCMFormat: "s16", DitherPolicy: "none-v1",
		},
	}, nil
}

func ceilRenderCount(duration, rate domain.RationalTime) (domain.UInt64, error) {
	if duration.Validate() != nil || !duration.IsPositive() || rate.Validate() != nil || !rate.IsPositive() {
		return 0, ErrRenderPlanInvalid
	}
	numerator := new(big.Int).Mul(
		big.NewInt(duration.Value.Value()),
		big.NewInt(rate.Value.Value()),
	)
	denominator := new(big.Int).Mul(
		big.NewInt(int64(duration.Scale)),
		big.NewInt(int64(rate.Scale)),
	)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return 0, ErrRenderPlanInvalid
	}
	result, err := domain.NewUInt64(quotient.Uint64())
	if err != nil || result.Value() == 0 {
		return 0, ErrRenderPlanInvalid
	}
	return result, nil
}

func sequencePreviewDimensions(format domain.SequenceFormat) (uint32, uint32, error) {
	if format.Validate() != nil || !format.PixelAspect.IsPositive() {
		return 0, 0, ErrRenderPlanInvalid
	}
	displayWidth := new(big.Int).Mul(
		big.NewInt(int64(format.CanvasWidth)), big.NewInt(format.PixelAspect.Value.Value()),
	)
	displayScale := big.NewInt(int64(format.PixelAspect.Scale))
	heightScaled := new(big.Int).Mul(big.NewInt(int64(format.CanvasHeight)), displayScale)
	if displayWidth.Cmp(heightScaled) >= 0 {
		widthLimit := floorBigRatio(displayWidth, displayScale)
		width := evenFloor(minimumInt64(widthLimit, domain.SequencePreviewMaximumLongEdge))
		height := evenFloor(floorBigRatio(
			new(big.Int).Mul(big.NewInt(width), heightScaled), displayWidth,
		))
		if width < 2 || height < 2 {
			return 0, 0, ErrRenderPlanInvalid
		}
		return uint32(width), uint32(height), nil
	}
	height := evenFloor(minimumInt64(int64(format.CanvasHeight), domain.SequencePreviewMaximumLongEdge))
	width := evenFloor(floorBigRatio(
		new(big.Int).Mul(big.NewInt(height), displayWidth), heightScaled,
	))
	if width < 2 || height < 2 {
		return 0, 0, ErrRenderPlanInvalid
	}
	return uint32(width), uint32(height), nil
}

func floorBigRatio(numerator, denominator *big.Int) int64 {
	if numerator.Sign() <= 0 || denominator.Sign() <= 0 {
		return 0
	}
	result := new(big.Int).Quo(numerator, denominator)
	if !result.IsInt64() {
		return 0
	}
	return result.Int64()
}

func minimumInt64(left int64, right int) int64 {
	if left < int64(right) {
		return left
	}
	return int64(right)
}

func evenFloor(value int64) int64 {
	return value - value%2
}
