package application

import (
	"bytes"
	"errors"
	"math/big"
	"reflect"
	"sort"

	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrRenderPlanInvalid   = errors.New("render plan input is invalid")
	ErrRenderSequenceEmpty = errors.New("render sequence has no executable duration")
	ErrRenderInputRequired = errors.New("render input artifact is required")
	ErrRenderFontRequired  = errors.New("render font resource is required")
)

const MaximumRenderPlanItems = 65_536

type RenderAssetSnapshot struct {
	ID                  domain.AssetID
	Revision            domain.Revision
	AcceptedFingerprint domain.Digest
	Availability        domain.AssetAvailability
}

type RenderClipInputBinding struct {
	ClipID   domain.ClipID
	Artifact domain.ArtifactSummary
	Material RenderMaterial
}

type CompileRenderPlanInput struct {
	ProjectID               domain.ProjectID
	ObservedProjectRevision domain.Revision
	Sequence                domain.Sequence
	Clips                   []domain.ClipState
	Captions                []domain.CaptionState
	Assets                  map[string]RenderAssetSnapshot
	Bindings                []RenderClipInputBinding
	FontResource            *domain.RenderFontResource
}

type CompiledRenderPlan struct {
	Plan      domain.RenderPlan
	Canonical []byte
}

type renderTrack struct {
	state domain.Track
	layer uint16
}

type validatedRenderBinding struct {
	binding RenderClipInputBinding
	input   domain.RenderPlanInput
}

func CompileSequencePreviewPlan(input CompileRenderPlanInput) (CompiledRenderPlan, error) {
	return compileSequenceRenderPlan(
		input, domain.RenderPurposeSequencePreview, SourceProxyProfile,
		sequencePreviewOutput, ValidateSequencePreviewRenderPlanPayload,
	)
}

func CompileSequenceExportPlan(input CompileRenderPlanInput) (CompiledRenderPlan, error) {
	return compileSequenceRenderPlan(
		input, domain.RenderPurposeExport, RenderInputProfile,
		sequenceExportOutput, ValidateSequenceExportRenderPlanPayload,
	)
}

func compileSequenceRenderPlan(
	input CompileRenderPlanInput,
	purpose domain.RenderPlanPurpose,
	requiredProfile string,
	output func(domain.SequenceFormat, domain.RationalTime) (domain.RenderOutputPolicy, error),
	validate func(domain.RenderPlanPayload) error,
) (CompiledRenderPlan, error) {
	if err := validateRenderPlanHead(input); err != nil {
		return CompiledRenderPlan{}, err
	}
	tracks, err := normalizeRenderTracks(input.Sequence.Tracks)
	if err != nil {
		return CompiledRenderPlan{}, err
	}
	bindings, err := normalizeRenderBindings(input, tracks, requiredProfile)
	if err != nil {
		return CompiledRenderPlan{}, err
	}
	plan := domain.RenderPlanPayload{
		CompilerVersion: domain.RenderPlanCompilerV4,
		Purpose:         purpose,
		ProjectID:       input.ProjectID, SequenceID: input.Sequence.ID,
		SequenceRevision: input.Sequence.Revision, SequenceFormat: input.Sequence.Format,
		Inputs: []domain.RenderPlanInput{}, Video: []domain.RenderVideoInstruction{},
		Audio: []domain.RenderAudioInstruction{}, Captions: []domain.RenderCaptionInstruction{},
		FontResources: []domain.RenderFontResource{},
	}
	duration, err := compileRenderInstructions(&plan, input, tracks, bindings)
	if err != nil {
		return CompiledRenderPlan{}, err
	}
	if !duration.IsPositive() {
		return CompiledRenderPlan{}, ErrRenderSequenceEmpty
	}
	plan.Duration = duration
	plan.Inputs, err = uniqueRenderInputs(bindings)
	if err != nil {
		return CompiledRenderPlan{}, err
	}
	plan.Output, err = output(input.Sequence.Format, duration)
	if err != nil {
		return CompiledRenderPlan{}, err
	}
	if err := validate(plan); err != nil {
		return CompiledRenderPlan{}, err
	}
	canonical, digest, err := domain.CanonicalDigest("open-cut/render-plan", domain.RenderPlanSchema, plan)
	if err != nil {
		return CompiledRenderPlan{}, err
	}
	return CompiledRenderPlan{
		Plan:      domain.RenderPlan{Payload: plan, Digest: digest, ObservedProjectRevision: input.ObservedProjectRevision},
		Canonical: canonical,
	}, nil
}

