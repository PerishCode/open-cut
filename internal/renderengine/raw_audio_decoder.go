package renderengine

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
)

const (
	RawPCMDecodePolicyV1 = "render-material-s16le-track-ordinal-v2"
	rawPCMFrameBytes     = 4
	rawPCMChunkSamples   = 4_800
)

type StereoPCM16 struct {
	Left  int16
	Right int16
}

type RawPCMDecoderSpec struct {
	Executable  string
	Directory   string
	MediaPath   string
	InputCodec  string
	LastOrdinal uint64
	Profile     lifecycle.Profile
}

type RawPCMDecoder struct {
	reader          *io.PipeReader
	writer          *io.PipeWriter
	process         *lifecycle.Process
	diagnostic      *pipelineDiagnostic
	wait            chan error
	chunk           []byte
	chunkStart      uint64
	chunkSamples    uint64
	nextOrdinal     uint64
	expectedSamples uint64
	lastRequested   uint64
	hasRequested    bool
	finished        bool
	finishErr       error
}

func StartAudioDecodeRun(
	ctx context.Context,
	manifest ExecutionManifest,
	run AudioDecodeRun,
	attemptRoot string,
	profile lifecycle.Profile,
) (*RawPCMDecoder, error) {
	if manifest.Validate() != nil || !cleanAbsoluteDirectory(attemptRoot) ||
		validateAudioDecodeRun(manifest.Plan, run) != nil || int(run.InputIndex) >= len(manifest.Inputs) {
		return nil, fmt.Errorf("audio decode run material is invalid")
	}
	input := manifest.Plan.Inputs[run.InputIndex]
	material := manifest.Inputs[run.InputIndex]
	inputCodec, err := rawPCMInputCodec(input.Profile)
	if err != nil {
		return nil, err
	}
	if input.ArtifactID != run.InputArtifactID || input.Audio == nil ||
		input.Audio.SourceStreamID != run.SourceStreamID || run.LastOrdinal >= input.Audio.DecodedSampleCount.Value() ||
		material.ArtifactID != run.InputArtifactID || manifest.Budget.AudioChunkSamples != rawPCMChunkSamples {
		return nil, fmt.Errorf("audio decode run input is invalid")
	}
	return StartRawPCMDecoder(ctx, RawPCMDecoderSpec{
		Executable: manifest.Tools[0].Path, Directory: attemptRoot,
		MediaPath:  filepath.Join(material.ArtifactRoot, material.MediaRelativePath),
		InputCodec: inputCodec, LastOrdinal: run.LastOrdinal, Profile: profile,
	})
}

// StartRawPCMDecoder starts one ordinal-zero, profile-pinned decoder run. It
// retains at most one 4,800-sample interleaved stereo S16LE chunk. The child is
// intentionally terminated after the declared last ordinal because FFmpeg's
// audio frame limit counts codec frames rather than product samples.
func StartRawPCMDecoder(ctx context.Context, spec RawPCMDecoderSpec) (*RawPCMDecoder, error) {
	if spec.LastOrdinal >= MaximumDecodedAudioSamples || !cleanAbsoluteRegular(spec.Executable) ||
		!cleanAbsoluteDirectory(spec.Directory) || !cleanAbsoluteRegular(spec.MediaPath) ||
		(spec.InputCodec != "libopus" && spec.InputCodec != "pcm_s16le") {
		return nil, fmt.Errorf("raw PCM decoder configuration is invalid")
	}
	expected := spec.LastOrdinal + 1
	if _, overflow := multiplyUint64(expected, rawPCMFrameBytes); overflow {
		return nil, ResourceLimitError{Subject: "decoded-audio-bytes"}
	}
	reader, writer := io.Pipe()
	diagnostic := &pipelineDiagnostic{limit: maximumPipelineDiagnostic}
	processSpec := rawPCMDecodeProcessSpec(spec)
	processSpec.Stdout = writer
	processSpec.Stderr = diagnostic
	process, err := lifecycle.Start(ctx, processSpec)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, err
	}
	decoder := &RawPCMDecoder{
		reader: reader, writer: writer, process: process, diagnostic: diagnostic,
		wait: make(chan error, 1), chunk: make([]byte, rawPCMChunkSamples*rawPCMFrameBytes),
		expectedSamples: expected,
	}
	go func() {
		waitErr := process.Wait()
		_ = writer.CloseWithError(waitErr)
		decoder.wait <- waitErr
	}()
	return decoder, nil
}

