package renderengine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
)

const (
	videoIntermediateFilename = "video-only.webm"
	audioIntermediateFilename = "audio-only.webm"
	maximumPipelineDiagnostic = 64 << 10
)

type RawAVProducers struct {
	Video StreamProducer
	Audio StreamProducer
}

// RunRawAVPipeline consumes the evaluator's exact CFR frames and PCM samples.
// It keeps raw bytes on backpressured pipes, stores only compressed
// intermediates, and uses a final bitexact stream-copy mux.
func RunRawAVPipeline(
	ctx context.Context,
	manifest ExecutionManifest,
	attemptRoot string,
	profile lifecycle.Profile,
	producers RawAVProducers,
) error {
	if manifest.Validate() != nil || !cleanAbsoluteDirectory(attemptRoot) || producers.Video == nil || producers.Audio == nil {
		return fmt.Errorf("raw A/V pipeline configuration is invalid")
	}
	ffmpeg := manifest.Tools[0].Path
	videoPath := filepath.Join(attemptRoot, videoIntermediateFilename)
	audioPath := filepath.Join(attemptRoot, audioIntermediateFilename)
	outputPath := filepath.Join(attemptRoot, manifest.Output.RelativePath)
	for _, path := range []string{videoPath, audioPath, outputPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("raw A/V pipeline output already exists")
		}
	}
	defer os.Remove(videoPath)
	defer os.Remove(audioPath)

	videoBytes, err := expectedVideoStreamBytes(manifest)
	if err != nil {
		return err
	}
	videoSpec := rawVideoProcessSpec(ffmpeg, attemptRoot, videoPath, manifest, profile)
	written, err := RunBoundedProcessStream(
		ctx, videoSpec, videoBytes,
		exactStreamProducer(videoBytes, producers.Video),
	)
	err = pipelineStreamError(err, videoSpec)
	if err != nil || written != videoBytes {
		return fmt.Errorf("encode bounded raw video: %w", firstPipelineError(err, io.ErrUnexpectedEOF))
	}
	videoSize, err := verifyPipelineOutput(videoPath, manifest.Budget.IntermediateByteLimit)
	if err != nil {
		return err
	}

	audioBytes, err := expectedAudioStreamBytes(manifest)
	if err != nil {
		return err
	}
	audioSpec := rawAudioProcessSpec(ffmpeg, attemptRoot, audioPath, manifest, profile)
	written, err = RunBoundedProcessStream(
		ctx, audioSpec, audioBytes,
		exactStreamProducer(audioBytes, producers.Audio),
	)
	err = pipelineStreamError(err, audioSpec)
	if err != nil || written != audioBytes {
		return fmt.Errorf("encode bounded raw audio: %w", firstPipelineError(err, io.ErrUnexpectedEOF))
	}
	audioSize, err := verifyPipelineOutput(audioPath, manifest.Budget.IntermediateByteLimit)
	if err != nil {
		return err
	}
	if videoSize > manifest.Budget.AttemptByteLimit-audioSize {
		return ResourceLimitError{Subject: "attempt-bytes"}
	}

	if err := runPipelineProcess(ctx, rawMuxProcessSpec(
		ffmpeg, attemptRoot, videoPath, audioPath, outputPath, manifest, profile,
	)); err != nil {
		return fmt.Errorf("mux bounded raw A/V output: %w", err)
	}
	outputSize, err := verifyPipelineOutput(outputPath, manifest.Budget.OutputByteLimit)
	if err != nil {
		return err
	}
	if outputSize > manifest.Budget.AttemptByteLimit-videoSize-audioSize {
		return ResourceLimitError{Subject: "attempt-bytes"}
	}
	return nil
}

func rawVideoProcessSpec(
	executable, directory, output string,
	manifest ExecutionManifest,
	profile lifecycle.Profile,
) lifecycle.ProcessSpec {
	policy := manifest.Plan.Output
	rate := fmt.Sprintf("%d/%d", policy.FrameRate.Value.Value(), policy.FrameRate.Scale)
	size := fmt.Sprintf("%dx%d", policy.CanvasWidth, policy.CanvasHeight)
	return pipelineProcessSpec(executable, directory, profile, []string{
		"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
		"-f", "rawvideo", "-pixel_format", "yuv420p", "-video_size", size, "-framerate", rate,
		"-i", "pipe:0", "-map", "0:v:0", "-an", "-map_metadata", "-1", "-map_chapters", "-1",
		"-c:v", policy.Video.Encoder, "-pix_fmt", policy.Video.PixelFormat,
		"-deadline", policy.Video.Deadline, "-cpu-used", strconv.Itoa(int(policy.Video.CPUUsed)),
		"-threads", strconv.Itoa(int(policy.Video.ThreadCount)),
		"-row-mt", "0", "-tile-columns", "0", "-frame-parallel", "0",
		"-auto-alt-ref", "0", "-b:v", "0", "-crf", strconv.Itoa(int(policy.Video.CRF)), "-g", "10000000",
		"-force_key_frames", "expr:gte(t,n_forced*2)", "-fps_mode", "cfr", "-r", rate,
		"-color_range", "tv", "-colorspace", "bt709", "-color_trc", "bt709",
		"-color_primaries", "bt709", "-chroma_sample_location", "left",
		"-flags:v", "+bitexact", "-fflags", "+bitexact",
		"-fs", strconv.FormatUint(manifest.Budget.IntermediateByteLimit, 10), "-f", "webm", output,
	})
}

