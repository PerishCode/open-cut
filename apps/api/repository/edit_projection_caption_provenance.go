package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func insertCaptionProvenance(
	ctx context.Context,
	tx *sql.Tx,
	state domain.CaptionState,
) error {
	if state.Provenance.Kind != domain.CaptionProvenanceManual &&
		state.Provenance.Kind != domain.CaptionProvenanceTranscriptDerivation {
		return application.ErrEditInvalid
	}
	if (state.Provenance.Kind == domain.CaptionProvenanceManual) != (state.Provenance.Derivation == nil) {
		return application.ErrEditInvalid
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO caption_provenance (caption_id, kind) VALUES (?, ?)`,
		state.ID.String(), state.Provenance.Kind); err != nil {
		return err
	}
	if state.Provenance.Derivation == nil {
		return nil
	}
	value := state.Provenance.Derivation
	if value.Policy.Validate() != nil || value.SourceExcerptID.IsZero() || value.AssetID.IsZero() ||
		value.ArtifactID.IsZero() || value.SourceStreamID.IsZero() || value.ClipID.IsZero() ||
		len(value.SegmentIDs) == 0 || len(value.SegmentIDs) > 256 || len(value.CorrectionRevisions) > 256 ||
		!equalRepositoryRange(state.Range, value.DerivedRange) || state.Language != value.DerivedLanguage ||
		state.Text != value.DerivedText {
		return application.ErrEditInvalid
	}
	policy := value.Policy
	if _, err := tx.ExecContext(ctx, `
INSERT INTO caption_derivations (
  caption_id, source_excerpt_id, source_excerpt_revision, asset_id, accepted_fingerprint,
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
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		state.ID.String(), value.SourceExcerptID.String(), value.SourceExcerptRevision.Value(),
		value.AssetID.String(), value.AcceptedFingerprint.String(), value.ArtifactID.String(),
		value.SourceStreamID.String(), value.ClipID.String(), value.ClipRevision.Value(),
		value.ClipSourceRange.Start.Value.Value(), value.ClipSourceRange.Start.Scale,
		value.ClipSourceRange.Duration.Value.Value(), value.ClipSourceRange.Duration.Scale,
		value.ClipTimelineRange.Start.Value.Value(), value.ClipTimelineRange.Start.Scale,
		value.ClipTimelineRange.Duration.Value.Value(), value.ClipTimelineRange.Duration.Scale,
		value.EvidenceSourceRange.Start.Value.Value(), value.EvidenceSourceRange.Start.Scale,
		value.EvidenceSourceRange.Duration.Value.Value(), value.EvidenceSourceRange.Duration.Scale,
		policy.ID, policy.MaximumLines, policy.MaximumLineGraphemes,
		policy.MinimumDuration.Value.Value(), policy.MinimumDuration.Scale,
		policy.MaximumDuration.Value.Value(), policy.MaximumDuration.Scale,
		policy.MaximumGap.Value.Value(), policy.MaximumGap.Scale, policy.MaximumReadingRate,
		policy.BoundaryPolicy, policy.TimingPolicy, policy.UnicodeSegmentationID,
		value.DerivedRange.Start.Value.Value(), value.DerivedRange.Start.Scale,
		value.DerivedRange.Duration.Value.Value(), value.DerivedRange.Duration.Scale,
		value.DerivedLanguage.String(), value.DerivedText,
	); err != nil {
		return err
	}
	for ordinal, segmentID := range value.SegmentIDs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO caption_derivation_segments (caption_id, ordinal, segment_id) VALUES (?, ?, ?)`,
			state.ID.String(), ordinal, segmentID.String()); err != nil {
			return err
		}
	}
	for ordinal, correction := range value.CorrectionRevisions {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO caption_derivation_corrections (
  caption_id, ordinal, correction_id, correction_revision
) VALUES (?, ?, ?, ?)`, state.ID.String(), ordinal, correction.ID.String(), correction.Revision.Value()); err != nil {
			return err
		}
	}
	return nil
}

func equalRepositoryRange(left, right domain.TimeRange) bool {
	start, startErr := left.Start.Compare(right.Start)
	duration, durationErr := left.Duration.Compare(right.Duration)
	return startErr == nil && durationErr == nil && start == 0 && duration == 0
}
