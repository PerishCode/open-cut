package renderengine

import (
	"fmt"
	"math"
	"sort"

	"github.com/PerishCode/open-cut/product/domain"
)

type DecodedVideoLayer struct {
	InstructionIndex uint32
	Frame            []byte
}

type LinearRGBA16 struct {
	R uint16
	G uint16
	B uint16
	A uint16
}

type videoCompositionBinding struct {
	instructionIndex uint32
	firstFrame       uint64
	endFrame         uint64
}

type compiledVideoLayer struct {
	instructionIndex uint32
	width            uint32
	height           uint32
	opacity          uint16
	horizontal       []ResampleAxisWeights
	vertical         []ResampleAxisWeights
}

type VideoCompositor struct {
	plan               domain.RenderPlanPayload
	bindings           []videoCompositionBinding
	nextBinding        int
	active             []int
	captionBindings    []captionCompositionBinding
	nextCaptionBinding int
	activeCaptions     []int
	captionModeSet     bool
	captionMode        bool
	nextFrame          uint64
	workspace          []LinearRGBA16
	fullCb             []uint16
	fullCr             []uint16
	output             []byte
	cache              map[uint32]*compiledVideoLayer
	finished           bool
}

func NewVideoCompositor(manifest ExecutionManifest) (*VideoCompositor, error) {
	if manifest.Validate() != nil {
		return nil, fmt.Errorf("video compositor manifest is invalid")
	}
	return newVideoCompositor(manifest.Plan)
}

func newVideoCompositor(plan domain.RenderPlanPayload) (*VideoCompositor, error) {
	width, height := plan.Output.CanvasWidth, plan.Output.CanvasHeight
	frameBytes, err := rawYUVFrameBytes(width, height)
	if err != nil || plan.Output.VideoFrameCount.Value() == 0 {
		return nil, fmt.Errorf("video compositor output is invalid")
	}
	pixels, overflow := multiplyUint64(uint64(width), uint64(height))
	if overflow || pixels > uint64(math.MaxInt) {
		return nil, ResourceLimitError{Subject: "compositor-frame-bytes"}
	}
	bindings := make([]videoCompositionBinding, 0, len(plan.Video))
	for index, instruction := range plan.Video {
		if index > math.MaxUint32 {
			return nil, ResourceLimitError{Subject: "video-instructions"}
		}
		first, after, err := outputFrameRange(
			instruction.TimelineRange, plan.Output.FrameRate, plan.Output.VideoFrameCount.Value(),
		)
		if err != nil {
			return nil, err
		}
		if first != after {
			bindings = append(bindings, videoCompositionBinding{
				instructionIndex: uint32(index), firstFrame: first, endFrame: after,
			})
		}
	}
	sort.Slice(bindings, func(left, right int) bool {
		if bindings[left].firstFrame != bindings[right].firstFrame {
			return bindings[left].firstFrame < bindings[right].firstFrame
		}
		return bindings[left].instructionIndex < bindings[right].instructionIndex
	})
	captionBindings, err := compileCaptionCompositionBindings(plan)
	if err != nil {
		return nil, err
	}
	return &VideoCompositor{
		plan: plan, bindings: bindings, active: make([]int, 0, MaximumActiveVideoLayers),
		captionBindings: captionBindings, activeCaptions: make([]int, 0, MaximumActiveCaptionLayers),
		workspace: make([]LinearRGBA16, int(pixels)), fullCb: make([]uint16, int(pixels)),
		fullCr: make([]uint16, int(pixels)), output: make([]byte, frameBytes),
		cache: make(map[uint32]*compiledVideoLayer, MaximumActiveVideoLayers),
	}, nil
}

// CompositeFrame consumes every active video instruction exactly once in
// canonical instruction order. Returned bytes are valid until the next call.
func (compositor *VideoCompositor) CompositeFrame(
	outputFrame uint64,
	layers []DecodedVideoLayer,
) ([]byte, error) {
	if err := compositor.selectCaptionMode(false); err != nil {
		return nil, err
	}
	if err := compositor.beginVideoFrame(outputFrame, layers); err != nil {
		return nil, err
	}
	return compositor.finishComposedFrame()
}

