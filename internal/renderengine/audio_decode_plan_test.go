package renderengine

import (
	"errors"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestAudioDecodePlanUsesTrackRelativeSamplesAndMonotonicLanes(t *testing.T) {
	artifactID := mustRenderID(t, domain.ParseArtifactID, "00000000-0000-7000-8000-000000000020")
	streamID := mustRenderID(t, domain.ParseSourceStreamID, "00000000-0000-7000-8000-000000000021")
	count, _ := domain.NewUInt64(10)
	outputCount, _ := domain.NewUInt64(12)
	input := domain.RenderAudioInput{
		SourceStreamID: streamID, SourceStart: audioSampleTime(t, 2),
		SourceTimeBase:   mustSourceMapTime(t, 1, 48_000),
		MaterialTimeBase: mustSourceMapTime(t, 1, 1_000),
		SampleRate:       domain.SequencePreviewAudioSampleRate, ChannelLayout: "stereo",
		DecodedSampleCount: count,
	}
	plan := domain.RenderPlanPayload{
		Inputs: []domain.RenderPlanInput{{ArtifactID: artifactID, Audio: &input}},
		Output: domain.RenderOutputPolicy{
			AudioSampleCount: outputCount,
			Audio:            domain.RenderAudioOutputPolicy{SampleRate: domain.SequencePreviewAudioSampleRate},
		},
		Audio: []domain.RenderAudioInstruction{
			audioDecodeInstruction(t, artifactID, streamID, 0, 0, 4, 1),
			audioDecodeInstruction(t, artifactID, streamID, 4, 4, 3, 1),
			audioDecodeInstruction(t, artifactID, streamID, 7, 2, 2, 1),
			audioDecodeInstruction(t, artifactID, streamID, 2, 5, 3, 2),
		},
	}
	sources := map[string]audioDecodeSource{
		artifactID.String(): {inputIndex: 0, input: input},
	}
	result, err := planAudioDecodeLanes(plan, sources, 13)
	if err != nil {
		t.Fatal(err)
	}
	if result.Policy != AudioDecodePlanPolicyV1 || len(result.Lanes) != 2 || result.TraversalSamples != 13 {
		t.Fatalf("result=%+v", result)
	}
	first := result.Lanes[0]
	if len(first.Runs) != 2 || len(first.Runs[0].Requests) != 2 ||
		first.Runs[0].Requests[0].FirstOutputSample != 2 ||
		first.Runs[0].Requests[0].FirstOrdinal != 0 || first.Runs[0].LastOrdinal != 4 ||
		first.Runs[1].LastOrdinal != 1 {
		t.Fatalf("lane0=%+v", first)
	}
	second := result.Lanes[1]
	if len(second.Runs) != 1 || second.Runs[0].LastOrdinal != 5 || second.Runs[0].TraversalSamples != 6 {
		t.Fatalf("lane1=%+v", second)
	}
	for _, lane := range result.Lanes {
		for _, run := range lane.Runs {
			if err := validateAudioDecodeRun(plan, run); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, err = planAudioDecodeLanes(plan, sources, 12)
	var limit ResourceLimitError
	if !errors.As(err, &limit) || limit.Subject != "decoded-audio-samples" {
		t.Fatalf("limit=%+v err=%v", limit, err)
	}
}

func TestAudioDecodePlanRejectsActiveTailBeyondProxy(t *testing.T) {
	artifactID := mustRenderID(t, domain.ParseArtifactID, "00000000-0000-7000-8000-000000000030")
	streamID := mustRenderID(t, domain.ParseSourceStreamID, "00000000-0000-7000-8000-000000000031")
	count, _ := domain.NewUInt64(10)
	outputCount, _ := domain.NewUInt64(2)
	input := domain.RenderAudioInput{
		SourceStreamID: streamID, SourceStart: audioSampleTime(t, 0),
		SampleRate: 48_000, ChannelLayout: "stereo", DecodedSampleCount: count,
	}
	plan := domain.RenderPlanPayload{
		Inputs: []domain.RenderPlanInput{{ArtifactID: artifactID, Audio: &input}},
		Output: domain.RenderOutputPolicy{
			AudioSampleCount: outputCount, Audio: domain.RenderAudioOutputPolicy{SampleRate: 48_000},
		},
		Audio: []domain.RenderAudioInstruction{
			audioDecodeInstruction(t, artifactID, streamID, 0, 9, 2, 1),
		},
	}
	_, err := planAudioDecodeLanes(plan, map[string]audioDecodeSource{
		artifactID.String(): {inputIndex: 0, input: input},
	}, 10)
	if !errors.Is(err, ErrAudioSourceRangeInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestOutputSampleRangeUsesExactHalfOpenGrid(t *testing.T) {
	start := mustSourceMapTime(t, 1, 96_000)
	duration := mustSourceMapTime(t, 1, 48_000)
	first, after, err := outputSampleRange(domain.TimeRange{Start: start, Duration: duration}, 10)
	if err != nil || first != 1 || after != 2 {
		t.Fatalf("first=%d after=%d err=%v", first, after, err)
	}
}

func audioDecodeInstruction(
	t *testing.T,
	artifactID domain.ArtifactID,
	streamID domain.SourceStreamID,
	timelineStart, sourceStart, duration int64,
	layer uint16,
) domain.RenderAudioInstruction {
	t.Helper()
	clipValue := 500 + timelineStart*10 + int64(layer)
	clipID := mustRenderID(t, domain.ParseClipID,
		"00000000-0000-7000-8000-"+leftPadDecodePlanID(clipValue))
	return domain.RenderAudioInstruction{
		ClipID: clipID, Layer: layer, InputArtifactID: artifactID, SourceStreamID: streamID,
		SourceRange: domain.TimeRange{
			Start: audioSampleTime(t, sourceStart), Duration: audioSampleTime(t, duration),
		},
		TimelineRange: domain.TimeRange{
			Start: audioSampleTime(t, timelineStart), Duration: audioSampleTime(t, duration),
		},
	}
}

func audioSampleTime(t *testing.T, samples int64) domain.RationalTime {
	t.Helper()
	return mustSourceMapTime(t, samples, domain.SequencePreviewAudioSampleRate)
}
