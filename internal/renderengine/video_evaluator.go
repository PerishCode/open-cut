package renderengine

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/PerishCode/open-cut/lifecycle"
)

type videoRunDecoder interface {
	ReadTo(uint64) ([]byte, error)
	Finish() error
	Close() error
}

type videoRunDecoderFactory func(
	context.Context,
	ExecutionManifest,
	VideoDecodeRun,
	string,
	lifecycle.Profile,
) (videoRunDecoder, error)

type videoEvaluatorBinding struct {
	instructionIndex uint32
	laneIndex        int
	runIndex         int
	requestIndex     int
	request          VideoDecodeRequest
}

type videoLaneEvaluator struct {
	lane         VideoDecodeLane
	nextRun      int
	decoder      videoRunDecoder
	cursor       *SourceMapCursor
	lastSelected uint64
	hasSelected  bool
	manifest     ExecutionManifest
	attemptRoot  string
	profile      lifecycle.Profile
	factory      videoRunDecoderFactory
	sources      map[uint32]*SourceMap
}

func NewVideoStreamProducer(
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
) (StreamProducer, error) {
	return newVideoStreamProducer(manifest, attemptRoot, profile, startVideoRunDecoder)
}

func NewCaptionedVideoStreamProducer(
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
	captions *CaptionCoverageEvaluator,
) (StreamProducer, error) {
	return newCaptionedVideoStreamProducer(
		manifest, attemptRoot, profile, startVideoRunDecoder, captions,
	)
}

func newVideoStreamProducer(
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
	factory videoRunDecoderFactory,
) (StreamProducer, error) {
	if manifest.Validate() != nil || !cleanAbsoluteDirectory(attemptRoot) || factory == nil ||
		manifest.Budget.PeakVideoLayers > MaximumActiveVideoLayers || manifest.Budget.VideoChunkFrames != 1 {
		return nil, fmt.Errorf("video evaluator configuration is invalid")
	}
	decodePlan, err := CompileVideoDecodePlan(manifest)
	if err != nil {
		return nil, err
	}
	bindings, err := compileVideoEvaluatorBindings(manifest, decodePlan)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, destination io.Writer) error {
		return evaluateVideoStream(
			ctx, destination, manifest, attemptRoot, profile, decodePlan, bindings, factory, nil,
		)
	}, nil
}

func newCaptionedVideoStreamProducer(
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
	factory videoRunDecoderFactory,
	captions *CaptionCoverageEvaluator,
) (StreamProducer, error) {
	if captions == nil || captions.finished || captions.nextFrame != 0 ||
		!reflect.DeepEqual(captions.plan, manifest.Plan) || captions.budget != manifest.Budget {
		return nil, fmt.Errorf("captioned video evaluator configuration is invalid")
	}
	decodePlan, err := CompileVideoDecodePlan(manifest)
	if manifest.Validate() != nil || !cleanAbsoluteDirectory(attemptRoot) || factory == nil || err != nil {
		return nil, fmt.Errorf("captioned video evaluator configuration is invalid")
	}
	bindings, err := compileVideoEvaluatorBindings(manifest, decodePlan)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, destination io.Writer) error {
		return evaluateVideoStream(
			ctx, destination, manifest, attemptRoot, profile, decodePlan, bindings, factory, captions,
		)
	}, nil
}

func compileVideoEvaluatorBindings(
	manifest ExecutionManifest,
	decodePlan VideoDecodePlan,
) ([]videoEvaluatorBinding, error) {
	bindings := make([]videoEvaluatorBinding, 0, len(manifest.Plan.Video))
	seen := make(map[uint32]struct{}, len(manifest.Plan.Video))
	for laneIndex, lane := range decodePlan.Lanes {
		if int(lane.Index) != laneIndex || laneIndex >= MaximumActiveVideoLayers {
			return nil, fmt.Errorf("video evaluator lane is invalid")
		}
		for runIndex, run := range lane.Runs {
			if err := validateVideoDecodeRun(manifest.Plan, run); err != nil {
				return nil, err
			}
			for requestIndex, request := range run.Requests {
				if _, exists := seen[request.InstructionIndex]; exists {
					return nil, fmt.Errorf("video evaluator instruction is duplicated")
				}
				seen[request.InstructionIndex] = struct{}{}
				bindings = append(bindings, videoEvaluatorBinding{
					instructionIndex: request.InstructionIndex, laneIndex: laneIndex,
					runIndex: runIndex, requestIndex: requestIndex, request: request,
				})
			}
		}
	}
	for index, instruction := range manifest.Plan.Video {
		first, after, err := outputFrameRange(
			instruction.TimelineRange, manifest.Plan.Output.FrameRate,
			manifest.Plan.Output.VideoFrameCount.Value(),
		)
		_, bound := seen[uint32(index)]
		if err != nil || (first != after) != bound {
			return nil, fmt.Errorf("video evaluator binding is incomplete")
		}
	}
	sort.Slice(bindings, func(left, right int) bool {
		if bindings[left].request.FirstOutputFrame != bindings[right].request.FirstOutputFrame {
			return bindings[left].request.FirstOutputFrame < bindings[right].request.FirstOutputFrame
		}
		return bindings[left].instructionIndex < bindings[right].instructionIndex
	})
	return bindings, nil
}

