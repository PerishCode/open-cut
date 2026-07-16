package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func ensureNarrativeNodeState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.NarrativeNodeID,
) error {
	if _, exists := state.AuthoredTexts[id.String()]; exists {
		return nil
	}
	if _, exists := state.SourceExcerpts[id.String()]; exists {
		return nil
	}
	if _, exists := state.Sections[id.String()]; exists {
		return nil
	}
	if _, exists := state.VisualIntents[id.String()]; exists {
		return nil
	}
	if _, exists := state.Notes[id.String()]; exists {
		return nil
	}
	var kind string
	if err := tx.QueryRowContext(ctx, `
SELECT kind FROM narrative_nodes WHERE id = ? AND project_id = ?`,
		id.String(), state.ProjectID.String()).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrEditInvalid
		}
		return err
	}
	switch domain.NarrativeNodeKind(kind) {
	case domain.NarrativeNodeSection:
		return ensureSectionState(ctx, tx, state, id)
	case domain.NarrativeNodeAuthoredText:
		return ensureAuthoredTextState(ctx, tx, state, id)
	case domain.NarrativeNodeSourceExcerpt:
		return ensureSourceExcerptState(ctx, tx, state, id)
	case domain.NarrativeNodeVisualIntent:
		return ensureVisualIntentState(ctx, tx, state, id)
	case domain.NarrativeNodeNote:
		return ensureNoteState(ctx, tx, state, id)
	default:
		return application.ErrEditInvalid
	}
}

func ensureSourceExcerptState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.NarrativeNodeID,
) error {
	if _, exists := state.SourceExcerpts[id.String()]; exists {
		return nil
	}
	var nodeValue, documentValue, parentValue, assetValue, fingerprint string
	var languageValue, effectiveText, artifactValue, streamValue string
	var revisionValue uint64
	var sourceStartValue, sourceDurationValue int64
	var sourceStartScale, sourceDurationScale int32
	var orderIndex int64
	var tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT node.id, node.document_id, node.parent_id, node.revision, node.order_index,
       node.tombstoned, value.asset_id, value.accepted_fingerprint,
       value.source_start_value, value.source_start_scale,
       value.source_duration_value, value.source_duration_scale,
       value.language, value.effective_text, value.transcript_artifact_id,
       value.source_stream_id
FROM narrative_nodes node
JOIN narrative_source_excerpt_values value ON value.id = node.id
WHERE node.id = ? AND node.project_id = ? AND node.kind = 'source-excerpt'`,
		id.String(), state.ProjectID.String()).Scan(
		&nodeValue, &documentValue, &parentValue, &revisionValue, &orderIndex,
		&tombstoned, &assetValue, &fingerprint, &sourceStartValue, &sourceStartScale,
		&sourceDurationValue, &sourceDurationScale, &languageValue, &effectiveText,
		&artifactValue, &streamValue,
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
	assetID, assetErr := domain.ParseAssetID(assetValue)
	revision, revisionErr := domain.NewRevision(revisionValue)
	accepted, fingerprintErr := domain.ParseDigest(fingerprint)
	start, startErr := domain.NewRationalTime(sourceStartValue, sourceStartScale)
	duration, durationErr := domain.NewRationalTime(sourceDurationValue, sourceDurationScale)
	rangeValue, rangeErr := domain.NewTimeRange(start, duration)
	language, languageErr := domain.ParseCaptionLanguage(languageValue)
	artifactID, artifactErr := domain.ParseArtifactID(artifactValue)
	streamID, streamErr := domain.ParseSourceStreamID(streamValue)
	if nodeErr != nil || documentErr != nil || parentErr != nil || assetErr != nil || revisionErr != nil ||
		fingerprintErr != nil || startErr != nil || durationErr != nil || rangeErr != nil || languageErr != nil ||
		artifactErr != nil || streamErr != nil || effectiveText == "" {
		return application.ErrEditInvalid
	}
	after, err := loadNarrativeAfter(ctx, tx, documentID, &parentID, orderIndex, nodeID)
	if err != nil {
		return err
	}
	segmentIDs, err := loadSourceExcerptSegmentIDs(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	correctionRefs, err := loadSourceExcerptCorrectionRefs(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	state.SourceExcerpts[nodeValue] = domain.SourceExcerptState{
		ID: nodeID, Revision: revision, DocumentID: documentID, ParentID: parentID,
		AfterNodeID: after, AssetID: assetID, AcceptedFingerprint: accepted,
		SourceRange: rangeValue, Language: language, EffectiveText: effectiveText,
		Evidence: domain.SourceExcerptTranscriptEvidence{
			ArtifactID: artifactID, SourceStreamID: streamID,
			SegmentIDs: segmentIDs, CorrectionRevisions: correctionRefs,
		},
		Tombstoned: tombstoned,
	}
	return nil
}

func loadNarrativeAfter(
	ctx context.Context,
	tx *sql.Tx,
	documentID domain.NarrativeDocumentID,
	parentID *domain.NarrativeNodeID,
	orderIndex int64,
	id domain.NarrativeNodeID,
) (*domain.NarrativeNodeID, error) {
	if parentID == nil {
		return nil, nil
	}
	var value string
	err := tx.QueryRowContext(ctx, `
SELECT id FROM narrative_nodes
WHERE document_id = ? AND parent_id = ? AND tombstoned = 0 AND
      (order_index < ? OR (order_index = ? AND id < ?))
ORDER BY order_index DESC, id DESC LIMIT 1`,
		documentID.String(), parentID.String(), orderIndex, orderIndex, id.String()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	parsed, err := domain.ParseNarrativeNodeID(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func loadSourceExcerptSegmentIDs(
	ctx context.Context,
	tx *sql.Tx,
	id domain.NarrativeNodeID,
) ([]domain.TranscriptSegmentID, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT segment_id FROM narrative_source_excerpt_segments
WHERE node_id = ? ORDER BY ordinal`, id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.TranscriptSegmentID, 0)
	for rows.Next() {
		if len(result) >= 256 {
			return nil, application.ErrEditInvalid
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		parsed, err := domain.ParseTranscriptSegmentID(value)
		if err != nil {
			return nil, application.ErrEditInvalid
		}
		result = append(result, parsed)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, application.ErrEditInvalid
	}
	return result, nil
}

