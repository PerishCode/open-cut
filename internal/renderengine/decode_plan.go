package renderengine

import (
	"fmt"
	"math"
	"math/big"
	"path/filepath"
	"sort"

	"github.com/PerishCode/open-cut/product/domain"
)

const VideoDecodePlanPolicyV1 = "monotonic-lanes-ordinal-zero-v1"

type VideoDecodePlan struct {
	Policy          string
	Lanes           []VideoDecodeLane
	TraversalFrames uint64
}

type VideoDecodeLane struct {
	Index uint16
	Runs  []VideoDecodeRun
}

type VideoDecodeRun struct {
	InputIndex      uint32
	InputArtifactID domain.ArtifactID
	SourceStreamID  domain.SourceStreamID
	Requests        []VideoDecodeRequest
	LastOrdinal     uint64
	TraversalFrames uint64
}

type VideoDecodeRequest struct {
	InstructionIndex uint32
	FirstOutputFrame uint64
	EndOutputFrame   uint64
	FirstOrdinal     uint64
	LastOrdinal      uint64
}

type videoDecodeSource struct {
	inputIndex uint32
	input      domain.RenderVideoInput
	mapFile    *SourceMap
}

type videoDecodeCandidate struct {
	request    VideoDecodeRequest
	inputIndex uint32
	artifactID domain.ArtifactID
	streamID   domain.SourceStreamID
	layer      uint16
	clipID     string
}

// CompileVideoDecodePlan derives process slots and monotonic decoder runs from
// the exact execution materials. Each run starts at presentation ordinal zero;
// binary source-map lookup never reduces the charged decode traversal.
func CompileVideoDecodePlan(manifest ExecutionManifest) (VideoDecodePlan, error) {
	if manifest.Validate() != nil {
		return VideoDecodePlan{}, fmt.Errorf("video decode manifest is invalid")
	}
	sources := make(map[string]videoDecodeSource, len(manifest.Plan.Inputs))
	opened := make([]*SourceMap, 0, len(manifest.Plan.Inputs))
	defer func() {
		for _, source := range opened {
			_ = source.Close()
		}
	}()
	for index, input := range manifest.Plan.Inputs {
		if input.Video == nil {
			continue
		}
		mapPath := filepath.Join(manifest.Inputs[index].ArtifactRoot, "video-time-map.bin")
		mapFile, err := OpenSourceMap(mapPath, input.Video.TimeMapDigest)
		if err != nil {
			return VideoDecodePlan{}, fmt.Errorf("open video source map %s: %w", input.ArtifactID, err)
		}
		opened = append(opened, mapFile)
		sources[input.ArtifactID.String()] = videoDecodeSource{
			inputIndex: uint32(index), input: *input.Video, mapFile: mapFile,
		}
	}
	return planVideoDecodeLanes(
		manifest.Plan, sources, manifest.Budget.DecodedVideoFrameLimit,
	)
}

