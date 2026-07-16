package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const creatorClipPlacementSchema = "v1"

func (repository *SQLiteProjects) ReadCreatorClipPlacementPreview(
	ctx context.Context,
	query application.CreatorClipPlacementPreviewQuery,
) (application.CreatorClipPlacementPreview, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.CreatorClipPlacementPreview{}, err
	}
	defer tx.Rollback()
	state, err := loadEditHeads(ctx, tx, query.ProjectID, query.SequenceID)
	if err != nil {
		return application.CreatorClipPlacementPreview{}, err
	}
	if err := validateCreatorPlacementAsset(ctx, tx, query); err != nil {
		return application.CreatorClipPlacementPreview{}, err
	}
	timelineRange, err := domain.NewTimeRange(query.Input.TimelineStart, query.Input.SourceRange.Duration)
	if err != nil {
		return application.CreatorClipPlacementPreview{}, application.ErrEditInvalid
	}
	operations, lanes, allocations, err := buildCreatorPlacementOperations(
		ctx, tx, &state, query, timelineRange,
	)
	if err != nil {
		return application.CreatorClipPlacementPreview{}, err
	}
	conditions := creatorPlacementPreconditions(state, query.Input)
	requestID, _ := domain.ParseRequestID("ui:clip-placement-preview")
	input := application.EditProposeInput{
		RequestID: requestID, Intent: "Preview Creator source placement",
		BaseProjectRevision: state.ProjectRevision, Preconditions: conditions, Operations: operations,
	}
	proposalID, _ := domain.ParseProposalID("018f0000-0000-7000-8000-000000000100")
	if _, _, err := application.NormalizeEditProposal(application.NormalizeEditInput{
		ProposalID: proposalID, ProjectID: query.ProjectID, SequenceID: query.SequenceID,
		Actor: query.Actor, Allocation: allocations, Input: input,
		CreatedAt: time.Unix(0, 0).UTC(), State: state,
	}); err != nil {
		return application.CreatorClipPlacementPreview{}, err
	}
	_, outputDigest, err := domain.CanonicalDigest(
		"open-cut/creator-clip-placement", creatorClipPlacementSchema,
		struct {
			BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
			Preconditions       []domain.EntityPrecondition      `json:"preconditions"`
			Operations          []application.EditOperationInput `json:"operations"`
		}{state.ProjectRevision, conditions, operations},
	)
	if err != nil {
		return application.CreatorClipPlacementPreview{}, err
	}
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.CreatorClipPlacementPreview{}, err
	}
	result := application.CreatorClipPlacementPreview{
		BaseProjectRevision: state.ProjectRevision, Preconditions: conditions, Operations: operations,
		AssetID: query.Input.AssetID, AssetRevision: query.Input.AssetRevision,
		AcceptedFingerprint: query.Input.AcceptedFingerprint, SourceRange: query.Input.SourceRange,
		TimelineRange: timelineRange, Lanes: lanes, Linked: len(lanes) == 2,
		OutputDigest: outputDigest, ActivityCursor: cursor,
	}
	if err := tx.Commit(); err != nil {
		return application.CreatorClipPlacementPreview{}, err
	}
	return result, nil
}

func validateCreatorPlacementAsset(
	ctx context.Context,
	tx *sql.Tx,
	query application.CreatorClipPlacementPreviewQuery,
) error {
	var revision uint64
	var accepted, availability, observed string
	err := tx.QueryRowContext(ctx, `
SELECT asset.revision, asset.accepted_fingerprint, media.availability, media.observed_fingerprint
FROM assets asset
JOIN asset_media_state media ON media.asset_id = asset.id
WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0
  AND asset.accepted_fingerprint IS NOT NULL AND media.observed_fingerprint IS NOT NULL`,
		query.Input.AssetID.String(), query.ProjectID.String(),
	).Scan(&revision, &accepted, &availability, &observed)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditConflict
	}
	if err != nil {
		return err
	}
	if revision != query.Input.AssetRevision.Value() || accepted != query.Input.AcceptedFingerprint.String() {
		return application.ErrEditConflict
	}
	if observed != accepted ||
		(availability != string(domain.AssetOnline) && availability != string(domain.AssetManagedState)) {
		return application.ErrEditInvalid
	}
	return nil
}

