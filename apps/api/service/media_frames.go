package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type ExternalMediaFrameExecutor struct {
	access        *SourceAccess
	probe         string
	decoder       string
	version       string
	tempRoot      string
	profile       lifecycle.Profile
	wallTime      time.Duration
	maximumFrames uint64
}

func NewExternalMediaFrameExecutor(
	access *SourceAccess,
	probe string,
	decoder string,
	version string,
	tempRoot string,
	profile lifecycle.Profile,
) (*ExternalMediaFrameExecutor, error) {
	if access == nil || !cleanAbsolute(probe) || !cleanAbsolute(decoder) ||
		version == "" || len(version) > 256 || !cleanAbsolute(tempRoot) ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("media frame executor configuration is invalid")
	}
	for _, executable := range []string{probe, decoder} {
		if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("media frame executor is unavailable")
		}
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create media attempt root: %w", err)
	}
	return &ExternalMediaFrameExecutor{
		access: access, probe: probe, decoder: decoder, version: version, tempRoot: tempRoot,
		profile: profile, wallTime: 2 * time.Minute, maximumFrames: 10_000_000,
	}, nil
}

func (executor *ExternalMediaFrameExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{Kind: domain.MediaJobFrameSet, Version: executor.version}
}

func (executor *ExternalMediaFrameExecutor) Execute(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	frameSet, err := executor.decode(ctx, claim)
	if err != nil {
		return application.MediaJobExecution{}, err
	}
	return application.MediaJobExecution{FrameSet: &frameSet}, nil
}

