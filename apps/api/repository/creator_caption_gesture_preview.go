package repository

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const creatorCaptionGestureSchema = "v1"

func (repository *SQLiteProjects) ReadCreatorCaptionGesturePreview(
	ctx context.Context,
	query application.CreatorCaptionGesturePreviewQuery,
) (application.CreatorCaptionGesturePreview, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	defer tx.Rollback()
	operation, err := creatorCaptionGestureOperation(query.Input)
	if err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	initialConditions := []domain.EntityPrecondition{{
		Kind: domain.EntityTrack, ID: query.Input.TrackID.String(), Revision: query.Input.TrackRevision,
	}}
	if query.Input.CaptionID != nil && query.Input.CaptionRevision != nil {
		initialConditions = append(initialConditions, domain.EntityPrecondition{
			Kind: domain.EntityCaption, ID: query.Input.CaptionID.String(), Revision: *query.Input.CaptionRevision,
		})
	}
	requestID, _ := domain.ParseRequestID("ui:caption-gesture-preview")
	state, err := loadEditNormalizationState(ctx, tx, application.ProposeEditRecord{
		ProjectID: query.ProjectID, SequenceID: query.SequenceID, Actor: query.Actor,
		Input: application.EditProposeInput{
			RequestID: requestID, Intent: "Preview Creator Caption gesture", BaseProjectRevision: 1,
			Preconditions: initialConditions, Operations: []application.EditOperationInput{operation},
		},
	})
	if err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	track := state.Tracks[query.Input.TrackID.String()]
	if track.ID.IsZero() || track.SequenceID != query.SequenceID || track.Type != domain.TrackCaption ||
		track.Revision != query.Input.TrackRevision {
		return application.CreatorCaptionGesturePreview{}, application.ErrEditConflict
	}
	var current domain.CaptionState
	var alignmentIDs []domain.AlignmentID
	if query.Input.CaptionID != nil {
		current = state.Captions[query.Input.CaptionID.String()]
		if current.ID.IsZero() || current.Tombstoned || current.Revision != *query.Input.CaptionRevision ||
			current.TrackID != track.ID || current.SequenceID != query.SequenceID {
			return application.CreatorCaptionGesturePreview{}, application.ErrEditConflict
		}
		alignmentIDs = append([]domain.AlignmentID(nil), state.CaptionAlignments[current.ID.String()]...)
		if len(alignmentIDs) > 511 {
			return application.CreatorCaptionGesturePreview{}, application.ErrEditInvalid
		}
	}
	alignmentOperations, alignmentEffects, err := creatorCaptionAlignmentOperations(
		ctx, tx, &state, alignmentIDs, query.Input,
	)
	if err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	operations := append([]application.EditOperationInput{operation}, alignmentOperations...)
	preconditions, err := creatorCaptionGesturePreconditions(state, current, alignmentIDs, query.Input)
	if err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	input := application.EditProposeInput{
		RequestID: requestID, Intent: "Preview Creator Caption gesture",
		BaseProjectRevision: state.ProjectRevision, Preconditions: preconditions, Operations: operations,
	}
	if err := validateCreatorCaptionGesturePlan(query, state, input, creatorCaptionGestureAllocation(query.Input)); err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	subject := creatorCaptionGestureSubject(current, query.Input)
	_, outputDigest, err := domain.CanonicalDigest("open-cut/creator-caption-gesture", creatorCaptionGestureSchema, struct {
		BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
		Preconditions       []domain.EntityPrecondition      `json:"preconditions"`
		Operations          []application.EditOperationInput `json:"operations"`
	}{state.ProjectRevision, preconditions, operations})
	if err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	cursor, err := loadActivityHead(ctx, tx, "project", query.ProjectID.String())
	if err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	result := application.CreatorCaptionGesturePreview{
		BaseProjectRevision: state.ProjectRevision, Preconditions: preconditions, Operations: operations,
		Kind: query.Input.Kind, Subject: subject, AlignmentEffects: alignmentEffects,
		OutputDigest: outputDigest, ActivityCursor: cursor,
	}
	if err := tx.Commit(); err != nil {
		return application.CreatorCaptionGesturePreview{}, err
	}
	return result, nil
}

