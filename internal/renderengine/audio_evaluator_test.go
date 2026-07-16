package renderengine

import (
	"bytes"
	"context"
	"encoding/binary"
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

func TestAudioEvaluatorStreamsExactPrefixSilenceOverlapGainAndGaps(t *testing.T) {
	fixture := newAudioEvaluatorFixture(t, executionClosure(t))
	factory := &fakeAudioRunFactory{failInput: -1}
	producer, err := newAudioStreamProducer(
		fixture.manifest, fixture.attemptRoot, lifecycle.ProfileHarness, factory.start,
	)
	if err != nil {
		t.Fatal(err)
	}
	destination := &audioChunkCapture{}
	if err := producer(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	expectedBytes, err := expectedAudioStreamBytes(fixture.manifest)
	if err != nil || uint64(destination.Len()) != expectedBytes ||
		destination.maximumWrite > int(fixture.manifest.Budget.AudioChunkSamples)*rawPCMFrameBytes {
		t.Fatalf("bytes=%d expected=%d maximumWrite=%d err=%v", destination.Len(), expectedBytes, destination.maximumWrite, err)
	}
	unity, _ := GainCoefficientQ31(0)
	reduced, _ := GainCoefficientQ31(-6_000)
	want := []StereoPCM16{
		{},
		mixedStereo(t, []int16{200}, []int16{-200}, []int64{reduced}),
		mixedStereo(t, []int16{100, 201}, []int16{-100, -201}, []int64{unity, reduced}),
		mixedStereo(t, []int16{101, 202}, []int16{-101, -202}, []int64{unity, reduced}),
		mixedStereo(t, []int16{203}, []int16{-203}, []int64{reduced}),
		{},
		{Left: 102, Right: -102},
		{Left: 103, Right: -103},
		{},
		{Left: 100, Right: -100},
		{Left: 101, Right: -101},
	}
	for ordinal, expected := range want {
		if got := decodeStereoPCM(destination.Bytes(), uint64(ordinal)); got != expected {
			t.Fatalf("ordinal=%d got=%+v want=%+v", ordinal, got, expected)
		}
	}
	last := fixture.manifest.Plan.Output.AudioSampleCount.Value() - 1
	if got := decodeStereoPCM(destination.Bytes(), last); got != (StereoPCM16{}) {
		t.Fatalf("tail=%+v", got)
	}
	if factory.started != 3 || factory.finished != 3 || factory.closed != 0 {
		t.Fatalf("factory=%+v", factory)
	}
}

func TestAudioEvaluatorClosesEveryLiveLaneAfterDecodeFailure(t *testing.T) {
	fixture := newAudioEvaluatorFixture(t, executionClosure(t))
	factory := &fakeAudioRunFactory{failInput: 1, failOrdinal: 1}
	producer, err := newAudioStreamProducer(
		fixture.manifest, fixture.attemptRoot, lifecycle.ProfileHarness, factory.start,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = producer(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "fixture decode failure") {
		t.Fatalf("err=%v", err)
	}
	if factory.started != 2 || factory.finished != 0 || factory.closed != 2 {
		t.Fatalf("factory=%+v", factory)
	}
}

func TestPinnedAudioEvaluatorTraversesRealFixedPointDecoder(t *testing.T) {
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
	fixture := newAudioEvaluatorFixture(t, closure)
	audioBytes, err := expectedAudioStreamBytes(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	firstProxy := filepath.Join(fixture.artifactRoots[0], "proxy.webm")
	written, err := RunBoundedProcessStream(
		context.Background(),
		rawAudioProcessSpec(ffmpeg, fixture.attemptRoot, firstProxy, fixture.manifest, lifecycle.ProfileHarness),
		audioBytes,
		func(_ context.Context, destination io.Writer) error {
			_, writeErr := destination.Write(make([]byte, int(audioBytes)))
			return writeErr
		},
	)
	if err != nil || written != audioBytes {
		t.Fatalf("encode written=%d err=%v", written, err)
	}
	encoded, err := os.ReadFile(firstProxy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.artifactRoots[1], "proxy.webm"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	producer, err := NewAudioStreamProducer(fixture.manifest, fixture.attemptRoot, lifecycle.ProfileHarness)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := producer(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	if uint64(output.Len()) != audioBytes || !allZero(output.Bytes()) {
		t.Fatalf("decoded bytes=%d expected=%d", output.Len(), audioBytes)
	}
}

type audioEvaluatorFixture struct {
	manifest      ExecutionManifest
	attemptRoot   string
	artifactRoots []string
}

func newAudioEvaluatorFixture(t *testing.T, closure ExecutionClosure) audioEvaluatorFixture {
	t.Helper()
	published := captionRenderPlan(t)
	payload := published.Plan.Payload
	revision, _ := domain.NewRevision(1)
	zero, _ := domain.NewRationalTime(0, 1)
	timeBase := audioSampleTime(t, 1)
	count, _ := domain.NewUInt64(domain.SequencePreviewAudioSampleRate)
	artifactIDs := []domain.ArtifactID{
		mustRenderID(t, domain.ParseArtifactID, "00000000-0000-7000-8000-000000000010"),
		mustRenderID(t, domain.ParseArtifactID, "00000000-0000-7000-8000-000000000020"),
	}
	streamIDs := []domain.SourceStreamID{
		mustRenderID(t, domain.ParseSourceStreamID, "00000000-0000-7000-8000-000000000011"),
		mustRenderID(t, domain.ParseSourceStreamID, "00000000-0000-7000-8000-000000000021"),
	}
	assetIDs := []domain.AssetID{
		mustRenderID(t, domain.ParseAssetID, "00000000-0000-7000-8000-000000000012"),
		mustRenderID(t, domain.ParseAssetID, "00000000-0000-7000-8000-000000000022"),
	}
	payload.Inputs = make([]domain.RenderPlanInput, 2)
	for index := range payload.Inputs {
		sourceStart := zero
		if index == 0 {
			sourceStart = audioSampleTime(t, 2)
		}
		payload.Inputs[index] = domain.RenderPlanInput{
			ArtifactID:      artifactIDs[index],
			ArtifactDigest:  domain.Digest(fmt.Sprintf("sha256:%064x", 100+index)),
			ProducerVersion: "fixture-proxy-v2", Profile: application.SourceProxyProfile,
			AssetID: assetIDs[index], AssetRevision: revision,
			Fingerprint: domain.Digest(fmt.Sprintf("sha256:%064x", 200+index)),
			SourceEpoch: zero,
			MediaDigest: domain.Digest(fmt.Sprintf("sha256:%064x", 300+index)),
			Audio: &domain.RenderAudioInput{
				SourceStreamID: streamIDs[index], SourceStart: sourceStart, MaterialStart: zero,
				SourceTimeBase: timeBase, MaterialTimeBase: timeBase,
				SampleRate: domain.SequencePreviewAudioSampleRate, ChannelLayout: "stereo",
				DecodedSampleCount: count,
			},
		}
	}
	payload.Audio = []domain.RenderAudioInstruction{
		audioEvaluatorInstruction(t, artifactIDs[0], streamIDs[0], 0, 0, 4, 0, 0),
		audioEvaluatorInstruction(t, artifactIDs[0], streamIDs[0], 6, 4, 2, 0, 0),
		audioEvaluatorInstruction(t, artifactIDs[0], streamIDs[0], 9, 2, 2, 0, 0),
		audioEvaluatorInstruction(t, artifactIDs[1], streamIDs[1], 1, 0, 4, 1, -6_000),
	}
	_, digest, err := domain.CanonicalDigest("open-cut/render-plan", domain.RenderPlanSchema, payload)
	if err != nil {
		t.Fatal(err)
	}
	published.Plan.Payload = payload
	published.Plan.Digest = digest
	artifactRoots := []string{normalizeMaterialPath(t.TempDir()), normalizeMaterialPath(t.TempDir())}
	fontRoot := normalizeMaterialPath(t.TempDir())
	manifest, _, err := CompileExecutionManifest(
		published,
		application.SequencePreviewRendererIdentity{Version: "fixture-renderer-v1", Target: target.Host().String()},
		closure,
		MaterialPaths{
			ArtifactRoots: map[string]string{
				artifactIDs[0].String(): artifactRoots[0], artifactIDs[1].String(): artifactRoots[1],
			},
			Resources: map[string]string{"font:noto-caption-bundle": fontRoot},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return audioEvaluatorFixture{
		manifest: manifest, attemptRoot: normalizeMaterialPath(t.TempDir()), artifactRoots: artifactRoots,
	}
}

func audioEvaluatorInstruction(
	t *testing.T,
	artifactID domain.ArtifactID,
	streamID domain.SourceStreamID,
	timelineStart, sourceStart, duration int64,
	layer uint16,
	gain int32,
) domain.RenderAudioInstruction {
	t.Helper()
	revision, _ := domain.NewRevision(1)
	clipIdentity := 100 + int64(layer)*100 + timelineStart
	return domain.RenderAudioInstruction{
		ClipID: mustRenderID(t, domain.ParseClipID,
			fmt.Sprintf("00000000-0000-7000-8000-%012d", clipIdentity)),
		ClipRevision: revision,
		TrackID: mustRenderID(t, domain.ParseTrackID,
			fmt.Sprintf("00000000-0000-7000-8000-%012d", 200+layer)),
		TrackRevision: revision,
		Layer:         layer, InputArtifactID: artifactID, SourceStreamID: streamID,
		SourceRange: domain.TimeRange{
			Start: audioSampleTime(t, sourceStart), Duration: audioSampleTime(t, duration),
		},
		TimelineRange: domain.TimeRange{
			Start: audioSampleTime(t, timelineStart), Duration: audioSampleTime(t, duration),
		},
		ChannelMapping: "render-material-stereo-v1", GainMilliDB: gain,
	}
}

type fakeAudioRunFactory struct {
	failInput   int
	failOrdinal uint64
	started     int
	finished    int
	closed      int
}

func (factory *fakeAudioRunFactory) start(
	_ context.Context,
	_ ExecutionManifest,
	run AudioDecodeRun,
	_ string,
	_ lifecycle.Profile,
) (audioRunDecoder, error) {
	factory.started++
	return &fakeAudioRunDecoder{factory: factory, run: run}, nil
}

type fakeAudioRunDecoder struct {
	factory    *fakeAudioRunFactory
	run        AudioDecodeRun
	last       uint64
	hasRead    bool
	terminated bool
}

func (decoder *fakeAudioRunDecoder) ReadTo(ordinal uint64) (StereoPCM16, error) {
	if decoder.terminated || ordinal > decoder.run.LastOrdinal || decoder.hasRead && ordinal < decoder.last {
		return StereoPCM16{}, fmt.Errorf("fixture decoder traversal is invalid")
	}
	if int(decoder.run.InputIndex) == decoder.factory.failInput && ordinal == decoder.factory.failOrdinal {
		return StereoPCM16{}, fmt.Errorf("fixture decode failure")
	}
	decoder.last, decoder.hasRead = ordinal, true
	base := int16((decoder.run.InputIndex + 1) * 100)
	return StereoPCM16{Left: base + int16(ordinal), Right: -base - int16(ordinal)}, nil
}

func (decoder *fakeAudioRunDecoder) Finish() error {
	if decoder.terminated || !decoder.hasRead || decoder.last != decoder.run.LastOrdinal {
		return fmt.Errorf("fixture decoder finish is invalid")
	}
	decoder.terminated = true
	decoder.factory.finished++
	return nil
}

func (decoder *fakeAudioRunDecoder) Close() error {
	if !decoder.terminated {
		decoder.terminated = true
		decoder.factory.closed++
	}
	return nil
}

type audioChunkCapture struct {
	bytes.Buffer
	maximumWrite int
}

func (capture *audioChunkCapture) Write(value []byte) (int, error) {
	if len(value) > capture.maximumWrite {
		capture.maximumWrite = len(value)
	}
	return capture.Buffer.Write(value)
}

func mixedStereo(t *testing.T, left, right []int16, gains []int64) StereoPCM16 {
	t.Helper()
	mixedLeft, err := MixPCM16(left, gains)
	if err != nil {
		t.Fatal(err)
	}
	mixedRight, err := MixPCM16(right, gains)
	if err != nil {
		t.Fatal(err)
	}
	return StereoPCM16{Left: mixedLeft, Right: mixedRight}
}

func decodeStereoPCM(value []byte, ordinal uint64) StereoPCM16 {
	offset := ordinal * rawPCMFrameBytes
	return StereoPCM16{
		Left:  int16(binary.LittleEndian.Uint16(value[offset : offset+2])),
		Right: int16(binary.LittleEndian.Uint16(value[offset+2 : offset+4])),
	}
}

func allZero(value []byte) bool {
	for _, current := range value {
		if current != 0 {
			return false
		}
	}
	return true
}
