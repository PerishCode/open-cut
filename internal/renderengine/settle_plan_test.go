package renderengine

import (
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func settleTime(t *testing.T, value int64, scale int32) domain.RationalTime {
	t.Helper()
	time, err := domain.NewRationalTime(value, scale)
	if err != nil {
		t.Fatal(err)
	}
	return time
}

func settleRange(t *testing.T, start, duration domain.RationalTime) domain.TimeRange {
	t.Helper()
	return domain.TimeRange{Start: start, Duration: duration}
}

func settleFrameCount(t *testing.T, value uint64) domain.UInt64 {
	t.Helper()
	count, err := domain.NewUInt64(value)
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// settlePlanFixture is two seconds of 30fps output: one instruction covering
// [0s, 2s) from source 5s, and one covering [2s, 3s) from source 0s.
func settlePlanFixture(t *testing.T) domain.RenderPlanPayload {
	t.Helper()
	return domain.RenderPlanPayload{
		Duration: settleTime(t, 3, 1),
		Output: domain.RenderOutputPolicy{
			CanvasWidth: 64, CanvasHeight: 64,
			FrameRate:        settleTime(t, 30, 1),
			VideoFrameCount:  settleFrameCount(t, 90),
			AudioSampleCount: settleFrameCount(t, 144_000),
		},
		Video: []domain.RenderVideoInstruction{
			{
				TimelineRange: settleRange(t, settleTime(t, 0, 1), settleTime(t, 2, 1)),
				SourceRange:   settleRange(t, settleTime(t, 5, 1), settleTime(t, 2, 1)),
			},
			{
				TimelineRange: settleRange(t, settleTime(t, 2, 1), settleTime(t, 1, 1)),
				SourceRange:   settleRange(t, settleTime(t, 0, 1), settleTime(t, 1, 1)),
			},
		},
		Audio: []domain.RenderAudioInstruction{{
			TimelineRange: settleRange(t, settleTime(t, 0, 1), settleTime(t, 3, 1)),
			SourceRange:   settleRange(t, settleTime(t, 0, 1), settleTime(t, 3, 1)),
		}},
		Captions: []domain.RenderCaptionInstruction{
			{Text: "early", Range: settleRange(t, settleTime(t, 0, 1), settleTime(t, 1, 1))},
			{Text: "late", Range: settleRange(t, settleTime(t, 2, 1), settleTime(t, 1, 1))},
		},
	}
}

// The rebase is only worth anything if it selects the same picture. The engine's
// own mapping is the oracle: the settled plan at frame zero must derive exactly
// the source time the pinned plan derives at the requested frame.
func TestSettlePlanPreservesSourceSelectionAtEveryFrame(t *testing.T) {
	plan := settlePlanFixture(t)
	frameRate := plan.Output.FrameRate
	for _, outputFrame := range []uint64{0, 1, 29, 30, 44, 59, 60, 89} {
		settled, err := SettlePlan(plan, outputFrame)
		if err != nil {
			t.Fatalf("frame %d: %v", outputFrame, err)
		}
		if len(settled.Video) != 1 {
			t.Fatalf("frame %d: active instructions=%d", outputFrame, len(settled.Video))
		}
		settledTime, active, err := VideoInstructionSourceTime(settled.Video[0], 0, frameRate)
		if err != nil || !active {
			t.Fatalf("frame %d: settled active=%v err=%v", outputFrame, active, err)
		}
		var found bool
		for _, instruction := range plan.Video {
			pinnedTime, pinnedActive, err := VideoInstructionSourceTime(instruction, outputFrame, frameRate)
			if err != nil || !pinnedActive {
				continue
			}
			found = true
			comparison, err := settledTime.Compare(pinnedTime)
			if err != nil || comparison != 0 {
				t.Fatalf(
					"frame %d: settled source time %v is not the pinned source time %v",
					outputFrame, settledTime, pinnedTime,
				)
			}
		}
		if !found {
			t.Fatalf("frame %d: the pinned plan had no active instruction", outputFrame)
		}
	}
}

func TestSettlePlanKeepsOnlyTheInstantsThatAreActive(t *testing.T) {
	plan := settlePlanFixture(t)
	early, err := SettlePlan(plan, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(early.Video) != 1 || len(early.Captions) != 1 || early.Captions[0].Text != "early" {
		t.Fatalf("video=%d captions=%+v", len(early.Video), early.Captions)
	}
	late, err := SettlePlan(plan, 75)
	if err != nil {
		t.Fatal(err)
	}
	if len(late.Video) != 1 || len(late.Captions) != 1 || late.Captions[0].Text != "late" {
		t.Fatalf("video=%d captions=%+v", len(late.Video), late.Captions)
	}
	gapless, err := SettlePlan(plan, 45)
	if err != nil {
		t.Fatal(err)
	}
	if len(gapless.Captions) != 0 {
		t.Fatalf("no caption covers this instant: %+v", gapless.Captions)
	}
}

func TestSettlePlanDropsAudioAndBoundsTheOutputToOneFrame(t *testing.T) {
	plan := settlePlanFixture(t)
	settled, err := SettlePlan(plan, 30)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Audio != nil {
		t.Fatalf("a settled frame carries no samples: %+v", settled.Audio)
	}
	if settled.Output.VideoFrameCount.Value() != 1 || settled.Output.AudioSampleCount.Value() != 0 {
		t.Fatalf("output=%+v", settled.Output)
	}
	oneFrame := settleTime(t, 1, 30)
	comparison, err := settled.Duration.Compare(oneFrame)
	if err != nil || comparison != 0 {
		t.Fatalf("duration=%v", settled.Duration)
	}
	if plan.Audio == nil || plan.Output.VideoFrameCount.Value() != 90 {
		t.Fatal("settling mutated the pinned plan")
	}
}

func TestSettlePlanTreatsAnEmptyInstantAsAGapRatherThanAnError(t *testing.T) {
	plan := settlePlanFixture(t)
	plan.Video = []domain.RenderVideoInstruction{{
		TimelineRange: settleRange(t, settleTime(t, 0, 1), settleTime(t, 1, 1)),
		SourceRange:   settleRange(t, settleTime(t, 0, 1), settleTime(t, 1, 1)),
	}}
	settled, err := SettlePlan(plan, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(settled.Video) != 0 {
		t.Fatalf("video=%+v", settled.Video)
	}
}

func TestSettlePlanRejectsAFrameOutsideTheOutput(t *testing.T) {
	plan := settlePlanFixture(t)
	if _, err := SettlePlan(plan, 90); err == nil {
		t.Fatal("a frame past the output grid was accepted")
	}
	plan.Output.FrameRate = domain.RationalTime{}
	if _, err := SettlePlan(plan, 0); err == nil {
		t.Fatal("an invalid frame rate was accepted")
	}
}

// A rebased plan is only useful if the ordinary compositor accepts it: the
// binding for the settled instruction has to cover frame zero.
func TestSettledPlanDrivesTheOrdinaryCompositor(t *testing.T) {
	plan := settlePlanFixture(t)
	settled, err := SettlePlan(plan, 45)
	if err != nil {
		t.Fatal(err)
	}
	compositor, err := newVideoCompositor(settled)
	if err != nil {
		t.Fatal(err)
	}
	if len(compositor.bindings) != 1 {
		t.Fatalf("bindings=%+v", compositor.bindings)
	}
	if compositor.bindings[0].firstFrame != 0 || compositor.bindings[0].endFrame != 1 {
		t.Fatalf("the settled binding does not cover exactly frame zero: %+v", compositor.bindings[0])
	}
}
