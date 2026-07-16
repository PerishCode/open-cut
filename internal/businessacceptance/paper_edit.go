package businessacceptance

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	acceptanceSourceExcerptLocal = "installed_transcript_excerpt"
	acceptanceRoughCutPrefix     = "installed_rough"
)

func (actor Actor) ProposeAndApplyTranscriptSourceExcerpt(
	ctx context.Context,
	base Observation,
) (Observation, error) {
	if base.ProjectID == "" || base.ProjectRevision == "" || base.NarrativeDocument == "" ||
		base.NarrativeRoot == "" || base.AssetID == "" || base.AssetFingerprint == "" ||
		base.TranscriptArtifact == "" || len(base.TranscriptSegmentIDs) != 1 ||
		base.TranscriptSourceRange == nil || base.TranscriptLanguage == "" || base.TranscriptText == "" {
		return Observation{}, fmt.Errorf("source-excerpt acceptance context is incomplete")
	}
	if err := actor.discoverLeaves(ctx, "narrative", "show"); err != nil {
		return Observation{}, err
	}
	if err := actor.discoverLeaves(ctx, "entity", "show"); err != nil {
		return Observation{}, err
	}
	if err := actor.discoverLeaves(ctx, "edit", "propose", "apply", "show", "history"); err != nil {
		return Observation{}, err
	}
	before, err := actor.narrativeRoot(ctx, base)
	if err != nil {
		return Observation{}, err
	}
	parentRevision, _ := record(record(before.data)["parent"])["revision"].(string)
	if before.status != "succeeded" || parentRevision == "" {
		return Observation{}, fmt.Errorf("source-excerpt parent read omitted its revision")
	}
	proposalInput := map[string]any{
		"requestId":           "installed-acceptance.source-excerpt-propose.v1",
		"intent":              "Cite exact installed transcript evidence in the paper edit",
		"baseProjectRevision": base.ProjectRevision,
		"preconditions": []any{map[string]any{
			"kind": "narrative-node", "id": base.NarrativeRoot, "revision": parentRevision,
		}},
		"operations": []any{map[string]any{
			"type": "insert-source-excerpt", "createAs": acceptanceSourceExcerptLocal,
			"parentId": base.NarrativeRoot, "assetId": base.AssetID,
			"acceptedFingerprint":  base.AssetFingerprint,
			"transcriptArtifactId": base.TranscriptArtifact,
			"transcriptSegmentIds": append([]string(nil), base.TranscriptSegmentIDs...),
			"sourceRange":          *base.TranscriptSourceRange, "language": base.TranscriptLanguage,
			"correctionRevisions": []any{},
		}},
	}
	input, err := json.Marshal(proposalInput)
	if err != nil {
		return Observation{}, err
	}
	proposeArguments := append([]string{"edit", "propose", "--input", "-"}, editContext(base)...)
	proposed, err := actor.commandInput(ctx, input, proposeArguments...)
	if err != nil {
		return Observation{}, err
	}
	proposal := record(record(proposed.data)["proposal"])
	proposalID, _ := proposal["id"].(string)
	digest, _ := proposal["digest"].(string)
	excerptID := allocationID(proposal["allocation"], acceptanceSourceExcerptLocal, "narrative-node")
	if proposed.status != "succeeded" || proposal["status"] != "open" ||
		proposalID == "" || digest == "" || excerptID == "" {
		return Observation{}, fmt.Errorf("source-excerpt proposal omitted its exact allocation")
	}
	applyArguments := append([]string{
		"edit", "apply", "--proposal-id", proposalID,
		"--request-id", "installed-acceptance.source-excerpt-apply.v1", "--proposal-digest", digest,
	}, editContext(base)...)
	applied, err := actor.command(ctx, applyArguments...)
	if err != nil {
		return Observation{}, err
	}
	appliedData := record(applied.data)
	appliedProposal := record(appliedData["proposal"])
	transaction := record(appliedData["transaction"])
	transactionID, _ := transaction["id"].(string)
	committedRevision, _ := transaction["committedProjectRevision"].(string)
	if applied.status != "succeeded" || appliedProposal["id"] != proposalID ||
		appliedProposal["status"] != "applied" || appliedProposal["appliedTransactionId"] != transactionID ||
		transactionID == "" || committedRevision == "" {
		return Observation{}, fmt.Errorf("source-excerpt apply did not commit the exact proposal")
	}
	entity, err := actor.command(
		ctx, "entity", "show", "--project-id", base.ProjectID,
		"--kind", "narrative-node", "--id", excerptID,
	)
	if err != nil {
		return Observation{}, err
	}
	excerpt := record(record(entity.data)["sourceExcerpt"])
	evidence := record(excerpt["evidence"])
	correctionRevisions, correctionRevisionsOK := array(evidence["correctionRevisions"])
	if entity.status != "succeeded" || record(entity.data)["sourceExcerptEvidenceStatus"] != "exact" ||
		excerpt["id"] != excerptID || excerpt["revision"] != "1" || excerpt["parentId"] != base.NarrativeRoot ||
		excerpt["assetId"] != base.AssetID || excerpt["acceptedFingerprint"] != base.AssetFingerprint ||
		excerpt["language"] != base.TranscriptLanguage || excerpt["effectiveText"] != base.TranscriptText ||
		excerpt["tombstoned"] != false || !exactRangeEqual(excerpt["sourceRange"], *base.TranscriptSourceRange) ||
		evidence["artifactId"] != base.TranscriptArtifact || evidence["sourceStreamId"] != base.MediaStreamID ||
		!sameStringList(evidence["segmentIds"], base.TranscriptSegmentIDs) ||
		!correctionRevisionsOK || len(correctionRevisions) != 0 {
		return Observation{}, fmt.Errorf("source-excerpt entity did not preserve exact transcript evidence")
	}
	narrative, err := actor.narrativeRoot(ctx, base)
	if err != nil {
		return Observation{}, err
	}
	if narrative.status != "succeeded" || !containsSourceExcerpt(narrative.data, excerptID, base.TranscriptText) {
		return Observation{}, fmt.Errorf("narrative readback omitted the committed source excerpt")
	}
	if err := actor.confirmAppliedEdit(ctx, base, proposalID, transactionID, base.ProjectRevision, committedRevision); err != nil {
		return Observation{}, err
	}
	result := base
	result.ProjectRevision = committedRevision
	result.SourceExcerptID = excerptID
	result.SourceExcerptRevision = "1"
	result.SourceExcerptEvidence = "exact"
	result.SourceExcerptProposalID = proposalID
	result.SourceExcerptTransactionID = transactionID
	return result, nil
}

