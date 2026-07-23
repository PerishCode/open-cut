package rendercontract

import (
	"reflect"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
)

// ValidateSequencePreviewRenderPlanPayload validates the complete private
// execution contract. It is intentionally stricter than JSON decoding: a
// digest proves byte identity, while this function proves that every byte is a
// closed first-slice render instruction understood by the registered engine.
func ValidateSequencePreviewRenderPlanPayload(plan domain.RenderPlanPayload) error {
	if plan.Purpose != domain.RenderPurposeSequencePreview {
		return ErrRenderPlanInvalid
	}
	return validateRenderPlanPayload(plan, SourceProxyProfile, SequencePreviewOutput)
}

func ValidateSequenceExportRenderPlanPayload(plan domain.RenderPlanPayload) error {
	if plan.Purpose != domain.RenderPurposeExport {
		return ErrRenderPlanInvalid
	}
	return validateRenderPlanPayload(plan, RenderInputProfile, SequenceExportOutput)
}

func ValidateRenderPlanPayload(plan domain.RenderPlanPayload) error {
	switch plan.Purpose {
	case domain.RenderPurposeSequencePreview:
		return ValidateSequencePreviewRenderPlanPayload(plan)
	case domain.RenderPurposeExport:
		return ValidateSequenceExportRenderPlanPayload(plan)
	default:
		return ErrRenderPlanInvalid
	}
}

func validateRenderPlanPayload(
	plan domain.RenderPlanPayload,
	requiredProfile string,
	output func(domain.SequenceFormat, domain.RationalTime) (domain.RenderOutputPolicy, error),
) error {
	if plan.CompilerVersion != domain.RenderPlanCompilerV4 ||
		plan.ProjectID.IsZero() ||
		plan.SequenceID.IsZero() || plan.SequenceRevision.Value() == 0 ||
		plan.SequenceFormat.Validate() != nil || plan.Duration.Validate() != nil ||
		!plan.Duration.IsPositive() || len(plan.Inputs) > MaximumRenderPlanItems ||
		len(plan.Video) > MaximumRenderPlanItems || len(plan.Audio) > MaximumRenderPlanItems ||
		len(plan.Captions) > MaximumRenderPlanItems || len(plan.FontResources) > MaximumRenderPlanItems {
		return ErrRenderPlanInvalid
	}
	expectedOutput, err := output(plan.SequenceFormat, plan.Duration)
	if err != nil || !reflect.DeepEqual(plan.Output, expectedOutput) {
		return ErrRenderPlanInvalid
	}
	inputs, err := validatePublishedRenderInputs(plan.Inputs, requiredProfile)
	if err != nil {
		return err
	}
	fonts, err := validatePublishedRenderFonts(plan.FontResources)
	if err != nil {
		return err
	}
	used := make(map[string]struct{}, len(inputs))
	duration, err := validatePublishedRenderVideo(plan.Video, inputs, used)
	if err != nil {
		return err
	}
	audioDuration, err := validatePublishedRenderAudio(plan.Audio, inputs, used)
	if err != nil {
		return err
	}
	duration = laterRenderTime(duration, audioDuration)
	captionDuration, err := validatePublishedRenderCaptions(plan.Captions, fonts)
	if err != nil {
		return err
	}
	usedFonts := make(map[string]struct{}, len(fonts))
	for _, caption := range plan.Captions {
		usedFonts[caption.Style.FontResourceID] = struct{}{}
	}
	duration = laterRenderTime(duration, captionDuration)
	if comparison, compareErr := duration.Compare(plan.Duration); compareErr != nil || comparison != 0 ||
		len(used) != len(inputs) || len(usedFonts) != len(fonts) {
		return ErrRenderPlanInvalid
	}
	return nil
}

