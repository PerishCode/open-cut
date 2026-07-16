package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func loadCaptionProvenance(
	ctx context.Context,
	tx *sql.Tx,
	captionID domain.CaptionID,
) (domain.CaptionProvenance, error) {
	var kind string
	if err := tx.QueryRowContext(ctx, `
SELECT kind FROM caption_provenance WHERE caption_id = ?`, captionID.String()).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.CaptionProvenance{}, application.ErrEditInvalid
		}
		return domain.CaptionProvenance{}, err
	}
	if domain.CaptionProvenanceKind(kind) == domain.CaptionProvenanceManual {
		return domain.CaptionProvenance{Kind: domain.CaptionProvenanceManual}, nil
	}
	if domain.CaptionProvenanceKind(kind) != domain.CaptionProvenanceTranscriptDerivation {
		return domain.CaptionProvenance{}, application.ErrEditInvalid
	}
	var sourceExcerptValue, assetValue, fingerprintValue, artifactValue, streamValue, clipValue string
	var policyID, boundaryPolicy, timingPolicy, unicodePolicy, languageValue, text string
	var sourceExcerptRevision, clipRevision uint64
	var maximumLines uint8
	var maximumLineGraphemes, maximumReadingRate uint16
	var clipSourceStart, clipSourceDuration, clipTimelineStart, clipTimelineDuration int64
	var evidenceStart, evidenceDuration, minimumDuration, maximumDuration, maximumGap int64
	var derivedStart, derivedDuration int64
	var clipSourceStartScale, clipSourceDurationScale, clipTimelineStartScale, clipTimelineDurationScale int32
	var evidenceStartScale, evidenceDurationScale, minimumDurationScale, maximumDurationScale, maximumGapScale int32
	var derivedStartScale, derivedDurationScale int32
	err := tx.QueryRowContext(ctx, `
SELECT source_excerpt_id, source_excerpt_revision, asset_id, accepted_fingerprint,
       transcript_artifact_id, source_stream_id, clip_id, clip_revision,
       clip_source_start_value, clip_source_start_scale, clip_source_duration_value, clip_source_duration_scale,
       clip_timeline_start_value, clip_timeline_start_scale, clip_timeline_duration_value, clip_timeline_duration_scale,
       evidence_start_value, evidence_start_scale, evidence_duration_value, evidence_duration_scale,
       policy_id, policy_maximum_lines, policy_maximum_line_graphemes,
       policy_minimum_duration_value, policy_minimum_duration_scale,
       policy_maximum_duration_value, policy_maximum_duration_scale,
       policy_maximum_gap_value, policy_maximum_gap_scale, policy_maximum_reading_rate,
       policy_boundary, policy_timing, policy_unicode,
       derived_start_value, derived_start_scale, derived_duration_value, derived_duration_scale,
       derived_language, derived_text
FROM caption_derivations WHERE caption_id = ?`, captionID.String()).Scan(
		&sourceExcerptValue, &sourceExcerptRevision, &assetValue, &fingerprintValue,
		&artifactValue, &streamValue, &clipValue, &clipRevision,
		&clipSourceStart, &clipSourceStartScale, &clipSourceDuration, &clipSourceDurationScale,
		&clipTimelineStart, &clipTimelineStartScale, &clipTimelineDuration, &clipTimelineDurationScale,
		&evidenceStart, &evidenceStartScale, &evidenceDuration, &evidenceDurationScale,
		&policyID, &maximumLines, &maximumLineGraphemes,
		&minimumDuration, &minimumDurationScale, &maximumDuration, &maximumDurationScale,
		&maximumGap, &maximumGapScale, &maximumReadingRate,
		&boundaryPolicy, &timingPolicy, &unicodePolicy,
		&derivedStart, &derivedStartScale, &derivedDuration, &derivedDurationScale,
		&languageValue, &text,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CaptionProvenance{}, application.ErrEditInvalid
	}
	if err != nil {
		return domain.CaptionProvenance{}, err
	}
	sourceExcerptID, sourceErr := domain.ParseNarrativeNodeID(sourceExcerptValue)
	assetID, assetErr := domain.ParseAssetID(assetValue)
	fingerprint, fingerprintErr := domain.ParseDigest(fingerprintValue)
	artifactID, artifactErr := domain.ParseArtifactID(artifactValue)
	streamID, streamErr := domain.ParseSourceStreamID(streamValue)
	clipID, clipErr := domain.ParseClipID(clipValue)
	sourceRevision, sourceRevisionErr := domain.NewRevision(sourceExcerptRevision)
	parsedClipRevision, clipRevisionErr := domain.NewRevision(clipRevision)
	language, languageErr := domain.ParseCaptionLanguage(languageValue)
	clipSourceRange, clipSourceErr := storedCaptionTimeRange(
		clipSourceStart, clipSourceStartScale, clipSourceDuration, clipSourceDurationScale,
	)
	clipTimelineRange, clipTimelineErr := storedCaptionTimeRange(
		clipTimelineStart, clipTimelineStartScale, clipTimelineDuration, clipTimelineDurationScale,
	)
	evidenceRange, evidenceErr := storedCaptionTimeRange(
		evidenceStart, evidenceStartScale, evidenceDuration, evidenceDurationScale,
	)
	derivedRange, derivedErr := storedCaptionTimeRange(
		derivedStart, derivedStartScale, derivedDuration, derivedDurationScale,
	)
	minimum, minimumErr := domain.NewRationalTime(minimumDuration, minimumDurationScale)
	maximum, maximumErr := domain.NewRationalTime(maximumDuration, maximumDurationScale)
	gap, gapErr := domain.NewRationalTime(maximumGap, maximumGapScale)
	if sourceErr != nil || assetErr != nil || fingerprintErr != nil || artifactErr != nil || streamErr != nil ||
		clipErr != nil || sourceRevisionErr != nil || clipRevisionErr != nil || languageErr != nil ||
		clipSourceErr != nil || clipTimelineErr != nil || evidenceErr != nil || derivedErr != nil ||
		minimumErr != nil || maximumErr != nil || gapErr != nil || text == "" {
		return domain.CaptionProvenance{}, application.ErrEditInvalid
	}
	policy := domain.CaptionDerivationPolicy{
		ID: policyID, MaximumLines: maximumLines, MaximumLineGraphemes: maximumLineGraphemes,
		MinimumDuration: minimum, MaximumDuration: maximum, MaximumGap: gap,
		MaximumReadingRate: maximumReadingRate, BoundaryPolicy: boundaryPolicy,
		TimingPolicy: timingPolicy, UnicodeSegmentationID: unicodePolicy,
	}
	if policy.Validate() != nil {
		return domain.CaptionProvenance{}, application.ErrEditInvalid
	}
	segments, err := loadCaptionDerivationSegments(ctx, tx, captionID)
	if err != nil {
		return domain.CaptionProvenance{}, err
	}
	corrections, err := loadCaptionDerivationCorrections(ctx, tx, captionID)
	if err != nil {
		return domain.CaptionProvenance{}, err
	}
	return domain.CaptionProvenance{
		Kind: domain.CaptionProvenanceTranscriptDerivation,
		Derivation: &domain.CaptionDerivationProvenance{
			SourceExcerptID: sourceExcerptID, SourceExcerptRevision: sourceRevision,
			AssetID: assetID, AcceptedFingerprint: fingerprint, ArtifactID: artifactID,
			SourceStreamID: streamID, SegmentIDs: segments, CorrectionRevisions: corrections,
			ClipID: clipID, ClipRevision: parsedClipRevision, ClipSourceRange: clipSourceRange,
			ClipTimelineRange: clipTimelineRange, EvidenceSourceRange: evidenceRange,
			Policy: policy, DerivedRange: derivedRange, DerivedLanguage: language, DerivedText: text,
		},
	}, nil
}

