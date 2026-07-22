package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func validateUndoEditRecord(record application.UndoEditRecord) error {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.RunID.IsZero() ||
		record.TurnID.IsZero() || record.TargetTransactionID.IsZero() || record.ProposalID.IsZero() ||
		record.ApplicationID.IsZero() || record.TransactionID.IsZero() || record.ActivityEventID.IsZero() ||
		record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorAgent ||
		!json.Valid(record.InputCanonical) || record.OccurredAt.IsZero() {
		return application.ErrEditInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrEditInvalid
	}
	if _, err := domain.ParseDigest(record.InputDigest.String()); err != nil {
		return application.ErrEditInvalid
	}
	return nil
}

func validateUndoTargetCurrent(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	target domain.EditTransaction,
) error {
	for _, change := range target.Changes {
		if change.Kind == domain.EntityNarrativeDocument || change.Kind == domain.EntitySequence {
			continue
		}
		current, err := loadEditEntityRevision(ctx, tx, projectID, change.Kind, change.ID)
		if err != nil || current != change.After {
			return application.ErrEditConflict
		}
	}
	return nil
}

func operationRevisionMap(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	operations []domain.NormalizedEditOperation,
) (map[string]domain.Revision, error) {
	result := make(map[string]domain.Revision, len(operations))
	for _, operation := range operations {
		kind, id, err := normalizedOperationIdentity(operation)
		if err != nil {
			return nil, err
		}
		revision, err := loadEditEntityRevision(ctx, tx, projectID, kind, id)
		if err != nil {
			return nil, application.ErrEditConflict
		}
		result[string(kind)+"\x00"+id] = revision
	}
	return result, nil
}

