package renderengine

import (
	"fmt"
	"math"
	"math/big"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	ResamplePlanPolicyV1    = "mitchell-q30-widened-512-v1"
	MaximumResampleAxisTaps = 512
)

type VideoResampleInstructionPlan struct {
	Policy           string
	InstructionIndex uint32
	Horizontal       ResampleAxisPlan
	Vertical         ResampleAxisPlan
	ActiveFrames     uint64
	TapWorkPerFrame  uint64
	TapWork          uint64
}

type ResampleAxisPlan struct {
	Samples             []ResampleSourceSpan
	TotalTaps           uint64
	ActiveOutputSamples uint32
	UniqueSourceSamples uint32
	MaximumTaps         uint16
	MaximumKernelTaps   uint16
	sourcePositions     []*big.Rat
	filterScale         *big.Rat
}

type ResampleSourceSpan struct {
	First       uint32
	Count       uint16
	KernelFirst int32
	KernelCount uint16
}

func CompileVideoResampleInstructionPlan(
	plan domain.RenderPlanPayload,
	instructionIndex uint32,
) (VideoResampleInstructionPlan, error) {
	if int(instructionIndex) >= len(plan.Video) || plan.Output.CanvasWidth == 0 ||
		plan.Output.CanvasHeight == 0 || plan.Output.VideoFrameCount.Value() == 0 {
		return VideoResampleInstructionPlan{}, fmt.Errorf("video resample instruction is invalid")
	}
	instruction := plan.Video[instructionIndex]
	var input *domain.RenderVideoInput
	for index := range plan.Inputs {
		if plan.Inputs[index].ArtifactID == instruction.InputArtifactID {
			input = plan.Inputs[index].Video
			break
		}
	}
	if input == nil || input.SourceStreamID != instruction.SourceStreamID || input.Width == 0 || input.Height == 0 ||
		instruction.Orientation != "normalized-by-render-material-v1" {
		return VideoResampleInstructionPlan{}, fmt.Errorf("video resample source is invalid")
	}
	geometry, err := compileVideoGeometry(*input, instruction.Placement, plan.Output.CanvasWidth, plan.Output.CanvasHeight)
	if err != nil {
		return VideoResampleInstructionPlan{}, err
	}
	horizontal, err := compileResampleAxis(
		input.Width, plan.Output.CanvasWidth, geometry.cropX, geometry.cropWidth,
		geometry.sourceAnchorX, geometry.canvasAnchorX, geometry.scaleX,
	)
	if err != nil {
		return VideoResampleInstructionPlan{}, err
	}
	vertical, err := compileResampleAxis(
		input.Height, plan.Output.CanvasHeight, geometry.cropY, geometry.cropHeight,
		geometry.sourceAnchorY, geometry.canvasAnchorY, geometry.scaleY,
	)
	if err != nil {
		return VideoResampleInstructionPlan{}, err
	}
	horizontalWork, overflow := multiplyUint64(horizontal.TotalTaps, uint64(vertical.UniqueSourceSamples))
	if overflow {
		return VideoResampleInstructionPlan{}, ResourceLimitError{Subject: "resample-tap-work"}
	}
	verticalWork, overflow := multiplyUint64(vertical.TotalTaps, uint64(horizontal.ActiveOutputSamples))
	if overflow || math.MaxUint64-horizontalWork < verticalWork {
		return VideoResampleInstructionPlan{}, ResourceLimitError{Subject: "resample-tap-work"}
	}
	perFrame := horizontalWork + verticalWork
	firstFrame, afterFrame, err := outputFrameRange(
		instruction.TimelineRange, plan.Output.FrameRate, plan.Output.VideoFrameCount.Value(),
	)
	if err != nil {
		return VideoResampleInstructionPlan{}, err
	}
	activeFrames := afterFrame - firstFrame
	work, overflow := multiplyUint64(perFrame, activeFrames)
	if overflow {
		return VideoResampleInstructionPlan{}, ResourceLimitError{Subject: "resample-tap-work"}
	}
	return VideoResampleInstructionPlan{
		Policy: ResamplePlanPolicyV1, InstructionIndex: instructionIndex,
		Horizontal: horizontal, Vertical: vertical, ActiveFrames: activeFrames,
		TapWorkPerFrame: perFrame, TapWork: work,
	}, nil
}

func compileResampleTapWork(plan domain.RenderPlanPayload) (uint64, error) {
	var total uint64
	for index := range plan.Video {
		if index > math.MaxUint32 {
			return 0, ResourceLimitError{Subject: "resample-instructions"}
		}
		instruction, err := CompileVideoResampleInstructionPlan(plan, uint32(index))
		if err != nil {
			return 0, err
		}
		if instruction.TapWork > math.MaxUint64-total {
			return 0, ResourceLimitError{Subject: "resample-tap-work"}
		}
		total += instruction.TapWork
	}
	return total, nil
}