func (actor Actor) DeriveAndApplyRoughCut(ctx context.Context, base Observation) (Observation, error) {
	if base.ProjectID == "" || base.ProjectRevision == "" || base.SequenceID == "" || base.SequenceRevision == "" ||
		base.SourceExcerptID == "" || base.SourceExcerptRevision == "" || base.TranscriptSourceRange == nil ||
		base.VideoTrackID == "" || base.VideoTrackRevision == "" || base.AudioTrackID == "" ||
		base.AudioTrackRevision == "" || base.VideoStreamID == "" || base.MediaStreamID == "" {
		return Observation{}, fmt.Errorf("rough-cut acceptance context is incomplete")
	}
	if err := actor.discoverLeaves(ctx, "edit", "derive-rough-cut", "propose", "apply", "show", "history"); err != nil {
		return Observation{}, err
	}
	if err := actor.discoverLeaves(ctx, "entity", "show"); err != nil {
		return Observation{}, err
	}
	if err := actor.discoverLeaves(ctx, "sequence", "show"); err != nil {
		return Observation{}, err
	}
	deriveInput := map[string]any{
		"timelineStart": ExactTimeEvidence{Value: "0", Scale: 1},
		"localPrefix":   acceptanceRoughCutPrefix,
		"items": []any{map[string]any{
			"sourceExcerptId": base.SourceExcerptID, "sourceExcerptRevision": base.SourceExcerptRevision,
			"video": map[string]any{
				"trackId": base.VideoTrackID, "trackRevision": base.VideoTrackRevision,
				"sourceStreamId": base.VideoStreamID,
			},
			"audio": map[string]any{
				"trackId": base.AudioTrackID, "trackRevision": base.AudioTrackRevision,
				"sourceStreamId": base.MediaStreamID,
			},
		}},
	}
	input, err := json.Marshal(deriveInput)
	if err != nil {
		return Observation{}, err
	}
	derived, err := actor.commandInput(
		ctx, input, "edit", "derive-rough-cut", "--input", "-",
		"--project-id", base.ProjectID, "--sequence-id", base.SequenceID,
	)
	if err != nil {
		return Observation{}, err
	}
	data := record(derived.data)
	operation := record(data["operation"])
	preconditions, preconditionsOK := array(data["preconditions"])
	outputDigest, _ := data["outputDigest"].(string)
	operationDigest, _ := operation["roughCutOutputDigest"].(string)
	outputs := list(operation["derivedRoughCut"])
	previewStart, previewStartOK := parseExactTime(operation["roughCutTimelineStart"])
	previewItems, previewItemsOK := array(operation["roughCutItems"])
	if derived.status != "succeeded" || data["baseProjectRevision"] != base.ProjectRevision ||
		!preconditionsOK || len(preconditions) == 0 || operation["type"] != "derive-rough-cut" ||
		record(operation["roughCutPolicy"])["id"] != "paper-edit-rough-cut-v1" ||
		!previewStartOK || previewStart != (ExactTimeEvidence{Value: "0", Scale: 1}) ||
		operation["roughCutLocalPrefix"] != acceptanceRoughCutPrefix || !previewItemsOK || len(previewItems) != 1 ||
		operationDigest == "" || operationDigest != outputDigest || len(outputs) != 1 {
		return Observation{}, fmt.Errorf("rough-cut preview omitted its closed deterministic operation")
	}
	previewItem := record(previewItems[0])
	previewVideo := record(previewItem["video"])
	previewAudio := record(previewItem["audio"])
	if previewItem["sourceExcerptId"] != base.SourceExcerptID ||
		previewVideo["trackId"] != base.VideoTrackID ||
		previewVideo["sourceStreamId"] != base.VideoStreamID ||
		previewAudio["trackId"] != base.AudioTrackID || previewAudio["sourceStreamId"] != base.MediaStreamID ||
		!hasEntityPrecondition(preconditions, "narrative-node", base.SourceExcerptID, base.SourceExcerptRevision) ||
		!hasEntityPrecondition(preconditions, "track", base.VideoTrackID, base.VideoTrackRevision) ||
		!hasEntityPrecondition(preconditions, "track", base.AudioTrackID, base.AudioTrackRevision) ||
		!hasEntityPrecondition(preconditions, "sequence", base.SequenceID, base.SequenceRevision) {
		return Observation{}, fmt.Errorf("rough-cut preview changed its exact planning query")
	}
	output := record(outputs[0])
	video := record(output["video"])
	audio := record(output["audio"])
	videoAs, _ := video["clipAs"].(string)
	audioAs, _ := audio["clipAs"].(string)
	groupAs, _ := output["linkGroupAs"].(string)
	alignmentAs, _ := output["alignmentAs"].(string)
	wantTimelineRange := ExactRangeEvidence{
		Start: ExactTimeEvidence{Value: "0", Scale: 1}, Duration: base.TranscriptSourceRange.Duration,
	}
	if output["sourceExcerptId"] != base.SourceExcerptID ||
		!exactRangeEqual(output["sourceRange"], *base.TranscriptSourceRange) ||
		!exactRangeEqual(output["timelineRange"], wantTimelineRange) ||
		videoAs == "" || video["trackId"] != base.VideoTrackID || video["sourceStreamId"] != base.VideoStreamID ||
		audioAs == "" || audio["trackId"] != base.AudioTrackID || audio["sourceStreamId"] != base.MediaStreamID ||
		groupAs == "" || alignmentAs == "" {
		return Observation{}, fmt.Errorf("rough-cut preview changed its exact A/V lane evidence")
	}
	proposalInput := map[string]any{
		"requestId":           "installed-acceptance.rough-cut-propose.v1",
		"intent":              "Materialize the exact transcript-backed paper edit",
		"baseProjectRevision": base.ProjectRevision,
		"preconditions":       preconditions,
		"operations":          []any{operation},
	}
	proposalBytes, err := json.Marshal(proposalInput)
	if err != nil {
		return Observation{}, err
	}
	proposeArguments := append([]string{"edit", "propose", "--input", "-"}, editContext(base)...)
	proposed, err := actor.commandInput(ctx, proposalBytes, proposeArguments...)
	if err != nil {
		return Observation{}, err
	}
	proposal := record(record(proposed.data)["proposal"])
	proposalID, _ := proposal["id"].(string)
	proposalDigest, _ := proposal["digest"].(string)
	videoClipID := allocationID(proposal["allocation"], videoAs, "clip")
	audioClipID := allocationID(proposal["allocation"], audioAs, "clip")
	linkGroupID := allocationID(proposal["allocation"], groupAs, "link-group")
	alignmentID := allocationID(proposal["allocation"], alignmentAs, "alignment")
	if proposed.status != "succeeded" || proposal["status"] != "open" || proposalID == "" ||
		proposalDigest == "" || videoClipID == "" || audioClipID == "" || linkGroupID == "" || alignmentID == "" {
		return Observation{}, fmt.Errorf("rough-cut proposal omitted an allocated output")
	}
	applyArguments := append([]string{
		"edit", "apply", "--proposal-id", proposalID,
		"--request-id", "installed-acceptance.rough-cut-apply.v1", "--proposal-digest", proposalDigest,
	}, editContext(base)...)
	applied, err := actor.command(ctx, applyArguments...)
	if err != nil {
		return Observation{}, err
	}
	appliedData := record(applied.data)
	appliedProposal := record(appliedData["proposal"])
	transaction := record(appliedData["transaction"])
	transactionID, _ := transaction["id"].(string)
	committedRevision, _ := transaction["committedProjectRevision"].(string)
	if applied.status != "succeeded" || appliedProposal["id"] != proposalID || appliedProposal["status"] != "applied" ||
		appliedProposal["appliedTransactionId"] != transactionID ||
		transactionID == "" || committedRevision == "" {
		return Observation{}, fmt.Errorf("rough-cut apply did not commit the exact proposal")
	}
	if err := actor.confirmClip(ctx, base, videoClipID, base.VideoTrackID, base.VideoStreamID, linkGroupID, *base.TranscriptSourceRange, wantTimelineRange); err != nil {
		return Observation{}, err
	}
	if err := actor.confirmClip(ctx, base, audioClipID, base.AudioTrackID, base.MediaStreamID, linkGroupID, *base.TranscriptSourceRange, wantTimelineRange); err != nil {
		return Observation{}, err
	}
	if err := actor.confirmRoughCutEntities(ctx, base, linkGroupID, alignmentID, videoClipID, audioClipID); err != nil {
		return Observation{}, err
	}
	sequenceRevision, err := actor.confirmSequenceWindow(
		ctx, base, videoClipID, audioClipID, linkGroupID, alignmentID, wantTimelineRange,
	)
	if err != nil {
		return Observation{}, err
	}
	if err := actor.confirmAppliedEdit(ctx, base, proposalID, transactionID, base.ProjectRevision, committedRevision); err != nil {
		return Observation{}, err
	}
	result := base
	result.ProjectRevision = committedRevision
	result.SequenceRevision = sequenceRevision
	result.RoughCutProposalID = proposalID
	result.RoughCutTransactionID = transactionID
	result.RoughCutClipID = audioClipID
	result.RoughCutVideoClipID = videoClipID
	result.RoughCutLinkGroupID = linkGroupID
	result.RoughCutAlignmentID = alignmentID
	result.RoughCutStatus = "applied"
	return result, nil
}

