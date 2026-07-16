package service

import (
	"strings"

	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func validCLIHTTPBinding(path []string, requestPath string) bool {
	name := strings.Join(path, " ")
	switch name {
	case "product status":
		return requestPath == "/v1/product/status"
	case "project list":
		return requestPath == "/v1/projects"
	case "activity list":
		return requestPath == "/v1/activity"
	default:
		_, ok := commandHTTPContext(name, requestPath)
		return ok
	}
}

func validCommandContext(descriptor command.Descriptor, value command.Context, requestPath string) bool {
	if descriptor.AppState.Project && (value.ProjectID == nil || value.ProjectID.IsZero()) {
		return false
	}
	if descriptor.AppState.Sequence && (value.SequenceID == nil || value.SequenceID.IsZero()) {
		return false
	}
	if (descriptor.AppState.Run && (value.RunID == nil || value.RunID.IsZero())) ||
		(descriptor.AppState.Turn && (value.TurnID == nil || value.TurnID.IsZero())) {
		return false
	}
	bound, ok := commandHTTPContext(strings.Join(descriptor.Path, " "), requestPath)
	if descriptor.AppState.Project || descriptor.AppState.Sequence || descriptor.AppState.Run || descriptor.AppState.Turn {
		if !ok {
			return false
		}
		if descriptor.AppState.Project && (bound.ProjectID == nil || *bound.ProjectID != *value.ProjectID) {
			return false
		}
		if descriptor.AppState.Sequence && (bound.SequenceID == nil || *bound.SequenceID != *value.SequenceID) {
			return false
		}
		if descriptor.AppState.Run && (bound.RunID == nil || *bound.RunID != *value.RunID) {
			return false
		}
		if descriptor.AppState.Turn && (bound.TurnID == nil || *bound.TurnID != *value.TurnID) {
			return false
		}
	}
	return true
}

func commandHTTPContext(name, value string) (command.Context, bool) {
	segments := strings.Split(strings.TrimPrefix(value, "/"), "/")
	if len(segments) < 3 || segments[0] != "v1" || segments[1] != "projects" {
		return command.Context{}, false
	}
	projectID, err := domain.ParseProjectID(segments[2])
	if err != nil {
		return command.Context{}, false
	}
	result := command.Context{ProjectID: &projectID}
	switch name {
	case "project show":
		return result, len(segments) == 3
	case "asset list":
		return result, len(segments) == 4 && segments[3] == "assets"
	case "asset inspect":
		if len(segments) != 5 || segments[3] != "assets" {
			return command.Context{}, false
		}
		_, err := domain.ParseAssetID(segments[4])
		return result, err == nil
	case "transcript read":
		if len(segments) != 6 || segments[3] != "assets" || segments[5] != "transcript" {
			return command.Context{}, false
		}
		_, err := domain.ParseAssetID(segments[4])
		return result, err == nil
	case "asset frames":
		if len(segments) != 10 || segments[3] != "runs" || segments[5] != "turns" ||
			segments[7] != "assets" || segments[9] != "frames" {
			return command.Context{}, false
		}
		runID, runErr := domain.ParseRunID(segments[4])
		turnID, turnErr := domain.ParseTurnID(segments[6])
		_, assetErr := domain.ParseAssetID(segments[8])
		if runErr != nil || turnErr != nil || assetErr != nil {
			return command.Context{}, false
		}
		result.RunID, result.TurnID = &runID, &turnID
		return result, true
	case "run begin", "run show", "run wait", "run resume", "run complete", "run cancel":
		project, run, turn, action, ok := parseRunHTTPBinding(value)
		if !ok || project != projectID || action != strings.TrimPrefix(name, "run ") {
			return command.Context{}, false
		}
		result.RunID = optionalRunID(run)
		result.TurnID = optionalTurnID(turn)
		return result, true
	case "narrative show":
		if len(segments) != 6 || segments[3] != "narratives" || segments[5] != "subtree" {
			return command.Context{}, false
		}
		_, err := domain.ParseNarrativeDocumentID(segments[4])
		return result, err == nil
	case "sequence show":
		if len(segments) != 6 || segments[3] != "sequences" || segments[5] != "window" {
			return command.Context{}, false
		}
		sequenceID, err := domain.ParseSequenceID(segments[4])
		if err != nil {
			return command.Context{}, false
		}
		result.SequenceID = &sequenceID
		return result, true
	case "entity show":
		if len(segments) != 6 || segments[3] != "entities" {
			return command.Context{}, false
		}
		return result, validEditEntityID(segments[4], segments[5])
	case "edit show":
		if len(segments) != 6 || segments[3] != "edit" || segments[4] != "proposals" {
			return command.Context{}, false
		}
		_, err := domain.ParseProposalID(segments[5])
		return result, err == nil
	case "edit history":
		return result, len(segments) == 5 && segments[3] == "edit" && segments[4] == "transactions"
	case "edit derive-captions":
		return parseEditDerivationHTTPContext(segments, "caption-derivation", result)
	case "edit derive-rough-cut":
		return parseEditDerivationHTTPContext(segments, "rough-cut-derivation", result)
	case "edit propose", "edit apply", "edit undo":
		return parseEditCommandHTTPContext(name, segments, result)
	default:
		return command.Context{}, false
	}
}

func validEditEntityID(kind, value string) bool {
	switch kind {
	case "narrative-node":
		_, err := domain.ParseNarrativeNodeID(value)
		return err == nil
	case "transcript-correction":
		_, err := domain.ParseTranscriptCorrectionID(value)
		return err == nil
	case "caption":
		_, err := domain.ParseCaptionID(value)
		return err == nil
	case "alignment":
		_, err := domain.ParseAlignmentID(value)
		return err == nil
	case "clip":
		_, err := domain.ParseClipID(value)
		return err == nil
	case "link-group":
		_, err := domain.ParseLinkGroupID(value)
		return err == nil
	default:
		return false
	}
}

func parseEditDerivationHTTPContext(
	segments []string,
	derivation string,
	result command.Context,
) (command.Context, bool) {
	if len(segments) != 7 || segments[3] != "sequences" || segments[5] != "edit" || segments[6] != derivation {
		return command.Context{}, false
	}
	sequenceID, err := domain.ParseSequenceID(segments[4])
	if err != nil {
		return command.Context{}, false
	}
	result.SequenceID = &sequenceID
	return result, true
}

func parseEditCommandHTTPContext(name string, segments []string, result command.Context) (command.Context, bool) {
	if len(segments) < 11 || segments[3] != "sequences" || segments[5] != "runs" ||
		segments[7] != "turns" || segments[9] != "edit" {
		return command.Context{}, false
	}
	sequenceID, sequenceErr := domain.ParseSequenceID(segments[4])
	runID, runErr := domain.ParseRunID(segments[6])
	turnID, turnErr := domain.ParseTurnID(segments[8])
	if sequenceErr != nil || runErr != nil || turnErr != nil {
		return command.Context{}, false
	}
	result.SequenceID, result.RunID, result.TurnID = &sequenceID, &runID, &turnID
	switch name {
	case "edit propose":
		return result, len(segments) == 11 && segments[10] == "proposals"
	case "edit apply":
		if len(segments) != 13 || segments[10] != "proposals" || segments[12] != "apply" {
			return command.Context{}, false
		}
		_, err := domain.ParseProposalID(segments[11])
		return result, err == nil
	case "edit undo":
		if len(segments) != 13 || segments[10] != "transactions" || segments[12] != "undo" {
			return command.Context{}, false
		}
		_, err := domain.ParseTransactionID(segments[11])
		return result, err == nil
	default:
		return command.Context{}, false
	}
}

func optionalRunID(value domain.RunID) *domain.RunID {
	if value.IsZero() {
		return nil
	}
	return &value
}

func optionalTurnID(value domain.TurnID) *domain.TurnID {
	if value.IsZero() {
		return nil
	}
	return &value
}

func parseRunHTTPBinding(value string) (domain.ProjectID, domain.RunID, domain.TurnID, string, bool) {
	segments := strings.Split(strings.TrimPrefix(value, "/"), "/")
	if len(segments) < 4 || segments[0] != "v1" || segments[1] != "projects" || segments[3] != "runs" {
		return domain.ProjectID{}, domain.RunID{}, domain.TurnID{}, "", false
	}
	projectID, err := domain.ParseProjectID(segments[2])
	if err != nil {
		return domain.ProjectID{}, domain.RunID{}, domain.TurnID{}, "", false
	}
	if len(segments) == 4 {
		return projectID, domain.RunID{}, domain.TurnID{}, "begin", true
	}
	runID, err := domain.ParseRunID(segments[4])
	if err != nil {
		return domain.ProjectID{}, domain.RunID{}, domain.TurnID{}, "", false
	}
	if len(segments) == 5 {
		return projectID, runID, domain.TurnID{}, "show", true
	}
	if len(segments) == 6 && segments[5] == "wait" {
		return projectID, runID, domain.TurnID{}, "wait", true
	}
	if len(segments) != 8 || segments[5] != "turns" {
		return domain.ProjectID{}, domain.RunID{}, domain.TurnID{}, "", false
	}
	turnID, err := domain.ParseTurnID(segments[6])
	if err != nil || (segments[7] != "resume" && segments[7] != "complete" && segments[7] != "cancel") {
		return domain.ProjectID{}, domain.RunID{}, domain.TurnID{}, "", false
	}
	return projectID, runID, turnID, segments[7], true
}
