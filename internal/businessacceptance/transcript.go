package businessacceptance

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
)

func (actor Actor) ObserveProductionTranscript(ctx context.Context, base Observation) (Observation, error) {
	if base.ProjectID == "" || base.AssetID == "" || base.MediaStreamID == "" ||
		base.TranscriptJobState != "blocked" {
		return Observation{}, fmt.Errorf("transcript acceptance context is incomplete")
	}
	if err := actor.discoverLeaves(ctx, "transcript", "read"); err != nil {
		return Observation{}, err
	}
	artifactID := ""
	err := poll(ctx, 250*time.Millisecond, func() (bool, error) {
		result, commandErr := actor.command(
			ctx, "asset", "inspect", "--project-id", base.ProjectID, "--asset-id", base.AssetID,
		)
		if commandErr != nil {
			return false, commandErr
		}
		if result.status != "succeeded" {
			return false, fmt.Errorf("asset inspect status = %q while waiting for transcript", result.status)
		}
		candidate, ready, inspectErr := readyTranscriptArtifact(result.data, base)
		if ready {
			artifactID = candidate
		}
		return ready, inspectErr
	})
	if err != nil {
		return Observation{}, err
	}
	page, err := actor.command(
		ctx, "transcript", "read", "--project-id", base.ProjectID,
		"--asset-id", base.AssetID, "--artifact-id", artifactID, "--limit", "20",
	)
	if err != nil {
		return Observation{}, err
	}
	if page.status != "succeeded" {
		return Observation{}, fmt.Errorf("transcript read status = %q", page.status)
	}
	data := record(page.data)
	artifact := record(data["artifact"])
	segments, segmentsOK := array(data["segments"])
	_, correctionsOK := array(data["corrections"])
	modelVersion, _ := artifact["modelVersion"].(string)
	language, _ := artifact["detectedLanguage"].(string)
	if data["schema"] != "open-cut/transcript-read/v1" || artifact["id"] != artifactID ||
		artifact["assetId"] != base.AssetID || artifact["sourceStreamId"] != base.MediaStreamID ||
		artifact["recognitionProfile"] != acceptanceTranscriptResource ||
		artifact["modelName"] != acceptanceTranscriptResource || modelVersion == "" || language == "" ||
		stringValue(artifact["normalizedSampleCount"]) == "" || !segmentsOK || len(segments) == 0 ||
		!correctionsOK {
		return Observation{}, fmt.Errorf("bounded transcript read omitted its production artifact or segments")
	}
	firstID := ""
	firstText := ""
	var firstRange *ExactRangeEvidence
	firstTokenCount := 0
	var recognized strings.Builder
	for index, value := range segments {
		segment := record(value)
		id, _ := segment["id"].(string)
		text, _ := segment["text"].(string)
		evidenceRange, tokenCount, evidenceErr := transcriptSegmentEvidence(segment)
		if id == "" || text == "" || evidenceErr != nil {
			return Observation{}, fmt.Errorf("bounded transcript segment %d is incomplete", index)
		}
		if index == 0 {
			firstID = id
			firstText = text
			firstRange = &evidenceRange
			firstTokenCount = tokenCount
		}
		recognized.WriteString(text)
	}
	normalized := lettersAndNumbers(recognized.String())
	for _, anchor := range []string{"alphabravo", "spokenideas", "editablestory"} {
		if !strings.Contains(normalized, anchor) {
			return Observation{}, fmt.Errorf("production transcript omitted speech fixture anchor %q", anchor)
		}
	}
	result := base
	result.TranscriptJobState = "succeeded"
	result.TranscriptArtifact = artifactID
	result.TranscriptSegment = firstID
	result.TranscriptSegmentIDs = []string{firstID}
	result.TranscriptSegments = len(segments)
	result.TranscriptTokens = firstTokenCount
	result.TranscriptLanguage = language
	result.TranscriptModel = modelVersion
	result.TranscriptRead = "succeeded"
	result.TranscriptText = firstText
	result.TranscriptSourceRange = firstRange
	return result, nil
}

