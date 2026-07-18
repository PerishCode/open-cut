package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type ExternalMediaRenderInputExecutor struct {
	media   *ExternalMediaProxyExecutor
	version string
	target  string
}

func NewExternalMediaRenderInputExecutor(
	access *SourceAccess,
	probe string,
	encoder string,
	version string,
	targetValue string,
	tempRoot string,
	profile lifecycle.Profile,
) (*ExternalMediaRenderInputExecutor, error) {
	if access == nil || !cleanAbsolute(probe) || !cleanAbsolute(encoder) || version == "" ||
		len(version) > 1024 || validateRendererTarget(targetValue) != nil || !cleanAbsolute(tempRoot) ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("media render-input executor configuration is invalid")
	}
	for _, executable := range []string{probe, encoder} {
		if info, err := os.Lstat(executable); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("media render-input executor is unavailable")
		}
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create render-input attempt root: %w", err)
	}
	return &ExternalMediaRenderInputExecutor{
		media: &ExternalMediaProxyExecutor{
			access: access, probe: probe, encoder: encoder, version: version,
			tempRoot: tempRoot, profile: profile, wallTime: 12 * time.Hour,
		},
		version: version, target: targetValue,
	}, nil
}

func (executor *ExternalMediaRenderInputExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{
		Kind: domain.MediaJobRenderInput, Version: executor.version, Target: executor.target,
	}
}

func (executor *ExternalMediaRenderInputExecutor) Execute(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	result, err := executor.encode(ctx, claim)
	if err != nil {
		return application.MediaJobExecution{}, err
	}
	return application.MediaJobExecution{RenderInput: &result}, nil
}

