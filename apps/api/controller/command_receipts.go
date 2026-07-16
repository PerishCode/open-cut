package controller

import (
	"context"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func recordCommandEvidence(
	ctx context.Context,
	runs *application.AgentRuns,
	projectID domain.ProjectID,
	result any,
	status application.CommandReceiptStatus,
	refs []application.CommandReceiptRef,
	projectRevision domain.Revision,
	activityCursor domain.Cursor,
) error {
	if runs == nil {
		return application.ErrCommandReceiptInvalid
	}
	var revision *domain.Revision
	if projectRevision.Value() > 0 {
		value := projectRevision
		revision = &value
	}
	var cursor *domain.Cursor
	if activityCursor.Value() > 0 {
		value := activityCursor
		cursor = &value
	}
	_, err := runs.RecordEvidence(ctx, projectID, application.CommandEvidenceResult{
		Status: status, Result: result, ResultRefs: refs,
		ProjectRevision: revision, ActivityCursor: cursor,
	})
	return err
}

func commandReceiptRef(kind, id string, revision domain.Revision) application.CommandReceiptRef {
	ref := application.CommandReceiptRef{Kind: kind, ID: id}
	if revision.Value() > 0 {
		value := revision
		ref.Revision = &value
	}
	return ref
}
