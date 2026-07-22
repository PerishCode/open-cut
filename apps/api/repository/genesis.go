package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) CreateGenesis(
	ctx context.Context,
	record application.CreateProjectRecord,
) (application.CreateProjectOutcome, error) {
	if err := validateCreateProjectRecord(record); err != nil {
		return application.CreateProjectOutcome{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	defer tx.Rollback()

	replayed, err := loadExistingGenesis(ctx, tx, record)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return application.CreateProjectOutcome{}, err
		}
		replayed.Replayed = true
		return replayed, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return application.CreateProjectOutcome{}, err
	}

	genesis := record.Genesis
	project := genesis.Project
	document := project.NarrativeDocuments[0]
	root := document.Nodes[0]
	sequence := project.Sequences[0]
	createdAt := genesis.Record.CreatedAt.UTC().Format(time.RFC3339Nano)
	actorKind, actorID := string(record.Actor.Kind), record.Actor.IDString()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO local_creators (id, singleton, created_at) VALUES (?, 1, ?) ON CONFLICT(id) DO NOTHING`,
		actorID, createdAt,
	); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist local creator: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO projects (
  id, revision, lifecycle_revision, name, status, narrative_document_id, main_sequence_id, creator_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, project.ID.String(), project.Revision.Value(), project.LifecycleRevision.Value(), project.Name, project.Status,
		document.ID.String(), sequence.ID.String(), actorID, createdAt); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist project genesis: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO narrative_documents (id, project_id, revision, kind, root_node_id) VALUES (?, ?, ?, ?, ?)
`, document.ID.String(), project.ID.String(), document.Revision.Value(), document.Kind, document.RootNodeID.String()); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist narrative genesis: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO narrative_nodes (
  id, project_id, document_id, parent_id, revision, kind, order_index,
  tombstoned, last_transaction_id
) VALUES (?, ?, ?, NULL, ?, ?, 0, 0, ?)
`, root.ID.String(), project.ID.String(), document.ID.String(), root.Revision.Value(), root.Kind,
		genesis.Record.TransactionID.String()); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist root narrative node: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO narrative_section_values (id, title, language) VALUES (?, ?, ?)
`, root.ID.String(), root.Title, root.Language.String()); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist root narrative section value: %w", err)
	}
	format := sequence.Format
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequences (
  id, project_id, revision, name, role, canvas_width, canvas_height,
  pixel_aspect_value, pixel_aspect_scale, frame_rate_value, frame_rate_scale,
  audio_sample_rate, audio_layout, color_policy
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, sequence.ID.String(), project.ID.String(), sequence.Revision.Value(), sequence.Name, sequence.Role,
		format.CanvasWidth, format.CanvasHeight, format.PixelAspect.Value.Value(), format.PixelAspect.Scale,
		format.FrameRate.Value.Value(), format.FrameRate.Scale, format.AudioSampleRate, format.AudioLayout, format.ColorPolicy); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist sequence genesis: %w", err)
	}
	for _, track := range sequence.Tracks {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO tracks (id, project_id, sequence_id, revision, type, label, order_key) VALUES (?, ?, ?, ?, ?, ?, ?)
`, track.ID.String(), project.ID.String(), sequence.ID.String(), track.Revision.Value(), track.Type, track.Label, track.OrderKey); err != nil {
			return application.CreateProjectOutcome{}, fmt.Errorf("persist track genesis: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO edit_proposals (
  id, project_id, schema_version, digest, canonical_json, actor_kind, actor_id, status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'applied', ?)
`, genesis.Record.ProposalID.String(), project.ID.String(), domain.ProjectGenesisProposalSchema,
		genesis.Record.ProposalDigest.String(), string(record.ProposalCanonical), actorKind, actorID, createdAt); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist genesis proposal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO edit_transactions (
  id, project_id, proposal_id, project_revision, schema_version, operation_json, inverse_json,
  actor_kind, actor_id, committed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, genesis.Record.TransactionID.String(), project.ID.String(), genesis.Record.ProposalID.String(),
		genesis.Record.CommittedProjectRevision.Value(), domain.ProjectGenesisTransactionSchema,
		string(record.ProposalCanonical), string(record.InverseCanonical), actorKind, actorID, createdAt); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist genesis transaction: %w", err)
	}
	if _, err := insertProjectVersionSnapshot(ctx, tx, projectVersionCapture{
		ID: genesis.Record.ProjectVersionID, ProjectID: project.ID, CreatorID: *record.Actor.CreatorID,
		Source: application.ProjectVersionGenesis, Name: "Project created",
		Retention: application.ProjectVersionPinned, CreatedAt: genesis.Record.CreatedAt,
	}); err != nil {
		return application.CreateProjectOutcome{}, err
	}

	projectPayload, err := projectCreatedActivityPayload(genesis)
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	projectCursor, err := appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: project.ID.String(), EventID: genesis.Record.ActivityEventID.String(),
		Kind: "project.created", OccurredAt: createdAt, ActorKind: actorKind, ActorID: actorID,
		ProjectID: project.ID.String(), ProjectRevision: int64(project.Revision.Value()),
		OutcomeKind: "transaction", OutcomeID: genesis.Record.TransactionID.String(),
		SummaryCode: "project-created", Payload: projectPayload,
	})
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	installationPayload, err := json.Marshal(struct {
		ProjectID string `json:"projectId"`
	}{ProjectID: project.ID.String()})
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	installationCursor, err := appendActivity(ctx, tx, activityRecord{
		ScopeKind: "installation", ScopeID: record.InstallationID, EventID: record.ActivityEventID.String(),
		Kind: "workspace.project-created", OccurredAt: createdAt, ActorKind: actorKind, ActorID: actorID,
		ProjectID: project.ID.String(), ProjectRevision: int64(project.Revision.Value()),
		OutcomeKind: "project", OutcomeID: project.ID.String(), SummaryCode: "project-created",
		Payload: installationPayload,
	})
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO request_identities (
  actor_kind, actor_id, request_id, schema_version, input_digest, input_json, installation_id,
  project_id, proposal_id, transaction_id, project_activity_event_id, installation_activity_event_id,
  status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'committed', ?)