func (executor *ExternalMediaRenderInputExecutor) encode(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaRenderInputExecution, error) {
	parameters, err := application.DecodeInitialMediaJobParameters(claim.ParametersJSON)
	var video, audio *domain.SourceStream
	var selectionErr error
	if err == nil && parameters.RenderInputSelection != nil {
		video, audio, selectionErr = application.SelectSourceProxyStreams(
			claim.SourceStreams, *parameters.RenderInputSelection,
		)
	}
	if err != nil || selectionErr != nil || claim.Kind != domain.MediaJobRenderInput ||
		claim.AttemptID.IsZero() || claim.AssetID.IsZero() || claim.AcceptedFingerprint == nil ||
		claim.ExecutorTarget != executor.target || parameters.AssetID != claim.AssetID ||
		parameters.Kind != domain.MediaJobRenderInput || parameters.Profile != application.RenderInputProfile ||
		(video == nil) == (audio == nil) {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	colorInterpretation, err := renderInputColorInterpretation(video)
	if err != nil {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"render-input-color-unsupported", err,
		)
	}
	channelProjection, err := proxyChannelProjection(audio)
	if err != nil {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"render-input-audio-layout-unsupported", err,
		)
	}
	width, height := uint32(0), uint32(0)
	if video != nil {
		width, height, err = normalizedRenderInputDimensions(video.Descriptor)
		if err != nil {
			return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
				"render-input-profile-unsupported", err,
			)
		}
	}
	source, err := executor.media.access.resolveAssetSource(ctx, claim.AssetID)
	if err != nil {
		return application.MediaRenderInputExecution{}, err
	}
	if source.Observation != claim.ExpectedObservation {
		return application.MediaRenderInputExecution{}, application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	attemptRoot := filepath.Join(executor.media.tempRoot, claim.AttemptID.String())
	if !pathWithin(executor.media.tempRoot, attemptRoot) {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"attempt-storage-unavailable", err,
		)
	}
	keepWorkspace := false
	defer func() {
		if !keepWorkspace {
			_ = os.RemoveAll(attemptRoot)
		}
	}()
	executionContext, cancel := context.WithTimeout(ctx, executor.media.wallTime)
	defer cancel()
	var sourceVideoPTS, sourceAudioPTS []int64
	if video != nil {
		sourceVideoPTS, err = executor.media.inventoryTrackPTS(
			executionContext, attemptRoot, source.Path, video.Descriptor,
			application.MaximumRenderInputFrames,
		)
	}
	if err == nil && audio != nil {
		sourceAudioPTS, err = executor.media.inventoryTrackPTS(
			executionContext, attemptRoot, source.Path, audio.Descriptor, 1,
		)
	}
	if err != nil {
		return application.MediaRenderInputExecution{}, executor.failure(
			ctx, executionContext, source, *claim.AcceptedFingerprint, err,
		)
	}
	epoch, videoStart, audioStart, err := proxySourceEpoch(video, sourceVideoPTS, audio, sourceAudioPTS)
	if err != nil {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"render-input-time-invalid", err,
		)
	}
	outputPath := filepath.Join(attemptRoot, "render-input.mkv")
	args, err := renderInputEncodeArgs(
		source.Path, outputPath, video, audio, epoch, videoStart, audioStart,
		colorInterpretation, channelProjection,
	)
	if err != nil {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"render-input-profile-unsupported", err,
		)
	}
	stderr := &boundedBuffer{limit: 64 << 10}
	err = lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executor.media.encoder, Args: args, Directory: attemptRoot, Env: executorEnvironment(),
		Stdout: io.Discard, Stderr: stderr, Profile: executor.media.profile,
		Presentation: lifecycle.PresentationHeadless, ContainProcessTree: true,
		TerminationGrace: 5 * time.Second,
	})
	if err != nil || stderr.exceeded {
		return application.MediaRenderInputExecution{}, executor.failure(
			ctx, executionContext, source, *claim.AcceptedFingerprint,
			fmt.Errorf("render-input encode failed: %s", strings.TrimSpace(stderr.String())),
		)
	}
	outputProbe, err := executor.media.probeOutput(executionContext, attemptRoot, outputPath)
	if err != nil {
		return application.MediaRenderInputExecution{}, executor.failure(
			ctx, executionContext, source, *claim.AcceptedFingerprint, err,
		)
	}
	outputVideo, outputAudio, err := validateRenderInputOutput(
		outputProbe, video != nil, audio != nil, width, height,
	)
	if err != nil {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"render-input-output-invalid", err,
		)
	}
	result := application.MediaRenderInputExecution{SourceEpoch: epoch}
	result.Media, err = inspectRenderInputFile(outputPath, "render-input.mkv", "video/x-matroska")
	if err != nil {
		return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
			"render-input-output-invalid", err,
		)
	}
	if video != nil {
		materialPTS, ptsErr := executor.media.inventoryTrackPTS(
			executionContext, attemptRoot, outputPath, *outputVideo,
			application.MaximumRenderInputFrames,
		)
		if ptsErr != nil || len(materialPTS) != len(sourceVideoPTS) {
			return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
				"render-input-frame-map-invalid", ptsErr,
			)
		}
		mapRecord, mapErr := writeRenderInputTimeMap(attemptRoot, sourceVideoPTS, materialPTS)
		if mapErr != nil {
			return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
				"render-input-frame-map-invalid", mapErr,
			)
		}
		materialStart, timeErr := frameTime(materialPTS[0], outputVideo.TimeBase)
		if timeErr != nil {
			return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
				"render-input-time-invalid", timeErr,
			)
		}
		frameCount, _ := domain.NewUInt64(uint64(len(materialPTS)))
		result.Video = &application.RenderInputVideoTrack{
			Source: *video, SourceStartTime: *videoStart, MaterialStartTime: materialStart,
			TimeBase: outputVideo.TimeBase, Codec: "ffv1", Width: width, Height: height,
			PixelFormat: "yuv420p", ColorRange: "tv", ColorSpace: "bt709",
			ColorTransfer: "bt709", ColorPrimaries: "bt709",
			ColorInterpretation: colorInterpretation, FrameCount: frameCount, TimeMap: mapRecord,
		}
	}
	if audio != nil {
		decodedSampleCount, sampleErr := executor.inventoryPCMAudioSamples(
			executionContext, attemptRoot, outputPath, *outputAudio,
		)
		if sampleErr != nil {
			return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
				"render-input-audio-samples-invalid", sampleErr,
			)
		}
		materialPTS, ptsErr := executor.media.inventoryTrackPTS(
			executionContext, attemptRoot, outputPath, *outputAudio, 1,
		)
		if ptsErr != nil {
			return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
				"render-input-time-invalid", ptsErr,
			)
		}
		materialStart, timeErr := frameTime(materialPTS[0], outputAudio.TimeBase)
		if timeErr != nil {
			return application.MediaRenderInputExecution{}, application.NewMediaExecutionError(
				"render-input-time-invalid", timeErr,
			)
		}
		result.Audio = &application.RenderInputAudioTrack{
			Source: *audio, SourceStartTime: *audioStart, MaterialStartTime: materialStart,
			TimeBase: outputAudio.TimeBase, Codec: "pcm_s16le", SampleFormat: "s16",
			SampleRate: 48000, Channels: 2, ChannelLayout: "stereo",
			ChannelProjection: channelProjection, DecodedSampleCount: decodedSampleCount,
		}
	}
	if err := verifyProbeSource(source.Path, source.Observation, *claim.AcceptedFingerprint); err != nil {
		return application.MediaRenderInputExecution{}, err
	}
	result.Workspace = newRenderInputWorkspace(attemptRoot, video != nil)
	keepWorkspace = true
	return result, nil
}