func validatePublishedRenderInputs(
	source []domain.RenderPlanInput,
	requiredProfile string,
) (map[string]domain.RenderPlanInput, error) {
	result := make(map[string]domain.RenderPlanInput, len(source))
	previous := ""
	for _, input := range source {
		key := input.ArtifactID.String()
		if input.ArtifactID.IsZero() || key <= previous || input.AssetID.IsZero() ||
			input.AssetRevision.Value() == 0 || input.Profile != requiredProfile ||
			input.ProducerVersion == "" || len(input.ProducerVersion) > 256 ||
			input.SourceEpoch.Validate() != nil || !validRenderDigest(input.ArtifactDigest) ||
			!validRenderDigest(input.Fingerprint) || !validRenderDigest(input.MediaDigest) ||
			(input.Video == nil && input.Audio == nil) {
			return nil, ErrRenderPlanInvalid
		}
		if input.Video != nil && validatePublishedRenderVideoInput(*input.Video) != nil {
			return nil, ErrRenderPlanInvalid
		}
		if input.Audio != nil && validatePublishedRenderAudioInput(*input.Audio) != nil {
			return nil, ErrRenderPlanInvalid
		}
		result[key] = input
		previous = key
	}
	return result, nil
}

func validatePublishedRenderVideoInput(input domain.RenderVideoInput) error {
	if input.SourceStreamID.IsZero() || input.SourceStart.Validate() != nil ||
		input.MaterialStart.Validate() != nil || input.SourceTimeBase.Validate() != nil ||
		!input.SourceTimeBase.IsPositive() || input.MaterialTimeBase.Validate() != nil ||
		!input.MaterialTimeBase.IsPositive() || !validRenderDigest(input.TimeMapDigest) ||
		input.Width == 0 || input.Height == 0 || input.Width > MaximumRenderDimension ||
		input.Height > MaximumRenderDimension {
		return ErrRenderPlanInvalid
	}
	return nil
}

func validatePublishedRenderAudioInput(input domain.RenderAudioInput) error {
	if input.SourceStreamID.IsZero() || input.SourceStart.Validate() != nil ||
		input.MaterialStart.Validate() != nil || input.SourceTimeBase.Validate() != nil ||
		!input.SourceTimeBase.IsPositive() || input.MaterialTimeBase.Validate() != nil ||
		!input.MaterialTimeBase.IsPositive() || input.SampleRate != domain.SequencePreviewAudioSampleRate ||
		input.ChannelLayout != "stereo" || input.DecodedSampleCount.Value() == 0 ||
		input.DecodedSampleCount.Value() > MaximumSourceProxySamples {
		return ErrRenderPlanInvalid
	}
	return nil
}

func validatePublishedRenderFonts(
	source []domain.RenderFontResource,
) (map[string]domain.RenderFontResource, error) {
	result := make(map[string]domain.RenderFontResource, len(source))
	previous := ""
	for _, font := range source {
		if !ValidFontResource(font) || font.ResourceID <= previous {
			return nil, ErrRenderPlanInvalid
		}
		result[font.ResourceID] = font
		previous = font.ResourceID
	}
	return result, nil
}

func validatePublishedRenderVideo(
	source []domain.RenderVideoInstruction,
	inputs map[string]domain.RenderPlanInput,
	used map[string]struct{},
) (domain.RationalTime, error) {
	zero, _ := domain.NewRationalTime(0, 1)
	duration := zero
	lastByTrack := make(map[string]domain.RationalTime)
	var previous *domain.RenderVideoInstruction
	for index := range source {
		instruction := source[index]
		input, exists := inputs[instruction.InputArtifactID.String()]
		if !exists || input.Video == nil || instruction.ClipID.IsZero() ||
			instruction.ClipRevision.Value() == 0 || instruction.TrackID.IsZero() ||
			instruction.TrackRevision.Value() == 0 || instruction.SourceStreamID != input.Video.SourceStreamID ||
			validatePositiveRange(instruction.SourceRange, false) != nil ||
			validatePositiveRange(instruction.TimelineRange, true) != nil ||
			instruction.Orientation != "normalized-by-render-material-v1" ||
			validateRenderPlacement(instruction.Placement) != nil ||
			!renderInstructionOrdered(previous, &instruction) {
			return domain.RationalTime{}, ErrRenderPlanInvalid
		}
		if equal, compareErr := instruction.SourceRange.Duration.Compare(instruction.TimelineRange.Duration); compareErr != nil || equal != 0 {
			return domain.RationalTime{}, ErrRenderPlanInvalid
		}
		end, _ := instruction.TimelineRange.End()
		if last, exists := lastByTrack[instruction.TrackID.String()]; exists {
			if comparison, compareErr := instruction.TimelineRange.Start.Compare(last); compareErr != nil || comparison < 0 {
				return domain.RationalTime{}, ErrRenderPlanInvalid
			}
		}
		lastByTrack[instruction.TrackID.String()] = end
		duration = laterRenderTime(duration, end)
		used[instruction.InputArtifactID.String()] = struct{}{}
		previous = &instruction
	}
	return duration, nil
}