type videoGeometry struct {
	cropX, cropY                 *big.Rat
	cropWidth, cropHeight        *big.Rat
	sourceAnchorX, sourceAnchorY *big.Rat
	canvasAnchorX, canvasAnchorY *big.Rat
	scaleX, scaleY               *big.Rat
}

func compileVideoGeometry(
	input domain.RenderVideoInput,
	placement domain.RenderPlacement,
	canvasWidth, canvasHeight uint32,
) (videoGeometry, error) {
	if input.Width == 0 || input.Height == 0 || canvasWidth == 0 || canvasHeight == 0 ||
		placement.CropWidthBasisPoints == 0 || placement.CropHeightBasisPoints == 0 ||
		uint32(placement.CropXBasisPoints)+uint32(placement.CropWidthBasisPoints) > 10_000 ||
		uint32(placement.CropYBasisPoints)+uint32(placement.CropHeightBasisPoints) > 10_000 ||
		placement.AnchorXBasisPoints > 10_000 || placement.AnchorYBasisPoints > 10_000 ||
		placement.ScaleX.Validate() != nil || placement.ScaleY.Validate() != nil ||
		!placement.ScaleX.IsPositive() || !placement.ScaleY.IsPositive() ||
		placement.TranslateX.Validate() != nil || placement.TranslateY.Validate() != nil ||
		(placement.FitPolicy != "contain" && placement.FitPolicy != "cover") {
		return videoGeometry{}, fmt.Errorf("video resample geometry is invalid")
	}
	cropX := basisPointProduct(input.Width, placement.CropXBasisPoints)
	cropY := basisPointProduct(input.Height, placement.CropYBasisPoints)
	cropWidth := basisPointProduct(input.Width, placement.CropWidthBasisPoints)
	cropHeight := basisPointProduct(input.Height, placement.CropHeightBasisPoints)
	fitX := new(big.Rat).Quo(new(big.Rat).SetInt64(int64(canvasWidth)), cropWidth)
	fitY := new(big.Rat).Quo(new(big.Rat).SetInt64(int64(canvasHeight)), cropHeight)
	fit := new(big.Rat).Set(fitX)
	comparison := fitX.Cmp(fitY)
	if placement.FitPolicy == "contain" && comparison > 0 || placement.FitPolicy == "cover" && comparison < 0 {
		fit.Set(fitY)
	}
	scaleX := new(big.Rat).Mul(fit, exactRational(placement.ScaleX))
	scaleY := new(big.Rat).Mul(fit, exactRational(placement.ScaleY))
	anchorX := basisPointFraction(placement.AnchorXBasisPoints)
	anchorY := basisPointFraction(placement.AnchorYBasisPoints)
	sourceAnchorX := new(big.Rat).Add(cropX, new(big.Rat).Mul(cropWidth, anchorX))
	sourceAnchorY := new(big.Rat).Add(cropY, new(big.Rat).Mul(cropHeight, anchorY))
	canvasAnchorX := new(big.Rat).Mul(new(big.Rat).SetInt64(int64(canvasWidth)), anchorX)
	canvasAnchorX.Add(canvasAnchorX, new(big.Rat).Mul(
		new(big.Rat).SetInt64(int64(canvasWidth)), exactRational(placement.TranslateX),
	))
	canvasAnchorY := new(big.Rat).Mul(new(big.Rat).SetInt64(int64(canvasHeight)), anchorY)
	canvasAnchorY.Add(canvasAnchorY, new(big.Rat).Mul(
		new(big.Rat).SetInt64(int64(canvasHeight)), exactRational(placement.TranslateY),
	))
	return videoGeometry{
		cropX: cropX, cropY: cropY, cropWidth: cropWidth, cropHeight: cropHeight,
		sourceAnchorX: sourceAnchorX, sourceAnchorY: sourceAnchorY,
		canvasAnchorX: canvasAnchorX, canvasAnchorY: canvasAnchorY,
		scaleX: scaleX, scaleY: scaleY,
	}, nil
}