func storedCaptionTimeRange(
	startValue int64,
	startScale int32,
	durationValue int64,
	durationScale int32,
) (domain.TimeRange, error) {
	start, err := domain.NewRationalTime(startValue, startScale)
	if err != nil {
		return domain.TimeRange{}, err
	}
	duration, err := domain.NewRationalTime(durationValue, durationScale)
	if err != nil {
		return domain.TimeRange{}, err
	}
	return domain.NewTimeRange(start, duration)
}

func loadCaptionDerivationSegments(
	ctx context.Context,
	tx *sql.Tx,
	captionID domain.CaptionID,
) ([]domain.TranscriptSegmentID, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT segment_id FROM caption_derivation_segments WHERE caption_id = ? ORDER BY ordinal`, captionID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.TranscriptSegmentID, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		id, err := domain.ParseTranscriptSegmentID(value)
		if err != nil {
			return nil, application.ErrEditInvalid
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 || len(result) > 256 {
		return nil, application.ErrEditInvalid
	}
	return result, nil
}

func loadCaptionDerivationCorrections(
	ctx context.Context,
	tx *sql.Tx,
	captionID domain.CaptionID,
) ([]domain.TranscriptCorrectionRevisionRef, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT correction_id, correction_revision
FROM caption_derivation_corrections WHERE caption_id = ? ORDER BY ordinal`, captionID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.TranscriptCorrectionRevisionRef, 0)
	for rows.Next() {
		var value string
		var revisionValue uint64
		if err := rows.Scan(&value, &revisionValue); err != nil {
			return nil, err
		}
		id, idErr := domain.ParseTranscriptCorrectionID(value)
		revision, revisionErr := domain.NewRevision(revisionValue)
		if idErr != nil || revisionErr != nil {
			return nil, application.ErrEditInvalid
		}
		result = append(result, domain.TranscriptCorrectionRevisionRef{ID: id, Revision: revision})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) > 256 {
		return nil, application.ErrEditInvalid
	}
	return result, nil
}