func validatePublishedRenderAudio(
	source []domain.RenderAudioInstruction,
	inputs map[string]domain.RenderPlanInput,
	used map[string]struct{},
) (domain.RationalTime, error) {
	zero, _ := domain.NewRationalTime(0, 1)
	duration := zero
	lastByTrack := make(map[string]domain.RationalTime)
	var previous *domain.RenderAudioInstruction
	for index := range source {
		instruction := source[index]
		input, exists := inputs[instruction.InputArtifactID.String()]
		if !exists || input.Audio == nil || instruction.ClipID.IsZero() ||
			instruction.ClipRevision.Value() == 0 || instruction.TrackID.IsZero() ||
			instruction.TrackRevision.Value() == 0 || instruction.SourceStreamID != input.Audio.SourceStreamID ||
			validatePositiveRange(instruction.SourceRange, false) != nil ||
			validatePositiveRange(instruction.TimelineRange, true) != nil ||
			instruction.ChannelMapping != "render-material-stereo-v1" ||
			instruction.GainMilliDB < domain.RenderGainMinimumMilliDB ||
			instruction.GainMilliDB > domain.RenderGainMaximumMilliDB ||
			!renderInstructionOrdered(previous, &instruction) {
			return domain.RationalTime{}, ErrRenderPlanInvalid
		}
		if equal, compareErr := instruction.SourceRange.Duration.Compare(instruction.TimelineRange.Duration); compareErr != nil || equal != 0 {
			return domain.RationalTime{}, ErrRenderPlanInvalid
		}
		end, _ := instruction.TimelineRange.End()
		if last, exists := lastByTrack[instruction.TrackID.String()]; exists {
			if comparison, compareErr := instruction.TimelineRange.Start.Compare(last); compareErr != nil || comparison < 0 {
				return domain.RationalTime{}, ErrRenderPlanInvalid
			}
		}
		lastByTrack[instruction.TrackID.String()] = end
		duration = laterRenderTime(duration, end)
		used[instruction.InputArtifactID.String()] = struct{}{}
		previous = &instruction
	}
	return duration, nil
}

func validatePublishedRenderCaptions(
	source []domain.RenderCaptionInstruction,
	fonts map[string]domain.RenderFontResource,
) (domain.RationalTime, error) {
	zero, _ := domain.NewRationalTime(0, 1)
	duration := zero
	lastByTrack := make(map[string]domain.RationalTime)
	var previous *domain.RenderCaptionInstruction
	for index := range source {
		instruction := source[index]
		if instruction.CaptionID.IsZero() || instruction.CaptionRevision.Value() == 0 ||
			instruction.TrackID.IsZero() || instruction.TrackRevision.Value() == 0 ||
			validatePositiveRange(instruction.Range, true) != nil ||
			instruction.Language.Validate() != nil || !ValidCaptionText(instruction.Text) ||
			validateRenderCaptionStyle(instruction.Style, fonts) != nil ||
			!renderInstructionOrdered(previous, &instruction) {
			return domain.RationalTime{}, ErrRenderPlanInvalid
		}
		end, _ := instruction.Range.End()
		if last, exists := lastByTrack[instruction.TrackID.String()]; exists {
			if comparison, compareErr := instruction.Range.Start.Compare(last); compareErr != nil || comparison < 0 {
				return domain.RationalTime{}, ErrRenderPlanInvalid
			}
		}
		lastByTrack[instruction.TrackID.String()] = end
		duration = laterRenderTime(duration, end)
		previous = &instruction
	}
	return duration, nil
}

