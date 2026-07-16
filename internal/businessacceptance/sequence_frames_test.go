package businessacceptance

import (
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActorInspectsCommittedSequenceFramesOnlyThroughCLI(t *testing.T) {
	projectID := "018f0000-0000-7000-8000-000000000401"
	sequenceID := "018f0000-0000-7000-8000-000000000402"
	runID := "018f0000-0000-7000-8000-000000000403"
	turnID := "018f0000-0000-7000-8000-000000000404"
	jobID := "018f0000-0000-7000-8000-000000000405"
	resourceIDs := []string{
		"018f0000-0000-7000-8000-000000000406",
		"018f0000-0000-7000-8000-000000000407",
	}
	root := t.TempDir()
	paths := []string{filepath.Join(root, "frame-000.png"), filepath.Join(root, "frame-001.png")}
	resources := make([]any, 0, len(paths))
	samples := []any{
		sequenceFrameSample(ExactTimeEvidence{Value: "0", Scale: 1}, "0"),
		sequenceFrameSample(ExactTimeEvidence{Value: "1", Scale: 1}, "30"),
	}
	expiry := time.Now().Add(4 * time.Minute).UTC().Format(time.RFC3339Nano)
	for index, path := range paths {
		data := writeAcceptancePNG(t, path, color.NRGBA{R: uint8(index * 127), G: 80, B: 200, A: 0xff})
		resources = append(resources, map[string]any{
			"resourceId": resourceIDs[index], "mimeType": "image/png",
			"byteSize": fmt.Sprintf("%d", len(data)), "sha256": fmt.Sprintf("sha256:%x", sha256.Sum256(data)),
			"requestedTime": record(samples[index])["requestedTime"],
			"sequenceTime":  record(samples[index])["sequenceTime"],
			"frameIndex":    record(samples[index])["frameIndex"],
			"readOnlyPath":  path, "expiresAt": expiry,
		})
	}
	accepted := sequenceFrameAcceptanceData(
		projectID, sequenceID, "2", jobID, "accepted", "queued", samples, []any{},
	)
	ready := sequenceFrameAcceptanceData(
		projectID, sequenceID, "2", jobID, "ready", "succeeded", samples, resources,
	)
	contextArguments := acceptanceEditContext(projectID, sequenceID, runID, turnID)
	steps := discoverySteps("sequence", "frames")
	steps = append(steps,
		scriptedStep{append([]string{
			"sequence", "frames", "--sequence-revision", "2", "--time", "0/1", "--time", "1/1",
		}, contextArguments...), result("accepted", accepted, nil)},
		scriptedStep{append([]string{
			"sequence", "frames", "--job-id", jobID,
		}, contextArguments...), result("succeeded", ready, nil)},
		scriptedStep{append([]string{
			"sequence", "frames", "--job-id", jobID,
		}, contextArguments...), result("succeeded", ready, nil)},
	)
	cli := &scriptedCLI{steps: steps}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observation, err := (Actor{CLI: cli}).InspectCommittedSequenceFrames(
		ctx,
		Observation{
			ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: "2",
			RunID: runID, TurnID: turnID, RoughCutStatus: "applied",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if observation.SequenceFrameJobID != jobID || observation.SequenceFrameStatus != "ready" ||
		observation.SequenceFrameProfile != "sequence-frame-srgb-png-v1" ||
		!sameStringSlices(observation.SequenceFrameResourceIDs, resourceIDs) ||
		!sameStringSlices(observation.SequenceFrameOrdinals, []string{"0", "30"}) {
		t.Fatalf("Sequence-frame observation=%+v", observation)
	}
	if cli.index != len(cli.steps) {
		t.Fatalf("executed %d of %d CLI steps", cli.index, len(cli.steps))
	}
}

func sequenceFrameAcceptanceData(
	projectID, sequenceID, revision, jobID, status, jobState string,
	samples, resources []any,
) map[string]any {
	return map[string]any{
		"status": status, "projectId": projectID, "sequenceId": sequenceID,
		"sequenceRevision": revision, "profile": "sequence-frame-srgb-png-v1",
		"samples": samples, "resources": resources, "recovery": "none",
		"job": map[string]any{
			"id": jobID, "state": jobState, "progressBasisPoints": 0,
			"createdAt": "2026-07-16T10:00:00Z", "updatedAt": "2026-07-16T10:00:00Z",
		},
		"activityCursor": "12",
	}
}

func sequenceFrameSample(instant ExactTimeEvidence, ordinal string) map[string]any {
	return map[string]any{
		"requestedTime": instant, "sequenceTime": instant, "frameIndex": ordinal,
	}
}

func writeAcceptancePNG(t *testing.T, path string, value color.NRGBA) []byte {
	t.Helper()
	frame := image.NewNRGBA(image.Rect(0, 0, 16, 9))
	for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
		for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
			frame.SetNRGBA(x, y, value)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, frame); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