func creatorCaptionGestureOperation(
	input application.CreatorCaptionGesturePreviewInput,
) (application.EditOperationInput, error) {
	switch input.Kind {
	case application.CreatorCaptionCreate:
		return application.EditOperationInput{
			Type: domain.EditAddCaption, CreateAs: input.CaptionAs, TrackID: &input.TrackID,
			Range: input.Range, Language: input.Language, Text: input.Text,
		}, nil
	case application.CreatorCaptionUpdate:
		return application.EditOperationInput{
			Type: domain.EditUpdateCaption, CaptionID: input.CaptionID,
			Range: input.Range, Language: input.Language, Text: input.Text,
		}, nil
	case application.CreatorCaptionRemove:
		return application.EditOperationInput{Type: domain.EditRemoveCaption, CaptionID: input.CaptionID}, nil
	default:
		return application.EditOperationInput{}, application.ErrEditInvalid
	}
}

func creatorCaptionAlignmentOperations(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	alignmentIDs []domain.AlignmentID,
	input application.CreatorCaptionGesturePreviewInput,
) ([]application.EditOperationInput, []application.CreatorCaptionAlignmentEffect, error) {
	if len(alignmentIDs) == 0 {
		return nil, []application.CreatorCaptionAlignmentEffect{}, nil
	}
	if input.AlignmentHandling == nil {
		return nil, nil, application.ErrEditInvalid
	}
	operations := make([]application.EditOperationInput, 0, len(alignmentIDs))
	effects := make([]application.CreatorCaptionAlignmentEffect, 0, len(alignmentIDs))
	for _, alignmentID := range alignmentIDs {
		alignment := state.Alignments[alignmentID.String()]
		operation := application.EditOperationInput{AlignmentID: &alignmentID}
		switch *input.AlignmentHandling {
		case application.CreatorCaptionStaleAlignment:
			operation.Type = domain.EditMarkAlignmentStale
		case application.CreatorCaptionUnbindAlignment:
			operation.Type = domain.EditUnbindAlignment
		case application.CreatorCaptionPreserveAlignment:
			operation.Type = domain.EditRemapAlignment
			for _, target := range alignment.Targets {
				if target.Type != domain.AlignmentTargetCaption || target.Caption == nil {
					return nil, nil, application.ErrEditInvalid
				}
				if err := ensureCaptionState(ctx, tx, state, target.Caption.CaptionID); err != nil {
					return nil, nil, err
				}
				localRange := target.Caption.LocalRange
				operation.AlignmentTargets = append(operation.AlignmentTargets, application.AlignmentTargetInput{
					Type:    domain.AlignmentTargetCaption,
					Caption: &application.EditReference{ID: target.Caption.CaptionID.String()}, LocalRange: &localRange,
				})
			}
		default:
			return nil, nil, application.ErrEditInvalid
		}
		operations = append(operations, operation)
		effects = append(effects, application.CreatorCaptionAlignmentEffect{
			AlignmentID: alignment.ID, Revision: alignment.Revision,
			Handling: *input.AlignmentHandling, TargetCount: len(alignment.Targets),
		})
	}
	return operations, effects, nil
}