func (actor Actor) confirmAppliedEdit(
	ctx context.Context,
	base Observation,
	proposalID, transactionID, afterRevision, committedRevision string,
) error {
	shown, err := actor.command(ctx, "edit", "show", "--project-id", base.ProjectID, "--proposal-id", proposalID)
	if err != nil {
		return err
	}
	proposal := record(record(shown.data)["proposal"])
	if shown.status != "succeeded" || proposal["status"] != "applied" ||
		proposal["appliedTransactionId"] != transactionID {
		return fmt.Errorf("proposal readback did not confirm application")
	}
	history, err := actor.command(
		ctx, "edit", "history", "--project-id", base.ProjectID,
		"--after", afterRevision, "--limit", "10",
	)
	if err != nil {
		return err
	}
	if history.status != "succeeded" || !containsTransaction(history.data, transactionID, committedRevision) {
		return fmt.Errorf("transaction history omitted the committed edit")
	}
	return nil
}

func (actor Actor) confirmClip(
	ctx context.Context,
	base Observation,
	id, trackID, streamID, groupID string,
	sourceRange, timelineRange ExactRangeEvidence,
) error {
	result, err := actor.command(
		ctx, "entity", "show", "--project-id", base.ProjectID, "--kind", "clip", "--id", id,
	)
	if err != nil {
		return err
	}
	clip := record(record(result.data)["clip"])
	if result.status != "succeeded" || clip["id"] != id || clip["revision"] != "1" ||
		clip["sequenceId"] != base.SequenceID || clip["trackId"] != trackID || clip["assetId"] != base.AssetID ||
		clip["sourceStreamId"] != streamID || clip["linkGroupId"] != groupID || clip["enabled"] != true ||
		clip["tombstoned"] != false || !exactRangeEqual(clip["sourceRange"], sourceRange) ||
		!exactRangeEqual(clip["timelineRange"], timelineRange) {
		return fmt.Errorf("committed Clip %s changed its exact lane evidence", id)
	}
	return nil
}

