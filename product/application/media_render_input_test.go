package application

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestRenderInputManifestIsCanonicalAndExactStream(t *testing.T) {
	assetID, err := domain.ParseAssetID("00000000-0000-7000-8000-000000000020")
	if err != nil {
		t.Fatal(err)
	}
	stream := proxyVideoStream(t, "00000000-0000-7000-8000-000000000021", 2, nil)
	fingerprint := domain.Digest("sha256:" + strings.Repeat("a", 64))
	mediaDigest := domain.Digest("sha256:" + strings.Repeat("b", 64))
	mapDigest := domain.Digest("sha256:" + strings.Repeat("c", 64))
	zero, _ := domain.NewRationalTime(0, 1)
	timeBase, _ := domain.NewRationalTime(1, 1000)
	mediaSize, _ := domain.NewUInt64(4096)
	frameCount, _ := domain.NewUInt64(2)
	mapSize, _ := domain.NewUInt64(sourceProxyTimeMapHeaderSize + 2*sourceProxyTimeMapRecordSize)
	manifest := RenderInputArtifactManifest{
		AssetID: assetID, Fingerprint: fingerprint, Profile: RenderInputProfile,
		Producer: "render-input-fixture-v1", SourceEpoch: zero,
		Media: RenderInputArtifactFile{
			Path: "render-input.mkv", MimeType: "video/x-matroska",
			ByteSize: mediaSize, SHA256: mediaDigest,
		},
		Video: &RenderInputVideoTrack{
			Source: stream, SourceStartTime: zero, MaterialStartTime: zero, TimeBase: timeBase,
			Codec: "ffv1", Width: 1920, Height: 1080, PixelFormat: "yuv420p",
			ColorRange: "tv", ColorSpace: "bt709", ColorTransfer: "bt709",
			ColorPrimaries: "bt709", ColorInterpretation: "source-metadata",
			FrameCount: frameCount,
			TimeMap: RenderInputArtifactFile{
				Path: "video-time-map.bin", MimeType: "application/vnd.open-cut.pts-map",
				ByteSize: mapSize, SHA256: mapDigest,
			},
		},
	}
	canonical, digest, err := CanonicalRenderInputArtifactManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRenderInputArtifactManifest(canonical)
	if err != nil || decoded.Video == nil || decoded.Video.Source.ID != stream.ID {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	reencoded, repeatedDigest, err := CanonicalRenderInputArtifactManifest(decoded)
	if err != nil || repeatedDigest != digest || !bytes.Equal(reencoded, canonical) {
		t.Fatalf("render-input canonical round trip changed: %v", err)
	}
	corrupted := bytes.Replace(canonical, []byte(`"codec":"ffv1"`), []byte(`"codec":"huffyuv"`), 1)
	if _, err := DecodeRenderInputArtifactManifest(corrupted); err == nil {
		t.Fatal("unsupported render-input codec was accepted")
	}

	videoID := stream.ID
	parameters := InitialMediaJobParameters{
		AssetID: assetID, Kind: domain.MediaJobRenderInput, Profile: RenderInputProfile,
		RenderInputSelection: &SourceProxySelection{
			Policy: SourceProxySelectionExplicit, VideoStreamID: &videoID,
		},
	}
	if err := parameters.Validate(); err != nil {
		t.Fatal(err)
	}
	audioID := mustProxySourceStreamID(t, "00000000-0000-7000-8000-000000000022")
	parameters.RenderInputSelection.AudioStreamID = &audioID
	if err := parameters.Validate(); err == nil {
		t.Fatal("multi-stream render-input selection was accepted")
	}
}
