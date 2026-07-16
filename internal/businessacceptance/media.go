package businessacceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const acceptanceTranscriptResource = "whisper-small-multilingual-v1"

func (actor Actor) ObserveMediaPipeline(
	ctx context.Context,
	base Observation,
	expectedChannels string,
	expectedVideo bool,
) (Observation, error) {
	if base.ProjectID == "" || base.AssetID == "" || base.AssetState != "online" || expectedChannels == "" {
		return Observation{}, fmt.Errorf("media acceptance context is incomplete")
	}
	if err := actor.discoverLeaves(ctx, "asset", "inspect"); err != nil {
		return Observation{}, err
	}
	observed := base
	err := poll(ctx, 100*time.Millisecond, func() (bool, error) {
		result, commandErr := actor.command(
			ctx, "asset", "inspect", "--project-id", base.ProjectID, "--asset-id", base.AssetID,
		)
		if commandErr != nil {
			return false, commandErr
		}
		if result.status != "succeeded" {
			return false, fmt.Errorf("asset inspect status = %q", result.status)
		}
		candidate, ready, inspectErr := inspectMediaPipeline(result.data, base, expectedChannels, expectedVideo)
		if ready {
			observed = candidate
		}
		return ready, inspectErr
	})
	if err != nil {
		return Observation{}, err
	}
	return observed, nil
}

