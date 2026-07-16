package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const SequenceExportDeleteArtifactSchema = "open-cut/sequence-export-delete-artifact/v1"

type SequenceExportOrigin string

const (
	SequenceExportOriginAgent   SequenceExportOrigin = "agent"
	SequenceExportOriginCreator SequenceExportOrigin = "creator"
)

type SequenceExportArtifactAvailability string

const (
	SequenceExportArtifactNone    SequenceExportArtifactAvailability = "none"
	SequenceExportArtifactReady   SequenceExportArtifactAvailability = "ready"
	SequenceExportArtifactInvalid SequenceExportArtifactAvailability = "invalid"
	SequenceExportArtifactDeleted SequenceExportArtifactAvailability = "deleted"
)

type SequenceExportLineage struct {
	Origin               SequenceExportOrigin               `json:"origin" enum:"agent,creator"`
	AttemptCount         domain.UInt64                      `json:"attemptCount" format:"uint64-decimal"`
	ArtifactAvailability SequenceExportArtifactAvailability `json:"artifactAvailability" enum:"none,ready,invalid,deleted"`
	RootCreatedAt        time.Time                          `json:"rootCreatedAt" format:"date-time"`
	Export               SequenceExportResult               `json:"export"`
}

type SequenceExportHistoryQuery struct {
	ProjectID      domain.ProjectID
	AfterRootID    string
	AfterCreatedAt time.Time
	Limit          int
}

type SequenceExportHistoryPage struct {
	Lineages       []SequenceExportLineage
	HasMore        bool
	ActivityCursor domain.Cursor
}

type ListSequenceExportHistoryInput struct {
	After string
	Limit uint16
}

type ListSequenceExportHistoryResult struct {
	Lineages       []SequenceExportLineage `json:"lineages" maxItems:"50" nullable:"false"`
	NextAfter      string                  `json:"nextAfter,omitempty" maxLength:"512"`
	ActivityCursor domain.Cursor           `json:"activityCursor" format:"uint64-decimal"`
}

func (exports *SequenceExports) ListForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	input ListSequenceExportHistoryInput,
) (ListSequenceExportHistoryResult, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return ListSequenceExportHistoryResult{}, err
	}
	if projectID.IsZero() {
		return ListSequenceExportHistoryResult{}, ErrSequenceExportInvalid
	}
	limit := int(input.Limit)
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return ListSequenceExportHistoryResult{}, ErrSequenceExportInvalid
	}
	after, err := decodeSequenceExportHistoryCursor(input.After)
	if err != nil {
		return ListSequenceExportHistoryResult{}, err
	}
	page, err := exports.repository.ListSequenceExportHistory(ctx, SequenceExportHistoryQuery{
		ProjectID: projectID, AfterRootID: after.rootID, AfterCreatedAt: after.createdAt, Limit: limit,
	})
	if err != nil {
		return ListSequenceExportHistoryResult{}, err
	}
	result := ListSequenceExportHistoryResult{
		Lineages: page.Lineages, ActivityCursor: page.ActivityCursor,
	}
	if result.Lineages == nil {
		result.Lineages = []SequenceExportLineage{}
	}
	if page.HasMore && len(page.Lineages) > 0 {
		last := page.Lineages[len(page.Lineages)-1]
		result.NextAfter, err = encodeSequenceExportHistoryCursor(last.RootCreatedAt, last.Export.Job.RootJobID)
		if err != nil {
			return ListSequenceExportHistoryResult{}, err
		}
	}
	return result, nil
}

type SequenceExportDeleteArtifactInput struct {
	RequestID  domain.RequestID  `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	ArtifactID domain.ArtifactID `json:"artifactId" format:"uuid"`
}

type DeleteSequenceExportArtifactRecord struct {
	ReadSequenceExportRecord
	ArtifactID       domain.ArtifactID
	RequestID        domain.RequestID
	RequestDigest    domain.Digest
	RequestCanonical []byte
	ActivityEventID  domain.ActivityEventID
	DeletedAt        time.Time
}

func (exports *SequenceExports) DeleteArtifactForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	jobID domain.WorkJobID,
	input SequenceExportDeleteArtifactInput,
) (SequenceExportResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if projectID.IsZero() || jobID.IsZero() || input.ArtifactID.IsZero() {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-delete-artifact", SequenceExportDeleteArtifactSchema, struct {
			ArtifactID domain.ArtifactID `json:"artifactId"`
			JobID      domain.WorkJobID  `json:"jobId"`
		}{input.ArtifactID, jobID},
	)
	if err != nil {
		return SequenceExportResult{}, err
	}
	now := exports.clock.Now().UTC()
	eventID, err := exports.newActivityEventID(ctx, now)
	if err != nil {
		return SequenceExportResult{}, err
	}
	owner := SequenceExportOwner{Kind: SequenceExportOwnerCreator, ID: authority.Actor.IDString()}
	return exports.repository.DeleteSequenceExportArtifact(ctx, DeleteSequenceExportArtifactRecord{
		ReadSequenceExportRecord: ReadSequenceExportRecord{
			ProjectID: projectID, Actor: authority.Actor, Owner: owner, JobID: jobID,
		},
		ArtifactID: input.ArtifactID, RequestID: input.RequestID,
		RequestDigest: digest, RequestCanonical: canonical,
		ActivityEventID: eventID, DeletedAt: now,
	})
}

type sequenceExportHistoryCursor struct {
	createdAt time.Time
	rootID    string
}

func encodeSequenceExportHistoryCursor(createdAt time.Time, rootID domain.WorkJobID) (string, error) {
	if createdAt.IsZero() || rootID.IsZero() {
		return "", ErrInvalidPageCursor
	}
	payload, err := json.Marshal(struct {
		CreatedAt string `json:"createdAt"`
		RootID    string `json:"rootId"`
	}{createdAt.UTC().Format(time.RFC3339Nano), rootID.String()})
	if err != nil {
		return "", err
	}
	return "export-root.v1." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeSequenceExportHistoryCursor(cursor string) (sequenceExportHistoryCursor, error) {
	if cursor == "" {
		return sequenceExportHistoryCursor{}, nil
	}
	const prefix = "export-root.v1."
	if !strings.HasPrefix(cursor, prefix) {
		return sequenceExportHistoryCursor{}, ErrInvalidPageCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, prefix))
	if err != nil {
		return sequenceExportHistoryCursor{}, ErrInvalidPageCursor
	}
	var payload struct {
		CreatedAt string `json:"createdAt"`
		RootID    string `json:"rootId"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return sequenceExportHistoryCursor{}, ErrInvalidPageCursor
	}
	id, err := domain.ParseWorkJobID(payload.RootID)
	if err != nil {
		return sequenceExportHistoryCursor{}, fmt.Errorf("%w: export root", ErrInvalidPageCursor)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil || payload.CreatedAt != createdAt.UTC().Format(time.RFC3339Nano) {
		return sequenceExportHistoryCursor{}, ErrInvalidPageCursor
	}
	canonical, err := encodeSequenceExportHistoryCursor(createdAt.UTC(), id)
	if err != nil || canonical != cursor {
		return sequenceExportHistoryCursor{}, ErrInvalidPageCursor
	}
	return sequenceExportHistoryCursor{createdAt: createdAt.UTC(), rootID: id.String()}, nil
}
