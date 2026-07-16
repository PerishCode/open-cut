package renderengine

import (
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	ExecutionBudgetPolicyV1 = "render-bounded-streams-v2"

	MaximumActiveVideoLayers   = 32
	MaximumActiveCaptionLayers = 64
	MaximumPixelSampleWork     = uint64(1) << 43
	MaximumAudioSampleWork     = uint64(1) << 38
	MaximumResampleTapWork     = uint64(1) << 46
	MaximumCaptionTextBytes    = uint64(16) << 20
	MaximumCaptionLines        = uint64(1) << 18
	MaximumCaptionClusters     = uint64(1) << 20
	MaximumCaptionRasterBytes  = uint64(256) << 20
	MaximumDecodedVideoFrames  = 40_000_000
	MaximumDecodedAudioSamples = 8_000_000_000
	MaximumOutputBytes         = uint64(16) << 30
	MaximumIntermediateBytes   = uint64(16) << 30
	MaximumAttemptBytes        = uint64(48) << 30
	MaximumWallTimeSeconds     = 12 * 60 * 60
	VideoChunkFrames           = 1
	AudioChunkSamples          = 4_800
	minimumScratchAdmission    = uint64(256) << 20
	estimatedVideoBytesPerSec  = uint64(2) << 20
	estimatedAudioBytesPerSec  = uint64(16) << 10
)

type ResourceLimitError struct {
	Subject string
}

func (failure ResourceLimitError) Error() string {
	return "render resource limit exceeded: " + failure.Subject
}

type ExecutionBudget struct {
	Policy                  string `json:"policy"`
	PeakVideoLayers         uint16 `json:"peakVideoLayers"`
	PeakAudioLayers         uint16 `json:"peakAudioLayers"`
	PeakCaptionLayers       uint16 `json:"peakCaptionLayers"`
	PixelSampleWork         uint64 `json:"pixelSampleWork"`
	AudioSampleWork         uint64 `json:"audioSampleWork"`
	ResampleTapWork         uint64 `json:"resampleTapWork"`
	ResampleTapLimit        uint64 `json:"resampleTapLimit"`
	CaptionTextBytes        uint64 `json:"captionTextBytes"`
	CaptionTextByteLimit    uint64 `json:"captionTextByteLimit"`
	CaptionLineCount        uint64 `json:"captionLineCount"`
	CaptionLineLimit        uint64 `json:"captionLineLimit"`
	CaptionClusterCount     uint64 `json:"captionClusterCount"`
	CaptionClusterLimit     uint64 `json:"captionClusterLimit"`
	CaptionRasterBytePeak   uint64 `json:"captionRasterBytePeak"`
	CaptionRasterByteLimit  uint64 `json:"captionRasterByteLimit"`
	DecodedVideoFrameLimit  uint64 `json:"decodedVideoFrameLimit"`
	DecodedAudioSampleLimit uint64 `json:"decodedAudioSampleLimit"`
	OutputByteLimit         uint64 `json:"outputByteLimit"`
	IntermediateByteLimit   uint64 `json:"intermediateByteLimit"`
	AttemptByteLimit        uint64 `json:"attemptByteLimit"`
	ScratchAdmissionBytes   uint64 `json:"scratchAdmissionBytes"`
	WallTimeSeconds         uint32 `json:"wallTimeSeconds"`
	VideoChunkFrames        uint16 `json:"videoChunkFrames"`
	AudioChunkSamples       uint32 `json:"audioChunkSamples"`
}