func inspectMediaPipeline(
	data any,
	base Observation,
	expectedChannels string,
	expectedVideo bool,
) (Observation, bool, error) {
	asset := record(record(data)["asset"])
	fingerprint, _ := asset["fingerprint"].(string)
	accepted, _ := asset["acceptedFingerprint"].(string)
	if asset["id"] != base.AssetID || asset["projectId"] != base.ProjectID ||
		asset["availability"] != "online" || fingerprint == "" || accepted != fingerprint ||
		!strings.HasPrefix(fingerprint, "sha256:") {
		return Observation{}, false, fmt.Errorf("asset inspect did not preserve the online Asset identity")
	}
	jobs, ok := array(asset["jobs"])
	if !ok || len(jobs) != 5 {
		return Observation{}, false, fmt.Errorf("asset inspect did not return the five initial media jobs")
	}
	byKind := make(map[string]map[string]any, len(jobs))
	for _, value := range jobs {
		job := record(value)
		kind, _ := job["kind"].(string)
		if kind == "" || byKind[kind] != nil {
			return Observation{}, false, fmt.Errorf("asset inspect returned an invalid media job set")
		}
		byKind[kind] = job
	}
	for _, kind := range []string{"identify", "probe", "proxy", "waveform", "transcript"} {
		if byKind[kind] == nil {
			return Observation{}, false, fmt.Errorf("asset inspect omitted the %s job", kind)
		}
	}
	for _, kind := range []string{"identify", "probe", "proxy"} {
		state, _ := byKind[kind]["state"].(string)
		if state == "failed" || state == "cancelled" {
			return Observation{}, false, fmt.Errorf("installed %s media job reached %s", kind, state)
		}
		if state != "succeeded" {
			return Observation{}, false, nil
		}
		if numberString(byKind[kind]["progressBasisPoints"]) != "10000" {
			return Observation{}, false, fmt.Errorf("succeeded %s job has incomplete progress", kind)
		}
	}
	if byKind["waveform"]["state"] != "blocked" ||
		!hasMediaPrerequisite(byKind["waveform"], "executor-required", "", "media-executor/waveform") {
		return Observation{}, false, fmt.Errorf("waveform job did not expose its missing executor prerequisite")
	}
	if byKind["transcript"]["state"] != "blocked" ||
		!hasMediaPrerequisite(byKind["transcript"], "model-required", acceptanceTranscriptResource, "") {
		return Observation{}, false, fmt.Errorf("transcript job did not expose its missing model prerequisite")
	}
	facts := record(asset["facts"])
	container, _ := facts["container"].(string)
	streams, streamsOK := array(facts["streams"])
	_, aliasesOK := array(facts["containerAliases"])
	expectedStreams := 1
	if expectedVideo {
		expectedStreams = 2
	}
	if container == "" || !streamsOK || len(streams) != expectedStreams || !aliasesOK {
		return Observation{}, false, fmt.Errorf("asset inspect omitted bounded media facts")
	}
	var audioStreamID, videoStreamID string
	var audioDescriptor, videoDescriptor map[string]any
	for _, value := range streams {
		stream := record(value)
		descriptor := record(stream["descriptor"])
		id, _ := stream["id"].(string)
		switch descriptor["mediaType"] {
		case "audio":
			audioStreamID, audioDescriptor = id, descriptor
		case "video":
			videoStreamID, videoDescriptor = id, descriptor
		}
	}
	audio := record(audioDescriptor["audio"])
	if audioStreamID == "" || audioDescriptor["codec"] == "" ||
		numberString(audio["sampleRate"]) != "48000" || numberString(audio["channels"]) != expectedChannels {
		return Observation{}, false, fmt.Errorf("installed fixture facts did not describe the source audio stream")
	}
	if _, dispositionsOK := array(audioDescriptor["dispositions"]); !dispositionsOK {
		return Observation{}, false, fmt.Errorf("source stream dispositions are not an array")
	}
	if expectedVideo {
		video := record(videoDescriptor["video"])
		if videoStreamID == "" || videoDescriptor["codec"] == "" ||
			numberString(video["width"]) != "160" || numberString(video["height"]) != "90" {
			return Observation{}, false, fmt.Errorf("installed fixture facts did not describe the source video stream")
		}
		if _, dispositionsOK := array(videoDescriptor["dispositions"]); !dispositionsOK {
			return Observation{}, false, fmt.Errorf("video stream dispositions are not an array")
		}
	}
	factsArtifactID, _ := byKind["probe"]["resultArtifactId"].(string)
	proxyArtifactID, _ := byKind["proxy"]["resultArtifactId"].(string)
	artifacts, artifactsOK := array(asset["artifacts"])
	if !artifactsOK || factsArtifactID == "" || proxyArtifactID == "" ||
		!hasReadyArtifact(artifacts, factsArtifactID, "media-facts", fingerprint) ||
		!hasReadyArtifact(artifacts, proxyArtifactID, "proxy", fingerprint) {
		return Observation{}, false, fmt.Errorf("media jobs and ready artifacts do not agree")
	}
	result := base
	result.AssetFingerprint = fingerprint
	result.MediaContainer = container
	result.MediaStreamID = audioStreamID
	result.VideoStreamID = videoStreamID
	result.MediaStreamType = "audio"
	result.MediaSampleRate = "48000"
	result.MediaChannels = expectedChannels
	result.FactsArtifactID = factsArtifactID
	result.ProxyArtifactID = proxyArtifactID
	result.IdentifyJobState = "succeeded"
	result.ProbeJobState = "succeeded"
	result.ProxyJobState = "succeeded"
	result.WaveformJobState = "blocked"
	result.TranscriptJobState = "blocked"
	return result, true, nil
}

func hasMediaPrerequisite(job map[string]any, kind, resource, capability string) bool {
	prerequisites, ok := array(job["prerequisites"])
	if !ok {
		return false
	}
	for _, value := range prerequisites {
		candidate := record(value)
		if candidate["kind"] == kind && stringValue(candidate["resourceId"]) == resource &&
			stringValue(candidate["capability"]) == capability {
			return true
		}
	}
	return false
}

func hasReadyArtifact(values []any, id, kind, fingerprint string) bool {
	for _, value := range values {
		artifact := record(value)
		if artifact["id"] == id && artifact["kind"] == kind && artifact["state"] == "ready" &&
			artifact["inputFingerprint"] == fingerprint {
			return true
		}
	}
	return false
}

func array(value any) ([]any, bool) {
	result, ok := value.([]any)
	return result, ok
}

func numberString(value any) string {
	number, _ := value.(json.Number)
	return number.String()
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}
