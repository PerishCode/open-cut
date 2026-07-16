package repository

import (
	"context"
	"database/sql"
	"sort"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ReadRoughCutDerivationPreview(
	ctx context.Context,
	query application.RoughCutDerivationPreviewQuery,
) (application.RoughCutDerivationPreview, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.RoughCutDerivationPreview{}, err
	}
	defer tx.Rollback()
	state, err := loadEditHeads(ctx, tx, query.ProjectID, query.SequenceID)
	if err != nil {
		return application.RoughCutDerivationPreview{}, err
	}
	items := make([]application.RoughCutDerivationItemInput, len(query.Input.Items))
	conditions := map[string]domain.EntityPrecondition{
		preconditionKey(domain.EntitySequence, query.SequenceID.String()): {
			Kind: domain.EntitySequence, ID: query.SequenceID.String(), Revision: state.SequenceRevision,
		},
	}
	for index, input := range query.Input.Items {
		if err := ensureSourceExcerptState(ctx, tx, &state, input.SourceExcerptID); err != nil {
			return application.RoughCutDerivationPreview{}, err
		}
		excerpt := state.SourceExcerpts[input.SourceExcerptID.String()]
		if excerpt.Revision != input.SourceExcerptRevision || excerpt.Tombstoned {
			return application.RoughCutDerivationPreview{}, application.ErrEditConflict
		}
		if err := ensureSourceExcerptEvidenceStatus(ctx, tx, &state, excerpt); err != nil {
			return application.RoughCutDerivationPreview{}, err
		}
		if state.SourceExcerptEvidence[excerpt.ID.String()] != domain.SourceExcerptEvidenceExact {
			return application.RoughCutDerivationPreview{}, application.ErrEditInvalid
		}
		conditions[preconditionKey(domain.EntityNarrativeNode, excerpt.ID.String())] = domain.EntityPrecondition{
			Kind: domain.EntityNarrativeNode, ID: excerpt.ID.String(), Revision: excerpt.Revision,
		}
		for _, correction := range excerpt.Evidence.CorrectionRevisions {
			conditions[preconditionKey(domain.EntityTranscriptCorrection, correction.ID.String())] = domain.EntityPrecondition{
				Kind: domain.EntityTranscriptCorrection, ID: correction.ID.String(), Revision: correction.Revision,
			}
		}
		item := application.RoughCutDerivationItemInput{SourceExcerptID: excerpt.ID}
		if input.Video != nil {
			lane, laneErr := loadRoughCutPreviewLane(ctx, tx, &state, excerpt, *input.Video, domain.TrackVideo)
			if laneErr != nil {
				return application.RoughCutDerivationPreview{}, laneErr
			}
			item.Video = &lane
		}
		if input.Audio != nil {
			lane, laneErr := loadRoughCutPreviewLane(ctx, tx, &state, excerpt, *input.Audio, domain.TrackAudio)
			if laneErr != nil {
				return application.RoughCutDerivationPreview{}, laneErr
			}
			item.Audio = &lane
		}
		for _, lane := range []*application.RoughCutLaneBindingInput{item.Video, item.Audio} {
			if lane == nil {
				continue
			}
			track := state.Tracks[lane.TrackID.String()]
			stream := state.SourceStreams[lane.SourceStreamID.String()]
			conditions[preconditionKey(domain.EntityTrack, track.ID.String())] = domain.EntityPrecondition{
				Kind: domain.EntityTrack, ID: track.ID.String(), Revision: track.Revision,
			}
			conditions[preconditionKey(domain.EntityAsset, stream.AssetID.String())] = domain.EntityPrecondition{
				Kind: domain.EntityAsset, ID: stream.AssetID.String(), Revision: stream.AssetRevision,
			}
		}
		items[index] = item
	}
	if len(conditions) > 2048 {
		return application.RoughCutDerivationPreview{}, application.ErrEditInvalid
	}
	operation, err := application.BuildRoughCutOperation(
		state, query.Input.TimelineStart, query.Input.LocalPrefix, items,
	)
	if err != nil {
		return application.RoughCutDerivationPreview{}, err
	}
	for _, output := range operation.DerivedRoughCut {
		for _, lane := range []*application.DerivedRoughCutLaneOutputInput{output.Video, output.Audio} {
			if lane == nil {
				continue
			}
			if err := loadClipOverlaps(
				ctx, tx, &state, query.SequenceID, lane.TrackID, output.TimelineRange,
			); err != nil {
				return application.RoughCutDerivationPreview{}, err
			}
		}
	}
	if len(state.Clips) != 0 {
		return application.RoughCutDerivationPreview{}, application.ErrEditInvalid
	}
	preconditions := make([]domain.EntityPrecondition, 0, len(conditions))
	for _, condition := range conditions {
		preconditions = append(preconditions, condition)
	}
	sort.Slice(preconditions, func(left, right int) bool {
		if preconditions[left].Kind != preconditions[right].Kind {
			return preconditions[left].Kind < preconditions[right].Kind
		}
		return preconditions[left].ID < preconditions[right].ID
	})
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.RoughCutDerivationPreview{}, err
	}
	result := application.RoughCutDerivationPreview{
		BaseProjectRevision: state.ProjectRevision, Preconditions: preconditions,
		Operation: operation, OutputDigest: *operation.RoughCutOutputDigest, ActivityCursor: cursor,
	}
	if err := tx.Commit(); err != nil {
		return application.RoughCutDerivationPreview{}, err
	}
	return result, nil
}

func loadRoughCutPreviewLane(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	excerpt domain.SourceExcerptState,
	input application.RoughCutDerivationPreviewLaneInput,
	kind domain.TrackType,
) (application.RoughCutLaneBindingInput, error) {
	if err := ensureTrackState(ctx, tx, state, input.TrackID); err != nil {
		return application.RoughCutLaneBindingInput{}, err
	}
	track := state.Tracks[input.TrackID.String()]
	if track.Revision != input.TrackRevision || track.SequenceID != state.SequenceID || track.Type != kind {
		return application.RoughCutLaneBindingInput{}, application.ErrEditConflict
	}
	if err := ensureSourceStreamState(ctx, tx, state, excerpt.AssetID, input.SourceStreamID); err != nil {
		return application.RoughCutLaneBindingInput{}, err
	}
	return application.RoughCutLaneBindingInput{
		TrackID: track.ID, SourceStreamID: input.SourceStreamID,
	}, nil
}

func preconditionKey(kind domain.EditEntityKind, id string) string {
	return string(kind) + "\x00" + id
}
