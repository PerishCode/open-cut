package businessacceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const maximumAcceptanceSequenceFrameBytes = 32 << 20

type sequenceFrameLeaseEvidence struct {
	jobID       string
	profile     string
	resourceIDs []string
	paths       []string
	expiresAt   []string
	ordinals    []string
}

func (actor Actor) InspectCommittedSequenceFrames(
	ctx context.Context,
	base Observation,
) (Observation, error) {
	if base.ProjectID == "" || base.SequenceID == "" || base.SequenceRevision == "" ||
		base.RunID == "" || base.TurnID == "" || base.RoughCutStatus != "applied" {
		return Observation{}, fmt.Errorf("Sequence-frame acceptance context is incomplete")
	}
	if err := actor.discoverLeaves(ctx, "sequence", "frames"); err != nil {
		return Observation{}, err
	}
	arguments := append([]string{
		"sequence", "frames", "--sequence-revision", base.SequenceRevision,
		"--time", "0/1", "--time", "1/1",
	}, editContext(base)...)
	result, err := actor.command(ctx, arguments...)
	if err != nil {
		return Observation{}, err
	}
	var ready sequenceFrameLeaseEvidence
	if err := poll(ctx, 250*time.Millisecond, func() (bool, error) {
		done, evidence, inspectErr := inspectSequenceFrameCommand(base, result)
		if inspectErr != nil || done {
			ready = evidence
			return done, inspectErr
		}
		jobID := sequenceFrameJobID(result.data)
		if jobID == "" {
			return false, fmt.Errorf("accepted Sequence-frame command omitted its job identity")
		}
		continueArguments := append([]string{
			"sequence", "frames", "--job-id", jobID,
		}, editContext(base)...)
		result, inspectErr = actor.command(ctx, continueArguments...)
		return false, inspectErr
	}); err != nil {
		return Observation{}, err
	}
	continueArguments := append([]string{
		"sequence", "frames", "--job-id", ready.jobID,
	}, editContext(base)...)
	reusedResult, err := actor.command(ctx, continueArguments...)
	if err != nil {
		return Observation{}, err
	}
	done, reused, err := inspectSequenceFrameCommand(base, reusedResult)
	if err != nil || !done {
		if err == nil {
			err = fmt.Errorf("ready Sequence-frame lease regressed during immediate reuse")
		}
		return Observation{}, err
	}
	if !sameSequenceFrameLease(ready, reused) {
		return Observation{}, fmt.Errorf("live whole-set Sequence-frame lease was not reused exactly")
	}
	observation := base
	observation.SequenceFrameJobID = ready.jobID
	observation.SequenceFrameStatus = "ready"
	observation.SequenceFrameProfile = ready.profile
	observation.SequenceFrameResourceIDs = append([]string(nil), ready.resourceIDs...)
	observation.SequenceFrameOrdinals = append([]string(nil), ready.ordinals...)
	return observation, nil
}