func (compositor *VideoCompositor) beginVideoFrame(
	outputFrame uint64,
	layers []DecodedVideoLayer,
) error {
	if compositor == nil || compositor.finished || outputFrame != compositor.nextFrame ||
		outputFrame >= compositor.plan.Output.VideoFrameCount.Value() {
		return fmt.Errorf("video compositor frame ordinal is invalid")
	}
	compositor.active = advanceVideoBindings(
		compositor.active, compositor.bindings, &compositor.nextBinding, outputFrame,
	)
	if len(compositor.active) > MaximumActiveVideoLayers || len(layers) != len(compositor.active) {
		return fmt.Errorf("video compositor active layer set is invalid")
	}
	activeInstructions := make(map[uint32]struct{}, len(layers))
	for index, bindingIndex := range compositor.active {
		expected := compositor.bindings[bindingIndex].instructionIndex
		if layers[index].InstructionIndex != expected {
			return fmt.Errorf("video compositor layer order is invalid")
		}
		activeInstructions[expected] = struct{}{}
	}
	for instruction := range compositor.cache {
		if _, active := activeInstructions[instruction]; !active {
			delete(compositor.cache, instruction)
		}
	}
	clear(compositor.workspace)
	for _, layer := range layers {
		compiled := compositor.cache[layer.InstructionIndex]
		if compiled == nil {
			var err error
			compiled, err = compileVideoLayer(compositor.plan, layer.InstructionIndex)
			if err != nil {
				return err
			}
			compositor.cache[layer.InstructionIndex] = compiled
		}
		if err := compositor.compositeLayer(compiled, layer.Frame); err != nil {
			return err
		}
	}
	return nil
}

func (compositor *VideoCompositor) finishComposedFrame() ([]byte, error) {
	if err := compositor.writeOutputYUV(); err != nil {
		return nil, err
	}
	compositor.nextFrame++
	return compositor.output, nil
}

func (compositor *VideoCompositor) selectCaptionMode(enabled bool) error {
	if compositor == nil {
		return fmt.Errorf("video compositor is unavailable")
	}
	if compositor.captionModeSet && compositor.captionMode != enabled {
		return fmt.Errorf("video compositor caption mode changed during traversal")
	}
	compositor.captionModeSet, compositor.captionMode = true, enabled
	return nil
}

func (compositor *VideoCompositor) Finish() error {
	if compositor == nil {
		return fmt.Errorf("video compositor is unavailable")
	}
	if compositor.finished {
		return nil
	}
	if compositor.nextFrame != compositor.plan.Output.VideoFrameCount.Value() {
		return fmt.Errorf("video compositor traversal is incomplete")
	}
	compositor.finished = true
	clear(compositor.cache)
	return nil
}

func advanceVideoBindings(
	active []int,
	bindings []videoCompositionBinding,
	next *int,
	frame uint64,
) []int {
	kept := active[:0]
	changed := false
	for _, index := range active {
		if bindings[index].endFrame > frame {
			kept = append(kept, index)
		} else {
			changed = true
		}
	}
	active = kept
	for *next < len(bindings) && bindings[*next].firstFrame == frame {
		active = append(active, *next)
		(*next)++
		changed = true
	}
	if changed {
		sort.Slice(active, func(left, right int) bool {
			return bindings[active[left]].instructionIndex < bindings[active[right]].instructionIndex
		})
	}
	return active
}

func compileVideoLayer(plan domain.RenderPlanPayload, instructionIndex uint32) (*compiledVideoLayer, error) {
	resample, err := CompileVideoResampleInstructionPlan(plan, instructionIndex)
	if err != nil {
		return nil, err
	}
	instruction := plan.Video[instructionIndex]
	var input *domain.RenderVideoInput
	for index := range plan.Inputs {
		if plan.Inputs[index].ArtifactID == instruction.InputArtifactID {
			input = plan.Inputs[index].Video
			break
		}
	}
	if input == nil {
		return nil, fmt.Errorf("video compositor source is invalid")
	}
	if instruction.Placement.OpacityBasisPoints > 10_000 {
		return nil, fmt.Errorf("video compositor opacity is invalid")
	}
	horizontal := make([]ResampleAxisWeights, len(resample.Horizontal.Samples))
	for output := range horizontal {
		horizontal[output], err = CompileResampleAxisWeights(resample.Horizontal, uint32(output))
		if err != nil {
			return nil, err
		}
	}
	vertical := make([]ResampleAxisWeights, len(resample.Vertical.Samples))
	for output := range vertical {
		vertical[output], err = CompileResampleAxisWeights(resample.Vertical, uint32(output))
		if err != nil {
			return nil, err
		}
	}
	return &compiledVideoLayer{
		instructionIndex: instructionIndex, width: input.Width, height: input.Height,
		opacity:    instruction.Placement.OpacityBasisPoints,
		horizontal: horizontal, vertical: vertical,
	}, nil
}

