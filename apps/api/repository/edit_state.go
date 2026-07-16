package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func loadEditNormalizationState(
	ctx context.Context,
	tx *sql.Tx,
	record application.ProposeEditRecord,
) (application.EditNormalizationState, error) {
	if err := validateEditNormalizationWriter(
		ctx, tx, record.ProjectID, record.SequenceID, record.RunID, record.TurnID, record.Actor,
	); err != nil {
		return application.EditNormalizationState{}, err
	}
	state, err := loadEditHeads(ctx, tx, record.ProjectID, record.SequenceID)
	if err != nil {
		return application.EditNormalizationState{}, err
	}
	for _, precondition := range record.Input.Preconditions {
		revision, err := loadEditEntityRevision(ctx, tx, record.ProjectID, precondition.Kind, precondition.ID)
		if err != nil {
			return application.EditNormalizationState{}, err
		}
		if revision != precondition.Revision {
			return application.EditNormalizationState{}, application.ErrEditConflict
		}
	}
	mutatedNodes := make(map[string]struct{})
	mutatedCaptions := make(map[string]struct{})
	mutatedClips := make(map[string]struct{})
	for _, operation := range record.Input.Operations {
		switch operation.Type {
		case domain.EditInsertSection, domain.EditInsertAuthoredText,
			domain.EditInsertVisualIntent, domain.EditInsertNote:
			if err := ensureSectionState(ctx, tx, &state, *operation.ParentID); err != nil {
				return state, err
			}
			if operation.After != nil && operation.After.ID != "" {
				id, _ := domain.ParseNarrativeNodeID(operation.After.ID)
				if err := ensureNarrativeNodeState(ctx, tx, &state, id); err != nil {
					return state, err
				}
			}
		case domain.EditUpdateSection, domain.EditUpdateAuthoredText,
			domain.EditUpdateVisualIntent, domain.EditUpdateNote:
			if err := ensureNarrativeNodeState(ctx, tx, &state, *operation.NodeID); err != nil {
				return state, err
			}
			mutatedNodes[operation.NodeID.String()] = struct{}{}
		case domain.EditMoveNarrativeNode:
			if err := ensureNarrativeNodeState(ctx, tx, &state, *operation.NodeID); err != nil {
				return state, err
			}
			parent := normalizationNarrativeParent(state, *operation.NodeID)
			if parent == nil {
				return state, application.ErrEditInvalid
			}
			if err := ensureSectionState(ctx, tx, &state, *parent); err != nil {
				return state, err
			}
			if err := ensureSectionState(ctx, tx, &state, *operation.ParentID); err != nil {
				return state, err
			}
			if operation.After != nil && operation.After.ID != "" {
				id, _ := domain.ParseNarrativeNodeID(operation.After.ID)
				if err := ensureNarrativeNodeState(ctx, tx, &state, id); err != nil {
					return state, err
				}
			}
			mutatedNodes[operation.NodeID.String()] = struct{}{}
		case domain.EditRemoveNarrativeNode:
			if err := ensureNarrativeNodeState(ctx, tx, &state, *operation.NodeID); err != nil {
				return state, err
			}
			parent := normalizationNarrativeParent(state, *operation.NodeID)
			if parent == nil {
				return state, application.ErrEditInvalid
			}
			if err := ensureSectionState(ctx, tx, &state, *parent); err != nil {
				return state, err
			}
			mutatedNodes[operation.NodeID.String()] = struct{}{}
		case domain.EditAddCaption:
			if err := ensureTrackState(ctx, tx, &state, *operation.TrackID); err != nil {
				return state, err
			}
			if err := loadCaptionOverlaps(ctx, tx, &state, record.SequenceID, *operation.TrackID, *operation.Range, nil); err != nil {
				return state, err
			}
		case domain.EditUpdateCaption:
			if err := ensureCaptionState(ctx, tx, &state, *operation.CaptionID); err != nil {
				return state, err
			}
			current := state.Captions[operation.CaptionID.String()]
			if err := ensureTrackState(ctx, tx, &state, current.TrackID); err != nil {
				return state, err
			}
			if err := loadCaptionOverlaps(ctx, tx, &state, record.SequenceID, current.TrackID, *operation.Range, &current.ID); err != nil {
				return state, err
			}
			mutatedCaptions[current.ID.String()] = struct{}{}
		case domain.EditRemoveCaption:
			if err := ensureCaptionState(ctx, tx, &state, *operation.CaptionID); err != nil {
				return state, err
			}
			current := state.Captions[operation.CaptionID.String()]
			if err := ensureTrackState(ctx, tx, &state, current.TrackID); err != nil {
				return state, err
			}
			mutatedCaptions[current.ID.String()] = struct{}{}
		case domain.EditBindAlignment:
			if operation.NarrativeNode.ID != "" {
				id, _ := domain.ParseNarrativeNodeID(operation.NarrativeNode.ID)
				if err := ensureNarrativeNodeState(ctx, tx, &state, id); err != nil {
					return state, err
				}
			}
			for _, target := range operation.AlignmentTargets {
				if target.Caption != nil && target.Caption.ID != "" {
					id, _ := domain.ParseCaptionID(target.Caption.ID)
					if err := ensureCaptionState(ctx, tx, &state, id); err != nil {
						return state, err
					}
				}
				if target.Clip != nil && target.Clip.ID != "" {
					id, _ := domain.ParseClipID(target.Clip.ID)
					if err := ensureClipState(ctx, tx, &state, id); err != nil {
						return state, err
					}
				}
			}
		case domain.EditRemapAlignment:
			if err := loadAlignmentRemapInput(ctx, tx, &state, operation); err != nil {
				return state, err
			}
		case domain.EditMarkAlignmentStale, domain.EditUnbindAlignment:
			if err := ensureAlignmentState(ctx, tx, &state, *operation.AlignmentID); err != nil {
				return state, err
			}
		case domain.EditAddClip:
			if err := ensureTrackState(ctx, tx, &state, *operation.TrackID); err != nil {
				return state, err
			}
			if err := ensureSourceStreamState(ctx, tx, &state, *operation.AssetID, *operation.SourceStreamID); err != nil {
				return state, err
			}
			if err := loadClipOverlaps(ctx, tx, &state, record.SequenceID, *operation.TrackID, *operation.TimelineRange); err != nil {
				return state, err
			}
			if operation.LinkGroup != nil && operation.LinkGroup.ID != "" {
				id, _ := domain.ParseLinkGroupID(operation.LinkGroup.ID)
				if err := loadLinkGroupMembers(ctx, tx, &state, id); err != nil {
					return state, err
				}
			}
		case domain.EditMoveClip, domain.EditTrimClip, domain.EditSplitClip, domain.EditRemoveClip:
			if err := loadClipMutationInput(ctx, tx, &state, operation, mutatedClips); err != nil {
				return state, err
			}
		case domain.EditLinkClips:
			if err := loadLinkClipsInput(ctx, tx, &state, operation, mutatedClips); err != nil {
				return state, err
			}
		case domain.EditUnlinkClips:
			if err := loadUnlinkClipsInput(ctx, tx, &state, operation, mutatedClips); err != nil {
				return state, err
			}
		case domain.EditAddTranscriptCorrection:
			if err := ensureEditTranscriptArtifact(
				ctx, tx, &state, *operation.TranscriptArtifactID, operation.TranscriptSegmentIDs,
			); err != nil {
				return state, err
			}
			if err := loadTranscriptCorrectionOverlaps(
				ctx, tx, &state, *operation.TranscriptArtifactID, *operation.Language, *operation.SourceRange,
			); err != nil {
				return state, err
			}
		case domain.EditUpdateTranscriptCorrection:
			if err := ensureTranscriptCorrectionState(ctx, tx, &state, *operation.TranscriptCorrectionID); err != nil {
				return state, err
			}
			current := state.TranscriptCorrections[operation.TranscriptCorrectionID.String()]
			if err := loadTranscriptCorrectionOverlaps(
				ctx, tx, &state, current.ArtifactID, *operation.Language, current.SourceRange,
			); err != nil {
				return state, err
			}
		case domain.EditRemoveTranscriptCorrection:
			if err := ensureTranscriptCorrectionState(ctx, tx, &state, *operation.TranscriptCorrectionID); err != nil {
				return state, err
			}
		case domain.EditInsertSourceExcerpt:
			if err := ensureSectionState(ctx, tx, &state, *operation.ParentID); err != nil {
				return state, err
			}
			if operation.After != nil && operation.After.ID != "" {
				id, _ := domain.ParseNarrativeNodeID(operation.After.ID)
				if err := ensureNarrativeNodeState(ctx, tx, &state, id); err != nil {
					return state, err
				}
			}
			if err := ensureEditTranscriptArtifact(
				ctx, tx, &state, *operation.TranscriptArtifactID, operation.TranscriptSegmentIDs,
			); err != nil {
				return state, err
			}
			if err := loadTranscriptCorrectionOverlaps(
				ctx, tx, &state, *operation.TranscriptArtifactID, *operation.Language, *operation.SourceRange,
			); err != nil {
				return state, err
			}
		case domain.EditDeriveCaptions:
			nodeID, parseErr := domain.ParseNarrativeNodeID(operation.NarrativeNode.ID)
			if parseErr != nil {
				return state, application.ErrEditInvalid
			}
			if err := ensureSourceExcerptState(ctx, tx, &state, nodeID); err != nil {
				return state, err
			}
			excerpt := state.SourceExcerpts[nodeID.String()]
			if err := ensureEditTranscriptArtifact(
				ctx, tx, &state, excerpt.Evidence.ArtifactID, excerpt.Evidence.SegmentIDs,
			); err != nil {
				return state, err
			}
			if err := loadTranscriptCorrectionOverlaps(
				ctx, tx, &state, excerpt.Evidence.ArtifactID, excerpt.Language, excerpt.SourceRange,
			); err != nil {
				return state, err
			}
			if err := ensureSourceStreamState(
				ctx, tx, &state, excerpt.AssetID, excerpt.Evidence.SourceStreamID,
			); err != nil {
				return state, err
			}
			clipID, parseErr := domain.ParseClipID(operation.Clip.ID)
			if parseErr != nil {
				return state, application.ErrEditInvalid
			}
			if err := ensureClipState(ctx, tx, &state, clipID); err != nil {
				return state, err
			}
			if err := ensureTrackState(ctx, tx, &state, *operation.TrackID); err != nil {
				return state, err
			}
			for _, output := range operation.DerivedCaptions {
				if err := loadCaptionOverlaps(
					ctx, tx, &state, record.SequenceID, *operation.TrackID, output.TimelineRange, nil,
				); err != nil {
					return state, err
				}
			}
		case domain.EditDeriveRoughCut:
			for _, item := range operation.RoughCutItems {
				if err := ensureSourceExcerptState(ctx, tx, &state, item.SourceExcerptID); err != nil {
					return state, err
				}
				excerpt := state.SourceExcerpts[item.SourceExcerptID.String()]
				if err := ensureSourceExcerptEvidenceStatus(ctx, tx, &state, excerpt); err != nil {
					return state, err
				}
				for _, lane := range []*application.RoughCutLaneBindingInput{item.Video, item.Audio} {
					if lane == nil {
						continue
					}
					if err := ensureTrackState(ctx, tx, &state, lane.TrackID); err != nil {
						return state, err
					}
					if err := ensureSourceStreamState(
						ctx, tx, &state, excerpt.AssetID, lane.SourceStreamID,
					); err != nil {
						return state, err
					}
				}
			}
			for _, output := range operation.DerivedRoughCut {
				for _, lane := range []*application.DerivedRoughCutLaneOutputInput{output.Video, output.Audio} {
					if lane == nil {
						continue
					}
					if err := loadClipOverlaps(
						ctx, tx, &state, record.SequenceID, lane.TrackID, output.TimelineRange,
					); err != nil {
						return state, err
					}
				}
			}
		}
	}
	for id := range mutatedNodes {
		if err := loadExactNodeAlignments(ctx, tx, &state, id); err != nil {
			return state, err
		}
	}
	for id := range mutatedCaptions {
		if err := loadExactCaptionAlignments(ctx, tx, &state, id); err != nil {
			return state, err
		}
	}
	for id := range mutatedClips {
		if err := loadExactClipAlignments(ctx, tx, &state, id); err != nil {
			return state, err
		}
	}
	return state, nil
}