`, actorKind, actorID, genesis.Record.RequestID.String(), application.ProjectCreateSchema,
		record.RequestDigest.String(), string(record.RequestCanonical), record.InstallationID,
		project.ID.String(), genesis.Record.ProposalID.String(), genesis.Record.TransactionID.String(),
		genesis.Record.ActivityEventID.String(), record.ActivityEventID.String(), createdAt); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("persist genesis request identity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.CreateProjectOutcome{}, fmt.Errorf("commit project genesis: %w", err)
	}
	return application.CreateProjectOutcome{
		Genesis: genesis, ProjectActivityCursor: projectCursor,
		InstallationActivityCursor: installationCursor,
	}, nil
}

func validateCreateProjectRecord(record application.CreateProjectRecord) error {
	if err := record.Actor.Validate(); err != nil || record.Actor.Kind != domain.ActorCreator {
		return domain.ErrInvalidCreativeActor
	}
	if _, err := domain.ParseRequestID(record.InstallationID); err != nil {
		return application.ErrAuthorityInvalid
	}
	if record.ActivityEventID.IsZero() || !json.Valid(record.RequestCanonical) ||
		!json.Valid(record.ProposalCanonical) || !json.Valid(record.InverseCanonical) {
		return fmt.Errorf("invalid project genesis persistence record")
	}
	genesis := record.Genesis
	if len(genesis.Project.NarrativeDocuments) != 1 || len(genesis.Project.NarrativeDocuments[0].Nodes) != 1 ||
		len(genesis.Project.Sequences) != 1 || len(genesis.Project.Sequences[0].Tracks) != 3 ||
		genesis.Record.Actor.IDString() != record.Actor.IDString() || genesis.Record.Actor.Kind != record.Actor.Kind {
		return fmt.Errorf("invalid project genesis graph")
	}
	return nil
}

func loadExistingGenesis(
	ctx context.Context,
	tx *sql.Tx,
	record application.CreateProjectRecord,
) (application.CreateProjectOutcome, error) {
	var digest, projectID, projectEventID, installationEventID string
	err := tx.QueryRowContext(ctx, `
