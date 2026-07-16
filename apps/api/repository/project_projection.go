package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ListProjects(
	ctx context.Context,
	query application.ProjectListQuery,
) (application.ProjectListPage, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProjectListPage{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT id, revision, lifecycle_revision, name, status, narrative_document_id, main_sequence_id
FROM projects WHERE status = ? AND id > ? ORDER BY id LIMIT ?
`, query.Status, query.AfterID, query.Limit+1)
	if err != nil {
		return application.ProjectListPage{}, err
	}
	projects := make([]application.ProjectSummary, 0, query.Limit+1)
	for rows.Next() {
		project, err := scanProjectSummary(rows)
		if err != nil {
			rows.Close()
			return application.ProjectListPage{}, err
		}
		projects = append(projects, project)
	}
	if err := rows.Close(); err != nil {
		return application.ProjectListPage{}, err
	}
	if err := rows.Err(); err != nil {
		return application.ProjectListPage{}, err
	}
	hasMore := len(projects) > query.Limit
	if hasMore {
		projects = projects[:query.Limit]
	}
	cursor, err := loadActivityHead(ctx, tx, "installation", query.ScopeID)
	if err != nil {
		return application.ProjectListPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.ProjectListPage{}, err
	}
	return application.ProjectListPage{Projects: projects, HasMore: hasMore, ActivityCursor: cursor}, nil
}

func (repository *SQLiteProjects) ShowProject(
	ctx context.Context,
	projectID domain.ProjectID,
) (application.ProjectOverview, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProjectOverview{}, err
	}
	defer tx.Rollback()
	project, err := loadProjectProjection(ctx, tx, projectID.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ProjectOverview{}, application.ErrProjectNotFound
		}
		return application.ProjectOverview{}, err
	}
	sequence := project.Sequences[0]
	document := project.NarrativeDocuments[0]
	cursor, err := loadActivityHead(ctx, tx, "project", project.ID.String())
	if err != nil {
		return application.ProjectOverview{}, err
	}
	tracks := make([]application.TrackSummary, 0, len(sequence.Tracks))
	for _, track := range sequence.Tracks {
		tracks = append(tracks, application.TrackSummary{
			ID: track.ID, Revision: track.Revision, Type: track.Type, Label: track.Label,
		})
	}
	overview := application.ProjectOverview{
		Project: application.ProjectSummary{
			ID: project.ID, Revision: project.Revision, LifecycleRevision: project.LifecycleRevision,
			Name: project.Name, Status: project.Status, NarrativeDocumentID: document.ID,
			MainSequenceID: sequence.ID,
		},
		NarrativeDocumentRevision: document.Revision, NarrativeRootNodeID: document.RootNodeID,
		MainSequenceRevision: sequence.Revision,
		Format:               sequence.Format, Tracks: tracks, ActivityCursor: cursor,
	}
	if err := tx.Commit(); err != nil {
		return application.ProjectOverview{}, err
	}
	return overview, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanProjectSummary(row rowScanner) (application.ProjectSummary, error) {
	var id, name, status, documentID, sequenceID string
	var revisionValue, lifecycleValue uint64
	if err := row.Scan(&id, &revisionValue, &lifecycleValue, &name, &status, &documentID, &sequenceID); err != nil {
		return application.ProjectSummary{}, err
	}
	project, err := domain.ParseProjectID(id)
	if err != nil {
		return application.ProjectSummary{}, err
	}
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return application.ProjectSummary{}, err
	}
	lifecycleRevision, err := domain.NewRevision(lifecycleValue)
	if err != nil {
		return application.ProjectSummary{}, err
	}
	document, err := domain.ParseNarrativeDocumentID(documentID)
	if err != nil {
		return application.ProjectSummary{}, err
	}
	sequence, err := domain.ParseSequenceID(sequenceID)
	if err != nil {
		return application.ProjectSummary{}, err
	}
	projectStatus := domain.ProjectStatus(status)
	if projectStatus != domain.ProjectActive && projectStatus != domain.ProjectArchived && projectStatus != domain.ProjectTombstoned {
		return application.ProjectSummary{}, fmt.Errorf("invalid persisted project status %q", status)
	}
	return application.ProjectSummary{
		ID: project, Revision: revision, LifecycleRevision: lifecycleRevision, Name: name, Status: projectStatus,
		NarrativeDocumentID: document, MainSequenceID: sequence,
	}, nil
}

func loadProjectProjection(ctx context.Context, tx *sql.Tx, id string) (domain.Project, error) {
	var (
		projectID, name, status                          string
		documentID, documentKind, rootID                 string
		rootKind, rootTitle, rootLanguage                string
		sequenceID, sequenceName, sequenceRole           string
		audioLayout, colorPolicy                         string
		projectRevision, lifecycleRevision               uint64
		documentRevision, rootRevision, sequenceRevision uint64
		canvasWidth, canvasHeight, audioSampleRate       uint32
		pixelValue, frameValue                           int64
		pixelScale, frameScale                           int32
		rootOrder                                        int64
		rootTombstoned                                   bool
	)
	err := tx.QueryRowContext(ctx, `
SELECT
  p.id, p.revision, p.lifecycle_revision, p.name, p.status,
  d.id, d.revision, d.kind, d.root_node_id,
  n.revision, n.kind, value.title, value.language, n.order_index, n.tombstoned,
  s.id, s.revision, s.name, s.role, s.canvas_width, s.canvas_height,
  s.pixel_aspect_value, s.pixel_aspect_scale, s.frame_rate_value, s.frame_rate_scale,
  s.audio_sample_rate, s.audio_layout, s.color_policy
FROM projects p
JOIN narrative_documents d ON d.id = p.narrative_document_id
JOIN narrative_nodes n ON n.id = d.root_node_id
JOIN narrative_section_values value ON value.id = n.id
JOIN sequences s ON s.id = p.main_sequence_id
WHERE p.id = ?
`, id).Scan(
		&projectID, &projectRevision, &lifecycleRevision, &name, &status,
		&documentID, &documentRevision, &documentKind, &rootID,
		&rootRevision, &rootKind, &rootTitle, &rootLanguage, &rootOrder, &rootTombstoned,
		&sequenceID, &sequenceRevision, &sequenceName, &sequenceRole, &canvasWidth, &canvasHeight,
		&pixelValue, &pixelScale, &frameValue, &frameScale, &audioSampleRate, &audioLayout, &colorPolicy,
	)
	if err != nil {
		return domain.Project{}, err
	}
	project, err := domain.ParseProjectID(projectID)
	if err != nil {
		return domain.Project{}, err
	}
	projectRev, err := domain.NewRevision(projectRevision)
	if err != nil {
		return domain.Project{}, err
	}
	lifecycleRev, err := domain.NewRevision(lifecycleRevision)
	if err != nil {
		return domain.Project{}, err
	}
	document, err := domain.ParseNarrativeDocumentID(documentID)
	if err != nil {
		return domain.Project{}, err
	}
	documentRev, err := domain.NewRevision(documentRevision)
	if err != nil {
		return domain.Project{}, err
	}
	root, err := domain.ParseNarrativeNodeID(rootID)
	if err != nil {
		return domain.Project{}, err
	}
	rootRev, err := domain.NewRevision(rootRevision)
	if err != nil {
		return domain.Project{}, err
	}
	language, err := domain.ParseCaptionLanguage(rootLanguage)
	if err != nil {
		return domain.Project{}, err
	}
	sequence, err := domain.ParseSequenceID(sequenceID)
	if err != nil {
		return domain.Project{}, err
	}
	sequenceRev, err := domain.NewRevision(sequenceRevision)
	if err != nil {
		return domain.Project{}, err
	}
	pixelAspect, err := domain.NewRationalTime(pixelValue, pixelScale)
	if err != nil {
		return domain.Project{}, err
	}
	frameRate, err := domain.NewRationalTime(frameValue, frameScale)
	if err != nil {
		return domain.Project{}, err
	}
	format := domain.SequenceFormat{
		CanvasWidth: canvasWidth, CanvasHeight: canvasHeight, PixelAspect: pixelAspect, FrameRate: frameRate,
		AudioSampleRate: audioSampleRate, AudioLayout: domain.AudioLayout(audioLayout), ColorPolicy: domain.ColorPolicy(colorPolicy),
	}
	if err := format.Validate(); err != nil {
		return domain.Project{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, revision, type, label, order_key FROM tracks WHERE sequence_id = ? ORDER BY order_key, id
`, sequenceID)
	if err != nil {
		return domain.Project{}, err
	}
	tracks := make([]domain.Track, 0, 3)
	for rows.Next() {
		var trackID, trackType, label, orderKey string
		var revisionValue uint64
		if err := rows.Scan(&trackID, &revisionValue, &trackType, &label, &orderKey); err != nil {
			rows.Close()
			return domain.Project{}, err
		}
		parsedID, err := domain.ParseTrackID(trackID)
		if err != nil {
			rows.Close()
			return domain.Project{}, err
		}
		parsedRevision, err := domain.NewRevision(revisionValue)
		if err != nil {
			rows.Close()
			return domain.Project{}, err
		}
		kind := domain.TrackType(trackType)
		if kind != domain.TrackVideo && kind != domain.TrackAudio && kind != domain.TrackCaption {
			rows.Close()
			return domain.Project{}, fmt.Errorf("invalid persisted track type %q", trackType)
		}
		tracks = append(tracks, domain.Track{ID: parsedID, Revision: parsedRevision, Type: kind, Label: label, OrderKey: orderKey})
	}
	if err := rows.Close(); err != nil {
		return domain.Project{}, err
	}
	if err := rows.Err(); err != nil {
		return domain.Project{}, err
	}
	if len(tracks) != 3 || rootOrder != 0 || rootTombstoned {
		return domain.Project{}, fmt.Errorf("persisted project genesis graph is incomplete")
	}
	projectStatus := domain.ProjectStatus(status)
	if projectStatus != domain.ProjectActive && projectStatus != domain.ProjectArchived && projectStatus != domain.ProjectTombstoned {
		return domain.Project{}, fmt.Errorf("invalid persisted project status %q", status)
	}
	return domain.Project{
		ID: project, Revision: projectRev, LifecycleRevision: lifecycleRev, Name: name, Status: projectStatus,
		NarrativeDocuments: []domain.NarrativeDocument{{
			ID: document, Revision: documentRev, Kind: domain.NarrativeDocumentKind(documentKind), RootNodeID: root,
			Nodes: []domain.NarrativeNode{{
				ID: root, Revision: rootRev, Kind: domain.NarrativeNodeKind(rootKind),
				Title: rootTitle, Language: language,
			}},
		}},
		Sequences: []domain.Sequence{{
			ID: sequence, Revision: sequenceRev, Name: sequenceName, Role: domain.SequenceRole(sequenceRole),
			Format: format, Tracks: tracks,
		}},
	}, nil
}