func validateRenderPlanHead(input CompileRenderPlanInput) error {
	if input.ProjectID.IsZero() || input.ObservedProjectRevision.Value() == 0 || input.Sequence.ID.IsZero() ||
		input.Sequence.Revision.Value() == 0 || input.Sequence.Format.Validate() != nil ||
		len(input.Sequence.Tracks) == 0 || len(input.Sequence.Tracks) > MaximumRenderPlanItems ||
		len(input.Clips) > MaximumRenderPlanItems || len(input.Captions) > MaximumRenderPlanItems ||
		len(input.Bindings) > MaximumRenderPlanItems || input.Assets == nil {
		return ErrRenderPlanInvalid
	}
	return nil
}

func normalizeRenderTracks(source []domain.Track) (map[string]renderTrack, error) {
	ordered := append([]domain.Track(nil), source...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].OrderKey != ordered[right].OrderKey {
			return ordered[left].OrderKey < ordered[right].OrderKey
		}
		return ordered[left].ID.String() < ordered[right].ID.String()
	})
	result := make(map[string]renderTrack, len(ordered))
	layers := map[domain.TrackType]uint16{
		domain.TrackVideo: 0, domain.TrackAudio: 0, domain.TrackCaption: 0,
	}
	previousOrder := ""
	for _, track := range ordered {
		if track.ID.IsZero() || track.Revision.Value() == 0 || track.OrderKey == "" ||
			track.OrderKey == previousOrder ||
			(track.Type != domain.TrackVideo && track.Type != domain.TrackAudio && track.Type != domain.TrackCaption) {
			return nil, ErrRenderPlanInvalid
		}
		if _, duplicate := result[track.ID.String()]; duplicate {
			return nil, ErrRenderPlanInvalid
		}
		layer := layers[track.Type]
		if layer == ^uint16(0) {
			return nil, ErrRenderPlanInvalid
		}
		result[track.ID.String()] = renderTrack{state: track, layer: layer}
		layers[track.Type] = layer + 1
		previousOrder = track.OrderKey
	}
	return result, nil
}

func normalizeRenderBindings(
	input CompileRenderPlanInput,
	tracks map[string]renderTrack,
	requiredProfile string,
) (map[string]validatedRenderBinding, error) {
	clips := make(map[string]domain.ClipState, len(input.Clips))
	for _, clip := range input.Clips {
		if _, duplicate := clips[clip.ID.String()]; duplicate || validateRenderClipHead(clip, input.Sequence.ID, tracks) != nil {
			return nil, ErrRenderPlanInvalid
		}
		clips[clip.ID.String()] = clip
	}
	result := make(map[string]validatedRenderBinding, len(input.Bindings))
	for _, binding := range input.Bindings {
		clip, exists := clips[binding.ClipID.String()]
		if !exists || clip.Tombstoned || !clip.Enabled {
			return nil, ErrRenderPlanInvalid
		}
		if _, duplicate := result[binding.ClipID.String()]; duplicate {
			return nil, ErrRenderPlanInvalid
		}
		asset, exists := input.Assets[clip.AssetID.String()]
		if !exists || asset.ID != clip.AssetID || asset.Revision.Value() == 0 {
			return nil, ErrRenderPlanInvalid
		}
		planInput, err := validateRenderBinding(
			binding, clip, tracks[clip.TrackID.String()].state.Type, asset, requiredProfile,
		)
		if err != nil {
			return nil, err
		}
		result[binding.ClipID.String()] = validatedRenderBinding{binding: binding, input: planInput}
	}
	for _, clip := range input.Clips {
		if clip.Tombstoned || !clip.Enabled {
			continue
		}
		if _, exists := result[clip.ID.String()]; !exists {
			return nil, ErrRenderInputRequired
		}
	}
	return result, nil
}

