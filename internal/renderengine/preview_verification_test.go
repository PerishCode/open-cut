package renderengine

import (
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequencePreviewProbeValidationRequiresExactClosedAVShape(t *testing.T) {
	duration, _ := domain.NewRationalTime(5, 1)
	rate, _ := domain.NewRationalTime(30, 1)
	frames, _ := domain.NewUInt64(150)
	samples, _ := domain.NewUInt64(240_000)
	expected := domain.SequencePreviewMediaFacts{
		SemanticDuration: duration, PresentationDuration: duration,
		CanvasWidth: 1280, CanvasHeight: 720, FrameRate: rate, VideoFrameCount: frames,
		AudioSampleRate: 48_000, AudioSampleCount: samples,
		VideoCodec: "vp9", AudioCodec: "opus", PixelFormat: "yuv420p", ChannelLayout: "stereo",
	}
	document := SequencePreviewProbeDocument{
		Format: SequencePreviewProbeFormat{FormatName: "matroska,webm"},
		Streams: []SequencePreviewProbeStream{
			{Index: 0, CodecName: "vp9", CodecType: "video", Width: 1280, Height: 720,
				AverageFrameRate: "30/1", PixelFormat: "yuv420p", ColorRange: "tv",
				ColorSpace: "bt709", ColorTransfer: "bt709", ColorPrimaries: "bt709",
				ReadFrameCount: "150"},
			{Index: 1, CodecName: "opus", CodecType: "audio", SampleRate: "48000",
				Channels: 2, ChannelLayout: "stereo"},
		},
	}
	if err := ValidateSequencePreviewProbeDocument(document, expected); err != nil {
		t.Fatal(err)
	}
	document.Streams[0].ReadFrameCount = "149"
	if err := ValidateSequencePreviewProbeDocument(document, expected); err == nil {
		t.Fatal("mismatched video frame count passed preview verification")
	}
}

func TestAudioSampleCollectorIsStreamingBoundedAndExact(t *testing.T) {
	collector := NewAudioSampleCollector(128)
	for _, chunk := range [][]byte{[]byte("960\n9"), []byte("60\r\n480"), []byte("\n")} {
		if _, err := collector.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if samples, err := collector.Finish(); err != nil || samples != 2_400 {
		t.Fatalf("samples=%d err=%v", samples, err)
	}
	overflow := NewAudioSampleCollector(128)
	if _, err := overflow.Write([]byte("2000000000\n1\n")); err != nil {
		t.Fatal(err)
	}
	if samples, err := overflow.Finish(); err == nil || samples > application.MaximumSequencePreviewAudioSamples {
		t.Fatalf("overflow samples=%d err=%v", samples, err)
	}
}
