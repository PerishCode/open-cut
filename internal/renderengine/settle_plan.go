package renderengine

import (
	"errors"
	"fmt"
	"math"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

// ErrSettleInstantEmpty reports that nothing is active at the requested instant.
// A render plan cannot express one frame of nothing: its duration must equal the
// latest instruction end, so an instant with no instruction has no valid plan.
// Emptiness is signalled here rather than encoded, exactly as an empty Sequence
// is signalled instead of compiled.
var ErrSettleInstantEmpty = errors.New("settle plan instant has no active instruction")

// SettlePlan rebases a pinned plan onto the single output frame at outputFrame,
// so that frame becomes frame zero of a one-frame plan.
//
// The compositor advances strictly forward from frame zero, which is what makes
// a full-sequence traversal cheap and a random-access frame impossible. Rebasing
// is how one instant becomes reachable without loosening that: the returned plan
// is an ordinary valid plan whose only frame is the requested one, composited by
// the ordinary path.
//
// The rebase preserves source selection exactly. For every instruction still
// active, the source time VideoInstructionSourceTime derives from the rebased
// plan at frame zero equals the source time it derives from the pinned plan at
// outputFrame, so the settled frame selects the same decoded picture the
// committed composition would.
//
// Audio is dropped: a settled frame carries no samples, and the derived output
// policy still declares the silence its profile requires. Inputs and fonts are
// pruned to what the surviving instructions reference, because a plan may not
// carry material nothing uses.
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
	settled.Inputs = nil
	settled.FontResources = nil
	usedArtifacts := make(map[string]struct{}, len(plan.Video))
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
		usedArtifacts[instruction.InputArtifactID.String()] = struct{}{}
	}
	usedFonts := make(map[string]struct{}, len(plan.Captions))
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
		usedFonts[instruction.Style.FontResourceID] = struct{}{}
	}
	if len(settled.Video) == 0 && len(settled.Captions) == 0 {
		return domain.RenderPlanPayload{}, ErrSettleInstantEmpty
	}
	for _, input := range plan.Inputs {
		if _, used := usedArtifacts[input.ArtifactID.String()]; used {
			settled.Inputs = append(settled.Inputs, input)
		}
	}
	for _, font := range plan.FontResources {
		if _, used := usedFonts[font.ResourceID]; used {
			settled.FontResources = append(settled.FontResources, font)
		}
	}
	settled.Output, err = settleOutputPolicy(plan.Purpose, settled.SequenceFormat, frameDuration)
	if err != nil {
		return domain.RenderPlanPayload{}, err
	}
	return settled, nil
}

// settleOutputPolicy derives the whole output policy from the settled duration.
// The contract requires the policy to be exactly what the purpose's own function
// produces, so it is derived rather than edited: hand-setting a frame count is
// how a plan silently stops being the plan its purpose describes.
func settleOutputPolicy(
	purpose domain.RenderPlanPurpose,
	format domain.SequenceFormat,
	duration domain.RationalTime,
) (domain.RenderOutputPolicy, error) {
	switch purpose {
	case domain.RenderPurposeSequencePreview:
		return rendercontract.SequencePreviewOutput(format, duration)
	case domain.RenderPurposeExport:
		return rendercontract.SequenceExportOutput(format, duration)
	default:
		return domain.RenderOutputPolicy{}, fmt.Errorf("settle plan purpose is invalid")
	}
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