func creatorCaptionGesturePreconditions(
	state application.EditNormalizationState,
	current domain.CaptionState,
	alignmentIDs []domain.AlignmentID,
	input application.CreatorCaptionGesturePreviewInput,
) ([]domain.EntityPrecondition, error) {
	conditions := map[string]domain.EntityPrecondition{
		preconditionKey(domain.EntitySequence, state.SequenceID.String()): {
			Kind: domain.EntitySequence, ID: state.SequenceID.String(), Revision: state.SequenceRevision,
		},
		preconditionKey(domain.EntityTrack, input.TrackID.String()): {
			Kind: domain.EntityTrack, ID: input.TrackID.String(), Revision: input.TrackRevision,
		},
	}
	if !current.ID.IsZero() {
		conditions[preconditionKey(domain.EntityCaption, current.ID.String())] = domain.EntityPrecondition{
			Kind: domain.EntityCaption, ID: current.ID.String(), Revision: current.Revision,
		}
	}
	for _, alignmentID := range alignmentIDs {
		alignment := state.Alignments[alignmentID.String()]
		conditions[preconditionKey(domain.EntityAlignment, alignment.ID.String())] = domain.EntityPrecondition{
			Kind: domain.EntityAlignment, ID: alignment.ID.String(), Revision: alignment.Revision,
		}
		if input.AlignmentHandling == nil || *input.AlignmentHandling != application.CreatorCaptionPreserveAlignment {
			continue
		}
		for _, target := range alignment.Targets {
			if target.Caption == nil {
				return nil, application.ErrEditInvalid
			}
			caption := state.Captions[target.Caption.CaptionID.String()]
			if caption.ID.IsZero() || caption.Tombstoned {
				return nil, application.ErrEditInvalid
			}
			conditions[preconditionKey(domain.EntityCaption, caption.ID.String())] = domain.EntityPrecondition{
				Kind: domain.EntityCaption, ID: caption.ID.String(), Revision: caption.Revision,
			}
		}
	}
	if len(conditions) > 2048 {
		return nil, application.ErrEditInvalid
	}
	result := make([]domain.EntityPrecondition, 0, len(conditions))
	for _, condition := range conditions {
		result = append(result, condition)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		return result[left].ID < result[right].ID
	})
	return result, nil
}

func creatorCaptionGestureSubject(
	current domain.CaptionState,
	input application.CreatorCaptionGesturePreviewInput,
) application.CreatorCaptionGestureSubject {
	if input.Kind == application.CreatorCaptionCreate {
		return application.CreatorCaptionGestureSubject{
			CaptionAs: input.CaptionAs, TrackID: input.TrackID, Range: *input.Range,
			Language: *input.Language, Text: *input.Text, Provenance: domain.CaptionProvenanceManual,
		}
	}
	captionID := current.ID
	if input.Kind == application.CreatorCaptionUpdate {
		return application.CreatorCaptionGestureSubject{
			CaptionID: &captionID, TrackID: current.TrackID, Range: *input.Range,
			Language: *input.Language, Text: *input.Text, Provenance: current.Provenance.Kind,
		}
	}
	return application.CreatorCaptionGestureSubject{
		CaptionID: &captionID, TrackID: current.TrackID, Range: current.Range,
		Language: current.Language, Text: current.Text, Provenance: current.Provenance.Kind,
	}
}

func creatorCaptionGestureAllocation(
	input application.CreatorCaptionGesturePreviewInput,
) []domain.LocalAllocation {
	if input.Kind != application.CreatorCaptionCreate || input.CaptionAs == nil {
		return nil
	}
	return []domain.LocalAllocation{{
		Local: *input.CaptionAs, Kind: domain.EntityCaption,
		ID: "018f0000-0000-7000-8000-000000000001",
	}}
}

func validateCreatorCaptionGesturePlan(
	query application.CreatorCaptionGesturePreviewQuery,
	state application.EditNormalizationState,
	input application.EditProposeInput,
	allocation []domain.LocalAllocation,
) error {
	proposalID, _ := domain.ParseProposalID("018f0000-0000-7000-8000-000000000100")
	_, _, err := application.NormalizeEditProposal(application.NormalizeEditInput{
		ProposalID: proposalID, ProjectID: query.ProjectID, SequenceID: query.SequenceID,
		Actor: query.Actor, Allocation: allocation, Input: input, CreatedAt: time.Unix(0, 0).UTC(), State: state,
	})
	return err
}
