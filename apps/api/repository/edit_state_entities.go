package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func ensureSectionState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.NarrativeNodeID,
) error {
	if _, exists := state.Sections[id.String()]; exists {
		return nil
	}
	var nodeValue, documentValue, title, languageValue string
	var parentValue sql.NullString
	var revisionValue uint64
	var orderIndex int64
	var tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT node.id, node.document_id, node.parent_id, node.revision, node.order_index,
       node.tombstoned, value.title, value.language
FROM narrative_nodes node
JOIN narrative_section_values value ON value.id = node.id
WHERE node.id = ? AND node.project_id = ? AND node.kind = 'section'`,
		id.String(), state.ProjectID.String()).Scan(
		&nodeValue, &documentValue, &parentValue, &revisionValue, &orderIndex,
		&tombstoned, &title, &languageValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditInvalid
	}
	if err != nil {
		return err
	}
	nodeID, _ := domain.ParseNarrativeNodeID(nodeValue)
	documentID, _ := domain.ParseNarrativeDocumentID(documentValue)
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return err
	}
	language, err := domain.ParseCaptionLanguage(languageValue)
	if err != nil {
		return application.ErrEditInvalid
	}
	var parentID *domain.NarrativeNodeID
	if parentValue.Valid {
		parsed, parseErr := domain.ParseNarrativeNodeID(parentValue.String)
		if parseErr != nil {
			return application.ErrEditInvalid
		}
		parentID = &parsed
	}
	after, err := loadNarrativeAfter(ctx, tx, documentID, parentID, orderIndex, nodeID)
	if err != nil {
		return err
	}
	state.Sections[nodeValue] = domain.NarrativeSectionState{
		ID: nodeID, DocumentID: documentID, ParentID: parentID, AfterNodeID: after,
		Revision: revision, Title: title, Language: language, Tombstoned: tombstoned,
	}
	var childCount int
	if err := tx.QueryRowContext(ctx, `
SELECT count(*) FROM narrative_nodes
WHERE document_id = ? AND parent_id = ? AND tombstoned = 0`,
		documentID.String(), nodeID.String()).Scan(&childCount); err != nil {
		return err
	}
	state.SectionChildCounts[nodeValue] = childCount
	if parentID != nil {
		return ensureSectionState(ctx, tx, state, *parentID)
	}
	return nil
}

func ensureTrackState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.TrackID,
) error {
	if _, exists := state.Tracks[id.String()]; exists {
		return nil
	}
	var trackValue, sequenceValue, kind string
	var revisionValue uint64
	err := tx.QueryRowContext(ctx, `
SELECT id, sequence_id, revision, type FROM tracks
WHERE id = ? AND project_id = ?`, id.String(), state.ProjectID.String()).Scan(
		&trackValue, &sequenceValue, &revisionValue, &kind,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditInvalid
	}
	if err != nil {
		return err
	}
	trackID, _ := domain.ParseTrackID(trackValue)
	sequenceID, _ := domain.ParseSequenceID(sequenceValue)
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return err
	}
	state.Tracks[trackValue] = application.EditTrackState{
		ID: trackID, SequenceID: sequenceID, Revision: revision, Type: domain.TrackType(kind),
	}
	return nil
}

func ensureAuthoredTextState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.NarrativeNodeID,
) error {
	if _, exists := state.AuthoredTexts[id.String()]; exists {
		return nil
	}
	var nodeValue, documentValue, parentValue, purposeValue, languageValue, text string
	var revisionValue uint64
	var orderIndex int64
	var tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT node.id, node.document_id, node.parent_id, node.revision,
       value.purpose, value.language, value.text, node.order_index, node.tombstoned
FROM narrative_nodes node
JOIN narrative_authored_text_values value ON value.id = node.id
WHERE node.id = ? AND node.project_id = ? AND node.kind = 'authored-text'`,
		id.String(), state.ProjectID.String()).Scan(
		&nodeValue, &documentValue, &parentValue, &revisionValue, &purposeValue,
		&languageValue, &text, &orderIndex, &tombstoned,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditInvalid
	}
	if err != nil {
		return err
	}
	nodeID, _ := domain.ParseNarrativeNodeID(nodeValue)
	documentID, _ := domain.ParseNarrativeDocumentID(documentValue)
	parentID, _ := domain.ParseNarrativeNodeID(parentValue)
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return err
	}
	language, languageErr := domain.ParseCaptionLanguage(languageValue)
	purpose := domain.AuthoredTextPurpose(purposeValue)
	if languageErr != nil || purpose.Validate() != nil || text == "" {
		return application.ErrEditInvalid
	}
	after, err := loadNarrativeAfter(ctx, tx, documentID, &parentID, orderIndex, nodeID)
	if err != nil {
		return err
	}
	state.AuthoredTexts[nodeValue] = domain.AuthoredTextState{
		ID: nodeID, Revision: revision, DocumentID: documentID, ParentID: parentID,
		AfterNodeID: after, Purpose: purpose, Language: language, Text: text, Tombstoned: tombstoned,
	}
	return nil
}

