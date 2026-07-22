package repository

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	projectVersionStateSchema       = "open-cut/project-version-state/v1"
	maximumProjectVersionStateBytes = 64 * 1024 * 1024
)

type projectVersionTrack struct {
	ID       domain.TrackID   `json:"id"`
	Revision domain.Revision  `json:"revision"`
	Type     domain.TrackType `json:"type"`
	Label    string           `json:"label"`
	OrderKey string           `json:"orderKey"`
}

type projectVersionState struct {
	ProjectID                 domain.ProjectID                   `json:"projectId"`
	ProjectRevision           domain.Revision                    `json:"projectRevision"`
	NarrativeDocumentID       domain.NarrativeDocumentID         `json:"narrativeDocumentId"`
	NarrativeDocumentRevision domain.Revision                    `json:"narrativeDocumentRevision"`
	SequenceID                domain.SequenceID                  `json:"sequenceId"`
	SequenceRevision          domain.Revision                    `json:"sequenceRevision"`
	Format                    domain.SequenceFormat              `json:"format"`
	Tracks                    []projectVersionTrack              `json:"tracks"`
	NarrativeNodes            []domain.NarrativeNodeState        `json:"narrativeNodes"`
	TranscriptCorrections     []domain.TranscriptCorrectionState `json:"transcriptCorrections"`
	Assets                    []domain.AssetState                `json:"assets"`
	LinkGroups                []domain.LinkGroupState            `json:"linkGroups"`
	Clips                     []domain.ClipState                 `json:"clips"`
	Captions                  []domain.CaptionState              `json:"captions"`
	Alignments                []domain.AlignmentState            `json:"alignments"`
}

func captureProjectVersionState(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
) (projectVersionState, []byte, domain.Digest, []byte, error) {
	state, err := loadProjectVersionState(ctx, tx, projectID)
	if err != nil {
		return projectVersionState{}, nil, "", nil, err
	}
	canonical, digest, err := domain.CanonicalDigest("open-cut/project-version-state", projectVersionStateSchema, state)
	if err != nil {
		return projectVersionState{}, nil, "", nil, err
	}
	if len(canonical) > maximumProjectVersionStateBytes {
		return projectVersionState{}, nil, "", nil, application.ErrProjectVersionInvalid
	}
	compressed, err := compressProjectVersionState(canonical)
	if err != nil {
		return projectVersionState{}, nil, "", nil, err
	}
	return state, canonical, digest, compressed, nil
}