func (compositor *VideoCompositor) compositeLayer(layer *compiledVideoLayer, frame []byte) error {
	frameBytes, err := rawYUVFrameBytes(layer.width, layer.height)
	if err != nil || len(frame) != frameBytes {
		return fmt.Errorf("video compositor source frame is invalid")
	}
	width := int(compositor.plan.Output.CanvasWidth)
	usage := make([]uint16, layer.height)
	for _, weights := range layer.vertical {
		for index := range weights.Coefficients {
			source := weights.First + uint32(index)
			if source >= layer.height || usage[source] == math.MaxUint16 {
				return ResourceLimitError{Subject: "compositor-row-cache"}
			}
			usage[source]++
		}
	}
	rows := make(map[uint32][]LinearRGBA16, MaximumResampleAxisTaps)
	sourceScratch := make([]LinearRGBA16, layer.width)
	verticalSamples := make([]LinearRGBA16, MaximumResampleAxisTaps)
	for outputY, vertical := range layer.vertical {
		if len(vertical.Coefficients) == 0 {
			continue
		}
		for index := range vertical.Coefficients {
			sourceY := vertical.First + uint32(index)
			if _, exists := rows[sourceY]; !exists {
				if len(rows) >= MaximumResampleAxisTaps {
					return ResourceLimitError{Subject: "compositor-row-cache"}
				}
				row, err := horizontalResampleRow(layer, frame, sourceY, sourceScratch)
				if err != nil {
					return err
				}
				rows[sourceY] = row
			}
		}
		for outputX, horizontal := range layer.horizontal {
			if len(horizontal.Coefficients) == 0 {
				continue
			}
			for index := range vertical.Coefficients {
				verticalSamples[index] = rows[vertical.First+uint32(index)][outputX]
			}
			source := weightedLinearRGBA16(verticalSamples[:len(vertical.Coefficients)], vertical.Coefficients)
			source = applyLinearOpacity(source, layer.opacity)
			offset := outputY*width + outputX
			compositor.workspace[offset] = sourceOverLinear(source, compositor.workspace[offset])
		}
		for index := range vertical.Coefficients {
			sourceY := vertical.First + uint32(index)
			usage[sourceY]--
			if usage[sourceY] == 0 {
				delete(rows, sourceY)
			}
		}
	}
	if len(rows) != 0 {
		return fmt.Errorf("video compositor row cache is incomplete")
	}
	return nil
}

func horizontalResampleRow(
	layer *compiledVideoLayer,
	frame []byte,
	sourceY uint32,
	source []LinearRGBA16,
) ([]LinearRGBA16, error) {
	if sourceY >= layer.height || uint32(len(source)) != layer.width {
		return nil, fmt.Errorf("video compositor source row is invalid")
	}
	pixels := int(layer.width * layer.height)
	chromaBytes := pixels / 4
	yPlane := frame[:pixels]
	cbPlane := frame[pixels : pixels+chromaBytes]
	crPlane := frame[pixels+chromaBytes:]
	for sourceX := uint32(0); sourceX < layer.width; sourceX++ {
		cb, err := ReconstructLeftChroma420(
			cbPlane, int(layer.width/2), int(layer.height/2), int(sourceX), int(sourceY),
		)
		if err != nil {
			return nil, err
		}
		cr, err := ReconstructLeftChroma420(
			crPlane, int(layer.width/2), int(layer.height/2), int(sourceX), int(sourceY),
		)
		if err != nil {
			return nil, err
		}
		rgb := LimitedRec709ToLinearRGB16(YUV8{
			Y: yPlane[int(sourceY*layer.width+sourceX)], Cb: cb, Cr: cr,
		})
		source[sourceX] = LinearRGBA16{R: rgb.R, G: rgb.G, B: rgb.B, A: math.MaxUint16}
	}
	result := make([]LinearRGBA16, len(layer.horizontal))
	for outputX, weights := range layer.horizontal {
		if len(weights.Coefficients) == 0 {
			continue
		}
		end := uint64(weights.First) + uint64(len(weights.Coefficients))
		if end > uint64(len(source)) {
			return nil, fmt.Errorf("video compositor horizontal span is invalid")
		}
		result[outputX] = weightedLinearRGBA16(
			source[weights.First:uint32(end)], weights.Coefficients,
		)
	}
	return result, nil
}