func CompileExecutionBudget(plan domain.RenderPlanPayload) (ExecutionBudget, error) {
	videoPeak, err := peakRanges(videoRanges(plan.Video))
	if err != nil {
		return ExecutionBudget{}, err
	}
	audioPeak, err := peakRanges(audioRanges(plan.Audio))
	if err != nil {
		return ExecutionBudget{}, err
	}
	captionPeak, err := peakRanges(captionRanges(plan.Captions))
	if err != nil {
		return ExecutionBudget{}, err
	}
	if videoPeak > MaximumActiveVideoLayers {
		return ExecutionBudget{}, ResourceLimitError{Subject: "active-video-layers"}
	}
	if audioPeak > MaximumActiveAudioLayers {
		return ExecutionBudget{}, ResourceLimitError{Subject: "active-audio-layers"}
	}
	if captionPeak > MaximumActiveCaptionLayers {
		return ExecutionBudget{}, ResourceLimitError{Subject: "active-caption-layers"}
	}
	pixelWork, err := pixelSampleWork(plan)
	if err != nil {
		return ExecutionBudget{}, err
	}
	if pixelWork > MaximumPixelSampleWork {
		return ExecutionBudget{}, ResourceLimitError{Subject: "pixel-sample-work"}
	}
	audioWork, err := audioSampleWork(plan)
	if err != nil {
		return ExecutionBudget{}, err
	}
	if audioWork > MaximumAudioSampleWork {
		return ExecutionBudget{}, ResourceLimitError{Subject: "audio-sample-work"}
	}
	resampleWork, err := compileResampleTapWork(plan)
	if err != nil {
		return ExecutionBudget{}, err
	}
	if resampleWork > MaximumResampleTapWork {
		return ExecutionBudget{}, ResourceLimitError{Subject: "resample-tap-work"}
	}
	captionTextBytes, captionLines, captionClusters, err := captionTextWork(plan.Captions)
	if err != nil {
		return ExecutionBudget{}, err
	}
	if captionTextBytes > MaximumCaptionTextBytes {
		return ExecutionBudget{}, ResourceLimitError{Subject: "caption-text-bytes"}
	}
	if captionLines > MaximumCaptionLines {
		return ExecutionBudget{}, ResourceLimitError{Subject: "caption-lines"}
	}
	if captionClusters > MaximumCaptionClusters {
		return ExecutionBudget{}, ResourceLimitError{Subject: "caption-clusters"}
	}
	captionRasterPeak, err := captionRasterBytePeak(plan)
	if err != nil {
		return ExecutionBudget{}, err
	}
	if captionRasterPeak > MaximumCaptionRasterBytes {
		return ExecutionBudget{}, ResourceLimitError{Subject: "caption-raster-bytes"}
	}
	scratchAdmission, err := scratchAdmissionBytes(plan.Output.AudioSampleCount.Value())
	if err != nil {
		return ExecutionBudget{}, err
	}
	return ExecutionBudget{
		Policy:          ExecutionBudgetPolicyV1,
		PeakVideoLayers: uint16(videoPeak), PeakAudioLayers: uint16(audioPeak),
		PeakCaptionLayers: uint16(captionPeak), PixelSampleWork: pixelWork, AudioSampleWork: audioWork,
		ResampleTapWork: resampleWork, ResampleTapLimit: MaximumResampleTapWork,
		CaptionTextBytes: captionTextBytes, CaptionTextByteLimit: MaximumCaptionTextBytes,
		CaptionLineCount: captionLines, CaptionLineLimit: MaximumCaptionLines,
		CaptionClusterCount: captionClusters, CaptionClusterLimit: MaximumCaptionClusters,
		CaptionRasterBytePeak: captionRasterPeak, CaptionRasterByteLimit: MaximumCaptionRasterBytes,
		DecodedVideoFrameLimit: MaximumDecodedVideoFrames, DecodedAudioSampleLimit: MaximumDecodedAudioSamples,
		OutputByteLimit: MaximumOutputBytes, IntermediateByteLimit: MaximumIntermediateBytes,
		AttemptByteLimit: MaximumAttemptBytes, ScratchAdmissionBytes: scratchAdmission,
		WallTimeSeconds:  MaximumWallTimeSeconds,
		VideoChunkFrames: VideoChunkFrames, AudioChunkSamples: AudioChunkSamples,
	}, nil
}