func evaluateVideoStream(
	ctx context.Context,
	destination io.Writer,
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
	decodePlan VideoDecodePlan,
	bindings []videoEvaluatorBinding,
	factory videoRunDecoderFactory,
	captions *CaptionCoverageEvaluator,
) (resultErr error) {
	if ctx == nil || destination == nil {
		return fmt.Errorf("video evaluator destination is invalid")
	}
	sources, err := openVideoEvaluatorSources(manifest)
	if err != nil {
		return err
	}
	lanes := make([]videoLaneEvaluator, len(decodePlan.Lanes))
	for index := range decodePlan.Lanes {
		lanes[index] = videoLaneEvaluator{
			lane: decodePlan.Lanes[index], manifest: manifest, attemptRoot: attemptRoot,
			profile: profile, factory: factory, sources: sources,
		}
	}
	defer func() {
		if closeErr := closeVideoEvaluation(lanes, sources); resultErr == nil {
			resultErr = closeErr
		}
	}()
	compositor, err := newVideoCompositor(manifest.Plan)
	if err != nil {
		return err
	}
	active := make([]int, 0, MaximumActiveVideoLayers)
	nextBinding := 0
	layers := make([]DecodedVideoLayer, 0, MaximumActiveVideoLayers)
	outputCount := manifest.Plan.Output.VideoFrameCount.Value()
	for outputFrame := uint64(0); outputFrame < outputCount; outputFrame++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		active = advanceVideoEvaluatorBindings(active, bindings, &nextBinding, outputFrame)
		if len(active) > MaximumActiveVideoLayers {
			return ResourceLimitError{Subject: "active-video-layers"}
		}
		layers = layers[:0]
		for _, bindingIndex := range active {
			binding := bindings[bindingIndex]
			frame, err := lanes[binding.laneIndex].read(ctx, binding, outputFrame)
			if err != nil {
				return err
			}
			layers = append(layers, DecodedVideoLayer{
				InstructionIndex: binding.instructionIndex, Frame: frame,
			})
		}
		var frame []byte
		if captions == nil {
			frame, err = compositor.CompositeFrame(outputFrame, layers)
		} else {
			captionLayers, captionErr := captions.LayersForFrame(outputFrame)
			if captionErr != nil {
				return captionErr
			}
			frame, err = compositor.CompositeFrameWithCaptions(outputFrame, layers, captionLayers)
		}
		if err != nil {
			return err
		}
		written, err := destination.Write(frame)
		if err != nil {
			return err
		}
		if written != len(frame) {
			return io.ErrShortWrite
		}
	}
	if err := compositor.Finish(); err != nil {
		return err
	}
	if captions != nil {
		if err := captions.Finish(); err != nil {
			return err
		}
	}
	if nextBinding != len(bindings) ||
		len(advanceVideoEvaluatorBindings(active, bindings, &nextBinding, outputCount)) != 0 {
		return fmt.Errorf("video evaluator schedule is incomplete")
	}
	for index := range lanes {
		if lanes[index].decoder != nil || lanes[index].nextRun != len(lanes[index].lane.Runs) {
			return fmt.Errorf("video evaluator traversal is incomplete")
		}
	}
	return nil
}

