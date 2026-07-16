package businessacceptance

import (
	"context"
	"testing"
)

func TestActorObservesProductionTranscriptOnlyThroughCLI(t *testing.T) {
	projectID := "018f0000-0000-7000-8000-000000000101"
	assetID := "018f0000-0000-7000-8000-000000000102"
	streamID := "018f0000-0000-7000-8000-000000000103"
	artifactID := "018f0000-0000-7000-8000-000000000104"
	segmentID := "018f0000-0000-7000-8000-000000000105"
	tokenOneID := "018f0000-0000-7000-8000-000000000106"
	tokenTwoID := "018f0000-0000-7000-8000-000000000107"
	fingerprint := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	cli := &scriptedCLI{steps: []scriptedStep{
		{[]string{"transcript", "--help"}, help([]any{child("read", true)}, nil)},
		{[]string{"transcript", "read", "--help"}, executableHelp("sha256:transcript-read")},
		{[]string{"asset", "inspect", "--project-id", projectID, "--asset-id", assetID}, result(
			"succeeded",
			map[string]any{"asset": map[string]any{
				"id": assetID, "projectId": projectID, "availability": "online",
				"fingerprint": fingerprint,
				"jobs":        []any{mediaJob("transcript", "succeeded", artifactID, []any{})},
				"artifacts": []any{map[string]any{
					"id": artifactID, "kind": "transcript", "state": "ready",
					"inputFingerprint": fingerprint,
				}},
			}},
			nil,
		)},
		{[]string{
			"transcript", "read", "--project-id", projectID,
			"--asset-id", assetID, "--artifact-id", artifactID, "--limit", "20",
		}, result("succeeded", map[string]any{
			"schema": "open-cut/transcript-read/v1",
			"artifact": map[string]any{
				"id": artifactID, "assetId": assetID, "sourceStreamId": streamID,
				"recognitionProfile":    acceptanceTranscriptResource,
				"modelName":             acceptanceTranscriptResource,
				"modelVersion":          "ggml-small-v3-q5_1",
				"detectedLanguage":      "en",
				"normalizedSampleCount": "146064",
			},
			"segments": []any{map[string]any{
				"id": segmentID, "text": "Alpha Bravo. Spoken ideas become an editable story.",
				"tokens": []any{
					map[string]any{
						"id": tokenOneID, "text": "Alpha Bravo. ",
						"sourceRange": map[string]any{
							"start":    map[string]any{"value": "0", "scale": 1},
							"duration": map[string]any{"value": "1", "scale": 1},
						},
					},
					map[string]any{
						"id": tokenTwoID, "text": "Spoken ideas become an editable story.",
						"sourceRange": map[string]any{
							"start":    map[string]any{"value": "1", "scale": 1},
							"duration": map[string]any{"value": "2", "scale": 1},
						},
					},
				},
				"sourceRange": map[string]any{
					"start":    map[string]any{"value": "0", "scale": 1},
					"duration": map[string]any{"value": "3", "scale": 1},
				},
			}},
			"corrections": []any{},
		}, nil)},
	}}
	base := Observation{
		ProjectID:          projectID,
		AssetID:            assetID,
		AssetState:         "online",
		MediaStreamID:      streamID,
		AssetFingerprint:   fingerprint,
		TranscriptJobState: "blocked",
	}
	observed, err := (Actor{CLI: cli}).ObserveProductionTranscript(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if observed.TranscriptJobState != "succeeded" || observed.TranscriptArtifact != artifactID ||
		observed.TranscriptSegment != segmentID || observed.TranscriptSegments != 1 ||
		observed.TranscriptTokens != 2 || observed.TranscriptSourceRange == nil ||
		*observed.TranscriptSourceRange != (ExactRangeEvidence{
			Start:    ExactTimeEvidence{Value: "0", Scale: 1},
			Duration: ExactTimeEvidence{Value: "3", Scale: 1},
		}) ||
		observed.TranscriptLanguage != "en" || observed.TranscriptModel != "ggml-small-v3-q5_1" ||
		observed.TranscriptRead != "succeeded" {
		t.Fatalf("transcript observation=%+v", observed)
	}
	if cli.index != len(cli.steps) {
		t.Fatalf("executed %d of %d CLI steps", cli.index, len(cli.steps))
	}
}