func validateEditNormalizationWriter(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	actor domain.ActorRef,
) error {
	if actor.Kind == domain.ActorAgent {
		return validateEditWriter(ctx, tx, projectID, sequenceID, runID, turnID, actor)
	}
	if actor.Kind != domain.ActorCreator || actor.Validate() != nil || !runID.IsZero() || !turnID.IsZero() {
		return application.ErrEditInvalid
	}
	return validateCreatorEditSequence(ctx, tx, projectID, sequenceID)
}

func validateCreatorEditSequence(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
) error {
	if projectID.IsZero() || sequenceID.IsZero() {
		return application.ErrEditInvalid
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sequences WHERE id = ? AND project_id = ?`,
		sequenceID.String(), projectID.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrEditInvalid
		}
		return err
	}
	return nil
}

func loadEditHeads(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
) (application.EditNormalizationState, error) {
	var projectValue, status, documentValue, sequenceValue string
	var projectRevision, documentRevision, sequenceRevision uint64
	err := tx.QueryRowContext(ctx, `
SELECT p.id, p.revision, p.status, d.id, d.revision, s.id, s.revision
FROM projects p
JOIN narrative_documents d ON d.id = p.narrative_document_id
JOIN sequences s ON s.id = ? AND s.project_id = p.id
WHERE p.id = ?`, sequenceID.String(), projectID.String()).Scan(
		&projectValue, &projectRevision, &status, &documentValue, &documentRevision, &sequenceValue, &sequenceRevision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.EditNormalizationState{}, application.ErrProjectNotFound
	}
	if err != nil {
		return application.EditNormalizationState{}, err
	}
	if status != string(domain.ProjectActive) {
		return application.EditNormalizationState{}, application.ErrProjectNotActive
	}
	parsedProject, _ := domain.ParseProjectID(projectValue)
	parsedDocument, _ := domain.ParseNarrativeDocumentID(documentValue)
	parsedSequence, _ := domain.ParseSequenceID(sequenceValue)
	projectRev, err := domain.NewRevision(projectRevision)
	if err != nil {
		return application.EditNormalizationState{}, err
	}
	documentRev, err := domain.NewRevision(documentRevision)
	if err != nil {
		return application.EditNormalizationState{}, err
	}
	sequenceRev, err := domain.NewRevision(sequenceRevision)
	if err != nil {
		return application.EditNormalizationState{}, err
	}
	return application.EditNormalizationState{
		ProjectID: parsedProject, ProjectRevision: projectRev,
		DocumentID: parsedDocument, DocumentRevision: documentRev,
		SequenceID: parsedSequence, SequenceRevision: sequenceRev,
		Sections:              make(map[string]domain.NarrativeSectionState),
		Tracks:                make(map[string]application.EditTrackState),
		AuthoredTexts:         make(map[string]domain.AuthoredTextState),
		SourceExcerpts:        make(map[string]domain.SourceExcerptState),
		VisualIntents:         make(map[string]domain.VisualIntentState),
		Notes:                 make(map[string]domain.NoteState),
		SectionChildCounts:    make(map[string]int),
		SourceExcerptEvidence: make(map[string]domain.SourceExcerptEvidenceStatus),
		TranscriptCorrections: make(map[string]domain.TranscriptCorrectionState),
		TranscriptArtifacts:   make(map[string]application.EditTranscriptArtifactState),
		Captions:              make(map[string]domain.CaptionState),
		Clips:                 make(map[string]domain.ClipState),
		LinkGroups:            make(map[string]domain.LinkGroupState),
		LinkGroupClips:        make(map[string][]domain.ClipID),
		SourceStreams:         make(map[string]application.EditSourceStreamState),
		Alignments:            make(map[string]domain.AlignmentState),
		NodeAlignments:        make(map[string][]domain.AlignmentID),
		CaptionAlignments:     make(map[string][]domain.AlignmentID),
		ClipAlignments:        make(map[string][]domain.AlignmentID),
	}, nil
}

func validateEditWriter(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	actor domain.ActorRef,
) error {
	if projectID.IsZero() || sequenceID.IsZero() || runID.IsZero() || turnID.IsZero() ||
		actor.Validate() != nil || actor.Kind != domain.ActorAgent {
		return application.ErrEditInvalid
	}
	run, err := loadAgentRun(ctx, tx, projectID, runID)
	if err != nil {
		return err
	}
	if run.Actor.IDString() != actor.IDString() {
		return application.ErrEditConflict
	}
	if run.Status != application.AgentRunActive || run.CurrentTurn.ID != turnID ||
		run.CurrentTurn.Status != application.AgentTurnActive {
		return application.ErrEditStaleTurn
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sequences WHERE id = ? AND project_id = ?`,
		sequenceID.String(), projectID.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrEditInvalid
		}
		return err
	}
	return nil
}