func scratchAdmissionBytes(audioSampleCount uint64) (uint64, error) {
	seconds := audioSampleCount / domain.SequencePreviewAudioSampleRate
	if audioSampleCount%domain.SequencePreviewAudioSampleRate != 0 {
		seconds++
	}
	perSecond := estimatedVideoBytesPerSec + estimatedAudioBytesPerSec
	estimated, overflow := multiplyUint64(seconds, perSecond)
	if overflow || estimated > (MaximumAttemptBytes-minimumScratchAdmission)/2 {
		return MaximumAttemptBytes, nil
	}
	estimated = estimated*2 + minimumScratchAdmission
	if estimated > MaximumAttemptBytes {
		return MaximumAttemptBytes, nil
	}
	return estimated, nil
}

func (budget ExecutionBudget) Validate(plan domain.RenderPlanPayload) error {
	expected, err := CompileExecutionBudget(plan)
	if err != nil || budget != expected {
		return fmt.Errorf("render execution budget is invalid")
	}
	return nil
}

type budgetEvent struct {
	time  domain.RationalTime
	delta int
}

type weightedBudgetEvent struct {
	time  domain.RationalTime
	delta int64
}

func peakRanges(ranges []domain.TimeRange) (int, error) {
	events := make([]budgetEvent, 0, len(ranges)*2)
	for _, current := range ranges {
		if current.Start.Validate() != nil || current.Duration.Validate() != nil || !current.Duration.IsPositive() {
			return 0, fmt.Errorf("render budget range is invalid")
		}
		end, err := current.End()
		if err != nil {
			return 0, err
		}
		events = append(events, budgetEvent{time: current.Start, delta: 1}, budgetEvent{time: end, delta: -1})
	}
	var sortErr error
	sort.Slice(events, func(left, right int) bool {
		comparison, err := events[left].time.Compare(events[right].time)
		if err != nil {
			sortErr = err
			return false
		}
		if comparison != 0 {
			return comparison < 0
		}
		return events[left].delta < events[right].delta
	})
	if sortErr != nil {
		return 0, sortErr
	}
	active, peak := 0, 0
	for _, event := range events {
		active += event.delta
		if active < 0 {
			return 0, fmt.Errorf("render budget event order is invalid")
		}
		if active > peak {
			peak = active
		}
	}
	if active != 0 {
		return 0, fmt.Errorf("render budget event closure is invalid")
	}
	return peak, nil
}

func captionTextWork(source []domain.RenderCaptionInstruction) (uint64, uint64, uint64, error) {
	var textBytes, lines, clusters uint64
	for _, instruction := range source {
		prepared, currentBytes, currentClusters, err := prepareCaptionLines(instruction.Text)
		if err != nil {
			return 0, 0, 0, err
		}
		if math.MaxUint64-textBytes < currentBytes || math.MaxUint64-lines < uint64(len(prepared)) ||
			math.MaxUint64-clusters < currentClusters {
			return 0, 0, 0, ResourceLimitError{Subject: "caption-text-work"}
		}
		textBytes += currentBytes
		lines += uint64(len(prepared))
		clusters += currentClusters
	}
	return textBytes, lines, clusters, nil
}

func captionRasterBytePeak(plan domain.RenderPlanPayload) (uint64, error) {
	pixels, overflow := multiplyUint64(uint64(plan.Output.CanvasWidth), uint64(plan.Output.CanvasHeight))
	if overflow || pixels == 0 || pixels > math.MaxInt64/2 {
		return 0, fmt.Errorf("caption raster budget is invalid")
	}
	events := make([]weightedBudgetEvent, 0, len(plan.Captions)*2)
	for _, instruction := range plan.Captions {
		if instruction.Range.Start.Validate() != nil || instruction.Range.Duration.Validate() != nil ||
			!instruction.Range.Duration.IsPositive() {
			return 0, fmt.Errorf("caption raster range is invalid")
		}
		bytes := pixels
		if instruction.Style.OutlineBasisPoints != 0 {
			bytes *= 2
		}
		end, err := instruction.Range.End()
		if err != nil || bytes > math.MaxInt64 {
			return 0, ResourceLimitError{Subject: "caption-raster-bytes"}
		}
		events = append(events,
			weightedBudgetEvent{time: instruction.Range.Start, delta: int64(bytes)},
			weightedBudgetEvent{time: end, delta: -int64(bytes)},
		)
	}
	var sortErr error
	sort.Slice(events, func(left, right int) bool {
		comparison, err := events[left].time.Compare(events[right].time)
		if err != nil {
			sortErr = err
			return false
		}
		if comparison != 0 {
			return comparison < 0
		}
		return events[left].delta < events[right].delta
	})
	if sortErr != nil {
		return 0, sortErr
	}
	var active, peak int64
	for _, event := range events {
		if event.delta > 0 && active > math.MaxInt64-event.delta {
			return 0, ResourceLimitError{Subject: "caption-raster-bytes"}
		}
		active += event.delta
		if active < 0 {
			return 0, fmt.Errorf("caption raster event order is invalid")
		}
		if active > peak {
			peak = active
		}
	}
	if active != 0 {
		return 0, fmt.Errorf("caption raster event closure is invalid")
	}
	return uint64(peak), nil
}