func rebaseOperationSet(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	operations []domain.NormalizedEditOperation,
	base map[string]domain.Revision,
) ([]domain.NormalizedEditOperation, map[string]domain.Revision, error) {
	result := make([]domain.NormalizedEditOperation, len(operations))
	final := make(map[string]domain.Revision, len(base))
	for key, revision := range base {
		final[key] = revision
	}
	for index, operation := range operations {
		kind, id, err := normalizedOperationIdentity(operation)
		if err != nil {
			return nil, nil, err
		}
		key := string(kind) + "\x00" + id
		current, exists := base[key]
		if !exists {
			current, err = loadEditEntityRevision(ctx, tx, projectID, kind, id)
			if err != nil {
				return nil, nil, application.ErrEditConflict
			}
		}
		next, err := current.Next()
		if err != nil {
			return nil, nil, err
		}
		final[key] = next
		result[index] = cloneNormalizedOperation(operation)
		setNormalizedOperationRevision(&result[index], next)
	}
	for index := range result {
		alignment := result[index].Alignment
		if alignment == nil || alignment.Status != domain.AlignmentExact {
			continue
		}
		nodeKey := string(domain.EntityNarrativeNode) + "\x00" + alignment.NarrativeNodeID.String()
		var err error
		alignment.NarrativeNodeRevision, err = revisionFromMapOrStore(
			ctx, tx, projectID, domain.EntityNarrativeNode, alignment.NarrativeNodeID.String(), final, nodeKey,
		)
		if err != nil {
			return nil, nil, err
		}
		for targetIndex := range alignment.Targets {
			target := &alignment.Targets[targetIndex]
			switch target.Type {
			case domain.AlignmentTargetCaption:
				key := string(domain.EntityCaption) + "\x00" + target.Caption.CaptionID.String()
				target.Caption.CaptionRevision, err = revisionFromMapOrStore(
					ctx, tx, projectID, domain.EntityCaption, target.Caption.CaptionID.String(), final, key,
				)
			case domain.AlignmentTargetClip:
				key := string(domain.EntityClip) + "\x00" + target.Clip.ClipID.String()
				target.Clip.ClipRevision, err = revisionFromMapOrStore(
					ctx, tx, projectID, domain.EntityClip, target.Clip.ClipID.String(), final, key,
				)
			case domain.AlignmentTargetTimeline:
				target.Timeline.SequenceRevision, err = loadEditEntityRevision(
					ctx, tx, projectID, domain.EntitySequence, alignment.SequenceID.String(),
				)
				if err == nil && proposalChangesSequence(operations) {
					target.Timeline.SequenceRevision, err = target.Timeline.SequenceRevision.Next()
				}
			default:
				err = application.ErrEditInvalid
			}
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return result, final, nil
}

func undoChanges(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	target domain.EditTransaction,
	operations []domain.NormalizedEditOperation,
) ([]domain.EntityRevisionChange, error) {
	changes := make([]domain.EntityRevisionChange, 0, len(target.Changes))
	operationKeys := normalizedOperationEntityKeys(operations)
	for _, operation := range operations {
		kind, id, err := normalizedOperationIdentity(operation)
		if err != nil {
			return nil, err
		}
		current, err := loadEditEntityRevision(ctx, tx, projectID, kind, id)
		if err != nil {
			return nil, application.ErrEditConflict
		}
		after := normalizedOperationRevision(operation)
		copyCurrent := current
		changes = append(changes, domain.EntityRevisionChange{
			Kind: kind, ID: id, Before: &copyCurrent, After: after,
			Tombstoned: normalizedOperationTombstoned(operation),
		})
	}
	for _, targetChange := range target.Changes {
		if targetChange.Kind == domain.EntityNarrativeDocument || targetChange.Kind == domain.EntitySequence {
			continue
		}
		key := string(targetChange.Kind) + "\x00" + targetChange.ID
		if _, operated := operationKeys[key]; operated {
			continue
		}
		current, err := loadEditEntityRevision(ctx, tx, projectID, targetChange.Kind, targetChange.ID)
		if err != nil || current != targetChange.After {
			return nil, application.ErrEditConflict
		}
		after, err := current.Next()
		if err != nil {
			return nil, err
		}
		copyCurrent := current
		changes = append(changes, domain.EntityRevisionChange{
			Kind: targetChange.Kind, ID: targetChange.ID, Before: &copyCurrent, After: after,
		})
	}
	sortEntityChanges(changes)
	return changes, nil
}

func normalizedOperationIdentity(
	operation domain.NormalizedEditOperation,
) (domain.EditEntityKind, string, error) {
	switch operation.Type {
	case domain.NormalizedRestoreProjectVersion:
		return "", "", application.ErrEditInvalid
	case domain.NormalizedPutNarrativeNode:
		if operation.NarrativeNode == nil || operation.NarrativeNode.ID().IsZero() {
			return "", "", application.ErrEditInvalid
		}
		return domain.EntityNarrativeNode, operation.NarrativeNode.ID().String(), nil
	case domain.NormalizedPutCaption:
		if operation.Caption == nil {
			return "", "", application.ErrEditInvalid
		}
		return domain.EntityCaption, operation.Caption.ID.String(), nil
	case domain.NormalizedPutAlignment:
		if operation.Alignment == nil {
			return "", "", application.ErrEditInvalid
		}
		return domain.EntityAlignment, operation.Alignment.ID.String(), nil
	case domain.NormalizedPutAsset:
		if operation.Asset == nil {
			return "", "", application.ErrEditInvalid
		}
		return domain.EntityAsset, operation.Asset.ID.String(), nil
	case domain.NormalizedPutClip:
		if operation.Clip == nil {
			return "", "", application.ErrEditInvalid
		}
		return domain.EntityClip, operation.Clip.ID.String(), nil
	case domain.NormalizedPutLinkGroup:
		if operation.LinkGroup == nil {
			return "", "", application.ErrEditInvalid
		}
		return domain.EntityLinkGroup, operation.LinkGroup.ID.String(), nil
	case domain.NormalizedPutTranscriptCorrection:
		if operation.TranscriptCorrection == nil {
			return "", "", application.ErrEditInvalid
		}
		return domain.EntityTranscriptCorrection, operation.TranscriptCorrection.ID.String(), nil
	default:
		return "", "", application.ErrEditInvalid
	}
}

func cloneNormalizedOperation(operation domain.NormalizedEditOperation) domain.NormalizedEditOperation {
	result := domain.NormalizedEditOperation{Type: operation.Type}
	if operation.NarrativeNode != nil {
		copyValue := cloneStoredNarrativeNode(*operation.NarrativeNode)
		result.NarrativeNode = &copyValue
	}
	if operation.TranscriptCorrection != nil {
		copyValue := *operation.TranscriptCorrection
		copyValue.SegmentIDs = append([]domain.TranscriptSegmentID(nil), operation.TranscriptCorrection.SegmentIDs...)
		result.TranscriptCorrection = &copyValue
	}
	if operation.Caption != nil {
		copyValue := *operation.Caption
		if operation.Caption.Provenance.Derivation != nil {
			derivation := *operation.Caption.Provenance.Derivation
			derivation.SegmentIDs = append(
				[]domain.TranscriptSegmentID(nil), operation.Caption.Provenance.Derivation.SegmentIDs...,
			)
			derivation.CorrectionRevisions = append(
				[]domain.TranscriptCorrectionRevisionRef(nil),
				operation.Caption.Provenance.Derivation.CorrectionRevisions...,
			)
			copyValue.Provenance.Derivation = &derivation
		}
		if operation.Caption.ProvenanceStatus != nil {
			status := *operation.Caption.ProvenanceStatus
			copyValue.ProvenanceStatus = &status
		}
		result.Caption = &copyValue
	}
	if operation.Alignment != nil {
		copyValue := *operation.Alignment
		copyValue.Targets = cloneStoredAlignmentTargets(operation.Alignment.Targets)
		result.Alignment = &copyValue
	}
	if operation.Asset != nil {
		copyValue := *operation.Asset
		result.Asset = &copyValue
	}
	if operation.Clip != nil {
		copyValue := *operation.Clip
		result.Clip = &copyValue
	}
	if operation.LinkGroup != nil {
		copyValue := *operation.LinkGroup
		result.LinkGroup = &copyValue
	}
	if operation.ProjectVersion != nil {
		copyValue := *operation.ProjectVersion
		result.ProjectVersion = &copyValue
	}
	return result
}

func cloneStoredAlignmentTargets(source []domain.AlignmentTarget) []domain.AlignmentTarget {
	result := make([]domain.AlignmentTarget, len(source))
	for index, target := range source {
		result[index] = target
		if target.Caption != nil {
			value := *target.Caption
			result[index].Caption = &value
		}
		if target.Clip != nil {
			value := *target.Clip
			result[index].Clip = &value
		}
		if target.Timeline != nil {
			value := *target.Timeline
			result[index].Timeline = &value
		}
	}
	return result
}

func setNormalizedOperationRevision(operation *domain.NormalizedEditOperation, revision domain.Revision) {
	if operation.NarrativeNode != nil {
		setStoredNarrativeNodeRevision(operation.NarrativeNode, revision)
	}
	if operation.TranscriptCorrection != nil {
		operation.TranscriptCorrection.Revision = revision
	}
	if operation.Caption != nil {
		operation.Caption.Revision = revision
	}
	if operation.Alignment != nil {
		operation.Alignment.Revision = revision
	}
	if operation.Asset != nil {
		operation.Asset.Revision = revision
	}
	if operation.Clip != nil {
		operation.Clip.Revision = revision
	}
	if operation.LinkGroup != nil {
		operation.LinkGroup.Revision = revision
	}
}

func normalizedOperationRevision(operation domain.NormalizedEditOperation) domain.Revision {
	if operation.NarrativeNode != nil {
		return operation.NarrativeNode.RevisionValue()
	}
	if operation.TranscriptCorrection != nil {
		return operation.TranscriptCorrection.Revision
	}
	if operation.Caption != nil {
		return operation.Caption.Revision
	}
	if operation.Alignment != nil {
		return operation.Alignment.Revision
	}
	if operation.Asset != nil {
		return operation.Asset.Revision
	}
	if operation.Clip != nil {
		return operation.Clip.Revision
	}
	if operation.LinkGroup != nil {
		return operation.LinkGroup.Revision
	}
	return 0
}

func normalizedOperationTombstoned(operation domain.NormalizedEditOperation) bool {
	if operation.NarrativeNode != nil {
		return operation.NarrativeNode.IsTombstoned()
	}
	if operation.TranscriptCorrection != nil {
		return operation.TranscriptCorrection.Tombstoned
	}
	if operation.Caption != nil {
		return operation.Caption.Tombstoned
	}
	if operation.Asset != nil {
		return operation.Asset.Tombstoned
	}
	if operation.Clip != nil {
		return operation.Clip.Tombstoned
	}
	if operation.LinkGroup != nil {
		return operation.LinkGroup.Tombstoned
	}
	return false
}

func cloneStoredNarrativeNode(source domain.NarrativeNodeState) domain.NarrativeNodeState {
	result := source
	if source.Section != nil {
		value := *source.Section
		result.Section = &value
	}
	if source.AuthoredText != nil {
		value := *source.AuthoredText
		result.AuthoredText = &value
	}
	if source.SourceExcerpt != nil {
		value := *source.SourceExcerpt
		value.Evidence.SegmentIDs = append([]domain.TranscriptSegmentID(nil), value.Evidence.SegmentIDs...)
		value.Evidence.CorrectionRevisions = append(
			[]domain.TranscriptCorrectionRevisionRef(nil), value.Evidence.CorrectionRevisions...,
		)
		result.SourceExcerpt = &value
	}
	if source.VisualIntent != nil {
		value := *source.VisualIntent
		result.VisualIntent = &value
	}
	if source.Note != nil {
		value := *source.Note
		result.Note = &value
	}
	return result
}

func setStoredNarrativeNodeRevision(state *domain.NarrativeNodeState, revision domain.Revision) {
	switch state.Kind {
	case domain.NarrativeNodeSection:
		state.Section.Revision = revision
	case domain.NarrativeNodeAuthoredText:
		state.AuthoredText.Revision = revision
	case domain.NarrativeNodeSourceExcerpt:
		state.SourceExcerpt.Revision = revision
	case domain.NarrativeNodeVisualIntent:
		state.VisualIntent.Revision = revision
	case domain.NarrativeNodeNote:
		state.Note.Revision = revision
	}
}

func revisionFromMapOrStore(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	kind domain.EditEntityKind,
	id string,
	revisions map[string]domain.Revision,
	key string,
) (domain.Revision, error) {
	if revision, exists := revisions[key]; exists {
		return revision, nil
	}
	revision, err := loadEditEntityRevision(ctx, tx, projectID, kind, id)
	if err != nil {
		return 0, application.ErrEditConflict
	}
	return revision, nil
}
