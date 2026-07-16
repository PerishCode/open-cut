package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type narrativeProjectionIdentity struct {
	id          domain.NarrativeNodeID
	revision    domain.Revision
	documentID  domain.NarrativeDocumentID
	parentID    *domain.NarrativeNodeID
	afterNodeID *domain.NarrativeNodeID
	kind        domain.NarrativeNodeKind
	tombstoned  bool
}

func applyNarrativeNode(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	transactionID domain.TransactionID,
	state domain.NarrativeNodeState,
	change domain.EntityRevisionChange,
) error {
	identity, err := narrativeProjectionState(state)
	if err != nil || identity.id.String() != change.ID || identity.revision != change.After {
		return application.ErrEditInvalid
	}
	if change.Before == nil {
		if identity.parentID == nil || identity.tombstoned {
			return application.ErrEditInvalid
		}
		index, err := narrativeInsertIndex(
			ctx, tx, projectID, identity.documentID, *identity.parentID, identity.afterNodeID,
		)
		if err != nil {
			return err
		}
		if err := shiftNarrativeSiblings(ctx, tx, identity.documentID, *identity.parentID, index, 1); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO narrative_nodes (
  id, project_id, document_id, parent_id, revision, kind, order_index,
  tombstoned, last_transaction_id
) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`, identity.id.String(), projectID.String(),
			identity.documentID.String(), identity.parentID.String(), identity.revision.Value(),
			identity.kind, index, transactionID.String()); err != nil {
			return err
		}
		return insertNarrativePayload(ctx, tx, state)
	}

	var oldDocument, oldKind string
	var oldParent sql.NullString
	var oldOrder int64
	var oldTombstoned bool
	if err := tx.QueryRowContext(ctx, `
SELECT document_id, parent_id, kind, order_index, tombstoned
FROM narrative_nodes
WHERE id = ? AND project_id = ? AND revision = ?`, identity.id.String(), projectID.String(),
		change.Before.Value()).Scan(&oldDocument, &oldParent, &oldKind, &oldOrder, &oldTombstoned); err != nil {
		return application.ErrEditConflict
	}
	if oldDocument != identity.documentID.String() || oldKind != string(identity.kind) ||
		(oldParent.Valid != (identity.parentID != nil)) {
		return application.ErrEditInvalid
	}
	if identity.parentID == nil {
		if oldParent.Valid || identity.afterNodeID != nil || identity.tombstoned {
			return application.ErrEditInvalid
		}
		result, err := tx.ExecContext(ctx, `
UPDATE narrative_nodes SET revision = ?, last_transaction_id = ?
WHERE id = ? AND project_id = ? AND revision = ?`, identity.revision.Value(), transactionID.String(),
			identity.id.String(), projectID.String(), change.Before.Value())
		if err := requireOneEditRow(result, err); err != nil {
			return err
		}
		return updateNarrativePayload(ctx, tx, state)
	}

	if !oldTombstoned {
		if _, err := tx.ExecContext(ctx, `
UPDATE narrative_nodes SET tombstoned = 1
WHERE id = ? AND project_id = ? AND revision = ?`, identity.id.String(), projectID.String(),
			change.Before.Value()); err != nil {
			return err
		}
		oldParentID, parseErr := domain.ParseNarrativeNodeID(oldParent.String)
		if parseErr != nil {
			return application.ErrEditInvalid
		}
		if err := compactNarrativeSiblings(ctx, tx, identity.documentID, oldParentID, oldOrder); err != nil {
			return err
		}
	}
	newOrder := oldOrder
	if !identity.tombstoned {
		newOrder, err = narrativeInsertIndex(
			ctx, tx, projectID, identity.documentID, *identity.parentID, identity.afterNodeID,
		)
		if err != nil {
			return err
		}
		if err := shiftNarrativeSiblings(ctx, tx, identity.documentID, *identity.parentID, newOrder, 1); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `
UPDATE narrative_nodes
SET parent_id = ?, revision = ?, order_index = ?, tombstoned = ?, last_transaction_id = ?
WHERE id = ? AND project_id = ? AND revision = ?`, identity.parentID.String(), identity.revision.Value(),
		newOrder, identity.tombstoned, transactionID.String(), identity.id.String(), projectID.String(),
		change.Before.Value())
	if err := requireOneEditRow(result, err); err != nil {
		return err
	}
	return updateNarrativePayload(ctx, tx, state)
}

func narrativeProjectionState(state domain.NarrativeNodeState) (narrativeProjectionIdentity, error) {
	result := narrativeProjectionIdentity{kind: state.Kind}
	switch state.Kind {
	case domain.NarrativeNodeSection:
		if state.Section == nil || narrativePayloadCount(state) != 1 {
			return result, application.ErrEditInvalid
		}
		result.id, result.revision, result.documentID = state.Section.ID, state.Section.Revision, state.Section.DocumentID
		result.parentID, result.afterNodeID, result.tombstoned = state.Section.ParentID, state.Section.AfterNodeID, state.Section.Tombstoned
	case domain.NarrativeNodeAuthoredText:
		if state.AuthoredText == nil || narrativePayloadCount(state) != 1 || state.AuthoredText.Purpose.Validate() != nil ||
			state.AuthoredText.Language.Validate() != nil || state.AuthoredText.Text == "" {
			return result, application.ErrEditInvalid
		}
		value := state.AuthoredText
		result.id, result.revision, result.documentID = value.ID, value.Revision, value.DocumentID
		result.parentID, result.afterNodeID, result.tombstoned = &value.ParentID, value.AfterNodeID, value.Tombstoned
	case domain.NarrativeNodeSourceExcerpt:
		if state.SourceExcerpt == nil || narrativePayloadCount(state) != 1 || state.SourceExcerpt.Language.Validate() != nil ||
			len(state.SourceExcerpt.Evidence.SegmentIDs) == 0 || len(state.SourceExcerpt.Evidence.SegmentIDs) > 256 ||
			len(state.SourceExcerpt.Evidence.CorrectionRevisions) > 256 {
			return result, application.ErrEditInvalid
		}
		value := state.SourceExcerpt
		result.id, result.revision, result.documentID = value.ID, value.Revision, value.DocumentID
		result.parentID, result.afterNodeID, result.tombstoned = &value.ParentID, value.AfterNodeID, value.Tombstoned
	case domain.NarrativeNodeVisualIntent:
		if state.VisualIntent == nil || narrativePayloadCount(state) != 1 || state.VisualIntent.Purpose.Validate() != nil ||
			state.VisualIntent.Language.Validate() != nil || state.VisualIntent.Description == "" {
			return result, application.ErrEditInvalid
		}
		value := state.VisualIntent
		result.id, result.revision, result.documentID = value.ID, value.Revision, value.DocumentID
		result.parentID, result.afterNodeID, result.tombstoned = &value.ParentID, value.AfterNodeID, value.Tombstoned
	case domain.NarrativeNodeNote:
		if state.Note == nil || narrativePayloadCount(state) != 1 || state.Note.Language.Validate() != nil ||
			state.Note.Text == "" {
			return result, application.ErrEditInvalid
		}
		value := state.Note
		result.id, result.revision, result.documentID = value.ID, value.Revision, value.DocumentID
		result.parentID, result.afterNodeID, result.tombstoned = &value.ParentID, value.AfterNodeID, value.Tombstoned
	default:
		return result, application.ErrEditInvalid
	}
	if result.id.IsZero() || result.revision.Value() < 1 || result.documentID.IsZero() {
		return result, application.ErrEditInvalid
	}
	return result, nil
}

func narrativePayloadCount(state domain.NarrativeNodeState) int {
	count := 0
	for _, present := range []bool{
		state.Section != nil, state.AuthoredText != nil, state.SourceExcerpt != nil,
		state.VisualIntent != nil, state.Note != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

func narrativeInsertIndex(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	documentID domain.NarrativeDocumentID,
	parentID domain.NarrativeNodeID,
	after *domain.NarrativeNodeID,
) (int64, error) {
	if after == nil {
		return 0, nil
	}
	var index int64
	if err := tx.QueryRowContext(ctx, `
SELECT order_index + 1 FROM narrative_nodes
WHERE id = ? AND project_id = ? AND document_id = ? AND parent_id = ? AND tombstoned = 0`,
		after.String(), projectID.String(), documentID.String(), parentID.String()).Scan(&index); err != nil {
		return 0, application.ErrEditConflict
	}
	return index, nil
}

func shiftNarrativeSiblings(
	ctx context.Context,
	tx *sql.Tx,
	documentID domain.NarrativeDocumentID,
	parentID domain.NarrativeNodeID,
	from int64,
	delta int,
) error {
	_, err := tx.ExecContext(ctx, `
UPDATE narrative_nodes SET order_index = order_index + ?
WHERE document_id = ? AND parent_id = ? AND tombstoned = 0 AND order_index >= ?`,
		delta, documentID.String(), parentID.String(), from)
	return err
}

func compactNarrativeSiblings(
	ctx context.Context,
	tx *sql.Tx,
	documentID domain.NarrativeDocumentID,
	parentID domain.NarrativeNodeID,
	removed int64,
) error {
	_, err := tx.ExecContext(ctx, `
UPDATE narrative_nodes SET order_index = order_index - 1
WHERE document_id = ? AND parent_id = ? AND tombstoned = 0 AND order_index > ?`,
		documentID.String(), parentID.String(), removed)
	return err
}

func insertNarrativePayload(ctx context.Context, tx *sql.Tx, state domain.NarrativeNodeState) error {
	switch state.Kind {
	case domain.NarrativeNodeSection:
		_, err := tx.ExecContext(ctx, `
INSERT INTO narrative_section_values (id, title, language) VALUES (?, ?, ?)`,
			state.Section.ID.String(), state.Section.Title, state.Section.Language.String())
		return err
	case domain.NarrativeNodeAuthoredText:
		_, err := tx.ExecContext(ctx, `
INSERT INTO narrative_authored_text_values (id, purpose, language, text) VALUES (?, ?, ?, ?)`,
			state.AuthoredText.ID.String(), state.AuthoredText.Purpose,
			state.AuthoredText.Language.String(), state.AuthoredText.Text)
		return err
	case domain.NarrativeNodeSourceExcerpt:
		return insertSourceExcerptPayload(ctx, tx, *state.SourceExcerpt)
	case domain.NarrativeNodeVisualIntent:
		_, err := tx.ExecContext(ctx, `
INSERT INTO narrative_visual_intent_values (id, purpose, language, description) VALUES (?, ?, ?, ?)`,
			state.VisualIntent.ID.String(), state.VisualIntent.Purpose,
			state.VisualIntent.Language.String(), state.VisualIntent.Description)
		return err
	case domain.NarrativeNodeNote:
		_, err := tx.ExecContext(ctx, `
INSERT INTO narrative_note_values (id, language, text) VALUES (?, ?, ?)`,
			state.Note.ID.String(), state.Note.Language.String(), state.Note.Text)
		return err
	default:
		return application.ErrEditInvalid
	}
}

func updateNarrativePayload(ctx context.Context, tx *sql.Tx, state domain.NarrativeNodeState) error {
	var result sql.Result
	var err error
	switch state.Kind {
	case domain.NarrativeNodeSection:
		result, err = tx.ExecContext(ctx, `
UPDATE narrative_section_values SET title = ?, language = ? WHERE id = ?`,
			state.Section.Title, state.Section.Language.String(), state.Section.ID.String())
	case domain.NarrativeNodeAuthoredText:
		result, err = tx.ExecContext(ctx, `
UPDATE narrative_authored_text_values SET purpose = ?, language = ?, text = ? WHERE id = ?`,
			state.AuthoredText.Purpose, state.AuthoredText.Language.String(),
			state.AuthoredText.Text, state.AuthoredText.ID.String())
	case domain.NarrativeNodeSourceExcerpt:
		return nil
	case domain.NarrativeNodeVisualIntent:
		result, err = tx.ExecContext(ctx, `
UPDATE narrative_visual_intent_values SET purpose = ?, language = ?, description = ? WHERE id = ?`,
			state.VisualIntent.Purpose, state.VisualIntent.Language.String(),
			state.VisualIntent.Description, state.VisualIntent.ID.String())
	case domain.NarrativeNodeNote:
		result, err = tx.ExecContext(ctx, `
UPDATE narrative_note_values SET language = ?, text = ? WHERE id = ?`,
			state.Note.Language.String(), state.Note.Text, state.Note.ID.String())
	default:
		return application.ErrEditInvalid
	}
	return requireOneEditRow(result, err)
}

func insertSourceExcerptPayload(ctx context.Context, tx *sql.Tx, state domain.SourceExcerptState) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO narrative_source_excerpt_values (
  id, asset_id, accepted_fingerprint, source_start_value, source_start_scale,
  source_duration_value, source_duration_scale, language, effective_text,
  transcript_artifact_id, source_stream_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, state.ID.String(), state.AssetID.String(),
		state.AcceptedFingerprint.String(), state.SourceRange.Start.Value.Value(), state.SourceRange.Start.Scale,
		state.SourceRange.Duration.Value.Value(), state.SourceRange.Duration.Scale, state.Language.String(),
		state.EffectiveText, state.Evidence.ArtifactID.String(), state.Evidence.SourceStreamID.String()); err != nil {
		return err
	}
	for ordinal, segmentID := range state.Evidence.SegmentIDs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO narrative_source_excerpt_segments (node_id, ordinal, segment_id)
VALUES (?, ?, ?)`, state.ID.String(), ordinal, segmentID.String()); err != nil {
			return err
		}
	}
	for ordinal, correction := range state.Evidence.CorrectionRevisions {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO narrative_source_excerpt_corrections (
  node_id, ordinal, correction_id, correction_revision
) VALUES (?, ?, ?, ?)`, state.ID.String(), ordinal, correction.ID.String(), correction.Revision.Value()); err != nil {
			return err
		}
	}
	return nil
}
