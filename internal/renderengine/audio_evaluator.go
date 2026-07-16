package renderengine

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/domain"
)

type audioRunDecoder interface {
	ReadTo(uint64) (StereoPCM16, error)
	Finish() error
	Close() error
}

type audioRunDecoderFactory func(
	context.Context,
	ExecutionManifest,
	AudioDecodeRun,
	string,
	lifecycle.Profile,
) (audioRunDecoder, error)

type audioMixBinding struct {
	instructionIndex uint32
	laneIndex        int
	runIndex         int
	request          AudioDecodeRequest
	gainQ31          int64
}

type audioLaneEvaluator struct {
	lane        AudioDecodeLane
	nextRun     int
	decoder     audioRunDecoder
	manifest    ExecutionManifest
	attemptRoot string
	profile     lifecycle.Profile
	factory     audioRunDecoderFactory
}

// NewAudioStreamProducer compiles one exact audio decode/mix schedule. The
// returned producer writes only interleaved stereo S16LE and retains at most
// one 4,800-sample output block plus one bounded decoder chunk per live lane.
func NewAudioStreamProducer(
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
) (StreamProducer, error) {
	return newAudioStreamProducer(manifest, attemptRoot, profile, startAudioRunDecoder)
}

func newAudioStreamProducer(
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
	factory audioRunDecoderFactory,
) (StreamProducer, error) {
	if manifest.Validate() != nil || !cleanAbsoluteDirectory(attemptRoot) || factory == nil ||
		manifest.Budget.PeakAudioLayers > MaximumActiveAudioLayers ||
		manifest.Budget.AudioChunkSamples != AudioChunkSamples {
		return nil, fmt.Errorf("audio evaluator configuration is invalid")
	}
	decodePlan, err := CompileAudioDecodePlan(manifest)
	if err != nil {
		return nil, err
	}
	bindings, err := compileAudioMixBindings(manifest, decodePlan)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, destination io.Writer) error {
		return evaluateAudioStream(ctx, destination, manifest, attemptRoot, profile, decodePlan, bindings, factory)
	}, nil
}

func compileAudioMixBindings(
	manifest ExecutionManifest,
	decodePlan AudioDecodePlan,
) ([]audioMixBinding, error) {
	bindings := make([]audioMixBinding, 0, len(manifest.Plan.Audio))
	seen := make(map[uint32]struct{}, len(manifest.Plan.Audio))
	inputs := make(map[string]domain.RenderAudioInput, len(manifest.Plan.Inputs))
	for _, input := range manifest.Plan.Inputs {
		if input.Audio != nil {
			inputs[input.ArtifactID.String()] = *input.Audio
		}
	}
	for laneIndex, lane := range decodePlan.Lanes {
		if int(lane.Index) != laneIndex || laneIndex >= MaximumActiveAudioLayers {
			return nil, fmt.Errorf("audio evaluator lane is invalid")
		}
		for runIndex, run := range lane.Runs {
			if err := validateAudioDecodeRun(manifest.Plan, run); err != nil {
				return nil, err
			}
			for _, request := range run.Requests {
				if _, exists := seen[request.InstructionIndex]; exists {
					return nil, fmt.Errorf("audio evaluator instruction is duplicated")
				}
				instruction := manifest.Plan.Audio[request.InstructionIndex]
				gain, err := GainCoefficientQ31(instruction.GainMilliDB)
				if err != nil {
					return nil, err
				}
				seen[request.InstructionIndex] = struct{}{}
				bindings = append(bindings, audioMixBinding{
					instructionIndex: request.InstructionIndex,
					laneIndex:        laneIndex, runIndex: runIndex, request: request, gainQ31: gain,
				})
			}
		}
	}
	for index, instruction := range manifest.Plan.Audio {
		input, exists := inputs[instruction.InputArtifactID.String()]
		if !exists {
			return nil, fmt.Errorf("audio evaluator source is invalid")
		}
		_, present, err := audioDecodeRequest(
			uint32(index), instruction, input, manifest.Plan.Output.AudioSampleCount.Value(),
		)
		_, bound := seen[uint32(index)]
		if err != nil || present != bound {
			return nil, fmt.Errorf("audio evaluator binding is incomplete")
		}
	}
	sort.Slice(bindings, func(left, right int) bool {
		if bindings[left].request.FirstOutputSample != bindings[right].request.FirstOutputSample {
			return bindings[left].request.FirstOutputSample < bindings[right].request.FirstOutputSample
		}
		return bindings[left].instructionIndex < bindings[right].instructionIndex
	})
	return bindings, nil
}

