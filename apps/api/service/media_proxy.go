package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
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

type ExternalMediaProxyExecutor struct {
	access   *SourceAccess
	probe    string
	encoder  string
	version  string
	tempRoot string
	profile  lifecycle.Profile
	wallTime time.Duration
}

func NewExternalMediaProxyExecutor(
	access *SourceAccess,
	probe string,
	encoder string,
	version string,
	tempRoot string,
	profile lifecycle.Profile,
) (*ExternalMediaProxyExecutor, error) {
	if access == nil || !cleanAbsolute(probe) || !cleanAbsolute(encoder) || version == "" ||
		len(version) > 256 || !cleanAbsolute(tempRoot) ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("media proxy executor configuration is invalid")
	}
	for _, executable := range []string{probe, encoder} {
		if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("media proxy executor is unavailable")
		}
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create media attempt root: %w", err)
	}
	return &ExternalMediaProxyExecutor{
		access: access, probe: probe, encoder: encoder, version: version,
		tempRoot: tempRoot, profile: profile, wallTime: 12 * time.Hour,
	}, nil
}

func (executor *ExternalMediaProxyExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{Kind: domain.MediaJobProxy, Version: executor.version}
}

func (executor *ExternalMediaProxyExecutor) Execute(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	proxy, err := executor.encode(ctx, claim)
	if err != nil {
		return application.MediaJobExecution{}, err
	}
	return application.MediaJobExecution{Proxy: &proxy}, nil
}

