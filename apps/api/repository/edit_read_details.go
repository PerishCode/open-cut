package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ReadEditEntity(
	ctx context.Context,
	projectID domain.ProjectID,
	kind domain.EditEntityKind,
	id string,
) (application.EditEntityDetail, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.EditEntityDetail{}, err
	}
	defer tx.Rollback()
	state := application.EditNormalizationState{
		ProjectID: projectID, Sections: make(map[string]domain.NarrativeSectionState),
		AuthoredTexts:         make(map[string]domain.AuthoredTextState),
		SourceExcerpts:        make(map[string]domain.SourceExcerptState),
		VisualIntents:         make(map[string]domain.VisualIntentState),
		Notes:                 make(map[string]domain.NoteState),
		SectionChildCounts:    make(map[string]int),
		TranscriptCorrections: make(map[string]domain.TranscriptCorrectionState),
		Captions:              make(map[string]domain.CaptionState), Alignments: make(map[string]domain.AlignmentState),
		Clips: make(map[string]domain.ClipState), LinkGroups: make(map[string]domain.LinkGroupState),
	}
	detail := application.EditEntityDetail{Kind: kind}
	switch kind {
	case domain.EntityNarrativeNode:
		parsed, parseErr := domain.ParseNarrativeNodeID(id)
		if parseErr != nil {
			return detail, application.ErrEditInvalid
		}
		if err := ensureNarrativeNodeState(ctx, tx, &state, parsed); err != nil {
			return detail, application.ErrEditEntityNotFound
		}
		if value, exists := state.AuthoredTexts[id]; exists {
			detail.AuthoredText = &value
		} else if value, exists := state.Sections[id]; exists {
			detail.Section = &value
		} else if value, exists := state.SourceExcerpts[id]; exists {
			detail.SourceExcerpt = &value
			detail.SourceExcerptEvidenceStatus, err = sourceExcerptEvidenceStatus(ctx, tx, projectID, value)
			if err != nil {
				return application.EditEntityDetail{}, err
			}
		} else if value, exists := state.VisualIntents[id]; exists {
			detail.VisualIntent = &value
		} else if value, exists := state.Notes[id]; exists {
			detail.Note = &value
		} else {
			return detail, application.ErrEditEntityNotFound
		}
	case domain.EntityTranscriptCorrection:
		parsed, parseErr := domain.ParseTranscriptCorrectionID(id)
		if parseErr != nil {
			return detail, application.ErrEditInvalid
		}
		if err := ensureTranscriptCorrectionState(ctx, tx, &state, parsed); err != nil {
			return detail, application.ErrEditEntityNotFound
		}
		value := state.TranscriptCorrections[id]
		detail.TranscriptCorrection = &value
	case domain.EntityCaption:
		parsed, parseErr := domain.ParseCaptionID(id)
		if parseErr != nil {
			return detail, application.ErrEditInvalid
		}
		if err := ensureCaptionState(ctx, tx, &state, parsed); err != nil {
			return detail, application.ErrEditEntityNotFound
		}
		value := state.Captions[id]
		value.ProvenanceStatus, err = captionProvenanceStatus(ctx, tx, projectID, value)
		if err != nil {
			return application.EditEntityDetail{}, err
		}
		detail.Caption = &value
	case domain.EntityAlignment:
		parsed, parseErr := domain.ParseAlignmentID(id)
		if parseErr != nil {
			return detail, application.ErrEditInvalid
		}
		if err := ensureAlignmentState(ctx, tx, &state, parsed); err != nil {
			return detail, application.ErrEditEntityNotFound
		}
		value := state.Alignments[id]
		detail.Alignment = &value
	case domain.EntityClip:
		parsed, parseErr := domain.ParseClipID(id)
		if parseErr != nil {
			return detail, application.ErrEditInvalid
		}
		if err := ensureClipState(ctx, tx, &state, parsed); err != nil {
			return detail, application.ErrEditEntityNotFound
		}
		value := state.Clips[id]
		detail.Clip = &value
	case domain.EntityLinkGroup:
		parsed, parseErr := domain.ParseLinkGroupID(id)
		if parseErr != nil {
			return detail, application.ErrEditInvalid
		}
		if err := ensureLinkGroupState(ctx, tx, &state, parsed); err != nil {
			return detail, application.ErrEditEntityNotFound
		}
		value := state.LinkGroups[id]
		detail.LinkGroup = &value
	default:
		return detail, application.ErrEditInvalid
	}
	detail.ActivityCursor, err = loadActivityHead(ctx, tx, "project", projectID.String())
	if err != nil {
		return application.EditEntityDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.EditEntityDetail{}, err
	}
	return detail, nil
}

