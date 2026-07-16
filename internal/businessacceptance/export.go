package businessacceptance

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const acceptanceExportPreset = "webm-vp9-opus-v1"

type exportEvidence struct {
	jobID         string
	rootJobID     string
	artifactID    string
	preset        string
	verification  string
	contentDigest string
	byteSize      string
}

func (actor Actor) ExportCommittedSequence(
	ctx context.Context,
	base Observation,
) (Observation, error) {
	if base.ProjectID == "" || base.SequenceID == "" || base.SequenceRevision == "" ||
		base.RunID == "" || base.TurnID == "" || base.RoughCutStatus != "applied" ||
		base.SequenceFrameStatus != "ready" {
		return Observation{}, fmt.Errorf("export acceptance context is incomplete")
	}
	root, err := actor.help(ctx, "--help")
	if err != nil {
		return Observation{}, err
	}
	if !hasChild(root, "export", false) {
		return Observation{}, fmt.Errorf("root discovery omits export")
	}
	if err := actor.discoverLeaves(ctx, "export", "start", "show", "retry", "cancel"); err != nil {
		return Observation{}, err
	}
	arguments := append([]string{
		"export", "start", "--sequence-revision", base.SequenceRevision,
		"--preset", acceptanceExportPreset,
		"--request-id", "installed-acceptance.export-start.v1",
	}, editContext(base)...)
	result, err := actor.command(ctx, arguments...)
	if err != nil {
		return Observation{}, err
	}
	var completed exportEvidence
	if err := poll(ctx, 250*time.Millisecond, func() (bool, error) {
		done, evidence, inspectErr := inspectExportCommand(base, result)
		if inspectErr != nil || done {
			completed = evidence
			return done, inspectErr
		}
		if evidence.jobID == "" {
			return false, fmt.Errorf("accepted export omitted its job identity")
		}
		showArguments := append([]string{
			"export", "show", "--job-id", evidence.jobID,
		}, editContext(base)...)
		result, inspectErr = actor.command(ctx, showArguments...)
		return false, inspectErr
	}); err != nil {
		return Observation{}, err
	}
	showArguments := append([]string{
		"export", "show", "--job-id", completed.jobID,
	}, editContext(base)...)
	shown, err := actor.command(ctx, showArguments...)
	if err != nil {
		return Observation{}, err
	}
	done, confirmed, err := inspectExportCommand(base, shown)
	if err != nil || !done {
		if err == nil {
			err = fmt.Errorf("completed export regressed during durable readback")
		}
		return Observation{}, err
	}
	if confirmed != completed {
		return Observation{}, fmt.Errorf("durable export readback changed its verified identity")
	}
	observation := base
	observation.ExportJobID = completed.jobID
	observation.ExportRootJobID = completed.rootJobID
	observation.ExportArtifactID = completed.artifactID
	observation.ExportStatus = "succeeded"
	observation.ExportPreset = completed.preset
	observation.ExportVerification = completed.verification
	observation.ExportContentDigest = completed.contentDigest
	observation.ExportByteSize = completed.byteSize
	return observation, nil
}

