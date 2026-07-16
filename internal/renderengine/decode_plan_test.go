package renderengine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestVideoDecodePlanUsesBoundedLanesAndExplicitRestartRuns(t *testing.T) {
	sourceMap := decodePlanSourceMap(t, []int64{0, 1, 2, 3, 4, 5, 6})
	defer sourceMap.Close()
	artifactID := mustRenderID(t, domain.ParseArtifactID, "00000000-0000-7000-8000-000000000010")
	streamID := mustRenderID(t, domain.ParseSourceStreamID, "00000000-0000-7000-8000-000000000011")
	frameRate := mustSourceMapTime(t, 1, 1)
	frameCount, _ := domain.NewUInt64(6)
	input := domain.RenderVideoInput{
		SourceStreamID: streamID, SourceTimeBase: frameRate,
	}
	plan := domain.RenderPlanPayload{
		Inputs: []domain.RenderPlanInput{{ArtifactID: artifactID, Video: &input}},
		Output: domain.RenderOutputPolicy{FrameRate: frameRate, VideoFrameCount: frameCount},
		Video: []domain.RenderVideoInstruction{
			decodePlanInstruction(t, artifactID, streamID, 0, 0, 2, 1),
			decodePlanInstruction(t, artifactID, streamID, 2, 1, 2, 1),
			decodePlanInstruction(t, artifactID, streamID, 4, 0, 2, 1),
			decodePlanInstruction(t, artifactID, streamID, 1, 4, 2, 2),
		},
	}
	sources := map[string]videoDecodeSource{
		artifactID.String(): {inputIndex: 0, input: input, mapFile: sourceMap},
	}
	result, err := planVideoDecodeLanes(plan, sources, 11)
	if err != nil {
		t.Fatal(err)
	}
	if result.Policy != VideoDecodePlanPolicyV1 || len(result.Lanes) != 2 || result.TraversalFrames != 11 {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Lanes[0].Runs) != 2 || len(result.Lanes[0].Runs[0].Requests) != 2 ||
		result.Lanes[0].Runs[0].LastOrdinal != 2 || result.Lanes[0].Runs[1].LastOrdinal != 1 {
		t.Fatalf("lane0=%+v", result.Lanes[0])
	}
	if len(result.Lanes[1].Runs) != 1 || result.Lanes[1].Runs[0].LastOrdinal != 5 ||
		result.Lanes[1].Runs[0].TraversalFrames != 6 {
		t.Fatalf("lane1=%+v", result.Lanes[1])
	}
	for _, lane := range result.Lanes {
		for _, run := range lane.Runs {
			if err := validateVideoDecodeRun(plan, run); err != nil {
				t.Fatal(err)
			}
		}
	}
	mutated := result.Lanes[0].Runs[0]
	mutated.Requests[1].FirstOutputFrame = 1
	if err := validateVideoDecodeRun(plan, mutated); err == nil {
		t.Fatal("overlapping requests were accepted in one decode run")
	}
	_, err = planVideoDecodeLanes(plan, sources, 10)
	var limit ResourceLimitError
	if !errors.As(err, &limit) || limit.Subject != "decoded-video-frames" {
		t.Fatalf("limit=%+v err=%v", limit, err)
	}
}

func TestOutputFrameRangeUsesExactHalfOpenGrid(t *testing.T) {
	start := mustSourceMapTime(t, 1, 3)
	duration := mustSourceMapTime(t, 1, 3)
	rate := mustSourceMapTime(t, 3, 1)
	first, after, err := outputFrameRange(
		domain.TimeRange{Start: start, Duration: duration}, rate, 10,
	)
	if err != nil || first != 1 || after != 2 {
		t.Fatalf("first=%d after=%d err=%v", first, after, err)
	}
	shortStart := mustSourceMapTime(t, 1, 10)
	shortDuration := mustSourceMapTime(t, 1, 10)
	first, after, err = outputFrameRange(
		domain.TimeRange{Start: shortStart, Duration: shortDuration}, rate, 10,
	)
	if err != nil || first != 1 || after != 1 {
		t.Fatalf("empty sample range first=%d after=%d err=%v", first, after, err)
	}
}

func decodePlanInstruction(
	t *testing.T,
	artifactID domain.ArtifactID,
	streamID domain.SourceStreamID,
	timelineStart, sourceStart, duration int64,
	layer uint16,
) domain.RenderVideoInstruction {
	t.Helper()
	clipValue := 100 + timelineStart*10 + int64(layer)
	clipID := mustRenderID(t, domain.ParseClipID,
		"00000000-0000-7000-8000-"+leftPadDecodePlanID(clipValue))
	return domain.RenderVideoInstruction{
		ClipID: clipID, Layer: layer, InputArtifactID: artifactID, SourceStreamID: streamID,
		SourceRange: domain.TimeRange{
			Start: mustSourceMapTime(t, sourceStart, 1), Duration: mustSourceMapTime(t, duration, 1),
		},
		TimelineRange: domain.TimeRange{
			Start: mustSourceMapTime(t, timelineStart, 1), Duration: mustSourceMapTime(t, duration, 1),
		},
	}
}

func leftPadDecodePlanID(value int64) string {
	result := "000000000000" + domain.Int64(value).String()
	return result[len(result)-12:]
}

func decodePlanSourceMap(t *testing.T, values []int64) *SourceMap {
	t.Helper()
	encoded, err := application.EncodeSourceProxyTimeMap(values, values)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "video-time-map.bin")
	if err := os.WriteFile(filename, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	filename, err = filepath.EvalSymlinks(filename)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	result, err := OpenSourceMap(filename, domain.Digest("sha256:"+hex.EncodeToString(digest[:])))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