func loadSourceExcerptCorrectionRefs(
	ctx context.Context,
	tx *sql.Tx,
	id domain.NarrativeNodeID,
) ([]domain.TranscriptCorrectionRevisionRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT correction_id, correction_revision
FROM narrative_source_excerpt_corrections WHERE node_id = ? ORDER BY ordinal`, id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.TranscriptCorrectionRevisionRef, 0)
	for rows.Next() {
		if len(result) >= 256 {
			return nil, application.ErrEditInvalid
		}
		var value string
		var revisionValue uint64
		if err := rows.Scan(&value, &revisionValue); err != nil {
			return nil, err
		}
		parsed, parseErr := domain.ParseTranscriptCorrectionID(value)
		revision, revisionErr := domain.NewRevision(revisionValue)
		if parseErr != nil || revisionErr != nil {
			return nil, application.ErrEditInvalid
		}
		result = append(result, domain.TranscriptCorrectionRevisionRef{ID: parsed, Revision: revision})
	}
	return result, rows.Err()
}

func ensureTranscriptCorrectionState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.TranscriptCorrectionID,
) error {
	if _, exists := state.TranscriptCorrections[id.String()]; exists {
		return nil
	}
	var idValue, assetValue, artifactValue, replacementText, languageValue string
	var revisionValue uint64
	var startValue, durationValue int64
	var startScale, durationScale int32
	var tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT id, asset_id, artifact_id, revision, source_start_value,
       source_start_scale, source_duration_value, source_duration_scale,
       replacement_text, language, tombstoned
FROM transcript_corrections WHERE id = ? AND project_id = ?`,
		id.String(), state.ProjectID.String()).Scan(
		&idValue, &assetValue, &artifactValue, &revisionValue, &startValue,
		&startScale, &durationValue, &durationScale, &replacementText, &languageValue, &tombstoned,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditInvalid
	}
	if err != nil {
		return err
	}
	parsedID, idErr := domain.ParseTranscriptCorrectionID(idValue)
	assetID, assetErr := domain.ParseAssetID(assetValue)
	artifactID, artifactErr := domain.ParseArtifactID(artifactValue)
	revision, revisionErr := domain.NewRevision(revisionValue)
	start, startErr := domain.NewRationalTime(startValue, startScale)
	duration, durationErr := domain.NewRationalTime(durationValue, durationScale)
	rangeValue, rangeErr := domain.NewTimeRange(start, duration)
	language, languageErr := domain.ParseCaptionLanguage(languageValue)
	if idErr != nil || assetErr != nil || artifactErr != nil || revisionErr != nil || startErr != nil ||
		durationErr != nil || rangeErr != nil || languageErr != nil || replacementText == "" {
		return application.ErrEditInvalid
	}
	segmentIDs, err := loadTranscriptCorrectionSegmentIDs(ctx, tx, parsedID)
	if err != nil {
		return err
	}
	state.TranscriptCorrections[idValue] = domain.TranscriptCorrectionState{
		ID: parsedID, Revision: revision, AssetID: assetID, ArtifactID: artifactID,
		SegmentIDs: segmentIDs, SourceRange: rangeValue, ReplacementText: replacementText,
		Language: language, Tombstoned: tombstoned,
	}
	return nil
}

