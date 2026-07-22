package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func applyProjectVersionRestore(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	reference domain.ProjectVersionRestoreRef,
	_ map[string]domain.EntityRevisionChange,
) error {
	_, target, err := loadStoredProjectVersionState(ctx, tx, projectID, reference.ID)
	if err != nil {
		return err
	}
	_, digest, err := domain.CanonicalDigest("open-cut/project-version-state", projectVersionStateSchema, target)
	if err != nil || digest != reference.Digest {
		return application.ErrProjectVersionInvalid
	}
	current, err := loadProjectVersionState(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if err := validateVersionStateRoots(current, target); err != nil {
		return err
	}
	leafChanges, err := projectVersionRestoreChanges(current, target)
	if err != nil {
		return err
	}
	changes := make(map[string]domain.EntityRevisionChange, len(leafChanges))
	for _, change := range leafChanges {
		changes[string(change.Kind)+"\x00"+change.ID] = change
	}
	if err := applyVersionAssets(ctx, tx, projectID, transactionID, current, target, changes); err != nil {
		return err
	}
	if err := applyVersionCorrections(ctx, tx, projectID, transactionID, current, target, changes); err != nil {
		return err
	}
	if err := applyVersionNarrative(ctx, tx, projectID, transactionID, current, target, changes); err != nil {
		return err
	}
	if err := applyVersionLinkGroups(ctx, tx, projectID, transactionID, current, target, changes); err != nil {
		return err
	}
	if err := applyVersionClips(ctx, tx, projectID, transactionID, current, target, changes); err != nil {
		return err
	}
	if err := applyVersionCaptions(ctx, tx, projectID, transactionID, current, target, changes); err != nil {
		return err
	}
	return applyVersionAlignments(ctx, tx, projectID, transactionID, current, target, changes)
}

func applyVersionAssets(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, transactionID domain.TransactionID, current, target projectVersionState, changes map[string]domain.EntityRevisionChange) error {
	targets := assetVersionMap(target.Assets)
	for _, before := range current.Assets {
		value, ok := targets[before.ID.String()]
		if !ok {
			value = before
			value.Tombstoned = true
		}
		change, err := versionChange(changes, domain.EntityAsset, before.ID.String())
		if err != nil {
			return err
		}
		value.Revision = change.After
		if err := applyAsset(ctx, tx, projectID, transactionID, value, change); err != nil {
			return err
		}
	}
	return nil
}

func applyVersionCorrections(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, transactionID domain.TransactionID, current, target projectVersionState, changes map[string]domain.EntityRevisionChange) error {
	targets := correctionVersionMap(target.TranscriptCorrections)
	for _, before := range current.TranscriptCorrections {
		value, ok := targets[before.ID.String()]
		if !ok {
			value = before
			value.Tombstoned = true
		}
		change, err := versionChange(changes, domain.EntityTranscriptCorrection, before.ID.String())
		if err != nil {
			return err
		}
		value.Revision = change.After
		if err := applyTranscriptCorrection(ctx, tx, projectID, transactionID, value, change); err != nil {
			return err
		}
	}
	return nil
}

func applyVersionNarrative(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, transactionID domain.TransactionID, current, target projectVersionState, changes map[string]domain.EntityRevisionChange) error {
	targets := narrativeVersionMap(target.NarrativeNodes)
	for _, before := range current.NarrativeNodes {
		if _, ok := targets[before.ID().String()]; ok {
			continue
		}
		value := cloneStoredNarrativeNode(before)
		setVersionNarrativeTombstone(&value, true)
		change, err := versionChange(changes, domain.EntityNarrativeNode, before.ID().String())
		if err != nil {
			return err
		}
		setStoredNarrativeNodeRevision(&value, change.After)
		if err := applyNarrativeNode(ctx, tx, projectID, transactionID, value, change); err != nil {
			return err
		}
	}
	for _, targetValue := range target.NarrativeNodes {
		value := cloneStoredNarrativeNode(targetValue)
		change, err := versionChange(changes, domain.EntityNarrativeNode, value.ID().String())
		if err != nil {
			return err
		}
		setStoredNarrativeNodeRevision(&value, change.After)
		if err := applyNarrativeNode(ctx, tx, projectID, transactionID, value, change); err != nil {
			return err
		}
	}
	return nil
}

func applyVersionLinkGroups(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, transactionID domain.TransactionID, current, target projectVersionState, changes map[string]domain.EntityRevisionChange) error {
	targets := linkGroupVersionMap(target.LinkGroups)
	for _, before := range current.LinkGroups {
		value, ok := targets[before.ID.String()]
		if !ok {
			value = before
			value.Tombstoned = true
		}
		change, err := versionChange(changes, domain.EntityLinkGroup, before.ID.String())
		if err != nil {
			return err
		}
		value.Revision = change.After
		if err := applyLinkGroup(ctx, tx, projectID, transactionID, value, change); err != nil {
			return err
		}
	}
	return nil
}

func applyVersionClips(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, transactionID domain.TransactionID, current, target projectVersionState, changes map[string]domain.EntityRevisionChange) error {
	targets := clipVersionMap(target.Clips)
	for _, before := range current.Clips {
		value, ok := targets[before.ID.String()]
		if !ok {
			value = before
			value.Tombstoned = true
			value.LinkGroupID = nil
		}
		change, err := versionChange(changes, domain.EntityClip, before.ID.String())
		if err != nil {
			return err
		}
		value.Revision = change.After
		if err := applyClip(ctx, tx, projectID, transactionID, value, change); err != nil {
			return err
		}
	}
	return nil
}

func applyVersionCaptions(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, transactionID domain.TransactionID, current, target projectVersionState, changes map[string]domain.EntityRevisionChange) error {
	targets := captionVersionMap(target.Captions)
	for _, before := range current.Captions {
		value, ok := targets[before.ID.String()]
		if !ok {
			value = before
			value.Tombstoned = true
		}
		change, err := versionChange(changes, domain.EntityCaption, before.ID.String())
		if err != nil {
			return err
		}
		value.Revision = change.After
		if err := applyCaption(ctx, tx, projectID, transactionID, value, change); err != nil {
			return err
		}
	}
	return nil
}

func applyVersionAlignments(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, transactionID domain.TransactionID, current, target projectVersionState, changes map[string]domain.EntityRevisionChange) error {
	targets := alignmentVersionMap(target.Alignments)
	for _, before := range current.Alignments {
		value, ok := targets[before.ID.String()]
		if !ok {
			value = before
			value.Status = domain.AlignmentUnbound
		}
		change, err := versionChange(changes, domain.EntityAlignment, before.ID.String())
		if err != nil {
			return err
		}
		value.Revision = change.After
		if value.Status == domain.AlignmentExact {
			if node, err := versionChange(changes, domain.EntityNarrativeNode, value.NarrativeNodeID.String()); err == nil {
				value.NarrativeNodeRevision = node.After
			}
			for index := range value.Targets {
				switch value.Targets[index].Type {
				case domain.AlignmentTargetCaption:
					change, err := versionChange(changes, domain.EntityCaption, value.Targets[index].Caption.CaptionID.String())
					if err != nil {
						return err
					}
					value.Targets[index].Caption.CaptionRevision = change.After
				case domain.AlignmentTargetClip:
					change, err := versionChange(changes, domain.EntityClip, value.Targets[index].Clip.ClipID.String())
					if err != nil {
						return err
					}
					value.Targets[index].Clip.ClipRevision = change.After
				case domain.AlignmentTargetTimeline:
					sequenceChange, err := current.SequenceRevision.Next()
					if err != nil {
						return err
					}
					value.Targets[index].Timeline.SequenceRevision = sequenceChange
				}
			}
		}
		if err := applyAlignment(ctx, tx, projectID, transactionID, value, change); err != nil {
			return err
		}
	}
	return nil
}

func versionChange(changes map[string]domain.EntityRevisionChange, kind domain.EditEntityKind, id string) (domain.EntityRevisionChange, error) {
	change, ok := changes[string(kind)+"\x00"+id]
	if !ok || change.Before == nil {
		return domain.EntityRevisionChange{}, application.ErrProjectVersionInvalid
	}
	return change, nil
}

func setVersionNarrativeTombstone(value *domain.NarrativeNodeState, tombstoned bool) {
	if value.Section != nil {
		value.Section.Tombstoned = tombstoned
	}
	if value.AuthoredText != nil {
		value.AuthoredText.Tombstoned = tombstoned
	}
	if value.SourceExcerpt != nil {
		value.SourceExcerpt.Tombstoned = tombstoned
	}
	if value.VisualIntent != nil {
		value.VisualIntent.Tombstoned = tombstoned
	}
	if value.Note != nil {
		value.Note.Tombstoned = tombstoned
	}
}

func narrativeVersionMap(values []domain.NarrativeNodeState) map[string]domain.NarrativeNodeState {
	result := make(map[string]domain.NarrativeNodeState, len(values))
	for _, value := range values {
		result[value.ID().String()] = value
	}
	return result
}
func correctionVersionMap(values []domain.TranscriptCorrectionState) map[string]domain.TranscriptCorrectionState {
	result := make(map[string]domain.TranscriptCorrectionState, len(values))
	for _, value := range values {
		result[value.ID.String()] = value
	}
	return result
}
func assetVersionMap(values []domain.AssetState) map[string]domain.AssetState {
	result := make(map[string]domain.AssetState, len(values))
	for _, value := range values {
		result[value.ID.String()] = value
	}
	return result
}
func linkGroupVersionMap(values []domain.LinkGroupState) map[string]domain.LinkGroupState {
	result := make(map[string]domain.LinkGroupState, len(values))
	for _, value := range values {
		result[value.ID.String()] = value
	}
	return result
}
func clipVersionMap(values []domain.ClipState) map[string]domain.ClipState {
	result := make(map[string]domain.ClipState, len(values))
	for _, value := range values {
		result[value.ID.String()] = value
	}
	return result
}
func captionVersionMap(values []domain.CaptionState) map[string]domain.CaptionState {
	result := make(map[string]domain.CaptionState, len(values))
	for _, value := range values {
		result[value.ID.String()] = value
	}
	return result
}
func alignmentVersionMap(values []domain.AlignmentState) map[string]domain.AlignmentState {
	result := make(map[string]domain.AlignmentState, len(values))
	for _, value := range values {
		result[value.ID.String()] = value
	}
	return result
}

func narrativeVersionKeys(values []domain.NarrativeNodeState) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value.ID().String()] = struct{}{}
	}
	return result
}
func correctionVersionKeys(values []domain.TranscriptCorrectionState) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value.ID.String()] = struct{}{}
	}
	return result
}
func assetVersionKeys(values []domain.AssetState) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value.ID.String()] = struct{}{}
	}
	return result
}
func linkGroupVersionKeys(values []domain.LinkGroupState) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value.ID.String()] = struct{}{}
	}
	return result
}
func clipVersionKeys(values []domain.ClipState) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value.ID.String()] = struct{}{}
	}
	return result
}
func captionVersionKeys(values []domain.CaptionState) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value.ID.String()] = struct{}{}
	}
	return result
}
func alignmentVersionKeys(values []domain.AlignmentState) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value.ID.String()] = struct{}{}
	}
	return result
}