func weightedLinearRGBA16(samples []LinearRGBA16, coefficients []int64) LinearRGBA16 {
	var red, green, blue, alpha int64
	for index, coefficient := range coefficients {
		red += int64(samples[index].R) * coefficient
		green += int64(samples[index].G) * coefficient
		blue += int64(samples[index].B) * coefficient
		alpha += int64(samples[index].A) * coefficient
	}
	result := LinearRGBA16{
		R: clampUint16(roundHalfEvenSigned(red, resampleCoefficientOneQ30)),
		G: clampUint16(roundHalfEvenSigned(green, resampleCoefficientOneQ30)),
		B: clampUint16(roundHalfEvenSigned(blue, resampleCoefficientOneQ30)),
		A: clampUint16(roundHalfEvenSigned(alpha, resampleCoefficientOneQ30)),
	}
	if result.R > result.A {
		result.R = result.A
	}
	if result.G > result.A {
		result.G = result.A
	}
	if result.B > result.A {
		result.B = result.A
	}
	return result
}

func applyLinearOpacity(value LinearRGBA16, opacity uint16) LinearRGBA16 {
	return LinearRGBA16{
		R: uint16(roundHalfEvenSigned(int64(value.R)*int64(opacity), 10_000)),
		G: uint16(roundHalfEvenSigned(int64(value.G)*int64(opacity), 10_000)),
		B: uint16(roundHalfEvenSigned(int64(value.B)*int64(opacity), 10_000)),
		A: uint16(roundHalfEvenSigned(int64(value.A)*int64(opacity), 10_000)),
	}
}

func sourceOverLinear(source, destination LinearRGBA16) LinearRGBA16 {
	inverse := int64(math.MaxUint16 - source.A)
	compose := func(front, back uint16) uint16 {
		return clampUint16(int64(front) + roundHalfEvenSigned(int64(back)*inverse, math.MaxUint16))
	}
	result := LinearRGBA16{
		R: compose(source.R, destination.R), G: compose(source.G, destination.G),
		B: compose(source.B, destination.B), A: compose(source.A, destination.A),
	}
	if result.R > result.A {
		result.R = result.A
	}
	if result.G > result.A {
		result.G = result.A
	}
	if result.B > result.A {
		result.B = result.A
	}
	return result
}

func (compositor *VideoCompositor) writeOutputYUV() error {
	width, height := int(compositor.plan.Output.CanvasWidth), int(compositor.plan.Output.CanvasHeight)
	pixels := width * height
	for index, pixel := range compositor.workspace {
		converted := LinearRGB16ToLimitedRec709(RGB16{R: pixel.R, G: pixel.G, B: pixel.B})
		compositor.output[index] = converted.Y
		compositor.fullCb[index] = uint16(converted.Cb)
		compositor.fullCr[index] = uint16(converted.Cr)
	}
	chromaBytes := pixels / 4
	if err := writeDownsampledChroma(
		compositor.fullCb, width, height, compositor.output[pixels:pixels+chromaBytes],
	); err != nil {
		return err
	}
	return writeDownsampledChroma(
		compositor.fullCr, width, height, compositor.output[pixels+chromaBytes:],
	)
}

func writeDownsampledChroma(full []uint16, width, height int, destination []byte) error {
	if width <= 0 || height <= 0 || width%2 != 0 || height%2 != 0 || len(full) != width*height ||
		len(destination) != width*height/4 {
		return fmt.Errorf(
			"%w: output chroma shape w=%d h=%d full=%d dst=%d",
			ErrIntegerOracleInput, width, height, len(full), len(destination),
		)
	}
	chromaWidth := width / 2
	for y := 0; y < height/2; y++ {
		firstRow, secondRow := y*2, y*2+1
		for x := 0; x < chromaWidth; x++ {
			center := x * 2
			left, right := center-1, center+1
			if left < 0 {
				left = 0
			}
			if right >= width {
				right = width - 1
			}
			sum := int64(full[firstRow*width+left]) + 2*int64(full[firstRow*width+center]) +
				int64(full[firstRow*width+right]) + int64(full[secondRow*width+left]) +
				2*int64(full[secondRow*width+center]) + int64(full[secondRow*width+right])
			destination[y*chromaWidth+x] = uint8(roundHalfEvenSigned(sum, 8))
		}
	}
	return nil
}