func ensureCaptionState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.CaptionID,
) error {
	if _, exists := state.Captions[id.String()]; exists {
		return nil
	}
	caption, err := loadCaptionState(ctx, tx, state.ProjectID, id)
	if err != nil {
		return err
	}
	state.Captions[id.String()] = caption
	return nil
}

func loadCaptionState(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	id domain.CaptionID,
) (domain.CaptionState, error) {
	var captionValue, sequenceValue, trackValue, languageValue, text string
	var revisionValue uint64
	var startValue, durationValue int64
	var startScale, durationScale int32
	var tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT id, sequence_id, track_id, revision, start_value, start_scale,
       duration_value, duration_scale, language, text, tombstoned
FROM captions WHERE id = ? AND project_id = ?`, id.String(), projectID.String()).Scan(
		&captionValue, &sequenceValue, &trackValue, &revisionValue,
		&startValue, &startScale, &durationValue, &durationScale, &languageValue, &text, &tombstoned,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CaptionState{}, application.ErrEditInvalid
	}
	if err != nil {
		return domain.CaptionState{}, err
	}
	captionID, _ := domain.ParseCaptionID(captionValue)
	sequenceID, _ := domain.ParseSequenceID(sequenceValue)
	trackID, _ := domain.ParseTrackID(trackValue)
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return domain.CaptionState{}, err
	}
	start, err := domain.NewRationalTime(startValue, startScale)
	if err != nil {
		return domain.CaptionState{}, err
	}
	duration, err := domain.NewRationalTime(durationValue, durationScale)
	if err != nil {
		return domain.CaptionState{}, err
	}
	rangeValue, err := domain.NewTimeRange(start, duration)
	if err != nil {
		return domain.CaptionState{}, err
	}
	languageValueParsed, err := domain.ParseCaptionLanguage(languageValue)
	if err != nil {
		return domain.CaptionState{}, application.ErrEditInvalid
	}
	result := domain.CaptionState{
		ID: captionID, Revision: revision, SequenceID: sequenceID, TrackID: trackID,
		Range: rangeValue, Language: languageValueParsed, Text: text, Tombstoned: tombstoned,
	}
	result.Provenance, err = loadCaptionProvenance(ctx, tx, captionID)
	if err != nil {
		return domain.CaptionState{}, err
	}
	return result, nil
}

func ensureAlignmentState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.AlignmentID,
) error {
	if _, exists := state.Alignments[id.String()]; exists {
		return nil
	}
	var alignmentValue, nodeValue, sequenceValue, status string
	var revisionValue, nodeRevision uint64
	err := tx.QueryRowContext(ctx, `
SELECT id, revision, narrative_node_id, narrative_node_revision, sequence_id, status
FROM alignments WHERE id = ? AND project_id = ?`,
		id.String(), state.ProjectID.String()).Scan(
		&alignmentValue, &revisionValue, &nodeValue, &nodeRevision, &sequenceValue, &status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditInvalid
	}
	if err != nil {
		return err
	}
	alignmentID, _ := domain.ParseAlignmentID(alignmentValue)
	nodeID, _ := domain.ParseNarrativeNodeID(nodeValue)
	sequenceID, _ := domain.ParseSequenceID(sequenceValue)
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return err
	}
	nodeRev, err := domain.NewRevision(nodeRevision)
	if err != nil {
		return err
	}
	targets, err := loadAlignmentTargets(ctx, tx, alignmentID)
	if err != nil {
		return err
	}
	state.Alignments[alignmentValue] = domain.AlignmentState{
		ID: alignmentID, Revision: revision, NarrativeNodeID: nodeID, NarrativeNodeRevision: nodeRev,
		SequenceID: sequenceID, Targets: targets, Status: domain.AlignmentStatus(status),
	}
	return nil
}

func loadAlignmentTargets(ctx context.Context, tx *sql.Tx, alignmentID domain.AlignmentID) ([]domain.AlignmentTarget, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT kind, caption_id, clip_id, entity_revision,
       local_start_value, local_start_scale, local_duration_value, local_duration_scale,
       timeline_start_value, timeline_start_scale, timeline_duration_value,
       timeline_duration_scale, sequence_revision
FROM alignment_targets WHERE alignment_id = ? ORDER BY ordinal`, alignmentID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make([]domain.AlignmentTarget, 0, 2)
	for rows.Next() {
		if len(targets) >= 64 {
			return nil, application.ErrEditInvalid
		}
		var kind string
		var captionValue, clipValue sql.NullString
		var entityRevision, localStart, localStartScale, localDuration, localDurationScale sql.NullInt64
		var timelineStart, timelineStartScale, timelineDuration, timelineDurationScale, sequenceRevision sql.NullInt64
		if err := rows.Scan(
			&kind, &captionValue, &clipValue, &entityRevision,
			&localStart, &localStartScale, &localDuration, &localDurationScale,
			&timelineStart, &timelineStartScale, &timelineDuration, &timelineDurationScale, &sequenceRevision,
		); err != nil {
			return nil, err
		}
		target, err := parseAlignmentTarget(
			kind, captionValue, clipValue, entityRevision,
			localStart, localStartScale, localDuration, localDurationScale,
			timelineStart, timelineStartScale, timelineDuration, timelineDurationScale, sequenceRevision,
		)
		if err != nil {
			return nil, err
		}
		if len(targets) > 0 && targets[0].Type != target.Type {
			return nil, application.ErrEditInvalid
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, application.ErrEditInvalid
	}
	return targets, nil
}

func parseAlignmentTarget(
	kind string,
	captionValue, clipValue sql.NullString,
	entityRevision, localStart, localStartScale, localDuration, localDurationScale sql.NullInt64,
	timelineStart, timelineStartScale, timelineDuration, timelineDurationScale, sequenceRevision sql.NullInt64,
) (domain.AlignmentTarget, error) {
	if kind == string(domain.AlignmentTargetTimeline) {
		rangeValue, err := storedNullableTimeRange(timelineStart, timelineStartScale, timelineDuration, timelineDurationScale)
		if err != nil || !sequenceRevision.Valid {
			return domain.AlignmentTarget{}, application.ErrEditInvalid
		}
		revision, err := domain.NewRevision(uint64(sequenceRevision.Int64))
		if err != nil {
			return domain.AlignmentTarget{}, err
		}
		return domain.AlignmentTarget{Type: domain.AlignmentTargetTimeline, Timeline: &domain.TimelineAlignmentTarget{
			SequenceRevision: revision, Range: rangeValue,
		}}, nil
	}
	localRange, err := storedNullableTimeRange(localStart, localStartScale, localDuration, localDurationScale)
	if err != nil || !entityRevision.Valid {
		return domain.AlignmentTarget{}, application.ErrEditInvalid
	}
	revision, err := domain.NewRevision(uint64(entityRevision.Int64))
	if err != nil {
		return domain.AlignmentTarget{}, err
	}
	switch domain.AlignmentTargetType(kind) {
	case domain.AlignmentTargetCaption:
		id, err := domain.ParseCaptionID(captionValue.String)
		if err != nil {
			return domain.AlignmentTarget{}, err
		}
		return domain.AlignmentTarget{Type: domain.AlignmentTargetCaption, Caption: &domain.CaptionAlignmentTarget{
			CaptionID: id, CaptionRevision: revision, LocalRange: localRange,
		}}, nil
	case domain.AlignmentTargetClip:
		id, err := domain.ParseClipID(clipValue.String)
		if err != nil {
			return domain.AlignmentTarget{}, err
		}
		return domain.AlignmentTarget{Type: domain.AlignmentTargetClip, Clip: &domain.ClipAlignmentTarget{
			ClipID: id, ClipRevision: revision, LocalRange: localRange,
		}}, nil
	default:
		return domain.AlignmentTarget{}, application.ErrEditInvalid
	}
}

func storedNullableTimeRange(
	start, startScale, duration, durationScale sql.NullInt64,
) (domain.TimeRange, error) {
	if !start.Valid || !startScale.Valid || !duration.Valid || !durationScale.Valid {
		return domain.TimeRange{}, application.ErrEditInvalid
	}
	startValue, err := domain.NewRationalTime(start.Int64, int32(startScale.Int64))
	if err != nil {
		return domain.TimeRange{}, err
	}
	durationValue, err := domain.NewRationalTime(duration.Int64, int32(durationScale.Int64))
	if err != nil {
		return domain.TimeRange{}, err
	}
	return domain.NewTimeRange(startValue, durationValue)
}

func loadCaptionOverlaps(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	sequenceID domain.SequenceID,
	trackID domain.TrackID,
	rangeValue domain.TimeRange,
	exclude *domain.CaptionID,
) error {
	startKey, err := domain.RationalOrderKey(rangeValue.Start)
	if err != nil {
		return application.ErrEditInvalid
	}
	end, err := rangeValue.End()
	if err != nil {
		return application.ErrEditInvalid
	}
	endKey, err := domain.RationalOrderKey(end)
	if err != nil {
		return application.ErrEditInvalid
	}
	excluded := ""
	if exclude != nil {
		excluded = exclude.String()
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM captions
WHERE sequence_id = ? AND track_id = ? AND tombstoned = 0 AND id <> ?
  AND start_order_key < ? AND end_order_key > ?
ORDER BY start_order_key, id LIMIT 513`, sequenceID.String(), trackID.String(), excluded, endKey, startKey)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 512 {
			return application.ErrEditInvalid
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			return err
		}
		id, err := domain.ParseCaptionID(value)
		if err != nil {
			return err
		}
		if err := ensureCaptionState(ctx, tx, state, id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func loadEditEntityRevision(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	kind domain.EditEntityKind,
	id string,
) (domain.Revision, error) {
	var query string
	switch kind {
	case domain.EntityNarrativeDocument:
		query = `SELECT revision FROM narrative_documents WHERE id = ? AND project_id = ?`
	case domain.EntitySequence:
		query = `SELECT revision FROM sequences WHERE id = ? AND project_id = ?`
	case domain.EntityTrack:
		query = `SELECT revision FROM tracks WHERE id = ? AND project_id = ?`
	case domain.EntityCaption:
		query = `SELECT revision FROM captions WHERE id = ? AND project_id = ?`
	case domain.EntityAlignment:
		query = `SELECT revision FROM alignments WHERE id = ? AND project_id = ?`
	case domain.EntityAsset:
		query = `SELECT revision FROM assets WHERE id = ? AND project_id = ?`
	case domain.EntityClip:
		query = `SELECT revision FROM clips WHERE id = ? AND project_id = ?`
	case domain.EntityLinkGroup:
		query = `SELECT revision FROM clip_link_groups WHERE id = ? AND project_id = ?`
	case domain.EntityTranscriptCorrection:
		query = `SELECT revision FROM transcript_corrections WHERE id = ? AND project_id = ?`
	case domain.EntityNarrativeNode:
		var value uint64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM narrative_nodes WHERE id = ? AND project_id = ?`,
			id, projectID.String()).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, application.ErrEditInvalid
		}
		if err != nil {
			return 0, err
		}
		return domain.NewRevision(value)
	default:
		return 0, application.ErrEditInvalid
	}
	var value uint64
	if err := tx.QueryRowContext(ctx, query, id, projectID.String()).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, application.ErrEditInvalid
		}
		return 0, err
	}
	return domain.NewRevision(value)
}

