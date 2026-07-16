package renderengine

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/PerishCode/open-cut/product/domain"
)

const AudioDecodePlanPolicyV1 = "monotonic-s16-lanes-ordinal-zero-v1"

var ErrAudioSourceRangeInvalid = errors.New("audio source range is outside the decoded proxy")

type AudioDecodePlan struct {
	Policy           string
	Lanes            []AudioDecodeLane
	TraversalSamples uint64
}

type AudioDecodeLane struct {
	Index uint16
	Runs  []AudioDecodeRun
}

type AudioDecodeRun struct {
	InputIndex       uint32
	InputArtifactID  domain.ArtifactID
	SourceStreamID   domain.SourceStreamID
	Requests         []AudioDecodeRequest
	LastOrdinal      uint64
	TraversalSamples uint64
}

type AudioDecodeRequest struct {
	InstructionIndex  uint32
	FirstOutputSample uint64
	EndOutputSample   uint64
	FirstOrdinal      uint64
	LastOrdinal       uint64
}

type audioDecodeSource struct {
	inputIndex uint32
	input      domain.RenderAudioInput
}

type audioDecodeCandidate struct {
	request    AudioDecodeRequest
	inputIndex uint32
	artifactID domain.ArtifactID
	streamID   domain.SourceStreamID
	layer      uint16
	clipID     string
}

func CompileAudioDecodePlan(manifest ExecutionManifest) (AudioDecodePlan, error) {
	if manifest.Validate() != nil {
		return AudioDecodePlan{}, fmt.Errorf("audio decode manifest is invalid")
	}
	sources := make(map[string]audioDecodeSource, len(manifest.Plan.Inputs))
	for index, input := range manifest.Plan.Inputs {
		if input.Audio == nil {
			continue
		}
		sources[input.ArtifactID.String()] = audioDecodeSource{
			inputIndex: uint32(index), input: *input.Audio,
		}
	}
	return planAudioDecodeLanes(manifest.Plan, sources, manifest.Budget.DecodedAudioSampleLimit)
}

