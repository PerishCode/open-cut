package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ReadCaptionDerivationPreview(
	ctx context.Context,
	query application.CaptionDerivationPreviewQuery,
) (application.CaptionDerivationPreview, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	defer tx.Rollback()
	state, err := loadEditHeads(ctx, tx, query.ProjectID, query.SequenceID)
	if err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	if err := ensureSourceExcerptState(ctx, tx, &state, query.Input.SourceExcerptID); err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	excerpt := state.SourceExcerpts[query.Input.SourceExcerptID.String()]
	if err := ensureEditTranscriptArtifact(
		ctx, tx, &state, excerpt.Evidence.ArtifactID, excerpt.Evidence.SegmentIDs,
	); err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	if err := loadTranscriptCorrectionOverlaps(
		ctx, tx, &state, excerpt.Evidence.ArtifactID, excerpt.Language, excerpt.SourceRange,
	); err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	if err := ensureSourceStreamState(
		ctx, tx, &state, excerpt.AssetID, excerpt.Evidence.SourceStreamID,
	); err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	if err := ensureClipState(ctx, tx, &state, query.Input.ClipID); err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	if err := ensureTrackState(ctx, tx, &state, query.Input.TrackID); err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	track := state.Tracks[query.Input.TrackID.String()]
	clip := state.Clips[query.Input.ClipID.String()]
	if track.SequenceID != query.SequenceID || track.Type != domain.TrackCaption ||
		clip.SequenceID != query.SequenceID {
		return application.CaptionDerivationPreview{}, application.ErrEditInvalid
	}
	policy := domain.ReadableCaptionPolicyV1()
	cues, err := application.DeriveCaptionCues(
		state.TranscriptArtifacts[excerpt.Evidence.ArtifactID.String()], excerpt,
		state.TranscriptCorrections, clip, policy,
	)
	if err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	outputs := make([]application.DerivedCaptionOutputInput, len(cues))
	for index, cue := range cues {
		if err := loadCaptionOverlaps(
			ctx, tx, &state, query.SequenceID, track.ID, cue.TimelineRange, nil,
		); err != nil {
			return application.CaptionDerivationPreview{}, err
		}
		if len(state.Captions) != 0 {
			return application.CaptionDerivationPreview{}, application.ErrEditInvalid
		}
		captionLocal, _ := domain.ParseLocalID(fmt.Sprintf("%s_caption_%03d", query.Input.LocalPrefix, index+1))
		alignmentLocal, _ := domain.ParseLocalID(fmt.Sprintf("%s_alignment_%03d", query.Input.LocalPrefix, index+1))
		if captionLocal.String() == "" || alignmentLocal.String() == "" {
			return application.CaptionDerivationPreview{}, application.ErrEditInvalid
		}
		outputs[index] = application.DerivedCaptionOutputInput{
			CaptionAs: captionLocal, AlignmentAs: alignmentLocal,
			SourceRange: cue.SourceRange, TimelineRange: cue.TimelineRange, Text: cue.Text,
		}
	}
	stream := state.SourceStreams[excerpt.Evidence.SourceStreamID.String()]
	preconditions := []domain.EntityPrecondition{
		{Kind: domain.EntityNarrativeNode, ID: excerpt.ID.String(), Revision: excerpt.Revision},
		{Kind: domain.EntityAsset, ID: excerpt.AssetID.String(), Revision: stream.AssetRevision},
		{Kind: domain.EntityClip, ID: clip.ID.String(), Revision: clip.Revision},
		{Kind: domain.EntityTrack, ID: track.ID.String(), Revision: track.Revision},
	}
	for _, correction := range excerpt.Evidence.CorrectionRevisions {
		preconditions = append(preconditions, domain.EntityPrecondition{
			Kind: domain.EntityTranscriptCorrection, ID: correction.ID.String(), Revision: correction.Revision,
		})
	}
	sort.Slice(preconditions, func(left, right int) bool {
		if preconditions[left].Kind != preconditions[right].Kind {
			return preconditions[left].Kind < preconditions[right].Kind
		}
		return preconditions[left].ID < preconditions[right].ID
	})
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	result := application.CaptionDerivationPreview{
		BaseProjectRevision: state.ProjectRevision, Preconditions: preconditions,
		Operation: application.EditOperationInput{
			Type:          domain.EditDeriveCaptions,
			NarrativeNode: &application.EditReference{ID: excerpt.ID.String()},
			Clip:          &application.EditReference{ID: clip.ID.String()}, TrackID: &track.ID,
			CaptionPolicy: &policy, DerivedCaptions: outputs,
		},
		Language: excerpt.Language, ActivityCursor: cursor,
	}
	if err := tx.Commit(); err != nil {
		return application.CaptionDerivationPreview{}, err
	}
	return result, nil
}
