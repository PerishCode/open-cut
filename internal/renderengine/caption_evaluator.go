package renderengine

import (
	"fmt"
	"math"

	"github.com/PerishCode/open-cut/product/domain"
)

type CaptionCoverageEvaluator struct {
	plan        domain.RenderPlanPayload
	budget      ExecutionBudget
	native      CaptionNativeText
	textPlans   []CaptionTextPlan
	bindings    []captionCompositionBinding
	active      []int
	nextBinding int
	nextFrame   uint64
	layers      map[uint32]CaptionCoverageLayer
	liveBytes   uint64
	finished    bool
}

func NewCaptionCoverageEvaluator(
	manifest ExecutionManifest,
	bundle CaptionFontBundle,
	native CaptionNativeText,
) (*CaptionCoverageEvaluator, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return newCaptionCoverageEvaluator(manifest.Plan, manifest.Budget, bundle, native)
}

func newCaptionCoverageEvaluator(
	plan domain.RenderPlanPayload,
	budget ExecutionBudget,
	bundle CaptionFontBundle,
	native CaptionNativeText,
) (*CaptionCoverageEvaluator, error) {
	if native == nil || bundle.Validate() != nil || budget.Validate(plan) != nil ||
		budget.PeakCaptionLayers > MaximumActiveCaptionLayers ||
		budget.CaptionRasterBytePeak > budget.CaptionRasterByteLimit ||
		budget.CaptionRasterByteLimit != MaximumCaptionRasterBytes {
		return nil, fmt.Errorf("caption evaluator input is invalid")
	}
	bindings, err := compileCaptionCompositionBindings(plan)
	if err != nil {
		return nil, err
	}
	textPlans := make([]CaptionTextPlan, len(plan.Captions))
	for index, instruction := range plan.Captions {
		if index > math.MaxUint32 {
			return nil, ResourceLimitError{Subject: "caption-instructions"}
		}
		textPlans[index], err = CompileCaptionTextPlan(
			uint32(index), instruction, plan.Output.CanvasWidth, plan.Output.CanvasHeight, bundle,
		)
		if err != nil {
			return nil, err
		}
	}
	return &CaptionCoverageEvaluator{
		plan: plan, budget: budget, native: native, textPlans: textPlans, bindings: bindings,
		active: make([]int, 0, MaximumActiveCaptionLayers),
		layers: make(map[uint32]CaptionCoverageLayer, MaximumActiveCaptionLayers),
	}, nil
}

// LayersForFrame returns every active caption in canonical instruction order.
// Raster work happens once at activation and is released immediately at the
// half-open end boundary.
func (evaluator *CaptionCoverageEvaluator) LayersForFrame(frame uint64) ([]CaptionCoverageLayer, error) {
	if evaluator.finished || frame != evaluator.nextFrame || frame >= evaluator.plan.Output.VideoFrameCount.Value() {
		return nil, fmt.Errorf("caption evaluator traversal is invalid")
	}
	evaluator.active = advanceCaptionBindings(
		evaluator.active, evaluator.bindings, &evaluator.nextBinding, frame,
	)
	wanted := make(map[uint32]struct{}, len(evaluator.active))
	for _, bindingIndex := range evaluator.active {
		wanted[evaluator.bindings[bindingIndex].instructionIndex] = struct{}{}
	}
	for instructionIndex, layer := range evaluator.layers {
		if _, exists := wanted[instructionIndex]; exists {
			continue
		}
		evaluator.liveBytes -= captionCoverageBytes(layer)
		delete(evaluator.layers, instructionIndex)
	}
	for _, bindingIndex := range evaluator.active {
		instructionIndex := evaluator.bindings[bindingIndex].instructionIndex
		if _, exists := evaluator.layers[instructionIndex]; exists {
			continue
		}
		layer, err := RasterizeCaptionText(
			evaluator.textPlans[instructionIndex], evaluator.plan.Captions[instructionIndex],
			evaluator.plan.Output.CanvasWidth, evaluator.plan.Output.CanvasHeight, evaluator.native,
		)
		if err != nil {
			return nil, err
		}
		bytes := captionCoverageBytes(layer)
		if math.MaxUint64-evaluator.liveBytes < bytes || evaluator.liveBytes+bytes > evaluator.budget.CaptionRasterBytePeak ||
			evaluator.liveBytes+bytes > evaluator.budget.CaptionRasterByteLimit {
			return nil, ResourceLimitError{Subject: "caption-raster-bytes"}
		}
		evaluator.liveBytes += bytes
		evaluator.layers[instructionIndex] = layer
	}
	result := make([]CaptionCoverageLayer, 0, len(evaluator.active))
	for _, bindingIndex := range evaluator.active {
		instructionIndex := evaluator.bindings[bindingIndex].instructionIndex
		layer, exists := evaluator.layers[instructionIndex]
		if !exists {
			return nil, fmt.Errorf("caption evaluator active layer is missing")
		}
		result = append(result, layer)
	}
	evaluator.nextFrame++
	return result, nil
}

func (evaluator *CaptionCoverageEvaluator) Finish() error {
	if evaluator.finished || evaluator.nextFrame != evaluator.plan.Output.VideoFrameCount.Value() {
		return fmt.Errorf("caption evaluator did not close its traversal")
	}
	evaluator.active = advanceCaptionBindings(
		evaluator.active, evaluator.bindings, &evaluator.nextBinding, evaluator.nextFrame,
	)
	if len(evaluator.active) != 0 || evaluator.nextBinding != len(evaluator.bindings) {
		return fmt.Errorf("caption evaluator binding closure is invalid")
	}
	evaluator.layers = nil
	evaluator.liveBytes = 0
	evaluator.finished = true
	return nil
}

func captionCoverageBytes(layer CaptionCoverageLayer) uint64 {
	return uint64(len(layer.Fill)) + uint64(len(layer.Outline))
}