func (executor *ExternalMediaRenderInputExecutor) failure(
	parent context.Context,
	execution context.Context,
	source resolvedAssetSource,
	fingerprint domain.Digest,
	cause error,
) error {
	if sourceErr := verifyProbeSource(source.Path, source.Observation, fingerprint); sourceErr != nil {
		return sourceErr
	}
	code := "render-input-encode-failed"
	if errors.Is(execution.Err(), context.DeadlineExceeded) {
		code = "render-input-encode-timeout"
	} else if parent.Err() != nil {
		return parent.Err()
	}
	return application.NewMediaExecutionError(code, cause)
}

func renderInputColorInterpretation(video *domain.SourceStream) (string, error) {
	if video == nil {
		return "", nil
	}
	facts := video.Descriptor.Video
	if facts == nil {
		return "", fmt.Errorf("source video facts are unavailable")
	}
	transfer := strings.ToLower(facts.ColorTransfer)
	if transfer == "smpte2084" || transfer == "arib-std-b67" ||
		strings.Contains(strings.ToLower(video.Descriptor.CodecProfile), "dolby vision") {
		return "", fmt.Errorf("HDR source requires a later render-input profile")
	}
	// The render-input geometry contract is square-pixel SDR yuv420p at limited
	// range; those are structural and stay strict.
	if facts.PixelAspect == nil || facts.PixelAspect.Value.Value() != int64(facts.PixelAspect.Scale) ||
		(strings.ToLower(facts.ColorRange) != "tv" && strings.ToLower(facts.ColorRange) != "mpeg") ||
		strings.ToLower(facts.PixelFormat) != "yuv420p" {
		return "", fmt.Errorf("source is not admitted square-pixel SDR yuv420p tv-range")
	}
	// Match the proxy path: incomplete color tags adopt a recorded Rec.709
	// assumption rather than block export, so footage that previews also
	// exports. Explicitly tagged non-Rec.709 SDR is still rejected.
	if facts.ColorSpace == "" || facts.ColorTransfer == "" || facts.ColorPrimaries == "" {
		return "assumed-bt709", nil
	}
	if strings.ToLower(facts.ColorSpace) != "bt709" || strings.ToLower(facts.ColorTransfer) != "bt709" ||
		strings.ToLower(facts.ColorPrimaries) != "bt709" {
		return "", fmt.Errorf("source is not Rec.709 SDR")
	}
	return "source-metadata", nil
}

func normalizedRenderInputDimensions(descriptor domain.SourceStreamDescriptor) (uint32, uint32, error) {
	if descriptor.Video == nil || descriptor.Video.Width < 2 || descriptor.Video.Height < 2 ||
		descriptor.Video.PixelAspect == nil ||
		descriptor.Video.PixelAspect.Value.Value() != int64(descriptor.Video.PixelAspect.Scale) {
		return 0, 0, domain.ErrInvalidMediaFacts
	}
	width, height := descriptor.Video.Width, descriptor.Video.Height
	if descriptor.Video.Rotation == 90 || descriptor.Video.Rotation == 270 {
		width, height = height, width
	}
	if width%2 != 0 || height%2 != 0 {
		return 0, 0, domain.ErrInvalidMediaFacts
	}
	return width, height, nil
}