func planVideoDecodeLanes(
	plan domain.RenderPlanPayload,
	sources map[string]videoDecodeSource,
	frameLimit uint64,
) (VideoDecodePlan, error) {
	if plan.Output.FrameRate.Validate() != nil || !plan.Output.FrameRate.IsPositive() ||
		plan.Output.VideoFrameCount.Value() == 0 || frameLimit == 0 || frameLimit > MaximumDecodedVideoFrames {
		return VideoDecodePlan{}, fmt.Errorf("video decode plan input is invalid")
	}
	candidates := make([]videoDecodeCandidate, 0, len(plan.Video))
	for index, instruction := range plan.Video {
		source, exists := sources[instruction.InputArtifactID.String()]
		if !exists || source.mapFile == nil || source.input.SourceStreamID != instruction.SourceStreamID ||
			index > math.MaxUint32 {
			return VideoDecodePlan{}, fmt.Errorf("video decode source is invalid")
		}
		firstFrame, endFrame, err := outputFrameRange(
			instruction.TimelineRange, plan.Output.FrameRate, plan.Output.VideoFrameCount.Value(),
		)
		if err != nil {
			return VideoDecodePlan{}, err
		}
		if firstFrame == endFrame {
			continue
		}
		cursor, err := source.mapFile.NewCursor()
		if err != nil {
			return VideoDecodePlan{}, err
		}
		firstTime, active, err := VideoInstructionSourceTime(instruction, firstFrame, plan.Output.FrameRate)
		if err != nil || !active {
			return VideoDecodePlan{}, fmt.Errorf("video decode first sample is invalid")
		}
		first, err := cursor.SelectFloor(firstTime, source.input.SourceTimeBase)
		if err != nil {
			return VideoDecodePlan{}, err
		}
		lastTime, active, err := VideoInstructionSourceTime(instruction, endFrame-1, plan.Output.FrameRate)
		if err != nil || !active {
			return VideoDecodePlan{}, fmt.Errorf("video decode last sample is invalid")
		}
		last, err := cursor.SelectFloor(lastTime, source.input.SourceTimeBase)
		if err != nil || last.Ordinal < first.Ordinal {
			return VideoDecodePlan{}, fmt.Errorf("video decode ordinal range is invalid")
		}
		candidates = append(candidates, videoDecodeCandidate{
			request: VideoDecodeRequest{
				InstructionIndex: uint32(index), FirstOutputFrame: firstFrame, EndOutputFrame: endFrame,
				FirstOrdinal: first.Ordinal, LastOrdinal: last.Ordinal,
			},
			inputIndex: source.inputIndex, artifactID: instruction.InputArtifactID,
			streamID: instruction.SourceStreamID, layer: instruction.Layer, clipID: instruction.ClipID.String(),
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		return videoDecodeCandidateLess(candidates[left], candidates[right])
	})
	result := VideoDecodePlan{Policy: VideoDecodePlanPolicyV1, Lanes: []VideoDecodeLane{}}
	laneEnds := make([]uint64, 0)
	for _, candidate := range candidates {
		lane := selectVideoDecodeLane(result.Lanes, laneEnds, candidate)
		if lane == len(result.Lanes) {
			if lane >= MaximumActiveVideoLayers {
				return VideoDecodePlan{}, ResourceLimitError{Subject: "video-decode-lanes"}
			}
			result.Lanes = append(result.Lanes, VideoDecodeLane{Index: uint16(lane), Runs: []VideoDecodeRun{}})
			laneEnds = append(laneEnds, 0)
		}
		appendVideoDecodeCandidate(&result.Lanes[lane], candidate)
		laneEnds[lane] = candidate.request.EndOutputFrame
	}
	tracker, err := NewDecodeTraversalTracker(frameLimit)
	if err != nil {
		return VideoDecodePlan{}, err
	}
	for laneIndex := range result.Lanes {
		for runIndex := range result.Lanes[laneIndex].Runs {
			run := &result.Lanes[laneIndex].Runs[runIndex]
			if err := tracker.BeginRun(); err != nil {
				return VideoDecodePlan{}, err
			}
			for _, request := range run.Requests {
				if err := tracker.Observe(request.FirstOrdinal); err != nil {
					return VideoDecodePlan{}, err
				}
				if err := tracker.Observe(request.LastOrdinal); err != nil {
					return VideoDecodePlan{}, err
				}
			}
			run.TraversalFrames = run.LastOrdinal + 1
		}
	}
	result.TraversalFrames = tracker.Traversed()
	return result, nil
}

func outputFrameRange(
	value domain.TimeRange,
	frameRate domain.RationalTime,
	outputCount uint64,
) (uint64, uint64, error) {
	if value.Start.Validate() != nil || value.Start.IsNegative() || value.Duration.Validate() != nil ||
		!value.Duration.IsPositive() || frameRate.Validate() != nil || !frameRate.IsPositive() {
		return 0, 0, fmt.Errorf("video decode timeline range is invalid")
	}
	end, err := value.End()
	if err != nil {
		return 0, 0, err
	}
	first, err := ceilFrameIndex(value.Start, frameRate)
	if err != nil {
		return 0, 0, err
	}
	after, err := ceilFrameIndex(end, frameRate)
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
		return 0, 0, fmt.Errorf("video decode output range is invalid")
	}
	return first, after, nil
}

func ceilFrameIndex(value, frameRate domain.RationalTime) (uint64, error) {
	if value.Validate() != nil || value.IsNegative() || frameRate.Validate() != nil || !frameRate.IsPositive() {
		return 0, fmt.Errorf("video decode frame boundary is invalid")
	}
	numerator := new(big.Int).Mul(big.NewInt(value.Value.Value()), big.NewInt(frameRate.Value.Value()))
	denominator := new(big.Int).Mul(big.NewInt(int64(value.Scale)), big.NewInt(int64(frameRate.Scale)))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return 0, ResourceLimitError{Subject: "output-frame-time"}
	}
	return quotient.Uint64(), nil
}

func videoDecodeCandidateLess(left, right videoDecodeCandidate) bool {
	if left.request.FirstOutputFrame != right.request.FirstOutputFrame {
		return left.request.FirstOutputFrame < right.request.FirstOutputFrame
	}
	if left.layer != right.layer {
		return left.layer < right.layer
	}
	if left.clipID != right.clipID {
		return left.clipID < right.clipID
	}
	return left.request.InstructionIndex < right.request.InstructionIndex
}

func selectVideoDecodeLane(
	lanes []VideoDecodeLane,
	ends []uint64,
	candidate videoDecodeCandidate,
) int {
	preferred, fallback := -1, -1
	for index := range lanes {
		if ends[index] > candidate.request.FirstOutputFrame {
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
		if preferred < 0 {
			preferred = index
			continue
		}
		selected := lanes[preferred].Runs[len(lanes[preferred].Runs)-1]
		if run.LastOrdinal > selected.LastOrdinal {
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

func appendVideoDecodeCandidate(lane *VideoDecodeLane, candidate videoDecodeCandidate) {
	newRun := len(lane.Runs) == 0
	if !newRun {
		last := lane.Runs[len(lane.Runs)-1]
		newRun = last.InputArtifactID != candidate.artifactID || last.SourceStreamID != candidate.streamID ||
			last.LastOrdinal > candidate.request.FirstOrdinal
	}
	if newRun {
		lane.Runs = append(lane.Runs, VideoDecodeRun{
			InputIndex: candidate.inputIndex, InputArtifactID: candidate.artifactID,
			SourceStreamID: candidate.streamID, Requests: []VideoDecodeRequest{},
		})
	}
	run := &lane.Runs[len(lane.Runs)-1]
	run.Requests = append(run.Requests, candidate.request)
	run.LastOrdinal = candidate.request.LastOrdinal
}