func validateRenderPlacement(value domain.RenderPlacement) error {
	maximumScale, _ := domain.NewExactRational(64, 1)
	maximumTranslation, _ := domain.NewExactRational(16, 1)
	minimumTranslation, _ := domain.NewExactRational(-16, 1)
	if value.CropWidthBasisPoints == 0 || value.CropHeightBasisPoints == 0 ||
		uint32(value.CropXBasisPoints)+uint32(value.CropWidthBasisPoints) > 10_000 ||
		uint32(value.CropYBasisPoints)+uint32(value.CropHeightBasisPoints) > 10_000 ||
		value.AnchorXBasisPoints > 10_000 || value.AnchorYBasisPoints > 10_000 ||
		value.OpacityBasisPoints > 10_000 || value.ScaleX.Validate() != nil || value.ScaleY.Validate() != nil ||
		value.TranslateX.Validate() != nil || value.TranslateY.Validate() != nil ||
		value.FitPolicy != "contain" && value.FitPolicy != "cover" {
		return ErrRenderPlanInvalid
	}
	for _, scale := range []domain.ExactRational{value.ScaleX, value.ScaleY} {
		if comparison, err := scale.Compare(maximumScale); err != nil || comparison > 0 || !scale.IsPositive() {
			return ErrRenderPlanInvalid
		}
	}
	for _, translation := range []domain.ExactRational{value.TranslateX, value.TranslateY} {
		if lower, err := translation.Compare(minimumTranslation); err != nil || lower < 0 {
			return ErrRenderPlanInvalid
		}
		if upper, err := translation.Compare(maximumTranslation); err != nil || upper > 0 {
			return ErrRenderPlanInvalid
		}
	}
	return nil
}

func validateRenderCaptionStyle(
	style domain.RenderCaptionStyle,
	fonts map[string]domain.RenderFontResource,
) error {
	if _, exists := fonts[style.FontResourceID]; !exists || style.FontSizeBasisPoint == 0 ||
		style.FontSizeBasisPoint > 10_000 || style.OutlineBasisPoints > 10_000 ||
		style.LineHeightBasisPoints < 10_000 || style.LineHeightBasisPoints > 30_000 ||
		style.PositionYBasisPoint > 10_000 || style.SafeWidthBasisPoint == 0 ||
		style.SafeWidthBasisPoint > 10_000 || style.Alignment != "bottom-center" ||
		style.WrapPolicy != "explicit-lines-clip-v1" || !validRenderRGBA(style.TextColorRGBA) ||
		!validRenderRGBA(style.OutlineColorRGBA) {
		return ErrRenderPlanInvalid
	}
	return nil
}

func ValidCaptionText(value string) bool {
	if value == "" || !utf8.ValidString(value) || len([]byte(value)) > domain.MaximumAuthoredTextBytes {
		return false
	}
	for _, current := range value {
		if !domain.IsRenderCaptionRune(current) {
			return false
		}
	}
	return true
}

func validRenderRGBA(value string) bool {
	if len(value) != 9 || value[0] != '#' {
		return false
	}
	for _, current := range value[1:] {
		if current < '0' || current > '9' {
			if current < 'a' || current > 'f' {
				return false
			}
		}
	}
	return true
}

func validRenderDigest(value domain.Digest) bool {
	_, err := domain.ParseDigest(value.String())
	return err == nil
}

type layeredRenderInstruction interface {
	domain.RenderVideoInstruction | domain.RenderAudioInstruction | domain.RenderCaptionInstruction
}

func renderInstructionOrdered[T layeredRenderInstruction](previous, current *T) bool {
	if previous == nil {
		return true
	}
	previousLayer, previousStart, previousID := renderInstructionOrder(*previous)
	currentLayer, currentStart, currentID := renderInstructionOrder(*current)
	if previousLayer != currentLayer {
		return previousLayer < currentLayer
	}
	comparison, err := previousStart.Compare(currentStart)
	return err == nil && (comparison < 0 || comparison == 0 && previousID < currentID)
}

func renderInstructionOrder[T layeredRenderInstruction](value T) (uint16, domain.RationalTime, string) {
	switch current := any(value).(type) {
	case domain.RenderVideoInstruction:
		return current.Layer, current.TimelineRange.Start, current.ClipID.String()
	case domain.RenderAudioInstruction:
		return current.Layer, current.TimelineRange.Start, current.ClipID.String()
	case domain.RenderCaptionInstruction:
		return current.Layer, current.Range.Start, current.CaptionID.String()
	default:
		panic("unreachable render instruction")
	}
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

func laterRenderTime(left, right domain.RationalTime) domain.RationalTime {
	comparison, _ := left.Compare(right)
	if comparison < 0 {
		return right
	}
	return left
}