func (executor *ExternalMediaProxyExecutor) encode(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaProxyExecution, error) {
	parameters, err := application.DecodeInitialMediaJobParameters(claim.ParametersJSON)
	var video, audio *domain.SourceStream
	var selectionErr error
	if err == nil && parameters.ProxySelection != nil {
		video, audio, selectionErr = application.SelectSourceProxyStreams(
			claim.SourceStreams, *parameters.ProxySelection,
		)
	}
	if err != nil || selectionErr != nil || claim.Kind != domain.MediaJobProxy || claim.AttemptID.IsZero() ||
		claim.AssetID.IsZero() || claim.AcceptedFingerprint == nil || parameters.AssetID != claim.AssetID ||
		parameters.Kind != domain.MediaJobProxy || parameters.Profile != application.SourceProxyProfile {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	colorInterpretation, err := proxyColorInterpretation(video)
	if err != nil {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-color-unsupported", err)
	}
	channelProjection, err := proxyChannelProjection(audio)
	if err != nil {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-audio-layout-unsupported", err)
	}
	source, err := executor.access.resolveAssetSource(ctx, claim.AssetID)
	if err != nil {
		return application.MediaProxyExecution{}, err
	}
	if source.Observation != claim.ExpectedObservation {
		return application.MediaProxyExecution{}, application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	attemptRoot := filepath.Join(executor.tempRoot, claim.AttemptID.String())
	if !pathWithin(executor.tempRoot, attemptRoot) {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError(
			"attempt-storage-unavailable", err,
		)
	}
	keepWorkspace := false
	defer func() {
		if !keepWorkspace {
			_ = os.RemoveAll(attemptRoot)
		}
	}()
	executionContext, cancel := context.WithTimeout(ctx, executor.wallTime)
	defer cancel()

	var sourceVideoPTS, sourceAudioPTS []int64
	if video != nil {
		sourceVideoPTS, err = executor.inventoryTrackPTS(
			executionContext, attemptRoot, source.Path, video.Descriptor, application.MaximumSourceProxyFrames,
		)
	}
	if err == nil && audio != nil {
		sourceAudioPTS, err = executor.inventoryTrackPTS(
			executionContext, attemptRoot, source.Path, audio.Descriptor, 1,
		)
	}
	if err != nil {
		return application.MediaProxyExecution{}, executor.proxyFailure(
			ctx, executionContext, source, *claim.AcceptedFingerprint, err,
		)
	}
	epoch, videoStart, audioStart, err := proxySourceEpoch(video, sourceVideoPTS, audio, sourceAudioPTS)
	if err != nil {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-time-invalid", err)
	}
	width, height := uint32(0), uint32(0)
	if video != nil {
		width, height, err = normalizedProxyDimensions(video.Descriptor)
		if err != nil {
			return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-profile-invalid", err)
		}
	}
	outputPath := filepath.Join(attemptRoot, "proxy.webm")
	args, err := proxyEncodeArgs(
		source.Path, outputPath, video, audio, epoch, videoStart, audioStart,
		width, height, colorInterpretation, channelProjection,
	)
	if err != nil {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-profile-invalid", err)
	}
	stderr := &boundedBuffer{limit: 64 << 10}
	err = lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executor.encoder, Args: args, Directory: attemptRoot, Env: executorEnvironment(),
		Stdout: io.Discard, Stderr: stderr, Profile: executor.profile,
		Presentation: lifecycle.PresentationHeadless, ContainProcessTree: true,
		TerminationGrace: 5 * time.Second,
	})
	if err != nil || stderr.exceeded {
		return application.MediaProxyExecution{}, executor.proxyFailure(
			ctx, executionContext, source, *claim.AcceptedFingerprint,
			fmt.Errorf("proxy encode failed: %s", strings.TrimSpace(stderr.String())),
		)
	}
	outputProbe, err := executor.probeOutput(executionContext, attemptRoot, outputPath)
	if err != nil {
		return application.MediaProxyExecution{}, executor.proxyFailure(
			ctx, executionContext, source, *claim.AcceptedFingerprint, err,
		)
	}
	outputVideo, outputAudio, err := validateProxyOutput(outputProbe, video != nil, audio != nil, width, height)
	if err != nil {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-output-invalid", err)
	}
	result := application.MediaProxyExecution{SourceEpoch: epoch}
	result.Media, err = inspectProxyFile(outputPath, "proxy.webm", proxyMIME(video != nil))
	if err != nil {
		return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-output-invalid", err)
	}
	if video != nil {
		proxyPTS, ptsErr := executor.inventoryTrackPTS(
			executionContext, attemptRoot, outputPath, *outputVideo, application.MaximumSourceProxyFrames,
		)
		if ptsErr != nil || len(proxyPTS) != len(sourceVideoPTS) {
			return application.MediaProxyExecution{}, application.NewMediaExecutionError(
				"proxy-frame-map-invalid", ptsErr,
			)
		}
		mapRecord, mapErr := writeProxyTimeMap(attemptRoot, sourceVideoPTS, proxyPTS)
		if mapErr != nil {
			return application.MediaProxyExecution{}, application.NewMediaExecutionError(
				"proxy-frame-map-invalid", mapErr,
			)
		}
		proxyStart, timeErr := frameTime(proxyPTS[0], outputVideo.TimeBase)
		if timeErr != nil {
			return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-time-invalid", timeErr)
		}
		frameCount, _ := domain.NewUInt64(uint64(len(proxyPTS)))
		result.Video = &application.SourceProxyVideoTrack{
			Source: *video, SourceStartTime: *videoStart, ProxyStartTime: proxyStart,
			TimeBase: outputVideo.TimeBase, Codec: "vp9", Width: width, Height: height,
			PixelFormat: "yuv420p", ColorRange: "tv", ColorSpace: "bt709",
			ColorTransfer: "bt709", ColorPrimaries: "bt709",
			ColorInterpretation: colorInterpretation, FrameCount: frameCount, TimeMap: mapRecord,
		}
	}
	if audio != nil {
		decodedSampleCount, sampleErr := executor.inventoryProxyAudioSamples(
			executionContext, attemptRoot, outputPath, *outputAudio,
		)
		if sampleErr != nil {
			return application.MediaProxyExecution{}, application.NewMediaExecutionError(
				"proxy-audio-samples-invalid", sampleErr,
			)
		}
		proxyPTS, ptsErr := executor.inventoryTrackPTS(executionContext, attemptRoot, outputPath, *outputAudio, 1)
		if ptsErr != nil {
			return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-time-invalid", ptsErr)
		}
		proxyStart, timeErr := frameTime(proxyPTS[0], outputAudio.TimeBase)
		if timeErr != nil {
			return application.MediaProxyExecution{}, application.NewMediaExecutionError("proxy-time-invalid", timeErr)
		}
		result.Audio = &application.SourceProxyAudioTrack{
			Source: *audio, SourceStartTime: *audioStart, ProxyStartTime: proxyStart,
			TimeBase: outputAudio.TimeBase, Codec: "opus", SampleRate: 48000,
			Channels: 2, ChannelLayout: "stereo",
			ChannelProjection: channelProjection, DecodedSampleCount: decodedSampleCount,
		}
	}
	if err := verifyProbeSource(source.Path, source.Observation, *claim.AcceptedFingerprint); err != nil {
		return application.MediaProxyExecution{}, err
	}
	result.Workspace = newProxyWorkspace(attemptRoot, video != nil)
	keepWorkspace = true
	return result, nil
}

