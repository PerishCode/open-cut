package mediatoolchain

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestPinnedMediaToolsProduceClosedConformanceEvidence(t *testing.T) {
	root := os.Getenv("OPEN_CUT_MEDIA_TOOL_ROOT")
	if root == "" {
		t.Skip("set OPEN_CUT_MEDIA_TOOL_ROOT to a staged media-tool directory")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(root, target.Host().ExecutableName("ffprobe"))
	ffmpeg := filepath.Join(root, target.Host().ExecutableName("ffmpeg"))
	observations, err := qualifyBaseCapabilities(context.Background(), probe, ffmpeg, ffmpeg, ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{
		CapabilityProbeV1, CapabilityFrameRGBV1, CapabilitySourceProxyV1, CapabilityRenderInputV1,
	} {
		if len(observations[capability]) == 0 {
			t.Fatalf("capability %s produced no evidence", capability)
		}
	}
}

func TestPinnedRendererProducesClosedSemanticMatrixEvidence(t *testing.T) {
	root := os.Getenv("OPEN_CUT_RENDERER_CONFORMANCE_ROOT")
	if root == "" {
		t.Skip("set OPEN_CUT_RENDERER_CONFORMANCE_ROOT to a staged media-tool directory")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	helper := os.Getenv("OPEN_CUT_RENDERER_CONFORMANCE_HELPER")
	if helper == "" {
		helper = filepath.Join(root, "media", target.Host().ExecutableName("open-cut-render"))
	}
	helper, err = filepath.Abs(helper)
	if err != nil {
		t.Fatal(err)
	}
	font, err := rendererConformanceFontRecord(root)
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(root, "media", target.Host().ExecutableName("ffmpeg"))
	ffprobe := filepath.Join(root, "media", target.Host().ExecutableName("ffprobe"))
	helperDigest, _, err := digestFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	ffmpegDigest, _, err := digestFile(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	ffprobeDigest, _, err := digestFile(ffprobe)
	if err != nil {
		t.Fatal(err)
	}
	input := RendererConformanceInput{
		HelperPath: helper, HelperSHA256: helperDigest,
		FFmpegPath: ffmpeg, FFmpegSHA256: ffmpegDigest,
		FFprobePath: ffprobe, FFprobeSHA256: ffprobeDigest,
		FontRoot: filepath.Join(root, filepath.FromSlash(font.Root)), Font: font,
	}
	for _, capabilityID := range []string{
		CapabilitySequencePreviewRendererV1, CapabilitySequenceExportRendererV1,
	} {
		observations, qualifyErr := qualifyRendererCapability(
			context.Background(), target.Host(), capabilityID, input,
		)
		if qualifyErr != nil {
			t.Fatalf("%s: %v", capabilityID, qualifyErr)
		}
		if len(observations) != 28 {
			t.Fatalf("%s observations=%d", capabilityID, len(observations))
		}
	}
}

func rendererConformanceFontRecord(root string) (ResourceRecord, error) {
	record := ResourceRecord{
		ID: renderengine.CaptionFontBundleID, Kind: ResourceKindFontBundle,
		Version: renderengine.CaptionFontBundleVersion, Root: captionFontResourceRoot,
		Files: make([]ResourceFileRecord, 0, len(captionFontSelections())+1),
	}
	for _, selection := range captionFontSelections() {
		filename := filepath.Join(root, filepath.FromSlash(record.Root), selection.Destination)
		digest, size, err := digestFile(filename)
		if err != nil {
			return ResourceRecord{}, err
		}
		record.Files = append(record.Files, ResourceFileRecord{
			Path: selection.Destination, SHA256: digest, ByteSize: size,
		})
	}
	manifest := filepath.Join(root, filepath.FromSlash(record.Root), renderengine.CaptionFontBundleFilename)
	digest, size, err := digestFile(manifest)
	if err != nil {
		return ResourceRecord{}, err
	}
	record.Files = append(record.Files, ResourceFileRecord{
		Path: renderengine.CaptionFontBundleFilename, SHA256: digest, ByteSize: size,
	})
	slices.SortFunc(record.Files, func(left, right ResourceFileRecord) int {
		if left.Path < right.Path {
			return -1
		}
		if left.Path > right.Path {
			return 1
		}
		return 0
	})
	record.SHA256, err = resourceClosureDigest(record)
	return record, err
}