func advanceVideoEvaluatorBindings(
	active []int,
	bindings []videoEvaluatorBinding,
	next *int,
	frame uint64,
) []int {
	kept := active[:0]
	changed := false
	for _, index := range active {
		if bindings[index].request.EndOutputFrame > frame {
			kept = append(kept, index)
		} else {
			changed = true
		}
	}
	active = kept
	for *next < len(bindings) && bindings[*next].request.FirstOutputFrame == frame {
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

func (lane *videoLaneEvaluator) read(
	ctx context.Context,
	binding videoEvaluatorBinding,
	outputFrame uint64,
) ([]byte, error) {
	if lane == nil || binding.runIndex != lane.nextRun || binding.runIndex >= len(lane.lane.Runs) ||
		binding.request.FirstOutputFrame > outputFrame || binding.request.EndOutputFrame <= outputFrame {
		return nil, fmt.Errorf("video evaluator lane request is invalid")
	}
	run := lane.lane.Runs[binding.runIndex]
	if binding.requestIndex >= len(run.Requests) || run.Requests[binding.requestIndex] != binding.request {
		return nil, fmt.Errorf("video evaluator run request is invalid")
	}
	if lane.decoder == nil {
		source := lane.sources[run.InputIndex]
		if source == nil {
			return nil, fmt.Errorf("video evaluator source map is unavailable")
		}
		cursor, err := source.NewCursor()
		if err != nil {
			return nil, err
		}
		decoder, err := lane.factory(ctx, lane.manifest, run, lane.attemptRoot, lane.profile)
		if err != nil {
			return nil, fmt.Errorf("start video decode run: %w", err)
		}
		lane.cursor, lane.decoder = cursor, decoder
		lane.hasSelected = false
	}
	instruction := lane.manifest.Plan.Video[binding.instructionIndex]
	input := lane.manifest.Plan.Inputs[run.InputIndex].Video
	if input == nil {
		return nil, fmt.Errorf("video evaluator source input is unavailable")
	}
	target, active, err := VideoInstructionSourceTime(
		instruction, outputFrame, lane.manifest.Plan.Output.FrameRate,
	)
	if err != nil || !active {
		return nil, fmt.Errorf("video evaluator source time is invalid")
	}
	selection, err := lane.cursor.SelectFloor(target, input.SourceTimeBase)
	if err != nil || selection.Ordinal < binding.request.FirstOrdinal || selection.Ordinal > binding.request.LastOrdinal ||
		lane.hasSelected && selection.Ordinal < lane.lastSelected ||
		outputFrame == binding.request.FirstOutputFrame && selection.Ordinal != binding.request.FirstOrdinal ||
		outputFrame+1 == binding.request.EndOutputFrame && selection.Ordinal != binding.request.LastOrdinal {
		return nil, fmt.Errorf("video evaluator source selection is invalid")
	}
	frame, err := lane.decoder.ReadTo(selection.Ordinal)
	if err != nil {
		return nil, fmt.Errorf("evaluate video frame: %w", err)
	}
	lane.lastSelected, lane.hasSelected = selection.Ordinal, true
	lastRequest := binding.requestIndex+1 == len(run.Requests)
	if lastRequest && outputFrame+1 == binding.request.EndOutputFrame {
		if selection.Ordinal != run.LastOrdinal {
			return nil, fmt.Errorf("video evaluator run tail is invalid")
		}
		if err := lane.decoder.Finish(); err != nil {
			return nil, fmt.Errorf("finish video decode run: %w", err)
		}
		lane.decoder, lane.cursor = nil, nil
		lane.nextRun++
	}
	return frame, nil
}

func openVideoEvaluatorSources(manifest ExecutionManifest) (map[uint32]*SourceMap, error) {
	result := make(map[uint32]*SourceMap, len(manifest.Plan.Inputs))
	for index, input := range manifest.Plan.Inputs {
		if input.Video == nil {
			continue
		}
		source, err := OpenSourceMap(
			filepath.Join(manifest.Inputs[index].ArtifactRoot, "video-time-map.bin"),
			input.Video.TimeMapDigest,
		)
		if err != nil {
			for _, opened := range result {
				_ = opened.Close()
			}
			return nil, err
		}
		result[uint32(index)] = source
	}
	return result, nil
}

func closeVideoEvaluation(lanes []videoLaneEvaluator, sources map[uint32]*SourceMap) error {
	var first error
	for index := range lanes {
		if lanes[index].decoder != nil {
			if err := lanes[index].decoder.Close(); err != nil && first == nil {
				first = err
			}
			lanes[index].decoder = nil
		}
	}
	for _, source := range sources {
		if err := source.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func startVideoRunDecoder(
	ctx context.Context,
	manifest ExecutionManifest,
	run VideoDecodeRun,
	attemptRoot string,
	profile lifecycle.Profile,
) (videoRunDecoder, error) {
	return StartVideoDecodeRun(ctx, manifest, run, attemptRoot, profile)
}
