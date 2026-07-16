package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ReadNarrativeSubtree(
	ctx context.Context,
	query application.NarrativeSubtreeQuery,
) (application.NarrativeSubtreeResult, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.NarrativeSubtreeResult{}, err
	}
	defer tx.Rollback()
	var documentValue, parentValue, title, languageValue string
	var documentRevision, parentRevision uint64
	err = tx.QueryRowContext(ctx, `
SELECT d.id, d.revision, n.id, n.revision, value.title, value.language
FROM narrative_documents d
JOIN narrative_nodes n ON n.id = ? AND n.document_id = d.id AND n.kind = 'section'
JOIN narrative_section_values value ON value.id = n.id
WHERE d.id = ? AND d.project_id = ?`,
		query.ParentID.String(), query.DocumentID.String(), query.ProjectID.String()).Scan(
		&documentValue, &documentRevision, &parentValue, &parentRevision, &title, &languageValue)
	if errors.Is(err, sql.ErrNoRows) {
		return application.NarrativeSubtreeResult{}, application.ErrEditEntityNotFound
	}
	if err != nil {
		return application.NarrativeSubtreeResult{}, err
	}
	if query.AfterID != "" &&
		(query.AfterDocumentRevision.Value() != documentRevision || query.AfterParentRevision.Value() != parentRevision) {
		return application.NarrativeSubtreeResult{}, application.ErrInvalidEditCursor
	}
	afterIndex := int64(-1)
	if query.AfterID != "" {
		if err := tx.QueryRowContext(ctx, `
SELECT order_index FROM narrative_nodes
WHERE id = ? AND document_id = ? AND parent_id = ? AND tombstoned = 0`,
			query.AfterID, documentValue, parentValue).Scan(&afterIndex); err != nil {
			return application.NarrativeSubtreeResult{}, application.ErrInvalidEditCursor
		}
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, kind, order_index
FROM narrative_nodes
WHERE document_id = ? AND parent_id = ? AND tombstoned = 0
  AND (order_index > ? OR (order_index = ? AND id > ?))
ORDER BY order_index, id LIMIT ?`,
		documentValue, parentValue, afterIndex, afterIndex, query.AfterID, query.Limit+1)
	if err != nil {
		return application.NarrativeSubtreeResult{}, err
	}
	defer rows.Close()
	documentID, _ := domain.ParseNarrativeDocumentID(documentValue)
	parentID, _ := domain.ParseNarrativeNodeID(parentValue)
	documentRev, _ := domain.NewRevision(documentRevision)
	parentRev, _ := domain.NewRevision(parentRevision)
	language, err := domain.ParseCaptionLanguage(languageValue)
	if err != nil {
		return application.NarrativeSubtreeResult{}, application.ErrEditInvalid
	}
	nodes := make([]domain.NarrativeNodeState, 0, query.Limit+1)
	state := application.EditNormalizationState{
		ProjectID: query.ProjectID, Sections: make(map[string]domain.NarrativeSectionState),
		AuthoredTexts:  make(map[string]domain.AuthoredTextState),
		SourceExcerpts: make(map[string]domain.SourceExcerptState),
		VisualIntents:  make(map[string]domain.VisualIntentState), Notes: make(map[string]domain.NoteState),
		SectionChildCounts: make(map[string]int),
	}
	for rows.Next() {
		var idValue, kind string
		var orderIndex int64
		if err := rows.Scan(&idValue, &kind, &orderIndex); err != nil {
			return application.NarrativeSubtreeResult{}, err
		}
		id, parseErr := domain.ParseNarrativeNodeID(idValue)
		if parseErr != nil {
			return application.NarrativeSubtreeResult{}, application.ErrEditInvalid
		}
		if err := ensureNarrativeNodeState(ctx, tx, &state, id); err != nil {
			return application.NarrativeSubtreeResult{}, err
		}
		switch domain.NarrativeNodeKind(kind) {
		case domain.NarrativeNodeSection:
			value := state.Sections[idValue]
			nodes = append(nodes, domain.NarrativeNodeState{Kind: domain.NarrativeNodeSection, Section: &value})
		case domain.NarrativeNodeAuthoredText:
			value := state.AuthoredTexts[idValue]
			nodes = append(nodes, domain.NarrativeNodeState{Kind: domain.NarrativeNodeAuthoredText, AuthoredText: &value})
		case domain.NarrativeNodeSourceExcerpt:
			value := state.SourceExcerpts[idValue]
			status, err := sourceExcerptEvidenceStatus(ctx, tx, query.ProjectID, value)
			if err != nil {
				return application.NarrativeSubtreeResult{}, err
			}
			nodes = append(nodes, domain.NarrativeNodeState{
				Kind: domain.NarrativeNodeSourceExcerpt, SourceExcerpt: &value, EvidenceStatus: status,
			})
		case domain.NarrativeNodeVisualIntent:
			value := state.VisualIntents[idValue]
			nodes = append(nodes, domain.NarrativeNodeState{Kind: domain.NarrativeNodeVisualIntent, VisualIntent: &value})
		case domain.NarrativeNodeNote:
			value := state.Notes[idValue]
			nodes = append(nodes, domain.NarrativeNodeState{Kind: domain.NarrativeNodeNote, Note: &value})
		default:
			return application.NarrativeSubtreeResult{}, application.ErrEditInvalid
		}
	}
	if err := rows.Err(); err != nil {
		return application.NarrativeSubtreeResult{}, err
	}
	hasMore := len(nodes) > query.Limit
	if hasMore {
		nodes = nodes[:query.Limit]
	}
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.NarrativeSubtreeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.NarrativeSubtreeResult{}, err
	}
	return application.NarrativeSubtreeResult{
		DocumentID: documentID, DocumentRevision: documentRev,
		Parent: application.NarrativeSectionSummary{ID: parentID, Revision: parentRev, Title: title, Language: language},
		Nodes:  nodes, HasMore: hasMore, ActivityCursor: cursor,
	}, nil
}

func (repository *SQLiteProjects) ReadSequenceWindow(
	ctx context.Context,
	query application.SequenceWindowQuery,
) (application.SequenceWindowResult, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceWindowResult{}, err
	}
	defer tx.Rollback()
	var revisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM sequences WHERE id = ? AND project_id = ?`,
		query.SequenceID.String(), query.ProjectID.String()).Scan(&revisionValue); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.SequenceWindowResult{}, application.ErrEditEntityNotFound
		}
		return application.SequenceWindowResult{}, err
	}
	trackValue := ""
	if query.TrackID != nil {
		trackValue = query.TrackID.String()
	}
	rows, err := tx.QueryContext(ctx, `
SELECT item_kind, id, start_order_key
FROM (
  SELECT 'caption' AS item_kind, id, start_order_key
  FROM captions
  WHERE project_id = ? AND sequence_id = ? AND tombstoned = 0
    AND (? = '' OR track_id = ?)
    AND start_order_key < ? AND end_order_key > ?
  UNION ALL
  SELECT 'clip' AS item_kind, id, timeline_start_order_key AS start_order_key
  FROM clips
  WHERE project_id = ? AND sequence_id = ? AND tombstoned = 0
    AND (? = '' OR track_id = ?)
    AND timeline_start_order_key < ? AND timeline_end_order_key > ?
) items
WHERE (? = '' OR start_order_key > ? OR
  (start_order_key = ? AND (item_kind > ? OR (item_kind = ? AND id > ?))))
ORDER BY start_order_key, item_kind, id LIMIT ?`,
		query.ProjectID.String(), query.SequenceID.String(), trackValue, trackValue,
		query.EndKey, query.StartKey,
		query.ProjectID.String(), query.SequenceID.String(), trackValue, trackValue,
		query.EndKey, query.StartKey,
		query.AfterKey, query.AfterKey, query.AfterKey, query.AfterKind, query.AfterKind, query.AfterID,
		query.Limit+1)
	if err != nil {
		return application.SequenceWindowResult{}, err
	}
	defer rows.Close()
	type windowItem struct{ kind, id, key string }
	items := make([]windowItem, 0, query.Limit+1)
	for rows.Next() {
		var item windowItem
		if err := rows.Scan(&item.kind, &item.id, &item.key); err != nil {
			return application.SequenceWindowResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return application.SequenceWindowResult{}, err
	}
	hasMore := len(items) > query.Limit
	if hasMore {
		items = items[:query.Limit]
	}
	state := application.EditNormalizationState{
		ProjectID: query.ProjectID, Captions: make(map[string]domain.CaptionState),
		Clips: make(map[string]domain.ClipState), LinkGroups: make(map[string]domain.LinkGroupState),
	}
	captions := make([]domain.CaptionState, 0, len(items))
	clips := make([]domain.ClipState, 0, len(items))
	linkGroups := make([]domain.LinkGroupState, 0)
	seenGroups := make(map[string]struct{})
	for _, item := range items {
		switch item.kind {
		case "caption":
			id, parseErr := domain.ParseCaptionID(item.id)
			if parseErr != nil {
				return application.SequenceWindowResult{}, application.ErrEditInvalid
			}
			if err := ensureCaptionState(ctx, tx, &state, id); err != nil {
				return application.SequenceWindowResult{}, err
			}
			caption := state.Captions[item.id]
			caption.ProvenanceStatus, err = captionProvenanceStatus(ctx, tx, query.ProjectID, caption)
			if err != nil {
				return application.SequenceWindowResult{}, err
			}
			captions = append(captions, caption)
		case "clip":
			id, parseErr := domain.ParseClipID(item.id)
			if parseErr != nil {
				return application.SequenceWindowResult{}, application.ErrEditInvalid
			}
			if err := ensureClipState(ctx, tx, &state, id); err != nil {
				return application.SequenceWindowResult{}, err
			}
			clip := state.Clips[item.id]
			clips = append(clips, clip)
			if clip.LinkGroupID != nil {
				if _, seen := seenGroups[clip.LinkGroupID.String()]; !seen {
					if err := ensureLinkGroupState(ctx, tx, &state, *clip.LinkGroupID); err != nil {
						return application.SequenceWindowResult{}, err
					}
					seenGroups[clip.LinkGroupID.String()] = struct{}{}
					linkGroups = append(linkGroups, state.LinkGroups[clip.LinkGroupID.String()])
				}
			}
		default:
			return application.SequenceWindowResult{}, application.ErrEditInvalid
		}
	}
	alignments, err := loadWindowAlignments(ctx, tx, query.ProjectID, captions, clips)
	if err != nil {
		return application.SequenceWindowResult{}, err
	}
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.SequenceWindowResult{}, err
	}
	sequenceRevision, _ := domain.NewRevision(revisionValue)
	nextKey, nextKind, nextID := "", "", ""
	if len(items) > 0 {
		nextKey, nextKind, nextID = items[len(items)-1].key, items[len(items)-1].kind, items[len(items)-1].id
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceWindowResult{}, err
	}
	return application.SequenceWindowResult{
		SequenceID: query.SequenceID, SequenceRevision: sequenceRevision,
		Captions: captions, Clips: clips, LinkGroups: linkGroups,
		Alignments: alignments, HasMore: hasMore, ActivityCursor: cursor,
		NextKey: nextKey, NextKind: nextKind, NextID: nextID,
	}, nil
}