func (executor *ExternalMediaFrameExecutor) decode(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaFrameSetExecution, error) {
	parameters, err := application.DecodeFrameSetParameters(claim.ParametersJSON)
	if err != nil || claim.Kind != domain.MediaJobFrameSet || claim.AttemptID.IsZero() ||
		claim.AssetID.IsZero() || claim.AcceptedFingerprint == nil || claim.SourceStream == nil ||
		parameters.AssetID != claim.AssetID || parameters.Fingerprint != *claim.AcceptedFingerprint ||
		parameters.SourceStreamID != claim.SourceStream.ID ||
		claim.SourceStream.Descriptor.MediaType != domain.MediaVideo || claim.SourceStream.Descriptor.Video == nil {
		return application.MediaFrameSetExecution{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	source, err := executor.access.resolveAssetSource(ctx, claim.AssetID)
	if err != nil {
		return application.MediaFrameSetExecution{}, err
	}
	if source.Observation != claim.ExpectedObservation {
		return application.MediaFrameSetExecution{}, application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	attemptRoot := filepath.Join(executor.tempRoot, claim.AttemptID.String())
	if !pathWithin(executor.tempRoot, attemptRoot) {
		return application.MediaFrameSetExecution{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return application.MediaFrameSetExecution{}, application.NewMediaExecutionError(
			"attempt-storage-unavailable", err,
		)
	}
	defer os.RemoveAll(attemptRoot)
	executionContext, cancel := context.WithTimeout(ctx, executor.wallTime)
	defer cancel()
	selectedPTS, err := executor.inventoryPTS(
		executionContext, attemptRoot, source.Path, claim.SourceStream.Descriptor, parameters.Times,
	)
	if err != nil {
		return application.MediaFrameSetExecution{}, executor.frameFailure(
			executionContext, source, *claim.AcceptedFingerprint, err,
		)
	}
	width, height, err := normalizedFrameDimensions(claim.SourceStream.Descriptor)
	if err != nil {
		return application.MediaFrameSetExecution{}, application.NewMediaExecutionError("frame-profile-invalid", err)
	}
	decoded := make(map[int64][]byte, len(selectedPTS))
	result := application.MediaFrameSetExecution{
		SourceStreamID: parameters.SourceStreamID, Profile: parameters.Profile,
		Samples: make([]application.MediaFrameSample, 0, len(parameters.Times)),
	}
	total := 0
	for index, pts := range selectedPTS {
		pngBytes, exists := decoded[pts]
		if !exists {
			pngBytes, err = executor.decodePNG(
				executionContext, attemptRoot, source.Path, claim.SourceStream.Descriptor, pts, width, height,
			)
			if err != nil {
				return application.MediaFrameSetExecution{}, executor.frameFailure(
					executionContext, source, *claim.AcceptedFingerprint, err,
				)
			}
			decoded[pts] = pngBytes
		}
		sourceTime, err := frameTime(pts, claim.SourceStream.Descriptor.TimeBase)
		if err != nil {
			return application.MediaFrameSetExecution{}, application.NewMediaExecutionError("frame-time-invalid", err)
		}
		total += len(pngBytes)
		if total > application.MaximumFrameSetArtifactSize {
			return application.MediaFrameSetExecution{}, application.NewMediaExecutionError(
				"frame-output-limit", errors.New("frame set exceeded the output limit"),
			)
		}
		result.Samples = append(result.Samples, application.MediaFrameSample{
			RequestedTime: parameters.Times[index], SourceTime: sourceTime,
			Width: width, Height: height, PNG: append([]byte(nil), pngBytes...),
		})
	}
	if err := verifyProbeSource(source.Path, source.Observation, *claim.AcceptedFingerprint); err != nil {
		return application.MediaFrameSetExecution{}, err
	}
	return result, nil
}

func (executor *ExternalMediaFrameExecutor) frameFailure(
	ctx context.Context,
	source resolvedAssetSource,
	fingerprint domain.Digest,
	cause error,
) error {
	if sourceErr := verifyProbeSource(source.Path, source.Observation, fingerprint); sourceErr != nil {
		return sourceErr
	}
	code := "frame-decode-failed"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		code = "frame-decode-timeout"
	}
	return application.NewMediaExecutionError(code, cause)
}

func (executor *ExternalMediaFrameExecutor) inventoryPTS(
	ctx context.Context,
	directory string,
	source string,
	descriptor domain.SourceStreamDescriptor,
	requests []domain.RationalTime,
) ([]int64, error) {
	scanContext, stopScan := context.WithCancel(ctx)
	collector := newFramePTSCollector(descriptor.TimeBase, requests, executor.maximumFrames, stopScan)
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err := lifecycle.Run(scanContext, lifecycle.ProcessSpec{
		Executable: executor.probe,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "file",
			"-select_streams", strconv.FormatUint(uint64(descriptor.Index), 10),
			"-show_frames", "-show_entries", "frame=best_effort_timestamp",
			"-of", "csv=p=0", source,
		},
		Directory: directory, Env: executorEnvironment(), Stdout: collector, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	stopScan()
	if finishErr := collector.Finish(); finishErr != nil {
		return nil, finishErr
	}
	if stderr.exceeded {
		return nil, errors.New("frame timestamp diagnostics exceeded the limit")
	}
	if err != nil && !collector.StoppedAfterMaximum() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("frame timestamp inventory failed: %s", strings.TrimSpace(stderr.String()))
	}
	return collector.Selected()
}

func (executor *ExternalMediaFrameExecutor) decodePNG(
	ctx context.Context,
	directory string,
	source string,
	descriptor domain.SourceStreamDescriptor,
	pts int64,
	width uint32,
	height uint32,
) ([]byte, error) {
	expected := int(width) * int(height) * 3
	stdout := &boundedBuffer{limit: expected}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	filters := orientationFilters(descriptor.Video.Rotation)
	filters = append(filters,
		fmt.Sprintf("scale=%d:%d:flags=bilinear", width, height), "format=rgb24",
	)
	selectFilter := "select=eq(pts\\," + strconv.FormatInt(pts, 10) + ")"
	filters = append([]string{selectFilter}, filters...)
	err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: executor.decoder,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-protocol_whitelist", "file,pipe,fd",
			"-noautorotate", "-i", source, "-map", "0:" + strconv.FormatUint(uint64(descriptor.Index), 10),
			"-vf", strings.Join(filters, ","), "-frames:v", "1", "-fps_mode", "passthrough",
			"-f", "rawvideo", "pipe:1",
		},
		Directory: directory, Env: executorEnvironment(), Stdout: stdout, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded || stdout.Len() != expected {
		return nil, fmt.Errorf("exact frame decode failed: %s", strings.TrimSpace(stderr.String()))
	}
	frame := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	rgb := stdout.Bytes()
	for pixel, sourceOffset := 0, 0; sourceOffset < len(rgb); pixel, sourceOffset = pixel+1, sourceOffset+3 {
		destination := pixel * 4
		frame.Pix[destination] = rgb[sourceOffset]
		frame.Pix[destination+1] = rgb[sourceOffset+1]
		frame.Pix[destination+2] = rgb[sourceOffset+2]
		frame.Pix[destination+3] = 0xff
	}
	output := new(bytes.Buffer)
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(output, frame); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func orientationFilters(rotation int16) []string {
	switch rotation {
	case 90:
		return []string{"transpose=clock"}
	case 180:
		return []string{"transpose=clock", "transpose=clock"}
	case 270:
		return []string{"transpose=cclock"}
	default:
		return nil
	}
}

func normalizedFrameDimensions(descriptor domain.SourceStreamDescriptor) (uint32, uint32, error) {
	if descriptor.Video == nil || descriptor.Video.Width == 0 || descriptor.Video.Height == 0 {
		return 0, 0, domain.ErrInvalidMediaFacts
	}
	width := new(big.Rat).SetInt64(int64(descriptor.Video.Width))
	height := new(big.Rat).SetInt64(int64(descriptor.Video.Height))
	if descriptor.Video.PixelAspect != nil {
		aspect := new(big.Rat).SetFrac(
			big.NewInt(descriptor.Video.PixelAspect.Value.Value()),
			big.NewInt(int64(descriptor.Video.PixelAspect.Scale)),
		)
		width.Mul(width, aspect)
	}
	if descriptor.Video.Rotation == 90 || descriptor.Video.Rotation == 270 {
		width, height = height, width
	}
	long := width
	if height.Cmp(width) > 0 {
		long = height
	}
	scale := new(big.Rat).SetInt64(1)
	maximum := new(big.Rat).SetInt64(application.MaximumFrameLongEdge)
	if long.Cmp(maximum) > 0 {
		scale.Quo(maximum, long)
	}
	width.Mul(width, scale)
	height.Mul(height, scale)
	resultWidth, ok := roundedPositiveUint32(width)
	if !ok {
		return 0, 0, domain.ErrInvalidMediaFacts
	}
	resultHeight, ok := roundedPositiveUint32(height)
	if !ok {
		return 0, 0, domain.ErrInvalidMediaFacts
	}
	return resultWidth, resultHeight, nil
}

func roundedPositiveUint32(value *big.Rat) (uint32, bool) {
	numerator := new(big.Int).Mul(value.Num(), big.NewInt(2))
	numerator.Add(numerator, value.Denom())
	denominator := new(big.Int).Mul(value.Denom(), big.NewInt(2))
	rounded := new(big.Int).Quo(numerator, denominator)
	if rounded.Sign() <= 0 {
		return 1, true
	}
	if !rounded.IsUint64() || rounded.Uint64() > application.MaximumFrameLongEdge {
		return 0, false
	}
	return uint32(rounded.Uint64()), true
}

func frameTime(pts int64, timeBase domain.RationalTime) (domain.RationalTime, error) {
	numerator := new(big.Int).Mul(big.NewInt(pts), big.NewInt(timeBase.Value.Value()))
	denominator := big.NewInt(int64(timeBase.Scale))
	divisor := new(big.Int).GCD(nil, nil, new(big.Int).Abs(new(big.Int).Set(numerator)), denominator)
	numerator.Quo(numerator, divisor)
	denominator.Quo(denominator, divisor)
	if !numerator.IsInt64() || !denominator.IsInt64() || denominator.Int64() > int64(^uint32(0)>>1) {
		return domain.RationalTime{}, domain.ErrInvalidRationalTime
	}
	return domain.NewRationalTime(numerator.Int64(), int32(denominator.Int64()))
}

type framePTSCollector struct {
	timeBase      domain.RationalTime
	requests      []domain.RationalTime
	selected      []int64
	hasSelection  []bool
	first         int64
	hasFirst      bool
	previous      int64
	hasPrevious   bool
	buffer        []byte
	frames        uint64
	maximumFrames uint64
	stop          context.CancelFunc
	stopped       bool
	err           error
}

func newFramePTSCollector(
	timeBase domain.RationalTime,
	requests []domain.RationalTime,
	maximumFrames uint64,
	stop context.CancelFunc,
) *framePTSCollector {
	return &framePTSCollector{
		timeBase: timeBase, requests: append([]domain.RationalTime(nil), requests...),
		selected: make([]int64, len(requests)), hasSelection: make([]bool, len(requests)),
		maximumFrames: maximumFrames, stop: stop,
	}
}

func (collector *framePTSCollector) Write(value []byte) (int, error) {
	written := len(value)
	if collector.err != nil || collector.stopped {
		return written, nil
	}
	collector.buffer = append(collector.buffer, value...)
	if len(collector.buffer) > 4096 && !bytes.ContainsRune(collector.buffer, '\n') {
		collector.err = errors.New("frame timestamp line exceeded the limit")
		collector.stop()
		return written, nil
	}
	for {
		newline := bytes.IndexByte(collector.buffer, '\n')
		if newline < 0 {
			break
		}
		line := append([]byte(nil), collector.buffer[:newline]...)
		collector.buffer = collector.buffer[newline+1:]
		collector.consume(line)
		if collector.err != nil || collector.stopped {
			break
		}
	}
	return written, nil
}

func (collector *framePTSCollector) consume(line []byte) {
	value := strings.TrimSpace(string(line))
	if value == "" {
		return
	}
	if strings.Contains(value, ",") {
		parts := strings.Split(value, ",")
		value = parts[len(parts)-1]
	}
	pts, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		collector.err = errors.New("frame timestamp inventory returned an invalid PTS")
		collector.stop()
		return
	}
	collector.frames++
	if collector.frames > collector.maximumFrames {
		collector.err = errors.New("frame timestamp inventory exceeded the frame limit")
		collector.stop()
		return
	}
	if collector.hasPrevious && pts < collector.previous {
		collector.err = errors.New("frame timestamp inventory was not presentation ordered")
		collector.stop()
		return
	}
	collector.previous, collector.hasPrevious = pts, true
	if !collector.hasFirst {
		collector.first, collector.hasFirst = pts, true
	}
	afterMaximum := true
	for index, request := range collector.requests {
		comparison, compareErr := compareFramePTS(pts, collector.timeBase, request)
		if compareErr != nil {
			collector.err = compareErr
			collector.stop()
			return
		}
		if comparison <= 0 {
			collector.selected[index], collector.hasSelection[index] = pts, true
			afterMaximum = false
		}
	}
	if afterMaximum {
		collector.stopped = true
		collector.stop()
	}
}

func (collector *framePTSCollector) Finish() error {
	if collector.err != nil {
		return collector.err
	}
	if len(bytes.TrimSpace(collector.buffer)) > 0 && !collector.stopped {
		collector.consume(collector.buffer)
	}
	if collector.err != nil {
		return collector.err
	}
	if !collector.hasFirst {
		return errors.New("selected SourceStream has no presentation frames")
	}
	for index := range collector.selected {
		if !collector.hasSelection[index] {
			collector.selected[index] = collector.first
		}
	}
	return nil
}

func (collector *framePTSCollector) Selected() ([]int64, error) {
	if err := collector.Finish(); err != nil {
		return nil, err
	}
	return append([]int64(nil), collector.selected...), nil
}

func (collector *framePTSCollector) StoppedAfterMaximum() bool { return collector.stopped }

func compareFramePTS(pts int64, timeBase, request domain.RationalTime) (int, error) {
	if timeBase.Validate() != nil || request.Validate() != nil {
		return 0, domain.ErrInvalidRationalTime
	}
	left := new(big.Int).Mul(big.NewInt(pts), big.NewInt(timeBase.Value.Value()))
	left.Mul(left, big.NewInt(int64(request.Scale)))
	right := new(big.Int).Mul(big.NewInt(request.Value.Value()), big.NewInt(int64(timeBase.Scale)))
	return left.Cmp(right), nil
}