func validateRenderClipHead(
	clip domain.ClipState,
	sequenceID domain.SequenceID,
	tracks map[string]renderTrack,
) error {
	track, exists := tracks[clip.TrackID.String()]
	if clip.ID.IsZero() || clip.Revision.Value() == 0 || clip.SequenceID != sequenceID || !exists ||
		clip.AssetID.IsZero() || clip.SourceStreamID.IsZero() ||
		validatePositiveRange(clip.SourceRange, false) != nil ||
		validatePositiveRange(clip.TimelineRange, true) != nil {
		return ErrRenderPlanInvalid
	}
	equal, err := clip.SourceRange.Duration.Compare(clip.TimelineRange.Duration)
	if err != nil || equal != 0 || (track.state.Type != domain.TrackVideo && track.state.Type != domain.TrackAudio) {
		return ErrRenderPlanInvalid
	}
	return nil
}

func validatePositiveRange(value domain.TimeRange, nonnegativeStart bool) error {
	if value.Start.Validate() != nil || value.Duration.Validate() != nil || !value.Duration.IsPositive() ||
		(nonnegativeStart && value.Start.IsNegative()) {
		return ErrRenderPlanInvalid
	}
	if _, err := value.End(); err != nil {
		return ErrRenderPlanInvalid
	}
	return nil
}

func validateRenderBinding(
	binding RenderClipInputBinding,
	clip domain.ClipState,
	trackType domain.TrackType,
	asset RenderAssetSnapshot,
	requiredProfile string,
) (domain.RenderPlanInput, error) {
	if requiredProfile == "" || binding.Material.profile != requiredProfile {
		return domain.RenderPlanInput{}, ErrRenderInputRequired
	}
	result, err := binding.Material.planInput(binding.Artifact, asset)
	if err != nil {
		return domain.RenderPlanInput{}, err
	}
	if (trackType == domain.TrackVideo && (result.Video == nil || result.Video.SourceStreamID != clip.SourceStreamID)) ||
		(trackType == domain.TrackAudio && (result.Audio == nil || result.Audio.SourceStreamID != clip.SourceStreamID)) {
		return domain.RenderPlanInput{}, ErrRenderInputRequired
	}
	return result, nil
}

func compileRenderInstructions(
	plan *domain.RenderPlanPayload,
	input CompileRenderPlanInput,
	tracks map[string]renderTrack,
	bindings map[string]validatedRenderBinding,
) (domain.RationalTime, error) {
	zero, _ := domain.NewRationalTime(0, 1)
	duration := zero
	clips := append([]domain.ClipState(nil), input.Clips...)
	sort.Slice(clips, func(left, right int) bool { return renderClipLess(clips[left], clips[right], tracks) })
	lastClipByTrack := make(map[string]domain.RationalTime)
	for _, clip := range clips {
		if clip.Tombstoned || !clip.Enabled {
			continue
		}
		track := tracks[clip.TrackID.String()]
		if end, err := clip.TimelineRange.End(); err != nil {
			return domain.RationalTime{}, ErrRenderPlanInvalid
		} else {
			if previous, exists := lastClipByTrack[clip.TrackID.String()]; exists {
				comparison, compareErr := clip.TimelineRange.Start.Compare(previous)
				if compareErr != nil || comparison < 0 {
					return domain.RationalTime{}, ErrRenderPlanInvalid
				}
			}
			lastClipByTrack[clip.TrackID.String()] = end
			duration = laterRenderTime(duration, end)
		}
		binding := bindings[clip.ID.String()]
		switch track.state.Type {
		case domain.TrackVideo:
			plan.Video = append(plan.Video, domain.RenderVideoInstruction{
				ClipID: clip.ID, ClipRevision: clip.Revision, TrackID: clip.TrackID,
				TrackRevision: track.state.Revision, Layer: track.layer,
				InputArtifactID: binding.input.ArtifactID, SourceStreamID: clip.SourceStreamID,
				SourceRange: clip.SourceRange, TimelineRange: clip.TimelineRange,
				Orientation: "normalized-by-render-material-v1", Placement: defaultRenderPlacement(),
			})
		case domain.TrackAudio:
			plan.Audio = append(plan.Audio, domain.RenderAudioInstruction{
				ClipID: clip.ID, ClipRevision: clip.Revision, TrackID: clip.TrackID,
				TrackRevision: track.state.Revision, Layer: track.layer,
				InputArtifactID: binding.input.ArtifactID, SourceStreamID: clip.SourceStreamID,
				SourceRange: clip.SourceRange, TimelineRange: clip.TimelineRange,
				ChannelMapping: "render-material-stereo-v1", GainMilliDB: 0,
			})
		default:
			return domain.RationalTime{}, ErrRenderPlanInvalid
		}
	}
	captionDuration, err := compileRenderCaptions(plan, input, tracks)
	if err != nil {
		return domain.RationalTime{}, err
	}
	return laterRenderTime(duration, captionDuration), nil
}

