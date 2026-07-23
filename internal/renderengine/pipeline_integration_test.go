package renderengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestPinnedRawAVPipelineIsByteStable(t *testing.T) {
	mediaRoot := os.Getenv("OPEN_CUT_MEDIA_TOOL_ROOT")
	if mediaRoot == "" {
		t.Skip("OPEN_CUT_MEDIA_TOOL_ROOT is not set")
	}
	ffmpeg := filepath.Join(mediaRoot, target.Host().ExecutableName("ffmpeg"))
	if info, err := os.Stat(ffmpeg); err != nil || !info.Mode().IsRegular() {
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
			SHA256: domain.Digest("sha256:" + strings.Repeat("c", 64)),
			Tools: map[string]ExecutionToolPin{
				"ffmpeg": {Path: ffmpeg, SHA256: domain.Digest("sha256:" + strings.Repeat("d", 64))},
			},
		},
		MaterialPaths{
			ArtifactRoots: map[string]string{},
			Resources:     map[string]string{"font:noto-caption-bundle": fontRoot},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var first []byte
	for attempt := 0; attempt < 2; attempt++ {
		root := normalizeMaterialPath(t.TempDir())
		if err := RunRawAVPipeline(
			context.Background(), manifest, root, lifecycle.ProfileHarness, blackSilenceProducers(manifest),
		); err != nil {
			t.Fatal(err)
		}
		output, err := os.ReadFile(filepath.Join(root, "preview.webm"))
		if err != nil || len(output) == 0 {
			t.Fatalf("output bytes=%d err=%v", len(output), err)
		}
		if attempt == 0 {
			first = output
		} else if !bytes.Equal(first, output) {
			t.Fatalf("raw A/V pipeline was not byte stable: %x != %x", sha256.Sum256(first), sha256.Sum256(output))
		}
		for _, intermediate := range []string{videoIntermediateFilename, audioIntermediateFilename} {
			if _, err := os.Stat(filepath.Join(root, intermediate)); !os.IsNotExist(err) {
				t.Fatalf("compressed intermediate survived: %s", intermediate)
			}
		}
	}
}

func blackSilenceProducers(manifest ExecutionManifest) RawAVProducers {
	width, height := int(manifest.Plan.Output.CanvasWidth), int(manifest.Plan.Output.CanvasHeight)
	frame := make([]byte, width*height*3/2)
	for index := 0; index < width*height; index++ {
		frame[index] = 16
	}
	for index := width * height; index < len(frame); index++ {
		frame[index] = 128
	}
	return RawAVProducers{
		Video: func(_ context.Context, destination io.Writer) error {
			for frameIndex := uint64(0); frameIndex < manifest.Plan.Output.VideoFrameCount.Value(); frameIndex++ {
				if _, err := destination.Write(frame); err != nil {
					return err
				}
			}
			return nil
		},
		Audio: func(_ context.Context, destination io.Writer) error {
			remaining := manifest.Plan.Output.AudioSampleCount.Value() * 4
			chunk := make([]byte, AudioChunkSamples*4)
			for remaining > 0 {
				count := uint64(len(chunk))
				if count > remaining {
					count = remaining
				}
				if _, err := destination.Write(chunk[:count]); err != nil {
					return err
				}
				remaining -= count
			}
			return nil
		},
	}
}

func TestRawAVPipelineExpectedStreamBounds(t *testing.T) {
	format := domain.DefaultSequenceFormat()
	format.CanvasWidth, format.CanvasHeight = 16, 16
	manifest := ExecutionManifest{Plan: captionRenderPlanWithFormat(t, format).Plan.Payload}
	video, err := expectedVideoStreamBytes(manifest)
	if err != nil || video != 16*16*3/2*30 {
		t.Fatalf("video=%d err=%v", video, err)
	}
	audio, err := expectedAudioStreamBytes(manifest)
	if err != nil || audio != 48_000*4 {
		t.Fatalf("audio=%d err=%v", audio, err)
	}
	if got := fmt.Sprint(video, "/", audio); got != "11520/192000" {
		t.Fatalf("bounds=%s", got)
	}
}