func transcriptSegmentEvidence(segment map[string]any) (ExactRangeEvidence, int, error) {
	segmentRange, segmentRangeOK := parseExactRange(segment["sourceRange"])
	tokens, tokensOK := array(segment["tokens"])
	text, _ := segment["text"].(string)
	if !segmentRangeOK || !tokensOK || len(tokens) == 0 || text == "" {
		return ExactRangeEvidence{}, 0, fmt.Errorf("transcript segment evidence is incomplete")
	}
	var lexical strings.Builder
	var firstStart, previousEnd ExactTimeEvidence
	for index, value := range tokens {
		token := record(value)
		id, _ := token["id"].(string)
		tokenText, _ := token["text"].(string)
		rangeValue, rangeOK := parseExactRange(token["sourceRange"])
		if id == "" || tokenText == "" || !rangeOK {
			return ExactRangeEvidence{}, 0, fmt.Errorf("transcript token evidence is invalid")
		}
		end, endOK := exactRangeEnd(rangeValue)
		if !endOK ||
			(index > 0 && exactTimeCompare(rangeValue.Start, previousEnd) < 0) {
			return ExactRangeEvidence{}, 0, fmt.Errorf("transcript token evidence is invalid")
		}
		if index == 0 {
			firstStart = rangeValue.Start
		}
		previousEnd = end
		lexical.WriteString(tokenText)
	}
	if lexical.String() != text {
		return ExactRangeEvidence{}, 0, fmt.Errorf("transcript tokens do not reconstruct the segment")
	}
	evidenceRange, rangeOK := exactRangeFromBounds(firstStart, previousEnd)
	segmentEnd, segmentEndOK := exactRangeEnd(segmentRange)
	evidenceEnd, evidenceEndOK := exactRangeEnd(evidenceRange)
	if !rangeOK || !segmentEndOK || !evidenceEndOK ||
		exactTimeCompare(evidenceRange.Start, segmentRange.Start) < 0 ||
		exactTimeCompare(evidenceEnd, segmentEnd) > 0 {
		return ExactRangeEvidence{}, 0, fmt.Errorf("transcript token span escapes the segment")
	}
	return evidenceRange, len(tokens), nil
}

func readyTranscriptArtifact(data any, base Observation) (string, bool, error) {
	asset := record(record(data)["asset"])
	if asset["id"] != base.AssetID || asset["projectId"] != base.ProjectID ||
		asset["availability"] != "online" {
		return "", false, fmt.Errorf("transcript polling changed the Asset identity")
	}
	fingerprint, _ := asset["fingerprint"].(string)
	jobs, jobsOK := array(asset["jobs"])
	if fingerprint == "" || (base.AssetFingerprint != "" && fingerprint != base.AssetFingerprint) || !jobsOK {
		return "", false, fmt.Errorf("transcript polling omitted Asset work state")
	}
	for _, value := range jobs {
		job := record(value)
		if job["kind"] != "transcript" {
			continue
		}
		state, _ := job["state"].(string)
		if state == "failed" || state == "cancelled" {
			code := stringValue(job["terminalErrorCode"])
			if state == "failed" && code == "" {
				return "", false, fmt.Errorf("failed production transcript omitted its terminal error code")
			}
			if code != "" {
				return "", false, fmt.Errorf("production transcript job reached %s (%s)", state, code)
			}
			return "", false, fmt.Errorf("production transcript job reached %s", state)
		}
		if state != "succeeded" {
			return "", false, nil
		}
		artifactID, _ := job["resultArtifactId"].(string)
		artifacts, artifactsOK := array(asset["artifacts"])
		if artifactID == "" || !artifactsOK ||
			!hasReadyArtifact(artifacts, artifactID, "transcript", fingerprint) {
			return "", false, fmt.Errorf("transcript job and ready artifact do not agree")
		}
		return artifactID, true, nil
	}
	return "", false, fmt.Errorf("transcript polling omitted the durable job")
}

func lettersAndNumbers(value string) string {
	var result strings.Builder
	for _, current := range strings.ToLower(value) {
		if unicode.IsLetter(current) || unicode.IsNumber(current) {
			result.WriteRune(current)
		}
	}
	return result.String()
}
