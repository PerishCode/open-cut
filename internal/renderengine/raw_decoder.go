package renderengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/domain"
)

const RawYUVDecodePolicyV1 = "render-material-yuv420p-presentation-ordinal-v2"

type RawYUVDecoderSpec struct {
	Executable  string
	Directory   string
	MediaPath   string
	Width       uint32
	Height      uint32
	LastOrdinal uint64
	Profile     lifecycle.Profile
}

type RawYUVDecoder struct {
	reader         *io.PipeReader
	writer         *io.PipeWriter
	process        *lifecycle.Process
	diagnostic     *pipelineDiagnostic
	wait           chan error
	frame          []byte
	expectedFrames uint64
	nextOrdinal    uint64
	currentOrdinal uint64
	hasCurrent     bool
	finished       bool
	finishErr      error
}

func StartVideoDecodeRun(
	ctx context.Context,
	manifest ExecutionManifest,
	run VideoDecodeRun,
	attemptRoot string,
	profile lifecycle.Profile,
) (*RawYUVDecoder, error) {
	if manifest.Validate() != nil || !cleanAbsoluteDirectory(attemptRoot) ||
		validateVideoDecodeRun(manifest.Plan, run) != nil || int(run.InputIndex) >= len(manifest.Inputs) {
		return nil, fmt.Errorf("video decode run material is invalid")
	}
	input := manifest.Plan.Inputs[run.InputIndex]
	material := manifest.Inputs[run.InputIndex]
	if input.ArtifactID != run.InputArtifactID || input.Video == nil ||
		input.Video.SourceStreamID != run.SourceStreamID || material.ArtifactID != run.InputArtifactID {
		return nil, fmt.Errorf("video decode run input is invalid")
	}
	mediaPath := filepath.Join(material.ArtifactRoot, material.MediaRelativePath)
	return StartRawYUVDecoder(ctx, RawYUVDecoderSpec{
		Executable: manifest.Tools[0].Path, Directory: attemptRoot, MediaPath: mediaPath,
		Width: input.Video.Width, Height: input.Video.Height,
		LastOrdinal: run.LastOrdinal, Profile: profile,
	})
}

// StartRawYUVDecoder starts one ordinal-zero decoder run. Returned frame bytes
// are valid only until the next ReadTo call; the decoder never retains more
// than one complete source frame.
func StartRawYUVDecoder(ctx context.Context, spec RawYUVDecoderSpec) (*RawYUVDecoder, error) {
	frameBytes, err := rawYUVFrameBytes(spec.Width, spec.Height)
	if err != nil || spec.LastOrdinal >= MaximumDecodedVideoFrames ||
		!cleanAbsoluteRegular(spec.Executable) || !cleanAbsoluteDirectory(spec.Directory) ||
		!cleanAbsoluteRegular(spec.MediaPath) {
		return nil, fmt.Errorf("raw YUV decoder configuration is invalid")
	}
	expectedFrames := spec.LastOrdinal + 1
	if _, overflow := multiplyUint64(uint64(frameBytes), expectedFrames); overflow {
		return nil, ResourceLimitError{Subject: "decoded-video-bytes"}
	}
	reader, writer := io.Pipe()
	diagnostic := &pipelineDiagnostic{limit: maximumPipelineDiagnostic}
	processSpec := rawYUVDecodeProcessSpec(spec)
	processSpec.Stdout = writer
	processSpec.Stderr = diagnostic
	process, err := lifecycle.Start(ctx, processSpec)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, err
	}
	decoder := &RawYUVDecoder{
		reader: reader, writer: writer, process: process, diagnostic: diagnostic,
		wait: make(chan error, 1), frame: make([]byte, frameBytes), expectedFrames: expectedFrames,
	}
	go func() {
		waitErr := process.Wait()
		_ = writer.CloseWithError(waitErr)
		decoder.wait <- waitErr
	}()
	return decoder, nil
}

func (decoder *RawYUVDecoder) ReadTo(ordinal uint64) ([]byte, error) {
	if decoder == nil || decoder.finished || decoder.reader == nil || ordinal >= decoder.expectedFrames {
		return nil, fmt.Errorf("raw YUV decoder ordinal is invalid")
	}
	if decoder.hasCurrent && ordinal < decoder.currentOrdinal {
		return nil, fmt.Errorf("raw YUV decoder run is not monotonic")
	}
	if decoder.hasCurrent && ordinal == decoder.currentOrdinal {
		return decoder.frame, nil
	}
	for decoder.nextOrdinal <= ordinal {
		if _, err := io.ReadFull(decoder.reader, decoder.frame); err != nil {
			return nil, decoder.childFailure("read raw YUV frame", err)
		}
		decoder.currentOrdinal = decoder.nextOrdinal
		decoder.nextOrdinal++
		decoder.hasCurrent = true
	}
	return decoder.frame, nil
}

