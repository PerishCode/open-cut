package renderengine

import (
	"errors"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestExecutionBudgetIsDerivedAndHalfOpen(t *testing.T) {
	plan := captionRenderPlan(t).Plan.Payload
	one, _ := domain.NewRationalTime(1, 1)
	plan.Captions = append(plan.Captions, plan.Captions[0])
	plan.Captions[1].Range.Start = one
	budget, err := CompileExecutionBudget(plan)
	if err != nil {
		t.Fatal(err)
	}
	if budget.PeakCaptionLayers != 1 || budget.PeakVideoLayers != 0 || budget.PeakAudioLayers != 0 ||
		budget.PixelSampleWork == 0 || budget.AudioSampleWork != plan.Output.AudioSampleCount.Value() ||
		budget.ResampleTapWork != 0 || budget.ResampleTapLimit != MaximumResampleTapWork ||
		budget.CaptionTextBytes != uint64(len(plan.Captions[0].Text))*2 || budget.CaptionLineCount != 2 ||
		budget.CaptionClusterCount == 0 || budget.CaptionTextByteLimit != MaximumCaptionTextBytes ||
		budget.CaptionLineLimit != MaximumCaptionLines || budget.CaptionClusterLimit != MaximumCaptionClusters ||
		budget.CaptionRasterBytePeak == 0 || budget.CaptionRasterByteLimit != MaximumCaptionRasterBytes {
		t.Fatalf("budget=%+v", budget)
	}
	mutated := budget
	mutated.AttemptByteLimit++
	if mutated.Validate(plan) == nil {
		t.Fatal("mutated execution budget was accepted")
	}
}

func TestCaptionBudgetCountsExpandedTextAndWeightedHalfOpenRasters(t *testing.T) {
	plan := captionRenderPlan(t).Plan.Payload
	plan.Captions[0].Text = "a\tb\n"
	one, _ := domain.NewRationalTime(1, 1)
	second := plan.Captions[0]
	second.Range.Start = one
	second.Style.OutlineBasisPoints = 0
	plan.Captions = append(plan.Captions, second)
	budget, err := CompileExecutionBudget(plan)
	if err != nil {
		t.Fatal(err)
	}
	pixels := uint64(plan.Output.CanvasWidth) * uint64(plan.Output.CanvasHeight)
	if budget.CaptionTextBytes != 12 || budget.CaptionLineCount != 4 || budget.CaptionClusterCount != 12 ||
		budget.CaptionRasterBytePeak != pixels*2 {
		t.Fatalf("budget=%+v", budget)
	}
}

func TestExecutionBudgetRejectsActiveLayerAndWorkOverflow(t *testing.T) {
	plan := captionRenderPlan(t).Plan.Payload
	caption := plan.Captions[0]
	plan.Captions = nil
	for range MaximumActiveCaptionLayers + 1 {
		plan.Captions = append(plan.Captions, caption)
	}
	_, err := CompileExecutionBudget(plan)
	var limit ResourceLimitError
	if !errors.As(err, &limit) || limit.Subject != "active-caption-layers" {
		t.Fatalf("limit=%+v err=%v", limit, err)
	}
	plan = captionRenderPlan(t).Plan.Payload
	plan.Captions = nil
	for range MaximumActiveAudioLayers + 1 {
		plan.Audio = append(plan.Audio, domain.RenderAudioInstruction{TimelineRange: caption.Range})
	}
	_, err = CompileExecutionBudget(plan)
	limit = ResourceLimitError{}
	if !errors.As(err, &limit) || limit.Subject != "active-audio-layers" {
		t.Fatalf("limit=%+v err=%v", limit, err)
	}
	plan = captionRenderPlan(t).Plan.Payload
	plan.Output.CanvasWidth = 16_384
	plan.Output.CanvasHeight = 16_384
	plan.Output.VideoFrameCount, _ = domain.NewUInt64(10_000_000)
	_, err = CompileExecutionBudget(plan)
	limit = ResourceLimitError{}
	if !errors.As(err, &limit) || limit.Subject != "pixel-sample-work" {
		t.Fatalf("limit=%+v err=%v", limit, err)
	}
}