func loadProjectVersionState(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
) (projectVersionState, error) {
	var result projectVersionState
	var projectRevision, documentRevision, sequenceRevision uint64
	var projectValue, documentValue, sequenceValue, audioLayout, colorPolicy string
	var canvasWidth, canvasHeight, audioSampleRate uint32
	var pixelValue, frameValue int64
	var pixelScale, frameScale int32
	err := tx.QueryRowContext(ctx, `
SELECT p.id, p.revision, d.id, d.revision, s.id, s.revision,
       s.canvas_width, s.canvas_height, s.pixel_aspect_value, s.pixel_aspect_scale,
       s.frame_rate_value, s.frame_rate_scale, s.audio_sample_rate, s.audio_layout, s.color_policy
FROM projects p
JOIN narrative_documents d ON d.id = p.narrative_document_id
JOIN sequences s ON s.id = p.main_sequence_id
WHERE p.id = ? AND p.status = 'active'`, projectID.String()).Scan(
		&projectValue, &projectRevision, &documentValue, &documentRevision,
		&sequenceValue, &sequenceRevision, &canvasWidth, &canvasHeight,
		&pixelValue, &pixelScale, &frameValue, &frameScale, &audioSampleRate,
		&audioLayout, &colorPolicy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return result, application.ErrProjectNotFound
	}
	if err != nil {
		return result, err
	}
	result.ProjectID, err = domain.ParseProjectID(projectValue)
	if err != nil {
		return result, application.ErrProjectVersionInvalid
	}
	result.ProjectRevision, err = domain.NewRevision(projectRevision)
	if err != nil {
		return result, err
	}
	result.NarrativeDocumentID, err = domain.ParseNarrativeDocumentID(documentValue)
	if err != nil {
		return result, application.ErrProjectVersionInvalid
	}
	result.NarrativeDocumentRevision, err = domain.NewRevision(documentRevision)
	if err != nil {
		return result, err
	}
	result.SequenceID, err = domain.ParseSequenceID(sequenceValue)
	if err != nil {
		return result, application.ErrProjectVersionInvalid
	}
	result.SequenceRevision, err = domain.NewRevision(sequenceRevision)
	if err != nil {
		return result, err
	}
	pixelAspect, pixelErr := domain.NewRationalTime(pixelValue, pixelScale)
	frameRate, frameErr := domain.NewRationalTime(frameValue, frameScale)
	result.Format = domain.SequenceFormat{
		CanvasWidth: canvasWidth, CanvasHeight: canvasHeight, PixelAspect: pixelAspect,
		FrameRate: frameRate, AudioSampleRate: audioSampleRate,
		AudioLayout: domain.AudioLayout(audioLayout), ColorPolicy: domain.ColorPolicy(colorPolicy),
	}
	if pixelErr != nil || frameErr != nil || result.Format.Validate() != nil {
		return result, application.ErrProjectVersionInvalid
	}
	if result.Tracks, err = loadProjectVersionTracks(ctx, tx, projectID, result.SequenceID); err != nil {
		return result, err
	}
	editState := emptyVersionEditState(projectID)
	if result.NarrativeNodes, err = loadProjectVersionNarrative(ctx, tx, &editState); err != nil {
		return result, err
	}
	if result.TranscriptCorrections, err = loadProjectVersionCorrections(ctx, tx, &editState); err != nil {
		return result, err
	}
	if result.Assets, err = loadProjectVersionAssets(ctx, tx, projectID); err != nil {
		return result, err
	}
	if result.LinkGroups, err = loadProjectVersionLinkGroups(ctx, tx, projectID); err != nil {
		return result, err
	}
	if result.Clips, err = loadProjectVersionClips(ctx, tx, &editState); err != nil {
		return result, err
	}
	if result.Captions, err = loadProjectVersionCaptions(ctx, tx, projectID); err != nil {
		return result, err
	}
	if result.Alignments, err = loadProjectVersionAlignments(ctx, tx, &editState); err != nil {
		return result, err
	}
	return result, nil
}

func emptyVersionEditState(projectID domain.ProjectID) application.EditNormalizationState {
	return application.EditNormalizationState{
		ProjectID: projectID, Sections: map[string]domain.NarrativeSectionState{},
		AuthoredTexts: map[string]domain.AuthoredTextState{}, SourceExcerpts: map[string]domain.SourceExcerptState{},
		VisualIntents: map[string]domain.VisualIntentState{}, Notes: map[string]domain.NoteState{},
		SectionChildCounts: map[string]int{}, SourceExcerptEvidence: map[string]domain.SourceExcerptEvidenceStatus{},
		TranscriptCorrections: map[string]domain.TranscriptCorrectionState{}, Captions: map[string]domain.CaptionState{},
		Clips: map[string]domain.ClipState{}, LinkGroups: map[string]domain.LinkGroupState{},
		LinkGroupClips: map[string][]domain.ClipID{}, Tracks: map[string]application.EditTrackState{},
		SourceStreams:       map[string]application.EditSourceStreamState{},
		TranscriptArtifacts: map[string]application.EditTranscriptArtifactState{},
		Alignments:          map[string]domain.AlignmentState{}, NodeAlignments: map[string][]domain.AlignmentID{},
		CaptionAlignments: map[string][]domain.AlignmentID{}, ClipAlignments: map[string][]domain.AlignmentID{},
	}
}