func (executor *ExternalMediaProxyExecutor) proxyFailure(
	parent context.Context,
	execution context.Context,
	source resolvedAssetSource,
	fingerprint domain.Digest,
	cause error,
) error {
	if sourceErr := verifyProbeSource(source.Path, source.Observation, fingerprint); sourceErr != nil {
		return sourceErr
	}
	code := "proxy-encode-failed"
	if errors.Is(execution.Err(), context.DeadlineExceeded) {
		code = "proxy-encode-timeout"
	} else if parent.Err() != nil {
		return parent.Err()
	}
	return application.NewMediaExecutionError(code, cause)
}

func proxyColorInterpretation(video *domain.SourceStream) (string, error) {
	if video == nil {
		return "", nil
	}
	facts := video.Descriptor.Video
	transfer := strings.ToLower(facts.ColorTransfer)
	if transfer == "smpte2084" || transfer == "arib-std-b67" ||
		strings.Contains(strings.ToLower(video.Descriptor.CodecProfile), "dolby vision") {
		return "", fmt.Errorf("HDR source requires a later tone-mapped proxy profile")
	}
	if facts.ColorSpace == "" || facts.ColorTransfer == "" || facts.ColorPrimaries == "" {
		return "assumed-bt709", nil
	}
	return "source-metadata", nil
}

func proxyChannelProjection(audio *domain.SourceStream) (string, error) {
	if audio == nil {
		return "", nil
	}
	switch audio.Descriptor.Audio.Channels {
	case 1:
		return "mono-duplicate-v1", nil
	case 2:
		return "stereo-pass-v1", nil
	default:
		return "", fmt.Errorf("audio layout has no v1 downmix policy")
	}
}

func proxySourceEpoch(
	video *domain.SourceStream,
	videoPTS []int64,
	audio *domain.SourceStream,
	audioPTS []int64,
) (domain.RationalTime, *domain.RationalTime, *domain.RationalTime, error) {
	var videoStart, audioStart *domain.RationalTime
	if video != nil {
		if len(videoPTS) == 0 {
			return domain.RationalTime{}, nil, nil, domain.ErrInvalidMediaFacts
		}
		value, err := frameTime(videoPTS[0], video.Descriptor.TimeBase)
		if err != nil {
			return domain.RationalTime{}, nil, nil, err
		}
		videoStart = &value
	}
	if audio != nil {
		if len(audioPTS) == 0 {
			return domain.RationalTime{}, nil, nil, domain.ErrInvalidMediaFacts
		}
		value, err := frameTime(audioPTS[0], audio.Descriptor.TimeBase)
		if err != nil {
			return domain.RationalTime{}, nil, nil, err
		}
		audioStart = &value
	}
	epoch := videoStart
	if epoch == nil {
		epoch = audioStart
	} else if audioStart != nil {
		comparison, err := audioStart.Compare(*epoch)
		if err != nil {
			return domain.RationalTime{}, nil, nil, err
		}
		if comparison < 0 {
			epoch = audioStart
		}
	}
	if epoch == nil {
		return domain.RationalTime{}, nil, nil, domain.ErrInvalidMediaFacts
	}
	return *epoch, videoStart, audioStart, nil
}

