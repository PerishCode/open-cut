package productcli

import (
	"encoding/json"
	"fmt"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func validateBusinessResponse(
	name string,
	data []byte,
) (json.RawMessage, *domain.Revision, *domain.Cursor, error) {
	raw := json.RawMessage(append([]byte(nil), data...))
	switch name {
	case "product status":
		var result command.ProductStatusData
		if err := json.Unmarshal(data, &result); err != nil || result.Validate() != nil {
			return nil, nil, nil, fmt.Errorf("invalid product status response")
		}
		return raw, nil, nil, nil
	case "project list":
		var result command.ProjectListData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		cursor := result.ActivityCursor
		return raw, nil, &cursor, nil
	case "project show":
		var result command.ProjectShowData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		revision := result.Project.Revision
		cursor := result.ActivityCursor
		return raw, &revision, &cursor, nil
	case "asset list":
		var result command.AssetListData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		cursor := result.ActivityCursor
		return raw, nil, &cursor, nil
	case "asset inspect":
		var result command.AssetInspectData
		if err := json.Unmarshal(data, &result); err != nil || result.Asset.ID.IsZero() {
			return nil, nil, nil, fmt.Errorf("incomplete Asset response")
		}
		revision := result.Asset.Revision
		cursor := result.ActivityCursor
		return raw, &revision, &cursor, nil
	case "asset frames":
		var result command.AssetFramesData
		if err := json.Unmarshal(data, &result); err != nil || result.Job.ID.IsZero() ||
			(result.Status != application.MediaFrameSetAccepted && result.Status != application.MediaFrameSetReady) {
			return nil, nil, nil, fmt.Errorf("incomplete frame-set response")
		}
		cursor := result.ActivityCursor
		return raw, nil, &cursor, nil
	case "sequence frames":
		var result command.SequenceFramesData
		if err := json.Unmarshal(data, &result); err != nil || result.Validate() != nil {
			return nil, nil, nil, fmt.Errorf("incomplete Sequence frame response")
		}
		cursor := result.ActivityCursor
		return raw, nil, &cursor, nil
	case "export start", "export show", "export retry", "export cancel":
		var result command.ExportData
		if err := json.Unmarshal(data, &result); err != nil || result.Validate() != nil {
			return nil, nil, nil, fmt.Errorf("incomplete export response")
		}
		cursor := result.ActivityCursor
		return raw, nil, &cursor, nil
	case "transcript read":
		var result command.TranscriptReadData
		if err := json.Unmarshal(data, &result); err != nil || result.Schema != application.TranscriptReadSchema ||
			result.Artifact.ID.IsZero() || result.Artifact.AssetID.IsZero() {
			return nil, nil, nil, fmt.Errorf("incomplete transcript response")
		}
		cursor := result.ActivityCursor
		return raw, nil, &cursor, nil
	case "activity list":
		var result command.ActivityListData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		cursor := result.Cursor
		return raw, nil, &cursor, nil
	case "run begin", "run show", "run wait", "run resume", "run complete", "run cancel":
		var result command.RunData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		if result.Run.ID.IsZero() || result.Run.ProjectID.IsZero() || result.Run.CurrentTurn.ID.IsZero() {
			return nil, nil, nil, fmt.Errorf("incomplete AgentRun response")
		}
		revision := result.Run.LatestObservedProjectRevision
		cursor := result.Run.ActivityCursor
		return raw, &revision, &cursor, nil
	case "narrative show", "sequence show", "entity show", "edit show", "edit history", "edit derive-captions",
		"edit derive-rough-cut",
		"edit propose", "edit apply", "edit undo":
		return editResultMetadata(name, data)
	default:
		return nil, nil, nil, command.ErrUnknownCommand
	}
}