func loadExactNodeAlignments(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	nodeID string,
) error {
	return loadExactAlignments(ctx, tx, state, "node", nodeID, state.NodeAlignments)
}

func loadExactCaptionAlignments(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	captionID string,
) error {
	return loadExactAlignments(ctx, tx, state, "caption", captionID, state.CaptionAlignments)
}

func loadExactClipAlignments(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	clipID string,
) error {
	return loadExactAlignments(ctx, tx, state, "clip", clipID, state.ClipAlignments)
}

func loadExactAlignments(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	target string,
	entityID string,
	destination map[string][]domain.AlignmentID,
) error {
	var query string
	switch target {
	case "node":
		query = `SELECT id FROM alignments WHERE project_id = ? AND narrative_node_id = ? AND status = 'exact' ORDER BY id LIMIT 2049`
	case "caption":
		query = `SELECT a.id FROM alignments a JOIN alignment_targets t ON t.alignment_id = a.id WHERE a.project_id = ? AND t.caption_id = ? AND a.status = 'exact' ORDER BY a.id LIMIT 2049`
	case "clip":
		query = `SELECT a.id FROM alignments a JOIN alignment_targets t ON t.alignment_id = a.id WHERE a.project_id = ? AND t.clip_id = ? AND a.status = 'exact' ORDER BY a.id LIMIT 2049`
	default:
		return fmt.Errorf("invalid alignment dependency target")
	}
	rows, err := tx.QueryContext(ctx, query, state.ProjectID.String(), entityID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if len(destination[entityID]) >= 2048 {
			return application.ErrEditInvalid
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			return err
		}
		id, err := domain.ParseAlignmentID(value)
		if err != nil {
			return err
		}
		destination[entityID] = append(destination[entityID], id)
		if err := ensureAlignmentState(ctx, tx, state, id); err != nil {
			return err
		}
	}
	return rows.Err()
}
