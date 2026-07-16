package renderengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

func TestVideoEvaluatorSelectsEverySourceMapOrdinalAndStreamsCompositedFrames(t *testing.T) {
	fixture := newVideoEvaluatorFixture(t, executionClosure(t))
	factory := &fakeVideoRunFactory{}
	producer, err := newVideoStreamProducer(
		fixture.manifest, fixture.attemptRoot, lifecycle.ProfileHarness, factory.start,
	)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := producer(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	frameBytes, _ := rawYUVFrameBytes(16, 16)
	if output.Len() != frameBytes*30 || factory.started != 1 || factory.finished != 1 || factory.closed != 0 {
		t.Fatalf("bytes=%d factory=%+v", output.Len(), factory)
	}
	assertSolidYUV420(t, output.Bytes()[:frameBytes], 16, 16, 235, 128, 128)
	assertSolidYUV420(t, output.Bytes()[frameBytes:frameBytes*2], 16, 16, 16, 128, 128)
	assertSolidYUV420(t, output.Bytes()[frameBytes*29:], 16, 16, 16, 128, 128)
}

func TestCaptionedVideoEvaluatorRendersCaptionOnlyFrames(t *testing.T) {
	published := captionRenderPlan(t)
	fontRoot := normalizeMaterialPath(t.TempDir())
	manifest, _, err := CompileExecutionManifest(
		published,
		application.SequencePreviewRendererIdentity{Version: "fixture-renderer-v1", Target: target.Host().String()},
		executionClosure(t),
		MaterialPaths{ArtifactRoots: map[string]string{}, Resources: map[string]string{
			"font:noto-caption-bundle": fontRoot,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := NewPinnedCaptionFontBundle(captionTextFixtureFontFiles())
	captions, err := NewCaptionCoverageEvaluator(manifest, bundle, &fixtureCaptionNative{})
	if err != nil {
		t.Fatal(err)
	}
	producer, err := newCaptionedVideoStreamProducer(
		manifest, normalizeMaterialPath(t.TempDir()), lifecycle.ProfileHarness,
		func(context.Context, ExecutionManifest, VideoDecodeRun, string, lifecycle.Profile) (videoRunDecoder, error) {
			return nil, errors.New("caption-only producer started a video decoder")
		}, captions,
	)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := producer(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	frameBytes, _ := rawYUVFrameBytes(manifest.Plan.Output.CanvasWidth, manifest.Plan.Output.CanvasHeight)
	if output.Len() != frameBytes*int(manifest.Plan.Output.VideoFrameCount.Value()) {
		t.Fatalf("output bytes=%d", output.Len())
	}
	first := output.Bytes()[:frameBytes]
	allBlack := true
	for _, value := range first[:int(manifest.Plan.Output.CanvasWidth*manifest.Plan.Output.CanvasHeight)] {
		if value != 16 {
			allBlack = false
			break
		}
	}
	if allBlack {
		t.Fatal("caption-only frame remained black")
	}
}

func TestPinnedVideoEvaluatorTraversesRealDecoderAndCompositor(t *testing.T) {
	mediaRoot := os.Getenv("OPEN_CUT_MEDIA_TOOL_ROOT")
	if mediaRoot == "" {
		t.Skip("OPEN_CUT_MEDIA_TOOL_ROOT is not set")
	}
	ffmpeg := filepath.Join(mediaRoot, target.Host().ExecutableName("ffmpeg"))
	ffmpeg, err := filepath.EvalSymlinks(ffmpeg)
	if err != nil || !cleanAbsoluteRegular(ffmpeg) {
		t.Skip("pinned FFmpeg is unavailable")
	}
	closure := ExecutionClosure{
		SHA256: domain.Digest("sha256:" + strings.Repeat("c", 64)),
		Tools: map[string]ExecutionToolPin{
			"ffmpeg": {Path: ffmpeg, SHA256: domain.Digest("sha256:" + strings.Repeat("d", 64))},
		},
	}
	fixture := newVideoEvaluatorFixture(t, closure)
	videoBytes, err := expectedVideoStreamBytes(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	proxyPath := filepath.Join(fixture.artifactRoot, "proxy.webm")
	black := solidYUV420(16, 16, 16, 128, 128)
	written, err := RunBoundedProcessStream(
		context.Background(),
		rawVideoProcessSpec(ffmpeg, fixture.attemptRoot, proxyPath, fixture.manifest, lifecycle.ProfileHarness),
		videoBytes,
		func(_ context.Context, destination io.Writer) error {
			for range 30 {
				if _, err := destination.Write(black); err != nil {
					return err
				}
			}
			return nil
		},
	)
	if err != nil || written != videoBytes {
		t.Fatalf("encode written=%d err=%v", written, err)
	}
	producer, err := NewVideoStreamProducer(fixture.manifest, fixture.attemptRoot, lifecycle.ProfileHarness)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := producer(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	frameBytes := len(black)
	if uint64(output.Len()) != videoBytes {
		t.Fatalf("decoded bytes=%d expected=%d", output.Len(), videoBytes)
	}
	for frame := 0; frame < 30; frame++ {
		assertSolidYUV420(t, output.Bytes()[frame*frameBytes:(frame+1)*frameBytes], 16, 16, 16, 128, 128)
	}
}

type videoEvaluatorFixture struct {
	manifest     ExecutionManifest
	attemptRoot  string
	artifactRoot string
}

func newVideoEvaluatorFixture(t *testing.T, closure ExecutionClosure) videoEvaluatorFixture {
	t.Helper()
	format := domain.DefaultSequenceFormat()
	format.CanvasWidth, format.CanvasHeight = 16, 16
	published := captionRenderPlanWithFormat(t, format)
	payload := published.Plan.Payload
	revision, _ := domain.NewRevision(1)
	zero, _ := domain.NewRationalTime(0, 1)
	timeBase, _ := domain.NewRationalTime(1, 30)
	proxyTimeBase, _ := domain.NewRationalTime(1, 1_000)
	one, _ := domain.NewExactRational(1, 1)
	zeroPlacement, _ := domain.NewExactRational(0, 1)
	artifactID := mustRenderID(t, domain.ParseArtifactID, "00000000-0000-7000-8000-000000000070")
	assetID := mustRenderID(t, domain.ParseAssetID, "00000000-0000-7000-8000-000000000071")
	streamID := mustRenderID(t, domain.ParseSourceStreamID, "00000000-0000-7000-8000-000000000072")
	artifactRoot := normalizeMaterialPath(t.TempDir())
	values := make([]int64, 30)
	for index := range values {
		values[index] = int64(index)
	}
	encodedMap, err := application.EncodeSourceProxyTimeMap(values, values)
	if err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(artifactRoot, "video-time-map.bin")
	if err := os.WriteFile(mapPath, encodedMap, 0o600); err != nil {
		t.Fatal(err)
	}
	mapDigest := sha256.Sum256(encodedMap)
	payload.Inputs = []domain.RenderPlanInput{{
		ArtifactID:      artifactID,
		ArtifactDigest:  domain.Digest("sha256:" + strings.Repeat("1", 64)),
		ProducerVersion: "fixture-proxy-v2", Profile: application.SourceProxyProfile,
		AssetID: assetID, AssetRevision: revision,
		Fingerprint: domain.Digest("sha256:" + strings.Repeat("2", 64)),
		SourceEpoch: zero,
		MediaDigest: domain.Digest("sha256:" + strings.Repeat("3", 64)),
		Video: &domain.RenderVideoInput{
			SourceStreamID: streamID, SourceStart: zero, MaterialStart: zero,
			SourceTimeBase: timeBase, MaterialTimeBase: proxyTimeBase,
			TimeMapDigest: domain.Digest("sha256:" + hex.EncodeToString(mapDigest[:])),
			Width:         16, Height: 16,
		},
	}}
	payload.Video = []domain.RenderVideoInstruction{{
		ClipID:        mustRenderID(t, domain.ParseClipID, "00000000-0000-7000-8000-000000000073"),
		ClipRevision:  revision,
		TrackID:       mustRenderID(t, domain.ParseTrackID, "00000000-0000-7000-8000-000000000074"),
		TrackRevision: revision,
		Layer:         0, InputArtifactID: artifactID, SourceStreamID: streamID,
		SourceRange:   domain.TimeRange{Start: zero, Duration: payload.Duration},
		TimelineRange: domain.TimeRange{Start: zero, Duration: payload.Duration},
		Orientation:   "normalized-by-render-material-v1",
		Placement: domain.RenderPlacement{
			CropWidthBasisPoints: 10_000, CropHeightBasisPoints: 10_000,
			ScaleX: one, ScaleY: one, TranslateX: zeroPlacement, TranslateY: zeroPlacement,
			AnchorXBasisPoints: 5_000, AnchorYBasisPoints: 5_000,
			OpacityBasisPoints: 10_000, FitPolicy: "contain",
		},
	}}
	_, planDigest, err := domain.CanonicalDigest("open-cut/render-plan", domain.RenderPlanSchema, payload)
	if err != nil {
		t.Fatal(err)
	}
	published.Plan.Payload, published.Plan.Digest = payload, planDigest
	fontRoot := normalizeMaterialPath(t.TempDir())
	manifest, _, err := CompileExecutionManifest(
		published,
		application.SequencePreviewRendererIdentity{
			Version: "fixture-renderer-v1", Target: target.Host().String(),
		},
		closure,
		MaterialPaths{
			ArtifactRoots: map[string]string{artifactID.String(): artifactRoot},
			Resources:     map[string]string{"font:noto-caption-bundle": fontRoot},
		},
	)
	if err != nil {
		t.Fatalf("compile video fixture: %v", err)
	}
	return videoEvaluatorFixture{
		manifest: manifest, attemptRoot: normalizeMaterialPath(t.TempDir()), artifactRoot: artifactRoot,
	}
}

type fakeVideoRunFactory struct {
	started  int
	finished int
	closed   int
}

func (factory *fakeVideoRunFactory) start(
	_ context.Context,
	_ ExecutionManifest,
	run VideoDecodeRun,
	_ string,
	_ lifecycle.Profile,
) (videoRunDecoder, error) {
	factory.started++
	return &fakeVideoRunDecoder{factory: factory, run: run}, nil
}

type fakeVideoRunDecoder struct {
	factory    *fakeVideoRunFactory
	run        VideoDecodeRun
	last       uint64
	hasRead    bool
	terminated bool
	frame      []byte
}

func (decoder *fakeVideoRunDecoder) ReadTo(ordinal uint64) ([]byte, error) {
	if decoder.terminated || ordinal > decoder.run.LastOrdinal || decoder.hasRead && ordinal < decoder.last {
		return nil, fmt.Errorf("fixture video traversal is invalid")
	}
	decoder.last, decoder.hasRead = ordinal, true
	y := byte(235)
	if ordinal%2 != 0 {
		y = 16
	}
	decoder.frame = solidYUV420(16, 16, y, 128, 128)
	return decoder.frame, nil
}

func (decoder *fakeVideoRunDecoder) Finish() error {
	if decoder.terminated || !decoder.hasRead || decoder.last != decoder.run.LastOrdinal {
		return fmt.Errorf("fixture video finish is invalid")
	}
	decoder.terminated = true
	decoder.factory.finished++
	return nil
}

func (decoder *fakeVideoRunDecoder) Close() error {
	if !decoder.terminated {
		decoder.terminated = true
		decoder.factory.closed++
	}
	return nil
}