func proxyEncodeArgs(
	source string,
	output string,
	video *domain.SourceStream,
	audio *domain.SourceStream,
	epoch domain.RationalTime,
	videoStart *domain.RationalTime,
	audioStart *domain.RationalTime,
	width uint32,
	height uint32,
	colorInterpretation string,
	channelProjection string,
) ([]string, error) {
	filters := make([]string, 0, 2)
	args := []string{
		"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
		"-protocol_whitelist", "file,pipe,fd",
		"-noautorotate", "-i", source,
	}
	if video != nil {
		offset, err := rationalDifference(*videoStart, epoch)
		if err != nil {
			return nil, err
		}
		chain := orientationFilters(video.Descriptor.Video.Rotation)
		chain = append(chain,
			fmt.Sprintf("scale=%d:%d:flags=lanczos+accurate_rnd+full_chroma_int", width, height),
			"setsar=1",
		)
		color := "colorspace=all=bt709:range=tv:format=yuv420p:fast=0"
		if colorInterpretation == "assumed-bt709" {
			color += ":iall=bt709"
		}
		chain = append(chain, color, "format=yuv420p", "setpts=PTS-STARTPTS+"+filterTime(offset)+"/TB")
		filters = append(filters, fmt.Sprintf("[0:%d]%s[v]", video.Descriptor.Index, strings.Join(chain, ",")))
	}
	if audio != nil {
		offset, err := rationalDifference(*audioStart, epoch)
		if err != nil {
			return nil, err
		}
		pan := "pan=stereo|c0=c0|c1=c1"
		if channelProjection == "mono-duplicate-v1" {
			pan = "pan=stereo|c0=c0|c1=c0"
		}
		chain := []string{
			pan,
			"aresample=48000:filter_size=32:phase_shift=10:linear_interp=0:exact_rational=1",
			"aformat=sample_fmts=fltp:channel_layouts=stereo",
			"asetpts=PTS-STARTPTS+" + filterTime(offset) + "/TB",
		}
		filters = append(filters, fmt.Sprintf("[0:%d]%s[a]", audio.Descriptor.Index, strings.Join(chain, ",")))
	}
	args = append(args, "-filter_complex", strings.Join(filters, ";"))
	if video != nil {
		args = append(args,
			"-map", "[v]", "-c:v", "libvpx-vp9", "-pix_fmt", "yuv420p",
			"-deadline", "good", "-cpu-used", "4", "-threads", "1", "-row-mt", "0",
			"-tile-columns", "0", "-tile-rows", "0", "-frame-parallel", "0",
			"-lag-in-frames", "0", "-auto-alt-ref", "0", "-b:v", "0", "-crf", "32",
			"-g", "999999", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-fps_mode", "passthrough", "-color_primaries", "bt709", "-color_trc", "bt709",
			"-colorspace", "bt709", "-color_range", "tv",
		)
	}
	if audio != nil {
		args = append(args,
			"-map", "[a]", "-c:a", "libopus", "-ar", "48000", "-ac", "2", "-b:a", "128k",
			"-vbr", "off", "-compression_level", "10", "-frame_duration", "20",
			"-application", "audio", "-mapping_family", "0",
		)
	}
	return append(args,
		"-map_metadata", "-1", "-map_chapters", "-1", "-fflags", "+bitexact",
		"-f", "webm", "-y", output,
	), nil
}

func filterTime(value domain.RationalTime) string {
	return "(" + value.Value.String() + "/" + strconv.FormatInt(int64(value.Scale), 10) + ")"
}

func rationalDifference(left, right domain.RationalTime) (domain.RationalTime, error) {
	numerator := new(big.Int).Sub(
		new(big.Int).Mul(big.NewInt(left.Value.Value()), big.NewInt(int64(right.Scale))),
		new(big.Int).Mul(big.NewInt(right.Value.Value()), big.NewInt(int64(left.Scale))),
	)
	denominator := new(big.Int).Mul(big.NewInt(int64(left.Scale)), big.NewInt(int64(right.Scale)))
	divisor := new(big.Int).GCD(nil, nil, new(big.Int).Abs(new(big.Int).Set(numerator)), denominator)
	numerator.Quo(numerator, divisor)
	denominator.Quo(denominator, divisor)
	if !numerator.IsInt64() || !denominator.IsInt64() || denominator.Int64() > math.MaxInt32 {
		return domain.RationalTime{}, domain.ErrTimeOverflow
	}
	return domain.NewRationalTime(numerator.Int64(), int32(denominator.Int64()))
}