func renderInputEncodeArgs(
	source string,
	output string,
	video *domain.SourceStream,
	audio *domain.SourceStream,
	epoch domain.RationalTime,
	videoStart *domain.RationalTime,
	audioStart *domain.RationalTime,
	colorInterpretation string,
	channelProjection string,
) ([]string, error) {
	if colorInterpretation != "" && colorInterpretation != "source-metadata" &&
		colorInterpretation != "assumed-bt709" {
		return nil, domain.ErrInvalidMediaFacts
	}
	filters := make([]string, 0, 1)
	args := []string{
		"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
		"-protocol_whitelist", "file,pipe,fd", "-noautorotate", "-i", source,
	}
	if video != nil {
		offset, err := rationalDifference(*videoStart, epoch)
		if err != nil {
			return nil, err
		}
		chain := orientationFilters(video.Descriptor.Video.Rotation)
		color := "colorspace=all=bt709:range=tv:format=yuv420p:fast=0"
		if colorInterpretation == "assumed-bt709" {
			color += ":iall=bt709"
		}
		chain = append(chain,
			"setsar=1", color,
			"format=yuv420p", "setpts=PTS-STARTPTS+"+filterTime(offset)+"/TB",
		)
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
			"aresample=48000:filter_size=32:phase_shift=10:linear_interp=0:exact_rational=1:dither_method=none:osf=s16",
			"aformat=sample_fmts=s16:channel_layouts=stereo",
			"asetpts=PTS-STARTPTS+" + filterTime(offset) + "/TB",
		}
		filters = append(filters, fmt.Sprintf("[0:%d]%s[a]", audio.Descriptor.Index, strings.Join(chain, ",")))
	}
	args = append(args, "-filter_complex", strings.Join(filters, ";"))
	if video != nil {
		args = append(args,
			"-map", "[v]", "-c:v", "ffv1", "-level", "3", "-coder", "1", "-context", "1",
			"-g", "1", "-slicecrc", "1", "-threads", "1", "-pix_fmt", "yuv420p",
			"-flags:v", "+bitexact",
			"-fps_mode", "passthrough", "-color_primaries", "bt709", "-color_trc", "bt709",
			"-colorspace", "bt709", "-color_range", "tv",
		)
	}
	if audio != nil {
		args = append(args,
			"-map", "[a]", "-c:a", "pcm_s16le", "-ar", "48000", "-ac", "2", "-flags:a", "+bitexact",
		)
	}
	return append(args,
		"-map_metadata", "-1", "-map_chapters", "-1", "-fflags", "+bitexact",
		"-f", "matroska", "-y", output,
	), nil
}

