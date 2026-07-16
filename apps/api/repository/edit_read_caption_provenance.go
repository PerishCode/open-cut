package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func captionProvenanceStatus(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	caption domain.CaptionState,
) (*domain.CaptionProvenanceStatus, error) {
	derivation := caption.Provenance.Derivation
	if caption.Provenance.Kind == domain.CaptionProvenanceManual && derivation == nil {
		return nil, nil
	}
	if caption.Provenance.Kind != domain.CaptionProvenanceTranscriptDerivation || derivation == nil {
		return nil, nil
	}
	status := &domain.CaptionProvenanceStatus{
		Content: domain.CaptionContentModified, Evidence: domain.CaptionEvidenceStale,
	}
	if caption.Text == derivation.DerivedText && caption.Language == derivation.DerivedLanguage &&
		equalRepositoryRange(caption.Range, derivation.DerivedRange) {
		status.Content = domain.CaptionContentExact
	}
	state := domain.SourceExcerptState{}
	normalization := newCaptionEvidenceState(projectID)
	if err := ensureSourceExcerptState(ctx, tx, &normalization, derivation.SourceExcerptID); err != nil {
		if errors.Is(err, application.ErrEditInvalid) {
			return status, nil
		}
		return nil, err
	}
	state = normalization.SourceExcerpts[derivation.SourceExcerptID.String()]
	if state.Tombstoned || state.Revision != derivation.SourceExcerptRevision ||
		state.AssetID != derivation.AssetID || state.AcceptedFingerprint != derivation.AcceptedFingerprint ||
		state.Evidence.ArtifactID != derivation.ArtifactID || state.Evidence.SourceStreamID != derivation.SourceStreamID ||
		!transcriptRangeContainsRepository(state.SourceRange, derivation.EvidenceSourceRange) {
		return status, nil
	}
	excerptStatus, err := sourceExcerptEvidenceStatus(ctx, tx, projectID, state)
	if err != nil {
		return nil, err
	}
	if excerptStatus != domain.SourceExcerptEvidenceExact {
		return status, nil
	}
	clip, err := loadClipState(ctx, tx, projectID, derivation.ClipID)
	if errors.Is(err, application.ErrEditInvalid) {
		return status, nil
	}
	if err != nil {
		return nil, err
	}
	if clip.Tombstoned || !clip.Enabled || clip.Revision != derivation.ClipRevision ||
		clip.AssetID != derivation.AssetID || clip.SourceStreamID != derivation.SourceStreamID ||
		!equalRepositoryRange(clip.SourceRange, derivation.ClipSourceRange) ||
		!equalRepositoryRange(clip.TimelineRange, derivation.ClipTimelineRange) {
		return status, nil
	}
	status.Evidence = domain.CaptionEvidenceExact
	return status, nil
}

func newCaptionEvidenceState(projectID domain.ProjectID) application.EditNormalizationState {
	return application.EditNormalizationState{
		ProjectID: projectID, SourceExcerpts: make(map[string]domain.SourceExcerptState),
		AuthoredTexts: make(map[string]domain.AuthoredTextState),
	}
}

func transcriptRangeContainsRepository(parent, child domain.TimeRange) bool {
	parentEnd, parentErr := parent.End()
	childEnd, childErr := child.End()
	startComparison, startErr := child.Start.Compare(parent.Start)
	endComparison, endErr := childEnd.Compare(parentEnd)
	return parentErr == nil && childErr == nil && startErr == nil && endErr == nil &&
		startComparison >= 0 && endComparison <= 0
}