func loadProjectGenesis(ctx context.Context, tx *sql.Tx, projectID string) (domain.ProjectGenesis, error) {
	project, err := loadProjectProjection(ctx, tx, projectID)
	if err != nil {
		return domain.ProjectGenesis{}, err
	}
	var requestID, actorKind, actorID, proposalID, proposalDigest, transactionID, activityEventID, createdAt string
	var committedRevision uint64
	if err := tx.QueryRowContext(ctx, `
SELECT
  r.request_id, r.actor_kind, r.actor_id, p.id, p.digest, t.id, t.project_revision,
  r.project_activity_event_id, t.committed_at
FROM request_identities r
JOIN edit_proposals p ON p.id = r.proposal_id
JOIN edit_transactions t ON t.id = r.transaction_id
WHERE r.project_id = ? AND t.project_revision = 1
`, projectID).Scan(
		&requestID, &actorKind, &actorID, &proposalID, &proposalDigest, &transactionID,
		&committedRevision, &activityEventID, &createdAt,
	); err != nil {
		return domain.ProjectGenesis{}, err
	}
	request, err := domain.ParseRequestID(requestID)
	if err != nil {
		return domain.ProjectGenesis{}, err
	}
	creator, err := domain.ParseCreatorID(actorID)
	if err != nil || domain.CreativeActor(actorKind) != domain.ActorCreator {
		return domain.ProjectGenesis{}, domain.ErrInvalidCreativeActor
	}
	proposal, err := domain.ParseProposalID(proposalID)
	if err != nil {
		return domain.ProjectGenesis{}, err
	}
	digest, err := domain.ParseDigest(proposalDigest)
	if err != nil {
		return domain.ProjectGenesis{}, err
	}
	transaction, err := domain.ParseTransactionID(transactionID)
	if err != nil {
		return domain.ProjectGenesis{}, err
	}
	revision, err := domain.NewRevision(committedRevision)
	if err != nil {
		return domain.ProjectGenesis{}, err
	}
	activity, err := domain.ParseActivityEventID(activityEventID)
	if err != nil {
		return domain.ProjectGenesis{}, err
	}
	committedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.ProjectGenesis{}, err
	}
	return domain.ProjectGenesis{
		Project: project,
		Record: domain.GenesisRecord{
			ProposalID: proposal, TransactionID: transaction, RequestID: request,
			Actor: domain.CreatorActor(creator), CommittedProjectRevision: revision,
			ProposalDigest: digest, ActivityEventID: activity, CreatedAt: committedAt.UTC(),
		},
	}, nil
}

func loadActivityHead(ctx context.Context, tx *sql.Tx, scopeKind, scopeID string) (domain.Cursor, error) {
	var value uint64
	err := tx.QueryRowContext(ctx, `
SELECT cursor FROM activity_heads WHERE scope_kind = ? AND scope_id = ?
`, scopeKind, scopeID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NewCursor(0)
	}
	if err != nil {
		return 0, err
	}
	return domain.NewCursor(value)
}