func evaluateAudioStream(
	ctx context.Context,
	destination io.Writer,
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
	decodePlan AudioDecodePlan,
	bindings []audioMixBinding,
	factory audioRunDecoderFactory,
) (resultErr error) {
	if ctx == nil || destination == nil {
		return fmt.Errorf("audio evaluator destination is invalid")
	}
	lanes := make([]audioLaneEvaluator, len(decodePlan.Lanes))
	for index := range decodePlan.Lanes {
		lanes[index] = audioLaneEvaluator{
			lane: decodePlan.Lanes[index], manifest: manifest, attemptRoot: attemptRoot,
			profile: profile, factory: factory,
		}
	}
	defer func() {
		if closeErr := closeAudioLanes(lanes); resultErr == nil {
			resultErr = closeErr
		}
	}()

	active := make([]int, 0, MaximumActiveAudioLayers)
	nextBinding := 0
	chunk := make([]byte, int(manifest.Budget.AudioChunkSamples)*rawPCMFrameBytes)
	var leftSamples, rightSamples [MaximumActiveAudioLayers]int16
	var gains [MaximumActiveAudioLayers]int64
	outputCount := manifest.Plan.Output.AudioSampleCount.Value()
	for chunkStart := uint64(0); chunkStart < outputCount; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		chunkSamples := uint64(manifest.Budget.AudioChunkSamples)
		if remaining := outputCount - chunkStart; remaining < chunkSamples {
			chunkSamples = remaining
		}
		for offset := uint64(0); offset < chunkSamples; offset++ {
			ordinal := chunkStart + offset
			active = advanceAudioBindings(active, bindings, &nextBinding, ordinal)
			if len(active) > MaximumActiveAudioLayers {
				return ResourceLimitError{Subject: "active-audio-layers"}
			}
			for activeIndex, bindingIndex := range active {
				binding := bindings[bindingIndex]
				sample, err := lanes[binding.laneIndex].read(ctx, binding, ordinal)
				if err != nil {
					return err
				}
				leftSamples[activeIndex] = sample.Left
				rightSamples[activeIndex] = sample.Right
				gains[activeIndex] = binding.gainQ31
			}
			left, err := MixPCM16(leftSamples[:len(active)], gains[:len(active)])
			if err != nil {
				return err
			}
			right, err := MixPCM16(rightSamples[:len(active)], gains[:len(active)])
			if err != nil {
				return err
			}
			byteOffset := offset * rawPCMFrameBytes
			binary.LittleEndian.PutUint16(chunk[byteOffset:byteOffset+2], uint16(left))
			binary.LittleEndian.PutUint16(chunk[byteOffset+2:byteOffset+4], uint16(right))
		}
		bytes := chunkSamples * rawPCMFrameBytes
		written, err := destination.Write(chunk[:bytes])
		if err != nil {
			return err
		}
		if uint64(written) != bytes {
			return io.ErrShortWrite
		}
		chunkStart += chunkSamples
	}
	if nextBinding != len(bindings) || len(advanceAudioBindings(active, bindings, &nextBinding, outputCount)) != 0 {
		return fmt.Errorf("audio evaluator schedule is incomplete")
	}
	for index := range lanes {
		if lanes[index].decoder != nil || lanes[index].nextRun != len(lanes[index].lane.Runs) {
			return fmt.Errorf("audio evaluator traversal is incomplete")
		}
	}
	return nil
}

func advanceAudioBindings(active []int, bindings []audioMixBinding, next *int, ordinal uint64) []int {
	kept := active[:0]
	changed := false
	for _, index := range active {
		if bindings[index].request.EndOutputSample > ordinal {
			kept = append(kept, index)
		} else {
			changed = true
		}
	}
	active = kept
	for *next < len(bindings) && bindings[*next].request.FirstOutputSample == ordinal {
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

func (lane *audioLaneEvaluator) read(
	ctx context.Context,
	binding audioMixBinding,
	outputOrdinal uint64,
) (StereoPCM16, error) {
	if lane == nil || binding.runIndex != lane.nextRun || binding.request.FirstOutputSample > outputOrdinal ||
		binding.request.EndOutputSample <= outputOrdinal || binding.runIndex >= len(lane.lane.Runs) {
		return StereoPCM16{}, fmt.Errorf("audio evaluator lane request is invalid")
	}
	run := lane.lane.Runs[binding.runIndex]
	if lane.decoder == nil {
		decoder, err := lane.factory(ctx, lane.manifest, run, lane.attemptRoot, lane.profile)
		if err != nil {
			return StereoPCM16{}, fmt.Errorf("start audio decode run: %w", err)
		}
		lane.decoder = decoder
	}
	ordinal := binding.request.FirstOrdinal + outputOrdinal - binding.request.FirstOutputSample
	sample, err := lane.decoder.ReadTo(ordinal)
	if err != nil {
		return StereoPCM16{}, fmt.Errorf("evaluate audio sample: %w", err)
	}
	if ordinal == run.LastOrdinal {
		if err := lane.decoder.Finish(); err != nil {
			return StereoPCM16{}, fmt.Errorf("finish audio decode run: %w", err)
		}
		lane.decoder = nil
		lane.nextRun++
	}
	return sample, nil
}

func closeAudioLanes(lanes []audioLaneEvaluator) error {
	var first error
	for index := range lanes {
		if lanes[index].decoder == nil {
			continue
		}
		if err := lanes[index].decoder.Close(); err != nil && first == nil {
			first = err
		}
		lanes[index].decoder = nil
	}
	return first
}

func startAudioRunDecoder(
	ctx context.Context,
	manifest ExecutionManifest,
	run AudioDecodeRun,
	attemptRoot string,
	profile lifecycle.Profile,
) (audioRunDecoder, error) {
	return StartAudioDecodeRun(ctx, manifest, run, attemptRoot, profile)
}
