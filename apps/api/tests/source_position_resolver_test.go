package tests

import (
	"bytes"
	"os"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestResolveVideoSourcePositionUsesAbsoluteVFRPresentationBoundaries(t *testing.T) {
	parallelAPITest(t)
	encoded, err := application.EncodeSourceProxyTimeMap(
		[]int64{-2000, 0, 3000},
		[]int64{0, 2000, 5000},
	)
	if err != nil {
		t.Fatal(err)
	}
	file := writePositionMap(t, encoded)
	defer file.Close()
	track := sourcePositionVideoTrack(t)

	settled, err := service.ResolveVideoSourcePosition(file, track, service.SourcePositionRequest{
		Operation: service.SourcePositionSettle, Target: positionTime(t, 1, 1),
	})
	if err != nil || settled.Source != positionTime(t, 0, 1) || settled.Proxy != positionTime(t, 2, 1) ||
		settled.Boundary != service.SourcePositionVideoPresentation || settled.AtStart || settled.AtEnd {
		t.Fatalf("settled=%+v err=%v", settled, err)
	}

	previous, err := service.ResolveVideoSourcePosition(file, track, service.SourcePositionRequest{
		Operation: service.SourcePositionPrevious, Target: positionTime(t, 0, 1),
	})
	if err != nil || previous.Source != positionTime(t, -2, 1) || !previous.AtStart {
		t.Fatalf("previous=%+v err=%v", previous, err)
	}

	next, err := service.ResolveVideoSourcePosition(file, track, service.SourcePositionRequest{
		Operation: service.SourcePositionNext, Target: positionTime(t, 3, 1),
	})
	if err != nil || next.Source != positionTime(t, 5, 1) || next.Proxy != positionTime(t, 7, 1) ||
		next.Boundary != service.SourcePositionCoverageEnd || !next.AtEnd {
		t.Fatalf("next=%+v err=%v", next, err)
	}
}

func TestResolveAudioSourcePositionUsesSourceSampleBoundaries(t *testing.T) {
	parallelAPITest(t)
	start := positionTime(t, -2, 1)
	duration := positionTime(t, 4, 1)
	timeBase := positionTime(t, 1, 48000)
	sampleCount, _ := domain.NewUInt64(192000)
	streamID, _ := domain.ParseSourceStreamID("00000000-0000-7000-8000-000000000002")
	track := application.SourceProxyAudioTrack{
		Source: domain.SourceStream{ID: streamID, Descriptor: domain.SourceStreamDescriptor{
			Index: 1, MediaType: domain.MediaAudio, Codec: "pcm", TimeBase: timeBase,
			StartTime: &start, Duration: &duration, Dispositions: []string{},
			Audio: &domain.AudioStreamFacts{SampleFormat: "s16", SampleRate: 48000, Channels: 1, ChannelLayout: "mono"},
		}},
		SourceStartTime: start, ProxyStartTime: positionTime(t, 0, 1), TimeBase: timeBase,
		Codec: "opus", SampleRate: 48000, Channels: 2, ChannelLayout: "stereo",
		ChannelProjection: "mono-duplicate-v1", DecodedSampleCount: sampleCount,
	}
	target := positionTime(t, -3, 2)
	settled, err := service.ResolveAudioSourcePosition(track, service.SourcePositionRequest{
		Operation: service.SourcePositionSettle, Target: target,
	})
	if err != nil || settled.Source != target || settled.Proxy != positionTime(t, 1, 2) ||
		settled.Boundary != service.SourcePositionAudioSample {
		t.Fatalf("settled=%+v err=%v", settled, err)
	}
	next, err := service.ResolveAudioSourcePosition(track, service.SourcePositionRequest{
		Operation: service.SourcePositionNext, Target: target,
	})
	if err != nil || next.Source != positionTime(t, -71999, 48000) ||
		next.Proxy != positionTime(t, 24001, 48000) {
		t.Fatalf("next=%+v err=%v", next, err)
	}
}

func writePositionMap(t *testing.T, content []byte) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "position-map-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bytes.NewReader(content).WriteTo(file); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return file
}

func sourcePositionVideoTrack(t *testing.T) application.SourceProxyVideoTrack {
	t.Helper()
	start := positionTime(t, -2, 1)
	duration := positionTime(t, 7, 1)
	timeBase := positionTime(t, 1, 1000)
	streamID, _ := domain.ParseSourceStreamID("00000000-0000-7000-8000-000000000001")
	frameCount, _ := domain.NewUInt64(3)
	return application.SourceProxyVideoTrack{
		Source: domain.SourceStream{ID: streamID, Descriptor: domain.SourceStreamDescriptor{
			Index: 0, MediaType: domain.MediaVideo, Codec: "test", TimeBase: timeBase,
			StartTime: &start, Duration: &duration, Dispositions: []string{},
			Video: &domain.VideoStreamFacts{Width: 16, Height: 16},
		}},
		SourceStartTime: start, ProxyStartTime: positionTime(t, 0, 1), TimeBase: timeBase,
		Codec: "vp9", Width: 16, Height: 16, PixelFormat: "yuv420p",
		ColorRange: "tv", ColorSpace: "bt709", ColorTransfer: "bt709", ColorPrimaries: "bt709",
		ColorInterpretation: "source-metadata", FrameCount: frameCount,
	}
}

func positionTime(t *testing.T, value int64, scale int32) domain.RationalTime {
	t.Helper()
	result, err := domain.NewRationalTime(value, scale)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