func planAudioDecodeLanes(
	plan domain.RenderPlanPayload,
	sources map[string]audioDecodeSource,
	sampleLimit uint64,
) (AudioDecodePlan, error) {
	if plan.Output.Audio.SampleRate != domain.SequencePreviewAudioSampleRate ||
		plan.Output.AudioSampleCount.Value() == 0 || sampleLimit == 0 || sampleLimit > MaximumDecodedAudioSamples {
		return AudioDecodePlan{}, fmt.Errorf("audio decode plan input is invalid")
	}
	candidates := make([]audioDecodeCandidate, 0, len(plan.Audio))
	for index, instruction := range plan.Audio {
		source, exists := sources[instruction.InputArtifactID.String()]
		if !exists || source.input.SourceStreamID != instruction.SourceStreamID || index > math.MaxUint32 {
			return AudioDecodePlan{}, fmt.Errorf("audio decode source is invalid")
		}
		request, present, err := audioDecodeRequest(
			uint32(index), instruction, source.input, plan.Output.AudioSampleCount.Value(),
		)
		if err != nil {
			return AudioDecodePlan{}, err
		}
		if !present {
			continue
		}
		candidates = append(candidates, audioDecodeCandidate{
			request: request, inputIndex: source.inputIndex, artifactID: instruction.InputArtifactID,
			streamID: instruction.SourceStreamID, layer: instruction.Layer, clipID: instruction.ClipID.String(),
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		return audioDecodeCandidateLess(candidates[left], candidates[right])
	})
	result := AudioDecodePlan{Policy: AudioDecodePlanPolicyV1, Lanes: []AudioDecodeLane{}}
	laneEnds := make([]uint64, 0)
	for _, candidate := range candidates {
		lane := selectAudioDecodeLane(result.Lanes, laneEnds, candidate)
		if lane == len(result.Lanes) {
			if lane >= MaximumActiveAudioLayers {
				return AudioDecodePlan{}, ResourceLimitError{Subject: "audio-decode-lanes"}
			}
			result.Lanes = append(result.Lanes, AudioDecodeLane{Index: uint16(lane), Runs: []AudioDecodeRun{}})
			laneEnds = append(laneEnds, 0)
		}
		appendAudioDecodeCandidate(&result.Lanes[lane], candidate)
		laneEnds[lane] = candidate.request.EndOutputSample
	}
	for laneIndex := range result.Lanes {
		for runIndex := range result.Lanes[laneIndex].Runs {
			run := &result.Lanes[laneIndex].Runs[runIndex]
			run.TraversalSamples = run.LastOrdinal + 1
			if run.TraversalSamples > sampleLimit-result.TraversalSamples {
				return AudioDecodePlan{}, ResourceLimitError{Subject: "decoded-audio-samples"}
			}
			result.TraversalSamples += run.TraversalSamples
		}
	}
	return result, nil
}

func audioDecodeRequest(
	index uint32,
	instruction domain.RenderAudioInstruction,
	input domain.RenderAudioInput,
	outputCount uint64,
) (AudioDecodeRequest, bool, error) {
	first, after, err := outputSampleRange(instruction.TimelineRange, outputCount)
	if err != nil || first == after {
		return AudioDecodeRequest{}, false, err
	}
	sourceTime, active, err := AudioInstructionSourceTime(instruction, first)
	if err != nil || !active {
		return AudioDecodeRequest{}, false, fmt.Errorf("audio decode first sample is invalid")
	}
	firstOrdinal, err := audioTrackFloorOrdinal(sourceTime, input.SourceStart)
	if err != nil {
		return AudioDecodeRequest{}, false, err
	}
	decodeFirst := first
	if firstOrdinal.Sign() < 0 {
		silence := new(big.Int).Neg(firstOrdinal)
		available := new(big.Int).SetUint64(after - first)
		if silence.Cmp(available) >= 0 {
			return AudioDecodeRequest{}, false, nil
		}
		if !silence.IsUint64() {
			return AudioDecodeRequest{}, false, ResourceLimitError{Subject: "audio-sample-time"}
		}
		decodeFirst += silence.Uint64()
		firstOrdinal.SetInt64(0)
	}
	if !firstOrdinal.IsUint64() {
		return AudioDecodeRequest{}, false, ResourceLimitError{Subject: "audio-sample-time"}
	}
	ordinal := firstOrdinal.Uint64()
	length := after - decodeFirst
	if length == 0 || ordinal > ^uint64(0)-(length-1) {
		return AudioDecodeRequest{}, false, ResourceLimitError{Subject: "audio-sample-time"}
	}
	last := ordinal + length - 1
	if input.DecodedSampleCount.Value() == 0 || last >= input.DecodedSampleCount.Value() {
		return AudioDecodeRequest{}, false, fmt.Errorf("%w: instruction %d", ErrAudioSourceRangeInvalid, index)
	}
	return AudioDecodeRequest{
		InstructionIndex: index, FirstOutputSample: decodeFirst, EndOutputSample: after,
		FirstOrdinal: ordinal, LastOrdinal: last,
	}, true, nil
}

func outputSampleRange(value domain.TimeRange, outputCount uint64) (uint64, uint64, error) {
	if value.Start.Validate() != nil || value.Start.IsNegative() || value.Duration.Validate() != nil ||
		!value.Duration.IsPositive() || outputCount == 0 {
		return 0, 0, fmt.Errorf("audio decode timeline range is invalid")
	}
	end, err := value.End()
	if err != nil {
		return 0, 0, err
	}
	first, err := ceilAudioSampleIndex(value.Start)
	if err != nil {
		return 0, 0, err
	}
	after, err := ceilAudioSampleIndex(end)
	if err != nil {
		return 0, 0, err
	}
	if first > outputCount {
		first = outputCount
	}
	if after > outputCount {
		after = outputCount
	}
	if after < first {
		return 0, 0, fmt.Errorf("audio decode output range is invalid")
	}
	return first, after, nil
}

func ceilAudioSampleIndex(value domain.RationalTime) (uint64, error) {
	if value.Validate() != nil || value.IsNegative() {
		return 0, fmt.Errorf("audio sample boundary is invalid")
	}
	numerator := new(big.Int).Mul(
		big.NewInt(value.Value.Value()), big.NewInt(domain.SequencePreviewAudioSampleRate),
	)
	denominator := big.NewInt(int64(value.Scale))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return 0, ResourceLimitError{Subject: "audio-sample-time"}
	}
	return quotient.Uint64(), nil
}

