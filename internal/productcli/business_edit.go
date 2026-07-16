package productcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func readEditProposalInput(reader io.Reader) (command.EditProposeInput, error) {
	if reader == nil {
		return command.EditProposeInput{}, fmt.Errorf("edit propose stdin is unavailable")
	}
	limit := int64(command.InitialMutationLimits.CanonicalBytes)
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return command.EditProposeInput{}, fmt.Errorf("read edit proposal: %w", err)
	}
	if len(raw) == 0 || int64(len(raw)) > limit {
		return command.EditProposeInput{}, fmt.Errorf("edit proposal exceeds its input limit")
	}
	var input command.EditProposeInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return command.EditProposeInput{}, fmt.Errorf("decode edit proposal: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return command.EditProposeInput{}, fmt.Errorf("edit proposal must contain exactly one JSON value")
	}
	return input, nil
}

func readRoughCutDerivationInput(reader io.Reader) (command.RoughCutDeriveInput, error) {
	if reader == nil {
		return command.RoughCutDeriveInput{}, fmt.Errorf("rough-cut derivation stdin is unavailable")
	}
	limit := int64(command.InitialMutationLimits.CanonicalBytes)
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return command.RoughCutDeriveInput{}, fmt.Errorf("read rough-cut derivation: %w", err)
	}
	if len(raw) == 0 || int64(len(raw)) > limit {
		return command.RoughCutDeriveInput{}, fmt.Errorf("rough-cut derivation exceeds its input limit")
	}
	var input command.RoughCutDeriveInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return command.RoughCutDeriveInput{}, fmt.Errorf("decode rough-cut derivation: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return command.RoughCutDeriveInput{}, fmt.Errorf("rough-cut derivation must contain exactly one JSON value")
	}
	return input, nil
}

func parseRationalArgument(value string, positive bool) (domain.RationalTime, error) {
	numeratorValue, scaleValue, ok := strings.Cut(value, "/")
	if !ok || numeratorValue == "" || scaleValue == "" || strings.Contains(scaleValue, "/") {
		return domain.RationalTime{}, domain.ErrInvalidRationalTime
	}
	numerator, err := strconv.ParseInt(numeratorValue, 10, 64)
	if err != nil || strconv.FormatInt(numerator, 10) != numeratorValue {
		return domain.RationalTime{}, domain.ErrInvalidRationalTime
	}
	scale64, err := strconv.ParseInt(scaleValue, 10, 32)
	if err != nil || strconv.FormatInt(scale64, 10) != scaleValue {
		return domain.RationalTime{}, domain.ErrInvalidRationalTime
	}
	result, err := domain.NewRationalTime(numerator, int32(scale64))
	if err != nil || result.IsNegative() || (positive && !result.IsPositive()) {
		return domain.RationalTime{}, domain.ErrInvalidRationalTime
	}
	return result, nil
}

func setBoundedQuery(values url.Values, afterName, after string, limitName string, limit uint) {
	if after != "" {
		values.Set(afterName, after)
	}
	if limit != 0 {
		values.Set(limitName, strconv.FormatUint(uint64(limit), 10))
	}
}

func validCLIEditEntityID(kind domain.EditEntityKind, value string) bool {
	switch kind {
	case domain.EntityNarrativeNode:
		_, err := domain.ParseNarrativeNodeID(value)
		return err == nil
	case domain.EntityTranscriptCorrection:
		_, err := domain.ParseTranscriptCorrectionID(value)
		return err == nil
	case domain.EntityCaption:
		_, err := domain.ParseCaptionID(value)
		return err == nil
	case domain.EntityAlignment:
		_, err := domain.ParseAlignmentID(value)
		return err == nil
	case domain.EntityClip:
		_, err := domain.ParseClipID(value)
		return err == nil
	case domain.EntityLinkGroup:
		_, err := domain.ParseLinkGroupID(value)
		return err == nil
	default:
		return false
	}
}

