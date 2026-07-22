package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) RestoreProjectVersion(
	ctx context.Context,
	record application.RestoreProjectVersionRecord,
) (application.RestoreProjectVersionResult, error) {
	if record.ProjectID.IsZero() || record.VersionID.IsZero() || record.SafetyVersionID.IsZero() ||
		record.ProposalID.IsZero() || record.ApplicationID.IsZero() || record.TransactionID.IsZero() ||
		record.ActivityEventID.IsZero() || record.Creator.Validate() != nil || record.Creator.Kind != domain.ActorCreator ||
		!json.Valid(record.RequestCanonical) || record.OccurredAt.IsZero() {
		return application.RestoreProjectVersionResult{}, application.ErrProjectVersionInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	defer tx.Rollback()
	if result, found, err := loadRestoreVersionReplay(ctx, tx, record); err != nil {
		return application.RestoreProjectVersionResult{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return application.RestoreProjectVersionResult{}, err
		}
		result.Replayed = true
		return result, nil
	}
	target, targetState, err := loadStoredProjectVersionState(ctx, tx, record.ProjectID, record.VersionID)
	if err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	currentState, _, _, _, err := captureProjectVersionState(ctx, tx, record.ProjectID)
	if err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	if currentState.ProjectRevision != record.ExpectedProjectRevision ||
		target.CapturedProjectRevision == currentState.ProjectRevision {
		return application.RestoreProjectVersionResult{}, application.ErrEditConflict
	}
	if err := validateVersionStateRoots(currentState, targetState); err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	if err := ensureVersionTargetSubset(currentState, targetState); err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	safety, err := insertProjectVersionSnapshot(ctx, tx, projectVersionCapture{
		ID: record.SafetyVersionID, ProjectID: record.ProjectID, CreatorID: *record.Creator.CreatorID,
		Source: application.ProjectVersionPreRestore, Name: "Before restore",
		TriggerKind: "version", TriggerID: target.ID.String(), Retention: application.ProjectVersionPinned,
		CreatedAt: record.OccurredAt,
	})
	if err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	operation := domain.NormalizedEditOperation{Type: domain.NormalizedRestoreProjectVersion,
		ProjectVersion: &domain.ProjectVersionRestoreRef{ID: target.ID, Digest: target.Digest}}
	inverse := domain.NormalizedEditOperation{Type: domain.NormalizedRestoreProjectVersion,
		ProjectVersion: &domain.ProjectVersionRestoreRef{ID: safety.ID, Digest: safety.Digest}}
	proposal, canonical, err := buildProjectVersionRestoreProposal(record, currentState, operation, inverse)
	if err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	if err := insertEditProposalRow(ctx, tx, proposal, canonical); err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	transaction, err := prepareEditTransaction(ctx, tx, proposal, record.TransactionID, record.OccurredAt, nil)
	if err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	if err := persistAndApplyEditTransaction(ctx, tx, proposal, transaction, domain.RunID{}, domain.TurnID{}); err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	if err := insertProposalApplication(ctx, tx, record.ApplicationID, proposal, record.Creator,
		record.RequestID, record.RequestDigest, transaction.ID, record.OccurredAt); err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	if err := markEditProposalApplied(ctx, tx, proposal.ID, transaction.ID, record.OccurredAt); err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	cursor, err := appendEditCommittedActivity(ctx, tx, transaction, record.ActivityEventID, domain.RunID{})
	if err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO project_version_requests (
  creator_id, request_id, command, input_digest, input_json, project_id, version_id,
  safety_version_id, transaction_id, activity_event_id, created_at
) VALUES (?, ?, 'restore', ?, ?, ?, ?, ?, ?, ?, ?)`, record.Creator.IDString(), record.RequestID.String(),
		record.RequestDigest.String(), string(record.RequestCanonical), record.ProjectID.String(), target.ID.String(),
		safety.ID.String(), transaction.ID.String(), record.ActivityEventID.String(), formatInstant(record.OccurredAt)); err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.RestoreProjectVersionResult{}, err
	}
	return application.RestoreProjectVersionResult{Version: target, SafetyVersion: safety,
		TransactionID: transaction.ID, CommittedProjectRevision: transaction.CommittedProjectRevision,
		ActivityCursor: cursor}, nil
}

func buildProjectVersionRestoreProposal(
	record application.RestoreProjectVersionRecord,
	current projectVersionState,
	operation domain.NormalizedEditOperation,
	inverse domain.NormalizedEditOperation,
) (domain.EditProposal, []byte, error) {
	proposal := domain.EditProposal{
		ID: record.ProposalID, ProjectID: record.ProjectID, SequenceID: &current.SequenceID,
		RequestID: record.RequestID, Actor: record.Creator,
		Intent: "Restore project version " + record.VersionID.String(), BaseProjectRevision: current.ProjectRevision,
		Preconditions: []domain.EntityPrecondition{}, Allocation: []domain.LocalAllocation{},
		Operations: []domain.NormalizedEditOperation{operation}, InversePreview: []domain.NormalizedEditOperation{inverse},
		Changes: []domain.EntityRevisionChange{}, Impact: domain.EditImpact{Classifier: domain.EditImpactClassifierV1, Class: "reversible-local"},
		Status: domain.ProposalOpen, CreatedAt: record.OccurredAt.UTC(),
	}
	canonical, digest, err := domain.CanonicalDigest("open-cut/edit-proposal", domain.EditProposalSchema, struct {
		Actor               domain.ActorRef                  `json:"actor"`
		Allocation          []domain.LocalAllocation         `json:"allocation"`
		BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
		Changes             []domain.EntityRevisionChange    `json:"changes"`
		Impact              domain.EditImpact                `json:"impact"`
		Intent              string                           `json:"intent"`
		Inverse             []domain.NormalizedEditOperation `json:"inverse"`
		Operations          []domain.NormalizedEditOperation `json:"operations"`
		Preconditions       []domain.EntityPrecondition      `json:"preconditions"`
		ProjectID           domain.ProjectID                 `json:"projectId"`
		SequenceID          domain.SequenceID                `json:"sequenceId"`
	}{Actor: proposal.Actor, Allocation: proposal.Allocation, BaseProjectRevision: proposal.BaseProjectRevision,
		Changes: proposal.Changes, Impact: proposal.Impact, Intent: proposal.Intent,
		Inverse: proposal.InversePreview, Operations: proposal.Operations, Preconditions: proposal.Preconditions,
		ProjectID: proposal.ProjectID, SequenceID: *proposal.SequenceID})
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	proposal.Digest = digest
	return proposal, canonical, nil
}

func loadStoredProjectVersionState(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	versionID domain.ProjectVersionID,
) (application.ProjectVersion, projectVersionState, error) {
	version, err := scanProjectVersion(tx.QueryRowContext(ctx, projectVersionSelect+`WHERE id = ? AND project_id = ?`,
		versionID.String(), projectID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return application.ProjectVersion{}, projectVersionState{}, application.ErrProjectVersionNotFound
	}
	if err != nil {
		return application.ProjectVersion{}, projectVersionState{}, err
	}
	var schema, digestValue string
	var bytes []byte
	if err := tx.QueryRowContext(ctx, `SELECT state_schema, state_digest, state_bytes FROM project_versions WHERE id = ?`,
		versionID.String()).Scan(&schema, &digestValue, &bytes); err != nil {
		return application.ProjectVersion{}, projectVersionState{}, err
	}
	if schema != projectVersionStateSchema || digestValue != version.Digest.String() {
		return application.ProjectVersion{}, projectVersionState{}, application.ErrProjectVersionInvalid
	}
	state, err := decodeProjectVersionState(bytes, version.Digest)
	if err != nil {
		return application.ProjectVersion{}, projectVersionState{}, err
	}
	if state.ProjectID != projectID || state.ProjectRevision != version.CapturedProjectRevision {
		return application.ProjectVersion{}, projectVersionState{}, application.ErrProjectVersionInvalid
	}
	return version, state, nil
}

func validateVersionStateRoots(current, target projectVersionState) error {
	if current.ProjectID != target.ProjectID || current.NarrativeDocumentID != target.NarrativeDocumentID ||
		current.SequenceID != target.SequenceID || !reflect.DeepEqual(current.Format, target.Format) ||
		len(current.Tracks) != len(target.Tracks) {
		return application.ErrProjectVersionInvalid
	}
	for index := range current.Tracks {
		if current.Tracks[index].ID != target.Tracks[index].ID || current.Tracks[index].Type != target.Tracks[index].Type ||
			current.Tracks[index].Label != target.Tracks[index].Label || current.Tracks[index].OrderKey != target.Tracks[index].OrderKey {
			return application.ErrProjectVersionInvalid
		}
	}
	return nil
}

func projectVersionRestoreChanges(current, target projectVersionState) ([]domain.EntityRevisionChange, error) {
	changes := make([]domain.EntityRevisionChange, 0, len(current.NarrativeNodes)+len(current.Assets)+len(current.Clips)+len(current.Captions)+len(current.Alignments))
	appendChange := func(kind domain.EditEntityKind, id string, before domain.Revision, tombstoned bool) error {
		after, err := before.Next()
		if err != nil {
			return err
		}
		copyBefore := before
		changes = append(changes, domain.EntityRevisionChange{Kind: kind, ID: id, Before: &copyBefore, After: after, Tombstoned: tombstoned})
		return nil
	}
	targetNodes := narrativeVersionMap(target.NarrativeNodes)
	for _, value := range current.NarrativeNodes {
		targetValue, ok := targetNodes[value.ID().String()]
		if err := appendChange(domain.EntityNarrativeNode, value.ID().String(), value.RevisionValue(), !ok || targetValue.IsTombstoned()); err != nil {
			return nil, err
		}
	}
	targetCorrections := correctionVersionMap(target.TranscriptCorrections)
	for _, value := range current.TranscriptCorrections {
		targetValue, ok := targetCorrections[value.ID.String()]
		if err := appendChange(domain.EntityTranscriptCorrection, value.ID.String(), value.Revision, !ok || targetValue.Tombstoned); err != nil {
			return nil, err
		}
	}
	targetAssets := assetVersionMap(target.Assets)
	for _, value := range current.Assets {
		targetValue, ok := targetAssets[value.ID.String()]
		if err := appendChange(domain.EntityAsset, value.ID.String(), value.Revision, !ok || targetValue.Tombstoned); err != nil {
			return nil, err
		}
	}
	targetGroups := linkGroupVersionMap(target.LinkGroups)
	for _, value := range current.LinkGroups {
		targetValue, ok := targetGroups[value.ID.String()]
		if err := appendChange(domain.EntityLinkGroup, value.ID.String(), value.Revision, !ok || targetValue.Tombstoned); err != nil {
			return nil, err
		}
	}
	targetClips := clipVersionMap(target.Clips)
	for _, value := range current.Clips {
		targetValue, ok := targetClips[value.ID.String()]
		if err := appendChange(domain.EntityClip, value.ID.String(), value.Revision, !ok || targetValue.Tombstoned); err != nil {
			return nil, err
		}
	}
	targetCaptions := captionVersionMap(target.Captions)
	for _, value := range current.Captions {
		targetValue, ok := targetCaptions[value.ID.String()]
		if err := appendChange(domain.EntityCaption, value.ID.String(), value.Revision, !ok || targetValue.Tombstoned); err != nil {
			return nil, err
		}
	}
	targetAlignments := alignmentVersionMap(target.Alignments)
	for _, value := range current.Alignments {
		_, ok := targetAlignments[value.ID.String()]
		if err := appendChange(domain.EntityAlignment, value.ID.String(), value.Revision, !ok); err != nil {
			return nil, err
		}
	}
	if err := ensureVersionTargetSubset(current, target); err != nil {
		return nil, err
	}
	sortVersionChanges(changes)
	return changes, nil
}

func ensureVersionTargetSubset(current, target projectVersionState) error {
	checks := []struct{ current, target map[string]struct{} }{
		{narrativeVersionKeys(current.NarrativeNodes), narrativeVersionKeys(target.NarrativeNodes)},
		{correctionVersionKeys(current.TranscriptCorrections), correctionVersionKeys(target.TranscriptCorrections)},
		{assetVersionKeys(current.Assets), assetVersionKeys(target.Assets)}, {linkGroupVersionKeys(current.LinkGroups), linkGroupVersionKeys(target.LinkGroups)},
		{clipVersionKeys(current.Clips), clipVersionKeys(target.Clips)}, {captionVersionKeys(current.Captions), captionVersionKeys(target.Captions)},
		{alignmentVersionKeys(current.Alignments), alignmentVersionKeys(target.Alignments)},
	}
	for _, check := range checks {
		for id := range check.target {
			if _, ok := check.current[id]; !ok {
				return application.ErrProjectVersionInvalid
			}
		}
	}
	return nil
}

func loadRestoreVersionReplay(ctx context.Context, tx *sql.Tx, record application.RestoreProjectVersionRecord) (application.RestoreProjectVersionResult, bool, error) {
	var digestValue, projectValue, versionValue, safetyValue, transactionValue, eventValue string
	err := tx.QueryRowContext(ctx, `
SELECT input_digest, project_id, version_id, safety_version_id, transaction_id, activity_event_id
FROM project_version_requests WHERE creator_id = ? AND request_id = ?`, record.Creator.IDString(), record.RequestID.String()).Scan(
		&digestValue, &projectValue, &versionValue, &safetyValue, &transactionValue, &eventValue)
	if errors.Is(err, sql.ErrNoRows) {
		return application.RestoreProjectVersionResult{}, false, nil
	}
	if err != nil {
		return application.RestoreProjectVersionResult{}, false, err
	}
	if digestValue != record.RequestDigest.String() || projectValue != record.ProjectID.String() || versionValue != record.VersionID.String() {
		return application.RestoreProjectVersionResult{}, false, application.ErrProjectVersionRequestReused
	}
	version, err := scanProjectVersion(tx.QueryRowContext(ctx, projectVersionSelect+`WHERE id = ?`, versionValue))
	if err != nil {
		return application.RestoreProjectVersionResult{}, false, err
	}
	safety, err := scanProjectVersion(tx.QueryRowContext(ctx, projectVersionSelect+`WHERE id = ?`, safetyValue))
	if err != nil {
		return application.RestoreProjectVersionResult{}, false, err
	}
	transactionID, err := domain.ParseTransactionID(transactionValue)
	if err != nil {
		return application.RestoreProjectVersionResult{}, false, err
	}
	transaction, err := loadEditTransaction(ctx, tx, record.ProjectID, transactionID)
	if err != nil {
		return application.RestoreProjectVersionResult{}, false, err
	}
	cursor, err := activityCursorForEvent(ctx, tx, eventValue)
	return application.RestoreProjectVersionResult{Version: version, SafetyVersion: safety, TransactionID: transactionID,
		CommittedProjectRevision: transaction.CommittedProjectRevision, ActivityCursor: cursor}, true, err
}
