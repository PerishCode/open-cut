package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ReadTranscript(
	ctx context.Context,
	query application.TranscriptReadQuery,
) (application.TranscriptReadPage, error) {
	after := int64(-1)
	if query.After != "" {
		value, err := strconv.ParseUint(query.After, 10, 32)
		if err != nil || strconv.FormatUint(value, 10) != query.After {
			return application.TranscriptReadPage{}, application.ErrTranscriptReadInvalid
		}
		after = int64(value)
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.TranscriptReadPage{}, err
	}
	defer tx.Rollback()
	artifactID := ""
	if query.ArtifactID != nil {
		artifactID = query.ArtifactID.String()
	} else if err := tx.QueryRowContext(ctx, `
SELECT selection.artifact_id
FROM asset_transcript_selection selection
JOIN assets asset ON asset.id = selection.asset_id
WHERE selection.asset_id = ? AND asset.project_id = ?`,
		query.AssetID.String(), query.ProjectID.String(),
	).Scan(&artifactID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.TranscriptReadPage{}, application.ErrTranscriptNotFound
		}
		return application.TranscriptReadPage{}, err
	}
	page, err := loadTranscriptReadHeader(ctx, tx, query, artifactID)
	if err != nil {
		return application.TranscriptReadPage{}, err
	}
	type segmentRecord struct {
		id, text                  string
		ordinal                   uint32
		startValue, durationValue int64
		startScale, durationScale int32
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, ordinal, source_start_value, source_start_scale,
       source_duration_value, source_duration_scale, text
FROM transcript_segments
WHERE artifact_id = ? AND ordinal > ?
ORDER BY ordinal, id LIMIT ?`, artifactID, after, int(query.Limit)+1)
	if err != nil {
		return application.TranscriptReadPage{}, err
	}
	records := make([]segmentRecord, 0, query.Limit+1)
	for rows.Next() {
		var record segmentRecord
		if err := rows.Scan(
			&record.id, &record.ordinal, &record.startValue, &record.startScale,
			&record.durationValue, &record.durationScale, &record.text,
		); err != nil {
			rows.Close()
			return application.TranscriptReadPage{}, err
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return application.TranscriptReadPage{}, err
	}
	if err := rows.Err(); err != nil {
		return application.TranscriptReadPage{}, err
	}
	hasMore := len(records) > int(query.Limit)
	if hasMore {
		records = records[:query.Limit]
	}
	page.Segments = make([]application.TranscriptSegmentView, 0, len(records))
	tokenCount := 0
	for index, record := range records {
		segment, err := loadTranscriptSegmentView(ctx, tx, record)
		if err != nil {
			return application.TranscriptReadPage{}, err
		}
		if tokenCount+len(segment.Tokens) > application.MaximumTranscriptReadTokens {
			hasMore = true
			break
		}
		tokenCount += len(segment.Tokens)
		page.Segments = append(page.Segments, segment)
		if index+1 < len(records) && tokenCount == application.MaximumTranscriptReadTokens {
			hasMore = true
			break
		}
	}
	if hasMore && len(page.Segments) > 0 {
		page.NextAfter = strconv.FormatUint(uint64(page.Segments[len(page.Segments)-1].Ordinal), 10)
	}
	page.Corrections, err = loadTranscriptCorrectionViews(
		ctx, tx, query.ProjectID, page.Artifact.ID, page.Segments,
	)
	if err != nil {
		return application.TranscriptReadPage{}, err
	}
	page.ActivityCursor, err = loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.TranscriptReadPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.TranscriptReadPage{}, err
	}
	return page, nil
}

func loadTranscriptReadHeader(
	ctx context.Context,
	tx *sql.Tx,
	query application.TranscriptReadQuery,
	artifactID string,
) (application.TranscriptReadPage, error) {
	var idValue, assetValue, sourceStreamValue, engineVersion, engineTarget string
	var modelName, modelVersion, language, sourceStartValue, createdAt string
	var sourceStartScale int32
	var sampleCount uint64
	var languageConfidence sql.NullInt64
	var isDefault bool
	err := tx.QueryRowContext(ctx, `
SELECT artifact.id, artifact.asset_id, transcript.source_stream_id,
       binding.engine_version, binding.engine_target, binding.model_name, binding.model_version,
       transcript.detected_language, transcript.language_confidence_basis_points,
       CAST(transcript.source_start_value AS TEXT), transcript.source_start_scale,
       transcript.sample_count, artifact.created_at,
       EXISTS (
         SELECT 1 FROM asset_transcript_selection selection
         WHERE selection.asset_id = artifact.asset_id AND selection.artifact_id = artifact.id
       )
FROM media_artifacts artifact
JOIN transcript_artifacts transcript ON transcript.artifact_id = artifact.id
JOIN transcript_job_bindings binding ON binding.binding_digest = transcript.binding_digest
WHERE artifact.id = ? AND artifact.project_id = ? AND artifact.asset_id = ?
  AND artifact.kind = 'transcript' AND artifact.state = 'ready'`,
		artifactID, query.ProjectID.String(), query.AssetID.String(),
	).Scan(
		&idValue, &assetValue, &sourceStreamValue, &engineVersion, &engineTarget, &modelName, &modelVersion,
		&language, &languageConfidence, &sourceStartValue, &sourceStartScale, &sampleCount, &createdAt, &isDefault,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.TranscriptReadPage{}, application.ErrTranscriptNotFound
	}
	if err != nil {
		return application.TranscriptReadPage{}, err
	}
	id, idErr := domain.ParseArtifactID(idValue)
	assetID, assetErr := domain.ParseAssetID(assetValue)
	streamID, streamErr := domain.ParseSourceStreamID(sourceStreamValue)
	startNumber, numberErr := strconv.ParseInt(sourceStartValue, 10, 64)
	start, startErr := domain.NewRationalTime(startNumber, sourceStartScale)
	samples, sampleErr := domain.NewUInt64(sampleCount)
	created, createdErr := time.Parse(time.RFC3339Nano, createdAt)
	if idErr != nil || assetErr != nil || streamErr != nil || numberErr != nil || startErr != nil || sampleErr != nil ||
		createdErr != nil || assetID != query.AssetID || engineVersion == "" || len(engineVersion) > 1024 ||
		engineTarget == "" || len(engineTarget) > 128 || !domain.ValidProductResourceName(modelName) ||
		modelVersion == "" || len(modelVersion) > 128 {
		return application.TranscriptReadPage{}, application.ErrTranscriptReadInvalid
	}
	if _, err := domain.ParseCaptionLanguage(language); err != nil {
		return application.TranscriptReadPage{}, application.ErrTranscriptReadInvalid
	}
	var confidence *uint16
	if languageConfidence.Valid {
		if languageConfidence.Int64 < 0 || languageConfidence.Int64 > 10_000 {
			return application.TranscriptReadPage{}, application.ErrTranscriptReadInvalid
		}
		value := uint16(languageConfidence.Int64)
		confidence = &value
	}
	return application.TranscriptReadPage{
		Schema:      application.TranscriptReadSchema,
		Corrections: []application.TranscriptCorrectionView{},
		Artifact: application.TranscriptArtifactView{
			ID: id, AssetID: assetID, SourceStreamID: streamID,
			RecognitionProfile: application.TranscriptProfile,
			EngineVersion:      engineVersion, EngineTarget: engineTarget,
			ModelName: modelName, ModelVersion: modelVersion,
			DetectedLanguage: language, LanguageConfidenceBasisPoints: confidence,
			SourceStartTime: start, NormalizedSampleCount: samples,
			IsDefault: isDefault, CreatedAt: created.UTC(),
		},
	}, nil
}

func loadTranscriptCorrectionViews(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	artifactID domain.ArtifactID,
	segments []application.TranscriptSegmentView,
) ([]application.TranscriptCorrectionView, error) {
	result := make([]application.TranscriptCorrectionView, 0)
	if len(segments) == 0 {
		return result, nil
	}
	pageRange, err := transcriptPageRange(segments)
	if err != nil {
		return nil, err
	}
	startKey, endKey, err := sourceOrderKeys(pageRange)
	if err != nil {
		return nil, application.ErrTranscriptReadInvalid
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM transcript_corrections
WHERE project_id = ? AND artifact_id = ? AND tombstoned = 0
  AND source_start_order_key < ? AND source_end_order_key > ?
ORDER BY source_start_order_key, id LIMIT 257`,
		projectID.String(), artifactID.String(), endKey, startKey)
	if err != nil {
		return nil, err
	}
	ids := make([]domain.TranscriptCorrectionID, 0)
	for rows.Next() {
		if len(ids) >= 256 {
			rows.Close()
			return nil, application.ErrTranscriptReadInvalid
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return nil, err
		}
		id, parseErr := domain.ParseTranscriptCorrectionID(value)
		if parseErr != nil {
			rows.Close()
			return nil, application.ErrTranscriptReadInvalid
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	state := application.EditNormalizationState{
		ProjectID:             projectID,
		TranscriptCorrections: make(map[string]domain.TranscriptCorrectionState),
		TranscriptArtifacts:   make(map[string]application.EditTranscriptArtifactState),
	}
	for _, id := range ids {
		if err := ensureTranscriptCorrectionState(ctx, tx, &state, id); err != nil {
			return nil, application.ErrTranscriptReadInvalid
		}
		correction := state.TranscriptCorrections[id.String()]
		if err := ensureEditTranscriptArtifact(
			ctx, tx, &state, correction.ArtifactID, correction.SegmentIDs,
		); err != nil {
			return nil, application.ErrTranscriptReadInvalid
		}
		original, err := application.ResolveTranscriptEvidenceText(
			state.TranscriptArtifacts[correction.ArtifactID.String()], correction.SegmentIDs,
			correction.SourceRange, nil,
		)
		if err != nil {
			return nil, application.ErrTranscriptReadInvalid
		}
		result = append(result, application.TranscriptCorrectionView{
			ID: correction.ID, Revision: correction.Revision,
			SegmentIDs:  append([]domain.TranscriptSegmentID(nil), correction.SegmentIDs...),
			SourceRange: correction.SourceRange, OriginalText: original,
			EffectiveText: correction.ReplacementText, Language: correction.Language,
		})
	}
	return result, nil
}

func transcriptPageRange(segments []application.TranscriptSegmentView) (domain.TimeRange, error) {
	start := segments[0].SourceRange.Start
	end, err := segments[len(segments)-1].SourceRange.End()
	if err != nil {
		return domain.TimeRange{}, application.ErrTranscriptReadInvalid
	}
	duration, err := end.Subtract(start)
	if err != nil {
		return domain.TimeRange{}, application.ErrTranscriptReadInvalid
	}
	return domain.NewTimeRange(start, duration)
}

func loadTranscriptSegmentView(
	ctx context.Context,
	tx *sql.Tx,
	record struct {
		id, text                  string
		ordinal                   uint32
		startValue, durationValue int64
		startScale, durationScale int32
	},
) (application.TranscriptSegmentView, error) {
	id, idErr := domain.ParseTranscriptSegmentID(record.id)
	start, startErr := domain.NewRationalTime(record.startValue, record.startScale)
	duration, durationErr := domain.NewRationalTime(record.durationValue, record.durationScale)
	rangeValue, rangeErr := domain.NewTimeRange(start, duration)
	if idErr != nil || startErr != nil || durationErr != nil || rangeErr != nil || !duration.IsPositive() ||
		record.text == "" || len([]byte(record.text)) > domain.MaximumTranscriptSegmentBytes ||
		strings.TrimSpace(record.text) != record.text {
		return application.TranscriptSegmentView{}, application.ErrTranscriptReadInvalid
	}
	segment := application.TranscriptSegmentView{
		ID: id, Ordinal: record.ordinal, SourceRange: rangeValue,
		Text: record.text, Tokens: []application.TranscriptTokenView{},
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, ordinal, source_start_value, source_start_scale,
       source_duration_value, source_duration_scale, text, confidence_basis_points
FROM transcript_tokens WHERE segment_id = ? ORDER BY ordinal, id`, record.id)
	if err != nil {
		return application.TranscriptSegmentView{}, err
	}
	var concatenated strings.Builder
	for rows.Next() {
		var tokenIDValue, text string
		var ordinal uint32
		var startValue, durationValue int64
		var startScale, durationScale int32
		var confidenceValue sql.NullInt64
		if err := rows.Scan(
			&tokenIDValue, &ordinal, &startValue, &startScale,
			&durationValue, &durationScale, &text, &confidenceValue,
		); err != nil {
			rows.Close()
			return application.TranscriptSegmentView{}, err
		}
		tokenID, tokenIDErr := domain.ParseTranscriptTokenID(tokenIDValue)
		tokenStart, tokenStartErr := domain.NewRationalTime(startValue, startScale)
		tokenDuration, tokenDurationErr := domain.NewRationalTime(durationValue, durationScale)
		tokenRange, tokenRangeErr := domain.NewTimeRange(tokenStart, tokenDuration)
		if tokenIDErr != nil || tokenStartErr != nil || tokenDurationErr != nil || tokenRangeErr != nil ||
			!tokenDuration.IsPositive() || ordinal != uint32(len(segment.Tokens)) || text == "" ||
			len([]byte(text)) > domain.MaximumTranscriptTokenBytes || len(segment.Tokens) >= domain.MaximumTranscriptTokensPerSegment {
			rows.Close()
			return application.TranscriptSegmentView{}, application.ErrTranscriptReadInvalid
		}
		var confidence *uint16
		if confidenceValue.Valid {
			if confidenceValue.Int64 < 0 || confidenceValue.Int64 > 10_000 {
				rows.Close()
				return application.TranscriptSegmentView{}, application.ErrTranscriptReadInvalid
			}
			value := uint16(confidenceValue.Int64)
			confidence = &value
		}
		segment.Tokens = append(segment.Tokens, application.TranscriptTokenView{
			ID: tokenID, SourceRange: tokenRange, Text: text, ConfidenceBasisPoints: confidence,
		})
		concatenated.WriteString(text)
	}
	if err := rows.Close(); err != nil {
		return application.TranscriptSegmentView{}, err
	}
	if err := rows.Err(); err != nil {
		return application.TranscriptSegmentView{}, err
	}
	if len(segment.Tokens) == 0 || concatenated.String() != segment.Text {
		return application.TranscriptSegmentView{}, application.ErrTranscriptReadInvalid
	}
	return segment, nil
}
