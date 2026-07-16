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

func TestRawPCMDecodeSpecPinsProfileCodecAndS16WithoutAudioFilters(t *testing.T) {
	for _, inputCodec := range []string{"libopus", "pcm_s16le"} {
		spec := rawPCMDecodeProcessSpec(RawPCMDecoderSpec{
			Executable: "/tool/ffmpeg", Directory: "/attempt", MediaPath: "/attempt/material",
			InputCodec: inputCodec, LastOrdinal: 9, Profile: lifecycle.ProfileHarness,
		})
		for _, sequence := range [][]string{
			{"-cpuflags", "0"}, {"-c:a", inputCodec}, {"-request_sample_fmt", "s16"},
			{"-threads", "1"}, {"-c:a", "pcm_s16le"}, {"-f", "s16le"},
		} {
			if !containsArgumentSequence(spec.Args, sequence) {
				t.Fatalf("codec=%q missing sequence %q in %q", inputCodec, sequence, spec.Args)
			}
		}
		for _, forbidden := range []string{"-af", "-filter_complex", "-ar", "-ac", "-ss"} {
			if slices.Contains(spec.Args, forbidden) {
				t.Fatalf("codec=%q forbidden audio semantic option %q in %q", inputCodec, forbidden, spec.Args)
			}
		}
	}
}

func TestPinnedRawPCMDecoderIsBoundedAndMonotonic(t *testing.T) {
	mediaRoot := os.Getenv("OPEN_CUT_MEDIA_TOOL_ROOT")
	if mediaRoot == "" {
		t.Skip("OPEN_CUT_MEDIA_TOOL_ROOT is not set")
	}
	ffmpeg := filepath.Join(mediaRoot, target.Host().ExecutableName("ffmpeg"))
	ffmpeg, err := filepath.EvalSymlinks(ffmpeg)
	if err != nil || !cleanAbsoluteRegular(ffmpeg) {
		t.Skip("pinned FFmpeg is unavailable")
	}
	plan := captionRenderPlan(t)
	fontRoot := normalizeMaterialPath(t.TempDir())
	manifest, _, err := CompileExecutionManifest(
		plan,
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
	audioPath := filepath.Join(attemptRoot, audioIntermediateFilename)
	audioBytes, err := expectedAudioStreamBytes(manifest)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, int(audioBytes))
	written, err := RunBoundedProcessStream(
		context.Background(), rawAudioProcessSpec(ffmpeg, attemptRoot, audioPath, manifest, lifecycle.ProfileHarness),
		audioBytes,
		func(_ context.Context, destination io.Writer) error {
			_, writeErr := destination.Write(pcm)
			return writeErr
		},
	)
	if err != nil || written != audioBytes {
		t.Fatalf("encode written=%d err=%v", written, err)
	}
	decoder, err := StartRawPCMDecoder(context.Background(), RawPCMDecoderSpec{
		Executable: ffmpeg, Directory: attemptRoot, MediaPath: audioPath,
		InputCodec: "libopus", LastOrdinal: rawPCMChunkSamples - 1, Profile: lifecycle.ProfileHarness,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = decoder.Close() })
	first, err := decoder.ReadTo(0)
	if err != nil || first != (StereoPCM16{}) {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	last, err := decoder.ReadTo(rawPCMChunkSamples - 1)
	if err != nil || last != (StereoPCM16{}) {
		t.Fatalf("last=%+v err=%v", last, err)
	}
	if _, err := decoder.ReadTo(1); err == nil {
		t.Fatal("backward raw PCM decode was accepted")
	}
	if err := decoder.Finish(); err != nil {
		t.Fatal(err)
	}
}
