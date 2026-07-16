package businessacceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestActorCommitsTranscriptEvidenceAsLinkedAVRoughCut(t *testing.T) {
	projectID := "018f0000-0000-7000-8000-000000000301"
	sequenceID := "018f0000-0000-7000-8000-000000000302"
	videoTrackID := "018f0000-0000-7000-8000-000000000303"
	audioTrackID := "018f0000-0000-7000-8000-000000000304"
	documentID := "018f0000-0000-7000-8000-000000000305"
	rootID := "018f0000-0000-7000-8000-000000000306"
	assetID := "018f0000-0000-7000-8000-000000000307"
	videoStreamID := "018f0000-0000-7000-8000-000000000308"
	audioStreamID := "018f0000-0000-7000-8000-000000000309"
	transcriptID := "018f0000-0000-7000-8000-00000000030a"
	segmentID := "018f0000-0000-7000-8000-00000000030b"
	runID := "018f0000-0000-7000-8000-00000000030c"
	turnID := "018f0000-0000-7000-8000-00000000030d"
	excerptID := "018f0000-0000-7000-8000-00000000030e"
	excerptProposalID := "018f0000-0000-7000-8000-00000000030f"
	excerptTransactionID := "018f0000-0000-7000-8000-000000000310"
	roughProposalID := "018f0000-0000-7000-8000-000000000311"
	roughTransactionID := "018f0000-0000-7000-8000-000000000312"
	videoClipID := "018f0000-0000-7000-8000-000000000313"
	audioClipID := "018f0000-0000-7000-8000-000000000314"
	groupID := "018f0000-0000-7000-8000-000000000315"
	alignmentID := "018f0000-0000-7000-8000-000000000316"
	fingerprint := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	excerptDigest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	roughDigest := "sha256:abababababababababababababababababababababababababababababababab"
	outputDigest := "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	text := "Spoken ideas become an editable story."
	sourceRange := ExactRangeEvidence{
		Start:    ExactTimeEvidence{Value: "1", Scale: 10},
		Duration: ExactTimeEvidence{Value: "29", Scale: 10},
	}
	timelineRange := ExactRangeEvidence{
		Start:    ExactTimeEvidence{Value: "0", Scale: 1},
		Duration: sourceRange.Duration,
	}
	localRange := timelineRange
	base := Observation{
		ProjectID: projectID, ProjectRevision: "2", SequenceID: sequenceID, SequenceRevision: "1",
		VideoTrackID: videoTrackID, VideoTrackRevision: "1",
		AudioTrackID: audioTrackID, AudioTrackRevision: "1",
		NarrativeDocument: documentID, NarrativeRoot: rootID,
		AssetID: assetID, AssetFingerprint: fingerprint,
		VideoStreamID: videoStreamID, MediaStreamID: audioStreamID,
		TranscriptArtifact: transcriptID, TranscriptSegment: segmentID,
		TranscriptSegmentIDs: []string{segmentID}, TranscriptSegments: 1, TranscriptTokens: 7,
		TranscriptLanguage: "en", TranscriptText: text, TranscriptSourceRange: &sourceRange,
		RunID: runID, TurnID: turnID, RunStatus: "active",
	}
	excerpt := map[string]any{
		"id": excerptID, "revision": "1", "documentId": documentID,
		"parentId": rootID, "assetId": assetID, "acceptedFingerprint": fingerprint,
		"sourceRange": sourceRange, "language": "en", "effectiveText": text,
		"evidence": map[string]any{
			"artifactId": transcriptID, "sourceStreamId": audioStreamID,
			"segmentIds": []string{segmentID}, "correctionRevisions": []any{},
		},
		"tombstoned": false,
	}
	preconditions := []any{
		map[string]any{"kind": "sequence", "id": sequenceID, "revision": "1"},
		map[string]any{"kind": "narrative-node", "id": excerptID, "revision": "1"},
		map[string]any{"kind": "track", "id": videoTrackID, "revision": "1"},
		map[string]any{"kind": "track", "id": audioTrackID, "revision": "1"},
	}
	roughOutput := map[string]any{
		"sourceExcerptId": excerptID, "sourceRange": sourceRange, "timelineRange": timelineRange,
		"video": map[string]any{
			"clipAs": "installed_rough_video_001", "trackId": videoTrackID, "sourceStreamId": videoStreamID,
		},
		"audio": map[string]any{
			"clipAs": "installed_rough_audio_001", "trackId": audioTrackID, "sourceStreamId": audioStreamID,
		},
		"linkGroupAs": "installed_rough_group_001", "alignmentAs": "installed_rough_alignment_001",
	}
	roughOperation := map[string]any{
		"type": "derive-rough-cut", "roughCutPolicy": map[string]any{"id": "paper-edit-rough-cut-v1"},
		"roughCutTimelineStart": ExactTimeEvidence{Value: "0", Scale: 1},
		"roughCutLocalPrefix":   acceptanceRoughCutPrefix,
		"roughCutItems": []any{map[string]any{
			"sourceExcerptId": excerptID,
			"video": map[string]any{
				"trackId": videoTrackID, "sourceStreamId": videoStreamID,
			},
			"audio": map[string]any{
				"trackId": audioTrackID, "sourceStreamId": audioStreamID,
			},
		}},
		"derivedRoughCut": []any{roughOutput}, "roughCutOutputDigest": outputDigest,
	}
	steps := append([]scriptedStep{}, discoverySteps("narrative", "show")...)
	steps = append(steps, discoverySteps("entity", "show")...)
	steps = append(steps, discoverySteps("edit", "propose", "apply", "show", "history")...)
	steps = append(steps,
		scriptedStep{[]string{
			"narrative", "show", "--project-id", projectID,
			"--document-id", documentID, "--parent-id", rootID,
		}, result("succeeded", narrativeData(documentID, rootID, "2", nil), nil)},
		scriptedStep{append([]string{"edit", "propose", "--input", "-"}, acceptanceEditContext(projectID, sequenceID, runID, turnID)...),
			result("succeeded", proposalWithAllocations(excerptProposalID, excerptDigest, "open", "", []any{
				allocation(acceptanceSourceExcerptLocal, "narrative-node", excerptID),
			}), nil)},
		scriptedStep{append([]string{
			"edit", "apply", "--proposal-id", excerptProposalID,
			"--request-id", "installed-acceptance.source-excerpt-apply.v1", "--proposal-digest", excerptDigest,
		}, acceptanceEditContext(projectID, sequenceID, runID, turnID)...), result("succeeded", map[string]any{
			"proposal": map[string]any{
				"id": excerptProposalID, "status": "applied", "appliedTransactionId": excerptTransactionID,
			},
			"transaction": map[string]any{"id": excerptTransactionID, "committedProjectRevision": "3"},
		}, nil)},
		scriptedStep{[]string{
			"entity", "show", "--project-id", projectID, "--kind", "narrative-node", "--id", excerptID,
		}, result("succeeded", map[string]any{
			"sourceExcerpt": excerpt, "sourceExcerptEvidenceStatus": "exact",
		}, nil)},
		scriptedStep{[]string{
			"narrative", "show", "--project-id", projectID,
			"--document-id", documentID, "--parent-id", rootID,
		}, result("succeeded", narrativeData(documentID, rootID, "3", []any{
			map[string]any{"kind": "source-excerpt", "sourceExcerpt": excerpt, "evidenceStatus": "exact"},
		}), nil)},
		scriptedStep{[]string{"edit", "show", "--project-id", projectID, "--proposal-id", excerptProposalID},
			result("succeeded", proposalWithAllocations(excerptProposalID, excerptDigest, "applied", excerptTransactionID, nil), nil)},
		scriptedStep{[]string{"edit", "history", "--project-id", projectID, "--after", "2", "--limit", "10"},
			result("succeeded", transactionPage(excerptTransactionID, "3"), nil)},
	)
	steps = append(steps, discoverySteps("edit", "derive-rough-cut", "propose", "apply", "show", "history")...)
	steps = append(steps, discoverySteps("entity", "show")...)
	steps = append(steps, discoverySteps("sequence", "show")...)
	steps = append(steps,
		scriptedStep{[]string{
			"edit", "derive-rough-cut", "--input", "-", "--project-id", projectID, "--sequence-id", sequenceID,
		}, result("succeeded", map[string]any{
			"baseProjectRevision": "3", "preconditions": preconditions,
			"operation": roughOperation, "outputDigest": outputDigest,
		}, nil)},
		scriptedStep{append([]string{"edit", "propose", "--input", "-"}, acceptanceEditContext(projectID, sequenceID, runID, turnID)...),
			result("succeeded", proposalWithAllocations(roughProposalID, roughDigest, "open", "", []any{
				allocation("installed_rough_video_001", "clip", videoClipID),
				allocation("installed_rough_audio_001", "clip", audioClipID),
				allocation("installed_rough_group_001", "link-group", groupID),
				allocation("installed_rough_alignment_001", "alignment", alignmentID),
			}), nil)},
		scriptedStep{append([]string{
			"edit", "apply", "--proposal-id", roughProposalID,
			"--request-id", "installed-acceptance.rough-cut-apply.v1", "--proposal-digest", roughDigest,
		}, acceptanceEditContext(projectID, sequenceID, runID, turnID)...), result("succeeded", map[string]any{
			"proposal": map[string]any{
				"id": roughProposalID, "status": "applied", "appliedTransactionId": roughTransactionID,
			},
			"transaction": map[string]any{"id": roughTransactionID, "committedProjectRevision": "4"},
		}, nil)},
		scriptedStep{[]string{"entity", "show", "--project-id", projectID, "--kind", "clip", "--id", videoClipID},
			result("succeeded", map[string]any{"clip": clipEntity(videoClipID, sequenceID, videoTrackID, assetID, videoStreamID, groupID, sourceRange, timelineRange)}, nil)},
		scriptedStep{[]string{"entity", "show", "--project-id", projectID, "--kind", "clip", "--id", audioClipID},
			result("succeeded", map[string]any{"clip": clipEntity(audioClipID, sequenceID, audioTrackID, assetID, audioStreamID, groupID, sourceRange, timelineRange)}, nil)},
		scriptedStep{[]string{"entity", "show", "--project-id", projectID, "--kind", "link-group", "--id", groupID},
			result("succeeded", map[string]any{"linkGroup": map[string]any{
				"id": groupID, "revision": "1", "sequenceId": sequenceID, "tombstoned": false,
			}}, nil)},
		scriptedStep{[]string{"entity", "show", "--project-id", projectID, "--kind", "alignment", "--id", alignmentID},
			result("succeeded", map[string]any{"alignment": map[string]any{
				"id": alignmentID, "revision": "1", "narrativeNodeId": excerptID,
				"narrativeNodeRevision": "1", "sequenceId": sequenceID, "status": "exact",
				"targets": []any{
					clipTarget(videoClipID, localRange), clipTarget(audioClipID, localRange),
				},
			}}, nil)},
		scriptedStep{[]string{
			"sequence", "show", "--project-id", projectID, "--sequence-id", sequenceID,
			"--start", "0/1", "--duration", "29/10", "--limit", "20",
		}, result("succeeded", map[string]any{
			"sequenceId": sequenceID, "sequenceRevision": "2",
			"clips":      []any{map[string]any{"id": videoClipID}, map[string]any{"id": audioClipID}},
			"linkGroups": []any{map[string]any{"id": groupID}},
			"alignments": []any{map[string]any{"id": alignmentID}},
		}, nil)},
		scriptedStep{[]string{"edit", "show", "--project-id", projectID, "--proposal-id", roughProposalID},
			result("succeeded", proposalWithAllocations(roughProposalID, roughDigest, "applied", roughTransactionID, nil), nil)},
		scriptedStep{[]string{"edit", "history", "--project-id", projectID, "--after", "3", "--limit", "10"},
			result("succeeded", transactionPage(roughTransactionID, "4"), nil)},
	)
	cli := &scriptedCLI{steps: steps}
	expectedRoughBase := base
	expectedRoughBase.ProjectRevision = "3"
	expectedRoughBase.SourceExcerptID = excerptID
	expectedRoughBase.SourceExcerptRevision = "1"
	cli.inputValidators = []func([]byte) error{
		validateExcerptProposal(base, "2"),
		validateRoughCutQuery(expectedRoughBase),
		validateRoughCutProposal(roughOperation, preconditions),
	}
	actor := Actor{CLI: cli}
	withExcerpt, err := actor.ProposeAndApplyTranscriptSourceExcerpt(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if withExcerpt.ProjectRevision != "3" || withExcerpt.SourceExcerptID != excerptID ||
		withExcerpt.SourceExcerptEvidence != "exact" || withExcerpt.SourceExcerptProposalID != excerptProposalID ||
		withExcerpt.SourceExcerptTransactionID != excerptTransactionID {
		t.Fatalf("excerpt observation=%+v", withExcerpt)
	}
	cut, err := actor.DeriveAndApplyRoughCut(context.Background(), withExcerpt)
	if err != nil {
		t.Fatal(err)
	}
	if cut.ProjectRevision != "4" || cut.SequenceRevision != "2" || cut.RoughCutProposalID != roughProposalID ||
		cut.RoughCutTransactionID != roughTransactionID || cut.RoughCutVideoClipID != videoClipID ||
		cut.RoughCutClipID != audioClipID || cut.RoughCutLinkGroupID != groupID ||
		cut.RoughCutAlignmentID != alignmentID || cut.RoughCutStatus != "applied" {
		t.Fatalf("rough-cut observation=%+v", cut)
	}
	if cli.index != len(cli.steps) || len(cli.inputValidators) != 0 {
		t.Fatalf("executed %d of %d CLI steps with %d stdin checks left", cli.index, len(cli.steps), len(cli.inputValidators))
	}
}

func discoverySteps(group string, leaves ...string) []scriptedStep {
	children := make([]any, 0, len(leaves))
	for _, leaf := range leaves {
		children = append(children, child(leaf, true))
	}
	steps := []scriptedStep{{[]string{group, "--help"}, help(children, nil)}}
	for _, leaf := range leaves {
		steps = append(steps, scriptedStep{[]string{group, leaf, "--help"}, executableHelp("sha256:" + group + "-" + leaf)})
	}
	return steps
}

func allocation(local, kind, id string) map[string]any {
	return map[string]any{"local": local, "kind": kind, "id": id}
}

func proposalWithAllocations(id, digest, status, transactionID string, allocations []any) map[string]any {
	if allocations == nil {
		allocations = []any{}
	}
	proposal := map[string]any{"id": id, "digest": digest, "status": status, "allocation": allocations}
	if transactionID != "" {
		proposal["appliedTransactionId"] = transactionID
	}
	return map[string]any{"proposal": proposal}
}

func transactionPage(id, revision string) map[string]any {
	return map[string]any{"transactions": []any{map[string]any{
		"id": id, "committedProjectRevision": revision,
	}}}
}

func clipEntity(
	id, sequenceID, trackID, assetID, streamID, groupID string,
	sourceRange, timelineRange ExactRangeEvidence,
) map[string]any {
	return map[string]any{
		"id": id, "revision": "1", "sequenceId": sequenceID, "trackId": trackID,
		"assetId": assetID, "sourceStreamId": streamID, "linkGroupId": groupID,
		"sourceRange": sourceRange, "timelineRange": timelineRange,
		"enabled": true, "tombstoned": false,
	}
}

func clipTarget(id string, localRange ExactRangeEvidence) map[string]any {
	return map[string]any{"type": "clip", "clip": map[string]any{
		"clipId": id, "clipRevision": "1", "localRange": localRange,
	}}
}

func validateExcerptProposal(base Observation, parentRevision string) func([]byte) error {
	return func(input []byte) error {
		value, err := oneJSONObject(input)
		if err != nil {
			return err
		}
		operations, operationsOK := array(value["operations"])
		preconditions, preconditionsOK := array(value["preconditions"])
		if value["requestId"] != "installed-acceptance.source-excerpt-propose.v1" ||
			value["baseProjectRevision"] != base.ProjectRevision || !operationsOK || len(operations) != 1 ||
			!preconditionsOK || len(preconditions) != 1 {
			return fmt.Errorf("source-excerpt proposal envelope is incomplete")
		}
		precondition := record(preconditions[0])
		operation := record(operations[0])
		corrections, correctionsOK := array(operation["correctionRevisions"])
		if precondition["kind"] != "narrative-node" || precondition["id"] != base.NarrativeRoot ||
			precondition["revision"] != parentRevision || operation["type"] != "insert-source-excerpt" ||
			operation["createAs"] != acceptanceSourceExcerptLocal || operation["parentId"] != base.NarrativeRoot ||
			operation["assetId"] != base.AssetID || operation["acceptedFingerprint"] != base.AssetFingerprint ||
			operation["transcriptArtifactId"] != base.TranscriptArtifact ||
			!sameStringList(operation["transcriptSegmentIds"], base.TranscriptSegmentIDs) ||
			!exactRangeEqual(operation["sourceRange"], *base.TranscriptSourceRange) ||
			operation["language"] != base.TranscriptLanguage || !correctionsOK || len(corrections) != 0 {
			return fmt.Errorf("source-excerpt proposal changed transcript evidence")
		}
		return nil
	}
}

func validateRoughCutQuery(base Observation) func([]byte) error {
	return func(input []byte) error {
		value, err := oneJSONObject(input)
		if err != nil {
			return err
		}
		items, itemsOK := array(value["items"])
		timelineStart, timelineStartOK := parseExactTime(value["timelineStart"])
		if !itemsOK || len(items) != 1 || value["localPrefix"] != acceptanceRoughCutPrefix ||
			!timelineStartOK || timelineStart != (ExactTimeEvidence{Value: "0", Scale: 1}) {
			return fmt.Errorf("rough-cut query envelope is incomplete")
		}
		item := record(items[0])
		video := record(item["video"])
		audio := record(item["audio"])
		if item["sourceExcerptId"] != base.SourceExcerptID || item["sourceExcerptRevision"] != base.SourceExcerptRevision ||
			video["trackId"] != base.VideoTrackID || video["trackRevision"] != base.VideoTrackRevision ||
			video["sourceStreamId"] != base.VideoStreamID || audio["trackId"] != base.AudioTrackID ||
			audio["trackRevision"] != base.AudioTrackRevision || audio["sourceStreamId"] != base.MediaStreamID {
			return fmt.Errorf("rough-cut query changed exact lane bindings")
		}
		return nil
	}
}

func validateRoughCutProposal(operation map[string]any, preconditions []any) func([]byte) error {
	return func(input []byte) error {
		value, err := oneJSONObject(input)
		if err != nil {
			return err
		}
		operations, operationsOK := array(value["operations"])
		actualPreconditions, preconditionsOK := array(value["preconditions"])
		if value["requestId"] != "installed-acceptance.rough-cut-propose.v1" ||
			value["baseProjectRevision"] != "3" || !operationsOK || len(operations) != 1 ||
			!preconditionsOK || !reflect.DeepEqual(actualPreconditions, normalizedTestValue(preconditions)) ||
			!reflect.DeepEqual(record(operations[0]), normalizedTestRecord(operation)) {
			return fmt.Errorf("rough-cut proposal did not carry the exact preview bytes")
		}
		return nil
	}
}

func normalizedTestRecord(value any) map[string]any {
	return record(normalizedTestValue(value))
}

func normalizedTestValue(value any) any {
	encoded, _ := jsonMarshal(value)
	decoded, _ := oneJSONObject(encoded)
	if object, ok := value.(map[string]any); ok && object != nil {
		return decoded
	}
	return decoded["value"]
}

func jsonMarshal(value any) ([]byte, error) {
	if _, ok := value.(map[string]any); ok {
		return json.Marshal(value)
	}
	return json.Marshal(map[string]any{"value": value})
}
