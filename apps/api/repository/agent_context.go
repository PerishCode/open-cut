package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func validateAgentContextAttachments(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID *domain.SequenceID,
	attachments []application.AgentContextAttachment,
) error {
	if application.ValidateAgentContextAttachments(attachments) != nil {
		return application.ErrAgentBridgeInvalid
	}
	for _, attachment := range attachments {
		if err := validateAgentContextAttachment(ctx, tx, projectID, sequenceID, attachment); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentContextAttachment(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID *domain.SequenceID,
	attachment application.AgentContextAttachment,
) error {
	switch attachment.Kind {
	case application.AgentContextAsset:
		return validateAgentContextEntityRevision(ctx, tx,
			`SELECT revision FROM assets WHERE id = ? AND project_id = ?`,
			attachment.Entity.Revision, attachment.Entity.ID, projectID.String())
	case application.AgentContextNarrativeNode:
		return validateAgentContextEntityRevision(ctx, tx,
			`SELECT revision FROM narrative_nodes WHERE id = ? AND project_id = ?`,
			attachment.Entity.Revision, attachment.Entity.ID, projectID.String())
	case application.AgentContextClip, application.AgentContextCaption, application.AgentContextTrack:
		if sequenceID == nil {
			return application.ErrAgentBridgeInvalid
		}
		table := "clips"
		if attachment.Kind == application.AgentContextCaption {
			table = "captions"
		} else if attachment.Kind == application.AgentContextTrack {
			table = "tracks"
		}
		return validateAgentContextEntityRevision(ctx, tx,
			`SELECT entity.revision FROM `+table+` AS entity
JOIN sequences AS sequence ON sequence.id = entity.sequence_id
WHERE entity.id = ? AND entity.sequence_id = ? AND sequence.project_id = ?`,
			attachment.Entity.Revision, attachment.Entity.ID, sequenceID.String(), projectID.String())
	case application.AgentContextTranscriptSegment:
		var exists int
		err := tx.QueryRowContext(ctx, `
SELECT 1
FROM transcript_segments AS segment
JOIN transcript_artifacts AS transcript ON transcript.artifact_id = segment.artifact_id
JOIN media_artifacts AS artifact ON artifact.id = transcript.artifact_id
WHERE segment.id = ? AND segment.artifact_id = ? AND artifact.project_id = ?`,
			attachment.Transcript.SegmentID.String(), attachment.Transcript.ArtifactID.String(), projectID.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrAgentContextStale
		}
		return err
	case application.AgentContextSequencePoint:
		if sequenceID == nil || *sequenceID != attachment.Point.SequenceID {
			return application.ErrAgentBridgeInvalid
		}
		return validateAgentContextEntityRevision(ctx, tx,
			`SELECT revision FROM sequences WHERE id = ? AND project_id = ?`,
			attachment.Point.Revision, attachment.Point.SequenceID.String(), projectID.String())
	case application.AgentContextSequenceRange:
		if sequenceID == nil || *sequenceID != attachment.Range.SequenceID {
			return application.ErrAgentBridgeInvalid
		}
		return validateAgentContextEntityRevision(ctx, tx,
			`SELECT revision FROM sequences WHERE id = ? AND project_id = ?`,
			attachment.Range.Revision, attachment.Range.SequenceID.String(), projectID.String())
	default:
		return application.ErrAgentBridgeInvalid
	}
}

func validateAgentContextEntityRevision(
	ctx context.Context,
	tx *sql.Tx,
	query string,
	expected domain.Revision,
	args ...any,
) error {
	var current uint64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrAgentContextStale
		}
		return err
	}
	if current != expected.Value() {
		return application.ErrAgentContextStale
	}
	return nil
}

func insertAgentContextAttachments(
	ctx context.Context,
	tx *sql.Tx,
	turnID domain.TurnID,
	attachments []application.AgentContextAttachment,
) error {
	for ordinal, attachment := range attachments {
		encoded, err := json.Marshal(attachment)
		if err != nil {
			return application.ErrAgentBridgeInvalid
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_turn_context_attachments (turn_id, ordinal, kind, attachment_json)
VALUES (?, ?, ?, ?)`, turnID.String(), ordinal, string(attachment.Kind), string(encoded)); err != nil {
			return err
		}
	}
	return nil
}

func loadAgentContextAttachments(
	ctx context.Context,
	tx *sql.Tx,
	turnID domain.TurnID,
) ([]application.AgentContextAttachment, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT kind, attachment_json
FROM agent_turn_context_attachments WHERE turn_id = ? ORDER BY ordinal`, turnID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attachments := make([]application.AgentContextAttachment, 0)
	for rows.Next() {
		var kind, encoded string
		if err := rows.Scan(&kind, &encoded); err != nil {
			return nil, err
		}
		var attachment application.AgentContextAttachment
		if json.Unmarshal([]byte(encoded), &attachment) != nil || string(attachment.Kind) != kind {
			return nil, application.ErrAgentBridgeInvalid
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if application.ValidateAgentContextAttachments(attachments) != nil {
		return nil, application.ErrAgentBridgeInvalid
	}
	return attachments, nil
}
