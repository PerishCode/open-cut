package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ListActivity(
	ctx context.Context,
	query application.ActivityQuery,
) (application.ActivityPage, error) {
	if query.Scope.Kind != application.ActivityScopeProject && query.Scope.Kind != application.ActivityScopeInstallation {
		return application.ActivityPage{}, application.ErrInvalidActivityScope
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ActivityPage{}, err
	}
	defer tx.Rollback()
	head, err := loadActivityHead(ctx, tx, string(query.Scope.Kind), query.Scope.ID)
	if err != nil {
		return application.ActivityPage{}, err
	}
	if query.After.Value() > head.Value() {
		return application.ActivityPage{}, application.ErrInvalidActivityScope
	}
	rows, err := tx.QueryContext(ctx, `
SELECT
  cursor, event_id, schema_version, kind, occurred_at, actor_kind, actor_id,
  project_id, project_revision, outcome_kind, outcome_id, summary_code, payload_json
FROM activity_outbox
WHERE scope_kind = ? AND scope_id = ? AND cursor > ?
ORDER BY cursor LIMIT ?
`, query.Scope.Kind, query.Scope.ID, query.After.Value(), query.Limit+1)
	if err != nil {
		return application.ActivityPage{}, err
	}
	events := make([]application.ActivityEvent, 0, query.Limit+1)
	for rows.Next() {
		event, err := scanActivityEvent(rows, query.Scope)
		if err != nil {
			rows.Close()
			return application.ActivityPage{}, err
		}
		events = append(events, event)
	}
	if err := rows.Close(); err != nil {
		return application.ActivityPage{}, err
	}
	if err := rows.Err(); err != nil {
		return application.ActivityPage{}, err
	}
	hasMore := len(events) > query.Limit
	if hasMore {
		events = events[:query.Limit]
	}
	cursor := head
	if hasMore && len(events) > 0 {
		cursor = events[len(events)-1].Cursor
	}
	if err := tx.Commit(); err != nil {
		return application.ActivityPage{}, err
	}
	return application.ActivityPage{Events: events, Cursor: cursor, HasMore: hasMore}, nil
}

func scanActivityEvent(row rowScanner, scope application.ActivityScope) (application.ActivityEvent, error) {
	var (
		cursorValue                                             uint64
		eventID, schema, kind, occurredAt, summaryCode, payload string
		actorKind, actorID, projectID, outcomeKind, outcomeID   sql.NullString
		projectRevision                                         sql.NullInt64
	)
	if err := row.Scan(
		&cursorValue, &eventID, &schema, &kind, &occurredAt, &actorKind, &actorID,
		&projectID, &projectRevision, &outcomeKind, &outcomeID, &summaryCode, &payload,
	); err != nil {
		return application.ActivityEvent{}, err
	}
	cursor, err := domain.NewCursor(cursorValue)
	if err != nil {
		return application.ActivityEvent{}, err
	}
	id, err := domain.ParseActivityEventID(eventID)
	if err != nil {
		return application.ActivityEvent{}, err
	}
	instant, err := time.Parse(time.RFC3339Nano, occurredAt)
	if err != nil {
		return application.ActivityEvent{}, err
	}
	event := application.ActivityEvent{
		Schema: schema, EventID: id, Scope: scope, Cursor: cursor, Kind: kind,
		OccurredAt: instant.UTC(), ChangedEntityRefs: []application.ChangedEntityRef{}, SummaryCode: summaryCode,
	}
	if actorKind.Valid || actorID.Valid {
		if !actorKind.Valid || !actorID.Valid {
			return application.ActivityEvent{}, fmt.Errorf("incomplete persisted activity actor")
		}
		event.Actor = &application.ActivityActor{Kind: domain.CreativeActor(actorKind.String), ID: actorID.String}
		if event.Actor.Kind != domain.ActorCreator && event.Actor.Kind != domain.ActorAgent {
			return application.ActivityEvent{}, domain.ErrInvalidCreativeActor
		}
	}
	if projectID.Valid {
		parsed, err := domain.ParseProjectID(projectID.String)
		if err != nil {
			return application.ActivityEvent{}, err
		}
		event.ProjectID = &parsed
	}
	if projectRevision.Valid {
		if projectRevision.Int64 < 0 {
			return application.ActivityEvent{}, domain.ErrRevisionOverflow
		}
		parsed, err := domain.NewRevision(uint64(projectRevision.Int64))
		if err != nil {
			return application.ActivityEvent{}, err
		}
		event.ProjectRevision = &parsed
	}
	if outcomeKind.Valid || outcomeID.Valid {
		if !outcomeKind.Valid || !outcomeID.Valid {
			return application.ActivityEvent{}, fmt.Errorf("incomplete persisted activity outcome")
		}
		event.Outcome = &application.ActivityOutcomeRef{Kind: outcomeKind.String, ID: outcomeID.String}
	}
	var decoded struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return application.ActivityEvent{}, err
	}
	if decoded.ChangedEntityRefs != nil {
		event.ChangedEntityRefs = decoded.ChangedEntityRefs
	}
	return event, nil
}
