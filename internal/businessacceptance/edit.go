package businessacceptance

import (
	"context"
	"encoding/json"
	"fmt"
)

const acceptanceAuthoredTextLocal = "installed_acceptance_line"

func (actor Actor) ProposeAndApplyAuthoredText(
	ctx context.Context,
	base Observation,
	text string,
) (Observation, error) {
	if base.ProjectID == "" || base.ProjectRevision == "" || base.SequenceID == "" ||
		base.NarrativeDocument == "" || base.NarrativeRoot == "" || base.RunID == "" ||
		base.TurnID == "" || text == "" {
		return Observation{}, fmt.Errorf("authored-text acceptance context is incomplete")
	}
	for _, discovery := range []struct {
		group  string
		leaves []string
	}{
		{group: "narrative", leaves: []string{"show"}},
		{group: "entity", leaves: []string{"show"}},
		{group: "edit", leaves: []string{"propose", "apply", "show", "history"}},
	} {
		if err := actor.discoverLeaves(ctx, discovery.group, discovery.leaves...); err != nil {
			return Observation{}, err
		}
	}
	before, err := actor.narrativeRoot(ctx, base)
	if err != nil {
		return Observation{}, err
	}
	parent := record(before.data)["parent"]
	parentRevision, _ := record(parent)["revision"].(string)
	if before.status != "succeeded" || parentRevision == "" {
		return Observation{}, fmt.Errorf("narrative root read omitted its exact revision")
	}
	proposalInput := map[string]any{
		"requestId":           "installed-acceptance.edit-propose.v1",
		"intent":              "Write one installed acceptance line",
		"baseProjectRevision": base.ProjectRevision,
		"preconditions": []any{map[string]any{
			"kind": "narrative-node", "id": base.NarrativeRoot, "revision": parentRevision,
		}},
		"operations": []any{map[string]any{
			"type": "insert-authored-text", "createAs": acceptanceAuthoredTextLocal,
			"parentId": base.NarrativeRoot, "authoredTextPurpose": "spoken",
			"language": "en", "text": text,
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
	createdID := allocationID(proposal["allocation"], acceptanceAuthoredTextLocal, "narrative-node")
	if proposed.status != "succeeded" || proposal["status"] != "open" ||
		proposalID == "" || digest == "" || createdID == "" {
		return Observation{}, fmt.Errorf("edit propose did not return one open authored-text allocation")
	}
	applyArguments := append([]string{
		"edit", "apply", "--proposal-id", proposalID,
		"--request-id", "installed-acceptance.edit-apply.v1", "--proposal-digest", digest,
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
		return Observation{}, fmt.Errorf("edit apply did not commit the exact proposal")
	}
	after, err := actor.narrativeRoot(ctx, base)
	if err != nil {
		return Observation{}, err
	}
	if after.status != "succeeded" || !containsAuthoredText(after.data, createdID, text) {
		return Observation{}, fmt.Errorf("narrative readback omitted the committed authored text")
	}
	entity, err := actor.command(
		ctx, "entity", "show", "--project-id", base.ProjectID,
		"--kind", "narrative-node", "--id", createdID,
	)
	if err != nil {
		return Observation{}, err
	}
	entityText := record(record(entity.data)["authoredText"])
	if entity.status != "succeeded" || entityText["id"] != createdID || entityText["text"] != text ||
		entityText["purpose"] != "spoken" || entityText["language"] != "en" ||
		entityText["revision"] != "1" || entityText["tombstoned"] != false {
		return Observation{}, fmt.Errorf("entity readback did not confirm the committed authored text")
	}
	shown, err := actor.command(
		ctx, "edit", "show", "--project-id", base.ProjectID, "--proposal-id", proposalID,
	)
	if err != nil {
		return Observation{}, err
	}
	shownProposal := record(record(shown.data)["proposal"])
	if shown.status != "succeeded" || shownProposal["status"] != "applied" ||
		shownProposal["appliedTransactionId"] != transactionID {
		return Observation{}, fmt.Errorf("proposal readback did not confirm application")
	}
	history, err := actor.command(
		ctx, "edit", "history", "--project-id", base.ProjectID,
		"--after", base.ProjectRevision, "--limit", "10",
	)
	if err != nil {
		return Observation{}, err
	}
	if history.status != "succeeded" || !containsTransaction(history.data, transactionID, committedRevision) {
		return Observation{}, fmt.Errorf("transaction history omitted the committed edit")
	}
	result := base
	result.ProjectRevision = committedRevision
	result.ProposalID = proposalID
	result.TransactionID = transactionID
	result.AuthoredTextID = createdID
	result.EditStatus = "applied"
	return result, nil
}

func (actor Actor) discoverLeaves(ctx context.Context, group string, leaves ...string) error {
	discovery, err := actor.help(ctx, group, "--help")
	if err != nil {
		return err
	}
	for _, leaf := range leaves {
		if !hasChild(discovery, leaf, true) {
			return fmt.Errorf("%s discovery omits %s", group, leaf)
		}
		leafHelp, leafErr := actor.help(ctx, group, leaf, "--help")
		if leafErr != nil {
			return leafErr
		}
		if leafHelp["fingerprint"] == "" || leafHelp["input"] == nil || leafHelp["result"] == nil {
			return fmt.Errorf("%s %s discovery omits its executable schema", group, leaf)
		}
	}
	return nil
}

func (actor Actor) narrativeRoot(ctx context.Context, base Observation) (commandResult, error) {
	return actor.command(
		ctx, "narrative", "show", "--project-id", base.ProjectID,
		"--document-id", base.NarrativeDocument, "--parent-id", base.NarrativeRoot,
	)
}

func editContext(base Observation) []string {
	return []string{
		"--project-id", base.ProjectID, "--sequence-id", base.SequenceID,
		"--run-id", base.RunID, "--turn-id", base.TurnID,
	}
}

func allocationID(value any, local, kind string) string {
	for _, item := range list(value) {
		allocation := record(item)
		if allocation["local"] == local && allocation["kind"] == kind {
			id, _ := allocation["id"].(string)
			return id
		}
	}
	return ""
}

func containsAuthoredText(data any, id, text string) bool {
	for _, item := range list(record(data)["nodes"]) {
		state := record(record(item)["authoredText"])
		if state["id"] == id && state["text"] == text && state["tombstoned"] == false {
			return true
		}
	}
	return false
}

func containsTransaction(data any, id, revision string) bool {
	for _, item := range list(record(data)["transactions"]) {
		transaction := record(item)
		if transaction["id"] == id && transaction["committedProjectRevision"] == revision {
			return true
		}
	}
	return false
}