func AudioInstructionSourceTime(
	instruction domain.RenderAudioInstruction,
	outputSample uint64,
) (domain.RationalTime, bool, error) {
	if outputSample > math.MaxInt64 || instruction.SourceRange.Start.Validate() != nil ||
		instruction.SourceRange.Duration.Validate() != nil || instruction.TimelineRange.Start.Validate() != nil ||
		instruction.TimelineRange.Duration.Validate() != nil || !instruction.SourceRange.Duration.IsPositive() ||
		!instruction.TimelineRange.Duration.IsPositive() || instruction.TimelineRange.Start.IsNegative() {
		return domain.RationalTime{}, false, fmt.Errorf("audio source-time input is invalid")
	}
	equal, err := instruction.SourceRange.Duration.Compare(instruction.TimelineRange.Duration)
	if err != nil || equal != 0 {
		return domain.RationalTime{}, false, fmt.Errorf("audio source-time ranges are invalid")
	}
	outputTime, err := domain.NewRationalTime(int64(outputSample), domain.SequencePreviewAudioSampleRate)
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	end, err := instruction.TimelineRange.End()
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	startComparison, err := outputTime.Compare(instruction.TimelineRange.Start)
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	endComparison, err := outputTime.Compare(end)
	if err != nil || startComparison < 0 || endComparison >= 0 {
		return domain.RationalTime{}, false, err
	}
	negativeStart, err := domain.NewRationalTime(
		-instruction.TimelineRange.Start.Value.Value(), instruction.TimelineRange.Start.Scale,
	)
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	offset, err := outputTime.Add(negativeStart)
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	sourceTime, err := instruction.SourceRange.Start.Add(offset)
	return sourceTime, err == nil, err
}

func audioTrackFloorOrdinal(sourceTime, sourceStart domain.RationalTime) (*big.Int, error) {
	if sourceTime.Validate() != nil || sourceStart.Validate() != nil {
		return nil, fmt.Errorf("audio sample mapping input is invalid")
	}
	numerator := new(big.Int).Mul(
		big.NewInt(sourceTime.Value.Value()), big.NewInt(int64(sourceStart.Scale)),
	)
	numerator.Sub(numerator, new(big.Int).Mul(
		big.NewInt(sourceStart.Value.Value()), big.NewInt(int64(sourceTime.Scale)),
	))
	numerator.Mul(numerator, big.NewInt(domain.SequencePreviewAudioSampleRate))
	denominator := new(big.Int).Mul(
		big.NewInt(int64(sourceTime.Scale)), big.NewInt(int64(sourceStart.Scale)),
	)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if numerator.Sign() < 0 && remainder.Sign() != 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	return quotient, nil
}

func audioDecodeCandidateLess(left, right audioDecodeCandidate) bool {
	if left.request.FirstOutputSample != right.request.FirstOutputSample {
		return left.request.FirstOutputSample < right.request.FirstOutputSample
	}
	if left.layer != right.layer {
		return left.layer < right.layer
	}
	if left.clipID != right.clipID {
		return left.clipID < right.clipID
	}
	return left.request.InstructionIndex < right.request.InstructionIndex
}

