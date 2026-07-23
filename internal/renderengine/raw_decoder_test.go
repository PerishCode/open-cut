package renderengine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestRawYUVDecodeSpecIsOrdinalZeroAndCPUFixed(t *testing.T) {
	spec := rawYUVDecodeProcessSpec(RawYUVDecoderSpec{
		Executable: "/tool/ffmpeg", Directory: "/attempt", MediaPath: "/attempt/proxy.webm",
		Width: 16, Height: 16, LastOrdinal: 9, Profile: lifecycle.ProfileHarness,
	})
	for _, sequence := range [][]string{
		{"-cpuflags", "0"}, {"-threads", "1"}, {"-fps_mode", "passthrough"},
		{"-frames:v", "10"}, {"-pix_fmt", "yuv420p"}, {"-f", "rawvideo"},
	} {
		if !containsArgumentSequence(spec.Args, sequence) {
			t.Fatalf("missing sequence %q in %q", sequence, spec.Args)
		}
	}
	if frameBytes, err := rawYUVFrameBytes(16, 16); err != nil || frameBytes != 384 {
		t.Fatalf("frameBytes=%d err=%v", frameBytes, err)
	}
	if _, err := rawYUVFrameBytes(15, 16); err == nil {
		t.Fatal("odd YUV width was accepted")
	}
}

func TestPinnedRawYUVDecoderIsExactAndMonotonic(t *testing.T) {
	mediaRoot := os.Getenv("OPEN_CUT_MEDIA_TOOL_ROOT")
	if mediaRoot == "" {
		t.Skip("OPEN_CUT_MEDIA_TOOL_ROOT is not set")
	}
	ffmpeg := filepath.Join(mediaRoot, target.Host().ExecutableName("ffmpeg"))
	ffmpeg, err := filepath.EvalSymlinks(ffmpeg)
	if err != nil || !cleanAbsoluteRegular(ffmpeg) {
		t.Skip("pinned FFmpeg is unavailable")
	}
	format := domain.DefaultSequenceFormat()
	format.CanvasWidth, format.CanvasHeight = 16, 16
	plan := captionRenderPlanWithFormat(t, format)
	fontRoot := normalizeMaterialPath(t.TempDir())
	manifest, _, err := CompileExecutionManifest(
		plan.Plan,
		application.SequencePreviewRendererIdentity{Version: "fixture-renderer-v1", Target: target.Host().String()},
		ExecutionClosure{
			SHA256: domain.Digest("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
			Tools: map[string]ExecutionToolPin{
				"ffmpeg": {
					Path:   ffmpeg,
					SHA256: domain.Digest("sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
				},
			},
		},
		MaterialPaths{ArtifactRoots: map[string]string{}, Resources: map[string]string{
			"font:noto-caption-bundle": fontRoot,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	attemptRoot := normalizeMaterialPath(t.TempDir())
	videoPath := filepath.Join(attemptRoot, videoIntermediateFilename)
	videoBytes, err := expectedVideoStreamBytes(manifest)
	if err != nil {
		t.Fatal(err)
	}
	frameBytes, _ := rawYUVFrameBytes(16, 16)
	frame := make([]byte, frameBytes)
	for index := 0; index < 16*16; index++ {
		frame[index] = 16
	}
	for index := 16 * 16; index < len(frame); index++ {
		frame[index] = 128
	}
	written, err := RunBoundedProcessStream(
		context.Background(), rawVideoProcessSpec(ffmpeg, attemptRoot, videoPath, manifest, lifecycle.ProfileHarness),
		videoBytes,
		func(_ context.Context, destination io.Writer) error {
			for count := uint64(0); count < manifest.Plan.Output.VideoFrameCount.Value(); count++ {
				if _, writeErr := destination.Write(frame); writeErr != nil {
					return writeErr
				}
			}
			return nil
		},
	)
	if err != nil || written != videoBytes {
		t.Fatalf("encode written=%d err=%v", written, err)
	}
	decoder, err := StartRawYUVDecoder(context.Background(), RawYUVDecoderSpec{
		Executable: ffmpeg, Directory: attemptRoot, MediaPath: videoPath,
		Width: 16, Height: 16, LastOrdinal: 2, Profile: lifecycle.ProfileHarness,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = decoder.Close() })
	first, err := decoder.ReadTo(0)
	if err != nil || len(first) != frameBytes {
		t.Fatalf("first bytes=%d err=%v", len(first), err)
	}
	first = slices.Clone(first)
	last, err := decoder.ReadTo(2)
	if err != nil || !slices.Equal(first, last) {
		t.Fatalf("last bytes=%d err=%v", len(last), err)
	}
	if _, err := decoder.ReadTo(1); err == nil {
		t.Fatal("backward raw decode was accepted")
	}
	if err := decoder.Finish(); err != nil {
		t.Fatal(err)
	}
}

func containsArgumentSequence(arguments, sequence []string) bool {
	for index := 0; index+len(sequence) <= len(arguments); index++ {
		if slices.Equal(arguments[index:index+len(sequence)], sequence) {
			return true
		}
	}
	return false
}