func loadTranscriptCorrectionSegmentIDs(
	ctx context.Context,
	tx *sql.Tx,
	id domain.TranscriptCorrectionID,
) ([]domain.TranscriptSegmentID, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT segment_id FROM transcript_correction_segments
WHERE correction_id = ? ORDER BY ordinal`, id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.TranscriptSegmentID, 0)
	for rows.Next() {
		if len(result) >= 256 {
			return nil, application.ErrEditInvalid
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		parsed, err := domain.ParseTranscriptSegmentID(value)
		if err != nil {
			return nil, application.ErrEditInvalid
		}
		result = append(result, parsed)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, application.ErrEditInvalid
	}
	return result, nil
}

func ensureEditTranscriptArtifact(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.ArtifactID,
	segmentIDs []domain.TranscriptSegmentID,
) error {
	artifact, exists := state.TranscriptArtifacts[id.String()]
	if !exists {
		var idValue, assetValue, fingerprint, streamValue, languageValue string
		err := tx.QueryRowContext(ctx, `
SELECT artifact.id, artifact.asset_id, artifact.input_fingerprint,
       transcript.source_stream_id, transcript.detected_language
FROM media_artifacts artifact
JOIN transcript_artifacts transcript ON transcript.artifact_id = artifact.id
WHERE artifact.id = ? AND artifact.project_id = ? AND artifact.kind = 'transcript'
  AND artifact.state = 'ready'`, id.String(), state.ProjectID.String()).Scan(
			&idValue, &assetValue, &fingerprint, &streamValue, &languageValue,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrEditInvalid
		}
		if err != nil {
			return err
		}
		parsedID, idErr := domain.ParseArtifactID(idValue)
		assetID, assetErr := domain.ParseAssetID(assetValue)
		parsedFingerprint, fingerprintErr := domain.ParseDigest(fingerprint)
		streamID, streamErr := domain.ParseSourceStreamID(streamValue)
		language, languageErr := domain.ParseCaptionLanguage(languageValue)
		if idErr != nil || assetErr != nil || fingerprintErr != nil || streamErr != nil || languageErr != nil {
			return application.ErrEditInvalid
		}
		artifact = application.EditTranscriptArtifactState{
			ID: parsedID, AssetID: assetID, Fingerprint: parsedFingerprint,
			SourceStreamID: streamID, Language: language,
			Segments: make(map[string]application.EditTranscriptSegmentState),
		}
	}
	for _, segmentID := range segmentIDs {
		if _, loaded := artifact.Segments[segmentID.String()]; loaded {
			continue
		}
		segment, err := loadEditTranscriptSegment(ctx, tx, id, segmentID)
		if err != nil {
			return err
		}
		artifact.Segments[segmentID.String()] = segment
	}
	state.TranscriptArtifacts[id.String()] = artifact
	return nil
}

func loadEditTranscriptSegment(
	ctx context.Context,
	tx *sql.Tx,
	artifactID domain.ArtifactID,
	id domain.TranscriptSegmentID,
) (application.EditTranscriptSegmentState, error) {
	var idValue, text string
	var ordinal uint32
	var startValue, durationValue int64
	var startScale, durationScale int32
	err := tx.QueryRowContext(ctx, `
SELECT id, ordinal, source_start_value, source_start_scale,
       source_duration_value, source_duration_scale, text
FROM transcript_segments WHERE id = ? AND artifact_id = ?`,
		id.String(), artifactID.String()).Scan(
		&idValue, &ordinal, &startValue, &startScale, &durationValue, &durationScale, &text,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.EditTranscriptSegmentState{}, application.ErrEditInvalid
	}
	if err != nil {
		return application.EditTranscriptSegmentState{}, err
	}
	parsedID, idErr := domain.ParseTranscriptSegmentID(idValue)
	start, startErr := domain.NewRationalTime(startValue, startScale)
	duration, durationErr := domain.NewRationalTime(durationValue, durationScale)
	rangeValue, rangeErr := domain.NewTimeRange(start, duration)
	if idErr != nil || startErr != nil || durationErr != nil || rangeErr != nil || text == "" {
		return application.EditTranscriptSegmentState{}, application.ErrEditInvalid
	}
	tokens, err := loadEditTranscriptTokens(ctx, tx, parsedID)
	if err != nil {
		return application.EditTranscriptSegmentState{}, err
	}
	return application.EditTranscriptSegmentState{
		ID: parsedID, Ordinal: ordinal, SourceRange: rangeValue, Text: text, Tokens: tokens,
	}, nil
}

func loadEditTranscriptTokens(
	ctx context.Context,
	tx *sql.Tx,
	segmentID domain.TranscriptSegmentID,
) ([]domain.TranscriptToken, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, ordinal, source_start_value, source_start_scale,
       source_duration_value, source_duration_scale, text, confidence_basis_points
FROM transcript_tokens WHERE segment_id = ? ORDER BY ordinal`, segmentID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.TranscriptToken, 0)
	for rows.Next() {
		if len(result) >= domain.MaximumTranscriptTokensPerSegment {
			return nil, application.ErrEditInvalid
		}
		var idValue, text string
		var ordinal uint32
		var startValue, durationValue int64
		var startScale, durationScale int32
		var confidence sql.NullInt64
		if err := rows.Scan(
			&idValue, &ordinal, &startValue, &startScale, &durationValue,
			&durationScale, &text, &confidence,
		); err != nil {
			return nil, err
		}
		id, idErr := domain.ParseTranscriptTokenID(idValue)
		start, startErr := domain.NewRationalTime(startValue, startScale)
		duration, durationErr := domain.NewRationalTime(durationValue, durationScale)
		rangeValue, rangeErr := domain.NewTimeRange(start, duration)
		if idErr != nil || startErr != nil || durationErr != nil || rangeErr != nil || text == "" {
			return nil, application.ErrEditInvalid
		}
		var confidenceValue *uint16
		if confidence.Valid {
			if confidence.Int64 < 0 || confidence.Int64 > 10_000 {
				return nil, application.ErrEditInvalid
			}
			value := uint16(confidence.Int64)
			confidenceValue = &value
		}
		result = append(result, domain.TranscriptToken{
			ID: id, Ordinal: ordinal, SourceRange: rangeValue, Text: text,
			ConfidenceBasisPoints: confidenceValue,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, application.ErrEditInvalid
	}
	return result, nil
}

func loadTranscriptCorrectionOverlaps(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	artifactID domain.ArtifactID,
	language domain.CaptionLanguage,
	rangeValue domain.TimeRange,
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
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM transcript_corrections
WHERE project_id = ? AND artifact_id = ? AND language = ? AND tombstoned = 0
  AND source_start_order_key < ? AND source_end_order_key > ?
ORDER BY source_start_order_key, id LIMIT 257`,
		state.ProjectID.String(), artifactID.String(), language.String(), endKey, startKey)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 256 {
			return application.ErrEditInvalid
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			return err
		}
		id, err := domain.ParseTranscriptCorrectionID(value)
		if err != nil {
			return application.ErrEditInvalid
		}
		if err := ensureTranscriptCorrectionState(ctx, tx, state, id); err != nil {
			return err
		}
	}
	return rows.Err()
}