func (decoder *RawPCMDecoder) ReadTo(ordinal uint64) (StereoPCM16, error) {
	if decoder == nil || decoder.finished || ordinal >= decoder.expectedSamples {
		return StereoPCM16{}, fmt.Errorf("raw PCM decoder ordinal is invalid")
	}
	if decoder.hasRequested && ordinal < decoder.lastRequested {
		return StereoPCM16{}, fmt.Errorf("raw PCM decoder run is not monotonic")
	}
	for decoder.chunkSamples == 0 || ordinal >= decoder.chunkStart+decoder.chunkSamples {
		if decoder.nextOrdinal >= decoder.expectedSamples {
			return StereoPCM16{}, fmt.Errorf("raw PCM decoder traversal is incomplete")
		}
		remaining := decoder.expectedSamples - decoder.nextOrdinal
		samples := uint64(rawPCMChunkSamples)
		if remaining < samples {
			samples = remaining
		}
		bytes := samples * rawPCMFrameBytes
		if _, err := io.ReadFull(decoder.reader, decoder.chunk[:bytes]); err != nil {
			return StereoPCM16{}, decoder.childFailure("read raw PCM samples", err)
		}
		decoder.chunkStart = decoder.nextOrdinal
		decoder.chunkSamples = samples
		decoder.nextOrdinal += samples
	}
	offset := (ordinal - decoder.chunkStart) * rawPCMFrameBytes
	left := binary.LittleEndian.Uint16(decoder.chunk[offset : offset+2])
	right := binary.LittleEndian.Uint16(decoder.chunk[offset+2 : offset+4])
	decoder.lastRequested, decoder.hasRequested = ordinal, true
	return StereoPCM16{Left: int16(left), Right: int16(right)}, nil
}

func (decoder *RawPCMDecoder) Finish() error {
	if decoder == nil {
		return fmt.Errorf("raw PCM decoder is unavailable")
	}
	if decoder.finished {
		return decoder.finishErr
	}
	if !decoder.hasRequested || decoder.lastRequested+1 != decoder.expectedSamples ||
		decoder.nextOrdinal != decoder.expectedSamples {
		decoder.finishErr = fmt.Errorf("raw PCM decoder traversal is incomplete")
		_ = decoder.abort()
		decoder.finished = true
		return decoder.finishErr
	}
	waitErr, completed := decoder.completedChild()
	if !completed {
		// Stop the stdout bridge before waiting for the contained process. The
		// lifecycle command owns an io.Copy goroutine when stdout is an io.Pipe;
		// leaving its reader open can keep Wait blocked after the child is killed.
		_ = decoder.reader.Close()
		_ = decoder.process.Kill()
		// Once the exact declared final sample has been observed, both a
		// successful signal exit and a child that wins the kill race are the
		// intended tail condition. Early EOF was already rejected by ReadTo.
		// Waiting still proves that the contained child is gone before return.
		<-decoder.wait
	} else if waitErr != nil {
		decoder.finishErr = decoder.childFailure("finish raw PCM decode", waitErr)
	}
	_ = decoder.reader.Close()
	_ = decoder.writer.Close()
	decoder.finished = true
	if decoder.diagnostic.exceeded {
		decoder.finishErr = fmt.Errorf("finish raw PCM decode: media child diagnostics exceeded their bound")
	}
	return decoder.finishErr
}

func (decoder *RawPCMDecoder) completedChild() (error, bool) {
	select {
	case err := <-decoder.wait:
		return err, true
	default:
		return nil, false
	}
}

func (decoder *RawPCMDecoder) Close() error {
	if decoder == nil || decoder.finished {
		return nil
	}
	err := decoder.abort()
	decoder.finished = true
	return err
}

func (decoder *RawPCMDecoder) abort() error {
	_ = decoder.reader.Close()
	_ = decoder.writer.Close()
	killErr := decoder.process.Kill()
	<-decoder.wait
	return killErr
}

func (decoder *RawPCMDecoder) childFailure(operation string, cause error) error {
	if decoder != nil && decoder.diagnostic != nil {
		if decoder.diagnostic.exceeded {
			return fmt.Errorf("%s: media child diagnostics exceeded their bound", operation)
		}
		return fmt.Errorf("%s (%w): %s", operation, cause, decoder.diagnostic.String())
	}
	return fmt.Errorf("%s: %w", operation, cause)
}

func rawPCMDecodeProcessSpec(spec RawPCMDecoderSpec) lifecycle.ProcessSpec {
	return lifecycle.ProcessSpec{
		Executable: spec.Executable,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
			"-protocol_whitelist", "file,pipe,fd", "-c:a", spec.InputCodec,
			"-request_sample_fmt", "s16", "-threads", "1", "-i", spec.MediaPath,
			"-map", "0:a:0", "-vn", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1",
			"-c:a", "pcm_s16le", "-flags:a", "+bitexact", "-f", "s16le", "pipe:1",
		},
		Directory: spec.Directory, Stdout: io.Discard, Stderr: io.Discard,
		Profile: spec.Profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	}
}

func rawPCMInputCodec(profile string) (string, error) {
	switch profile {
	case application.SourceProxyProfile:
		return "libopus", nil
	case application.RenderInputProfile:
		return "pcm_s16le", nil
	default:
		return "", fmt.Errorf("raw PCM decoder input profile is invalid")
	}
}