func editCommandPath(context command.Context, suffix string) (string, error) {
	if context.ProjectID == nil || context.SequenceID == nil || context.RunID == nil || context.TurnID == nil {
		return "", fmt.Errorf("edit write requires project, sequence, run, and turn context")
	}
	return "/v1/projects/" + context.ProjectID.String() +
		"/sequences/" + context.SequenceID.String() +
		"/runs/" + context.RunID.String() +
		"/turns/" + context.TurnID.String() +
		"/edit/" + suffix, nil
}

func editResultMetadata(
	name string,
	data []byte,
) (json.RawMessage, *domain.Revision, *domain.Cursor, error) {
	raw := json.RawMessage(append([]byte(nil), data...))
	switch name {
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
			return nil, nil, nil, fmt.Errorf("invalid Asset response")
		}
		cursor := result.ActivityCursor
		return raw, nil, &cursor, nil
	case "narrative show":
		var result command.NarrativeShowData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		revision, cursor := result.DocumentRevision, result.ActivityCursor
		return raw, &revision, &cursor, nil
	case "sequence show":
		var result command.SequenceShowData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		revision, cursor := result.SequenceRevision, result.ActivityCursor
		return raw, &revision, &cursor, nil
	case "entity show":
		var result command.EntityShowData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		cursor := result.ActivityCursor
		return raw, nil, &cursor, nil
	case "edit show":
		var result command.EditShowData
		if err := json.Unmarshal(data, &result); err != nil || result.Proposal.ID.IsZero() {
			return nil, nil, nil, fmt.Errorf("invalid Edit Proposal response")
		}
		revision, cursor := result.Proposal.BaseProjectRevision, result.ActivityCursor
		return raw, &revision, &cursor, nil
	case "edit history":
		var result command.EditHistoryData
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, nil, nil, err
		}
		cursor := result.ActivityCursor
		var revision *domain.Revision
		if len(result.Transactions) > 0 {
			value := result.Transactions[len(result.Transactions)-1].CommittedProjectRevision
			revision = &value
		}
		return raw, revision, &cursor, nil
	case "edit derive-captions":
		var result command.CaptionDeriveData
		if err := json.Unmarshal(data, &result); err != nil || result.Operation.Type != domain.EditDeriveCaptions ||
			len(result.Operation.DerivedCaptions) == 0 {
			return nil, nil, nil, fmt.Errorf("invalid caption derivation response")
		}
		revision, cursor := result.BaseProjectRevision, result.ActivityCursor
		return raw, &revision, &cursor, nil
	case "edit derive-rough-cut":
		var result command.RoughCutDeriveData
		if err := json.Unmarshal(data, &result); err != nil || result.Operation.Type != domain.EditDeriveRoughCut ||
			len(result.Operation.DerivedRoughCut) == 0 || result.Operation.RoughCutOutputDigest == nil ||
			result.OutputDigest != *result.Operation.RoughCutOutputDigest {
			return nil, nil, nil, fmt.Errorf("invalid rough-cut derivation response")
		}
		revision, cursor := result.BaseProjectRevision, result.ActivityCursor
		return raw, &revision, &cursor, nil
	case "edit propose":
		var result command.EditProposalData
		if err := json.Unmarshal(data, &result); err != nil || result.Proposal.ID.IsZero() {
			return nil, nil, nil, fmt.Errorf("invalid Edit Proposal response")
		}
		revision, cursor := result.Proposal.BaseProjectRevision, result.ActivityCursor
		return raw, &revision, &cursor, nil
	case "edit apply", "edit undo":
		var result command.EditCommitData
		if err := json.Unmarshal(data, &result); err != nil || result.Transaction.ID.IsZero() {
			return nil, nil, nil, fmt.Errorf("invalid Edit Transaction response")
		}
		revision, cursor := result.Transaction.CommittedProjectRevision, result.ActivityCursor
		return raw, &revision, &cursor, nil
	default:
		return nil, nil, nil, command.ErrUnknownCommand
	}
}
