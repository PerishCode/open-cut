package renderengine

import "testing"

func TestCaptionCoverageEvaluatorRasterizesOnceAndClosesTraversal(t *testing.T) {
	plan := captionRenderPlan(t).Plan.Payload
	plan.Captions[0].Text = "a"
	budget, err := CompileExecutionBudget(plan)
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := NewPinnedCaptionFontBundle(captionTextFixtureFontFiles())
	native := &fixtureCaptionNative{}
	evaluator, err := newCaptionCoverageEvaluator(plan, budget, bundle, native)
	if err != nil {
		t.Fatal(err)
	}
	frameCount := plan.Output.VideoFrameCount.Value()
	for frame := uint64(0); frame < frameCount; frame++ {
		layers, frameErr := evaluator.LayersForFrame(frame)
		if frameErr != nil || len(layers) != 1 || layers[0].InstructionIndex != 0 {
			t.Fatalf("frame=%d layers=%+v err=%v", frame, layers, frameErr)
		}
	}
	if len(native.shapes) != 1 {
		t.Fatalf("shape calls=%d", len(native.shapes))
	}
	if err := evaluator.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := evaluator.LayersForFrame(frameCount); err == nil {
		t.Fatal("finished caption evaluator accepted another frame")
	}
}

func TestCaptionCoverageEvaluatorRejectsOutOfOrderTraversal(t *testing.T) {
	plan := captionRenderPlan(t).Plan.Payload
	budget, _ := CompileExecutionBudget(plan)
	bundle, _ := NewPinnedCaptionFontBundle(captionTextFixtureFontFiles())
	evaluator, err := newCaptionCoverageEvaluator(plan, budget, bundle, &fixtureCaptionNative{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evaluator.LayersForFrame(1); err == nil {
		t.Fatal("caption evaluator accepted a skipped frame")
	}
	if err := evaluator.Finish(); err == nil {
		t.Fatal("caption evaluator accepted an incomplete traversal")
	}
}
