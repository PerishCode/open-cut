package renderengine

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/PerishCode/open-cut/product/domain"
)

type CaptionCoverageLayer struct {
	InstructionIndex uint32
	X                int32
	Y                int32
	Width            uint32
	Height           uint32
	Outline          []byte
	Fill             []byte
}

type captionCompositionBinding struct {
	instructionIndex uint32
	firstFrame       uint64
	endFrame         uint64
}

func compileCaptionCompositionBindings(
	plan domain.RenderPlanPayload,
) ([]captionCompositionBinding, error) {
	bindings := make([]captionCompositionBinding, 0, len(plan.Captions))
	for index, instruction := range plan.Captions {
		if index > math.MaxUint32 {
			return nil, ResourceLimitError{Subject: "caption-instructions"}
		}
		first, after, err := outputFrameRange(
			instruction.Range, plan.Output.FrameRate, plan.Output.VideoFrameCount.Value(),
		)
		if err != nil {
			return nil, err
		}
		if first != after {
			bindings = append(bindings, captionCompositionBinding{
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
	return bindings, nil
}

// CompositeFrameWithCaptions composites exact tight grayscale caption coverage
// after all video layers. A traversal must use either this method or
// CompositeFrame consistently; it cannot silently switch caption semantics.
func (compositor *VideoCompositor) CompositeFrameWithCaptions(
	outputFrame uint64,
	video []DecodedVideoLayer,
	captions []CaptionCoverageLayer,
) ([]byte, error) {
	if err := compositor.selectCaptionMode(true); err != nil {
		return nil, err
	}
	if err := compositor.beginVideoFrame(outputFrame, video); err != nil {
		return nil, err
	}
	compositor.activeCaptions = advanceCaptionBindings(
		compositor.activeCaptions, compositor.captionBindings, &compositor.nextCaptionBinding, outputFrame,
	)
	if len(compositor.activeCaptions) > MaximumActiveCaptionLayers ||
		len(captions) != len(compositor.activeCaptions) {
		return nil, fmt.Errorf("caption compositor active layer set is invalid")
	}
	for index, bindingIndex := range compositor.activeCaptions {
		expected := compositor.captionBindings[bindingIndex].instructionIndex
		if captions[index].InstructionIndex != expected {
			return nil, fmt.Errorf("caption compositor layer order is invalid")
		}
		if err := compositor.compositeCaptionCoverage(captions[index]); err != nil {
			return nil, err
		}
	}
	return compositor.finishComposedFrame()
}

func advanceCaptionBindings(
	active []int,
	bindings []captionCompositionBinding,
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

func (compositor *VideoCompositor) compositeCaptionCoverage(layer CaptionCoverageLayer) error {
	if int(layer.InstructionIndex) >= len(compositor.plan.Captions) {
		return fmt.Errorf("caption compositor coverage shape is invalid")
	}
	if layer.Width == 0 || layer.Height == 0 {
		if layer.Width != 0 || layer.Height != 0 || layer.X != 0 || layer.Y != 0 ||
			len(layer.Fill) != 0 || len(layer.Outline) != 0 {
			return fmt.Errorf("caption compositor empty coverage is invalid")
		}
		return nil
	}
	if layer.Width > compositor.plan.Output.CanvasWidth || layer.Height > compositor.plan.Output.CanvasHeight {
		return fmt.Errorf("caption compositor coverage shape is invalid")
	}
	area, overflow := multiplyUint64(uint64(layer.Width), uint64(layer.Height))
	if overflow || area > uint64(math.MaxInt) || len(layer.Fill) != int(area) {
		return ResourceLimitError{Subject: "caption-raster-bytes"}
	}
	instruction := compositor.plan.Captions[layer.InstructionIndex]
	if instruction.Style.OutlineBasisPoints == 0 {
		if len(layer.Outline) != 0 {
			return fmt.Errorf("caption compositor unexpected outline coverage")
		}
	} else if len(layer.Outline) != int(area) {
		return fmt.Errorf("caption compositor outline coverage is invalid")
	}
	fill, err := parseCaptionLinearColor(instruction.Style.TextColorRGBA)
	if err != nil {
		return err
	}
	outline, err := parseCaptionLinearColor(instruction.Style.OutlineColorRGBA)
	if err != nil {
		return err
	}
	if len(layer.Outline) != 0 {
		compositor.compositeCoveragePlane(layer, layer.Outline, outline)
	}
	compositor.compositeCoveragePlane(layer, layer.Fill, fill)
	return nil
}

func (compositor *VideoCompositor) compositeCoveragePlane(
	layer CaptionCoverageLayer,
	coverage []byte,
	color LinearRGBA16,
) {
	canvasWidth := int64(compositor.plan.Output.CanvasWidth)
	canvasHeight := int64(compositor.plan.Output.CanvasHeight)
	for localY := uint32(0); localY < layer.Height; localY++ {
		canvasY := int64(layer.Y) + int64(localY)
		if canvasY < 0 || canvasY >= canvasHeight {
			continue
		}
		for localX := uint32(0); localX < layer.Width; localX++ {
			canvasX := int64(layer.X) + int64(localX)
			if canvasX < 0 || canvasX >= canvasWidth {
				continue
			}
			value := coverage[localY*layer.Width+localX]
			if value == 0 {
				continue
			}
			source := captionCoverageColor(color, value)
			offset := int(canvasY*canvasWidth + canvasX)
			compositor.workspace[offset] = sourceOverLinear(source, compositor.workspace[offset])
		}
	}
}

func parseCaptionLinearColor(value string) (LinearRGBA16, error) {
	if len(value) != 9 || value[0] != '#' {
		return LinearRGBA16{}, fmt.Errorf("caption color is invalid")
	}
	channels := [4]uint8{}
	for index := range channels {
		parsed, err := strconv.ParseUint(value[1+index*2:3+index*2], 16, 8)
		if err != nil {
			return LinearRGBA16{}, fmt.Errorf("caption color is invalid")
		}
		channels[index] = uint8(parsed)
	}
	return LinearRGBA16{
		R: rec709Lookup(uint16(channels[0])*257, 0),
		G: rec709Lookup(uint16(channels[1])*257, 0),
		B: rec709Lookup(uint16(channels[2])*257, 0),
		A: uint16(channels[3]) * 257,
	}, nil
}

func captionCoverageColor(color LinearRGBA16, coverage uint8) LinearRGBA16 {
	alpha := roundHalfEvenSigned(int64(color.A)*int64(coverage), 255)
	premultiply := func(channel uint16) uint16 {
		return uint16(roundHalfEvenSigned(int64(channel)*alpha, math.MaxUint16))
	}
	return LinearRGBA16{
		R: premultiply(color.R), G: premultiply(color.G), B: premultiply(color.B), A: uint16(alpha),
	}
}