func selectAudioDecodeLane(lanes []AudioDecodeLane, ends []uint64, candidate audioDecodeCandidate) int {
	preferred, fallback := -1, -1
	for index := range lanes {
		if ends[index] > candidate.request.FirstOutputSample {
			continue
		}
		if fallback < 0 || ends[index] > ends[fallback] {
			fallback = index
		}
		if len(lanes[index].Runs) == 0 {
			continue
		}
		run := lanes[index].Runs[len(lanes[index].Runs)-1]
		if run.InputArtifactID != candidate.artifactID || run.SourceStreamID != candidate.streamID ||
			run.LastOrdinal > candidate.request.FirstOrdinal {
			continue
		}
		if preferred < 0 || run.LastOrdinal > lanes[preferred].Runs[len(lanes[preferred].Runs)-1].LastOrdinal {
			preferred = index
		}
	}
	if preferred >= 0 {
		return preferred
	}
	if fallback >= 0 {
		return fallback
	}
	return len(lanes)
}

func appendAudioDecodeCandidate(lane *AudioDecodeLane, candidate audioDecodeCandidate) {
	newRun := len(lane.Runs) == 0
	if !newRun {
		last := lane.Runs[len(lane.Runs)-1]
		newRun = last.InputArtifactID != candidate.artifactID || last.SourceStreamID != candidate.streamID ||
			last.LastOrdinal > candidate.request.FirstOrdinal
	}
	if newRun {
		lane.Runs = append(lane.Runs, AudioDecodeRun{
			InputIndex: candidate.inputIndex, InputArtifactID: candidate.artifactID,
			SourceStreamID: candidate.streamID, Requests: []AudioDecodeRequest{},
		})
	}
	run := &lane.Runs[len(lane.Runs)-1]
	run.Requests = append(run.Requests, candidate.request)
	run.LastOrdinal = candidate.request.LastOrdinal
}

func validateAudioDecodeRun(plan domain.RenderPlanPayload, run AudioDecodeRun) error {
	if int(run.InputIndex) >= len(plan.Inputs) || len(run.Requests) == 0 ||
		run.TraversalSamples != run.LastOrdinal+1 || run.LastOrdinal >= MaximumDecodedAudioSamples {
		return fmt.Errorf("audio decode run head is invalid")
	}
	input := plan.Inputs[run.InputIndex]
	if input.ArtifactID != run.InputArtifactID || input.Audio == nil || input.Audio.SourceStreamID != run.SourceStreamID {
		return fmt.Errorf("audio decode run source is invalid")
	}
	var previousEnd, previousLast uint64
	for index, request := range run.Requests {
		if int(request.InstructionIndex) >= len(plan.Audio) || request.FirstOutputSample >= request.EndOutputSample ||
			request.EndOutputSample > plan.Output.AudioSampleCount.Value() || request.FirstOrdinal > request.LastOrdinal ||
			request.LastOrdinal > run.LastOrdinal ||
			(index > 0 && (previousEnd > request.FirstOutputSample || previousLast > request.FirstOrdinal)) {
			return fmt.Errorf("audio decode request is invalid")
		}
		instruction := plan.Audio[request.InstructionIndex]
		if instruction.InputArtifactID != run.InputArtifactID || instruction.SourceStreamID != run.SourceStreamID {
			return fmt.Errorf("audio decode request source is invalid")
		}
		expected, present, err := audioDecodeRequest(
			request.InstructionIndex, instruction, *input.Audio, plan.Output.AudioSampleCount.Value(),
		)
		if err != nil || !present || expected != request {
			return fmt.Errorf("audio decode request grid is invalid")
		}
		previousEnd, previousLast = request.EndOutputSample, request.LastOrdinal
	}
	if previousLast != run.LastOrdinal {
		return fmt.Errorf("audio decode run tail is invalid")
	}
	return nil
}