func pixelSampleWork(plan domain.RenderPlanPayload) (uint64, error) {
	pixels, overflow := multiplyUint64(uint64(plan.Output.CanvasWidth), uint64(plan.Output.CanvasHeight))
	if overflow || pixels == 0 {
		return 0, fmt.Errorf("render pixel budget is invalid")
	}
	frames := plan.Output.VideoFrameCount.Value()
	for _, instruction := range plan.Video {
		count, err := ceilRateProduct(instruction.TimelineRange.Duration, plan.Output.FrameRate)
		if err != nil || math.MaxUint64-frames < count {
			return 0, fmt.Errorf("render pixel budget overflows")
		}
		frames += count
	}
	for _, instruction := range plan.Captions {
		count, err := ceilRateProduct(instruction.Range.Duration, plan.Output.FrameRate)
		if err != nil || math.MaxUint64-frames < count {
			return 0, fmt.Errorf("render pixel budget overflows")
		}
		frames += count
	}
	result, overflow := multiplyUint64(pixels, frames)
	if overflow {
		return 0, ResourceLimitError{Subject: "pixel-sample-work"}
	}
	return result, nil
}

func audioSampleWork(plan domain.RenderPlanPayload) (uint64, error) {
	result := plan.Output.AudioSampleCount.Value()
	rate, _ := domain.NewRationalTime(domain.SequencePreviewAudioSampleRate, 1)
	for _, instruction := range plan.Audio {
		count, err := ceilRateProduct(instruction.TimelineRange.Duration, rate)
		if err != nil || math.MaxUint64-result < count {
			return 0, ResourceLimitError{Subject: "audio-sample-work"}
		}
		result += count
	}
	return result, nil
}

func ceilRateProduct(duration, rate domain.RationalTime) (uint64, error) {
	if duration.Validate() != nil || rate.Validate() != nil || !duration.IsPositive() || !rate.IsPositive() {
		return 0, fmt.Errorf("render work rate is invalid")
	}
	numerator := new(big.Int).Mul(big.NewInt(duration.Value.Value()), big.NewInt(rate.Value.Value()))
	denominator := new(big.Int).Mul(big.NewInt(int64(duration.Scale)), big.NewInt(int64(rate.Scale)))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return 0, ResourceLimitError{Subject: "work-count"}
	}
	return quotient.Uint64(), nil
}

func multiplyUint64(left, right uint64) (uint64, bool) {
	if left != 0 && right > math.MaxUint64/left {
		return 0, true
	}
	return left * right, false
}

func videoRanges(source []domain.RenderVideoInstruction) []domain.TimeRange {
	result := make([]domain.TimeRange, len(source))
	for index := range source {
		result[index] = source[index].TimelineRange
	}
	return result
}

func audioRanges(source []domain.RenderAudioInstruction) []domain.TimeRange {
	result := make([]domain.TimeRange, len(source))
	for index := range source {
		result[index] = source[index].TimelineRange
	}
	return result
}

func captionRanges(source []domain.RenderCaptionInstruction) []domain.TimeRange {
	result := make([]domain.TimeRange, len(source))
	for index := range source {
		result[index] = source[index].Range
	}
	return result
}
