package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func verifyMediaAttempt(
	ctx context.Context,
	tx *sql.Tx,
	claim application.MediaJobClaim,
	now time.Time,
	kind domain.MediaJobKind,
) error {
	var attemptState, owner, expires, jobState, jobKind, sourceGrant string
	var generation uint64
	err := tx.QueryRowContext(ctx, `
SELECT a.state, a.lease_owner, a.lease_expires_at, a.generation,
       j.state, j.kind, asset.source_grant_id
FROM work_job_attempts a
JOIN work_jobs j ON j.id = a.job_id
JOIN media_job_details detail ON detail.job_id = j.id
JOIN assets asset ON asset.id = detail.asset_id
WHERE a.id = ? AND a.job_id = ?`, claim.AttemptID.String(), claim.JobID.String()).Scan(
		&attemptState, &owner, &expires, &generation, &jobState, &jobKind, &sourceGrant,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrMediaLeaseLost
	}
	if err != nil {
		return err
	}
	leaseExpiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return err
	}
	if attemptState != "running" || owner != claim.LeaseOwner || generation != claim.Generation ||
		jobState != "running" || jobKind != string(kind) || sourceGrant != claim.SourceGrantID.String() ||
		!leaseExpiry.After(now.UTC()) {
		return application.ErrMediaLeaseLost
	}
	return nil
}

func appendMediaJobActivity(
	ctx context.Context,
	tx *sql.Tx,
	claim application.MediaJobClaim,
	eventID domain.ActivityEventID,
	at time.Time,
	kind, summary string,
	failureCode *string,
) error {
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, claim.ProjectID.String()).Scan(&projectRevision); err != nil {
		return err
	}
	revision, err := domain.NewRevision(projectRevision)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		JobID             domain.MediaJobID              `json:"jobId"`
		AttemptID         domain.JobAttemptID            `json:"attemptId"`
		FailureCode       *string                        `json:"failureCode,omitempty"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{{
			Kind: "asset-media-state", ID: claim.AssetID.String(), Revision: revision,
		}},
		JobID: claim.JobID, AttemptID: claim.AttemptID, FailureCode: failureCode,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: claim.ProjectID.String(), EventID: eventID.String(), Kind: kind,
		OccurredAt: formatInstant(at.UTC()), ActorKind: nil, ActorID: nil,
		ProjectID: claim.ProjectID.String(), ProjectRevision: int64(revision.Value()),
		OutcomeKind: "media-job", OutcomeID: claim.JobID.String(), SummaryCode: summary, Payload: payload,
	})
	return err
}