func validateRenderInputOutput(
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
			if video != nil || !hasVideo || descriptor.Codec != "ffv1" || descriptor.Video == nil ||
				descriptor.Video.Width != width || descriptor.Video.Height != height ||
				descriptor.Video.PixelFormat != "yuv420p" || descriptor.Video.ColorRange != "tv" ||
				descriptor.Video.ColorSpace != "bt709" || descriptor.Video.ColorTransfer != "bt709" ||
				descriptor.Video.ColorPrimaries != "bt709" {
				return nil, nil, domain.ErrInvalidMediaFacts
			}
			video = &current
		case domain.MediaAudio:
			if audio != nil || !hasAudio || descriptor.Codec != "pcm_s16le" || descriptor.Audio == nil ||
				descriptor.Audio.SampleFormat != "s16" || descriptor.Audio.SampleRate != 48000 ||
				descriptor.Audio.Channels != 2 || descriptor.Audio.ChannelLayout != "stereo" {
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

func (executor *ExternalMediaRenderInputExecutor) inventoryPCMAudioSamples(
	ctx context.Context,
	directory string,
	source string,
	descriptor domain.SourceStreamDescriptor,
) (domain.UInt64, error) {
	scanContext, stop := context.WithCancel(ctx)
	defer stop()
	collector := &proxyAudioSampleCollector{
		maximum: application.MaximumRenderInputAudioSamples, stop: stop,
	}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err := lifecycle.Run(scanContext, lifecycle.ProcessSpec{
		Executable: executor.media.probe,
		Args: []string{
			"-v", "error", "-hide_banner", "-cpuflags", "0", "-protocol_whitelist", "file",
			"-threads", "1", "-select_streams", strconv.FormatUint(uint64(descriptor.Index), 10),
			"-show_frames", "-show_entries", "frame=sample_fmt,nb_samples", "-of", "csv=p=0", source,
		},
		Directory: directory, Env: executorEnvironment(), Stdout: collector, Stderr: stderr,
		Profile: executor.media.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	if finishErr := collector.Finish(); finishErr != nil {
		return 0, finishErr
	}
	if stderr.exceeded {
		return 0, fmt.Errorf("PCM sample diagnostics exceeded the limit")
	}
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("PCM sample inventory failed: %s", strings.TrimSpace(stderr.String()))
	}
	return domain.NewUInt64(collector.total)
}

func inspectRenderInputFile(path, relative, mime string) (application.RenderInputArtifactFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return application.RenderInputArtifactFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		uint64(info.Size()) > application.MaximumRenderInputArtifactSize {
		return application.RenderInputArtifactFile{}, domain.ErrInvalidMediaFacts
	}
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() {
		return application.RenderInputArtifactFile{}, domain.ErrInvalidMediaFacts
	}
	size, err := domain.NewUInt64(uint64(written))
	if err != nil {
		return application.RenderInputArtifactFile{}, err
	}
	return application.RenderInputArtifactFile{
		Path: relative, MimeType: mime, ByteSize: size,
		SHA256: domain.Digest("sha256:" + hex.EncodeToString(digest.Sum(nil))),
	}, nil
}

func writeRenderInputTimeMap(
	root string,
	sourcePTS []int64,
	materialPTS []int64,
) (application.RenderInputArtifactFile, error) {
	path := filepath.Join(root, "video-time-map.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return application.RenderInputArtifactFile{}, err
	}
	digest := sha256.New()
	err = application.WriteSourceProxyTimeMap(io.MultiWriter(file, digest), sourcePTS, materialPTS)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return application.RenderInputArtifactFile{}, err
	}
	if closeErr != nil {
		return application.RenderInputArtifactFile{}, closeErr
	}
	size, err := domain.NewUInt64(uint64(16 + len(sourcePTS)*16))
	if err != nil {
		return application.RenderInputArtifactFile{}, err
	}
	return application.RenderInputArtifactFile{
		Path: "video-time-map.bin", MimeType: "application/vnd.open-cut.pts-map",
		ByteSize: size, SHA256: domain.Digest("sha256:" + hex.EncodeToString(digest.Sum(nil))),
	}, nil
}

type renderInputWorkspace struct {
	root     string
	allowed  map[string]struct{}
	mu       sync.Mutex
	released bool
}

func newRenderInputWorkspace(root string, hasVideo bool) *renderInputWorkspace {
	allowed := map[string]struct{}{"render-input.mkv": {}}
	if hasVideo {
		allowed["video-time-map.bin"] = struct{}{}
	}
	return &renderInputWorkspace{root: root, allowed: allowed}
}

func (workspace *renderInputWorkspace) Open(relativePath string) (io.ReadCloser, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released {
		return nil, fmt.Errorf("render-input workspace was released")
	}
	if _, allowed := workspace.allowed[relativePath]; !allowed || filepath.Base(relativePath) != relativePath {
		return nil, fmt.Errorf("render-input workspace file is unavailable")
	}
	path := filepath.Join(workspace.root, relativePath)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("render-input workspace file is invalid")
	}
	return os.Open(path)
}

func (workspace *renderInputWorkspace) Release() error {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released {
		return nil
	}
	workspace.released = true
	return os.RemoveAll(workspace.root)
}

var _ application.PreparedMediaWorkspace = (*renderInputWorkspace)(nil)