func (actor Actor) confirmRoughCutEntities(
	ctx context.Context,
	base Observation,
	groupID, alignmentID, videoClipID, audioClipID string,
) error {
	groupResult, err := actor.command(
		ctx, "entity", "show", "--project-id", base.ProjectID, "--kind", "link-group", "--id", groupID,
	)
	if err != nil {
		return err
	}
	group := record(record(groupResult.data)["linkGroup"])
	if groupResult.status != "succeeded" || group["id"] != groupID || group["revision"] != "1" ||
		group["sequenceId"] != base.SequenceID || group["tombstoned"] != false {
		return fmt.Errorf("committed LinkGroup projection is incomplete")
	}
	alignmentResult, err := actor.command(
		ctx, "entity", "show", "--project-id", base.ProjectID,
		"--kind", "alignment", "--id", alignmentID,
	)
	if err != nil {
		return err
	}
	alignment := record(record(alignmentResult.data)["alignment"])
	targets := list(alignment["targets"])
	if alignmentResult.status != "succeeded" || alignment["id"] != alignmentID ||
		alignment["revision"] != "1" || alignment["narrativeNodeId"] != base.SourceExcerptID ||
		alignment["narrativeNodeRevision"] != base.SourceExcerptRevision ||
		alignment["sequenceId"] != base.SequenceID || alignment["status"] != "exact" || len(targets) != 2 ||
		!alignmentHasClipTargets(targets, base.TranscriptSourceRange.Duration, videoClipID, audioClipID) {
		return fmt.Errorf("committed Alignment did not bind both linked Clips")
	}
	return nil
}