SELECT input_digest, project_id, project_activity_event_id, installation_activity_event_id
FROM request_identities WHERE actor_kind = ? AND actor_id = ? AND request_id = ?
`, record.Actor.Kind, record.Actor.IDString(), record.Genesis.Record.RequestID.String()).Scan(
		&digest, &projectID, &projectEventID, &installationEventID,
	)
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	if digest != record.RequestDigest.String() {
		return application.CreateProjectOutcome{}, application.ErrRequestIdentityReused
	}
	genesis, err := loadProjectGenesis(ctx, tx, projectID)
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	projectCursor, err := activityCursorForEvent(ctx, tx, projectEventID)
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	installationCursor, err := activityCursorForEvent(ctx, tx, installationEventID)
	if err != nil {
		return application.CreateProjectOutcome{}, err
	}
	return application.CreateProjectOutcome{
		Genesis: genesis, ProjectActivityCursor: projectCursor,
		InstallationActivityCursor: installationCursor,
	}, nil
}

type activityRecord struct {
	ScopeKind       string
	ScopeID         string
	EventID         string
	Kind            string
	OccurredAt      string
	ActorKind       any
	ActorID         any
	ProjectID       any
	ProjectRevision any
	OutcomeKind     string
	OutcomeID       string
	SummaryCode     string
	Payload         []byte
}

func appendActivity(ctx context.Context, tx *sql.Tx, record activityRecord) (domain.Cursor, error) {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO activity_heads (scope_kind, scope_id, cursor) VALUES (?, ?, 0)
ON CONFLICT(scope_kind, scope_id) DO NOTHING
`, record.ScopeKind, record.ScopeID); err != nil {
		return 0, fmt.Errorf("initialize activity head: %w", err)
	}
	var cursorValue uint64
	if err := tx.QueryRowContext(ctx, `
UPDATE activity_heads SET cursor = cursor + 1 WHERE scope_kind = ? AND scope_id = ? RETURNING cursor
`, record.ScopeKind, record.ScopeID).Scan(&cursorValue); err != nil {
		return 0, fmt.Errorf("advance activity head: %w", err)
	}
	cursor, err := domain.NewCursor(cursorValue)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO activity_outbox (
  scope_kind, scope_id, cursor, event_id, schema_version, kind, occurred_at,
  actor_kind, actor_id, project_id, project_revision, outcome_kind, outcome_id, summary_code, payload_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, record.ScopeKind, record.ScopeID, cursor.Value(), record.EventID, application.ActivitySchema,
		record.Kind, record.OccurredAt, record.ActorKind, record.ActorID, record.ProjectID,
		record.ProjectRevision, record.OutcomeKind, record.OutcomeID, record.SummaryCode, string(record.Payload)); err != nil {
		return 0, fmt.Errorf("append activity outbox: %w", err)
	}
	if err := commandOutcomeReceiptFromActivity(ctx, tx, record, cursor, domain.RunID{}, domain.TurnID{}); err != nil {
		return 0, fmt.Errorf("append command outcome receipt: %w", err)
	}
	return cursor, nil
}

func activityCursorForEvent(ctx context.Context, tx *sql.Tx, eventID string) (domain.Cursor, error) {
	var value uint64
	if err := tx.QueryRowContext(ctx, `SELECT cursor FROM activity_outbox WHERE event_id = ?`, eventID).Scan(&value); err != nil {
		return 0, err
	}
	return domain.NewCursor(value)
}

func projectCreatedActivityPayload(genesis domain.ProjectGenesis) ([]byte, error) {
	type changedEntity struct {
		Kind     string          `json:"kind"`
		ID       string          `json:"id"`
		Revision domain.Revision `json:"revision"`
	}
	project := genesis.Project
	document := project.NarrativeDocuments[0]
	sequence := project.Sequences[0]
	changed := []changedEntity{
		{Kind: "project", ID: project.ID.String(), Revision: project.Revision},
		{Kind: "narrative-document", ID: document.ID.String(), Revision: document.Revision},
		{Kind: "narrative-node", ID: document.Nodes[0].ID.String(), Revision: document.Nodes[0].Revision},
		{Kind: "sequence", ID: sequence.ID.String(), Revision: sequence.Revision},
	}
	for _, track := range sequence.Tracks {
		changed = append(changed, changedEntity{Kind: "track", ID: track.ID.String(), Revision: track.Revision})
	}
	return json.Marshal(struct {
		ChangedEntityRefs []changedEntity `json:"changedEntityRefs"`
		ProposalID        string          `json:"proposalId"`
		TransactionID     string          `json:"transactionId"`
	}{
		ChangedEntityRefs: changed, ProposalID: genesis.Record.ProposalID.String(),
		TransactionID: genesis.Record.TransactionID.String(),
	})
}
