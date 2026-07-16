package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func ensureVisualIntentState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.NarrativeNodeID,
) error {
	if _, exists := state.VisualIntents[id.String()]; exists {
		return nil
	}
	var nodeValue, documentValue, parentValue, purposeValue, languageValue, description string
	var revisionValue uint64
	var orderIndex int64
	var tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT node.id, node.document_id, node.parent_id, node.revision, node.order_index,
       node.tombstoned, value.purpose, value.language, value.description
FROM narrative_nodes node
JOIN narrative_visual_intent_values value ON value.id = node.id
WHERE node.id = ? AND node.project_id = ? AND node.kind = 'visual-intent'`,
		id.String(), state.ProjectID.String()).Scan(
		&nodeValue, &documentValue, &parentValue, &revisionValue, &orderIndex,
		&tombstoned, &purposeValue, &languageValue, &description,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditInvalid
	}
	if err != nil {
		return err
	}
	nodeID, nodeErr := domain.ParseNarrativeNodeID(nodeValue)
	documentID, documentErr := domain.ParseNarrativeDocumentID(documentValue)
	parentID, parentErr := domain.ParseNarrativeNodeID(parentValue)
	revision, revisionErr := domain.NewRevision(revisionValue)
	language, languageErr := domain.ParseCaptionLanguage(languageValue)
	purpose := domain.VisualIntentPurpose(purposeValue)
	if nodeErr != nil || documentErr != nil || parentErr != nil || revisionErr != nil ||
		languageErr != nil || purpose.Validate() != nil || description == "" {
		return application.ErrEditInvalid
	}
	after, err := loadNarrativeAfter(ctx, tx, documentID, &parentID, orderIndex, nodeID)
	if err != nil {
		return err
	}
	state.VisualIntents[nodeValue] = domain.VisualIntentState{
		ID: nodeID, Revision: revision, DocumentID: documentID, ParentID: parentID,
		AfterNodeID: after, Purpose: purpose, Language: language,
		Description: description, Tombstoned: tombstoned,
	}
	return nil
}

func ensureNoteState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.NarrativeNodeID,
) error {
	if _, exists := state.Notes[id.String()]; exists {
		return nil
	}
	var nodeValue, documentValue, parentValue, languageValue, text string
	var revisionValue uint64
	var orderIndex int64
	var tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT node.id, node.document_id, node.parent_id, node.revision, node.order_index,
       node.tombstoned, value.language, value.text
FROM narrative_nodes node
JOIN narrative_note_values value ON value.id = node.id
WHERE node.id = ? AND node.project_id = ? AND node.kind = 'note'`,
		id.String(), state.ProjectID.String()).Scan(
		&nodeValue, &documentValue, &parentValue, &revisionValue, &orderIndex,
		&tombstoned, &languageValue, &text,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditInvalid
	}
	if err != nil {
		return err
	}
	nodeID, nodeErr := domain.ParseNarrativeNodeID(nodeValue)
	documentID, documentErr := domain.ParseNarrativeDocumentID(documentValue)
	parentID, parentErr := domain.ParseNarrativeNodeID(parentValue)
	revision, revisionErr := domain.NewRevision(revisionValue)
	language, languageErr := domain.ParseCaptionLanguage(languageValue)
	if nodeErr != nil || documentErr != nil || parentErr != nil || revisionErr != nil ||
		languageErr != nil || text == "" {
		return application.ErrEditInvalid
	}
	after, err := loadNarrativeAfter(ctx, tx, documentID, &parentID, orderIndex, nodeID)
	if err != nil {
		return err
	}
	state.Notes[nodeValue] = domain.NoteState{
		ID: nodeID, Revision: revision, DocumentID: documentID, ParentID: parentID,
		AfterNodeID: after, Language: language, Text: text, Tombstoned: tombstoned,
	}
	return nil
}

func normalizationNarrativeParent(
	state application.EditNormalizationState,
	id domain.NarrativeNodeID,
) *domain.NarrativeNodeID {
	if value, exists := state.Sections[id.String()]; exists {
		return value.ParentID
	}
	if value, exists := state.AuthoredTexts[id.String()]; exists {
		result := value.ParentID
		return &result
	}
	if value, exists := state.SourceExcerpts[id.String()]; exists {
		result := value.ParentID
		return &result
	}
	if value, exists := state.VisualIntents[id.String()]; exists {
		result := value.ParentID
		return &result
	}
	if value, exists := state.Notes[id.String()]; exists {
		result := value.ParentID
		return &result
	}
	return nil
}