func compileRenderCaptions(
	plan *domain.RenderPlanPayload,
	input CompileRenderPlanInput,
	tracks map[string]renderTrack,
) (domain.RationalTime, error) {
	zero, _ := domain.NewRationalTime(0, 1)
	duration := zero
	captions := append([]domain.CaptionState(nil), input.Captions...)
	sort.Slice(captions, func(left, right int) bool { return renderCaptionLess(captions[left], captions[right], tracks) })
	lastByTrack := make(map[string]domain.RationalTime)
	fontRequired := false
	for _, caption := range captions {
		if caption.Tombstoned {
			continue
		}
		track, exists := tracks[caption.TrackID.String()]
		if caption.ID.IsZero() || caption.Revision.Value() == 0 || caption.SequenceID != input.Sequence.ID ||
			!exists || track.state.Type != domain.TrackCaption || validatePositiveRange(caption.Range, true) != nil ||
			caption.Language.Validate() != nil || !validRenderCaptionText(caption.Text) {
			return domain.RationalTime{}, ErrRenderPlanInvalid
		}
		end, err := caption.Range.End()
		if err != nil {
			return domain.RationalTime{}, ErrRenderPlanInvalid
		}
		if previous, exists := lastByTrack[caption.TrackID.String()]; exists {
			comparison, compareErr := caption.Range.Start.Compare(previous)
			if compareErr != nil || comparison < 0 {
				return domain.RationalTime{}, ErrRenderPlanInvalid
			}
		}
		lastByTrack[caption.TrackID.String()] = end
		duration = laterRenderTime(duration, end)
		fontRequired = true
	}
	if fontRequired {
		if input.FontResource == nil || !validRenderFont(*input.FontResource) {
			return domain.RationalTime{}, ErrRenderFontRequired
		}
		plan.FontResources = append(plan.FontResources, *input.FontResource)
	}
	for _, caption := range captions {
		if caption.Tombstoned {
			continue
		}
		track := tracks[caption.TrackID.String()]
		plan.Captions = append(plan.Captions, domain.RenderCaptionInstruction{
			CaptionID: caption.ID, CaptionRevision: caption.Revision,
			TrackID: caption.TrackID, TrackRevision: track.state.Revision, Layer: track.layer,
			Range: caption.Range, Language: caption.Language, Text: caption.Text,
			Style: defaultRenderCaptionStyle(input.FontResource.ResourceID),
		})
	}
	return duration, nil
}

func validRenderFont(font domain.RenderFontResource) bool {
	if font.ResourceID == "" || len(font.ResourceID) > 256 || font.Version == "" || len(font.Version) > 128 {
		return false
	}
	_, err := domain.ParseDigest(font.SHA256.String())
	return err == nil
}