func inspectExportCommand(base Observation, result commandResult) (bool, exportEvidence, error) {
	data := record(result.data)
	job := record(data["job"])
	jobID, _ := job["id"].(string)
	rootJobID, _ := job["rootJobId"].(string)
	state, _ := job["state"].(string)
	preset, _ := data["preset"].(string)
	evidence := exportEvidence{jobID: jobID, rootJobID: rootJobID, preset: preset}
	if data["projectId"] != base.ProjectID || data["sequenceId"] != base.SequenceID ||
		data["sequenceRevision"] != base.SequenceRevision || preset != acceptanceExportPreset ||
		jobID == "" || rootJobID != jobID || stringValue(data["activityCursor"]) == "" {
		return false, evidence, fmt.Errorf("export command changed its exact authority or root lineage")
	}
	if err := rejectExportInternalProjection(data, job); err != nil {
		return false, evidence, err
	}
	if result.status == "failed" || state == "failed" || state == "cancelled" {
		code, _ := job["terminalErrorCode"].(string)
		recovery, _ := data["recovery"].(string)
		if state == "cancelled" {
			code = "cancelled"
		}
		if code == "" || recovery == "" {
			return false, evidence, fmt.Errorf("terminal export omitted typed recovery evidence")
		}
		return false, evidence, fmt.Errorf("export failed (%s, recovery=%s)", code, recovery)
	}
	switch result.status {
	case "accepted":
		if state != "blocked" && state != "queued" && state != "running" {
			return false, evidence, fmt.Errorf("accepted export has invalid job state %q", state)
		}
		if data["artifact"] != nil || data["recovery"] != "none" {
			return false, evidence, fmt.Errorf("accepted export exposed an artifact or recovery action")
		}
		return false, evidence, nil
	case "succeeded":
		if state != "succeeded" || data["recovery"] != "none" {
			return false, evidence, fmt.Errorf("successful export has invalid terminal state")
		}
	default:
		return false, evidence, fmt.Errorf("export command status = %q", result.status)
	}
	artifact := record(data["artifact"])
	evidence.artifactID, _ = artifact["id"].(string)
	evidence.verification, _ = artifact["verification"].(string)
	evidence.contentDigest, _ = artifact["contentDigest"].(string)
	evidence.byteSize, _ = artifact["byteSize"].(string)
	if err := validateExportArtifact(artifact, evidence); err != nil {
		return false, evidence, err
	}
	return true, evidence, nil
}

func rejectExportInternalProjection(data, job map[string]any) error {
	for _, key := range []string{
		"path", "canonicalPath", "renderPlan", "renderPlanDigest", "rendererVersion", "rendererTarget",
		"material", "materials", "datadir", "sidecar", "capability", "attemptRoot",
	} {
		if _, exposed := data[key]; exposed {
			return fmt.Errorf("export command exposed internal provenance %q", key)
		}
		if _, exposed := job[key]; exposed {
			return fmt.Errorf("export job exposed internal provenance %q", key)
		}
	}
	return nil
}

func validateExportArtifact(artifact map[string]any, evidence exportEvidence) error {
	if evidence.artifactID == "" || evidence.verification != "passed" ||
		!validAcceptanceDigest(evidence.contentDigest) || !positiveDecimal(evidence.byteSize) ||
		artifact["videoCodec"] != "vp9" || artifact["audioCodec"] != "opus" ||
		artifact["pixelFormat"] != "yuv420p" || artifact["channelLayout"] != "stereo" ||
		numberString(artifact["audioSampleRate"]) != "48000" ||
		!positiveDecimal(numberString(artifact["canvasWidth"])) ||
		!positiveDecimal(numberString(artifact["canvasHeight"])) ||
		!positiveDecimal(stringValue(artifact["videoFrameCount"])) ||
		!positiveDecimal(stringValue(artifact["audioSampleCount"])) {
		return fmt.Errorf("verified export artifact is incomplete")
	}
	width, widthErr := strconv.ParseUint(numberString(artifact["canvasWidth"]), 10, 32)
	height, heightErr := strconv.ParseUint(numberString(artifact["canvasHeight"]), 10, 32)
	semantic, semanticOK := parseExactTime(artifact["semanticDuration"])
	presentation, presentationOK := parseExactTime(artifact["presentationDuration"])
	frameRate, frameRateOK := parseExactTime(artifact["frameRate"])
	if widthErr != nil || heightErr != nil || width < 2 || height < 2 || width%2 != 0 || height%2 != 0 ||
		!semanticOK || !presentationOK || !frameRateOK || !exactTimePositive(semantic) ||
		!exactTimePositive(presentation) || !exactTimePositive(frameRate) ||
		exactTimeCompare(presentation, semantic) < 0 {
		return fmt.Errorf("verified export media facts are invalid")
	}
	for _, key := range []string{"path", "readOnlyPath", "mediaPath", "manifestPath"} {
		if _, exposed := artifact[key]; exposed {
			return fmt.Errorf("export artifact exposed internal path %q", key)
		}
	}
	return nil
}

func validAcceptanceDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func positiveDecimal(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatUint(parsed, 10) == value
}