func rawAudioProcessSpec(
	executable, directory, output string,
	manifest ExecutionManifest,
	profile lifecycle.Profile,
) lifecycle.ProcessSpec {
	policy := manifest.Plan.Output.Audio
	return pipelineProcessSpec(executable, directory, profile, []string{
		"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
		"-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0",
		"-map", "0:a:0", "-vn", "-map_metadata", "-1", "-map_chapters", "-1",
		"-c:a", policy.Encoder, "-ar", strconv.FormatUint(uint64(policy.SampleRate), 10),
		"-ac", "2", "-b:a", strconv.FormatUint(uint64(policy.BitRate), 10), "-vbr", "off",
		"-compression_level", strconv.Itoa(int(policy.CompressionLevel)),
		"-frame_duration", strconv.Itoa(int(policy.FrameDurationMS)), "-application", "audio",
		"-flags:a", "+bitexact", "-fflags", "+bitexact",
		"-fs", strconv.FormatUint(manifest.Budget.IntermediateByteLimit, 10), "-f", "webm", output,
	})
}

func rawMuxProcessSpec(
	executable, directory, video, audio, output string,
	manifest ExecutionManifest,
	profile lifecycle.Profile,
) lifecycle.ProcessSpec {
	return pipelineProcessSpec(executable, directory, profile, []string{
		"-v", "error", "-hide_banner", "-nostdin", "-protocol_whitelist", "file,pipe,fd",
		"-i", video, "-i", audio, "-map", "0:v:0", "-map", "1:a:0",
		"-map_metadata", "-1", "-map_chapters", "-1", "-c", "copy", "-fflags", "+bitexact",
		"-color_range", "tv", "-colorspace", "bt709", "-color_trc", "bt709",
		"-color_primaries", "bt709", "-chroma_sample_location", "left",
		"-fs", strconv.FormatUint(manifest.Budget.OutputByteLimit, 10), "-f", "webm", output,
	})
}

func pipelineProcessSpec(
	executable, directory string,
	profile lifecycle.Profile,
	arguments []string,
) lifecycle.ProcessSpec {
	return lifecycle.ProcessSpec{
		Executable: executable, Args: arguments, Directory: directory,
		Stdout: io.Discard, Stderr: &pipelineDiagnostic{limit: maximumPipelineDiagnostic},
		Profile: profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	}
}

func runPipelineProcess(ctx context.Context, spec lifecycle.ProcessSpec) error {
	diagnostic, _ := spec.Stderr.(*pipelineDiagnostic)
	err := lifecycle.Run(ctx, spec)
	if err != nil || diagnostic == nil || diagnostic.exceeded {
		message := ""
		if diagnostic != nil {
			message = diagnostic.String()
		}
		return fmt.Errorf("media child failed (%v): %s", err, message)
	}
	return nil
}

func pipelineStreamError(err error, spec lifecycle.ProcessSpec) error {
	diagnostic, _ := spec.Stderr.(*pipelineDiagnostic)
	if diagnostic == nil {
		return err
	}
	if diagnostic.exceeded {
		return fmt.Errorf("media child diagnostics exceeded their bound")
	}
	if err != nil {
		return fmt.Errorf("media child failed (%w): %s", err, diagnostic.String())
	}
	return nil
}

func exactStreamProducer(expected uint64, producer StreamProducer) StreamProducer {
	return func(ctx context.Context, destination io.Writer) error {
		counter := &countingWriter{destination: destination}
		if err := producer(ctx, counter); err != nil {
			return err
		}
		if counter.written != expected {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
}

type countingWriter struct {
	destination io.Writer
	written     uint64
}

func (writer *countingWriter) Write(value []byte) (int, error) {
	written, err := writer.destination.Write(value)
	writer.written += uint64(written)
	return written, err
}

func expectedVideoStreamBytes(manifest ExecutionManifest) (uint64, error) {
	pixels, overflow := multiplyUint64(
		uint64(manifest.Plan.Output.CanvasWidth), uint64(manifest.Plan.Output.CanvasHeight),
	)
	if overflow || pixels%2 != 0 {
		return 0, fmt.Errorf("raw video shape is invalid")
	}
	frameBytes := pixels + pixels/2
	value, overflow := multiplyUint64(frameBytes, manifest.Plan.Output.VideoFrameCount.Value())
	if overflow || value == 0 {
		return 0, ResourceLimitError{Subject: "raw-video-bytes"}
	}
	return value, nil
}

func expectedAudioStreamBytes(manifest ExecutionManifest) (uint64, error) {
	value, overflow := multiplyUint64(manifest.Plan.Output.AudioSampleCount.Value(), 4)
	if overflow || value == 0 {
		return 0, ResourceLimitError{Subject: "raw-audio-bytes"}
	}
	return value, nil
}

func verifyPipelineOutput(path string, limit uint64) (uint64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 {
		return 0, fmt.Errorf("media pipeline output is invalid")
	}
	size := uint64(info.Size())
	if size > limit {
		return 0, ResourceLimitError{Subject: "encoded-file-bytes"}
	}
	return size, nil
}

func firstPipelineError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

type pipelineDiagnostic struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *pipelineDiagnostic) Write(value []byte) (int, error) {
	if buffer.exceeded {
		return len(value), nil
	}
	remaining := buffer.limit - buffer.Len()
	if len(value) > remaining {
		buffer.exceeded = true
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(value[:remaining])
		}
		return len(value), nil
	}
	return buffer.Buffer.Write(value)
}