func (repository *SQLiteProjects) ReadEditProposal(
	ctx context.Context,
	projectID domain.ProjectID,
	proposalID domain.ProposalID,
) (domain.EditProposal, domain.Cursor, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.EditProposal{}, 0, err
	}
	defer tx.Rollback()
	proposal, err := loadEditProposal(ctx, tx, projectID, proposalID)
	if err != nil {
		return domain.EditProposal{}, 0, err
	}
	cursor, err := loadActivityHead(ctx, tx, "project", projectID.String())
	if err != nil {
		return domain.EditProposal{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return domain.EditProposal{}, 0, err
	}
	return proposal, cursor, nil
}

func (repository *SQLiteProjects) ReadTransactionHistory(
	ctx context.Context,
	query application.TransactionHistoryQuery,
) (application.TransactionHistoryResult, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.TransactionHistoryResult{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, query.ProjectID.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.TransactionHistoryResult{}, application.ErrProjectNotFound
		}
		return application.TransactionHistoryResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM edit_transactions
WHERE project_id = ? AND digest IS NOT NULL AND project_revision > ?
ORDER BY project_revision, id LIMIT ?`,
		query.ProjectID.String(), query.AfterRevision.Value(), query.Limit+1)
	if err != nil {
		return application.TransactionHistoryResult{}, err
	}
	ids := make([]domain.TransactionID, 0, query.Limit+1)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return application.TransactionHistoryResult{}, err
		}
		id, err := domain.ParseTransactionID(value)
		if err != nil {
			rows.Close()
			return application.TransactionHistoryResult{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return application.TransactionHistoryResult{}, err
	}
	if err := rows.Err(); err != nil {
		return application.TransactionHistoryResult{}, err
	}
	hasMore := len(ids) > query.Limit
	if hasMore {
		ids = ids[:query.Limit]
	}
	transactions := make([]domain.EditTransaction, 0, len(ids))
	for _, id := range ids {
		transaction, err := loadEditTransaction(ctx, tx, query.ProjectID, id)
		if err != nil {
			return application.TransactionHistoryResult{}, err
		}
		transactions = append(transactions, transaction)
	}
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.TransactionHistoryResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.TransactionHistoryResult{}, err
	}
	return application.TransactionHistoryResult{
		Transactions: transactions, HasMore: hasMore, ActivityCursor: cursor,
	}, nil
}

func loadWindowAlignments(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	captions []domain.CaptionState,
	clips []domain.ClipState,
) ([]domain.AlignmentState, error) {
	state := application.EditNormalizationState{
		ProjectID: projectID, Alignments: make(map[string]domain.AlignmentState),
	}
	result := make([]domain.AlignmentState, 0)
	seen := make(map[string]struct{})
	load := func(column, entityID string) error {
		rows, err := tx.QueryContext(ctx, `
SELECT a.id FROM alignments a
JOIN alignment_targets t ON t.alignment_id = a.id
WHERE a.project_id = ? AND `+column+` = ? ORDER BY a.id LIMIT 2049`,
			projectID.String(), entityID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if len(result) >= 2048 {
				return application.ErrEditInvalid
			}
			var value string
			if err := rows.Scan(&value); err != nil {
				return err
			}
			if _, exists := seen[value]; exists {
				continue
			}
			id, err := domain.ParseAlignmentID(value)
			if err != nil {
				return err
			}
			if err := ensureAlignmentState(ctx, tx, &state, id); err != nil {
				return err
			}
			seen[value] = struct{}{}
			result = append(result, state.Alignments[value])
		}
		return rows.Err()
	}
	for _, caption := range captions {
		if err := load("t.caption_id", caption.ID.String()); err != nil {
			return nil, err
		}
	}
	for _, clip := range clips {
		if err := load("t.clip_id", clip.ID.String()); err != nil {
			return nil, err
		}
	}
	return result, nil
}