func compileResampleAxis(
	sourceSize, outputSize uint32,
	cropStart, cropSize, sourceAnchor, canvasAnchor, scale *big.Rat,
) (ResampleAxisPlan, error) {
	if sourceSize == 0 || outputSize == 0 || cropStart == nil || cropSize == nil || sourceAnchor == nil ||
		canvasAnchor == nil || scale == nil || cropSize.Sign() <= 0 || scale.Sign() <= 0 {
		return ResampleAxisPlan{}, fmt.Errorf("resample axis is invalid")
	}
	result := ResampleAxisPlan{
		Samples:         make([]ResampleSourceSpan, outputSize),
		sourcePositions: make([]*big.Rat, outputSize), filterScale: new(big.Rat),
	}
	unique := make([]bool, sourceSize)
	cropEnd := new(big.Rat).Add(cropStart, cropSize)
	filterScale := new(big.Rat).Set(scale)
	if filterScale.Cmp(big.NewRat(1, 1)) > 0 {
		filterScale.SetInt64(1)
	}
	result.filterScale.Set(filterScale)
	radius := new(big.Rat).Quo(big.NewRat(2, 1), filterScale)
	half := big.NewRat(1, 2)
	for output := uint32(0); output < outputSize; output++ {
		center := new(big.Rat).SetFrac(big.NewInt(int64(output)*2+1), big.NewInt(2))
		delta := new(big.Rat).Sub(center, canvasAnchor)
		sourcePosition := new(big.Rat).Add(sourceAnchor, new(big.Rat).Quo(delta, scale))
		result.sourcePositions[output] = new(big.Rat).Set(sourcePosition)
		if sourcePosition.Cmp(cropStart) < 0 || sourcePosition.Cmp(cropEnd) >= 0 {
			continue
		}
		firstCrop := ceilRational(new(big.Rat).Sub(cropStart, half))
		firstSupport := floorRational(new(big.Rat).Sub(new(big.Rat).Sub(sourcePosition, radius), half))
		firstSupport.Add(firstSupport, big.NewInt(1))
		first := maximumInteger(big.NewInt(0), firstCrop, firstSupport)
		afterCrop := ceilRational(new(big.Rat).Sub(cropEnd, half))
		afterSupport := ceilRational(new(big.Rat).Sub(new(big.Rat).Add(sourcePosition, radius), half))
		after := minimumInteger(new(big.Int).SetUint64(uint64(sourceSize)), afterCrop, afterSupport)
		if first.Cmp(after) >= 0 {
			continue
		}
		kernelCount := new(big.Int).Sub(afterSupport, firstSupport)
		count := new(big.Int).Sub(after, first)
		if !first.IsUint64() || !count.IsUint64() || !firstSupport.IsInt64() || !kernelCount.IsUint64() ||
			firstSupport.Int64() < math.MinInt32 || firstSupport.Int64() > math.MaxInt32 ||
			first.Uint64() >= uint64(sourceSize) || kernelCount.Uint64() > MaximumResampleAxisTaps ||
			count.Uint64() > kernelCount.Uint64() || first.Uint64()+count.Uint64() > uint64(sourceSize) {
			return ResampleAxisPlan{}, ResourceLimitError{Subject: "resample-axis-taps"}
		}
		span := ResampleSourceSpan{
			First: uint32(first.Uint64()), Count: uint16(count.Uint64()),
			KernelFirst: int32(firstSupport.Int64()), KernelCount: uint16(kernelCount.Uint64()),
		}
		result.Samples[output] = span
		result.ActiveOutputSamples++
		result.TotalTaps += uint64(span.Count)
		if span.Count > result.MaximumTaps {
			result.MaximumTaps = span.Count
		}
		if span.KernelCount > result.MaximumKernelTaps {
			result.MaximumKernelTaps = span.KernelCount
		}
		for source := span.First; source < span.First+uint32(span.Count); source++ {
			if !unique[source] {
				unique[source] = true
				result.UniqueSourceSamples++
			}
		}
	}
	return result, nil
}

func basisPointProduct(size uint32, basis uint16) *big.Rat {
	return new(big.Rat).SetFrac(big.NewInt(int64(size)*int64(basis)), big.NewInt(10_000))
}

func basisPointFraction(value uint16) *big.Rat {
	return new(big.Rat).SetFrac(big.NewInt(int64(value)), big.NewInt(10_000))
}

func exactRational(value domain.ExactRational) *big.Rat {
	return new(big.Rat).SetFrac(big.NewInt(value.Value.Value()), big.NewInt(int64(value.Scale)))
}

func floorRational(value *big.Rat) *big.Int {
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	if value.Sign() < 0 && remainder.Sign() != 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	return quotient
}

func ceilRational(value *big.Rat) *big.Int {
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	if value.Sign() > 0 && remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

func maximumInteger(values ...*big.Int) *big.Int {
	result := new(big.Int).Set(values[0])
	for _, value := range values[1:] {
		if value.Cmp(result) > 0 {
			result.Set(value)
		}
	}
	return result
}

func minimumInteger(values ...*big.Int) *big.Int {
	result := new(big.Int).Set(values[0])
	for _, value := range values[1:] {
		if value.Cmp(result) < 0 {
			result.Set(value)
		}
	}
	return result
}