func normalizedProxyDimensions(descriptor domain.SourceStreamDescriptor) (uint32, uint32, error) {
	if descriptor.Video == nil || descriptor.Video.Width == 0 || descriptor.Video.Height == 0 {
		return 0, 0, domain.ErrInvalidMediaFacts
	}
	width := new(big.Rat).SetInt64(int64(descriptor.Video.Width))
	height := new(big.Rat).SetInt64(int64(descriptor.Video.Height))
	if descriptor.Video.PixelAspect != nil {
		width.Mul(width, new(big.Rat).SetFrac(
			big.NewInt(descriptor.Video.PixelAspect.Value.Value()),
			big.NewInt(int64(descriptor.Video.PixelAspect.Scale)),
		))
	}
	if descriptor.Video.Rotation == 90 || descriptor.Video.Rotation == 270 {
		width, height = height, width
	}
	long := width
	if height.Cmp(width) > 0 {
		long = height
	}
	maximum := new(big.Rat).SetInt64(1920)
	if long.Cmp(maximum) > 0 {
		scale := new(big.Rat).Quo(maximum, long)
		width.Mul(width, scale)
		height.Mul(height, scale)
	}
	resultWidth, ok := roundedEvenDimension(width)
	if !ok {
		return 0, 0, domain.ErrInvalidMediaFacts
	}
	resultHeight, ok := roundedEvenDimension(height)
	if !ok {
		return 0, 0, domain.ErrInvalidMediaFacts
	}
	return resultWidth, resultHeight, nil
}

func roundedEvenDimension(value *big.Rat) (uint32, bool) {
	numerator := new(big.Int).Mul(value.Num(), big.NewInt(2))
	numerator.Add(numerator, value.Denom())
	denominator := new(big.Int).Mul(value.Denom(), big.NewInt(2))
	rounded := new(big.Int).Quo(numerator, denominator)
	if !rounded.IsUint64() || rounded.Uint64() < 2 || rounded.Uint64() > 1920 {
		return 0, false
	}
	result := uint32(rounded.Uint64())
	if result%2 != 0 {
		result--
	}
	return result, result >= 2
}