// Finish proves that the caller traversed the declared final ordinal and that
// the child emitted neither a partial frame nor trailing bytes.
func (decoder *RawYUVDecoder) Finish() error {
	if decoder == nil {
		return fmt.Errorf("raw YUV decoder is unavailable")
	}
	if decoder.finished {
		return decoder.finishErr
	}
	if !decoder.hasCurrent || decoder.nextOrdinal != decoder.expectedFrames {
		decoder.finishErr = fmt.Errorf("raw YUV decoder traversal is incomplete")
		_ = decoder.abort()
		decoder.finished = true
		return decoder.finishErr
	}
	var trailing [1]byte
	count, readErr := decoder.reader.Read(trailing[:])
	waitErr := <-decoder.wait
	_ = decoder.reader.Close()
	decoder.finished = true
	if count != 0 || !errors.Is(readErr, io.EOF) || waitErr != nil || decoder.diagnostic.exceeded {
		decoder.finishErr = decoder.childFailure("finish raw YUV decode", firstPipelineError(waitErr, readErr))
	}
	return decoder.finishErr
}

func (decoder *RawYUVDecoder) Close() error {
	if decoder == nil || decoder.finished {
		return nil
	}
	err := decoder.abort()
	decoder.finished = true
	return err
}

func (decoder *RawYUVDecoder) abort() error {
	_ = decoder.reader.Close()
	_ = decoder.writer.Close()
	killErr := decoder.process.Kill()
	<-decoder.wait
	return killErr
}

func (decoder *RawYUVDecoder) childFailure(operation string, cause error) error {
	diagnostic := ""
	if decoder != nil && decoder.diagnostic != nil {
		if decoder.diagnostic.exceeded {
			return fmt.Errorf("%s: media child diagnostics exceeded their bound", operation)
		}
		diagnostic = decoder.diagnostic.String()
	}
	return fmt.Errorf("%s (%w): %s", operation, cause, diagnostic)
}

func rawYUVDecodeProcessSpec(spec RawYUVDecoderSpec) lifecycle.ProcessSpec {
	return lifecycle.ProcessSpec{
		Executable: spec.Executable,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
			"-protocol_whitelist", "file,pipe,fd", "-threads", "1", "-noautorotate", "-i", spec.MediaPath,
			"-map", "0:v:0", "-an", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1",
			"-fps_mode", "passthrough", "-frames:v", strconv.FormatUint(spec.LastOrdinal+1, 10),
			"-c:v", "rawvideo", "-pix_fmt", "yuv420p", "-flags:v", "+bitexact", "-f", "rawvideo", "pipe:1",
		},
		Directory: spec.Directory, Stdout: io.Discard, Stderr: io.Discard,
		Profile: spec.Profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	}
}

func rawYUVFrameBytes(width, height uint32) (int, error) {
	if width < 2 || height < 2 || width > 16_384 || height > 16_384 || width%2 != 0 || height%2 != 0 {
		return 0, fmt.Errorf("raw YUV frame shape is invalid")
	}
	pixels, overflow := multiplyUint64(uint64(width), uint64(height))
	if overflow || pixels > uint64(^uint(0)>>1)*2/3 {
		return 0, ResourceLimitError{Subject: "raw-yuv-frame-bytes"}
	}
	return int(pixels + pixels/2), nil
}

func validateVideoDecodeRun(plan domain.RenderPlanPayload, run VideoDecodeRun) error {
	if int(run.InputIndex) >= len(plan.Inputs) || len(run.Requests) == 0 ||
		run.TraversalFrames != run.LastOrdinal+1 || run.LastOrdinal >= MaximumDecodedVideoFrames {
		return fmt.Errorf("video decode run head is invalid")
	}
	input := plan.Inputs[run.InputIndex]
	if input.ArtifactID != run.InputArtifactID || input.Video == nil ||
		input.Video.SourceStreamID != run.SourceStreamID {
		return fmt.Errorf("video decode run source is invalid")
	}
	var previousEnd, previousLast uint64
	for index, request := range run.Requests {
		if int(request.InstructionIndex) >= len(plan.Video) || request.FirstOutputFrame >= request.EndOutputFrame ||
			request.EndOutputFrame > plan.Output.VideoFrameCount.Value() ||
			request.FirstOrdinal > request.LastOrdinal || request.LastOrdinal > run.LastOrdinal ||
			(index > 0 && (previousEnd > request.FirstOutputFrame || previousLast > request.FirstOrdinal)) {
			return fmt.Errorf("video decode request is invalid")
		}
		instruction := plan.Video[request.InstructionIndex]
		if instruction.InputArtifactID != run.InputArtifactID || instruction.SourceStreamID != run.SourceStreamID {
			return fmt.Errorf("video decode request source is invalid")
		}
		first, after, err := outputFrameRange(
			instruction.TimelineRange, plan.Output.FrameRate, plan.Output.VideoFrameCount.Value(),
		)
		if err != nil || first != request.FirstOutputFrame || after != request.EndOutputFrame {
			return fmt.Errorf("video decode request grid is invalid")
		}
		previousEnd, previousLast = request.EndOutputFrame, request.LastOrdinal
	}
	if previousLast != run.LastOrdinal {
		return fmt.Errorf("video decode run tail is invalid")
	}
	return nil
}
