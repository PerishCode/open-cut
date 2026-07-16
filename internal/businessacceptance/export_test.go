package businessacceptance

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestActorExportsCommittedSequenceOnlyThroughRecursiveCLI(t *testing.T) {
	projectID := "018f0000-0000-7000-8000-000000000501"
	sequenceID := "018f0000-0000-7000-8000-000000000502"
	runID := "018f0000-0000-7000-8000-000000000503"
	turnID := "018f0000-0000-7000-8000-000000000504"
	jobID := "018f0000-0000-7000-8000-000000000505"
	artifactID := "018f0000-0000-7000-8000-000000000506"
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	accepted := exportAcceptanceData(projectID, sequenceID, "4", jobID, "queued", nil)
	succeeded := exportAcceptanceData(projectID, sequenceID, "4", jobID, "succeeded", map[string]any{
		"id": artifactID, "verification": "passed",
		"semanticDuration": exactJSONTime("4", 1), "presentationDuration": exactJSONTime("4", 1),
		"canvasWidth": json.Number("160"), "canvasHeight": json.Number("90"),
		"frameRate": exactJSONTime("30", 1), "videoFrameCount": "120",
		"audioSampleRate": json.Number("48000"), "audioSampleCount": "192000",
		"videoCodec": "vp9", "audioCodec": "opus", "pixelFormat": "yuv420p", "channelLayout": "stereo",
		"byteSize": "4096", "contentDigest": digest,
	})
	contextArguments := acceptanceEditContext(projectID, sequenceID, runID, turnID)
	steps := []scriptedStep{{[]string{"--help"}, help([]any{child("export", false)}, nil)}}
	steps = append(steps, discoverySteps("export", "start", "show", "retry", "cancel")...)
	steps = append(steps,
		scriptedStep{append([]string{
			"export", "start", "--sequence-revision", "4", "--preset", acceptanceExportPreset,
			"--request-id", "installed-acceptance.export-start.v1",
		}, contextArguments...), result("accepted", accepted, nil)},
		scriptedStep{append([]string{"export", "show", "--job-id", jobID}, contextArguments...),
			result("succeeded", succeeded, nil)},
		scriptedStep{append([]string{"export", "show", "--job-id", jobID}, contextArguments...),
			result("succeeded", succeeded, nil)},
	)
	cli := &scriptedCLI{steps: steps}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observation, err := (Actor{CLI: cli}).ExportCommittedSequence(ctx, Observation{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: "4",
		RunID: runID, TurnID: turnID, RoughCutStatus: "applied", SequenceFrameStatus: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if observation.ExportJobID != jobID || observation.ExportRootJobID != jobID ||
		observation.ExportArtifactID != artifactID || observation.ExportStatus != "succeeded" ||
		observation.ExportPreset != acceptanceExportPreset || observation.ExportVerification != "passed" ||
		observation.ExportContentDigest != digest || observation.ExportByteSize != "4096" {
		t.Fatalf("export observation=%+v", observation)
	}
	if cli.index != len(cli.steps) {
		t.Fatalf("executed %d of %d CLI steps", cli.index, len(cli.steps))
	}
}

func exportAcceptanceData(
	projectID, sequenceID, revision, jobID, state string,
	artifact map[string]any,
) map[string]any {
	data := map[string]any{
		"projectId": projectID, "sequenceId": sequenceID, "sequenceRevision": revision,
		"preset": acceptanceExportPreset, "recovery": "none", "replayed": false, "activityCursor": "22",
		"job": map[string]any{
			"id": jobID, "rootJobId": jobID, "state": state, "progressBasisPoints": json.Number("0"),
			"createdAt": "2026-07-16T10:00:00Z", "updatedAt": "2026-07-16T10:00:01Z",
		},
	}
	if artifact != nil {
		data["artifact"] = artifact
	}
	return data
}

func exactJSONTime(value string, scale int32) map[string]any {
	return map[string]any{"value": value, "scale": json.Number(strconv.FormatInt(int64(scale), 10))}
}
