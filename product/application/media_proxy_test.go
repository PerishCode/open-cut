package application

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSourceProxySelectsDefaultThenLowestContainerIndex(t *testing.T) {
	streams := []domain.SourceStream{
		proxyVideoStream(t, "00000000-0000-7000-8000-000000000001", 7, nil),
		proxyVideoStream(t, "00000000-0000-7000-8000-000000000002", 4, []string{"default"}),
		proxyVideoStream(t, "00000000-0000-7000-8000-000000000003", 2, []string{"default"}),
		proxyAudioStream(t, "00000000-0000-7000-8000-000000000004", 3, nil),
		proxyAudioStream(t, "00000000-0000-7000-8000-000000000005", 1, nil),
	}
	video, audio, err := SelectSourceProxyStreams(streams, SourceProxySelection{Policy: SourceProxySelectionDefault})
	if err != nil {
		t.Fatal(err)
	}
	if video.ID.String() != "00000000-0000-7000-8000-000000000003" ||
		audio.ID.String() != "00000000-0000-7000-8000-000000000005" {
		t.Fatalf("video=%+v audio=%+v", video, audio)
	}
}

func TestSourceProxySelectsOnlyExplicitStreams(t *testing.T) {
	videoID := mustProxySourceStreamID(t, "00000000-0000-7000-8000-000000000002")
	audioID := mustProxySourceStreamID(t, "00000000-0000-7000-8000-000000000005")
	streams := []domain.SourceStream{
		proxyVideoStream(t, "00000000-0000-7000-8000-000000000001", 0, []string{"default"}),
		proxyVideoStream(t, videoID.String(), 1, nil),
		proxyAudioStream(t, "00000000-0000-7000-8000-000000000004", 2, []string{"default"}),
		proxyAudioStream(t, audioID.String(), 3, nil),
	}
	video, audio, err := SelectSourceProxyStreams(streams, SourceProxySelection{
		Policy: SourceProxySelectionExplicit, VideoStreamID: &videoID, AudioStreamID: &audioID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if video == nil || audio == nil || video.ID != videoID || audio.ID != audioID {
		t.Fatalf("video=%+v audio=%+v", video, audio)
	}
	video, audio, err = SelectSourceProxyStreams(streams, SourceProxySelection{
		Policy: SourceProxySelectionExplicit, AudioStreamID: &audioID,
	})
	if err != nil || video != nil || audio == nil || audio.ID != audioID {
		t.Fatalf("audio-only video=%+v audio=%+v err=%v", video, audio, err)
	}
}

func TestSourceProxyTimeMapAndManifestAreExact(t *testing.T) {
	timeMap, err := EncodeSourceProxyTimeMap([]int64{-2, 0}, []int64{1000, 2000})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSourceProxyTimeMap(timeMap, 2); err != nil {
		t.Fatal(err)
	}
	point, err := ReadSourceProxyTimeMapPointAt(bytes.NewReader(timeMap), 2, 1)
	if err != nil || point.SourcePTS != 0 || point.ProxyPTS != 2000 {
		t.Fatalf("point=%+v err=%v", point, err)
	}
	corrupted := append([]byte(nil), timeMap...)
	copy(corrupted[32:40], corrupted[16:24])
	if err := ValidateSourceProxyTimeMap(corrupted, 2); err == nil {
		t.Fatal("non-increasing time map was accepted")
	}

	assetID, err := domain.ParseAssetID("00000000-0000-7000-8000-000000000010")
	if err != nil {
		t.Fatal(err)
	}
	epoch, _ := domain.NewRationalTime(-1, 1)
	videoSourceStart, _ := domain.NewRationalTime(0, 1)
	videoProxyStart, _ := domain.NewRationalTime(1, 1)
	audioStart, _ := domain.NewRationalTime(-1, 1)
	proxyZero, _ := domain.NewRationalTime(0, 1)
	millisecond, _ := domain.NewRationalTime(1, 1000)
	frameCount, _ := domain.NewUInt64(2)
	audioSampleCount, _ := domain.NewUInt64(48_000)
	timeMapSize, _ := domain.NewUInt64(uint64(len(timeMap)))
	mediaSize, _ := domain.NewUInt64(1234)
	digest := domain.Digest("sha256:" + strings.Repeat("a", 64))
	manifest := SourceProxyArtifactManifest{
		AssetID: assetID, Fingerprint: digest, Profile: SourceProxyProfile,
		Producer: "fixture-proxy-v1", SourceEpoch: epoch,
		Media: SourceProxyArtifactFile{
			Path: "proxy.webm", MimeType: "video/webm", ByteSize: mediaSize, SHA256: digest,
		},
		Video: &SourceProxyVideoTrack{
			Source:          proxyVideoStream(t, "00000000-0000-7000-8000-000000000011", 0, nil),
			SourceStartTime: videoSourceStart, ProxyStartTime: videoProxyStart,
			TimeBase: millisecond, Codec: "vp9", Width: 1920, Height: 1080,
			PixelFormat: "yuv420p", ColorRange: "tv", ColorSpace: "bt709",
			ColorTransfer: "bt709", ColorPrimaries: "bt709",
			ColorInterpretation: "assumed-bt709", FrameCount: frameCount,
			TimeMap: SourceProxyArtifactFile{
				Path: "video-time-map.bin", MimeType: "application/vnd.open-cut.pts-map",
				ByteSize: timeMapSize, SHA256: digest,
			},
		},
		Audio: &SourceProxyAudioTrack{
			Source:          proxyAudioStream(t, "00000000-0000-7000-8000-000000000012", 1, nil),
			SourceStartTime: audioStart, ProxyStartTime: proxyZero, TimeBase: millisecond, Codec: "opus",
			SampleRate: 48000, Channels: 2, ChannelLayout: "stereo",
			ChannelProjection: "stereo-pass-v1", DecodedSampleCount: audioSampleCount,
		},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	canonical, _, err := domain.CanonicalDigest(
		"open-cut/source-proxy-artifact", SourceProxyArtifactSchema, manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSourceProxyArtifactManifest(canonical)
	if err != nil || decoded.Video == nil || decoded.Video.FrameCount != frameCount {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
}

func proxyVideoStream(
	t *testing.T,
	idValue string,
	index uint32,
	dispositions []string,
) domain.SourceStream {
	t.Helper()
	id, err := domain.ParseSourceStreamID(idValue)
	if err != nil {
		t.Fatal(err)
	}
	timeBase, _ := domain.NewRationalTime(1, 1000)
	return domain.SourceStream{ID: id, Descriptor: domain.SourceStreamDescriptor{
		Index: index, MediaType: domain.MediaVideo, Codec: "vp9", TimeBase: timeBase,
		Dispositions: dispositions,
		Video:        &domain.VideoStreamFacts{Width: 1920, Height: 1080, Rotation: 0},
	}}
}

func proxyAudioStream(
	t *testing.T,
	idValue string,
	index uint32,
	dispositions []string,
) domain.SourceStream {
	t.Helper()
	id, err := domain.ParseSourceStreamID(idValue)
	if err != nil {
		t.Fatal(err)
	}
	timeBase, _ := domain.NewRationalTime(1, 48000)
	return domain.SourceStream{ID: id, Descriptor: domain.SourceStreamDescriptor{
		Index: index, MediaType: domain.MediaAudio, Codec: "opus", TimeBase: timeBase,
		Dispositions: dispositions,
		Audio:        &domain.AudioStreamFacts{SampleRate: 48000, Channels: 2, ChannelLayout: "stereo"},
	}}
}

func mustProxySourceStreamID(t *testing.T, value string) domain.SourceStreamID {
	t.Helper()
	id, err := domain.ParseSourceStreamID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