func renderClipLess(left, right domain.ClipState, tracks map[string]renderTrack) bool {
	leftTrack, rightTrack := tracks[left.TrackID.String()], tracks[right.TrackID.String()]
	if leftTrack.state.OrderKey != rightTrack.state.OrderKey {
		return leftTrack.state.OrderKey < rightTrack.state.OrderKey
	}
	if comparison, _ := left.TimelineRange.Start.Compare(right.TimelineRange.Start); comparison != 0 {
		return comparison < 0
	}
	return left.ID.String() < right.ID.String()
}

func renderCaptionLess(left, right domain.CaptionState, tracks map[string]renderTrack) bool {
	leftTrack, rightTrack := tracks[left.TrackID.String()], tracks[right.TrackID.String()]
	if leftTrack.state.OrderKey != rightTrack.state.OrderKey {
		return leftTrack.state.OrderKey < rightTrack.state.OrderKey
	}
	if comparison, _ := left.Range.Start.Compare(right.Range.Start); comparison != 0 {
		return comparison < 0
	}
	return left.ID.String() < right.ID.String()
}

func laterRenderTime(left, right domain.RationalTime) domain.RationalTime {
	comparison, _ := left.Compare(right)
	if comparison < 0 {
		return right
	}
	return left
}

func uniqueRenderInputs(bindings map[string]validatedRenderBinding) ([]domain.RenderPlanInput, error) {
	byArtifact := make(map[string]domain.RenderPlanInput, len(bindings))
	for _, binding := range bindings {
		key := binding.input.ArtifactID.String()
		if previous, exists := byArtifact[key]; exists && !reflect.DeepEqual(previous, binding.input) {
			return nil, ErrRenderPlanInvalid
		}
		byArtifact[key] = binding.input
	}
	result := make([]domain.RenderPlanInput, 0, len(byArtifact))
	for _, input := range byArtifact {
		result = append(result, input)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ArtifactID.String() < result[right].ArtifactID.String()
	})
	return result, nil
}

func defaultRenderPlacement() domain.RenderPlacement {
	one, _ := domain.NewExactRational(1, 1)
	zero, _ := domain.NewExactRational(0, 1)
	return domain.RenderPlacement{
		CropWidthBasisPoints:  domain.SequencePreviewOpacityBasisPoint,
		CropHeightBasisPoints: domain.SequencePreviewOpacityBasisPoint,
		ScaleX:                one, ScaleY: one, TranslateX: zero, TranslateY: zero,
		AnchorXBasisPoints: 5_000, AnchorYBasisPoints: 5_000,
		OpacityBasisPoints: domain.SequencePreviewOpacityBasisPoint, FitPolicy: "contain",
	}
}

func defaultRenderCaptionStyle(resourceID string) domain.RenderCaptionStyle {
	return domain.RenderCaptionStyle{
		FontResourceID: resourceID, FontSizeBasisPoint: 550,
		TextColorRGBA: "#ffffffff", OutlineColorRGBA: "#000000ff", OutlineBasisPoints: 35,
		LineHeightBasisPoints: 12_000,
		Alignment:             "bottom-center", PositionYBasisPoint: 8_800,
		SafeWidthBasisPoint: 9_000, WrapPolicy: "explicit-lines-clip-v1",
	}
}

func sequencePreviewOutput(
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
	if videoFrameCount.Value() > MaximumSequencePreviewVideoFrames ||
		audioSampleCount.Value() > MaximumSequencePreviewAudioSamples {
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

func sequenceExportOutput(
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
	if err != nil || videoFrameCount.Value() > MaximumSequencePreviewVideoFrames ||
		audioSampleCount.Value() > MaximumSequencePreviewAudioSamples {
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

func EqualCompiledRenderPlans(left, right CompiledRenderPlan) bool {
	return left.Plan.Digest == right.Plan.Digest && bytes.Equal(left.Canonical, right.Canonical)
}