func loadProjectVersionTracks(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID, sequenceID domain.SequenceID) ([]projectVersionTrack, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, revision, type, label, order_key FROM tracks
WHERE project_id = ? AND sequence_id = ? ORDER BY order_key, id`, projectID.String(), sequenceID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]projectVersionTrack, 0)
	for rows.Next() {
		var item projectVersionTrack
		var idValue string
		var revision uint64
		if err := rows.Scan(&idValue, &revision, &item.Type, &item.Label, &item.OrderKey); err != nil {
			return nil, err
		}
		item.ID, err = domain.ParseTrackID(idValue)
		if err != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		item.Revision, err = domain.NewRevision(revision)
		if err != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadProjectVersionNarrative(ctx context.Context, tx *sql.Tx, state *application.EditNormalizationState) ([]domain.NarrativeNodeState, error) {
	ids, err := versionEntityIDs(ctx, tx, `SELECT id FROM narrative_nodes WHERE project_id = ? ORDER BY parent_id, order_index, id`, state.ProjectID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.NarrativeNodeState, 0, len(ids))
	for _, value := range ids {
		id, parseErr := domain.ParseNarrativeNodeID(value)
		if parseErr != nil || ensureNarrativeNodeState(ctx, tx, state, id) != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		result = append(result, narrativeNodeFromEditState(*state, value))
	}
	return result, nil
}

func narrativeNodeFromEditState(state application.EditNormalizationState, id string) domain.NarrativeNodeState {
	if value, ok := state.Sections[id]; ok {
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeSection, Section: &value}
	}
	if value, ok := state.AuthoredTexts[id]; ok {
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeAuthoredText, AuthoredText: &value}
	}
	if value, ok := state.SourceExcerpts[id]; ok {
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeSourceExcerpt, SourceExcerpt: &value}
	}
	if value, ok := state.VisualIntents[id]; ok {
		return domain.NarrativeNodeState{Kind: domain.NarrativeNodeVisualIntent, VisualIntent: &value}
	}
	value := state.Notes[id]
	return domain.NarrativeNodeState{Kind: domain.NarrativeNodeNote, Note: &value}
}

func loadProjectVersionCorrections(ctx context.Context, tx *sql.Tx, state *application.EditNormalizationState) ([]domain.TranscriptCorrectionState, error) {
	ids, err := versionEntityIDs(ctx, tx, `SELECT id FROM transcript_corrections WHERE project_id = ? ORDER BY id`, state.ProjectID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.TranscriptCorrectionState, 0, len(ids))
	for _, value := range ids {
		id, parseErr := domain.ParseTranscriptCorrectionID(value)
		if parseErr != nil || ensureTranscriptCorrectionState(ctx, tx, state, id) != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		result = append(result, state.TranscriptCorrections[value])
	}
	return result, nil
}

func loadProjectVersionAssets(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID) ([]domain.AssetState, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, revision, source_grant_id, display_name, import_mode, accepted_fingerprint, tombstoned
FROM assets WHERE project_id = ? ORDER BY id`, projectID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.AssetState, 0)
	for rows.Next() {
		var idValue, grantValue, displayName, mode string
		var revision uint64
		var fingerprint sql.NullString
		var tombstoned bool
		if err := rows.Scan(&idValue, &revision, &grantValue, &displayName, &mode, &fingerprint, &tombstoned); err != nil {
			return nil, err
		}
		id, idErr := domain.ParseAssetID(idValue)
		grant, grantErr := domain.ParseSourceGrantID(grantValue)
		revisionValue, revisionErr := domain.NewRevision(revision)
		if idErr != nil || grantErr != nil || revisionErr != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		item := domain.AssetState{ID: id, Revision: revisionValue, ProjectID: projectID, SourceGrantID: grant,
			DisplayName: displayName, ImportMode: domain.AssetImportMode(mode), Tombstoned: tombstoned}
		if fingerprint.Valid {
			parsed, parseErr := domain.ParseDigest(fingerprint.String)
			if parseErr != nil {
				return nil, application.ErrProjectVersionInvalid
			}
			item.AcceptedFingerprint = &parsed
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadProjectVersionLinkGroups(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID) ([]domain.LinkGroupState, error) {
	ids, err := versionEntityIDs(ctx, tx, `SELECT id FROM clip_link_groups WHERE project_id = ? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.LinkGroupState, 0, len(ids))
	for _, value := range ids {
		id, parseErr := domain.ParseLinkGroupID(value)
		item, loadErr := loadLinkGroupState(ctx, tx, projectID, id)
		if parseErr != nil || loadErr != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		result = append(result, item)
	}
	return result, nil
}

func loadProjectVersionClips(ctx context.Context, tx *sql.Tx, state *application.EditNormalizationState) ([]domain.ClipState, error) {
	ids, err := versionEntityIDs(ctx, tx, `SELECT id FROM clips WHERE project_id = ? ORDER BY id`, state.ProjectID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.ClipState, 0, len(ids))
	for _, value := range ids {
		id, parseErr := domain.ParseClipID(value)
		if parseErr != nil || ensureClipState(ctx, tx, state, id) != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		result = append(result, state.Clips[value])
	}
	return result, nil
}

func loadProjectVersionCaptions(ctx context.Context, tx *sql.Tx, projectID domain.ProjectID) ([]domain.CaptionState, error) {
	ids, err := versionEntityIDs(ctx, tx, `SELECT id FROM captions WHERE project_id = ? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.CaptionState, 0, len(ids))
	for _, value := range ids {
		id, parseErr := domain.ParseCaptionID(value)
		item, loadErr := loadCaptionState(ctx, tx, projectID, id)
		if parseErr != nil || loadErr != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		result = append(result, item)
	}
	return result, nil
}

func loadProjectVersionAlignments(ctx context.Context, tx *sql.Tx, state *application.EditNormalizationState) ([]domain.AlignmentState, error) {
	ids, err := versionEntityIDs(ctx, tx, `SELECT id FROM alignments WHERE project_id = ? ORDER BY id`, state.ProjectID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.AlignmentState, 0, len(ids))
	for _, value := range ids {
		id, parseErr := domain.ParseAlignmentID(value)
		if parseErr != nil || ensureAlignmentState(ctx, tx, state, id) != nil {
			return nil, application.ErrProjectVersionInvalid
		}
		result = append(result, state.Alignments[value])
	}
	return result, nil
}

func versionEntityIDs(ctx context.Context, tx *sql.Tx, query string, projectID domain.ProjectID) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, projectID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func compressProjectVersionState(canonical []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	writer.Name = ""
	writer.Comment = ""
	if _, err := writer.Write(canonical); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decodeProjectVersionState(compressed []byte, expected domain.Digest) (projectVersionState, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return projectVersionState{}, application.ErrProjectVersionInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, maximumProjectVersionStateBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) > maximumProjectVersionStateBytes {
		return projectVersionState{}, application.ErrProjectVersionInvalid
	}
	var envelope struct {
		Domain  string              `json:"domain"`
		Payload projectVersionState `json:"payload"`
		Schema  string              `json:"schema"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Domain != "open-cut/project-version-state" || envelope.Schema != projectVersionStateSchema {
		return projectVersionState{}, application.ErrProjectVersionInvalid
	}
	canonical, digest, err := domain.CanonicalDigest("open-cut/project-version-state", projectVersionStateSchema, envelope.Payload)
	if err != nil || digest != expected || !bytes.Equal(canonical, raw) {
		return projectVersionState{}, application.ErrProjectVersionInvalid
	}
	return envelope.Payload, nil
}

func sortVersionChanges(changes []domain.EntityRevisionChange) {
	sort.Slice(changes, func(left, right int) bool {
		if changes[left].Kind != changes[right].Kind {
			return changes[left].Kind < changes[right].Kind
		}
		return changes[left].ID < changes[right].ID
	})
}

func versionStateError(label string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s project version state: %w", label, err)
}