func (actor Actor) confirmSequenceWindow(
	ctx context.Context,
	base Observation,
	videoClipID, audioClipID, groupID, alignmentID string,
	rangeValue ExactRangeEvidence,
) (string, error) {
	result, err := actor.command(
		ctx, "sequence", "show", "--project-id", base.ProjectID, "--sequence-id", base.SequenceID,
		"--start", exactTimeArgument(rangeValue.Start), "--duration", exactTimeArgument(rangeValue.Duration),
		"--limit", "20",
	)
	if err != nil {
		return "", err
	}
	data := record(result.data)
	clips, clipsOK := array(data["clips"])
	groups, groupsOK := array(data["linkGroups"])
	alignments, alignmentsOK := array(data["alignments"])
	sequenceRevision, _ := data["sequenceRevision"].(string)
	if result.status != "succeeded" || data["sequenceId"] != base.SequenceID || sequenceRevision == "" ||
		!clipsOK || !groupsOK || !alignmentsOK || !containsIDs(clips, videoClipID, audioClipID) ||
		!containsIDs(groups, groupID) || !containsIDs(alignments, alignmentID) {
		return "", fmt.Errorf("Sequence window omitted the committed rough cut")
	}
	return sequenceRevision, nil
}

func containsSourceExcerpt(data any, id, text string) bool {
	for _, item := range list(record(data)["nodes"]) {
		node := record(item)
		excerpt := record(node["sourceExcerpt"])
		if node["kind"] == "source-excerpt" && node["evidenceStatus"] == "exact" &&
			excerpt["id"] == id && excerpt["effectiveText"] == text && excerpt["tombstoned"] == false {
			return true
		}
	}
	return false
}

func sameStringList(value any, expected []string) bool {
	items, ok := array(value)
	if !ok || len(items) != len(expected) {
		return false
	}
	for index, item := range items {
		if item != expected[index] {
			return false
		}
	}
	return true
}

func alignmentHasClipTargets(targets []any, duration ExactTimeEvidence, expected ...string) bool {
	found := make(map[string]bool, len(expected))
	localRange := ExactRangeEvidence{Start: ExactTimeEvidence{Value: "0", Scale: 1}, Duration: duration}
	for _, value := range targets {
		target := record(value)
		clip := record(target["clip"])
		id, _ := clip["clipId"].(string)
		if target["type"] != "clip" || id == "" || clip["clipRevision"] != "1" ||
			!exactRangeEqual(clip["localRange"], localRange) {
			return false
		}
		found[id] = true
	}
	for _, id := range expected {
		if !found[id] {
			return false
		}
	}
	return len(found) == len(expected)
}

func containsIDs(values []any, expected ...string) bool {
	found := make(map[string]bool, len(values))
	for _, value := range values {
		id, _ := record(value)["id"].(string)
		found[id] = true
	}
	for _, id := range expected {
		if !found[id] {
			return false
		}
	}
	return true
}

func hasEntityPrecondition(values []any, kind, id, revision string) bool {
	for _, value := range values {
		precondition := record(value)
		if precondition["kind"] == kind && precondition["id"] == id && precondition["revision"] == revision {
			return true
		}
	}
	return false
}