func inspectSequenceFrameCommand(
	base Observation,
	result commandResult,
) (bool, sequenceFrameLeaseEvidence, error) {
	data := record(result.data)
	job := record(data["job"])
	jobID, _ := job["id"].(string)
	profile, _ := data["profile"].(string)
	status, _ := data["status"].(string)
	if data["projectId"] != base.ProjectID || data["sequenceId"] != base.SequenceID ||
		data["sequenceRevision"] != base.SequenceRevision ||
		profile != "sequence-frame-srgb-png-v1" || jobID == "" {
		return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("Sequence-frame command changed its exact authority context")
	}
	for _, internal := range []string{"artifactId", "previewJobId", "renderPlanDigest", "executorVersion"} {
		if _, exposed := data[internal]; exposed {
			return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("Sequence-frame command exposed internal provenance %q", internal)
		}
	}
	if result.status == "failed" || status == "failed" {
		code, _ := job["terminalErrorCode"].(string)
		recovery, _ := data["recovery"].(string)
		if code == "" || recovery == "" {
			return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("failed Sequence-frame command omitted recovery evidence")
		}
		return false, sequenceFrameLeaseEvidence{}, fmt.Errorf(
			"Sequence-frame job failed (%s, recovery=%s)", code, recovery,
		)
	}
	samples, samplesOK := array(data["samples"])
	resources, resourcesOK := array(data["resources"])
	if !samplesOK || len(samples) != 2 || !sequenceFrameSamplesAreExact(samples) {
		return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("Sequence-frame command changed its exact sample set")
	}
	switch result.status {
	case "accepted":
		if status != "accepted" || !resourcesOK || len(resources) != 0 || data["recovery"] != "none" {
			return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("accepted Sequence-frame command is incomplete")
		}
		return false, sequenceFrameLeaseEvidence{jobID: jobID, profile: profile}, nil
	case "succeeded":
		if status != "ready" || !resourcesOK || len(resources) != len(samples) || data["recovery"] != "none" {
			return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("ready Sequence-frame command is incomplete")
		}
	default:
		return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("Sequence-frame command status = %q", result.status)
	}
	evidence := sequenceFrameLeaseEvidence{
		jobID: jobID, profile: profile,
		resourceIDs: make([]string, 0, len(resources)), paths: make([]string, 0, len(resources)),
		expiresAt: make([]string, 0, len(resources)), ordinals: make([]string, 0, len(resources)),
	}
	seenResources := make(map[string]bool, len(resources))
	seenPaths := make(map[string]bool, len(resources))
	for index, value := range resources {
		resource := record(value)
		sample := record(samples[index])
		resourceID, _ := resource["resourceId"].(string)
		path, _ := resource["readOnlyPath"].(string)
		expiry, _ := resource["expiresAt"].(string)
		ordinal, _ := resource["frameIndex"].(string)
		if resourceID == "" || seenResources[resourceID] || path == "" || seenPaths[path] ||
			resource["mimeType"] != "image/png" || resource["requestedTime"] == nil ||
			resource["sequenceTime"] == nil || ordinal == "" || ordinal != sample["frameIndex"] ||
			!exactTimeValueEqual(resource["requestedTime"], sample["requestedTime"]) ||
			!exactTimeValueEqual(resource["sequenceTime"], sample["sequenceTime"]) {
			return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("Sequence-frame resource %d changed its coordinate", index)
		}
		if parsed, parseErr := time.Parse(time.RFC3339Nano, expiry); parseErr != nil || !parsed.After(time.Now()) {
			return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("Sequence-frame resource %d has no live expiry", index)
		}
		if err := validateSequenceFrameFile(resource, path); err != nil {
			return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("Sequence-frame resource %d: %w", index, err)
		}
		seenResources[resourceID], seenPaths[path] = true, true
		evidence.resourceIDs = append(evidence.resourceIDs, resourceID)
		evidence.paths = append(evidence.paths, path)
		evidence.expiresAt = append(evidence.expiresAt, expiry)
		evidence.ordinals = append(evidence.ordinals, ordinal)
	}
	first, firstErr := strconv.ParseUint(evidence.ordinals[0], 10, 64)
	second, secondErr := strconv.ParseUint(evidence.ordinals[1], 10, 64)
	if firstErr != nil || secondErr != nil || first >= second {
		return false, sequenceFrameLeaseEvidence{}, fmt.Errorf("Sequence-frame ordinals are not strictly increasing")
	}
	return true, evidence, nil
}

func sequenceFrameSamplesAreExact(samples []any) bool {
	expected := []ExactTimeEvidence{{Value: "0", Scale: 1}, {Value: "1", Scale: 1}}
	for index, value := range samples {
		sample := record(value)
		instant, ok := parseExactTime(sample["requestedTime"])
		if !ok || instant != expected[index] {
			return false
		}
		if _, ok := parseExactTime(sample["sequenceTime"]); !ok {
			return false
		}
		if ordinal, _ := sample["frameIndex"].(string); ordinal == "" {
			return false
		}
	}
	return true
}

func exactTimeValueEqual(left, right any) bool {
	leftTime, leftOK := parseExactTime(left)
	rightTime, rightOK := parseExactTime(right)
	return leftOK && rightOK && leftTime == rightTime
}

func validateSequenceFrameFile(resource map[string]any, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("read-only path is not clean and absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumAcceptanceSequenceFrameBytes {
		return fmt.Errorf("read-only path is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximumAcceptanceSequenceFrameBytes+1))
	if err != nil || len(data) == 0 || len(data) > maximumAcceptanceSequenceFrameBytes {
		return fmt.Errorf("read-only PNG is unavailable or oversized")
	}
	byteSize, _ := resource["byteSize"].(string)
	parsedSize, sizeErr := strconv.ParseUint(byteSize, 10, 64)
	digest, _ := resource["sha256"].(string)
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	if sizeErr != nil || parsedSize != uint64(len(data)) || digest != hash {
		return fmt.Errorf("read-only PNG authority does not match its bytes")
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "png" || decoded.Bounds().Dx() <= 0 || decoded.Bounds().Dy() <= 0 ||
		decoded.Bounds().Dx() > 1280 || decoded.Bounds().Dy() > 1280 {
		return fmt.Errorf("read-only resource is not a bounded PNG")
	}
	for y := decoded.Bounds().Min.Y; y < decoded.Bounds().Max.Y; y++ {
		for x := decoded.Bounds().Min.X; x < decoded.Bounds().Max.X; x++ {
			_, _, _, alpha := decoded.At(x, y).RGBA()
			if alpha != 0xffff {
				return fmt.Errorf("read-only PNG is not opaque")
			}
		}
	}
	return nil
}

func sequenceFrameJobID(data any) string {
	id, _ := record(record(data)["job"])["id"].(string)
	return id
}

func sameSequenceFrameLease(left, right sequenceFrameLeaseEvidence) bool {
	return left.jobID == right.jobID && left.profile == right.profile &&
		sameStringSlices(left.resourceIDs, right.resourceIDs) && sameStringSlices(left.paths, right.paths) &&
		sameStringSlices(left.expiresAt, right.expiresAt) && sameStringSlices(left.ordinals, right.ordinals)
}

func sameStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
