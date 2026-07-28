package renderengine

import (
	"fmt"
	"math"

	"github.com/PerishCode/open-cut/product/domain"
)

// SettlePlan rebases a pinned plan onto the single output frame at outputFrame,
// so that frame becomes frame zero of a one-frame plan.
//
// The compositor advances strictly forward from frame zero, which is what makes
// a full-sequence traversal cheap and a random-access frame impossible. Rebasing
// is how one instant becomes reachable without loosening that: the returned plan
// is an ordinary plan whose only frame is the requested one, composited by the
// ordinary path.
//
// The rebase preserves source selection exactly. For every instruction still
// active, the source time VideoInstructionSourceTime derives from the rebased
// plan at frame zero equals the source time it derives from the pinned plan at
// outputFrame, so the settled frame selects the same decoded picture the
// committed composition would.
//
// Audio is dropped: a settled frame carries no samples. Instructions that are
// not active at the requested instant are dropped, and a frame with nothing
// active is a legitimate gap rather than an error.
func SettlePlan(
	plan domain.RenderPlanPayload,
	outputFrame uint64,
) (domain.RenderPlanPayload, error) {
	frameRate := plan.Output.FrameRate
	if frameRate.Validate() != nil || !frameRate.IsPositive() ||
		outputFrame >= plan.Output.VideoFrameCount.Value() {
		return domain.RenderPlanPayload{}, fmt.Errorf("settle plan output frame is invalid")
	}
	frameDuration, err := oneFrameDuration(frameRate)
	if err != nil {
		return domain.RenderPlanPayload{}, err
	}
	origin, err := domain.NewRationalTime(0, 1)
	if err != nil {
		return domain.RenderPlanPayload{}, err
	}
	outputTime, err := settleOutputTime(outputFrame, frameRate)
	if err != nil {
		return domain.RenderPlanPayload{}, err
	}
	settled := plan
	settled.Duration = frameDuration
	settled.Audio = nil
	settled.Video = nil
	settled.Captions = nil
	for _, instruction := range plan.Video {
		sourceTime, active, err := VideoInstructionSourceTime(instruction, outputFrame, frameRate)
		if err != nil {
			return domain.RenderPlanPayload{}, err
		}
		if !active {
			continue
		}
		rebased := instruction
		rebased.TimelineRange = domain.TimeRange{Start: origin, Duration: frameDuration}
		rebased.SourceRange = domain.TimeRange{Start: sourceTime, Duration: frameDuration}
		settled.Video = append(settled.Video, rebased)
	}
	for _, instruction := range plan.Captions {
		active, err := coversOutputTime(instruction.Range, outputTime)
		if err != nil {
			return domain.RenderPlanPayload{}, err
		}
		if !active {
			continue
		}
		rebased := instruction
		rebased.Range = domain.TimeRange{Start: origin, Duration: frameDuration}
		settled.Captions = append(settled.Captions, rebased)
	}
	frameCount, err := domain.NewUInt64(1)
	if err != nil {
		return domain.RenderPlanPayload{}, err
	}
	sampleCount, err := domain.NewUInt64(0)
	if err != nil {
		return domain.RenderPlanPayload{}, err
	}
	settled.Output.VideoFrameCount = frameCount
	settled.Output.AudioSampleCount = sampleCount
	return settled, nil
}

func oneFrameDuration(frameRate domain.RationalTime) (domain.RationalTime, error) {
	value := frameRate.Value.Value()
	if value <= 0 || value > math.MaxInt32 {
		return domain.RationalTime{}, fmt.Errorf("settle plan frame rate is invalid")
	}
	return domain.NewRationalTime(int64(frameRate.Scale), int32(value))
}

func settleOutputTime(
	outputFrame uint64,
	frameRate domain.RationalTime,
) (domain.RationalTime, error) {
	numerator, overflow := multiplyUint64(outputFrame, uint64(frameRate.Scale))
	if overflow || numerator > math.MaxInt64 || frameRate.Value.Value() > math.MaxInt32 {
		return domain.RationalTime{}, ResourceLimitError{Subject: "output-frame-time"}
	}
	return domain.NewRationalTime(int64(numerator), int32(frameRate.Value.Value()))
}

func coversOutputTime(value domain.TimeRange, outputTime domain.RationalTime) (bool, error) {
	if value.Start.Validate() != nil || value.Duration.Validate() != nil ||
		!value.Duration.IsPositive() {
		return false, fmt.Errorf("settle plan caption range is invalid")
	}
	end, err := value.End()
	if err != nil {
		return false, err
	}
	start, err := outputTime.Compare(value.Start)
	if err != nil {
		return false, err
	}
	after, err := outputTime.Compare(end)
	if err != nil {
		return false, err
	}
	return start >= 0 && after < 0, nil
}