func buildCreatorPlacementOperations(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	query application.CreatorClipPlacementPreviewQuery,
	timelineRange domain.TimeRange,
) ([]application.EditOperationInput, []application.CreatorClipPlacementLane, []domain.LocalAllocation, error) {
	type laneSpec struct {
		kind  domain.TrackType
		input *application.CreatorClipPlacementLaneInput
	}
	specs := []laneSpec{{kind: domain.TrackVideo, input: query.Input.Video}, {kind: domain.TrackAudio, input: query.Input.Audio}}
	operations := make([]application.EditOperationInput, 0, 2)
	lanes := make([]application.CreatorClipPlacementLane, 0, 2)
	allocations := make([]domain.LocalAllocation, 0, 3)
	linked := query.Input.Video != nil && query.Input.Audio != nil
	var groupLocal domain.LocalID
	var err error
	if linked {
		groupLocal, err = creatorPlacementLocal(query.Input.LocalPrefix, "group")
		if err != nil {
			return nil, nil, nil, err
		}
		allocations = append(allocations, domain.LocalAllocation{
			Local: groupLocal, Kind: domain.EntityLinkGroup,
			ID: "018f0000-0000-7000-8000-000000000003",
		})
	}
	enabled := true
	for _, spec := range specs {
		if spec.input == nil {
			continue
		}
		if err := ensureTrackState(ctx, tx, state, spec.input.TrackID); err != nil {
			return nil, nil, nil, err
		}
		track := state.Tracks[spec.input.TrackID.String()]
		if track.ID.IsZero() || track.SequenceID != query.SequenceID || track.Type != spec.kind ||
			track.Revision != spec.input.TrackRevision {
			return nil, nil, nil, application.ErrEditConflict
		}
		if err := ensureSourceStreamState(ctx, tx, state, query.Input.AssetID, spec.input.SourceStreamID); err != nil {
			if errors.Is(err, application.ErrEditInvalid) {
				return nil, nil, nil, application.ErrEditConflict
			}
			return nil, nil, nil, err
		}
		stream := state.SourceStreams[spec.input.SourceStreamID.String()]
		if stream.AssetRevision != query.Input.AssetRevision ||
			(spec.kind == domain.TrackVideo && stream.Descriptor.MediaType != domain.MediaVideo) ||
			(spec.kind == domain.TrackAudio && stream.Descriptor.MediaType != domain.MediaAudio) {
			return nil, nil, nil, application.ErrEditConflict
		}
		if err := loadClipOverlaps(ctx, tx, state, query.SequenceID, track.ID, timelineRange); err != nil {
			return nil, nil, nil, err
		}
		clipLocal, err := creatorPlacementLocal(query.Input.LocalPrefix, string(spec.kind))
		if err != nil {
			return nil, nil, nil, err
		}
		operation := application.EditOperationInput{
			Type: domain.EditAddClip, CreateAs: &clipLocal, TrackID: &track.ID,
			AssetID: &query.Input.AssetID, SourceStreamID: &stream.ID,
			SourceRange: &query.Input.SourceRange, TimelineRange: &timelineRange, Enabled: &enabled,
		}
		if linked && len(operations) == 0 {
			operation.CreateLinkGroupAs = &groupLocal
		} else if linked {
			operation.LinkGroup = &application.EditReference{Local: &groupLocal}
		}
		operations = append(operations, operation)
		lanes = append(lanes, application.CreatorClipPlacementLane{
			Type: spec.kind, TrackID: track.ID, SourceStreamID: stream.ID,
		})
		allocationID := fmt.Sprintf("018f0000-0000-7000-8000-%012d", len(operations))
		allocations = append(allocations, domain.LocalAllocation{
			Local: clipLocal, Kind: domain.EntityClip, ID: allocationID,
		})
	}
	if len(state.Clips) != 0 {
		return nil, nil, nil, application.ErrEditInvalid
	}
	return operations, lanes, allocations, nil
}

func creatorPlacementLocal(prefix domain.LocalID, suffix string) (domain.LocalID, error) {
	value, err := domain.ParseLocalID(prefix.String() + "_" + suffix)
	if err != nil {
		return "", application.ErrEditInvalid
	}
	return value, nil
}

func creatorPlacementPreconditions(
	state application.EditNormalizationState,
	input application.CreatorClipPlacementPreviewInput,
) []domain.EntityPrecondition {
	conditions := []domain.EntityPrecondition{
		{Kind: domain.EntitySequence, ID: state.SequenceID.String(), Revision: state.SequenceRevision},
		{Kind: domain.EntityAsset, ID: input.AssetID.String(), Revision: input.AssetRevision},
	}
	for _, lane := range []*application.CreatorClipPlacementLaneInput{input.Video, input.Audio} {
		if lane != nil {
			conditions = append(conditions, domain.EntityPrecondition{
				Kind: domain.EntityTrack, ID: lane.TrackID.String(), Revision: lane.TrackRevision,
			})
		}
	}
	sort.Slice(conditions, func(left, right int) bool {
		if conditions[left].Kind != conditions[right].Kind {
			return conditions[left].Kind < conditions[right].Kind
		}
		return conditions[left].ID < conditions[right].ID
	})
	return conditions
}