func (executor *ExternalMediaProxyExecutor) inventoryTrackPTS(
	ctx context.Context,
	directory string,
	source string,
	descriptor domain.SourceStreamDescriptor,
	maximum int,
) ([]int64, error) {
	scanContext, stop := context.WithCancel(ctx)
	collector := &proxyPTSCollector{maximum: maximum, stop: stop}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err := lifecycle.Run(scanContext, lifecycle.ProcessSpec{
		Executable: executor.probe,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "file",
			"-select_streams", strconv.FormatUint(uint64(descriptor.Index), 10),
			"-show_frames", "-show_entries", "frame=best_effort_timestamp", "-of", "csv=p=0", source,
		},
		Directory: directory, Env: executorEnvironment(), Stdout: collector, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	stop()
	if finishErr := collector.Finish(); finishErr != nil {
		return nil, finishErr
	}
	if stderr.exceeded {
		return nil, fmt.Errorf("timestamp diagnostics exceeded the limit")
	}
	if err != nil && !collector.stopped {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("timestamp inventory failed: %s", strings.TrimSpace(stderr.String()))
	}
	return collector.Values(), nil
}

type proxyPTSCollector struct {
	maximum  int
	stop     context.CancelFunc
	values   []int64
	buffer   []byte
	previous int64
	hasValue bool
	stopped  bool
	err      error
}

func (collector *proxyPTSCollector) Write(data []byte) (int, error) {
	if collector.err != nil || collector.stopped {
		return len(data), nil
	}
	collector.buffer = append(collector.buffer, data...)
	if len(collector.buffer) > 256 && !strings.ContainsRune(string(collector.buffer), '\n') {
		collector.err = domain.ErrInvalidMediaFacts
		return len(data), nil
	}
	for {
		index := strings.IndexByte(string(collector.buffer), '\n')
		if index < 0 {
			break
		}
		collector.consume(collector.buffer[:index])
		collector.buffer = collector.buffer[index+1:]
		if collector.err != nil || collector.stopped {
			break
		}
	}
	return len(data), nil
}

func (collector *proxyPTSCollector) consume(line []byte) {
	text := strings.TrimSpace(string(line))
	if text == "" || text == "N/A" {
		return
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || (collector.hasValue && value <= collector.previous) {
		collector.err = domain.ErrInvalidMediaFacts
		return
	}
	collector.values = append(collector.values, value)
	collector.previous, collector.hasValue = value, true
	if len(collector.values) >= collector.maximum {
		collector.stopped = collector.maximum == 1
		if collector.stopped {
			collector.stop()
		}
	}
	if len(collector.values) > collector.maximum {
		collector.err = domain.ErrInvalidMediaFacts
	}
}

func (collector *proxyPTSCollector) Finish() error {
	if collector.err != nil {
		return collector.err
	}
	if len(collector.buffer) > 0 && !collector.stopped {
		collector.consume(collector.buffer)
	}
	if collector.err != nil || len(collector.values) == 0 {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func (collector *proxyPTSCollector) Values() []int64 {
	return append([]int64(nil), collector.values...)
}

func (executor *ExternalMediaProxyExecutor) probeOutput(
	ctx context.Context,
	directory string,
	path string,
) (application.MediaProbe, error) {
	stdout := &boundedBuffer{limit: maximumProbeOutputBytes}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: executor.probe,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "file",
			"-show_entries", probeShowEntries, "-of", "json=compact=1", path,
		},
		Directory: directory, Env: executorEnvironment(), Stdout: stdout, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded {
		return application.MediaProbe{}, fmt.Errorf("proxy probe failed: %s", strings.TrimSpace(stderr.String()))
	}
	return DecodeFFProbeOutput(stdout.Bytes())
}

func validateProxyOutput(
	probe application.MediaProbe,
	hasVideo bool,
	hasAudio bool,
	width uint32,
	height uint32,
) (*domain.SourceStreamDescriptor, *domain.SourceStreamDescriptor, error) {
	if (!proxyContainer(probe.Container) && !anyProxyContainer(probe.ContainerAliases)) ||
		len(probe.Streams) != boolCount(hasVideo)+boolCount(hasAudio) {
		return nil, nil, domain.ErrInvalidMediaFacts
	}
	var video, audio *domain.SourceStreamDescriptor
	for _, descriptor := range probe.Streams {
		current := descriptor
		switch descriptor.MediaType {
		case domain.MediaVideo:
			if video != nil || !hasVideo || descriptor.Codec != "vp9" || descriptor.Video == nil ||
				descriptor.Video.Width != width || descriptor.Video.Height != height ||
				descriptor.Video.PixelFormat != "yuv420p" || descriptor.Video.ColorRange != "tv" ||
				descriptor.Video.ColorSpace != "bt709" || descriptor.Video.ColorTransfer != "bt709" ||
				descriptor.Video.ColorPrimaries != "bt709" {
				return nil, nil, domain.ErrInvalidMediaFacts
			}
			video = &current
		case domain.MediaAudio:
			if audio != nil || !hasAudio || descriptor.Codec != "opus" || descriptor.Audio == nil ||
				descriptor.Audio.SampleRate != 48000 || descriptor.Audio.Channels != 2 ||
				descriptor.Audio.ChannelLayout != "stereo" {
				return nil, nil, domain.ErrInvalidMediaFacts
			}
			audio = &current
		default:
			return nil, nil, domain.ErrInvalidMediaFacts
		}
	}
	if (hasVideo && video == nil) || (hasAudio && audio == nil) {
		return nil, nil, domain.ErrInvalidMediaFacts
	}
	return video, audio, nil
}

func proxyContainer(value string) bool {
	return value == "webm" || value == "matroska"
}

func anyProxyContainer(values []string) bool {
	for _, value := range values {
		if proxyContainer(value) {
			return true
		}
	}
	return false
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func proxyMIME(hasVideo bool) string {
	if hasVideo {
		return "video/webm"
	}
	return "audio/webm"
}
